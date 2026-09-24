package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	RoutingRule   = "rule"
	RoutingGlobal = "global"
	RoutingDirect = "direct"
)

func ValidRoutingMode(mode string) bool {
	return mode == RoutingRule || mode == RoutingGlobal || mode == RoutingDirect
}

func (s *Store) RoutingMode() (string, error) {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return RoutingRule, nil
	}
	if err != nil {
		return "", err
	}
	var settings struct {
		RoutingMode string `json:"routingMode"`
	}
	if err := json.Unmarshal(data, &settings); err != nil || !ValidRoutingMode(settings.RoutingMode) {
		return "", fmt.Errorf("无法读取已保存的代理模式")
	}
	return settings.RoutingMode, nil
}

func (s *Store) SaveRoutingMode(mode string) error {
	if !ValidRoutingMode(mode) {
		return errors.New("无效的代理模式")
	}
	data, err := json.Marshal(struct {
		RoutingMode string `json:"routingMode"`
	}{RoutingMode: mode})
	if err != nil {
		return err
	}
	return writeProfile(filepath.Join(filepath.Dir(s.path), "settings.json"), string(data))
}
