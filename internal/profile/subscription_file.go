package profile

import (
	"errors"
	"os"
	"path/filepath"
)

const emptySubscriptionCatalog = `{"subscriptions":[]}`

// FileURLStore keeps subscription records in a private file. A legacy store is
// read only when the file does not exist, so an existing Keychain entry needs
// at most one migration attempt.
type FileURLStore struct {
	path   string
	legacy URLStore
}

func NewFileURLStore(dataDir string, legacy URLStore) *FileURLStore {
	return &FileURLStore{path: filepath.Join(dataDir, "subscriptions.json"), legacy: legacy}
}

func (s *FileURLStore) Get() (string, error) {
	data, err := os.ReadFile(s.path)
	if err == nil {
		return string(data), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := emptySubscriptionCatalog
	migratedLegacy := false
	if s.legacy != nil {
		value, err := s.legacy.Get()
		if err != nil && !errors.Is(err, ErrNoSubscription) {
			return "", err
		}
		if err == nil {
			raw = value
			migratedLegacy = true
		}
	}
	if err := s.Put(raw); err != nil {
		return "", err
	}
	if migratedLegacy {
		// The file is authoritative now. Failure to remove the old copy must
		// not hide the successfully migrated subscriptions.
		_ = s.legacy.Delete()
	}
	return raw, nil
}

func (s *FileURLStore) Put(value string) error {
	return writeProfile(s.path, value)
}

func (s *FileURLStore) Delete() error {
	return s.Put(emptySubscriptionCatalog)
}
