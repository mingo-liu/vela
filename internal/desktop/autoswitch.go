package desktop

import (
	"time"

	"github.com/mingo-liu/vela/internal/platform/macos"
	"github.com/mingo-liu/vela/internal/profile"
)

type nodeSelector interface {
	Select(group, option string) error
}

// AutoSwitcher selects the configured node whenever the default network changes
// between wired and wireless. Manual choices stay until the next change.
type AutoSwitcher struct {
	runner   nodeSelector
	store    *profile.Store
	detect   func() (iface, kind string, err error)
	onSwitch func()
	kick     chan struct{}

	iface   string
	config  profile.AutoSwitch
	applied bool
}

func NewAutoSwitcher(runner nodeSelector, store *profile.Store, detect func() (string, string, error), onSwitch func()) *AutoSwitcher {
	return &AutoSwitcher{runner: runner, store: store, detect: detect, onSwitch: onSwitch, kick: make(chan struct{}, 1)}
}

func (a *AutoSwitcher) Run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		a.check()
		select {
		case <-ticker.C:
		case <-a.kick:
		}
	}
}

// Kick re-evaluates immediately, e.g. after the settings change.
func (a *AutoSwitcher) Kick() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *AutoSwitcher) check() {
	settings, err := a.store.Settings()
	if err != nil {
		return
	}
	config := settings.AutoSwitch
	if !config.Enabled {
		a.iface, a.applied = "", false
		return
	}
	iface, kind, err := a.detect()
	if err != nil {
		return
	}
	if iface != a.iface || config != a.config {
		a.iface, a.config, a.applied = iface, config, false
	}
	if a.applied {
		return
	}
	target := map[string]string{macos.NetworkWired: config.Wired, macos.NetworkWireless: config.Wireless}[kind]
	if target != "" {
		// Failures are retried on the next tick, e.g. while the core is starting.
		if err := a.runner.Select(config.Group, target); err != nil {
			return
		}
		if a.onSwitch != nil {
			a.onSwitch()
		}
	}
	a.applied = true
}
