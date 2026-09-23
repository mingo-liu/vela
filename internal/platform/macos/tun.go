//go:build darwin

package macos

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
	"go.yaml.in/yaml/v3"
)

const tunInstallDir = "/Library/PrivilegedHelperTools/local.vela.desktop.tun"

// NewTunLauncher installs a root-owned helper on first use or after an update.
// Later TUN starts execute that helper directly, without another password prompt.
func NewTunLauncher(binary, dataDir string) func(configPath, stopPath string) (*exec.Cmd, error) {
	return func(configPath, stopPath string) (*exec.Cmd, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		absBinary, err := filepath.Abs(binary)
		if err != nil {
			return nil, err
		}
		if err := ensureTunHelper(self, absBinary); err != nil {
			return nil, err
		}
		return exec.Command(filepath.Join(tunInstallDir, "helper"), "--vela-tun-helper", dataDir, configPath, stopPath, strconv.Itoa(os.Getpid())), nil
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func UninstallTunHelper() error {
	script := "do shell script " + strconv.Quote("/bin/rm -rf "+shellQuote(tunInstallDir)) + " with administrator privileges with prompt \"移除 Vela Tun 辅助程序\""
	output, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("移除 Tun 辅助程序失败: %w；%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func installedCopyMatches(source, target string, setuid bool) bool {
	sourceHash, err := fileHash(source)
	if err != nil {
		return false
	}
	targetHash, err := fileHash(target)
	if err != nil || sourceHash != targetHash {
		return false
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm() != 0755 {
		return false
	}
	return !setuid || info.Mode()&os.ModeSetuid != 0
}

func ensureTunHelper(self, binary string) error {
	helper := filepath.Join(tunInstallDir, "helper")
	core := filepath.Join(tunInstallDir, "mihomo")
	ownerFile := filepath.Join(tunInstallDir, "owner-uid")
	ownerUID := strconv.Itoa(os.Getuid())
	installedOwner, _ := os.ReadFile(ownerFile)
	if strings.TrimSpace(string(installedOwner)) == ownerUID && installedCopyMatches(self, helper, true) && installedCopyMatches(binary, core, false) {
		return nil
	}
	selfHash, err := fileHash(self)
	if err != nil {
		return err
	}
	coreHash, err := fileHash(binary)
	if err != nil {
		return err
	}
	helperTemp := filepath.Join(tunInstallDir, fmt.Sprintf(".helper-%d", os.Getpid()))
	coreTemp := filepath.Join(tunInstallDir, fmt.Sprintf(".mihomo-%d", os.Getpid()))
	// Check the copied bytes before setting the privilege bit. Every variable
	// path is shell-quoted, and the destination directory is root-owned.
	command := strings.Join([]string{
		"set -e",
		"/usr/bin/install -d -o root -g wheel -m 0755 " + shellQuote(tunInstallDir),
		"/usr/bin/install -o root -g wheel -m 0755 " + shellQuote(self) + " " + shellQuote(helperTemp),
		"/usr/bin/install -o root -g wheel -m 0755 " + shellQuote(binary) + " " + shellQuote(coreTemp),
		"test \"$(/usr/bin/shasum -a 256 " + shellQuote(helperTemp) + " | /usr/bin/cut -d ' ' -f 1)\" = " + shellQuote(selfHash),
		"test \"$(/usr/bin/shasum -a 256 " + shellQuote(coreTemp) + " | /usr/bin/cut -d ' ' -f 1)\" = " + shellQuote(coreHash),
		"/bin/mv -f " + shellQuote(coreTemp) + " " + shellQuote(core),
		"/bin/mv -f " + shellQuote(helperTemp) + " " + shellQuote(helper),
		"/bin/chmod 4755 " + shellQuote(helper),
		"/usr/bin/printf %s " + shellQuote(ownerUID) + " > " + shellQuote(ownerFile),
		"/bin/chmod 0644 " + shellQuote(ownerFile),
	}, "; ")
	script := "do shell script " + strconv.Quote(command) + " with administrator privileges with prompt \"Vela 首次启用 Tun 需要安装辅助程序\""
	output, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 Tun 辅助程序失败: %w；%s", err, strings.TrimSpace(string(output)))
	}
	installedOwner, _ = os.ReadFile(ownerFile)
	if strings.TrimSpace(string(installedOwner)) != ownerUID || !installedCopyMatches(self, helper, true) || !installedCopyMatches(binary, core, false) {
		return errors.New("Tun 辅助程序安装后校验失败")
	}
	return nil
}

// RunTunHelper is entered only by the authorized child process. It owns the
// elevated core and stops it when the GUI exits or writes the stop marker.
func RunTunHelper(args []string) error {
	if os.Geteuid() != 0 || os.Getuid() == 0 {
		return errors.New("Tun helper 需要管理员权限")
	}
	if len(args) != 4 {
		return errors.New("Tun helper 参数无效")
	}
	dataDir, configPath, stopPath := args[0], args[1], args[2]
	parentPID, err := strconv.Atoi(args[3])
	if err != nil || parentPID <= 1 {
		return errors.New("Tun helper 父进程无效")
	}
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(configPath) || !filepath.IsAbs(stopPath) {
		return errors.New("Tun helper 路径必须为绝对路径")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if filepath.Clean(self) != filepath.Join(tunInstallDir, "helper") {
		return errors.New("Tun helper 安装位置无效")
	}
	ownerBytes, err := os.ReadFile(filepath.Join(tunInstallDir, "owner-uid"))
	if err != nil || strings.TrimSpace(string(ownerBytes)) != strconv.Itoa(os.Getuid()) {
		return errors.New("Tun helper 当前用户未授权")
	}
	binary := filepath.Join(tunInstallDir, "mihomo")
	compiled, err := validateTunConfig(dataDir, configPath, stopPath)
	if err != nil {
		return err
	}
	configFile, err := os.CreateTemp(tunInstallDir, ".runtime-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(configFile.Name())
	if _, err := configFile.Write(compiled); err != nil {
		configFile.Close()
		return err
	}
	if err := configFile.Close(); err != nil {
		return err
	}
	cmd := exec.Command(binary, "-d", dataDir, "-f", configFile.Name())
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/var/root"}
	logs := &tunLog{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 Tun 内核失败: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer os.Remove(stopPath)
	for {
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("Tun 内核退出: %w；%s", err, strings.TrimSpace(logs.String()))
			}
			return nil
		case <-ticker.C:
			_, markerErr := os.Stat(stopPath)
			parentErr := syscall.Kill(parentPID, 0)
			if markerErr == nil || errors.Is(parentErr, syscall.ESRCH) {
				_ = cmd.Process.Signal(os.Interrupt)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					_ = cmd.Process.Kill()
					<-done
				}
				return nil
			}
		}
	}
}

func validateTunConfig(dataDir, configPath, stopPath string) ([]byte, error) {
	owner, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		return nil, err
	}
	expectedDir := filepath.Join(owner.HomeDir, "Library", "Application Support", "Vela")
	if filepath.Clean(dataDir) != expectedDir || filepath.Clean(configPath) != filepath.Join(expectedDir, "runtime.yaml") {
		return nil, errors.New("Tun helper 配置路径无效")
	}
	if filepath.Dir(stopPath) != expectedDir || !strings.HasPrefix(filepath.Base(stopPath), "tun-stop-") {
		return nil, errors.New("Tun helper 停止标记无效")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	raw, err := profile.NewStore(dataDir).Load()
	if err != nil {
		return nil, err
	}
	return validateManagedTunConfig(data, raw)
}

func validateManagedTunConfig(data, raw []byte) ([]byte, error) {
	var settings struct {
		MixedPort  int    `yaml:"mixed-port"`
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
	}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	host, portText, err := net.SplitHostPort(settings.Controller)
	if err != nil || host != "127.0.0.1" || settings.MixedPort != 7890 {
		return nil, errors.New("Tun helper 控制地址无效")
	}
	apiPort, err := strconv.Atoi(portText)
	if err != nil || apiPort < 1 || apiPort > 65535 || len(settings.Secret) != 64 {
		return nil, errors.New("Tun helper 控制密钥无效")
	}
	if _, err := hex.DecodeString(settings.Secret); err != nil {
		return nil, errors.New("Tun helper 控制密钥无效")
	}
	expected, err := profile.CompileForMode(raw, 7890, apiPort, settings.Secret, true)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, expected) {
		return nil, errors.New("Tun helper 运行配置与受管配置不一致")
	}
	return expected, nil
}

type tunLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *tunLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = append(l.data, p...)
	if len(l.data) > 16<<10 {
		l.data = append([]byte(nil), l.data[len(l.data)-(16<<10):]...)
	}
	return len(p), nil
}

func (l *tunLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.data)
}
