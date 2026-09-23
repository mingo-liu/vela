package profile

import "testing"

func TestSelectedOptionsFollowProfileContent(t *testing.T) {
	store := NewStore(t.TempDir())
	first := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	second := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [REJECT, DIRECT]\n"
	if err := store.Import(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "missing"); err == nil {
		t.Fatal("invalid option was saved")
	}
	if err := store.Import(second); err != nil {
		t.Fatal(err)
	}
	selected, err := store.SelectedOptions()
	if err != nil || len(selected) != 0 {
		t.Fatalf("selection leaked into another profile: %+v, %v", selected, err)
	}
	if err := store.Import(first); err != nil {
		t.Fatal(err)
	}
	selected, err = store.SelectedOptions()
	if err != nil || selected["Choose"] != "REJECT" {
		t.Fatalf("selection was not restored: %+v, %v", selected, err)
	}
}
