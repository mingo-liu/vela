package mihomo

import (
	"errors"
	"os"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestRunnerWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import(`proxies: []
proxy-groups:
  - name: Choose
    type: select
    proxies: [DIRECT, REJECT]
rules:
  - GEOIP,CN,DIRECT
  - MATCH,Choose
`); err != nil {
		t.Fatal(err)
	}
	systemProxy := &testSystemProxy{}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, systemProxy, nil)
	t.Cleanup(runner.Close)
	state, err := runner.SetSystemProxy(true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("unexpected state: %+v", state)
	}
	groups, err := runner.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatal("no selector groups from controller")
	}
	if err := runner.Select("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	systemProxy.failDisable = true
	if state, err = runner.SetSystemProxy(false); err == nil || state.Status != "running" {
		t.Fatalf("core stopped before system proxy could be restored: %+v, %v", state, err)
	}
	systemProxy.failDisable = false
	state, err = runner.SetSystemProxy(false)
	if err != nil || state.Status != "stopped" {
		t.Fatalf("stop: %+v, %v", state, err)
	}
	if systemProxy.enabled {
		t.Fatal("system proxy was not restored")
	}
	systemProxy.failEnable = true
	state, err = runner.SetSystemProxy(true)
	if err == nil || state.Status != "stopped" || state.SystemProxyEnabled {
		t.Fatalf("core was not stopped after system proxy enable failed: %+v, %v", state, err)
	}
}

func TestGroupsAvailableBeforeCoreStarts(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import("proxies:\n  - name: Node A\n    type: socks5\n    server: example.com\n    port: 1080\nproxy-groups:\n  - name: Choose\n    type: select\n    proxies: [Node A, DIRECT]\n  - name: Auto\n    type: url-test\n    proxies: [DIRECT]\nrules: [MATCH,DIRECT]\n"); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, "", 7890, nil, nil)
	groups, err := runner.Groups()
	if err != nil || len(groups) != 1 || groups[0].Name != "Choose" || groups[0].Current != "Node A" || len(groups[0].Options) != 2 {
		t.Fatalf("offline groups: %+v, %v", groups, err)
	}
	nodes, err := runner.NodeNames()
	if err != nil || len(nodes) != 1 || nodes[0] != "Node A" {
		t.Fatalf("offline nodes: %+v, %v", nodes, err)
	}
	if err := runner.Select("Choose", "REJECT"); err == nil {
		t.Fatal("offline selection unexpectedly changed the connection")
	}
}

type testSystemProxy struct {
	enabled     bool
	failEnable  bool
	failDisable bool
}

func (p *testSystemProxy) Enable(int) error {
	if p.failEnable {
		return errors.New("enable failed")
	}
	p.enabled = true
	return nil
}
func (p *testSystemProxy) Disable() error {
	if p.failDisable {
		return errors.New("restore failed")
	}
	p.enabled = false
	return nil
}
func (p *testSystemProxy) Recover() error        { return p.Disable() }
func (p *testSystemProxy) Active() (bool, error) { return p.enabled, nil }

type testURLStore struct{ value string }

func (s *testURLStore) Get() (string, error) {
	if s.value == "" {
		return "", profile.ErrNoSubscription
	}
	return s.value, nil
}
func (s *testURLStore) Put(value string) error { s.value = value; return nil }
func (s *testURLStore) Delete() error          { s.value = ""; return nil }
