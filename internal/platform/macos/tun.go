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

	"github.com/mingo-liu/vela/internal/profile"
	"go.yaml.in/yaml/v3"
)

const tunInstallDir = "/Library/PrivilegedHelperTools/local.vela.desktop.tun"
const tunServiceID = "local.vela.desktop.tun"
const tunServicePlist = "/Library/LaunchDaemons/" + tunServiceID + ".plist"

// NewTunLauncher installs the standalone service and approved core when they change.
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
		service := filepath.Join(filepath.Dir(self), "..", "Resources", "vela-tun-service")
		if _, err := os.Stat(service); errors.Is(err, os.ErrNotExist) {
			service = filepath.Join("build", "resources", "vela-tun-service")
		}
		service, err = filepath.Abs(service)
		if err != nil {
			return nil, err
		}
		if err := ensureTunService(service, absBinary); err != nil {
			return nil, err
		}
		return exec.Command(self, "--vela-tun-client", dataDir, configPath, stopPath), nil
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func UninstallTunService() error {
	command := "/bin/launchctl bootout system/" + tunServiceID + " 2>/dev/null || true; /bin/rm -f " + shellQuote(tunServicePlist) + "; /bin/rm -rf " + shellQuote(tunInstallDir)
	script := "do shell script " + strconv.Quote(command) + " with administrator privileges with prompt \"移除 Vela Tun 服务\""
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

func installedCopyMatches(source, target string) bool {
	sourceHash, err := fileHash(source)
	if err != nil {
		return false
	}
	targetHash, err := fileHash(target)
	if err != nil || sourceHash != targetHash {
		return false
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || info.Mode().Perm() != 0755 {
		return false
	}
	return info.Mode()&os.ModeSetuid == 0
}

func ensureTunService(service, binary string) error {
	helper := filepath.Join(tunInstallDir, "service")
	core := filepath.Join(tunInstallDir, "mihomo")
	ownerFile := filepath.Join(tunInstallDir, "owner-uid")
	ownerUID := strconv.Itoa(os.Getuid())
	installedOwner, _ := os.ReadFile(ownerFile)
	if strings.TrimSpace(string(installedOwner)) == ownerUID && installedCopyMatches(service, helper) && installedCopyMatches(binary, core) && serviceAvailable() {
		return nil
	}
	serviceHash, err := fileHash(service)
	if err != nil {
		return err
	}
	coreHash, err := fileHash(binary)
	if err != nil {
		return err
	}
	helperTemp := filepath.Join(tunInstallDir, fmt.Sprintf(".service-%d", os.Getpid()))
	coreTemp := filepath.Join(tunInstallDir, fmt.Sprintf(".mihomo-%d", os.Getpid()))
	plist := "<?xml version=\"1.0\" encoding=\"UTF-8\"?><!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\"><plist version=\"1.0\"><dict><key>Label</key><string>" + tunServiceID + "</string><key>ProgramArguments</key><array><string>" + helper + "</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>"
	command := strings.Join([]string{
		"set -e",
		"/usr/bin/install -d -o root -g wheel -m 0755 " + shellQuote(tunInstallDir),
		"/usr/bin/install -o root -g wheel -m 0755 " + shellQuote(service) + " " + shellQuote(helperTemp),
		"/usr/bin/install -o root -g wheel -m 0755 " + shellQuote(binary) + " " + shellQuote(coreTemp),
		"test \"$(/usr/bin/shasum -a 256 " + shellQuote(helperTemp) + " | /usr/bin/cut -d ' ' -f 1)\" = " + shellQuote(serviceHash),
		"test \"$(/usr/bin/shasum -a 256 " + shellQuote(coreTemp) + " | /usr/bin/cut -d ' ' -f 1)\" = " + shellQuote(coreHash),
		"/bin/launchctl bootout system/" + tunServiceID + " 2>/dev/null || true",
		"/bin/mv -f " + shellQuote(coreTemp) + " " + shellQuote(core),
		"/bin/mv -f " + shellQuote(helperTemp) + " " + shellQuote(helper),
		"/bin/chmod 0755 " + shellQuote(helper),
		"/bin/rm -f " + shellQuote(filepath.Join(tunInstallDir, "helper")),
		"/usr/bin/printf %s " + shellQuote(ownerUID) + " > " + shellQuote(ownerFile),
		"/usr/sbin/chown root:wheel " + shellQuote(ownerFile),
		"/bin/chmod 0644 " + shellQuote(ownerFile),
		"/usr/bin/printf %s " + shellQuote(plist) + " > " + shellQuote(tunServicePlist),
		"/usr/sbin/chown root:wheel " + shellQuote(tunServicePlist),
		"/bin/chmod 0644 " + shellQuote(tunServicePlist),
		"/bin/launchctl bootstrap system " + shellQuote(tunServicePlist),
	}, "; ")
	script := "do shell script " + strconv.Quote(command) + " with administrator privileges with prompt \"Vela Tun 需要安装系统服务\""
	output, err := exec.Command("/usr/bin/osascript", "-e", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 Tun 辅助程序失败: %w；%s", err, strings.TrimSpace(string(output)))
	}
	installedOwner, _ = os.ReadFile(ownerFile)
	if strings.TrimSpace(string(installedOwner)) != ownerUID || !installedCopyMatches(service, helper) || !installedCopyMatches(binary, core) {
		return errors.New("Tun 辅助程序安装后校验失败")
	}
	return nil
}

func validateTunConfig(uid int, dataDir, configPath, stopPath string) ([]byte, error) {
	owner, err := user.LookupId(strconv.Itoa(uid))
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
	store := profile.NewStore(dataDir)
	raw, err := store.Load()
	if err != nil {
		return nil, err
	}
	routingMode, err := store.RoutingMode()
	if err != nil {
		return nil, err
	}
	return validateManagedTunConfig(data, raw, routingMode)
}

func validateManagedTunConfig(data, raw []byte, routingMode string) ([]byte, error) {
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
	expected, err := profile.CompileForMode(raw, 7890, apiPort, settings.Secret, true, routingMode)
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
