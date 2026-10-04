package mihomo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runner) Start() (State, error) {
	ctx, finish, err := r.beginConnection()
	if err != nil {
		return r.Snapshot(), err
	}
	defer finish()
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.startWithContext(ctx, false)
}

// The caller holds operationMu. Slow work temporarily releases mu; process exit
// handling uses operationMu too, so it cannot change the session mid-startup.
func (r *Runner) startWithContext(ctx context.Context, tun bool) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return r.state, operationError(err)
	}
	if r.cmd != nil {
		return r.state, nil
	}
	if r.binary == "" {
		return r.fail(errors.New("未找到 mihomo 内核；请先运行资源准备任务"))
	}
	if tun && r.tunLauncher == nil {
		return r.fail(errors.New("当前平台不支持 Tun 模式"))
	}
	raw, err := r.store.Load()
	if err != nil {
		return r.fail(err)
	}
	// mihomo can keep its controller alive after the mixed listener fails.
	// Check both TCP and UDP before starting, so another proxy is not mistaken
	// for our listener and port conflicts do not become readiness timeouts.
	mixedPort := r.state.Port
	r.mu.Unlock()
	err = checkMixedPortAvailable(mixedPort)
	r.mu.Lock()
	if err != nil || ctx.Err() != nil {
		return r.failStartup(ctx, err)
	}
	if err := os.MkdirAll(r.dataDir, 0700); err != nil {
		return r.fail(err)
	}
	r.state.Status, r.state.Error = "starting", ""
	r.logs = &tailWriter{}
	r.emit()
	r.mu.Unlock()
	err = r.ensureGeoIPDatabase(raw)
	r.mu.Lock()
	if err != nil || ctx.Err() != nil {
		return r.failStartup(ctx, err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return r.fail(err)
	}
	r.secret = hex.EncodeToString(secret)
	for attempt := 0; attempt < 3; attempt++ {
		port, err := freePort()
		if err != nil {
			return r.fail(err)
		}
		if port == r.state.Port {
			continue
		}
		r.apiPort = port
		compiled, err := r.compile(raw, port, tun)
		if err != nil {
			return r.fail(err)
		}
		configPath := filepath.Join(r.dataDir, "runtime.yaml")
		r.mu.Unlock()
		err = writePrivate(configPath, compiled)
		r.mu.Lock()
		if err != nil || ctx.Err() != nil {
			return r.failStartup(ctx, err)
		}
		// Starting mihomo parses the same config. For the system proxy we can
		// wait for readiness before changing macOS settings instead of launching
		// a second process solely to validate it. Keep the check for Tun so an
		// invalid config cannot trigger an unnecessary authorization prompt.
		if tun {
			checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			check := exec.CommandContext(checkCtx, r.binary, "-t", "-d", r.dataDir, "-f", configPath)
			check.Env = cleanEnv()
			check.WaitDelay = time.Second
			r.mu.Unlock()
			_, err = check.CombinedOutput()
			cancel()
			r.mu.Lock()
			if err != nil || ctx.Err() != nil {
				return r.failStartup(ctx, fmt.Errorf("内核配置校验失败（%v）；请检查节点与规则内容", err))
			}
		}
		var cmd *exec.Cmd
		if tun {
			stopPath := filepath.Join(r.dataDir, fmt.Sprintf("tun-stop-%s", r.secret))
			launcher := r.tunLauncher
			r.mu.Unlock()
			cmd, err = launcher(ctx, configPath, stopPath)
			r.mu.Lock()
			if err != nil || ctx.Err() != nil {
				return r.failStartup(ctx, fmt.Errorf("Tun 服务启动失败: %w", err))
			}
			r.tunStopPath = stopPath
		} else {
			cmd = exec.Command(r.binary, "-d", r.dataDir, "-f", configPath)
			cmd.Env = cleanEnv()
		}
		cmd.Stdout, cmd.Stderr = r.logs, r.logs
		cmd.WaitDelay = time.Second
		if err := cmd.Start(); err != nil {
			r.tunStopPath = ""
			return r.fail(err)
		}
		done := make(chan struct{})
		go func() {
			err := cmd.Wait()
			close(done)
			r.operationMu.Lock()
			defer r.operationMu.Unlock()
			r.mu.Lock()
			if r.cmd == cmd {
				var proxyErr error
				if r.systemProxy != nil {
					r.mu.Unlock()
					proxyErr = r.systemProxy.Disable()
					r.mu.Lock()
				}
				r.cmd = nil
				r.state.SystemProxyEnabled = proxyErr != nil
				r.state.TunEnabled = false
				r.tunStopPath = ""
				r.proxyGeneration++
				if r.state.Status != "stopping" {
					r.state.Status = "failed"
					r.state.Error = fmt.Sprintf("内核退出: %v", err)
					if proxyErr != nil {
						r.state.Error += fmt.Sprintf("；系统代理恢复失败: %v", proxyErr)
					}
					r.emit()
				}
			}
			r.mu.Unlock()
		}()
		r.cmd, r.done = cmd, done
		readyTimeout := 12 * time.Second
		if tun {
			readyTimeout = 2 * time.Minute
		}
		if err := r.waitReadyContext(ctx, done, readyTimeout); err != nil {
			return r.abortStartup(ctx, cmd, done, err)
		}
		if err := r.restoreSelectedOptionsContext(ctx); err != nil {
			_, _ = r.logs.Write([]byte(fmt.Sprintf("level=warning 保存的节点选择恢复失败: %v\n", err)))
		}
		if err := ctx.Err(); err != nil {
			return r.abortStartup(ctx, cmd, done, err)
		}
		select {
		case <-done:
			return r.abortStartup(ctx, cmd, done, errors.New("进程提前退出"))
		default:
		}
		r.state.Status = "running"
		r.state.TunEnabled = tun
		r.emit()
		return r.state, nil
	}
	return r.fail(errors.New("控制端口无法分配"))
}

// failStartup and abortStartup are called with mu and operationMu held.
func (r *Runner) failStartup(ctx context.Context, err error) (State, error) {
	if ctx.Err() != nil {
		r.state.Status, r.state.Error = "stopped", ""
		r.state.TunEnabled = false
		r.emit()
		return r.state, operationError(ctx.Err())
	}
	return r.fail(err)
}

func (r *Runner) abortStartup(ctx context.Context, cmd *exec.Cmd, done <-chan struct{}, cause error) (State, error) {
	stopPath, logs := r.tunStopPath, r.logs
	r.mu.Unlock()
	if stopPath != "" {
		_ = os.WriteFile(stopPath, nil, 0600)
	}
	_ = cmd.Process.Kill()
	<-done
	r.mu.Lock()
	r.cmd, r.tunStopPath = nil, ""
	detail := startupLogDetail(logs.String())
	if detail != "" {
		cause = fmt.Errorf("%w；%s", cause, detail)
	}
	return r.failStartup(ctx, fmt.Errorf("内核未就绪: %w", cause))
}

func startupLogDetail(output string) string {
	detail := strings.TrimSpace(output)
	lines := strings.Split(detail, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "level=error") || strings.Contains(lines[i], "level=fatal") {
			detail = strings.TrimSpace(lines[i])
			break
		}
	}
	if len(detail) > 500 {
		detail = detail[len(detail)-500:]
	}
	return detail
}

func (r *Runner) Stop() (State, error) {
	r.cancelConnection()
	r.CancelOperation(0)
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.stop()
}

// stop is called with operationMu held. It must not cancel its caller's context:
// connection changes use it before starting the replacement core.
func (r *Runner) stop() (State, error) {
	r.mu.Lock()
	if r.systemProxy != nil {
		r.mu.Unlock()
		err := r.systemProxy.Disable()
		r.mu.Lock()
		if err != nil {
			r.state.Error = fmt.Sprintf("系统代理恢复失败，内核仍在运行: %v", err)
			s := r.state
			r.mu.Unlock()
			return s, err
		}
		r.state.SystemProxyEnabled = false
		r.proxyGeneration++
	}
	if r.cmd == nil {
		r.state.Status, r.state.Error = "stopped", ""
		r.state.TunEnabled = false
		r.tunStopPath = ""
		r.emit()
		s := r.state
		r.mu.Unlock()
		return s, nil
	}
	cmd, done := r.cmd, r.done
	tunStopPath := r.tunStopPath
	r.state.Status = "stopping"
	r.emit()
	r.mu.Unlock()
	if tunStopPath != "" {
		if err := os.WriteFile(tunStopPath, nil, 0600); err != nil {
			return r.stopFailure(fmt.Errorf("停止 Tun 内核失败: %w", err))
		}
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		if tunStopPath != "" {
			return r.stopFailure(errors.New("Tun 内核未能及时停止"))
		}
		_ = cmd.Process.Kill()
		<-done
	}
	r.mu.Lock()
	if r.cmd == cmd {
		r.cmd = nil
	}
	r.state.Status, r.state.Error = "stopped", ""
	r.state.TunEnabled = false
	r.tunStopPath = ""
	r.emit()
	s := r.state
	r.mu.Unlock()
	return s, nil
}

func (r *Runner) stopFailure(err error) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Status = "running"
	r.state.Error = err.Error()
	r.emit()
	return r.state, err
}

func (r *Runner) waitReady(done <-chan struct{}) error {
	return r.waitReadyUntil(done, 12*time.Second)
}

func (r *Runner) waitReadyUntil(done <-chan struct{}, timeout time.Duration) error {
	return r.waitReadyContext(context.Background(), done, timeout)
}

// The caller holds mu and operationMu. Session identity stays fixed while mu is
// released for probes, and mu is held again on return.
func (r *Runner) waitReadyContext(ctx context.Context, done <-chan struct{}, timeout time.Duration) error {
	controller := controllerEndpoint{client: r.client, apiPort: r.apiPort, secret: r.secret}
	mixedPort := r.state.Port
	r.mu.Unlock()
	defer r.mu.Lock()
	return controller.waitReady(ctx, done, mixedPort, timeout)
}

func (controller controllerEndpoint) waitReady(parent context.Context, done <-chan struct{}, mixedPort int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// Process exit interrupts an in-flight HTTP request or TCP dial too.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-done:
			cancel()
		case <-watchDone:
		}
	}()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		if controller.requestContext(ctx, http.MethodGet, "/version", nil, nil) == nil {
			dialer := net.Dialer{Timeout: time.Second}
			conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", mixedPort))
			if err == nil {
				conn.Close()
				return nil
			}
		}
		select {
		case <-done:
			return errors.New("进程提前退出")
		case <-ctx.Done():
			if parent.Err() != nil {
				return operationError(parent.Err())
			}
			select {
			case <-done:
				return errors.New("进程提前退出")
			default:
			}
			return errors.New("等待控制接口与代理端口超时")
		case <-tick.C:
		}
	}
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func cleanEnv() []string {
	result := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "CLASH_") && key != "SAFE_PATHS" && key != "SKIP_SAFE_PATH_CHECK" {
			result = append(result, entry)
		}
	}
	return result
}
