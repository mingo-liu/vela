package mihomo

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestCustomRulesSurviveSubscriptionUpdateAndSwitch(t *testing.T) {
	data := "rules: ['MATCH,DIRECT']\nproxy-groups: [{name: Choose, type: select, proxies: [DIRECT, REJECT]}]\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(data)) }))
	defer server.Close()
	dir := t.TempDir()
	store := profile.NewStore(dir)
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, "", 7890, nil, nil)
	runner.secret = "test-secret"
	custom := []profile.CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "Choose", Enabled: true}}
	if _, err := runner.ImportSubscription(server.URL); err != nil {
		t.Fatal(err)
	}
	items, err := runner.Subscriptions()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.SelectSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.SaveProfileRules(items[0].ID, custom); err != nil {
		t.Fatal(err)
	}
	assertEffective := func(want bool) {
		t.Helper()
		raw, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := runner.compile(raw, 9090, false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(compiled), "DOMAIN,example.com,Choose") != want {
			t.Fatalf("effective profile = %s", compiled)
		}
		if strings.Contains(string(raw), "DOMAIN,example.com") {
			t.Fatal("source profile was modified")
		}
		got, err := runner.CustomRules()
		if err != nil || !reflect.DeepEqual(got, custom) {
			t.Fatalf("saved rules = %v, %v", got, err)
		}
	}
	assertEffective(true)
	data = "rules: ['MATCH,REJECT']\n"
	if _, err := runner.UpdateSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	assertEffective(false)
	if _, err := runner.Import("proxy-groups: [{name: Choose, type: select, proxies: [DIRECT]}]\nrules: ['MATCH,DIRECT']"); err != nil {
		t.Fatal(err)
	}
	local, err := runner.RuleEditor("")
	if err != nil || len(local.Rules) != 0 {
		t.Fatalf("local profile inherited subscription rules: %+v, %v", local, err)
	}
	if _, err := runner.SelectSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	assertEffective(false)
}

func TestCustomRulesHotReloadRollback(t *testing.T) {
	for _, tun := range []bool{false, true} {
		t.Run(strconv.FormatBool(tun), func(t *testing.T) {
			dir := t.TempDir()
			store := profile.NewStore(dir)
			if err := store.Import("rules: ['MATCH,DIRECT']\n"); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			var mu sync.Mutex
			var loaded string
			reject := false
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/configs" {
					var body struct {
						Payload string `json:"payload"`
					}
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					mu.Lock()
					defer mu.Unlock()
					// Simulate even a partially applied configuration before failure.
					loaded = body.Payload
					if reject && strings.Contains(loaded, "blocked.example.com") {
						w.WriteHeader(400)
						return
					}
				}
				if req.URL.Path == "/proxies" {
					_, _ = w.Write([]byte(`{"proxies":{}}`))
				}
			}))
			defer api.Close()
			subs := profile.NewSubscriptions(store, &testURLStore{})
			if err := subs.ImportDownloaded(profile.DownloadedSubscription{URL: "https://example.invalid/rollback", Data: []byte("rules: ['MATCH,DIRECT']\n")}); err != nil {
				t.Fatal(err)
			}
			items, err := subs.List()
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRunner(store, subs, dir, "", listener.Addr().(*net.TCPAddr).Port, nil, nil)
			if _, err := runner.SelectSubscription(items[0].ID); err != nil {
				t.Fatal(err)
			}
			address, _ := url.Parse(api.URL)
			runner.apiPort, _ = strconv.Atoi(address.Port())
			runner.secret = "test-secret"
			runner.state.Status = "running"
			runner.state.TunEnabled = tun
			runner.state.SystemProxyEnabled = !tun
			runner.cmd = &exec.Cmd{}
			runner.done = make(chan struct{})
			first := []profile.CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "REJECT", Enabled: true}}
			if _, err := runner.SaveProfileRules(items[0].ID, first); err != nil {
				t.Fatal(err)
			}
			before := runner.Snapshot()
			mu.Lock()
			reject = true
			previousConfig := loaded
			mu.Unlock()
			second := []profile.CustomRule{{ID: "two", Type: "DOMAIN", Domain: "blocked.example.com", Target: "REJECT", Enabled: true}}
			if _, err := runner.SaveProfileRules(items[0].ID, second); err == nil {
				t.Fatal("expected reload failure")
			}
			if got := runner.Snapshot(); got != before {
				t.Fatalf("connection state changed: %v -> %v", before, got)
			}
			got, err := runner.CustomRules()
			if err != nil || !reflect.DeepEqual(got, first) {
				t.Fatalf("rules not rolled back: %v, %v", got, err)
			}
			file, err := os.ReadFile(filepath.Join(dir, "runtime.yaml"))
			if err != nil || string(file) != previousConfig {
				t.Fatalf("runtime file not restored: %s, %v", file, err)
			}
			mu.Lock()
			restored := loaded
			mu.Unlock()
			if restored != previousConfig {
				t.Fatal("live profile not restored")
			}
		})
	}
}

func TestCustomRulesRoutingWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO for real core routing")
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import("rules: ['MATCH,DIRECT']\n"); err != nil {
		t.Fatal(err)
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	subs := profile.NewSubscriptions(store, &testURLStore{})
	for _, address := range []string{"https://example.invalid/one", "https://example.invalid/two"} {
		if err := subs.ImportDownloaded(profile.DownloadedSubscription{URL: address, Data: []byte("rules: ['MATCH,DIRECT']\n")}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, subs, dir, binary, port, nil, nil)
	t.Cleanup(runner.Close)
	if _, err := runner.SelectSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	custom := []profile.CustomRule{{ID: "block", Type: "DOMAIN-SUFFIX", Domain: "localhost", Target: "REJECT", Enabled: true}, {ID: "allow", Type: "DOMAIN", Domain: "localhost", Target: "DIRECT", Enabled: true}}
	if _, err := runner.SaveCustomRules(custom); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(); err != nil {
		t.Fatal(err)
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("reached")) }))
	defer destination.Close()
	address, _ := url.Parse(destination.URL)
	address.Host = "localhost:" + address.Port()
	proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	check := func(allowed bool) {
		t.Helper()
		response, err := client.Get(address.String())
		if err != nil {
			if allowed {
				t.Fatal(err)
			}
			return
		}
		defer response.Body.Close()
		if (response.StatusCode == 200) != allowed {
			t.Fatalf("status = %d, allowed = %v", response.StatusCode, allowed)
		}
	}
	check(false)
	before := runner.Snapshot()
	if _, err := runner.SaveProfileRules(items[1].ID, []profile.CustomRule{{ID: "other", Type: "DOMAIN", Domain: "localhost", Target: "DIRECT", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if got := runner.Snapshot(); got != before {
		t.Fatalf("editing inactive subscription changed runtime: %+v", got)
	}
	check(false)
	if _, err := runner.SelectSubscription(items[1].ID); err != nil {
		t.Fatal(err)
	}
	check(true)
	if _, err := runner.SelectSubscription(items[0].ID); err != nil {
		t.Fatal(err)
	}
	check(false)
	custom[0], custom[1] = custom[1], custom[0]
	if _, err := runner.SaveCustomRules(custom); err != nil {
		t.Fatal(err)
	}
	check(true)
	custom[0].Enabled = false
	if _, err := runner.SaveCustomRules(custom); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err := runner.SaveCustomRules(nil); err != nil {
		t.Fatal(err)
	}
	check(true)
}
