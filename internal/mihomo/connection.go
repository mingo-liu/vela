package mihomo

import (
	"errors"
	"fmt"
	"time"
)

type SystemProxy interface {
	Enable(port int) error
	Disable() error
	Recover() error
	Active() (bool, error)
}

func (r *Runner) SetSystemProxy(enabled bool) (State, error) {
	if enabled {
		return r.setMode("system")
	}
	return r.setMode("off")
}

func (r *Runner) SetTun(enabled bool) (State, error) {
	if enabled {
		return r.setMode("tun")
	}
	return r.setMode("off")
}

func (r *Runner) setMode(mode string) (State, error) {
	r.CancelOperation(0)
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	previous := "off"
	if r.state.SystemProxyEnabled {
		previous = "system"
	}
	if r.state.TunEnabled {
		previous = "tun"
	}
	r.mu.Unlock()
	if mode == previous {
		return r.Snapshot(), nil
	}
	if _, err := r.Stop(); err != nil {
		return r.Snapshot(), err
	}
	if mode == "off" {
		return r.Snapshot(), nil
	}
	state, err := r.activate(mode)
	if err == nil {
		return state, nil
	}
	if previous != "off" {
		if _, restoreErr := r.activate(previous); restoreErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("恢复原连接模式失败: %w", restoreErr))
		}
	}
	return r.Snapshot(), err
}

func (r *Runner) activate(mode string) (State, error) {
	if mode == "system" && r.systemProxy == nil {
		return r.Snapshot(), errors.New("当前平台不支持系统代理")
	}
	if _, err := r.start(mode == "tun"); err != nil {
		return r.Snapshot(), err
	}
	if mode == "tun" {
		return r.Snapshot(), nil
	}
	r.mu.Lock()
	if err := r.systemProxy.Enable(r.state.Port); err != nil {
		r.mu.Unlock()
		state, stopErr := r.Stop()
		return state, errors.Join(err, stopErr)
	}
	r.state.SystemProxyEnabled = true
	r.proxyGeneration++
	go r.monitorSystemProxy(r.proxyGeneration)
	r.state.Error = ""
	r.emit()
	state := r.state
	r.mu.Unlock()
	return state, nil
}

func (r *Runner) monitorSystemProxy(generation uint64) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		r.operationMu.Lock()
		r.mu.Lock()
		if !r.state.SystemProxyEnabled || r.proxyGeneration != generation {
			r.mu.Unlock()
			r.operationMu.Unlock()
			return
		}
		active, err := r.systemProxy.Active()
		if err == nil && !active {
			r.mu.Unlock()
			_, stopErr := r.Stop()
			r.mu.Lock()
			r.state.Error = "系统代理已被其他应用改写"
			if stopErr != nil {
				r.state.Error += fmt.Sprintf("；关闭失败: %v", stopErr)
			}
			r.emit()
			r.mu.Unlock()
			r.operationMu.Unlock()
			return
		}
		r.mu.Unlock()
		r.operationMu.Unlock()
	}
}
