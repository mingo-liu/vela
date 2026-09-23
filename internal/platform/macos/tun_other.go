//go:build !darwin

package macos

import (
	"errors"
	"os/exec"
)

func NewTunLauncher(_, _ string) func(configPath, stopPath string) (*exec.Cmd, error) {
	return nil
}

func RunTunHelper(_ []string) error { return errors.New("当前平台不支持 Tun 模式") }

func UninstallTunHelper() error { return errors.New("当前平台不支持 Tun 模式") }
