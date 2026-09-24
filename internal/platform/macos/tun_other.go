//go:build !darwin

package macos

import (
	"errors"
	"os/exec"
)

func NewTunLauncher(_, _ string, _ func() string) func(configPath, stopPath string) (*exec.Cmd, error) {
	return nil
}

func RunTunClient(_ []string) error { return errors.New("当前平台不支持 Tun 模式") }

func RunTunService() error { return errors.New("当前平台不支持 Tun 模式") }

func UninstallTunService() error { return errors.New("当前平台不支持 Tun 模式") }
