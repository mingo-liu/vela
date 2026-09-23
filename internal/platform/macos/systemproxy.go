package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type commandRunner func(name string, args ...string) (string, error)

type proxyEntry struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    string `json:"port"`
	Auth    bool   `json:"auth"`
}

type autoProxy struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type proxySnapshot struct {
	Service string     `json:"service"`
	Port    int        `json:"velaPort"`
	HTTP    proxyEntry `json:"http"`
	HTTPS   proxyEntry `json:"https"`
	SOCKS   proxyEntry `json:"socks"`
	PAC     autoProxy  `json:"pac"`
	Auto    bool       `json:"autoDiscover"`
}

// SystemProxy manages only the active macOS network service. The snapshot is
// stored before any mutation so a later app launch can recover after a crash.
type SystemProxy struct {
	path            string
	run             commandRunner
	watch           *os.File
	watchEnabled    bool
	watchExecutable string
}

func NewSystemProxy(dataDir string) *SystemProxy {
	return &SystemProxy{path: filepath.Join(dataDir, "system-proxy-snapshot.json"), run: systemCommand, watchEnabled: true}
}

func (p *SystemProxy) startWatcher() error {
	if !p.watchEnabled || p.watch != nil {
		return nil
	}
	self := p.watchExecutable
	if self == "" {
		var err error
		self, err = os.Executable()
		if err != nil {
			return err
		}
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "--vela-proxy-watch", p.path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = read, io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		read.Close()
		write.Close()
		return err
	}
	read.Close()
	p.watch = write
	go func() { _ = cmd.Wait() }()
	return nil
}

// WatchSystemProxy runs in a separate process. EOF means the GUI process has
// exited, including after an abnormal exit, so any proxy still owned by Vela
// must be restored.
func WatchSystemProxy(path string) error {
	_, _ = io.Copy(io.Discard, os.Stdin)
	p := &SystemProxy{path: path, run: systemCommand}
	return p.Recover()
}

func systemCommand(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s 失败: %s", args[0], detail)
	}
	return string(out), nil
}

func (p *SystemProxy) command(args ...string) (string, error) {
	return p.run("networksetup", args...)
}

func (p *SystemProxy) activeService() (string, error) {
	route, err := p.run("route", "-n", "get", "default")
	if err != nil {
		return "", err
	}
	device := regexp.MustCompile(`(?m)^\s*interface:\s*(\S+)`).FindStringSubmatch(route)
	if len(device) != 2 {
		return "", errors.New("无法确定当前默认网络接口")
	}
	order, err := p.command("-listnetworkserviceorder")
	if err != nil {
		return "", err
	}
	sections := regexp.MustCompile(`(?m)^\(\d+\)\s+([^\n]+)\n\(Hardware Port:[^\n]*Device:\s*([^\)]+)\)`).FindAllStringSubmatch(order, -1)
	for _, section := range sections {
		if strings.TrimSpace(section[2]) == device[1] && !strings.HasPrefix(section[1], "*") {
			return strings.TrimSpace(section[1]), nil
		}
	}
	return "", fmt.Errorf("未找到网络接口 %s 对应的服务", device[1])
}

func fields(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func (p *SystemProxy) readProxy(service, kind string) (proxyEntry, error) {
	out, err := p.command("-get"+kind, service)
	if err != nil {
		return proxyEntry{}, err
	}
	v := fields(out)
	return proxyEntry{Enabled: v["Enabled"] == "Yes", Server: v["Server"], Port: v["Port"], Auth: v["Authenticated Proxy Enabled"] == "1"}, nil
}

func (p *SystemProxy) read(service string, port int) (proxySnapshot, error) {
	s := proxySnapshot{Service: service, Port: port}
	var err error
	if s.HTTP, err = p.readProxy(service, "webproxy"); err != nil {
		return s, err
	}
	if s.HTTPS, err = p.readProxy(service, "securewebproxy"); err != nil {
		return s, err
	}
	if s.SOCKS, err = p.readProxy(service, "socksfirewallproxy"); err != nil {
		return s, err
	}
	out, err := p.command("-getautoproxyurl", service)
	if err != nil {
		return s, err
	}
	v := fields(out)
	s.PAC = autoProxy{Enabled: v["Enabled"] == "Yes", URL: v["URL"]}
	out, err = p.command("-getproxyautodiscovery", service)
	if err != nil {
		return s, err
	}
	s.Auto = fields(out)["Auto Proxy Discovery"] == "On"
	return s, nil
}

func owned(entry proxyEntry, port int) bool {
	return entry.Enabled && entry.Server == "127.0.0.1" && entry.Port == strconv.Itoa(port)
}

func (p *SystemProxy) writeSnapshot(s proxySnapshot) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p.path), ".system-proxy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p.path)
}

func (p *SystemProxy) snapshot() (proxySnapshot, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return proxySnapshot{}, err
	}
	var s proxySnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	if s.Service == "" || s.Port < 1 || s.Port > 65535 {
		return s, errors.New("系统代理恢复记录无效")
	}
	return s, nil
}

func (p *SystemProxy) setProxy(service, kind string, entry proxyEntry) error {
	if entry.Server != "" && entry.Port != "" && entry.Port != "0" {
		if _, err := p.command("-set"+kind, service, entry.Server, entry.Port); err != nil {
			return err
		}
	}
	state := "off"
	if entry.Enabled {
		state = "on"
	}
	_, err := p.command("-set"+kind+"state", service, state)
	return err
}

func (p *SystemProxy) Enable(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("代理端口无效")
	}
	if _, err := os.Stat(p.path); err == nil {
		if err := p.Disable(); err != nil {
			return fmt.Errorf("恢复上次系统代理失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	service, err := p.activeService()
	if err != nil {
		return err
	}
	s, err := p.read(service, port)
	if err != nil {
		return err
	}
	if s.HTTP.Auth || s.HTTPS.Auth || s.SOCKS.Auth {
		return errors.New("当前网络服务使用了需要认证的代理，Vela 无法安全保存其凭据")
	}
	if s.PAC.Enabled && s.PAC.URL == "" {
		return errors.New("当前 PAC 地址为空，无法安全恢复")
	}
	if err := p.writeSnapshot(s); err != nil {
		return err
	}
	if err := p.startWatcher(); err != nil {
		_ = os.Remove(p.path)
		return fmt.Errorf("启动系统代理恢复监视进程失败: %w", err)
	}
	entry := proxyEntry{Enabled: true, Server: "127.0.0.1", Port: strconv.Itoa(port)}
	rollback := func(cause error) error {
		if err := p.Disable(); err != nil {
			return errors.Join(cause, fmt.Errorf("系统代理回滚失败: %w", err))
		}
		return cause
	}
	for _, kind := range []string{"webproxy", "securewebproxy", "socksfirewallproxy"} {
		if err := p.setProxy(service, kind, entry); err != nil {
			return rollback(err)
		}
	}
	if _, err := p.command("-setautoproxystate", service, "off"); err != nil {
		return rollback(err)
	}
	if _, err := p.command("-setproxyautodiscovery", service, "off"); err != nil {
		return rollback(err)
	}
	active, err := p.Active()
	if err != nil {
		return rollback(err)
	}
	if !active {
		return rollback(errors.New("系统代理设置已被其他应用改写"))
	}
	return nil
}

func (p *SystemProxy) Active() (bool, error) {
	s, err := p.snapshot()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current, err := p.read(s.Service, s.Port)
	if err != nil {
		return false, err
	}
	return owned(current.HTTP, s.Port) && owned(current.HTTPS, s.Port) && owned(current.SOCKS, s.Port) && !current.PAC.Enabled && !current.Auto, nil
}

// Disable restores each proxy only while it still points to Vela. A later
// change made by another client remains untouched.
func (p *SystemProxy) Disable() error {
	s, err := p.snapshot()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	current, err := p.read(s.Service, s.Port)
	if err != nil {
		return err
	}
	var problems []error
	allOwned := owned(current.HTTP, s.Port) && owned(current.HTTPS, s.Port) && owned(current.SOCKS, s.Port)
	for _, item := range []struct {
		kind              string
		current, previous proxyEntry
	}{
		{"webproxy", current.HTTP, s.HTTP}, {"securewebproxy", current.HTTPS, s.HTTPS}, {"socksfirewallproxy", current.SOCKS, s.SOCKS},
	} {
		if owned(item.current, s.Port) {
			if err := p.setProxy(s.Service, item.kind, item.previous); err != nil {
				problems = append(problems, err)
			}
		}
	}
	if allOwned {
		if s.PAC.Enabled && !current.PAC.Enabled {
			if _, err := p.command("-setautoproxyurl", s.Service, s.PAC.URL); err != nil {
				problems = append(problems, err)
			}
		}
		if s.Auto && !current.Auto {
			if _, err := p.command("-setproxyautodiscovery", s.Service, "on"); err != nil {
				problems = append(problems, err)
			}
		}
	}
	if len(problems) > 0 {
		return errors.Join(problems...)
	}
	return os.Remove(p.path)
}

func (p *SystemProxy) Recover() error { return p.Disable() }
