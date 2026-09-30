package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func (s *Store) selectionCatalog() (map[string]map[string]string, string, error) {
	profile, err := s.Load()
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(profile)
	legacyKey := hex.EncodeToString(sum[:])
	key := legacyKey
	if s.subscriptions != nil {
		items, err := s.subscriptions.List()
		if err != nil {
			return nil, "", err
		}
		for _, item := range items {
			if item.Active {
				key = "subscription:" + item.ID
				break
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), "selections.json"))
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]map[string]string), key, nil
	}
	if err != nil {
		return nil, "", err
	}
	var catalog map[string]map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil || catalog == nil {
		return nil, "", errors.New("无法读取已保存的节点选择")
	}
	// Migrate existing choices once. Removing the old content key prevents an
	// independent subscription with identical YAML from inheriting the choice.
	if key != legacyKey && catalog[key] == nil && catalog[legacyKey] != nil {
		catalog[key] = catalog[legacyKey]
		delete(catalog, legacyKey)
		if err := s.saveSelectionCatalog(catalog); err != nil {
			return nil, "", err
		}
	}
	return catalog, key, nil
}

func (s *Store) SelectedOptions() (map[string]string, error) {
	catalog, key, err := s.selectionCatalog()
	if err != nil {
		return nil, err
	}
	if catalog[key] == nil {
		return map[string]string{}, nil
	}
	return catalog[key], nil
}

func (s *Store) SelectOption(group, option string) error {
	groups, err := s.SelectorGroups()
	if err != nil {
		return err
	}
	for _, candidate := range groups {
		if candidate.Name == group {
			return s.SelectOptionInGroup(group, option, candidate.Options)
		}
	}
	return errors.New("策略组不可手动选择")
}

// SelectOptionInGroup saves a choice validated against the effective options
// returned by the core, including nodes supplied through proxy providers.
func (s *Store) SelectOptionInGroup(group, option string, options []string) error {
	valid := false
	for _, name := range options {
		valid = valid || name == option
	}
	if group == "" || option == "" || !valid {
		return errors.New("节点不在策略组中")
	}
	catalog, key, err := s.selectionCatalog()
	if err != nil {
		return err
	}
	if catalog[key] == nil {
		catalog[key] = make(map[string]string)
	}
	catalog[key][group] = option
	return s.saveSelectionCatalog(catalog)
}

// ReconcileSelectedOptions drops choices whose group or node no longer exists.
// Call only with effective core groups, after a successful configuration reload:
// offline YAML cannot enumerate nodes supplied through providers.
func (s *Store) ReconcileSelectedOptions(groups []SelectorGroup) error {
	catalog, key, err := s.selectionCatalog()
	if err != nil {
		return err
	}
	selected := catalog[key]
	changed := false
	for group, option := range selected {
		valid := false
		for _, candidate := range groups {
			if candidate.Name == group {
				for _, name := range candidate.Options {
					valid = valid || name == option
				}
			}
		}
		if !valid {
			delete(selected, group)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.saveSelectionCatalog(catalog)
}

func (s *Store) saveSelectionCatalog(catalog map[string]map[string]string) error {
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return writeProfile(filepath.Join(filepath.Dir(s.path), "selections.json"), string(data))
}
