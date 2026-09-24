package profile

import "testing"

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
