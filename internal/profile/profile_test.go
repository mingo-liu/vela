package profile

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"rules:\n  - IP-ASN,1234,DIRECT\n",
		"rule-providers: [reject]\n",
		"port: 1\nport: 2\n",
		"proxies: &nodes [DIRECT]\nproxy-groups: *nodes\n",
	}
	for _, source := range cases {
		if _, err := Compile([]byte(source), 7890, 9090, "secret"); err == nil {
			t.Errorf("accepted unsafe config: %q", source)
		}
	}
}

func TestCompileAcceptsRuleProviders(t *testing.T) {
	source := `udp: true
dns:
  enable: true
  listen: 127.0.0.1:1053
rule-providers:
  gfw:
    type: http
    behavior: domain
    url: https://example.com/gfw.txt
    path: ./ruleset/gfw.yaml
    interval: 86400
rules:
  - RULE-SET,gfw,DIRECT
  - MATCH,DIRECT
`
	compiled, err := Compile([]byte(source), 7890, 9090, "secret")
	if err != nil {
		t.Fatal(err)
	}
	out := string(compiled)
	if !strings.Contains(out, "RULE-SET,gfw,DIRECT") || !strings.Contains(out, "path: rules/") || strings.Contains(out, "./ruleset/gfw.yaml") || !strings.Contains(out, "udp: true") {
		t.Fatalf("rule providers were lost: %s", out)
	}
	if strings.Contains(out, "listen:") {
		t.Fatalf("dns listen was kept: %s", out)
	}
}

func TestCompileAcceptsProxyProvidersAndSniffer(t *testing.T) {
	source := `proxy-providers:
  remote:
    type: http
    url: https://example.com/nodes.yaml
    path: ../../settings.json
    interval: 86400
  local:
    type: file
    path: providers/local.yaml
proxy-groups:
  - name: select
    type: select
    use: [remote, local]
sniffer:
  enable: true
  sniff:
    TLS:
      ports: [443]
`
	compiled, err := Compile([]byte(source), 7890, 9090, "secret")
	if err != nil {
		t.Fatal(err)
	}
	out := string(compiled)
	if !strings.Contains(out, "path: providers/") || strings.Contains(out, "settings.json") || !strings.Contains(out, "sniffer:") || !strings.Contains(out, "use: [remote, local]") {
		t.Fatalf("proxy providers or sniffer lost: %s", out)
	}
	for _, bad := range []string{
		"proxy-providers:\n  bad:\n    type: file\n    path: ../../private\n",
		"proxy-providers:\n  bad:\n    type: file\n    path: rules/not-a-proxy.yaml\n",
		"proxy-providers:\n  bad:\n    type: http\n    url: file:///tmp/private\n",
	} {
		if _, err := Compile([]byte(bad), 7890, 9090, "secret"); err == nil {
			t.Fatalf("accepted unsafe provider: %s", bad)
		}
	}
	if _, err := CompileForMode([]byte(source), 7890, 9090, "secret", true, RoutingRule); err == nil || !strings.Contains(err.Error(), "Tun 模式") {
		t.Fatalf("TUN accepted remote proxy provider: %v", err)
	}
}

func TestProxyProviderAndSnifferWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO for core integration")
	}
	source := `proxy-providers:
  sample:
    type: inline
    payload:
      - name: sample-node
        type: socks5
        server: 127.0.0.1
        port: 1080
proxy-groups:
  - name: choose
    type: select
    use: [sample]
    proxies: [DIRECT]
sniffer:
  enable: true
  sniff:
    TLS:
      ports: [443]
rules:
  - GEOSITE,cn,DIRECT
  - MATCH,choose
`
	compiled, err := Compile([]byte(source), 17890, 19090, "secret")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	geosite, err := os.ReadFile(filepath.Join(filepath.Dir(binary), "geosite.dat"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), geosite, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runtime.yaml")
	if err := os.WriteFile(path, compiled, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(binary, "-t", "-d", dir, "-f", path).CombinedOutput()
	if err != nil {
		t.Fatalf("core rejected provider profile: %v\n%s", err, output)
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

func TestCompileLogLevelOverride(t *testing.T) {
	source := []byte("log-level: warning\nrules: [MATCH,DIRECT]\n")
	for _, test := range []struct{ level, want string }{{LogFromProfile, "warning"}, {"debug", "debug"}} {
		compiled, err := CompileWithLogLevel(source, 7890, 9090, "secret", false, RoutingRule, test.level)
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(compiled, &doc); err != nil {
			t.Fatal(err)
		}
		if got := lookup(doc.Content[0], "log-level"); got == nil || got.Value != test.want || strings.Count(string(compiled), "log-level:") != 1 {
			t.Fatalf("log level override = %s, want %q", compiled, test.want)
		}
	}
	if _, err := CompileWithLogLevel(source, 7890, 9090, "secret", false, RoutingRule, "verbose"); err == nil {
		t.Fatal("invalid log level accepted")
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

func TestRuleDownloadCannotOverwriteApplicationFiles(t *testing.T) {
	for _, path := range []string{"profile.yaml", "settings.json", "../escape.yaml", "/etc/hosts", "rules/../../runtime.yaml"} {
		raw := []byte("rule-providers:\n  remote:\n    type: http\n    behavior: domain\n    url: https://example.com/rules\n    path: " + path + "\nrules: [MATCH,DIRECT]\n")
		compiled, err := Compile(raw, 7890, 9090, "secret")
		if err != nil {
			t.Fatal(err)
		}
		var doc yaml.Node
		yaml.Unmarshal(compiled, &doc)
		providers := lookup(doc.Content[0], "rule-providers")
		got := lookup(providers.Content[1], "path").Value
		if !strings.HasPrefix(got, "rules/") || len(filepath.Base(got)) != 69 {
			t.Fatalf("unsafe download path %q", got)
		}
		again, err := Compile(compiled, 7890, 9090, "secret")
		if err != nil || string(again) != string(compiled) {
			t.Fatalf("compilation not idempotent: %v", err)
		}
	}
	for _, path := range []string{"profile.yaml", "rules/../../secret", "/etc/passwd"} {
		raw := []byte("rule-providers:\n  local:\n    type: file\n    path: " + path + "\n")
		if _, err := Compile(raw, 7890, 9090, "secret"); err == nil {
			t.Fatalf("unsafe file provider accepted: %s", path)
		}
	}
}

func TestRuleProviderWithRealCoreCannotOverwriteProfile(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO for core integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "payload: [example.com]\n") }))
	defer server.Close()
	dir := t.TempDir()
	marker := []byte("application-profile-must-survive")
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	api := port.Addr().(*net.TCPAddr).Port
	port.Close()
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mixed := proxy.Addr().(*net.TCPAddr).Port
	proxy.Close()
	raw := []byte("rule-providers:\n  remote:\n    type: http\n    behavior: domain\n    url: " + server.URL + "\n    path: profile.yaml\n    interval: 3600\nrules:\n  - RULE-SET,remote,DIRECT\n  - MATCH,DIRECT\n")
	compiled, err := Compile(raw, mixed, api, "integration-secret")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "runtime.yaml")
	os.WriteFile(config, compiled, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", config)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + dir}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	deadline := time.Now().Add(8 * time.Second)
	for {
		files, _ := filepath.Glob(filepath.Join(dir, "rules", "*.yaml"))
		if len(files) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider not downloaded")
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
	if err != nil || !bytes.Equal(got, marker) {
		t.Fatalf("profile overwritten: %q %v", got, err)
	}
}
