package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

const CustomRulesFile = "custom-rules.json"
const LocalCustomRulesFile = "local-custom-rules.json"
const MaxCustomRulesSize = 256 << 10
const MaxCustomRules = 500

type CustomRule struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

// NormalizeCustomRules accepts host names, never URLs or rule expressions.
func NormalizeCustomRules(rules []CustomRule) ([]CustomRule, error) {
	if len(rules) > MaxCustomRules {
		return nil, errors.New("自定义规则不能超过 500 条")
	}
	result := make([]CustomRule, 0, len(rules))
	ids := map[string]bool{}
	for _, rule := range rules {
		if rule.ID == "" || len(rule.ID) > 64 || ids[rule.ID] || strings.IndexFunc(rule.ID, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' }) >= 0 {
			return nil, errors.New("自定义规则标识无效或重复")
		}
		ids[rule.ID] = true
		if rule.Type != "DOMAIN" && rule.Type != "DOMAIN-SUFFIX" {
			return nil, errors.New("自定义规则仅支持域名和域名后缀")
		}
		rule.Domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(rule.Domain), "."))
		if !validRuleDomain(rule.Domain) {
			return nil, errors.New("请输入有效域名，不含协议、端口、路径或通配符；国际化域名请使用 Punycode")
		}
		rule.Target = strings.TrimSpace(rule.Target)
		if !validRuleTarget(rule.Target) {
			return nil, errors.New("自定义规则目标无效")
		}
		result = append(result, rule)
	}
	return result, nil
}

func validRuleDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func validRuleTarget(target string) bool {
	return target != "" && target != "GLOBAL" && len(target) <= 256 && !strings.ContainsAny(target, ",\r\n") && strings.IndexFunc(target, unicode.IsControl) < 0
}

func DecodeCustomRules(data []byte) ([]CustomRule, error) {
	if len(data) > MaxCustomRulesSize {
		return nil, errors.New("自定义规则文件过大")
	}
	var rules []CustomRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("无法读取自定义规则: %w", err)
	}
	return NormalizeCustomRules(rules)
}

func (s *Store) CustomRules() ([]CustomRule, error) {
	if err := s.MigrateCustomRules(); err != nil {
		return nil, err
	}
	id, err := s.ActiveRuleProfileID()
	if err != nil {
		return nil, err
	}
	return s.ProfileCustomRules(id)
}

// CustomRulesPath accepts only a single subscription identifier, never a path.
func CustomRulesPath(id string) (string, error) {
	if id == "" {
		return LocalCustomRulesFile, nil
	}
	if len(id) > 128 || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) >= 0 {
		return "", errors.New("订阅标识无效")
	}
	return "subscriptions/" + id + ".rules.json", nil
}

// ActiveRuleProfileID also works for a store without a live subscription service.
func (s *Store) ActiveRuleProfileID() (string, error) {
	if s.subscriptions != nil {
		items, err := s.subscriptions.List()
		if err != nil {
			return "", err
		}
		return activeRuleProfileID(items)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), "subscriptions.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ActiveRuleProfileIDFromCatalog(data)
}

func ActiveRuleProfileIDFromCatalog(data []byte) (string, error) {
	if !strings.HasPrefix(string(data), "{") {
		// The legacy file catalog contains a single subscription URL.
		return subscriptionID(string(data)), nil
	}
	var catalog subscriptionCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return "", errors.New("无法读取已保存的订阅记录")
	}
	return activeRuleProfileID(catalog.Subscriptions)
}

func activeRuleProfileID(items []Subscription) (string, error) {
	id := ""
	for _, item := range items {
		if !item.Active {
			continue
		}
		if id != "" || item.ID == "" {
			return "", errors.New("当前订阅记录无效")
		}
		if _, err := CustomRulesPath(item.ID); err != nil {
			return "", err
		}
		id = item.ID
	}
	return id, nil
}

// MigrateCustomRules binds the former global list to the configuration selected
// before any subscription change. Keep a backup after the atomic scoped write.
func (s *Store) MigrateCustomRules() error {
	dir := filepath.Dir(s.path)
	legacy := filepath.Join(dir, CustomRulesFile)
	rules, err := readCustomRules(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	id, err := s.ActiveRuleProfileID()
	if err != nil {
		return err
	}
	relative, err := CustomRulesPath(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, relative)); errors.Is(err, os.ErrNotExist) {
		if err := s.SaveProfileCustomRules(id, rules); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return os.Rename(legacy, legacy+".migrated")
}

func (s *Store) ProfileCustomRules(id string) ([]CustomRule, error) {
	relative, err := CustomRulesPath(id)
	if err != nil {
		return nil, err
	}
	rules, err := readCustomRules(filepath.Join(filepath.Dir(s.path), relative))
	if errors.Is(err, os.ErrNotExist) {
		return []CustomRule{}, nil
	}
	return rules, err
}

func readCustomRules(path string) ([]CustomRule, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxCustomRulesSize+1))
	if err != nil {
		return nil, err
	}
	return DecodeCustomRules(data)
}

func (s *Store) SaveCustomRules(rules []CustomRule) error {
	if err := s.MigrateCustomRules(); err != nil {
		return err
	}
	id, err := s.ActiveRuleProfileID()
	if err != nil {
		return err
	}
	return s.SaveProfileCustomRules(id, rules)
}

func (s *Store) SaveProfileCustomRules(id string, rules []CustomRule) error {
	relative, err := CustomRulesPath(id)
	if err != nil {
		return err
	}
	rules, err = NormalizeCustomRules(rules)
	if err != nil {
		return err
	}
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if len(data) > MaxCustomRulesSize {
		return errors.New("自定义规则文件过大")
	}
	return writeProfile(filepath.Join(filepath.Dir(s.path), relative), string(data))
}

func OriginalRules(raw []byte) ([]string, error) {
	if _, err := Compile(raw, DefaultMixedPort, 9090, "validation-secret"); err != nil {
		return nil, err
	}
	var config struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if config.Rules == nil {
		config.Rules = []string{}
	}
	return config.Rules, nil
}

// RuleTargets includes all configured group types, including automatic groups.
func RuleTargets(raw []byte) ([]string, error) {
	var config struct {
		Groups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	groups := []string{}
	seen := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, group := range config.Groups {
		if validRuleTarget(group.Name) && !seen[group.Name] {
			groups = append(groups, group.Name)
			seen[group.Name] = true
		}
	}
	sort.Strings(groups)
	return append([]string{"DIRECT", "REJECT"}, groups...), nil
}

// ApplyCustomRules never edits the source profile. Missing group targets are
// retained in storage but omitted from this profile's effective rules.
func ApplyCustomRules(raw []byte, rules []CustomRule) ([]byte, error) {
	rules, err := NormalizeCustomRules(rules)
	if err != nil {
		return nil, err
	}
	if _, err := Compile(raw, DefaultMixedPort, 9090, "validation-secret"); err != nil {
		return nil, err
	}
	targets, err := RuleTargets(raw)
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, target := range targets {
		available[target] = true
	}
	custom := []*yaml.Node{}
	for _, rule := range rules {
		if rule.Enabled && available[rule.Target] {
			custom = append(custom, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: rule.Type + "," + rule.Domain + "," + rule.Target})
		}
	}
	if len(custom) == 0 {
		return raw, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	root := doc.Content[0]
	original := lookup(root, "rules")
	if original != nil && original.Tag != "!!null" {
		if original.Kind != yaml.SequenceNode {
			return nil, errors.New("配置规则必须是列表")
		}
		custom = append(custom, original.Content...)
	}
	remove(root, "rules")
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rules"}, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: custom})
	return yaml.Marshal(&doc)
}
