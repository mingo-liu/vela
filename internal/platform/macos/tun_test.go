//go:build darwin

package macos

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestTunServiceReadsUnixPeerUID(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "vela-peer-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "service.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	uid, err := tunPeerUID(server)
	if err != nil {
		t.Fatal(err)
	}
	if uid != os.Getuid() {
		t.Fatalf("peer uid = %d, want %d", uid, os.Getuid())
	}
}

func TestTunHelperAcceptsOnlyManagedConfig(t *testing.T) {
	raw := []byte("proxies: []\nrules:\n  - MATCH,DIRECT\n")
	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, mode := range []string{profile.RoutingRule, profile.RoutingGlobal, profile.RoutingDirect} {
		compiled, err := profile.CompileForMode(raw, 7890, 19090, secret, true, mode)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := validateManagedTunConfig(compiled, raw, mode); err != nil {
			t.Fatalf("mode %s rejected: %v", mode, err)
		}
	}
	debugConfig, err := profile.CompileWithLogLevel(raw, 7890, 19090, secret, true, profile.RoutingRule, "debug")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfigWithLogLevel(debugConfig, raw, profile.RoutingRule, "debug"); err != nil {
		t.Fatalf("managed log level rejected: %v", err)
	}
	if _, err := validateManagedTunConfig(debugConfig, raw, profile.RoutingRule); err == nil {
		t.Fatal("unexpected log level accepted")
	}
	customPortConfig, err := profile.CompileForMode(raw, 8900, 19090, secret, true, profile.RoutingRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfigWithSettings(customPortConfig, raw, profile.RoutingRule, profile.LogFromProfile, 8900); err != nil {
		t.Fatalf("managed custom port rejected: %v", err)
	}
	if _, err := validateManagedTunConfig(customPortConfig, raw, profile.RoutingRule); err == nil {
		t.Fatal("unexpected custom port accepted")
	}
	compiled, err := profile.CompileForMode(raw, 7890, 19090, secret, true, profile.RoutingRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfig(compiled, raw, profile.RoutingRule); err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		bytes.Replace(compiled, []byte("127.0.0.1:19090"), []byte("0.0.0.0:19090"), 1),
		bytes.Replace(compiled, []byte("auto-route: true"), []byte("auto-route: false"), 1),
		append(append([]byte(nil), compiled...), []byte("listeners: [{name: open, type: mixed, port: 9000}]\n")...),
	} {
		if _, err := validateManagedTunConfig(changed, raw, profile.RoutingRule); err == nil {
			t.Fatalf("accepted altered root config: %s", changed)
		}
	}
}

func TestTunCustomRulesReadAndValidation(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw := []byte("rules: ['MATCH,DIRECT']\n")
	if got, err := applyTunCustomRules(root, raw); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("missing custom file = %s, %v", got, err)
	}
	store := profile.NewStore(dir)
	custom := []profile.CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "REJECT", Enabled: true}}
	if err := store.SaveCustomRules(custom); err != nil {
		t.Fatal(err)
	}
	effective, err := applyTunCustomRules(root, raw)
	if err != nil {
		t.Fatal(err)
	}
	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	compiled, err := profile.CompileForMode(effective, 7890, 19090, secret, true, profile.RoutingRule)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfig(compiled, effective, profile.RoutingRule); err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfig(compiled, raw, profile.RoutingRule); err == nil {
		t.Fatal("custom rules accepted without source verification")
	}
	if err := os.WriteFile(filepath.Join(dir, profile.LocalCustomRulesFile), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTunCustomRules(root, raw); err == nil {
		t.Fatal("corrupt custom rules ignored")
	}
}

func TestTunCustomRulesFollowSelectedSubscription(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	subs := profile.NewSubscriptions(store, profile.NewFileURLStore(dir, nil))
	raw := []byte("rules: ['MATCH,DIRECT']\n")
	for _, address := range []string{"https://example.invalid/one", "https://example.invalid/two"} {
		if err := subs.ImportDownloaded(profile.DownloadedSubscription{URL: address, Data: raw}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfileCustomRules(items[0].ID, []profile.CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "REJECT", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for index, item := range items {
		if err := subs.Select(context.Background(), item.ID); err != nil {
			t.Fatal(err)
		}
		got, err := applyTunCustomRules(root, raw)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(got, []byte("DOMAIN,example.com,REJECT")) != (index == 0) {
			t.Fatalf("TUN used wrong rule scope: %s", got)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "subscriptions.json"), []byte(`{"subscriptions":[{"id":"../../outside","active":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTunCustomRules(root, raw); err == nil {
		t.Fatal("unsafe subscription path accepted")
	}
}
