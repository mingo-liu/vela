package mihomo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mingo-liu/vela/internal/profile"
)

type RuleEditorSnapshot struct {
	Rules         []profile.CustomRule `json:"rules"`
	Targets       []string             `json:"targets"`
	OriginalRules []string             `json:"originalRules"`
	Active        bool                 `json:"active"`
}

// ruleProfile is called with mu held. Editing an inactive subscription never
// selects it or loads its rules into the current core.
func (r *Runner) ruleProfile(id string) ([]byte, bool, error) {
	if _, err := profile.CustomRulesPath(id); err != nil {
		return nil, false, err
	}
	activeID, err := r.store.ActiveRuleProfileID()
	if err != nil {
		return nil, false, err
	}
	if id == "" {
		if activeID != "" {
			return nil, false, errors.New("请先切换到本地配置")
		}
		raw, err := r.store.Load()
		if errors.Is(err, profile.ErrNoProfile) {
			return nil, true, nil
		}
		return raw, true, err
	}
	if r.subs == nil {
		return nil, false, profile.ErrNoSubscription
	}
	items, err := r.subs.List()
	if err != nil {
		return nil, false, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if activeID == id {
			raw, err := r.store.Load()
			return raw, true, err
		}
		raw, err := r.store.LoadSubscription(id)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, errors.New("请先更新此订阅以下载配置")
		}
		return raw, false, err
	}
	return nil, false, profile.ErrNoSubscription
}

func (r *Runner) RuleEditor(id string) (RuleEditorSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.MigrateCustomRules(); err != nil {
		return RuleEditorSnapshot{}, err
	}
	raw, active, err := r.ruleProfile(id)
	if err != nil {
		return RuleEditorSnapshot{}, err
	}
	rules, err := r.store.ProfileCustomRules(id)
	if err != nil {
		return RuleEditorSnapshot{}, err
	}
	result := RuleEditorSnapshot{Rules: rules, Targets: []string{"DIRECT", "REJECT"}, OriginalRules: []string{}, Active: active}
	if raw == nil {
		return result, nil
	}
	result.Targets, err = profile.RuleTargets(raw)
	if err != nil {
		return RuleEditorSnapshot{}, err
	}
	result.OriginalRules, err = profile.OriginalRules(raw)
	return result, err
}

func (r *Runner) CustomRules() ([]profile.CustomRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.CustomRules()
}

func (r *Runner) RuleTargets() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := r.store.Load()
	if errors.Is(err, profile.ErrNoProfile) {
		return []string{"DIRECT", "REJECT"}, nil
	}
	if err != nil {
		return nil, err
	}
	return profile.RuleTargets(raw)
}

// SaveCustomRules serializes with subscription changes. A failed hot reload
// restores both the independent rule file and the effective runtime profile.
func (r *Runner) SaveCustomRules(rules []profile.CustomRule) (State, error) {
	return r.saveProfileRules("", true, rules)
}

func (r *Runner) SaveProfileRules(id string, rules []profile.CustomRule) (State, error) {
	return r.saveProfileRules(id, false, rules)
}

func (r *Runner) saveProfileRules(id string, current bool, rules []profile.CustomRule) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.MigrateCustomRules(); err != nil {
		return r.state, err
	}
	if current {
		var err error
		id, err = r.store.ActiveRuleProfileID()
		if err != nil {
			return r.state, err
		}
	}
	raw, active, err := r.ruleProfile(id)
	if err != nil {
		return r.state, err
	}
	if active && r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	rules, err = profile.NormalizeCustomRules(rules)
	if err != nil {
		return r.state, err
	}
	previous, err := r.store.ProfileCustomRules(id)
	if err != nil {
		return r.state, err
	}
	var compiled, previousConfig []byte
	if raw != nil {
		// Offline validation uses a disposable controller secret and port.
		port := r.apiPort
		if port == 0 {
			port = 9090
		}
		secret := r.secret
		if secret == "" {
			secret = "validation-secret"
		}
		compiled, err = r.compileWithCustomRules(raw, port, secret, active && r.state.TunEnabled, rules)
		if err != nil {
			return r.state, err
		}
	}
	if active && r.cmd != nil {
		previousConfig, err = r.compileWithCustomRules(raw, r.apiPort, r.secret, r.state.TunEnabled, previous)
		if err != nil {
			return r.state, err
		}
	}
	if err := r.store.SaveProfileCustomRules(id, rules); err != nil {
		return r.state, err
	}
	if !active {
		return r.state, nil
	}
	if r.cmd != nil {
		r.configChanging = true
		r.controllerRevision++
		defer func() { r.configChanging = false }()
		path := filepath.Join(r.dataDir, "runtime.yaml")
		reload := false
		err = writePrivate(path, compiled)
		if err == nil {
			reload = true
			err = r.reloadConfig(compiled)
		}
		if err == nil {
			err = r.waitReady(r.done)
		}
		if err != nil {
			restoreErr := r.store.SaveProfileCustomRules(id, previous)
			fileErr := writePrivate(path, previousConfig)
			var reloadErr error
			if reload {
				reloadErr = r.reloadConfig(previousConfig)
			}
			if reloadErr == nil && reload {
				reloadErr = r.waitReady(r.done)
				if reloadErr == nil {
					reloadErr = r.restoreSelectedOptions()
				}
			}
			return r.state, errors.Join(fmt.Errorf("应用自定义规则失败: %w", err), restoreErr, fileErr, reloadErr)
		}
		if err := r.restoreSelectedOptions(); err != nil {
			_, _ = r.logs.Write([]byte(fmt.Sprintf("level=warning 保存的节点选择恢复失败: %v\n", err)))
		}
	}
	r.state.ConfigVersion++
	r.state.Error = ""
	r.emit()
	return r.state, nil
}
