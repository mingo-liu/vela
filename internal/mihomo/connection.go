package mihomo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type SystemProxy interface {
	Enable(port int) error
	Disable() error
	Recover() error
	Active() (bool, error)
	Refresh() (bool, error)
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
	if _, err := r.stop(); err != nil {
		return r.Snapshot(), err
	}
	if mode == "off" {
		return r.Snapshot(), nil
	}
	state, err := r.activate(ctx, mode)
	if err == nil {
		return state, nil
	}
	if previous != "off" && ctx.Err() == nil {
		if _, restoreErr := r.activate(ctx, previous); restoreErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("恢复原连接模式失败: %w", restoreErr))
		}
	}
	return r.Snapshot(), err
}

func (r *Runner) activate(ctx context.Context, mode string) (State, error) {
	if mode == "system" && r.systemProxy == nil {
		return r.Snapshot(), errors.New("当前平台不支持系统代理")
	}
	if _, err := r.startWithContext(ctx, mode == "tun"); err != nil {
		return r.Snapshot(), err
	}
	if mode == "tun" {
		return r.Snapshot(), nil
	}
	r.mu.Lock()
	port := r.state.Port
	r.mu.Unlock()
	if err := r.systemProxy.Enable(port); err != nil {
		state, stopErr := r.stop()
		return state, errors.Join(err, stopErr)
	}
	if err := ctx.Err(); err != nil {
		state, stopErr := r.stop()
		return state, errors.Join(operationError(err), stopErr)
	}
	r.mu.Lock()
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
		if !r.refreshSystemProxy(generation) {
			return
		}
	}
}

func (r *Runner) refreshSystemProxy(generation uint64) bool {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.state.SystemProxyEnabled || r.proxyGeneration != generation {
		return false
	}
	r.mu.Unlock()
	active, err := r.systemProxy.Refresh()
	r.mu.Lock()
	const checkError = "检查系统代理失败: "
	if active {
		if err != nil {
			r.state.Error = checkError + err.Error()
			r.emit()
		} else if strings.HasPrefix(r.state.Error, checkError) {
			r.state.Error = ""
			r.emit()
		}
		return true
	}
	r.mu.Unlock()
	_, stopErr := r.stop()
	r.mu.Lock()
	r.state.Error = "系统代理已被其他应用改写"
	if err != nil {
		r.state.Error = err.Error()
	}
	if stopErr != nil {
		r.state.Error += fmt.Sprintf("；关闭失败: %v", stopErr)
	}
	r.emit()
	return false
}
