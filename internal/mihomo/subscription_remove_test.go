package mihomo

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func removalSubscriptions(t *testing.T, dir string) (*profile.Store, *profile.Subscriptions, []profile.Subscription) {
	t.Helper()
	store := profile.NewStore(dir)
	subs := profile.NewSubscriptions(store, &testURLStore{})
	for _, address := range []string{"https://example.com/one", "https://example.com/two"} {
		if err := subs.ImportDownloaded(profile.DownloadedSubscription{URL: address, Data: []byte("proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\nrules:\n  - MATCH,DIRECT\n")}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := subs.Select(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	return store, subs, items
}

func TestRemoveActiveSubscriptionUpdatesStateWhileStopped(t *testing.T) {
	dir := t.TempDir()
	store, subs, items := removalSubscriptions(t, dir)
	var emitted State
	runner := NewRunner(store, subs, dir, "", 7890, nil, func(state State) { emitted = state })
	before := runner.Snapshot()
	if err := runner.RemoveSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	state := runner.Snapshot()
	if state.Status != "stopped" || state.HasProfile || state.ConfigVersion != before.ConfigVersion+1 || emitted.HasProfile || emitted.ConfigVersion != state.ConfigVersion {
		t.Fatalf("deleted config not reflected in state: %+v, event %+v", state, emitted)
	}
	restarted := NewRunner(profile.NewStore(dir), subs, dir, "", 7890, nil, nil)
	if restarted.Snapshot().HasProfile {
		t.Fatal("deleted config returned after restart")
	}
	if _, err := runner.Start(); err == nil {
		t.Fatal("core can still start after active config deletion")
	}
	if _, err := runner.SelectSubscription(items[1].ID); err != nil || !runner.Snapshot().HasProfile {
		t.Fatalf("remaining subscription cannot be selected: %v", err)
	}
}

func TestRemoveActiveSubscriptionAbortsWhenConnectionCannotStop(t *testing.T) {
	for _, mode := range []string{"system", "tun"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			store, subs, items := removalSubscriptions(t, dir)
			proxy := &testSystemProxy{}
			runner := NewRunner(store, subs, dir, "", 7890, proxy, nil)
			runner.state.Status = "running"
			if mode == "system" {
				proxy.enabled, proxy.failDisable = true, true
				runner.state.SystemProxyEnabled = true
			} else {
				runner.state.TunEnabled = true
				runner.cmd = &exec.Cmd{}
				runner.tunStopPath = filepath.Join(dir, "missing", "stop")
			}
			before := runner.Snapshot()
			if err := runner.RemoveSubscription(items[0].ID); err == nil {
				t.Fatal("deletion accepted after stop failure")
			}
			state := runner.Snapshot()
			if !state.HasProfile || state.Status != "running" || state.SystemProxyEnabled != before.SystemProxyEnabled || state.TunEnabled != before.TunEnabled || state.ConfigVersion != before.ConfigVersion {
				t.Fatalf("failed stop changed config or connection: %+v", state)
			}
			remaining, err := subs.List()
			if err != nil || len(remaining) != 2 || !remaining[0].Active {
				t.Fatalf("failed stop deleted subscription: %+v, %v", remaining, err)
			}
			if _, err := store.LoadSubscription(items[0].ID); err != nil {
				t.Fatalf("failed stop removed cache: %v", err)
			}
		})
	}
}

func TestRemoveActiveSubscriptionWaitsForTunShutdown(t *testing.T) {
	dir := t.TempDir()
	store, subs, items := removalSubscriptions(t, dir)
	runner := NewRunner(store, subs, dir, "", 7890, nil, nil)
	runner.state.Status, runner.state.TunEnabled = "running", true
	runner.cmd = &exec.Cmd{}
	runner.done = make(chan struct{})
	runner.tunStopPath = filepath.Join(dir, "tun-stop-test")
	stopPath := runner.tunStopPath
	shutdown := make(chan bool, 1)
	go func() {
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-deadline.C:
				shutdown <- false
				close(runner.done)
				return
			case <-tick.C:
				if _, err := os.Stat(stopPath); err == nil {
					shutdown <- store.Exists()
					close(runner.done)
					return
				}
			}
		}
	}()
	if err := runner.RemoveSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if !<-shutdown {
		t.Fatal("config was removed before Tun shutdown")
	}
	if state := runner.Snapshot(); state.HasProfile || state.TunEnabled || state.Status != "stopped" || runner.cmd != nil {
		t.Fatalf("Tun or its config remains active: %+v", state)
	}
}

func TestRemoveSubscriptionWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, subs, items := removalSubscriptions(t, dir)
	proxy := &testSystemProxy{}
	runner := NewRunner(store, subs, dir, binary, port, proxy, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	before := runner.Snapshot()
	if err := runner.RemoveSubscription("missing"); !errors.Is(err, profile.ErrNoSubscription) || runner.Snapshot() != before {
		t.Fatalf("unknown subscription stopped connection: %v", err)
	}
	if err := runner.RemoveSubscription(items[1].ID); err != nil || runner.Snapshot() != before || !proxy.enabled {
		t.Fatalf("inactive deletion changed current connection: %+v, %v", runner.Snapshot(), err)
	}
	if groups, err := runner.Groups(); err != nil || !hasGroup(groups, "Choose") {
		t.Fatalf("inactive deletion changed running nodes: %v, %v", groups, err)
	}
	proxy.failDisable = true
	if err := runner.RemoveSubscription(items[0].ID); err == nil || !store.Exists() || runner.Snapshot().Status != "running" || !proxy.enabled {
		t.Fatalf("failed proxy restoration deleted active config: %+v, %v", runner.Snapshot(), err)
	}
	proxy.failDisable = false
	if err := runner.RemoveSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	state := runner.Snapshot()
	if state.Status != "stopped" || state.SystemProxyEnabled || state.TunEnabled || state.HasProfile || proxy.enabled || state.ConfigVersion != before.ConfigVersion+1 {
		t.Fatalf("active deletion left connection or config: %+v", state)
	}
	for _, name := range []string{"profile.yaml", "runtime.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("active config retained %s: %v", name, err)
		}
	}
	if _, err := store.NodeNames(); err == nil {
		t.Fatal("deleted nodes can still be read")
	}
	if _, err := store.SelectorGroups(); err == nil {
		t.Fatal("deleted groups can still be read")
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err == nil {
		connection.Close()
		t.Fatal("deleted profile's proxy listener is still running")
	}
}
