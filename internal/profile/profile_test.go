package profile

import (
	"os"
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
		"tun:\n  enable: true\n",
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
