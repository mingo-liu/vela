package profile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestScopedCustomRulesAndLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	subs := NewSubscriptions(store, NewFileURLStore(dir, nil))
	data := "proxy-groups: [{name: Auto, type: url-test, proxies: [DIRECT]}]\nrules: ['MATCH,DIRECT']\n"
	first := importRemovalSubscription(t, subs, "https://example.invalid/one", data)
	second := importRemovalSubscription(t, subs, "https://example.invalid/two", data)
	if err := subs.Select(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	rules := []CustomRule{{ID: "one", Type: "DOMAIN", Domain: "example.com", Target: "Auto", Enabled: true}}
	legacy, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeProfile(filepath.Join(dir, CustomRulesFile), string(legacy)); err != nil {
		t.Fatal(err)
	}
	// Switching without opening the rule editor still migrates to the old scope.
	if err := subs.Select(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.ProfileCustomRules(first); err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("migration = %v, %v", got, err)
	}
	if got, err := store.CustomRules(); err != nil || len(got) != 0 {
		t.Fatalf("rules leaked into identical subscription: %v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, CustomRulesFile+".migrated")); err != nil {
		t.Fatal("legacy backup missing:", err)
	}
	if _, err := os.Stat(filepath.Join(dir, CustomRulesFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file still active: %v", err)
	}
	other := []CustomRule{{ID: "two", Type: "DOMAIN-SUFFIX", Domain: "other.example.com", Target: "DIRECT", Enabled: true}}
	if err := store.SaveProfileCustomRules(second, other); err != nil {
		t.Fatal(err)
	}
	if got, err := NewStore(dir).CustomRules(); err != nil || !reflect.DeepEqual(got, other) {
		t.Fatalf("reopened scope = %v, %v", got, err)
	}
	if err := subs.Select(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if got, err := store.CustomRules(); err != nil || !reflect.DeepEqual(got, rules) {
		t.Fatalf("restored scope = %v, %v", got, err)
	}
	if err := subs.ImportLocal(data); err != nil {
		t.Fatal(err)
	}
	if got, err := store.CustomRules(); err != nil || len(got) != 0 {
		t.Fatalf("rules leaked into local config: %v, %v", got, err)
	}
	for _, id := range []string{"../outside", "/tmp/outside", "one/two", "one\\two", "one.rules.json"} {
		if err := store.SaveProfileCustomRules(id, rules); err == nil {
			t.Fatalf("accepted identifier %q", id)
		}
		if _, err := ActiveRuleProfileIDFromCatalog([]byte(`{"subscriptions":[{"id":"` + id + `","active":true}]}`)); err == nil {
			t.Fatalf("accepted unsafe catalog ID %q", id)
		}
	}
}

func TestLocalLegacyRulesMigration(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	legacy := `[{"id":"one","type":"DOMAIN","domain":"example.com","target":"DIRECT","enabled":true}]`
	if err := writeProfile(filepath.Join(dir, CustomRulesFile), legacy); err != nil {
		t.Fatal(err)
	}
	if got, err := store.CustomRules(); err != nil || len(got) != 1 {
		t.Fatalf("local migration = %v, %v", got, err)
	}
	if err := store.MigrateCustomRules(); err != nil {
		t.Fatal("migration is not idempotent:", err)
	}
}

func TestCustomRulesValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if rules, err := store.CustomRules(); err != nil || len(rules) != 0 {
		t.Fatalf("missing rules = %v, %v", rules, err)
	}
	rule := CustomRule{ID: "one", Type: "DOMAIN-SUFFIX", Domain: " EXAMPLE.COM. ", Target: "DIRECT", Enabled: true}
	if err := store.SaveCustomRules([]CustomRule{rule}); err != nil {
		t.Fatal(err)
	}
	rules, err := NewStore(dir).CustomRules()
	if err != nil || len(rules) != 1 || rules[0].Domain != "example.com" {
		t.Fatalf("round trip = %v, %v", rules, err)
	}
	info, err := os.Stat(filepath.Join(dir, LocalCustomRulesFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions = %v, %v", info, err)
	}
	for _, domain := range []string{"", "https://example.com", "example.com/a", "example.com:443", "*.example.com", "example.com,DIRECT", "-example.com", "example..com", "127.0.0.1", "::1", "例子.com", strings.Repeat("a", 64) + ".com", "example.com\nMATCH,REJECT"} {
		invalid := rule
		invalid.Domain = domain
		if err := store.SaveCustomRules([]CustomRule{invalid}); err == nil {
			t.Errorf("accepted domain %q", domain)
		}
	}
	for _, target := range []string{"", "GLOBAL", "DIRECT,no-resolve", "DIRECT\nMATCH,REJECT"} {
		invalid := rule
		invalid.Target = target
		if err := store.SaveCustomRules([]CustomRule{invalid}); err == nil {
			t.Errorf("accepted target %q", target)
		}
	}
	for _, invalid := range [][]CustomRule{{{ID: "one", Type: "MATCH", Domain: "example.com", Target: "DIRECT"}}, {rule, rule}, {{Type: "DOMAIN", Domain: "example.com", Target: "DIRECT"}}} {
		if err := store.SaveCustomRules(invalid); err == nil {
			t.Errorf("accepted invalid rules: %v", invalid)
		}
	}
	if got, _ := store.CustomRules(); !reflect.DeepEqual(got, rules) {
		t.Fatalf("invalid save changed stored rules: %v", got)
	}
	if err := store.SaveCustomRules(nil); err != nil {
		t.Fatal(err)
	}
	if got, err := store.CustomRules(); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("clear = %v, %v", got, err)
	}
	if _, err := DecodeCustomRules([]byte("[")); err == nil {
		t.Fatal("accepted corrupt JSON")
	}
	if _, err := DecodeCustomRules([]byte(strings.Repeat(" ", MaxCustomRulesSize+1))); err == nil {
		t.Fatal("accepted oversized file")
	}
}

func TestCustomRulePriorityAndUnavailableGroups(t *testing.T) {
	raw := []byte("proxy-groups:\n  - {name: Auto, type: url-test, proxies: [DIRECT]}\n  - {name: Fallback, type: fallback, proxies: [DIRECT]}\nrules: [MATCH,DIRECT]\n")
	// Use YAML sequence entries rather than a comma-separated flow sequence.
	raw = []byte(strings.ReplaceAll(string(raw), "[MATCH,DIRECT]", "['MATCH,DIRECT']"))
	targets, err := RuleTargets(raw)
	if err != nil || !reflect.DeepEqual(targets, []string{"DIRECT", "REJECT", "Auto", "Fallback"}) {
		t.Fatalf("targets = %v, %v", targets, err)
	}
	rules := []CustomRule{
		{ID: "specific", Type: "DOMAIN", Domain: "www.example.com", Target: "Auto", Enabled: true},
		{ID: "suffix", Type: "DOMAIN-SUFFIX", Domain: "example.com", Target: "REJECT", Enabled: true},
		{ID: "disabled", Type: "DOMAIN", Domain: "disabled.example.com", Target: "DIRECT"},
		{ID: "missing", Type: "DOMAIN", Domain: "missing.example.com", Target: "Missing", Enabled: true},
	}
	compiled, err := ApplyCustomRules(raw, rules)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		t.Fatal(err)
	}
	want := []string{"DOMAIN,www.example.com,Auto", "DOMAIN-SUFFIX,example.com,REJECT", "MATCH,DIRECT"}
	if !reflect.DeepEqual(config.Rules, want) {
		t.Fatalf("effective rules = %v", config.Rules)
	}
	rules[0], rules[1] = rules[1], rules[0]
	compiled, err = ApplyCustomRules(raw, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		t.Fatal(err)
	}
	if config.Rules[0] != want[1] {
		t.Fatalf("reordering did not change priority: %v", config.Rules)
	}
	for _, bad := range []string{"rules: ['MATCH,DIRECT']\n---\nrules: ['MATCH,REJECT']", "rules: ['MATCH,DIRECT']\nrules: []", "rules: bad"} {
		if _, err := ApplyCustomRules([]byte(bad), rules); err == nil {
			t.Fatalf("accepted malformed profile %q", bad)
		}
	}
}
