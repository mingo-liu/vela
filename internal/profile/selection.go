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
	key := hex.EncodeToString(sum[:])
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
	valid := false
	for _, candidate := range groups {
		if candidate.Name != group {
			continue
		}
		for _, name := range candidate.Options {
			valid = valid || name == option
		}
	}
	if !valid {
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
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return writeProfile(filepath.Join(filepath.Dir(s.path), "selections.json"), string(data))
}
