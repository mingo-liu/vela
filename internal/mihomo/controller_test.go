package mihomo

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestControllerReadsRemainResponsiveAndDiscardStaleData(t *testing.T) {
	readers := []struct {
		name, path, body string
		read             func(*Runner) (any, error)
		want, zero       any
	}{
		{
			name: "connections", path: "/connections",
			body: `{"uploadTotal":12,"downloadTotal":34,"connections":[{"id":"old"}]}`,
			read: func(r *Runner) (any, error) { return r.Connections() },
			want: ConnectionSnapshot{UploadTotal: 12, DownloadTotal: 34, Total: 1, Connections: []Connection{{ID: "old"}}},
			zero: ConnectionSnapshot{},
		},
		{
			name: "traffic", path: "/connections", body: `{"uploadTotal":12,"downloadTotal":34}`,
			read: func(r *Runner) (any, error) { return r.TrafficTotals() },
			want: TrafficTotals{Upload: 12, Download: 34}, zero: TrafficTotals{},
		},
		{
			name: "rules", path: "/rules", body: `{"rules":[{"type":"MATCH","proxy":"DIRECT"}]}`,
			read: func(r *Runner) (any, error) { return r.Rules() },
			want: []Rule{{Type: "MATCH", Proxy: "DIRECT"}}, zero: []Rule(nil),
		},
		{
			name: "groups", path: "/proxies",
			body: `{"proxies":{"Choose":{"type":"Selector","now":"DIRECT","all":["DIRECT","REJECT"]}}}`,
			read: func(r *Runner) (any, error) { return r.Groups() },
			want: []Group{{Name: "Choose", Current: "DIRECT", Options: []string{"DIRECT", "REJECT"}}}, zero: []Group(nil),
		},
	}
	changes := []struct {
		name  string
		apply func(*Runner) error
	}{
		{name: "unchanged", apply: func(*Runner) error { return nil }},
		{name: "disconnect", apply: func(r *Runner) error { _, err := r.SetSystemProxy(false); return err }},
		{name: "restart", apply: func(r *Runner) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			// Reuse the port and secret to verify that core identity alone is enough.
			r.done = make(chan struct{})
			return nil
		}},
		{name: "reload", apply: func(r *Runner) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.state.ConfigVersion++
			return nil
		}},
		{name: "exit", apply: func(r *Runner) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			// Process completion can precede the watcher updating the state.
			close(r.done)
			return nil
		}},
	}
	for _, reader := range readers {
		for _, change := range changes {
			t.Run(reader.name+"/"+change.name, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Method != http.MethodGet || req.URL.Path != reader.path || req.Header.Get("Authorization") != "Bearer test-secret" {
						t.Errorf("unexpected controller request: %s %s", req.Method, req.URL)
					}
					// Send headers immediately, but hold the response body to cover JSON decoding too.
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					close(started)
					<-release
					_, _ = w.Write([]byte(reader.body))
				}))
				t.Cleanup(server.Close)
				t.Cleanup(unblock)
				address, err := url.Parse(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				port, err := strconv.Atoi(address.Port())
				if err != nil {
					t.Fatal(err)
				}
				proxy := &testSystemProxy{}
				runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, proxy, nil)
				proxy.enabled = true
				t.Cleanup(runner.client.CloseIdleConnections)
				runner.apiPort, runner.secret, runner.done = port, "test-secret", make(chan struct{})
				runner.state.Status, runner.state.SystemProxyEnabled = "running", true
				type result struct {
					value any
					err   error
				}
				finished := make(chan result, 1)
				go func() {
					value, err := reader.read(runner)
					finished <- result{value, err}
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("controller request did not start")
				}
				snapshot := make(chan State, 1)
				go func() { snapshot <- runner.Snapshot() }()
				select {
				case state := <-snapshot:
					if state.Status != "running" {
						t.Fatalf("unexpected state: %+v", state)
					}
				case <-time.After(500 * time.Millisecond):
					t.Fatal("slow controller read blocked state access")
				}
				changed := make(chan error, 1)
				go func() { changed <- change.apply(runner) }()
				select {
				case err := <-changed:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(500 * time.Millisecond):
					t.Fatal("slow controller read blocked connection or configuration changes")
				}
				unblock()
				select {
				case got := <-finished:
					if change.name == "unchanged" {
						if got.err != nil || !reflect.DeepEqual(got.value, reader.want) {
							t.Fatalf("read = %+v, %v; want %+v", got.value, got.err, reader.want)
						}
					} else if !errors.Is(got.err, errControllerChanged) || !reflect.DeepEqual(got.value, reader.zero) {
						t.Fatalf("stale response returned data: %+v, %v", got.value, got.err)
					}
				case <-time.After(time.Second):
					t.Fatal("controller read did not finish")
				}
				if change.name == "disconnect" && (proxy.enabled || runner.Snapshot().SystemProxyEnabled) {
					t.Fatal("system proxy remained enabled")
				}
			})
		}
	}
}
