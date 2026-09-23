package mihomo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	if _, err := runner.TestGroupDelay("Choose"); err != nil {
		t.Fatalf("offline delay test: %v", err)
	}
	if state := runner.Snapshot(); state.Status != "stopped" || state.SystemProxyEnabled || systemProxy.enabled {
		t.Fatalf("offline delay test changed connection state: %+v", state)
	}
	if err := runner.Select("Choose", "DIRECT"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Select("GLOBAL", "Choose"); err != nil {
		t.Fatal(err)
	}
	state, err = runner.SetSystemProxy(true)
	if err != nil || state.Status != "running" {
		t.Fatalf("restart with offline choice: %+v, %v", state, err)
	}
	groups, err = runner.Groups()
	chosen := ""
	global := ""
	for _, group := range groups {
		if group.Name == "Choose" {
			chosen = group.Current
		}
		if group.Name == "GLOBAL" {
			global = group.Current
		}
	}
	if err != nil || chosen != "DIRECT" || global != "Choose" {
		t.Fatalf("offline choice not applied after start: %+v, %v", groups, err)
	}
	if _, err := runner.SetSystemProxy(false); err != nil {
		t.Fatal(err)
	}
	systemProxy.failEnable = true
	state, err = runner.SetSystemProxy(true)
	if err == nil || state.Status != "stopped" || state.SystemProxyEnabled {
		t.Fatalf("core was not stopped after system proxy enable failed: %+v, %v", state, err)
	}
}

func TestTunStartFailureRestoresSystemProxy(t *testing.T) {
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
	if err := store.Import("proxies: []\nrules:\n  - MATCH,DIRECT\n"); err != nil {
		t.Fatal(err)
	}
	proxy := &testSystemProxy{}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, proxy, nil)
	runner.SetTunLauncher(func(_, _ string) (*exec.Cmd, error) { return exec.Command("false"), nil })
	t.Cleanup(runner.Close)
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	state, err := runner.SetTun(true)
	if err == nil {
		t.Fatal("expected TUN launch failure")
	}
	if state.Status != "running" || !state.SystemProxyEnabled || state.TunEnabled || !proxy.enabled {
		t.Fatalf("original system proxy connection was not restored: %+v", state)
	}
}

func TestSelectSubscriptionWhileSystemProxyEnabled(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "One"
		if r.URL.Path == "/two" {
			name = "Two"
		}
		_, _ = w.Write([]byte("proxy-groups:\n  - name: " + name + "\n    type: select\n    proxies: [DIRECT, REJECT]\nrules:\n  - MATCH,DIRECT\n"))
	}))
	defer server.Close()
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	subs := profile.NewSubscriptions(store, &testURLStore{})
	systemProxy := &testSystemProxy{}
	runner := NewRunner(store, subs, dir, binary, port, systemProxy, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.ImportSubscription(server.URL + "/one"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.ImportSubscription(server.URL + "/two"); err != nil {
		t.Fatal(err)
	}
	list, err := subs.List()
	if err != nil || len(list) != 2 {
		t.Fatalf("subscriptions: %+v, %v", list, err)
	}
	server.Close() // Selection must use the cached profile.
	if state, err := runner.SelectSubscription(list[0].ID); err != nil || state.Status != "stopped" || state.SystemProxyEnabled {
		t.Fatalf("switch while disconnected: %+v, %v", state, err)
	}
	if state, err := runner.SelectSubscription(list[1].ID); err != nil || state.Status != "stopped" || state.SystemProxyEnabled {
		t.Fatalf("switch back while disconnected: %+v, %v", state, err)
	}
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	state, err := runner.SelectSubscription(list[0].ID)
	if err != nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("switch while connected: %+v, %v", state, err)
	}
	groups, err := runner.Groups()
	if err != nil || !hasGroup(groups, "One") || hasGroup(groups, "Two") {
		t.Fatalf("running config was not reloaded: %+v, %v", groups, err)
	}
	list, err = subs.List()
	if err != nil || !list[0].Active || list[1].Active {
		t.Fatalf("saved selection: %+v, %v", list, err)
	}
	// A cached YAML can pass Vela's structural checks but fail mihomo's parser.
	bad := "proxy-groups:\n  - name: Bad\n    type: select\n    proxies: [Missing]\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(filepath.Join(dir, "subscriptions", list[1].ID+".yaml"), []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if state, err = runner.SelectSubscription(list[1].ID); err == nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("failed switch changed connection: %+v, %v", state, err)
	}
	list, err = subs.List()
	if err != nil || !list[0].Active || list[1].Active {
		t.Fatalf("failed switch changed saved selection: %+v, %v", list, err)
	}
	groups, err = runner.Groups()
	if err != nil || !hasGroup(groups, "One") || hasGroup(groups, "Bad") {
		t.Fatalf("failed switch changed running config: %+v, %v", groups, err)
	}
}

func TestImportSubscriptionWhileSystemProxyEnabled(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "Two"
		switch r.URL.Path {
		case "/three":
			name = "Three"
		case "/invalid":
			_, _ = w.Write([]byte("listeners: [{name: unsafe, type: mixed, port: 9999}]\n"))
			return
		}
		_, _ = w.Write([]byte("proxy-groups:\n  - name: " + name + "\n    type: select\n    proxies: [DIRECT, REJECT]\nrules:\n  - MATCH,DIRECT\n"))
	}))
	defer server.Close()
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	subs := profile.NewSubscriptions(store, &testURLStore{})
	systemProxy := &testSystemProxy{}
	runner := NewRunner(store, subs, dir, binary, port, systemProxy, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.Import("proxy-groups:\n  - name: One\n    type: select\n    proxies: [DIRECT, REJECT]\nrules:\n  - MATCH,DIRECT\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	state, err := runner.ImportSubscription(server.URL + "/two")
	if err != nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("import while connected: %+v, %v", state, err)
	}
	groups, err := runner.Groups()
	if err != nil || !hasGroup(groups, "One") || hasGroup(groups, "Two") {
		t.Fatalf("import changed running config: %+v, %v", groups, err)
	}
	list, err := subs.List()
	if err != nil || len(list) != 1 || list[0].Active {
		t.Fatalf("import activated new subscription: %+v, %v", list, err)
	}
	if state, err = runner.ImportSubscription(server.URL + "/invalid"); err == nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("invalid import changed connection: %+v, %v", state, err)
	}
	list, err = subs.List()
	if err != nil || len(list) != 1 || list[0].Active {
		t.Fatalf("invalid import changed subscriptions: %+v, %v", list, err)
	}
	if state, err = runner.SelectSubscription(list[0].ID); err != nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("manual selection failed: %+v, %v", state, err)
	}
	groups, err = runner.Groups()
	if err != nil || !hasGroup(groups, "Two") || hasGroup(groups, "One") {
		t.Fatalf("manual selection did not apply config: %+v, %v", groups, err)
	}
	if state, err = runner.ImportSubscription(server.URL + "/three"); err != nil || state.Status != "running" || !state.SystemProxyEnabled || !systemProxy.enabled {
		t.Fatalf("second import while connected: %+v, %v", state, err)
	}
	list, err = subs.List()
	if err != nil || len(list) != 2 || !list[0].Active || list[1].Active {
		t.Fatalf("second import changed selection: %+v, %v", list, err)
	}
	groups, err = runner.Groups()
	if err != nil || !hasGroup(groups, "Two") || hasGroup(groups, "Three") {
		t.Fatalf("second import changed running config: %+v, %v", groups, err)
	}
	files, err := os.ReadDir(filepath.Join(dir, "subscriptions"))
	if err != nil || len(files) != 2 {
		t.Fatalf("imported profiles were not cached: %d, %v", len(files), err)
	}
}

func hasGroup(groups []Group, name string) bool {
	for _, group := range groups {
		if group.Name == name {
			return true
		}
	}
	return false
}

func TestGroupsAvailableBeforeCoreStarts(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import("proxies:\n  - name: Node A\n    type: socks5\n    server: example.com\n    port: 1080\nproxy-groups:\n  - name: Choose\n    type: select\n    proxies: [Node A, DIRECT]\n  - name: Auto\n    type: url-test\n    proxies: [DIRECT]\nrules: [MATCH,DIRECT]\n"); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, "", 7890, nil, nil)
	groups, err := runner.Groups()
	if err != nil || len(groups) != 2 || groups[0].Name != "Choose" || groups[0].Current != "Node A" || len(groups[0].Options) != 2 || groups[1].Name != "GLOBAL" {
		t.Fatalf("offline groups: %+v, %v", groups, err)
	}
	nodes, err := runner.NodeNames()
	if err != nil || len(nodes) != 1 || nodes[0] != "Node A" {
		t.Fatalf("offline nodes: %+v, %v", nodes, err)
	}
	if err := runner.Select("Choose", "DIRECT"); err != nil {
		t.Fatal(err)
	}
	groups, err = runner.Groups()
	if err != nil || len(groups) != 2 || groups[0].Current != "DIRECT" {
		t.Fatalf("offline selection: %+v, %v", groups, err)
	}
}

func TestGroupDelayTestsEachOption(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing controller authorization")
		}
		if r.URL.Path == "/proxies" {
			_, _ = w.Write([]byte(`{"proxies":{"Choose":{"type":"Selector","all":["Node/A","Unavailable"]}}}`))
			return
		}
		if r.URL.Query().Get("url") != delayTestURL || r.URL.Query().Get("timeout") != "5000" {
			t.Errorf("incorrect delay test query: %s", r.URL.RawQuery)
		}
		if r.URL.Path == "/proxies/Node/A/delay" {
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 123})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(strings.TrimPrefix(parsed.Host, "127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, nil, nil)
	runner.state.Status = "running"
	runner.apiPort = port
	runner.secret = "test-secret"
	delays, err := runner.TestGroupDelay("Choose")
	if err != nil || len(delays) != 1 || delays["Node/A"] != 123 {
		t.Fatalf("group delays: %+v, %v", delays, err)
	}
	if _, err := runner.TestGroupDelay("Missing"); err == nil {
		t.Fatal("unknown group accepted")
	}
	runner.state.Status = "stopped"
	if _, err := runner.TestGroupDelay("Choose"); err == nil {
		t.Fatal("delay test accepted with stopped core")
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
