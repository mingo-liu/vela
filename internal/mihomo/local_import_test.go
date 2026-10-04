package mihomo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestImportLocalProfileWhileConnected(t *testing.T) {
	const initial = "proxy-groups: [{name: One, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,DIRECT']\n"
	const replacement = "proxy-groups: [{name: Two, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,REJECT']\n"
	for _, tun := range []bool{false, true} {
		for _, previous := range []string{"local", "subscription"} {
			for _, outcome := range []string{"success", "invalid YAML", "reload failure"} {
				t.Run(strconv.FormatBool(tun)+"/"+previous+"/"+outcome, func(t *testing.T) {
					dir := t.TempDir()
					store := profile.NewStore(dir)
					subs := profile.NewSubscriptions(store, &testURLStore{})
					runner := NewRunner(store, subs, dir, "", 0, nil, nil)
					if previous == "local" {
						if _, err := runner.Import(initial); err != nil {
							t.Fatal(err)
						}
					} else {
						if err := subs.ImportDownloaded(profile.DownloadedSubscription{URL: "https://subscription.invalid/config", Data: []byte(initial)}); err != nil {
							t.Fatal(err)
						}
						items, _ := subs.List()
						if _, err := runner.SelectSubscription(items[0].ID); err != nil {
							t.Fatal(err)
						}
					}
					if _, err := runner.SetRoutingMode(profile.RoutingGlobal); err != nil {
						t.Fatal(err)
					}
					var loaded string
					api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
						if request.URL.Path == "/configs" && request.Method == http.MethodPut {
							var config struct {
								Payload string `json:"payload"`
							}
							if err := json.NewDecoder(request.Body).Decode(&config); err != nil {
								t.Error(err)
								w.WriteHeader(http.StatusBadRequest)
								return
							}
							if strings.Contains(config.Payload, "Missing") {
								w.WriteHeader(http.StatusBadRequest)
								return
							}
							loaded = config.Payload
						}
						_, _ = w.Write([]byte(`{"proxies":{}}`))
					}))
					defer api.Close()
					apiURL, _ := url.Parse(api.URL)
					runner.apiPort, _ = strconv.Atoi(apiURL.Port())
					runner.state.Port = runner.apiPort
					runner.state.Status = "running"
					runner.state.SystemProxyEnabled = !tun
					runner.state.TunEnabled = tun
					runner.secret = "test-secret"
					runner.cmd, runner.done = &exec.Cmd{}, make(chan struct{})
					cmd, done := runner.cmd, runner.done
					before := runner.Snapshot()
					itemsBefore, _ := subs.List()
					compiledBefore, err := runner.compile([]byte(initial), runner.apiPort, tun)
					if err != nil {
						t.Fatal(err)
					}
					loaded = string(compiledBefore)
					if err := writePrivate(filepath.Join(dir, "runtime.yaml"), compiledBefore); err != nil {
						t.Fatal(err)
					}
					input := replacement
					switch outcome {
					case "invalid YAML":
						input = "listeners: [{name: unsafe, type: mixed, port: 9999}]\n"
					case "reload failure":
						input = strings.Replace(replacement, "[DIRECT, REJECT]", "[Missing]", 1)
					}
					state, err := runner.Import(input)
					if (err == nil) != (outcome == "success") {
						t.Fatalf("import result: %+v, %v", state, err)
					}
					if runner.cmd != cmd || runner.done != done || state.Status != "running" || state.SystemProxyEnabled != !tun || state.TunEnabled != tun || state.RoutingMode != profile.RoutingGlobal {
						t.Fatalf("import changed the connection: %+v", state)
					}
					raw, err := store.Load()
					if err != nil {
						t.Fatal(err)
					}
					items, _ := subs.List()
					if outcome == "success" {
						if string(raw) != replacement || !strings.Contains(loaded, "name: Two") || state.ConfigVersion != before.ConfigVersion+1 {
							t.Fatalf("new profile not applied: %q, %q, %+v", raw, loaded, state)
						}
						for _, item := range items {
							if item.Active {
								t.Fatal("import retained the active subscription")
							}
						}
					} else {
						runtimeConfig, fileErr := os.ReadFile(filepath.Join(dir, "runtime.yaml"))
						if string(raw) != initial || state != before || !reflect.DeepEqual(items, itemsBefore) || loaded != string(compiledBefore) || fileErr != nil || string(runtimeConfig) != string(compiledBefore) {
							t.Fatalf("failed import did not restore profile: raw=%q, state=%+v, items=%+v, runtime=%q, error=%v", raw, state, items, runtimeConfig, fileErr)
						}
					}
				})
			}
		}
	}
}

func TestImportLocalProfileWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	proxy := &testSystemProxy{}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, proxy, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.Import("proxy-groups: [{name: One, type: select, proxies: [DIRECT, REJECT]}]\nrules: ['MATCH,DIRECT']\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	cmd := runner.cmd
	for _, options := range []string{"[DIRECT, REJECT]", "[Missing]"} {
		state, err := runner.Import("proxy-groups: [{name: Two, type: select, proxies: " + options + "}]\nrules: ['MATCH,DIRECT']\n")
		if (err == nil) != (options == "[DIRECT, REJECT]") || state.Status != "running" || !state.SystemProxyEnabled || !proxy.enabled || runner.cmd != cmd {
			t.Fatalf("local import: %+v, %v", state, err)
		}
		groups, err := runner.Groups()
		if err != nil || !hasGroup(groups, "Two") || hasGroup(groups, "One") {
			t.Fatalf("running profile: %+v, %v", groups, err)
		}
	}
}
