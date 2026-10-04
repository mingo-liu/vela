package mihomo

import (
	"errors"
	"fmt"
	"net"

	"github.com/mingo-liu/vela/internal/profile"
)

func (r *Runner) SetRoutingMode(mode string) (State, error) {
	if !profile.ValidRoutingMode(mode) {
		return r.Snapshot(), errors.New("无效的代理模式")
	}
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	previous := r.state.RoutingMode
	if mode == previous {
		return r.state, nil
	}
	if r.state.Status == "running" {
		if err := r.patchRoutingMode(mode); err != nil {
			return r.state, fmt.Errorf("切换代理模式失败: %w", err)
		}
	}
	if err := r.store.SaveRoutingMode(mode); err != nil {
		if r.state.Status == "running" {
			return r.state, errors.Join(fmt.Errorf("保存代理模式失败: %w", err), r.patchRoutingMode(previous))
		}
		return r.state, fmt.Errorf("保存代理模式失败: %w", err)
	}
	r.state.RoutingMode = mode
	r.state.Error = ""
	r.emit()
	return r.state, nil
}

func (r *Runner) SetLogLevel(level string) (profile.Settings, error) {
	if !profile.ValidLogLevel(level) {
		return profile.Settings{}, errors.New("无效的日志级别")
	}
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.LogLevel = level
		return nil
	})
}

// SetMixedPort restarts an active connection so the listener and system proxy
// always use the same port. A failed restart restores the saved port and mode.
func (r *Runner) SetMixedPort(port int) (State, error) {
	if !profile.ValidMixedPort(port) {
		return r.Snapshot(), errors.New("本地代理端口必须在 1024–65535 之间")
	}
	ctx, finish, err := r.beginConnection()
	if err != nil {
		return r.Snapshot(), err
	}
	defer finish()
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return r.Snapshot(), operationError(err)
	}
	r.mu.Lock()
	if r.cmd != nil && r.state.Status != "running" {
		state := r.state
		r.mu.Unlock()
		return state, errors.New("内核正在切换状态，请稍后重试")
	}
	previousPort := r.state.Port
	previousMode := "off"
	if r.state.SystemProxyEnabled {
		previousMode = "system"
	} else if r.state.TunEnabled {
		previousMode = "tun"
	}
	wasRunning := r.cmd != nil
	needsRestart := wasRunning || previousMode != "off"
	r.mu.Unlock()
	if port == previousPort {
		return r.Snapshot(), nil
	}
	if err := checkMixedPortAvailable(port); err != nil {
		return r.Snapshot(), err
	}
	before, err := r.store.Settings()
	if err != nil {
		return r.Snapshot(), err
	}
	restart := func() (State, error) {
		if previousMode != "off" {
			return r.activate(ctx, previousMode)
		}
		if wasRunning {
			return r.startWithContext(ctx, false)
		}
		return r.Snapshot(), nil
	}
	if needsRestart {
		if _, err := r.stop(); err != nil {
			return r.Snapshot(), err
		}
	}
	if _, err := r.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.MixedPort = port
		return nil
	}); err != nil {
		_, restoreErr := restart()
		return r.Snapshot(), errors.Join(err, restoreErr)
	}
	r.setMixedPortState(port)
	if !needsRestart {
		return r.Snapshot(), nil
	}
	if _, err := restart(); err != nil {
		if _, stopErr := r.stop(); stopErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("停止新端口连接失败: %w", stopErr))
		}
		if _, saveErr := r.store.UpdateSettings(func(settings *profile.Settings) error {
			settings.MixedPort = before.MixedPort
			return nil
		}); saveErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("恢复原端口设置失败: %w", saveErr))
		}
		r.setMixedPortState(previousPort)
		if ctx.Err() != nil {
			return r.Snapshot(), operationError(ctx.Err())
		}
		_, restoreErr := restart()
		return r.Snapshot(), errors.Join(fmt.Errorf("切换本地代理端口失败: %w", err), restoreErr)
	}
	return r.Snapshot(), nil
}

func (r *Runner) setMixedPortState(port int) {
	r.mu.Lock()
	r.state.Port = port
	r.state.Error = ""
	if r.cmd == nil {
		r.state.Status = "stopped"
	}
	r.emit()
	r.mu.Unlock()
}

func checkMixedPortAvailable(port int) error {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	tcp, err := net.Listen("tcp", address)
	if err != nil {
		return mixedPortUnavailable(port, err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", address)
	if err != nil {
		return mixedPortUnavailable(port, err)
	}
	return udp.Close()
}

func mixedPortUnavailable(port int, err error) error {
	return fmt.Errorf("本地代理端口 %d 不可用: %w；请关闭占用此端口的应用或在设置中更换端口", port, err)
}
