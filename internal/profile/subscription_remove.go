package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"go.yaml.in/yaml/v3"
)

// The runner must stop the core before deleting an active subscription. Keep
// backups until the catalog is saved so a failed deletion remains retryable.
func (s *Subscriptions) removeSubscriptionFiles(removed Subscription, remaining []Subscription) (func() error, error) {
	dir := filepath.Dir(s.profiles.path)
	cachePath := filepath.Join(dir, "subscriptions", removed.ID+".yaml")
	paths := map[string]bool{cachePath: true}
	if removed.Active {
		paths[s.profiles.path] = true
		paths[filepath.Join(dir, "runtime.yaml")] = true
	}
	backups := map[string][]byte{}
	for path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		backups[path] = data
	}

	// HTTP provider caches may be shared by subscriptions or a local profile.
	// Only remove Vela-owned downloads no remaining profile references.
	providers := map[string]bool{}
	for _, data := range backups {
		if err := collectProviderCaches(data, providers, false); err != nil {
			return nil, err
		}
	}
	retained := map[string]bool{}
	retainedPaths := []string{}
	if !removed.Active {
		retainedPaths = append(retainedPaths, s.profiles.path, filepath.Join(dir, "runtime.yaml"))
	}
	for _, item := range remaining {
		retainedPaths = append(retainedPaths, filepath.Join(dir, "subscriptions", item.ID+".yaml"))
	}
	for _, path := range retainedPaths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := collectProviderCaches(data, retained, true); err != nil {
			return nil, err
		}
	}
	for path := range providers {
		if retained[path] {
			continue
		}
		path = filepath.Join(dir, path)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		paths[path], backups[path] = true, data
	}

	selectionPath := filepath.Join(dir, "selections.json")
	selectionData, err := os.ReadFile(selectionPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var selectionUpdate []byte
	if err == nil {
		var selections map[string]map[string]string
		if err := json.Unmarshal(selectionData, &selections); err != nil || selections == nil {
			return nil, errors.New("无法读取已保存的节点选择")
		}
		before := len(selections)
		delete(selections, "subscription:"+removed.ID)
		if data, ok := backups[s.profiles.path]; removed.Active && ok {
			hash := sha256.Sum256(data)
			delete(selections, hex.EncodeToString(hash[:]))
		}
		if len(selections) != before {
			backups[selectionPath] = selectionData
			if len(selections) == 0 {
				paths[selectionPath] = true
			} else {
				selectionUpdate, err = json.Marshal(selections)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	changed := []string{}
	rollback := func() error {
		var err error
		for _, path := range changed {
			err = errors.Join(err, writeProfile(path, string(backups[path])))
		}
		return err
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, errors.Join(err, rollback())
		}
		changed = append(changed, path)
	}
	if selectionUpdate != nil {
		if err := writeProfile(selectionPath, string(selectionUpdate)); err != nil {
			return nil, errors.Join(err, rollback())
		}
		changed = append(changed, selectionPath)
	}
	return rollback, nil
}

func collectProviderCaches(data []byte, paths map[string]bool, retainLocal bool) error {
	type provider struct {
		Type string `yaml:"type"`
		URL  string `yaml:"url"`
		Path string `yaml:"path"`
	}
	var config struct {
		Proxies map[string]provider `yaml:"proxy-providers"`
		Rules   map[string]provider `yaml:"rule-providers"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return errors.New("无法读取配置中的提供器缓存")
	}
	for directory, providers := range map[string]map[string]provider{"providers": config.Proxies, "rules": config.Rules} {
		for name, item := range providers {
			if item.Type == "http" && item.URL != "" {
				paths[providerCachePath(directory, name, item.URL)] = true
			} else if retainLocal && item.Type == "file" && filepath.IsLocal(item.Path) {
				paths[filepath.ToSlash(filepath.Clean(item.Path))] = true
			}
		}
	}
	return nil
}
