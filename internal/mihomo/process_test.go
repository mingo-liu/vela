package mihomo

import (
	"encoding/json"
	"errors"
	"net"
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

func TestParseCoreInfo(t *testing.T) {
	info, err := parseCoreInfo("Mihomo Meta v1.19.31 darwin arm64 with go1.26.8\nUse tags: with_gvisor\n")
	if err != nil || info.Name != "Mihomo Meta" || info.Version != "v1.19.31" {
		t.Fatalf("core info = %+v, %v", info, err)
	}
	if _, err := parseCoreInfo("unknown output"); err == nil {
		t.Fatal("accepted version output without a version")
	}
}

func TestSetMixedPortOfflineAndOccupied(t *testing.T) {
	store := profile.NewStore(t.TempDir())
	runner := NewRunner(store, nil, "", "", profile.DefaultMixedPort, nil, nil)
	for _, port := range []int{0, 1023, 65536} {
		if _, err := runner.SetMixedPort(port); err == nil {
			t.Fatalf("invalid port %d accepted", port)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	occupied := listener.Addr().(*net.TCPAddr).Port
	if _, err := runner.SetMixedPort(occupied); err == nil {
		t.Fatal("occupied port accepted")
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	state, err := runner.SetMixedPort(port)
	if err != nil || state.Port != port {
		t.Fatalf("offline port update = %+v, %v", state, err)
	}
	settings, err := store.Settings()
	if err != nil || settings.MixedPort != port {
		t.Fatalf("saved port = %+v, %v", settings, err)
	}
}

func TestSetMixedPortReconnectsAndRollsBackWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	initialPort, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if _, err := store.UpdateSettings(func(settings *profile.Settings) error {
		settings.MixedPort = initialPort
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Import("proxies: []\nrules:\n  - MATCH,DIRECT\n"); err != nil {
		t.Fatal(err)
	}
	proxy := &testSystemProxy{}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, initialPort, proxy, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	newPort, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	proxy.failDisable = true
	if state, err := runner.SetMixedPort(newPort); err == nil || state.Port != initialPort || !state.SystemProxyEnabled {
		t.Fatalf("failed to stop old connection safely = %+v, %v", state, err)
	}
	proxy.failDisable = false
	state, err := runner.SetMixedPort(newPort)
	if err != nil || state.Port != newPort || !state.SystemProxyEnabled || proxy.port != newPort {
		t.Fatalf("reconnected with new port = %+v, proxy port %d, %v", state, proxy.port, err)
	}
	failingPort, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	proxy.failPort = failingPort
	state, err = runner.SetMixedPort(failingPort)
	if err == nil || state.Port != newPort || !state.SystemProxyEnabled || proxy.port != newPort {
		t.Fatalf("failed change did not restore connection = %+v, proxy port %d, %v", state, proxy.port, err)
	}
	settings, err := store.Settings()
	if err != nil || settings.MixedPort != newPort {
		t.Fatalf("failed change did not restore saved port = %+v, %v", settings, err)
	}
}

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

func TestRoutingModeOfflineAndControllerUpdate(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	runner := NewRunner(store, nil, dir, "", 7890, nil, nil)
	if state, err := runner.SetRoutingMode(profile.RoutingGlobal); err != nil || state.RoutingMode != profile.RoutingGlobal {
		t.Fatalf("offline selection: %+v, %v", state, err)
	}
	if got := NewRunner(store, nil, dir, "", 7890, nil, nil).Snapshot().RoutingMode; got != profile.RoutingGlobal {
		t.Fatalf("mode not restored: %q", got)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/configs" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("unexpected controller request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Mode == profile.RoutingDirect {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if body.Mode != profile.RoutingRule {
			t.Errorf("unexpected mode: %q", body.Mode)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	runner.apiPort = port
	runner.secret = "test-secret"
	runner.state.Status = "running"
	runner.cmd = &exec.Cmd{}
	if state, err := runner.SetRoutingMode(profile.RoutingRule); err != nil || state.RoutingMode != profile.RoutingRule {
		t.Fatalf("online selection: %+v, %v", state, err)
	}
	if state, err := runner.SetRoutingMode(profile.RoutingDirect); err == nil || state.RoutingMode != profile.RoutingRule {
		t.Fatalf("failed update changed state: %+v, %v", state, err)
	}
	if got, err := store.RoutingMode(); err != nil || got != profile.RoutingRule {
		t.Fatalf("failed update changed saved mode: %q, %v", got, err)
	}
	if _, err := runner.SetRoutingMode("invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestRoutingModeWithRealCore(t *testing.T) {
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
	if err := store.Import("proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\nrules:\n  - MATCH,Choose\n"); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, nil, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.SetRoutingMode(profile.RoutingGlobal); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{profile.RoutingGlobal, profile.RoutingRule, profile.RoutingDirect} {
		if _, err := runner.SetRoutingMode(mode); err != nil {
			t.Fatal(err)
		}
		var config struct {
			Mode string `json:"mode"`
		}
		if err := runner.request(http.MethodGet, "/configs", nil, &config); err != nil || config.Mode != mode {
			t.Fatalf("core mode = %q, %v; want %q", config.Mode, err, mode)
		}
	}
	if _, err := runner.Stop(); err != nil {
		t.Fatal(err)
	}
	restarted := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, nil, nil)
	t.Cleanup(restarted.Close)
	if state := restarted.Snapshot(); state.RoutingMode != profile.RoutingDirect {
		t.Fatalf("restart lost saved mode: %+v", state)
	}
	if _, err := restarted.Start(); err != nil {
		t.Fatal(err)
	}
	var config struct {
		Mode string `json:"mode"`
	}
	if err := restarted.request(http.MethodGet, "/configs", nil, &config); err != nil || config.Mode != profile.RoutingDirect {
		t.Fatalf("restarted core mode = %q, %v", config.Mode, err)
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
	if _, err := runner.SetRoutingMode(profile.RoutingGlobal); err != nil {
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
	var runningConfig struct {
		Mode string `json:"mode"`
	}
	if err := runner.request(http.MethodGet, "/configs", nil, &runningConfig); err != nil || runningConfig.Mode != profile.RoutingGlobal {
		t.Fatalf("subscription switch lost routing mode: %q, %v", runningConfig.Mode, err)
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

func TestUpdateSubscriptionWhileSystemProxyEnabled(t *testing.T) {
	config := func(name, option string) string {
		return "proxy-groups:\n  - name: " + name + "\n    type: select\n    proxies: [" + option + "]\nrules: [MATCH,DIRECT]\n"
	}
	one := config("One", "DIRECT")
	two := config("Two", "REJECT")
	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/one" {
			_, _ = w.Write([]byte(one))
		} else {
			_, _ = w.Write([]byte(two))
		}
	}))
	defer subscriptionServer.Close()

	dir := t.TempDir()
	store := profile.NewStore(dir)
	subs := profile.NewSubscriptions(store, &testURLStore{})
	runner := NewRunner(store, subs, dir, "", 0, &testSystemProxy{enabled: true}, nil)
	for _, path := range []string{"/one", "/two"} {
		if _, err := runner.ImportSubscription(subscriptionServer.URL + path); err != nil {
			t.Fatal(err)
		}
	}
	list, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.SelectSubscription(list[0].ID); err != nil {
		t.Fatal(err)
	}
	mixedListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mixedListener.Close()
	var loadedConfig string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/configs" && r.Method == http.MethodPut {
			var request struct {
				Payload string `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.Contains(request.Payload, "name: Bad") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			loadedConfig = request.Payload
		}
	}))
	defer api.Close()
	apiURL, err := url.Parse(api.URL)
	if err != nil {
		t.Fatal(err)
	}
	runner.apiPort, err = strconv.Atoi(apiURL.Port())
	if err != nil {
		t.Fatal(err)
	}
	runner.state.Port = mixedListener.Addr().(*net.TCPAddr).Port
	runner.state.Status = "running"
	runner.state.SystemProxyEnabled = true
	runner.cmd = &exec.Cmd{}
	runner.done = make(chan struct{})
	runner.secret = "test-secret"

	two = config("Updated", "DIRECT")
	state, err := runner.UpdateSubscription(list[1].ID)
	if err != nil || state.Status != "running" || !state.SystemProxyEnabled || !strings.Contains(loadedConfig, "name: Updated") {
		t.Fatalf("online update did not reload the profile: %+v, %v, %q", state, err, loadedConfig)
	}
	list, err = subs.List()
	if err != nil || list[0].Active || !list[1].Active {
		t.Fatalf("online update did not select the subscription: %+v, %v", list, err)
	}
	two = config("Refreshed", "REJECT")
	state, err = runner.UpdateSubscription(list[1].ID)
	if err != nil || state.Status != "running" || !state.SystemProxyEnabled || !strings.Contains(loadedConfig, "name: Refreshed") {
		t.Fatalf("active subscription update did not reload the profile: %+v, %v, %q", state, err, loadedConfig)
	}
	previousUpdatedAt := *list[0].UpdatedAt
	one = config("Bad", "DIRECT")
	state, err = runner.UpdateSubscription(list[0].ID)
	if err == nil || state.Status != "running" || !state.SystemProxyEnabled || !strings.Contains(loadedConfig, "name: Refreshed") {
		t.Fatalf("failed update changed the connection: %+v, %v, %q", state, err, loadedConfig)
	}
	list, err = subs.List()
	if err != nil || list[0].Active || !list[1].Active || !list[0].UpdatedAt.Equal(previousUpdatedAt) {
		t.Fatalf("failed update changed subscriptions: %+v, %v", list, err)
	}
	cached, err := store.LoadSubscription(list[0].ID)
	if err != nil || string(cached) != config("One", "DIRECT") {
		t.Fatalf("failed update changed cached profile: %q, %v", cached, err)
	}
	active, err := store.Load()
	if err != nil || string(active) != two {
		t.Fatalf("failed update changed active profile: %q, %v", active, err)
	}
	previousUpdatedAt = *list[1].UpdatedAt
	two = config("Bad", "DIRECT")
	state, err = runner.UpdateSubscription(list[1].ID)
	if err == nil || state.Status != "running" || !state.SystemProxyEnabled || !strings.Contains(loadedConfig, "name: Refreshed") {
		t.Fatalf("failed active update changed the connection: %+v, %v, %q", state, err, loadedConfig)
	}
	list, err = subs.List()
	if err != nil || !list[1].Active || !list[1].UpdatedAt.Equal(previousUpdatedAt) {
		t.Fatalf("failed active update changed subscriptions: %+v, %v", list, err)
	}
	active, err = store.Load()
	if err != nil || string(active) != config("Refreshed", "REJECT") {
		t.Fatalf("failed active update changed active profile: %q, %v", active, err)
	}
	cached, err = store.LoadSubscription(list[1].ID)
	if err != nil || string(cached) != config("Refreshed", "REJECT") {
		t.Fatalf("failed active update changed cached profile: %q, %v", cached, err)
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

func TestTrafficTotalsReadsAuthenticatedController(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/connections" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("unexpected traffic request: %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"uploadTotal":1234,"downloadTotal":5678,"connections":[]}`))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, nil, nil)
	runner.apiPort = port
	runner.secret = "test-secret"
	if totals, err := runner.TrafficTotals(); err != nil || totals != (TrafficTotals{}) || requests != 0 {
		t.Fatalf("stopped traffic: %+v, %v, requests=%d", totals, err, requests)
	}
	runner.state.Status = "running"
	if totals, err := runner.TrafficTotals(); err != nil || totals.Upload != 1234 || totals.Download != 5678 || requests != 1 {
		t.Fatalf("running traffic: %+v, %v, requests=%d", totals, err, requests)
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
	port        int
	failPort    int
	failEnable  bool
	failDisable bool
}

func (p *testSystemProxy) Enable(port int) error {
	if p.failEnable || port == p.failPort {
		return errors.New("enable failed")
	}
	p.enabled = true
	p.port = port
	return nil
}
func (p *testSystemProxy) Disable() error {
	if p.failDisable {
		return errors.New("restore failed")
	}
	p.enabled = false
	p.port = 0
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
