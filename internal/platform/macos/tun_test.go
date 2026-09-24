//go:build darwin

package macos

import (
	"bytes"
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
