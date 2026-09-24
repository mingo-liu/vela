package desktop

import (
	"errors"
	"os/exec"
	"runtime"
	"sync"

	"github.com/mingo-liu/vela/internal/mihomo"
	"github.com/mingo-liu/vela/internal/platform/macos"
	"github.com/mingo-liu/vela/internal/profile"
)

// RuntimeService is the Wails boundary for Vela's connection modes.
// It exposes no controller secret or raw controller URL.
type RuntimeService struct {
	runner           *mihomo.Runner
	store            *profile.Store
	dataDir          string
	settingsMu       sync.Mutex
	onLanguageChange func(string)
}

func NewRuntimeService(runner *mihomo.Runner, store *profile.Store, dataDir string, onLanguageChange func(string)) *RuntimeService {
	return &RuntimeService{runner: runner, store: store, dataDir: dataDir, onLanguageChange: onLanguageChange}
}

func (s *RuntimeService) SetLanguage(language string) (profile.Settings, error) {
	if !profile.ValidLanguage(language) {
		return profile.Settings{}, errors.New("无效的界面语言")
	}
	settings, err := s.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.Language = language
		return nil
	})
	if err == nil && s.onLanguageChange != nil {
		s.onLanguageChange(language)
	}
	return settings, err
}

func (s *RuntimeService) State() mihomo.State { return s.runner.Snapshot() }

func (s *RuntimeService) Logs() string { return s.runner.Logs() }

func (s *RuntimeService) CoreInfo() (mihomo.CoreInfo, error) { return s.runner.CoreInfo() }

func (s *RuntimeService) ImportProfile(contents string) (mihomo.State, error) {
	return s.runner.Import(contents)
}

func (s *RuntimeService) ImportSubscription(address string) (mihomo.State, error) {
	return s.runner.ImportSubscription(address)
}

func (s *RuntimeService) Subscriptions() ([]profile.Subscription, error) {
	return s.runner.Subscriptions()
}

func (s *RuntimeService) UpdateSubscription(id string) (mihomo.State, error) {
	return s.runner.UpdateSubscription(id)
}

func (s *RuntimeService) SelectSubscription(id string) (mihomo.State, error) {
	return s.runner.SelectSubscription(id)
}

func (s *RuntimeService) SetSystemProxy(enabled bool) (mihomo.State, error) {
	return s.runner.SetSystemProxy(enabled)
}

func (s *RuntimeService) SetTun(enabled bool) (mihomo.State, error) {
	return s.runner.SetTun(enabled)
}

func (s *RuntimeService) SetRoutingMode(mode string) (mihomo.State, error) {
	return s.runner.SetRoutingMode(mode)
}

func (s *RuntimeService) SetMixedPort(port int) (mihomo.State, error) {
	return s.runner.SetMixedPort(port)
}

func (s *RuntimeService) Settings() (profile.Settings, error) {
	settings, err := s.store.Settings()
	if err != nil {
		return settings, err
	}
	if runtime.GOOS == "darwin" {
		settings.LaunchAtLogin, err = macos.LoginLaunchEnabled()
	}
	return settings, err
}

func (s *RuntimeService) SetLaunchAtLogin(enabled bool) (profile.Settings, error) {
	if runtime.GOOS != "darwin" {
		return profile.Settings{}, errors.New("当前平台不支持登录时启动")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	before, err := s.Settings()
	if err != nil {
		return before, err
	}
	if before.LaunchAtLogin == enabled {
		return before, nil
	}
	if err := macos.SetLoginLaunch(enabled); err != nil {
		return before, err
	}
	settings, err := s.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.LaunchAtLogin = enabled
		return nil
	})
	if err != nil {
		_ = macos.SetLoginLaunch(before.LaunchAtLogin)
		return before, err
	}
	return settings, nil
}

func (s *RuntimeService) SetAutoConnect(enabled bool) (profile.Settings, error) {
	return s.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.AutoConnect = enabled
		return nil
	})
}

func (s *RuntimeService) SetAutoConnectMode(mode string) (profile.Settings, error) {
	if mode != "system" && mode != "tun" {
		return profile.Settings{}, errors.New("无效的自动连接方式")
	}
	if mode == "tun" && !s.runner.Snapshot().TunSupported {
		return profile.Settings{}, errors.New("当前平台不支持 Tun 模式")
	}
	return s.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.AutoConnectMode = mode
		return nil
	})
}

func (s *RuntimeService) SetLogLevel(level string) (profile.Settings, error) {
	return s.runner.SetLogLevel(level)
}

func (s *RuntimeService) OpenConfigDirectory() error {
	if runtime.GOOS != "darwin" {
		return errors.New("当前平台暂不支持打开配置目录")
	}
	return exec.Command("open", s.dataDir).Run()
}

func (s *RuntimeService) Groups() ([]mihomo.Group, error) { return s.runner.Groups() }

func (s *RuntimeService) NodeNames() ([]string, error) { return s.runner.NodeNames() }

func (s *RuntimeService) TestGroupDelay(group string) (map[string]int, error) {
	return s.runner.TestGroupDelay(group)
}

func (s *RuntimeService) Select(group, option string) error {
	return s.runner.Select(group, option)
}
