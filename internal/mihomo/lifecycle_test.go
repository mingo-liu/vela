package mihomo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

type lifecycleTransport func(*http.Request) (*http.Response, error)

func (f lifecycleTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func lifecycleResponse(body string) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

// A real child process exercises cleanup without changing macOS proxy settings
// or installing a privileged helper. The child owns the fake mixed listener,
// so the port is free before startup and released on cancellation or exit.
func newLifecycleRunner(t *testing.T) *Runner {
	t.Helper()
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import("proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\nrules: [MATCH,DIRECT]\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "fake-core")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"-t\" ]; then exit 0; fi\nprintf 'startup waiting\\n'\nexec /usr/bin/nc -l -k 127.0.0.1 %d\n", port)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, &testSystemProxy{}, nil)
	runner.client.Transport = lifecycleTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/proxies" {
			return lifecycleResponse(`{"proxies":{"Choose":{"type":"Selector","now":"DIRECT","all":["DIRECT","REJECT"]}}}`)
		}
		return lifecycleResponse(`{}`)
	})
	runner.SetTunLauncher(func(_ context.Context, _, _ string) (*exec.Cmd, error) {
		return exec.Command(binary), nil
	})
	t.Cleanup(runner.Close)
	return runner
}

func assertLifecycleResponsive(t *testing.T, runner *Runner, status string) {
	t.Helper()
	result := make(chan State, 1)
	go func() {
		state := runner.Snapshot()
		runner.Logs()
		result <- state
	}()
	select {
	case state := <-result:
		if state.Status != status {
			t.Fatalf("expected %s state, got %+v", status, state)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("startup blocked state or log reads")
	}
}

func TestConnectionStartupRemainsResponsiveAndCancels(t *testing.T) {
	for _, stage := range []string{"system-readiness", "tun-readiness", "tun-authorization", "restore-groups", "restore-selection"} {
		for _, stop := range []string{"disconnect", "stop", "close"} {
			t.Run(stage+"/"+stop, func(t *testing.T) {
				runner := newLifecycleRunner(t)
				entered := make(chan struct{})
				var once sync.Once
				block := func(ctx context.Context) error {
					once.Do(func() { close(entered) })
					<-ctx.Done()
					return ctx.Err()
				}
				transport := runner.client.Transport
				runner.client.Transport = lifecycleTransport(func(req *http.Request) (*http.Response, error) {
					if (strings.HasSuffix(stage, "readiness") && req.URL.Path == "/version") ||
						(stage == "restore-groups" && req.URL.Path == "/proxies") ||
						(stage == "restore-selection" && req.Method == http.MethodPut) {
						return nil, block(req.Context())
					}
					return transport.RoundTrip(req)
				})
				if stage == "tun-authorization" {
					runner.SetTunLauncher(func(ctx context.Context, _, _ string) (*exec.Cmd, error) {
						return nil, block(ctx)
					})
				}
				started := make(chan error, 1)
				go func() {
					var err error
					if strings.HasPrefix(stage, "tun-") {
						_, err = runner.SetTun(true)
					} else {
						_, err = runner.SetSystemProxy(true)
					}
					started <- err
				}()
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("startup did not reach blocked stage")
				}
				assertLifecycleResponsive(t, runner, "starting")
				stopped := make(chan error, 1)
				go func() {
					var err error
					switch stop {
					case "disconnect":
						_, err = runner.SetSystemProxy(false)
					case "stop":
						_, err = runner.Stop()
					case "close":
						runner.Close()
					}
					stopped <- err
				}()
				select {
				case err := <-stopped:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("stop waited for startup timeout")
				}
				select {
				case err := <-started:
					if !errors.Is(err, ErrOperationCancelled) {
						t.Fatalf("startup returned %v, want cancellation", err)
					}
				case <-time.After(time.Second):
					t.Fatal("startup did not finish")
				}
				state := runner.Snapshot()
				if state.Status != "stopped" || state.Error != "" || state.SystemProxyEnabled || state.TunEnabled {
					t.Fatalf("cancelled startup left connection or error: %+v", state)
				}
				runner.mu.Lock()
				cmd, done := runner.cmd, runner.done
				runner.mu.Unlock()
				if cmd != nil {
					t.Fatal("startup left its child process attached")
				}
				if done != nil {
					select {
					case <-done:
					default:
						t.Fatal("startup returned before child process was reaped")
					}
				}
				if stop == "close" {
					if _, err := runner.Start(); !errors.Is(err, ErrRunnerClosed) {
						t.Fatalf("startup allowed after close: %v", err)
					}
				} else {
					runner.client.Transport = transport
					// A successful replacement also proves the old process observer
					// cannot overwrite the new session after cancellation.
					if _, err := runner.Start(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestTunValidationCanBeCancelled(t *testing.T) {
	runner := newLifecycleRunner(t)
	marker := filepath.Join(runner.dataDir, "validating")
	// The checker blocks before a TUN session is launched. Cancellation must
	// terminate this process too, rather than waiting for its 15 second timeout.
	if err := os.WriteFile(runner.binary, []byte("#!/bin/sh\ntouch \""+marker+"\"\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := runner.SetTun(true); result <- err }()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("checker did not start")
		case <-tick.C:
		}
	}
	assertLifecycleResponsive(t, runner, "starting")
	closed := make(chan struct{})
	go func() { runner.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for validation timeout")
	}
	if err := <-result; !errors.Is(err, ErrOperationCancelled) {
		t.Fatal(err)
	}
}

func TestHotReloadRemainsResponsiveAndRejectsIntermediateReads(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[reject], func(t *testing.T) {
			runner := newLifecycleRunner(t)
			if _, err := runner.Start(); err != nil {
				t.Fatal(err)
			}
			before := runner.Snapshot()
			entered, release := make(chan struct{}), make(chan struct{})
			readStarted, releaseRead := make(chan struct{}), make(chan struct{})
			var releaseOnce, readOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			unblockRead := func() { readOnce.Do(func() { close(releaseRead) }) }
			t.Cleanup(unblock)
			t.Cleanup(unblockRead)
			var writes, reads atomic.Int32
			transport := runner.client.Transport
			runner.client.Transport = lifecycleTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/connections" {
					if reads.Add(1) == 1 {
						close(readStarted)
						select {
						case <-releaseRead:
						case <-req.Context().Done():
							return nil, req.Context().Err()
						}
					}
					return lifecycleResponse(`{"connections":[{"id":"intermediate"}]}`)
				}
				if req.URL.Path == "/configs" && writes.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
					if reject {
						response, _ := lifecycleResponse("invalid config")
						response.StatusCode = http.StatusBadRequest
						return response, nil
					}
				}
				return transport.RoundTrip(req)
			})
			oldRead := make(chan error, 1)
			go func() { _, err := runner.Connections(); oldRead <- err }()
			select {
			case <-readStarted:
			case <-time.After(time.Second):
				t.Fatal("old read did not start")
			}
			finished := make(chan error, 1)
			go func() {
				_, err := runner.SaveCustomRules([]profile.CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "REJECT", Enabled: true}})
				finished <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("reload did not start")
			}
			assertLifecycleResponsive(t, runner, "running")
			if _, err := runner.Connections(); !errors.Is(err, errControllerChanged) {
				t.Fatalf("read during reload exposed intermediate data: %v", err)
			}
			unblock()
			select {
			case err := <-finished:
				if (err != nil) != reject {
					t.Fatalf("reload error = %v, reject = %v", err, reject)
				}
			case <-time.After(time.Second):
				t.Fatal("reload did not finish")
			}
			if reject && runner.Snapshot() != before {
				t.Fatal("failed reload changed committed state")
			}
			unblockRead()
			if err := <-oldRead; !errors.Is(err, errControllerChanged) {
				t.Fatalf("stale read survived reload or rollback: %v", err)
			}
			if _, err := runner.Connections(); err != nil {
				t.Fatalf("fresh read failed after reload: %v", err)
			}
		})
	}
}

func TestCancelledModeSwitchDoesNotRestoreOldConnection(t *testing.T) {
	runner := newLifecycleRunner(t)
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	runner.SetTunLauncher(func(ctx context.Context, _, _ string) (*exec.Cmd, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() { _, err := runner.SetTun(true); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("mode switch did not reach authorization")
	}
	if _, err := runner.SetTun(false); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrOperationCancelled) {
		t.Fatal(err)
	}
	if state := runner.Snapshot(); state.Status != "stopped" || state.SystemProxyEnabled || state.TunEnabled {
		t.Fatalf("cancelled switch restored old connection: %+v", state)
	}
}

func TestShutdownCancelsQueuedStartup(t *testing.T) {
	runner := newLifecycleRunner(t)
	runner.operationMu.Lock()
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(runner.operationMu.Unlock) }
	t.Cleanup(unlock)
	started := make(chan error, 1)
	go func() { _, err := runner.Start(); started <- err }()
	wait := func(condition func() bool) {
		t.Helper()
		deadline := time.After(time.Second)
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for !condition() {
			select {
			case <-tick.C:
			case <-deadline:
				t.Fatal("queued lifecycle request did not register")
			}
		}
	}
	wait(func() bool {
		runner.taskMu.Lock()
		defer runner.taskMu.Unlock()
		return runner.connectionCancel != nil
	})
	closed := make(chan struct{})
	go func() { runner.Close(); close(closed) }()
	wait(func() bool {
		runner.taskMu.Lock()
		defer runner.taskMu.Unlock()
		return runner.closed
	})
	unlock()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	if err := <-started; !errors.Is(err, ErrOperationCancelled) {
		t.Fatalf("queued startup returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(runner.dataDir, "runtime.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("queued startup wrote a runtime config after shutdown")
	}
}

func TestReadinessWaitInterruptsHTTPOnDeadlineAndExit(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "process-exit"}[exit], func(t *testing.T) {
			entered := make(chan struct{})
			done := make(chan struct{})
			client := &http.Client{Transport: lifecycleTransport(func(req *http.Request) (*http.Response, error) {
				close(entered)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})}
			controller := controllerEndpoint{client: client, apiPort: 9090}
			result := make(chan error, 1)
			timeout := 50 * time.Millisecond
			if exit {
				timeout = time.Minute
			}
			go func() { result <- controller.waitReady(context.Background(), done, 7890, timeout) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("readiness request did not start")
			}
			if exit {
				close(done)
			}
			select {
			case err := <-result:
				want := "等待控制接口与代理端口超时"
				if exit {
					want = "进程提前退出"
				}
				if err == nil || err.Error() != want {
					t.Fatalf("readiness error = %v, want %s", err, want)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("deadline or exit did not interrupt HTTP request")
			}
		})
	}
}
