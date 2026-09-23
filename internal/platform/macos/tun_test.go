//go:build darwin

package macos

import (
	"bytes"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestTunHelperAcceptsOnlyManagedConfig(t *testing.T) {
	raw := []byte("proxies: []\nrules:\n  - MATCH,DIRECT\n")
	secret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	compiled, err := profile.CompileForMode(raw, 7890, 19090, secret, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateManagedTunConfig(compiled, raw); err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{
		bytes.Replace(compiled, []byte("127.0.0.1:19090"), []byte("0.0.0.0:19090"), 1),
		bytes.Replace(compiled, []byte("auto-route: true"), []byte("auto-route: false"), 1),
		append(append([]byte(nil), compiled...), []byte("listeners: [{name: open, type: mixed, port: 9000}]\n")...),
	} {
		if _, err := validateManagedTunConfig(changed, raw); err == nil {
			t.Fatalf("accepted altered root config: %s", changed)
		}
	}
}
