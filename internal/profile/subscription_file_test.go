package profile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type countingLegacyStore struct {
	value     string
	reads     int
	deletes   int
	deleteErr error
}

func (s *countingLegacyStore) Get() (string, error) {
	s.reads++
	if s.value == "" {
		return "", ErrNoSubscription
	}
	return s.value, nil
}

func (s *countingLegacyStore) Put(value string) error { s.value = value; return nil }
func (s *countingLegacyStore) Delete() error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.value = ""
	return nil
}

func TestFileURLStoreKeepsMigratedFileIfKeychainDeleteFails(t *testing.T) {
	dir := t.TempDir()
	const address = "https://example.invalid/sub?token=secret"
	legacy := &countingLegacyStore{value: address, deleteErr: errors.New("access denied")}
	store := NewFileURLStore(dir, legacy)
	subs := NewSubscriptions(NewStore(dir), store)
	list, err := subs.List()
	if err != nil || len(list) != 1 || list[0].URL != address {
		t.Fatalf("legacy subscription was lost: %+v, %v", list, err)
	}
	list, err = NewSubscriptions(NewStore(dir), NewFileURLStore(dir, legacy)).List()
	if err != nil || len(list) != 1 || list[0].URL != address || legacy.reads != 1 || legacy.deletes != 1 {
		t.Fatalf("migrated file was not authoritative: %+v, %v, legacy=%+v", list, err, legacy)
	}
}

func TestFileURLStoreMigratesLegacyOnce(t *testing.T) {
	dir := t.TempDir()
	const catalog = `{"subscriptions":[{"id":"old","url":"https://example.invalid/sub?token=secret","active":true}]}`
	legacy := &countingLegacyStore{value: catalog}
	store := NewFileURLStore(dir, legacy)
	for range 2 {
		got, err := store.Get()
		if err != nil || got != catalog {
			t.Fatalf("migrated catalog: %q, %v", got, err)
		}
	}
	if legacy.reads != 1 {
		t.Fatalf("legacy store read %d times", legacy.reads)
	}
	if legacy.deletes != 1 || legacy.value != "" {
		t.Fatalf("legacy entry was not removed after migration: %+v", legacy)
	}
	info, err := os.Stat(filepath.Join(dir, "subscriptions.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("subscription file permissions: %v, %v", info, err)
	}
	if err := store.Put(emptySubscriptionCatalog); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get()
	if err != nil || got != emptySubscriptionCatalog || legacy.reads != 1 {
		t.Fatalf("file should supersede Keychain: %q, %v, reads=%d", got, err, legacy.reads)
	}
}

func TestFileURLStoreRecordsEmptyMigration(t *testing.T) {
	legacy := &countingLegacyStore{}
	store := NewFileURLStore(t.TempDir(), legacy)
	for range 2 {
		got, err := store.Get()
		if err != nil || got != emptySubscriptionCatalog {
			t.Fatalf("empty catalog: %q, %v", got, err)
		}
	}
	if legacy.reads != 1 {
		t.Fatalf("missing legacy entry read %d times", legacy.reads)
	}
	if legacy.deletes != 0 {
		t.Fatalf("missing legacy entry was deleted: %+v", legacy)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(); err != nil || got != emptySubscriptionCatalog || legacy.reads != 1 {
		t.Fatalf("delete must keep empty marker: %q, %v, reads=%d", got, err, legacy.reads)
	}
}
