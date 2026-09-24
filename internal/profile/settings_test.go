package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoutingModeSettings(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if mode, err := store.RoutingMode(); err != nil || mode != RoutingRule {
		t.Fatalf("default mode = %q, %v", mode, err)
	}
	for _, mode := range []string{RoutingGlobal, RoutingDirect, RoutingRule} {
		if err := store.SaveRoutingMode(mode); err != nil {
			t.Fatal(err)
		}
		if got, err := NewStore(dir).RoutingMode(); err != nil || got != mode {
			t.Fatalf("saved mode = %q, %v; want %q", got, err, mode)
		}
	}
	if err := store.SaveRoutingMode("invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestSettingsMigrateAndPreserveOtherOptions(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"routingMode":"global"}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Settings()
	if err != nil || settings.MixedPort != DefaultMixedPort || settings.RoutingMode != RoutingGlobal || settings.AutoConnect || settings.AutoConnectMode != "system" || settings.LogLevel != LogFromProfile || settings.Language != LanguageChinese {
		t.Fatalf("legacy settings = %+v, %v", settings, err)
	}
	settings, err = store.UpdateSettings(func(value *Settings) error {
		value.AutoConnect = true
		value.AutoConnectMode = "tun"
		value.LogLevel = "debug"
		value.MixedPort = 8900
		value.Language = LanguageEnglish
		return nil
	})
	if err != nil || !settings.AutoConnect {
		t.Fatalf("update settings = %+v, %v", settings, err)
	}
	if err := store.SaveRoutingMode(RoutingDirect); err != nil {
		t.Fatal(err)
	}
	settings, err = NewStore(dir).Settings()
	if err != nil || settings.MixedPort != 8900 || settings.RoutingMode != RoutingDirect || !settings.AutoConnect || settings.AutoConnectMode != "tun" || settings.LogLevel != "debug" || settings.Language != LanguageEnglish {
		t.Fatalf("reloaded settings = %+v, %v", settings, err)
	}
	if _, err := store.UpdateSettings(func(value *Settings) error { value.LogLevel = "verbose"; return nil }); err == nil {
		t.Fatal("invalid log level accepted")
	}
	settings, err = store.Settings()
	if err != nil || settings.LogLevel != "debug" {
		t.Fatalf("invalid update changed settings = %+v, %v", settings, err)
	}
	for _, port := range []int{0, 1023, 65536} {
		if _, err := store.UpdateSettings(func(value *Settings) error { value.MixedPort = port; return nil }); err == nil {
			t.Fatalf("invalid port %d accepted", port)
		}
	}
	if _, err := store.UpdateSettings(func(value *Settings) error { value.Language = "fr-FR"; return nil }); err == nil {
		t.Fatal("unsupported language accepted")
	}
	settings, err = store.Settings()
	if err != nil || settings.Language != LanguageEnglish {
		t.Fatalf("invalid language changed settings = %+v, %v", settings, err)
	}
}
