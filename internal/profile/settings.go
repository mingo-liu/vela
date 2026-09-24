package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	RoutingRule      = "rule"
	RoutingGlobal    = "global"
	RoutingDirect    = "direct"
	LogFromProfile   = "profile"
	DefaultMixedPort = 7890
	LanguageChinese  = "zh-CN"
	LanguageEnglish  = "en-US"
)

type Settings struct {
	MixedPort       int    `json:"mixedPort"`
	RoutingMode     string `json:"routingMode"`
	AutoConnect     bool   `json:"autoConnect"`
	AutoConnectMode string `json:"autoConnectMode"`
	LogLevel        string `json:"logLevel"`
	LaunchAtLogin   bool   `json:"launchAtLogin"`
	Language        string `json:"language"`
}

func DefaultSettings() Settings {
	return Settings{MixedPort: DefaultMixedPort, RoutingMode: RoutingRule, AutoConnectMode: "system", LogLevel: LogFromProfile, Language: LanguageChinese}
}

func ValidLanguage(language string) bool {
	return language == LanguageChinese || language == LanguageEnglish
}

func ValidMixedPort(port int) bool { return port >= 1024 && port <= 65535 }

func ValidRoutingMode(mode string) bool {
	return mode == RoutingRule || mode == RoutingGlobal || mode == RoutingDirect
}

func ValidLogLevel(level string) bool {
	switch level {
	case LogFromProfile, "silent", "error", "warning", "info", "debug":
		return true
	}
	return false
}

func validSettings(settings Settings) bool {
	return ValidMixedPort(settings.MixedPort) && ValidRoutingMode(settings.RoutingMode) &&
		(settings.AutoConnectMode == "system" || settings.AutoConnectMode == "tun") &&
		ValidLogLevel(settings.LogLevel) && ValidLanguage(settings.Language)
}

func (s *Store) settingsPath() string {
	return filepath.Join(filepath.Dir(s.path), "settings.json")
}

func (s *Store) readSettings() (Settings, error) {
	settings := DefaultSettings()
	data, err := os.ReadFile(s.settingsPath())
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(data, &settings); err != nil || !validSettings(settings) {
		return Settings{}, fmt.Errorf("无法读取已保存的应用设置")
	}
	return settings, nil
}

func (s *Store) Settings() (Settings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.readSettings()
}

func (s *Store) UpdateSettings(update func(*Settings) error) (Settings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	settings, err := s.readSettings()
	if err != nil {
		return Settings{}, err
	}
	if err := update(&settings); err != nil {
		return Settings{}, err
	}
	if !validSettings(settings) {
		return Settings{}, errors.New("无效的应用设置")
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return Settings{}, err
	}
	if err := writeProfile(s.settingsPath(), string(data)); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func (s *Store) RoutingMode() (string, error) {
	settings, err := s.Settings()
	return settings.RoutingMode, err
}

func (s *Store) SaveRoutingMode(mode string) error {
	if !ValidRoutingMode(mode) {
		return errors.New("无效的代理模式")
	}
	_, err := s.UpdateSettings(func(settings *Settings) error {
		settings.RoutingMode = mode
		return nil
	})
	return err
}
