package profile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func importRemovalSubscription(t *testing.T, subs *Subscriptions, address, data string) string {
	t.Helper()
	if err := subs.ImportDownloaded(DownloadedSubscription{URL: address, Data: []byte(data)}); err != nil {
		t.Fatal(err)
	}
	items, err := subs.List()
	if err != nil {
		t.Fatal(err)
	}
	return items[len(items)-1].ID
}

func TestRemoveActiveSubscriptionClearsConfigAndUnsharedCaches(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	subs := NewSubscriptions(store, &memoryURLStore{})
	shared := "proxy-providers:\n  shared:\n    type: http\n    url: https://example.com/shared\n"
	fileHeld := providerCachePath("providers", "file-held", "https://example.com/file-held")
	activeData := shared + "  unique:\n    type: http\n    url: https://example.com/unique\n  file-held:\n    type: http\n    url: https://example.com/file-held\nrule-providers:\n  unique-rule:\n    type: http\n    url: https://example.com/rules\n  local:\n    type: file\n    path: rules/local.yaml\nproxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	active := importRemovalSubscription(t, subs, "https://example.com/one", activeData)
	other := importRemovalSubscription(t, subs, "https://example.com/two", shared+"  local-copy:\n    type: file\n    path: "+fileHeld+"\n")
	if err := subs.Select(context.Background(), active); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	custom := []CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "DIRECT", Enabled: true}}
	if err := store.SaveProfileCustomRules(active, custom); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfileCustomRules(other, custom); err != nil {
		t.Fatal(err)
	}
	if err := writeProfile(filepath.Join(dir, "runtime.yaml"), activeData); err != nil {
		t.Fatal(err)
	}
	sharedPath := filepath.Join(dir, providerCachePath("providers", "shared", "https://example.com/shared"))
	uniquePath := filepath.Join(dir, providerCachePath("providers", "unique", "https://example.com/unique"))
	rulePath := filepath.Join(dir, providerCachePath("rules", "unique-rule", "https://example.com/rules"))
	localPath := filepath.Join(dir, "rules/local.yaml")
	fileHeldPath := filepath.Join(dir, fileHeld)
	for _, path := range []string{sharedPath, uniquePath, rulePath, localPath, fileHeldPath} {
		if err := writeProfile(path, "payload: [test]\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := subs.Remove(active); err != nil {
		t.Fatal(err)
	}
	if removed, _ := store.ProfileCustomRules(active); len(removed) != 0 {
		t.Fatal("removed subscription retained custom rules")
	}
	if retained, err := store.ProfileCustomRules(other); err != nil || len(retained) != 1 {
		t.Fatalf("other subscription rules removed: %v, %v", retained, err)
	}
	for _, path := range []string{store.path, filepath.Join(dir, "runtime.yaml"), filepath.Join(dir, "subscriptions", active+".yaml"), filepath.Join(dir, "selections.json"), uniquePath, rulePath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted subscription retained %s: %v", path, err)
		}
	}
	for _, path := range []string{sharedPath, localPath, fileHeldPath, filepath.Join(dir, "subscriptions", other+".yaml")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("deleted a retained resource %s: %v", path, err)
		}
	}
	items, err := subs.List()
	if err != nil || len(items) != 1 || items[0].ID != other || items[0].Active {
		t.Fatalf("remaining subscription changed: %+v, %v", items, err)
	}
	if err := subs.Select(context.Background(), other); err != nil || !store.Exists() {
		t.Fatalf("remaining subscription cannot be selected: %v", err)
	}
}

func TestRemoveInactiveSubscriptionPreservesCurrentProfile(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "subscription", true: "local"}[local], func(t *testing.T) {
			dir := t.TempDir()
			store := NewStore(dir)
			subs := NewSubscriptions(store, &memoryURLStore{})
			data := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
			removed := importRemovalSubscription(t, subs, "https://example.com/removed", data)
			if err := subs.Select(context.Background(), removed); err != nil {
				t.Fatal(err)
			}
			if err := store.SelectOption("Choose", "REJECT"); err != nil {
				t.Fatal(err)
			}
			if local {
				if err := subs.ImportLocal(data); err != nil {
					t.Fatal(err)
				}
			} else {
				current := importRemovalSubscription(t, subs, "https://example.com/current", data)
				if err := subs.Select(context.Background(), current); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.SelectOption("Choose", "DIRECT"); err != nil {
				t.Fatal(err)
			}
			if err := writeProfile(filepath.Join(dir, "runtime.yaml"), data); err != nil {
				t.Fatal(err)
			}
			if err := subs.Remove(removed); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{store.path, filepath.Join(dir, "runtime.yaml")} {
				if saved, err := os.ReadFile(path); err != nil || string(saved) != data {
					t.Fatalf("current config changed: %s, %v", path, err)
				}
			}
			if _, err := store.LoadSubscription(removed); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removed cache retained: %v", err)
			}
			selected, err := store.SelectedOptions()
			if err != nil || selected["Choose"] != "DIRECT" {
				t.Fatalf("current node choice changed: %v, %v", selected, err)
			}
			selections, err := os.ReadFile(filepath.Join(dir, "selections.json"))
			var catalog map[string]map[string]string
			if err != nil || json.Unmarshal(selections, &catalog) != nil || catalog["subscription:"+removed] != nil {
				t.Fatalf("deleted node choice retained: %s, %v", selections, err)
			}
		})
	}
}

type failRemovalURLStore struct {
	memoryURLStore
	failNext bool
}

func (s *failRemovalURLStore) Put(value string) error {
	if s.failNext {
		s.failNext = false
		return errors.New("catalog save failed")
	}
	return s.memoryURLStore.Put(value)
}

func TestRemoveSubscriptionRollsBackOnCatalogFailure(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	urls := &failRemovalURLStore{}
	subs := NewSubscriptions(store, urls)
	data := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	id := importRemovalSubscription(t, subs, "https://example.com/one", data)
	if err := subs.Select(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfileCustomRules(id, []CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "DIRECT", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := writeProfile(filepath.Join(dir, "runtime.yaml"), data); err != nil {
		t.Fatal(err)
	}
	before := urls.value
	files := map[string]string{}
	customPath, err := CustomRulesPath(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store.path, filepath.Join(dir, "runtime.yaml"), filepath.Join(dir, "subscriptions", id+".yaml"), filepath.Join(dir, "selections.json"), filepath.Join(dir, customPath)} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = string(contents)
	}
	urls.failNext = true
	if err := subs.Remove(id); err == nil {
		t.Fatal("catalog failure accepted")
	}
	if urls.value != before {
		t.Fatal("catalog was not restored")
	}
	for path, expected := range files {
		if contents, err := os.ReadFile(path); err != nil || string(contents) != expected {
			t.Fatalf("file was not restored: %s, %v", path, err)
		}
	}
	if err := subs.Remove(id); err != nil || store.Exists() {
		t.Fatalf("cannot retry deletion: %v", err)
	}
}

func TestRemoveLegacySubscriptionWithoutCache(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Import("rules: [MATCH,DIRECT]\n"); err != nil {
		t.Fatal(err)
	}
	address := "https://example.com/legacy"
	subs := NewSubscriptions(store, &memoryURLStore{value: address})
	if err := subs.Remove(subscriptionID(address)); err != nil || store.Exists() {
		t.Fatalf("legacy profile retained: %v", err)
	}
	items, err := subs.List()
	if err != nil || len(items) != 0 {
		t.Fatalf("legacy subscription retained: %v, %v", items, err)
	}
}
