package profile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestSubscriptionSelectionSurvivesUpdatesAndStaysIsolated(t *testing.T) {
	body := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	store := NewStore(t.TempDir())
	subs := NewSubscriptions(store, &memoryURLStore{})
	for i := 0; i < 2; i++ {
		if err := subs.Import(context.Background(), server.URL); err != nil {
			t.Fatal(err)
		}
	}
	items, _ := subs.List()
	if err := subs.Select(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	body += "rules: ['MATCH,DIRECT']\n"
	if err := subs.Update(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	choices, err := store.SelectedOptions()
	if err != nil || choices["Choose"] != "REJECT" {
		t.Fatalf("update lost choice: %v, %v", choices, err)
	}
	if err := subs.Select(context.Background(), items[1].ID); err != nil {
		t.Fatal(err)
	}
	choices, err = store.SelectedOptions()
	if err != nil || len(choices) != 0 {
		t.Fatalf("choice leaked into independent subscription: %v, %v", choices, err)
	}
	if err := subs.Select(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	choices, err = store.SelectedOptions()
	if err != nil || choices["Choose"] != "REJECT" {
		t.Fatalf("switch lost choice: %v, %v", choices, err)
	}
	if err := store.ReconcileSelectedOptions([]SelectorGroup{{Name: "Choose", Options: []string{"DIRECT"}}}); err != nil {
		t.Fatal(err)
	}
	choices, err = store.SelectedOptions()
	if err != nil || len(choices) != 0 {
		t.Fatalf("obsolete choice retained: %v, %v", choices, err)
	}
}

func TestSubscriptionSelectionMigratesLegacyContentKey(t *testing.T) {
	store := NewStore(t.TempDir())
	body := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	if err := store.Import(body); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	subs := NewSubscriptions(store, &memoryURLStore{})
	if err := subs.Import(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	items, _ := subs.List()
	if err := subs.Select(context.Background(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	choices, err := store.SelectedOptions()
	if err != nil || choices["Choose"] != "REJECT" {
		t.Fatalf("legacy migration: %v, %v", choices, err)
	}
	if err := subs.ImportLocal(body); err != nil {
		t.Fatal(err)
	}
	choices, err = store.SelectedOptions()
	if err != nil || len(choices) != 0 {
		t.Fatalf("migrated content choice retained: %v, %v", choices, err)
	}
}
