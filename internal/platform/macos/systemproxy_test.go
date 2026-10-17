package macos

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeNetwork struct {
	state       proxySnapshot
	fail        string
	device      string
	others      map[string]*proxySnapshot
	failService string
}

func (f *fakeNetwork) run(name string, args ...string) (string, error) {
	if name == "route" {
		if f.device == "offline" {
			return "", errors.New("no default route")
		}
		device := f.device
		if device == "" {
			device = "en0"
		}
		return "   interface: " + device + "\n", nil
	}
	if name != "networksetup" || len(args) == 0 {
		return "", errors.New("unexpected command")
	}
	if args[0] == f.fail && (f.failService == "" || len(args) > 1 && args[1] == f.failService) {
		return "", errors.New("injected networksetup failure")
	}
	if args[0] == "-listnetworkserviceorder" {
		return "(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n(2) Ethernet\n(Hardware Port: USB LAN, Device: en1)\n", nil
	}
	if len(args) < 2 {
		return "", errors.New("missing network service")
	}
	state := &f.state
	if args[1] != "Wi-Fi" {
		state = f.others[args[1]]
		if state == nil {
			return "", errors.New("wrong network service")
		}
	}
	entry := func(kind string) *proxyEntry {
		switch kind {
		case "webproxy":
			return &state.HTTP
		case "securewebproxy":
			return &state.HTTPS
		default:
			return &state.SOCKS
		}
	}
	for _, kind := range []string{"webproxy", "securewebproxy", "socksfirewallproxy"} {
		if args[0] == "-get"+kind {
			v := entry(kind)
			flag := "No"
			if v.Enabled {
				flag = "Yes"
			}
			return fmt.Sprintf("Enabled: %s\nServer: %s\nPort: %s\nAuthenticated Proxy Enabled: 0\n", flag, v.Server, v.Port), nil
		}
		if args[0] == "-set"+kind {
			v := entry(kind)
			v.Enabled, v.Server, v.Port = true, args[2], args[3]
			return "", nil
		}
		if args[0] == "-set"+kind+"state" {
			entry(kind).Enabled = args[2] == "on"
			return "", nil
		}
	}
	switch args[0] {
	case "-getautoproxyurl":
		flag := "No"
		if state.PAC.Enabled {
			flag = "Yes"
		}
		return fmt.Sprintf("URL: %s\nEnabled: %s\n", state.PAC.URL, flag), nil
	case "-setautoproxyurl":
		state.PAC = autoProxy{Enabled: true, URL: args[2]}
	case "-setautoproxystate":
		state.PAC.Enabled = args[2] == "on"
	case "-getproxyautodiscovery":
		if state.Auto {
			return "Auto Proxy Discovery: On\n", nil
		}
		return "Auto Proxy Discovery: Off\n", nil
	case "-setproxyautodiscovery":
		state.Auto = args[2] == "on"
	default:
		return "", fmt.Errorf("unknown command: %s", args[0])
	}
	return "", nil
}

func TestSystemProxyRestoresPreviousProxy(t *testing.T) {
	previous := proxyEntry{Enabled: true, Server: "127.0.0.1", Port: "7897"}
	fake := &fakeNetwork{state: proxySnapshot{HTTP: previous, HTTPS: previous, SOCKS: previous, PAC: autoProxy{Enabled: true, URL: "http://example.test/proxy.pac"}, Auto: true}}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run = fake.run
	proxy.watchEnabled = false
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []proxyEntry{fake.state.HTTP, fake.state.HTTPS, fake.state.SOCKS} {
		if !owned(entry, 7890) {
			t.Fatalf("Vela did not take ownership: %+v", entry)
		}
	}
	if fake.state.PAC.Enabled || fake.state.Auto {
		t.Fatal("automatic proxy settings still enabled")
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	if fake.state.HTTP != previous || fake.state.HTTPS != previous || fake.state.SOCKS != previous || !fake.state.PAC.Enabled || !fake.state.Auto {
		t.Fatalf("previous settings not restored: %+v", fake.state)
	}
	if _, err := os.Stat(proxy.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot remains: %v", err)
	}
}

func TestSystemProxyPreservesExternalTakeover(t *testing.T) {
	fake := &fakeNetwork{}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run = fake.run
	proxy.watchEnabled = false
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	fake.state.HTTP = proxyEntry{Enabled: true, Server: "127.0.0.1", Port: "7897"}
	if active, err := proxy.Active(); err != nil || active {
		t.Fatalf("external takeover not detected: %t, %v", active, err)
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	if fake.state.HTTP.Port != "7897" {
		t.Fatal("external proxy was overwritten")
	}
}

func TestSystemProxyRollsBackPartialApply(t *testing.T) {
	fake := &fakeNetwork{fail: "-setsecurewebproxy"}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run = fake.run
	proxy.watchEnabled = false
	if err := proxy.Enable(7890); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("expected apply failure: %v", err)
	}
	if fake.state.HTTP.Enabled {
		t.Fatal("HTTP proxy was left enabled")
	}
	if _, err := os.Stat(proxy.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot remains: %v", err)
	}
}

func TestSystemProxyRealRoundTrip(t *testing.T) {
	if os.Getenv("VELA_TEST_SYSTEM_PROXY") != "1" {
		t.Skip("set VELA_TEST_SYSTEM_PROXY=1 to test the active macOS network service")
	}
	proxy := NewSystemProxy(t.TempDir())
	proxy.watchEnabled = false
	service, err := proxy.activeService()
	if err != nil {
		t.Fatal(err)
	}
	before, err := proxy.read(service, 7890)
	if err != nil {
		t.Fatal(err)
	}
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := proxy.Disable(); err != nil {
			t.Errorf("restore system proxy: %v", err)
		}
	})
	if active, err := proxy.Active(); err != nil || !active {
		t.Fatalf("system proxy not active: %t, %v", active, err)
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	after, err := proxy.read(service, 7890)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("network settings changed after restore: before=%+v after=%+v", before, after)
	}
}

func TestSystemProxyWatchRestoresAfterParentExit(t *testing.T) {
	binary := os.Getenv("VELA_TEST_APP_BINARY")
	if binary == "" {
		t.Skip("set VELA_TEST_APP_BINARY to a packaged Vela executable")
	}
	proxy := NewSystemProxy(t.TempDir())
	proxy.watchExecutable = binary
	service, err := proxy.activeService()
	if err != nil {
		t.Fatal(err)
	}
	before, err := proxy.read(service, 7890)
	if err != nil {
		t.Fatal(err)
	}
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := proxy.Disable(); err != nil {
			t.Errorf("restore system proxy: %v", err)
		}
	})
	if err := proxy.watch.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(proxy.path); errors.Is(err, os.ErrNotExist) {
			after, err := proxy.read(service, 7890)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("watcher did not restore prior settings: before=%+v after=%+v", before, after)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("watcher did not restore system proxy after parent pipe closed")
}

func TestSystemProxyFollowsDefaultServiceAndRestoresBoth(t *testing.T) {
	previousWiFi := proxySnapshot{HTTP: proxyEntry{Enabled: true, Server: "127.0.0.1", Port: "7897"}, HTTPS: proxyEntry{Server: "127.0.0.1", Port: "8001"}, SOCKS: proxyEntry{Server: "127.0.0.1", Port: "8002"}, PAC: autoProxy{Enabled: true, URL: "http://wifi.test/proxy.pac"}}
	previousEthernet := proxySnapshot{HTTP: proxyEntry{Server: "127.0.0.1", Port: "8003"}, HTTPS: proxyEntry{Server: "127.0.0.1", Port: "8004"}, SOCKS: proxyEntry{Enabled: true, Server: "127.0.0.1", Port: "7898"}, Auto: true}
	ethernet := previousEthernet
	fake := &fakeNetwork{state: previousWiFi, others: map[string]*proxySnapshot{"Ethernet": &ethernet}}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run, proxy.watchEnabled = fake.run, false
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	fake.device = "en1"
	if active, err := proxy.Active(); err != nil || active {
		t.Fatalf("old service reported active: %t, %v", active, err)
	}
	if active, err := proxy.Refresh(); err != nil || !active {
		t.Fatalf("handoff failed: %t, %v", active, err)
	}
	if !reflect.DeepEqual(fake.state, previousWiFi) || !owned(ethernet.HTTP, 7890) {
		t.Fatalf("old service not restored or new not configured: %+v, %+v", fake.state, ethernet)
	}
	fake.device = "en0"
	if active, err := proxy.Refresh(); err != nil || !active {
		t.Fatalf("return handoff failed: %t, %v", active, err)
	}
	if !reflect.DeepEqual(ethernet, previousEthernet) {
		t.Fatalf("Ethernet not restored: %+v", ethernet)
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.state, previousWiFi) {
		t.Fatalf("Wi-Fi not restored: %+v", fake.state)
	}
}

func TestSystemProxyHandoffPreservesTakeoverAndRetriesOffline(t *testing.T) {
	ethernet := proxySnapshot{}
	fake := &fakeNetwork{others: map[string]*proxySnapshot{"Ethernet": &ethernet}}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run, proxy.watchEnabled = fake.run, false
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	fake.device = "offline"
	if active, err := proxy.Refresh(); !active || err == nil {
		t.Fatalf("transient failure lost ownership: %t, %v", active, err)
	}
	if !owned(fake.state.HTTP, 7890) {
		t.Fatal("offline check changed proxy")
	}
	fake.device = "en1"
	fake.state.HTTP = proxyEntry{Enabled: true, Server: "127.0.0.1", Port: "7897"}
	if active, err := proxy.Refresh(); active || err != nil {
		t.Fatalf("takeover ignored: %t, %v", active, err)
	}
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	if fake.state.HTTP.Port != "7897" || ethernet.HTTP.Enabled {
		t.Fatal("takeover overwritten or new service changed")
	}
}

func TestSystemProxyHandoffRollsBackFailedNewService(t *testing.T) {
	ethernet := proxySnapshot{Auto: true}
	fake := &fakeNetwork{others: map[string]*proxySnapshot{"Ethernet": &ethernet}}
	proxy := NewSystemProxy(t.TempDir())
	proxy.run, proxy.watchEnabled = fake.run, false
	if err := proxy.Enable(7890); err != nil {
		t.Fatal(err)
	}
	fake.device, fake.fail, fake.failService = "en1", "-setsecurewebproxy", "Ethernet"
	if active, err := proxy.Refresh(); active || err == nil {
		t.Fatalf("handoff failure not reported: %t, %v", active, err)
	}
	if fake.state.HTTP.Enabled || ethernet.HTTP.Enabled || ethernet.HTTPS.Enabled || ethernet.SOCKS.Enabled || !ethernet.Auto {
		t.Fatalf("handoff left changed settings: %+v, %+v", fake.state, ethernet)
	}
}
