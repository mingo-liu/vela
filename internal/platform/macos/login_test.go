//go:build darwin

package macos

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoginLaunchRequiresPackagedAppAndCanDisable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := SetLoginLaunch(true); err == nil {
		t.Fatal("registered the test executable as a login app")
	}
	path := filepath.Join(home, "Library", "LaunchAgents", loginAgentName)
	if enabled, err := LoginLaunchEnabled(); err != nil || enabled {
		t.Fatalf("unexpected login item: %v, %v", enabled, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if enabled, err := LoginLaunchEnabled(); err != nil || !enabled {
		t.Fatalf("login item not detected: %v, %v", enabled, err)
	}
	if err := SetLoginLaunch(false); err != nil {
		t.Fatal(err)
	}
	if enabled, err := LoginLaunchEnabled(); err != nil || enabled {
		t.Fatalf("login item not removed: %v, %v", enabled, err)
	}
}
