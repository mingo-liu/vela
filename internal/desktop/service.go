package desktop

import (
	"github.com/mingo-liu/vela/internal/mihomo"
	"github.com/mingo-liu/vela/internal/profile"
)

// RuntimeService is the Wails boundary for the first local proxy slice.
// It exposes no controller secret or raw controller URL.
type RuntimeService struct {
	runner *mihomo.Runner
}

func NewRuntimeService(runner *mihomo.Runner) *RuntimeService {
	return &RuntimeService{runner: runner}
}

func (s *RuntimeService) State() mihomo.State { return s.runner.Snapshot() }

func (s *RuntimeService) ImportProfile(contents string) (mihomo.State, error) {
	return s.runner.Import(contents)
}

func (s *RuntimeService) ImportSubscription(address string) (mihomo.State, error) {
	return s.runner.ImportSubscription(address)
}

func (s *RuntimeService) Subscriptions() ([]profile.Subscription, error) {
	return s.runner.Subscriptions()
}

func (s *RuntimeService) UpdateSubscription(id string) (mihomo.State, error) {
	return s.runner.UpdateSubscription(id)
}

func (s *RuntimeService) SelectSubscription(id string) (mihomo.State, error) {
	return s.runner.SelectSubscription(id)
}

func (s *RuntimeService) SetSystemProxy(enabled bool) (mihomo.State, error) {
	return s.runner.SetSystemProxy(enabled)
}

func (s *RuntimeService) Groups() ([]mihomo.Group, error) { return s.runner.Groups() }

func (s *RuntimeService) NodeNames() ([]string, error) { return s.runner.NodeNames() }

func (s *RuntimeService) Select(group, option string) error {
	return s.runner.Select(group, option)
}
