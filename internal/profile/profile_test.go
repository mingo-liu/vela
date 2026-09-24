package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestCompileOwnsListenersAndPreservesNodes(t *testing.T) {
	source := `mixed-port: 8899
allow-lan: true
external-controller: 0.0.0.0:9090
proxies:
  - name: node
    type: socks5
    server: example.com
    port: 1080
proxy-groups:
  - name: choose
    type: select
    proxies: [node, DIRECT]
rules:
  - GEOIP,CN,DIRECT
  - MATCH,choose
`
	compiled, err := Compile([]byte(source), 7890, 19090, "secret")
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(compiled, &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	for key, want := range map[string]string{"mixed-port": "7890", "allow-lan": "false", "bind-address": "127.0.0.1", "external-controller": "127.0.0.1:19090", "secret": "secret"} {
		if got := lookup(root, key); got == nil || got.Value != want {
			t.Errorf("%s = %v, want %q", key, got, want)
		}
	}
	if !strings.Contains(string(compiled), "example.com") || !strings.Contains(string(compiled), "GEOIP,CN,DIRECT") || !strings.Contains(string(compiled), "MATCH,choose") {
		t.Fatal("compatible profile fields were lost")
	}
}

func TestCompileRejectsUnsafeOrUnsupportedConfig(t *testing.T) {
	cases := []string{
		"listeners:\n  - name: open\n    type: mixed\n    port: 9000\n",
		"proxy-providers:\n  remote:\n    type: file\n    path: /tmp/private\n",
		"rules:\n  - GEOSITE,CN,DIRECT\n",
		"dns:\n  listen: 0.0.0.0:53\n",
		"port: 1\nport: 2\n",
		"proxies: &nodes [DIRECT]\nproxy-groups: *nodes\n",
	}
	for _, source := range cases {
		if _, err := Compile([]byte(source), 7890, 9090, "secret"); err == nil {
			t.Errorf("accepted unsafe config: %q", source)
		}
	}
}

func TestCompileUsesSelectedRoutingMode(t *testing.T) {
	for _, mode := range []string{RoutingRule, RoutingGlobal, RoutingDirect} {
		compiled, err := CompileForMode([]byte("mode: direct\nrules: [MATCH,DIRECT]\n"), 7890, 9090, "secret", false, mode)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(compiled, &doc); err != nil {
			t.Fatal(err)
		}
		if got := lookup(doc.Content[0], "mode"); got == nil || got.Value != mode {
			t.Fatalf("compiled mode = %v, want %q", got, mode)
		}
	}
	if _, err := CompileForMode([]byte("rules: [MATCH,DIRECT]\n"), 7890, 9090, "secret", false, "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestCompileManagesTunForBothModes(t *testing.T) {
	source := []byte("tun:\n  enable: true\n  device: utun99\n  auto-route: false\nrules: [MATCH,DIRECT]\n")
	for _, enabled := range []bool{false, true} {
		compiled, err := CompileForMode(source, 7890, 9090, "secret", enabled, RoutingRule)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(compiled, &doc); err != nil {
			t.Fatal(err)
		}
		tun := lookup(doc.Content[0], "tun")
		if !enabled && tun != nil {
			t.Fatal("system proxy config retained TUN")
		}
		if enabled {
			if tun == nil || lookup(tun, "enable").Value != "true" || lookup(tun, "auto-route").Value != "true" || lookup(tun, "device") != nil {
				t.Fatalf("TUN config was not managed: %s", compiled)
			}
			if dns := lookup(doc.Content[0], "dns"); dns == nil || lookup(dns, "enable").Value != "true" {
				t.Fatalf("TUN DNS was not enabled: %s", compiled)
			}
		}
	}
}

func TestCompileTunWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to check generated TUN config")
	}
	dir := t.TempDir()
	compiled, err := CompileForMode([]byte("proxies: []\nrules:\n  - MATCH,DIRECT\n"), 7890, 9090, "secret", true, RoutingRule)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runtime.yaml")
	if err := os.WriteFile(path, compiled, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "-t", "-d", dir, "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo rejected TUN config: %v\n%s", err, output)
	}
}

func TestImportFailureKeepsLastProfile(t *testing.T) {
	store := NewStore(t.TempDir())
	valid := "proxies: []\nrules:\n  - MATCH,DIRECT\n"
	if err := store.Import(valid); err != nil {
		t.Fatal(err)
	}
	if err := store.Import("listeners: [{name: open, type: mixed, port: 9000}]"); err == nil {
		t.Fatal("expected unsupported listener rejection")
	}
	got, err := os.ReadFile(filepath.Join(filepath.Dir(store.path), "profile.yaml"))
	if err != nil || string(got) != valid {
		t.Fatalf("profile changed after failed import: %q, %v", got, err)
	}
}
