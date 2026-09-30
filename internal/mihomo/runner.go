package mihomo

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

type State struct {
	Status             string `json:"status"`
	Port               int    `json:"port"`
	HasProfile         bool   `json:"hasProfile"`
	Error              string `json:"error"`
	SystemProxyEnabled bool   `json:"systemProxyEnabled"`
	TunEnabled         bool   `json:"tunEnabled"`
	TunSupported       bool   `json:"tunSupported"`
	RoutingMode        string `json:"routingMode"`
	ConfigVersion      uint64 `json:"configVersion"`
}

type Runner struct {
	operationMu     sync.Mutex
	taskMu          sync.Mutex
	closed          bool
	taskProgress    OperationProgress
	taskCancel      context.CancelFunc
	onProgress      func(OperationProgress)
	mu              sync.Mutex
	store           *profile.Store
	subs            *profile.Subscriptions
	dataDir         string
	binary          string
	state           State
	cmd             *exec.Cmd
	done            chan struct{}
	apiPort         int
	secret          string
	client          *http.Client
	logs            *tailWriter
	onChange        func(State)
	systemProxy     SystemProxy
	tunLauncher     func(configPath, stopPath string) (*exec.Cmd, error)
	tunStopPath     string
	proxyGeneration uint64
}

func NewRunner(store *profile.Store, subs *profile.Subscriptions, dataDir, binary string, port int, systemProxy SystemProxy, onChange func(State)) *Runner {
	transport := &http.Transport{Proxy: nil}
	r := &Runner{store: store, subs: subs, dataDir: dataDir, binary: binary, state: State{Status: "stopped", Port: port, HasProfile: store.Exists(), RoutingMode: profile.RoutingRule}, client: &http.Client{Timeout: 2 * time.Second, Transport: transport}, logs: &tailWriter{}, onChange: onChange, systemProxy: systemProxy}
	if mode, err := store.RoutingMode(); err != nil {
		r.state.Error = err.Error()
	} else {
		r.state.RoutingMode = mode
	}
	if systemProxy != nil {
		if err := systemProxy.Recover(); err != nil {
			r.state.Error = fmt.Sprintf("上次系统代理恢复失败: %v", err)
		}
	}
	return r
}

func (r *Runner) SetTunLauncher(launcher func(configPath, stopPath string) (*exec.Cmd, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tunLauncher = launcher
	r.state.TunSupported = launcher != nil
}

func (r *Runner) Snapshot() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.HasProfile = r.store.Exists()
	return s
}

func (r *Runner) Close() {
	r.taskMu.Lock()
	r.closed = true
	r.taskMu.Unlock()
	r.CancelOperation(0)
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	_, _ = r.Stop()
}

func (r *Runner) fail(err error) (State, error) {
	r.state.Status, r.state.Error = "failed", err.Error()
	r.emit()
	return r.state, err
}

// emit is called with mu held. The callback must return without calling Runner.
func (r *Runner) emit() {
	if r.onChange != nil {
		r.onChange(r.state)
	}
}
