package mihomo

import (
	"errors"
	"strings"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

type refreshingSystemProxy struct {
	testSystemProxy
	active bool
	err    error
	calls  int
}

func (p *refreshingSystemProxy) Refresh() (bool, error) {
	p.calls++
	return p.active, p.err
}

func TestProxyMonitorRetriesTransientErrorsAndStopsAfterFailedHandoff(t *testing.T) {
	proxy := &refreshingSystemProxy{active: true, err: errors.New("route unavailable")}
	store := profile.NewStore(t.TempDir())
	runner := NewRunner(store, nil, "", "", 7890, proxy, nil)
	proxy.enabled = true
	runner.state.Status, runner.state.SystemProxyEnabled = "running", true
	runner.proxyGeneration = 4
	if runner.refreshSystemProxy(3) || proxy.calls != 0 {
		t.Fatal("stale monitor refreshed a replacement session")
	}
	if !runner.refreshSystemProxy(4) || !runner.Snapshot().SystemProxyEnabled || !strings.Contains(runner.Snapshot().Error, "route unavailable") {
		t.Fatal("transient route failure did not retain session and report error")
	}
	proxy.err = nil
	if !runner.refreshSystemProxy(4) || runner.Snapshot().Error != "" {
		t.Fatal("successful refresh did not clear transient error")
	}
	proxy.active, proxy.err = false, errors.New("handoff failed")
	if runner.refreshSystemProxy(4) {
		t.Fatal("failed handoff kept monitoring")
	}
	state := runner.Snapshot()
	if state.SystemProxyEnabled || state.Status != "stopped" || state.Error != "handoff failed" || proxy.enabled {
		t.Fatalf("failed handoff did not disconnect safely: %+v", state)
	}
}
