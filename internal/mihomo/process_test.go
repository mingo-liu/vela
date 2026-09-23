package mihomo

import (
	"os"
	"testing"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestRunnerWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO to run the real core integration test")
	}
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := profile.NewStore(dir)
	if err := store.Import(`proxies: []
proxy-groups:
  - name: Choose
    type: select
    proxies: [DIRECT, REJECT]
rules:
  - MATCH,Choose
`); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, nil)
	t.Cleanup(runner.Close)
	state, err := runner.Start()
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
	groups, err := runner.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		t.Fatal("no selector groups from controller")
	}
	if err := runner.Select("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	state, err = runner.Stop()
	if err != nil || state.Status != "stopped" {
		t.Fatalf("stop: %+v, %v", state, err)
	}
}

type testURLStore struct{ value string }

func (s *testURLStore) Get() (string, error) {
	if s.value == "" {
		return "", profile.ErrNoSubscription
	}
	return s.value, nil
}
func (s *testURLStore) Put(value string) error { s.value = value; return nil }
func (s *testURLStore) Delete() error          { s.value = ""; return nil }
