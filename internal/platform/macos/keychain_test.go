//go:build darwin

package macos

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestSubscriptionKeychainRoundTrip(t *testing.T) {
	if os.Getenv("VELA_TEST_KEYCHAIN") != "1" {
		t.Skip("set VELA_TEST_KEYCHAIN=1 to exercise the macOS Keychain")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	keychain := SubscriptionKeychain{Account: "test-" + hex.EncodeToString(suffix[:])}
	t.Cleanup(func() { _ = keychain.Delete() })
	if _, err := keychain.Get(); err != profile.ErrNoSubscription {
		t.Fatalf("expected missing test entry: %v", err)
	}
	const value = "https://example.invalid/subscribe?token=secret"
	if err := keychain.Put(value); err != nil {
		t.Fatal(err)
	}
	got, err := keychain.Get()
	if err != nil || got != value {
		t.Fatalf("round trip failed: %q, %v", got, err)
	}
	if err := keychain.Delete(); err != nil {
		t.Fatal(err)
	}
}
