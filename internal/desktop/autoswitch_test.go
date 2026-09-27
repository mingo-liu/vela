package desktop

import (
	"errors"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

type fakeSelector struct {
	calls []string
	err   error
}

func (f *fakeSelector) Select(group, option string) error {
	f.calls = append(f.calls, group+"/"+option)
	return f.err
}

func TestAutoSwitcherSelectsOnNetworkChange(t *testing.T) {
	store := profile.NewStore(t.TempDir())
	if _, err := store.UpdateSettings(func(s *profile.Settings) error {
		s.AutoSwitch = profile.AutoSwitch{Enabled: true, Group: "节点选择", Wired: "中转", Wireless: "直连"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	iface, kind := "en4", "wired"
	selector := &fakeSelector{}
	switches := 0
	a := NewAutoSwitcher(selector, store, func() (string, string, error) { return iface, kind, nil }, func() { switches++ })

	a.check()
	a.check()
	iface, kind = "en0", "wireless"
	a.check()
	iface, kind = "utun4", ""
	a.check()
	want := []string{"节点选择/中转", "节点选择/直连"}
	if len(selector.calls) != len(want) || selector.calls[0] != want[0] || selector.calls[1] != want[1] || switches != 2 {
		t.Fatalf("calls = %v, switches = %d", selector.calls, switches)
	}

	selector.err = errors.New("core starting")
	iface, kind = "en4", "wired"
	a.check()
	selector.err = nil
	a.check()
	a.check()
	if len(selector.calls) != 4 || switches != 3 {
		t.Fatalf("failed select was not retried once: %v, switches = %d", selector.calls, switches)
	}
}

func TestAutoSwitcherDisabled(t *testing.T) {
	selector := &fakeSelector{}
	a := NewAutoSwitcher(selector, profile.NewStore(t.TempDir()), func() (string, string, error) { return "en0", "wireless", nil }, nil)
	a.check()
	if len(selector.calls) != 0 {
		t.Fatalf("disabled switcher selected %v", selector.calls)
	}
}
