package mihomo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

type CoreInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TrafficTotals struct {
	Upload    int64  `json:"uploadTotal"`
	Download  int64  `json:"downloadTotal"`
	Interface string `json:"interface"`
}

type SystemProxy interface {
	Enable(port int) error
	Disable() error
	Recover() error
	Active() (bool, error)
}

type Group struct {
	Name    string   `json:"name"`
	Current string   `json:"current"`
	Options []string `json:"options"`
}

const delayTestURL = "https://www.gstatic.com/generate_204"

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

// Logs returns the recent core output, including output from a stopped core.
func (r *Runner) Logs() string {
	r.mu.Lock()
	logs := r.logs
	r.mu.Unlock()
	return logs.String()
}

func (r *Runner) TrafficTotals() (TrafficTotals, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status != "running" {
		return TrafficTotals{}, nil
	}
	var totals TrafficTotals
	if err := r.request(http.MethodGet, "/connections", nil, &totals); err != nil {
		return TrafficTotals{}, err
	}
	return totals, nil
}

func (r *Runner) CoreInfo() (CoreInfo, error) {
	if r.binary == "" {
		return CoreInfo{}, errors.New("未找到 mihomo 内核")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, r.binary, "-v").CombinedOutput()
	if err != nil {
		return CoreInfo{}, fmt.Errorf("读取内核版本失败: %w", err)
	}
	return parseCoreInfo(string(output))
}

func parseCoreInfo(output string) (CoreInfo, error) {
	line := strings.SplitN(strings.TrimSpace(output), "\n", 2)[0]
	fields := strings.Fields(line)
	for index, field := range fields {
		if index > 0 && len(field) > 1 && field[0] == 'v' && field[1] >= '0' && field[1] <= '9' {
			return CoreInfo{Name: strings.Join(fields[:index], " "), Version: field}, nil
		}
	}
	return CoreInfo{}, errors.New("无法识别内核名称和版本号")
}

func (r *Runner) Import(data string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return r.state, errors.New("请先停止内核再导入新配置")
	}
	if err := r.subs.ImportLocal(data); err != nil {
		return r.state, err
	}
	r.state.HasProfile = true
	r.state.ConfigVersion++
	r.state.Error = ""
	r.emit()
	return r.state, nil
}

func (r *Runner) ImportSubscription(address string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	ctx, id, err := r.beginOperation("subscription", "", 1)
	if err != nil {
		return r.Snapshot(), err
	}
	defer r.finishOperation(id)
	download, err := r.subs.Download(ctx, address)
	if ctx.Err() != nil {
		err = operationError(ctx.Err())
	}
	if err != nil {
		return r.Snapshot(), err
	}
	if err := r.beginApply(ctx, id); err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.subs.ImportDownloaded(download); err != nil {
		return r.state, err
	}
	return r.state, nil
}

func (r *Runner) Subscriptions() ([]profile.Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.subs.List()
}

func (r *Runner) UpdateSubscription(id string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	address, err := r.subs.URL(id)
	if err != nil {
		return r.Snapshot(), err
	}
	ctx, taskID, err := r.beginOperation("subscription", id, 1)
	if err != nil {
		return r.Snapshot(), err
	}
	defer r.finishOperation(taskID)
	download, err := r.subs.Download(ctx, address)
	if ctx.Err() != nil {
		err = operationError(ctx.Err())
	}
	if err != nil {
		return r.Snapshot(), err
	}
	if err := r.beginApply(ctx, taskID); err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	items, err := r.subs.List()
	if err != nil {
		return r.state, err
	}
	for _, item := range items {
		if item.ID == id && !item.Active {
			return r.state, r.subs.RefreshInactiveDownloaded(id, download)
		}
	}
	if r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	if r.cmd == nil {
		err = r.subs.UpdateDownloaded(id, download)
	} else {
		var previousProfile, previousConfig []byte
		var previousSubscriptions []profile.Subscription
		previousProfile, err = r.store.Load()
		if err == nil {
			previousConfig, err = r.compile(previousProfile, r.apiPort, r.state.TunEnabled)
		}
		if err == nil {
			previousSubscriptions, err = r.subs.List()
		}
		if err == nil {
			err = r.subs.UpdateDownloadedWithApply(id, download, func() error {
				return r.reloadSelectedProfile(previousProfile, previousConfig, previousSubscriptions)
			})
		}
	}
	if err != nil {
		return r.state, err
	}
	r.state.HasProfile, r.state.Error = true, ""
	r.state.ConfigVersion++
	r.emit()
	return r.state, nil
}

func (r *Runner) refreshInactiveSubscription(id string) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	address, err := r.subs.URL(id)
	if err != nil {
		return err
	}
	ctx, taskID, err := r.beginOperation("subscription", id, 1)
	if err != nil {
		return err
	}
	defer r.finishOperation(taskID)
	download, err := r.subs.Download(ctx, address)
	if ctx.Err() != nil {
		err = operationError(ctx.Err())
	}
	if err != nil {
		return err
	}
	if err := r.beginApply(ctx, taskID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.subs.RefreshInactiveDownloaded(id, download)
}

func (r *Runner) RemoveSubscription(id string) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	items, err := r.subs.List()
	active, found := false, false
	for _, item := range items {
		if item.ID == id {
			active, found = item.Active, true
			break
		}
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	if !found {
		return profile.ErrNoSubscription
	}
	if active {
		if _, err := r.Stop(); err != nil {
			r.mu.Lock()
			r.emit()
			r.mu.Unlock()
			return err
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.subs.Remove(id); err != nil {
		r.state.Error = err.Error()
		r.emit()
		return err
	}
	if active {
		r.state.HasProfile = false
		r.state.ConfigVersion++
		r.apiPort, r.secret = 0, ""
	}
	r.state.Error = ""
	r.emit()
	return nil
}

func (r *Runner) ReplaceSubscriptionURL(id, address string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	ctx, taskID, err := r.beginOperation("subscription", id, 1)
	if err != nil {
		return r.Snapshot(), err
	}
	defer r.finishOperation(taskID)
	download, err := r.subs.Download(ctx, address)
	if ctx.Err() != nil {
		err = operationError(ctx.Err())
	}
	if err != nil {
		return r.Snapshot(), err
	}
	if err := r.beginApply(ctx, taskID); err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	items, err := r.subs.List()
	if err != nil {
		return r.state, err
	}
	active := false
	for _, item := range items {
		if item.ID == id {
			active = item.Active
			break
		}
	}
	var apply func() error
	if active && r.cmd != nil {
		previousProfile, err := r.store.Load()
		if err != nil {
			return r.state, err
		}
		previousConfig, err := r.compile(previousProfile, r.apiPort, r.state.TunEnabled)
		if err != nil {
			return r.state, err
		}
		apply = func() error {
			return r.reloadSelectedProfile(previousProfile, previousConfig, items)
		}
	}
	if err := r.subs.ReplaceURLDownloaded(id, download, apply); err != nil {
		return r.state, err
	}
	r.state.Error = ""
	r.state.ConfigVersion++
	r.emit()
	return r.state, nil
}

// UpdateDueSubscriptions is called by the app scheduler. It never changes the
// selected subscription when refreshing an inactive one.
func (r *Runner) UpdateDueSubscriptions(now time.Time, interval time.Duration) {
	if interval <= 0 || r.Operation().Active {
		return
	}
	items, err := r.Subscriptions()
	if err != nil {
		return
	}
	for _, item := range items {
		if item.UpdatedAt != nil && now.Sub(*item.UpdatedAt) < interval {
			continue
		}
		if item.LastAttemptAt != nil && now.Sub(*item.LastAttemptAt) < time.Hour {
			continue
		}
		var updateErr error
		if item.Active {
			_, updateErr = r.UpdateSubscription(item.ID)
		} else {
			updateErr = r.refreshInactiveSubscription(item.ID)
		}
		if updateErr != nil {
			if errors.Is(updateErr, ErrOperationBusy) || errors.Is(updateErr, ErrOperationCancelled) || errors.Is(updateErr, ErrRunnerClosed) {
				return
			}
			r.operationMu.Lock()
			r.mu.Lock()
			_ = r.subs.RecordUpdateError(item.ID, updateErr)
			r.mu.Unlock()
			r.operationMu.Unlock()
		}
	}
}

func (r *Runner) SelectSubscription(id string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	var download *profile.DownloadedSubscription
	if _, err := r.store.LoadSubscription(id); errors.Is(err, os.ErrNotExist) {
		address, err := r.subs.URL(id)
		if err != nil {
			return r.Snapshot(), err
		}
		ctx, taskID, err := r.beginOperation("subscription", id, 1)
		if err != nil {
			return r.Snapshot(), err
		}
		defer r.finishOperation(taskID)
		fetched, err := r.subs.Download(ctx, address)
		if ctx.Err() != nil {
			err = operationError(ctx.Err())
		}
		if err != nil {
			return r.Snapshot(), err
		}
		if err := r.beginApply(ctx, taskID); err != nil {
			return r.Snapshot(), err
		}
		download = &fetched
	} else if err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	running := r.cmd != nil
	var previousProfile []byte
	var previousConfig []byte
	var previousSubscriptions []profile.Subscription
	if running {
		var err error
		previousProfile, err = r.store.Load()
		if err != nil {
			return r.state, err
		}
		previousConfig, err = r.compile(previousProfile, r.apiPort, r.state.TunEnabled)
		if err != nil {
			return r.state, err
		}
		previousSubscriptions, err = r.subs.List()
		if err != nil {
			return r.state, err
		}
	}
	var selectErr error
	if download != nil {
		selectErr = r.subs.UpdateDownloaded(id, *download)
	} else {
		selectErr = r.subs.Select(context.Background(), id)
	}
	if selectErr != nil {
		return r.state, selectErr
	}
	if running {
		if err := r.reloadSelectedProfile(previousProfile, previousConfig, previousSubscriptions); err != nil {
			return r.state, err
		}
	}
	r.state.HasProfile, r.state.Error = true, ""
	r.state.ConfigVersion++
	r.emit()
	return r.state, nil
}

// reloadSelectedProfile keeps the current process and connection mode in place.
// If reloading fails, both the saved selection and the running config are restored.
func (r *Runner) reloadSelectedProfile(previousProfile, previousConfig []byte, previousSubscriptions []profile.Subscription) error {
	rollback := func(cause error, reload bool) error {
		var restoreErr error
		activeID := ""
		for _, subscription := range previousSubscriptions {
			if subscription.Active {
				activeID = subscription.ID
				break
			}
		}
		if activeID == "" {
			restoreErr = r.subs.ImportLocal(string(previousProfile))
		} else {
			restoreErr = r.subs.Select(context.Background(), activeID)
		}
		if !reload {
			return errors.Join(cause, restoreErr)
		}
		fileErr := writePrivate(filepath.Join(r.dataDir, "runtime.yaml"), previousConfig)
		select {
		case <-r.done:
			return errors.Join(cause, restoreErr, fileErr)
		default:
		}
		reloadErr := r.reloadConfig(previousConfig)
		return errors.Join(cause, restoreErr, fileErr, reloadErr)
	}

	raw, err := r.store.Load()
	if err != nil {
		return rollback(err, false)
	}
	if err := r.ensureGeoIPDatabase(raw); err != nil {
		return rollback(err, false)
	}
	compiled, err := r.compile(raw, r.apiPort, r.state.TunEnabled)
	if err != nil {
		return rollback(err, false)
	}
	if err := writePrivate(filepath.Join(r.dataDir, "runtime.yaml"), compiled); err != nil {
		return rollback(err, false)
	}
	if err := r.reloadConfig(compiled); err != nil {
		return rollback(fmt.Errorf("切换订阅时重载内核配置失败: %w", err), true)
	}
	if err := r.waitReady(r.done); err != nil {
		return rollback(fmt.Errorf("切换订阅后内核未就绪: %w", err), true)
	}
	if err := r.restoreSelectedOptions(); err != nil {
		_, _ = r.logs.Write([]byte(fmt.Sprintf("level=warning 保存的节点选择恢复失败: %v\n", err)))
	}
	return nil
}

func (r *Runner) reloadConfig(compiled []byte) error {
	body, err := json.Marshal(map[string]string{"payload": string(compiled)})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: r.client.Transport}
	return r.requestWithClient(client, http.MethodPut, "/configs?force=true", strings.NewReader(string(body)), nil)
}

func (r *Runner) Start() (State, error) {
	return r.start(false)
}

func (r *Runner) start(tun bool) (State, error) {
	return r.startWithContext(context.Background(), tun)
}

func (r *Runner) startWithContext(ctx context.Context, tun bool) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return r.state, nil
	}
	if r.binary == "" {
		return r.fail(errors.New("未找到 mihomo 内核；请先运行资源准备任务"))
	}
	if tun && r.tunLauncher == nil {
		return r.fail(errors.New("当前平台不支持 Tun 模式"))
	}
	raw, err := r.store.Load()
	if err != nil {
		return r.fail(err)
	}
	if err := os.MkdirAll(r.dataDir, 0700); err != nil {
		return r.fail(err)
	}
	if err := r.ensureGeoIPDatabase(raw); err != nil {
		return r.fail(err)
	}
	r.state.Status, r.state.Error = "starting", ""
	r.logs = &tailWriter{}
	r.emit()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return r.fail(err)
	}
	r.secret = hex.EncodeToString(secret)
	for attempt := 0; attempt < 3; attempt++ {
		port, err := freePort()
		if err != nil {
			return r.fail(err)
		}
		if port == r.state.Port {
			continue
		}
		r.apiPort = port
		compiled, err := r.compile(raw, port, tun)
		if err != nil {
			return r.fail(err)
		}
		configPath := filepath.Join(r.dataDir, "runtime.yaml")
		if err := writePrivate(configPath, compiled); err != nil {
			return r.fail(err)
		}
		// Starting mihomo parses the same config. For the system proxy we can
		// wait for readiness before changing macOS settings instead of launching
		// a second process solely to validate it. Keep the check for Tun so an
		// invalid config cannot trigger an unnecessary authorization prompt.
		if tun {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			check := exec.CommandContext(ctx, r.binary, "-t", "-d", r.dataDir, "-f", configPath)
			check.Env = cleanEnv()
			_, err = check.CombinedOutput()
			cancel()
			if err != nil {
				return r.fail(fmt.Errorf("内核配置校验失败（%v）；请检查节点与规则内容", err))
			}
		}
		var cmd *exec.Cmd
		if tun {
			stopPath := filepath.Join(r.dataDir, fmt.Sprintf("tun-stop-%s", r.secret))
			cmd, err = r.tunLauncher(configPath, stopPath)
			if err != nil {
				return r.fail(fmt.Errorf("Tun 服务启动失败: %w", err))
			}
			r.tunStopPath = stopPath
		} else {
			cmd = exec.Command(r.binary, "-d", r.dataDir, "-f", configPath)
			cmd.Env = cleanEnv()
		}
		cmd.Stdout, cmd.Stderr = r.logs, r.logs
		if err := cmd.Start(); err != nil {
			r.tunStopPath = ""
			return r.fail(err)
		}
		done := make(chan struct{})
		go func() {
			err := cmd.Wait()
			close(done)
			r.mu.Lock()
			if r.cmd == cmd {
				var proxyErr error
				if r.systemProxy != nil {
					proxyErr = r.systemProxy.Disable()
				}
				r.cmd = nil
				r.state.SystemProxyEnabled = proxyErr != nil
				r.state.TunEnabled = false
				r.tunStopPath = ""
				r.proxyGeneration++
				if r.state.Status != "stopping" {
					r.state.Status = "failed"
					r.state.Error = fmt.Sprintf("内核退出: %v", err)
					if proxyErr != nil {
						r.state.Error += fmt.Sprintf("；系统代理恢复失败: %v", proxyErr)
					}
					r.emit()
				}
			}
			r.mu.Unlock()
		}()
		r.cmd, r.done = cmd, done
		readyTimeout := 12 * time.Second
		if tun {
			readyTimeout = 2 * time.Minute
		}
		if err := r.waitReadyContext(ctx, done, readyTimeout); err != nil {
			if tun {
				_ = os.WriteFile(r.tunStopPath, nil, 0600)
			}
			_ = cmd.Process.Kill()
			<-done
			r.cmd = nil
			r.tunStopPath = ""
			detail := strings.TrimSpace(r.logs.String())
			if len(detail) > 500 {
				detail = detail[len(detail)-500:]
			}
			if detail != "" {
				return r.fail(fmt.Errorf("内核未就绪: %w；%s", err, detail))
			}
			return r.fail(fmt.Errorf("内核未就绪: %w", err))
		}
		if err := r.restoreSelectedOptions(); err != nil {
			_, _ = r.logs.Write([]byte(fmt.Sprintf("level=warning 保存的节点选择恢复失败: %v\n", err)))
		}
		r.state.Status = "running"
		r.state.TunEnabled = tun
		r.emit()
		return r.state, nil
	}
	return r.fail(errors.New("控制端口无法分配"))
}

func (r *Runner) compile(raw []byte, apiPort int, tun bool) ([]byte, error) {
	settings, err := r.store.Settings()
	if err != nil {
		return nil, err
	}
	return profile.CompileWithLogLevel(raw, r.state.Port, apiPort, r.secret, tun, r.state.RoutingMode, settings.LogLevel)
}

func (r *Runner) ensureGeoIPDatabase(profile []byte) error {
	upper := bytes.ToUpper(profile)
	if bytes.Contains(upper, []byte("GEOIP,")) {
		found := false
		for _, name := range []string{"Country.mmdb", "geoip.db", "geoip.metadb"} {
			if _, err := os.Stat(filepath.Join(r.dataDir, name)); err == nil {
				found = true
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if !found {
			if err := r.copyGeoResource("Country.mmdb", "缺少 GeoIP 数据库，请重新打包应用"); err != nil {
				return err
			}
		}
	}
	if bytes.Contains(upper, []byte("GEOSITE,")) {
		if _, err := os.Stat(filepath.Join(r.dataDir, "geosite.dat")); errors.Is(err, os.ErrNotExist) {
			return r.copyGeoResource("geosite.dat", "缺少 GeoSite 数据库，请重新打包应用")
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) copyGeoResource(name, missing string) error {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(r.binary), name))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New(missing)
	}
	if err != nil {
		return fmt.Errorf("读取 %s 数据库失败: %w", name, err)
	}
	return writePrivate(filepath.Join(r.dataDir, name), data)
}

func (r *Runner) Stop() (State, error) {
	r.mu.Lock()
	if r.systemProxy != nil {
		if err := r.systemProxy.Disable(); err != nil {
			r.state.Error = fmt.Sprintf("系统代理恢复失败，内核仍在运行: %v", err)
			s := r.state
			r.mu.Unlock()
			return s, err
		}
		r.state.SystemProxyEnabled = false
		r.proxyGeneration++
	}
	if r.cmd == nil {
		r.state.Status, r.state.Error = "stopped", ""
		r.state.TunEnabled = false
		r.tunStopPath = ""
		r.emit()
		s := r.state
		r.mu.Unlock()
		return s, nil
	}
	cmd, done := r.cmd, r.done
	tunStopPath := r.tunStopPath
	r.state.Status = "stopping"
	r.emit()
	r.mu.Unlock()
	if tunStopPath != "" {
		if err := os.WriteFile(tunStopPath, nil, 0600); err != nil {
			return r.stopFailure(fmt.Errorf("停止 Tun 内核失败: %w", err))
		}
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		if tunStopPath != "" {
			return r.stopFailure(errors.New("Tun 内核未能及时停止"))
		}
		_ = cmd.Process.Kill()
		<-done
	}
	r.mu.Lock()
	if r.cmd == cmd {
		r.cmd = nil
	}
	r.state.Status, r.state.Error = "stopped", ""
	r.state.TunEnabled = false
	r.tunStopPath = ""
	r.emit()
	s := r.state
	r.mu.Unlock()
	return s, nil
}

func (r *Runner) stopFailure(err error) (State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Status = "running"
	r.state.Error = err.Error()
	r.emit()
	return r.state, err
}

func (r *Runner) SetSystemProxy(enabled bool) (State, error) {
	if enabled {
		return r.setMode("system")
	}
	return r.setMode("off")
}

func (r *Runner) SetTun(enabled bool) (State, error) {
	if enabled {
		return r.setMode("tun")
	}
	return r.setMode("off")
}

func (r *Runner) SetRoutingMode(mode string) (State, error) {
	if !profile.ValidRoutingMode(mode) {
		return r.Snapshot(), errors.New("无效的代理模式")
	}
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil && r.state.Status != "running" {
		return r.state, errors.New("内核正在切换状态，请稍后重试")
	}
	previous := r.state.RoutingMode
	if mode == previous {
		return r.state, nil
	}
	if r.state.Status == "running" {
		if err := r.patchRoutingMode(mode); err != nil {
			return r.state, fmt.Errorf("切换代理模式失败: %w", err)
		}
	}
	if err := r.store.SaveRoutingMode(mode); err != nil {
		if r.state.Status == "running" {
			return r.state, errors.Join(fmt.Errorf("保存代理模式失败: %w", err), r.patchRoutingMode(previous))
		}
		return r.state, fmt.Errorf("保存代理模式失败: %w", err)
	}
	r.state.RoutingMode = mode
	r.state.Error = ""
	r.emit()
	return r.state, nil
}

func (r *Runner) SetLogLevel(level string) (profile.Settings, error) {
	if !profile.ValidLogLevel(level) {
		return profile.Settings{}, errors.New("无效的日志级别")
	}
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.LogLevel = level
		return nil
	})
}

// SetMixedPort restarts an active connection so the listener and system proxy
// always use the same port. A failed restart restores the saved port and mode.
func (r *Runner) SetMixedPort(port int) (State, error) {
	if !profile.ValidMixedPort(port) {
		return r.Snapshot(), errors.New("本地代理端口必须在 1024–65535 之间")
	}
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	if r.cmd != nil && r.state.Status != "running" {
		state := r.state
		r.mu.Unlock()
		return state, errors.New("内核正在切换状态，请稍后重试")
	}
	previousPort := r.state.Port
	previousMode := "off"
	if r.state.SystemProxyEnabled {
		previousMode = "system"
	} else if r.state.TunEnabled {
		previousMode = "tun"
	}
	wasRunning := r.cmd != nil
	needsRestart := wasRunning || previousMode != "off"
	r.mu.Unlock()
	if port == previousPort {
		return r.Snapshot(), nil
	}
	if err := checkMixedPortAvailable(port); err != nil {
		return r.Snapshot(), err
	}
	before, err := r.store.Settings()
	if err != nil {
		return r.Snapshot(), err
	}
	restart := func() (State, error) {
		if previousMode != "off" {
			return r.activate(previousMode)
		}
		if wasRunning {
			return r.Start()
		}
		return r.Snapshot(), nil
	}
	if needsRestart {
		if _, err := r.Stop(); err != nil {
			return r.Snapshot(), err
		}
	}
	if _, err := r.store.UpdateSettings(func(settings *profile.Settings) error {
		settings.MixedPort = port
		return nil
	}); err != nil {
		_, restoreErr := restart()
		return r.Snapshot(), errors.Join(err, restoreErr)
	}
	r.setMixedPortState(port)
	if !needsRestart {
		return r.Snapshot(), nil
	}
	if _, err := restart(); err != nil {
		if _, stopErr := r.Stop(); stopErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("停止新端口连接失败: %w", stopErr))
		}
		if _, saveErr := r.store.UpdateSettings(func(settings *profile.Settings) error {
			settings.MixedPort = before.MixedPort
			return nil
		}); saveErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("恢复原端口设置失败: %w", saveErr))
		}
		r.setMixedPortState(previousPort)
		_, restoreErr := restart()
		return r.Snapshot(), errors.Join(fmt.Errorf("切换本地代理端口失败: %w", err), restoreErr)
	}
	return r.Snapshot(), nil
}

func (r *Runner) setMixedPortState(port int) {
	r.mu.Lock()
	r.state.Port = port
	r.state.Error = ""
	if r.cmd == nil {
		r.state.Status = "stopped"
	}
	r.emit()
	r.mu.Unlock()
}

func checkMixedPortAvailable(port int) error {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	tcp, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("本地代理端口 %d 不可用: %w", port, err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", address)
	if err != nil {
		return fmt.Errorf("本地代理端口 %d 不可用: %w", port, err)
	}
	return udp.Close()
}

// patchRoutingMode changes only routing. Connection mode and managed listeners stay active.
func (r *Runner) patchRoutingMode(mode string) error {
	body, err := json.Marshal(map[string]string{"mode": mode})
	if err != nil {
		return err
	}
	return r.request(http.MethodPatch, "/configs", strings.NewReader(string(body)), nil)
}

func (r *Runner) setMode(mode string) (State, error) {
	r.CancelOperation(0)
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	previous := "off"
	if r.state.SystemProxyEnabled {
		previous = "system"
	}
	if r.state.TunEnabled {
		previous = "tun"
	}
	r.mu.Unlock()
	if mode == previous {
		return r.Snapshot(), nil
	}
	if _, err := r.Stop(); err != nil {
		return r.Snapshot(), err
	}
	if mode == "off" {
		return r.Snapshot(), nil
	}
	state, err := r.activate(mode)
	if err == nil {
		return state, nil
	}
	if previous != "off" {
		if _, restoreErr := r.activate(previous); restoreErr != nil {
			return r.Snapshot(), errors.Join(err, fmt.Errorf("恢复原连接模式失败: %w", restoreErr))
		}
	}
	return r.Snapshot(), err
}

func (r *Runner) activate(mode string) (State, error) {
	if mode == "system" && r.systemProxy == nil {
		return r.Snapshot(), errors.New("当前平台不支持系统代理")
	}
	if _, err := r.start(mode == "tun"); err != nil {
		return r.Snapshot(), err
	}
	if mode == "tun" {
		return r.Snapshot(), nil
	}
	r.mu.Lock()
	if err := r.systemProxy.Enable(r.state.Port); err != nil {
		r.mu.Unlock()
		state, stopErr := r.Stop()
		return state, errors.Join(err, stopErr)
	}
	r.state.SystemProxyEnabled = true
	r.proxyGeneration++
	go r.monitorSystemProxy(r.proxyGeneration)
	r.state.Error = ""
	r.emit()
	state := r.state
	r.mu.Unlock()
	return state, nil
}

func (r *Runner) monitorSystemProxy(generation uint64) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		r.operationMu.Lock()
		r.mu.Lock()
		if !r.state.SystemProxyEnabled || r.proxyGeneration != generation {
			r.mu.Unlock()
			r.operationMu.Unlock()
			return
		}
		active, err := r.systemProxy.Active()
		if err == nil && !active {
			r.mu.Unlock()
			_, stopErr := r.Stop()
			r.mu.Lock()
			r.state.Error = "系统代理已被其他应用改写"
			if stopErr != nil {
				r.state.Error += fmt.Sprintf("；关闭失败: %v", stopErr)
			}
			r.emit()
			r.mu.Unlock()
			r.operationMu.Unlock()
			return
		}
		r.mu.Unlock()
		r.operationMu.Unlock()
	}
}

func (r *Runner) Groups() ([]Group, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status != "running" {
		selectors, err := r.store.SelectorGroups()
		if err != nil {
			return nil, err
		}
		selected, err := r.store.SelectedOptions()
		if err != nil {
			return nil, err
		}
		groups := make([]Group, 0, len(selectors))
		for _, selector := range selectors {
			current := ""
			if len(selector.Options) > 0 {
				current = selector.Options[0]
			}
			for _, option := range selector.Options {
				if option == selected[selector.Name] {
					current = option
					break
				}
			}
			groups = append(groups, Group{Name: selector.Name, Current: current, Options: selector.Options})
		}
		sortGroups(groups)
		return groups, nil
	}
	return r.controllerGroups()
}

// controllerGroups is called with the runner mutex held.
func (r *Runner) controllerGroups() ([]Group, error) {
	var response struct {
		Proxies map[string]struct {
			Type string   `json:"type"`
			Now  string   `json:"now"`
			All  []string `json:"all"`
		} `json:"proxies"`
	}
	if err := r.request(http.MethodGet, "/proxies", nil, &response); err != nil {
		return nil, err
	}
	groups := make([]Group, 0)
	for name, proxy := range response.Proxies {
		if proxy.Type == "Selector" {
			groups = append(groups, Group{Name: name, Current: proxy.Now, Options: proxy.All})
		}
	}
	sortGroups(groups)
	return groups, nil
}

func sortGroups(groups []Group) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Name == "GLOBAL" {
			return false
		}
		if groups[j].Name == "GLOBAL" {
			return true
		}
		return groups[i].Name < groups[j].Name
	})
}

func (r *Runner) NodeNames() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.NodeNames()
}

func (r *Runner) TestGroupDelay(group string) (delays map[string]int, err error) {
	r.operationMu.Lock()
	locked := true
	var temporary *exec.Cmd
	var taskID uint64
	defer func() {
		if temporary != nil {
			if !locked {
				r.operationMu.Lock()
				locked = true
			}
			r.mu.Lock()
			stop := r.cmd == temporary && !r.state.SystemProxyEnabled && !r.state.TunEnabled
			r.mu.Unlock()
			if stop {
				_, stopErr := r.Stop()
				err = errors.Join(err, stopErr)
			}
		}
		if locked {
			r.operationMu.Unlock()
		}
		if taskID != 0 {
			r.finishOperation(taskID)
		}
	}()
	ctx, id, err := r.beginOperation("delay", group, 0)
	if err != nil {
		return nil, err
	}
	taskID = id
	r.mu.Lock()
	running := r.state.Status == "running"
	r.mu.Unlock()
	if !running {
		if _, err := r.startWithContext(ctx, false); err != nil {
			if ctx.Err() != nil {
				return nil, operationError(ctx.Err())
			}
			return nil, err
		}
		r.mu.Lock()
		temporary = r.cmd
		r.mu.Unlock()
	}
	r.mu.Lock()
	apiPort, secret, transport := r.apiPort, r.secret, r.client.Transport
	groups, err := r.controllerGroups()
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var options []string
	found := false
	for _, candidate := range groups {
		if candidate.Name == group {
			options, found = candidate.Options, true
			break
		}
	}
	if !found {
		return nil, errors.New("策略组不存在")
	}
	r.taskMu.Lock()
	r.taskProgress.Total = len(options)
	r.emitProgress()
	r.taskMu.Unlock()
	// Connection changes may cancel the probes and take over a temporary core.
	// No state or operation lock is held while waiting on network requests.
	r.operationMu.Unlock()
	locked = false
	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	delays = make(map[string]int)
	var resultsMu sync.Mutex
	var workers sync.WaitGroup
	jobs := make(chan string)
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for option := range jobs {
				if ctx.Err() != nil {
					return
				}
				delay, probeErr := testProxyDelay(ctx, client, apiPort, secret, option)
				if probeErr == nil && delay > 0 {
					resultsMu.Lock()
					delays[option] = delay
					resultsMu.Unlock()
				}
				r.advanceOperation(taskID, len(options))
			}
		}()
	}
queue:
	for _, option := range options {
		select {
		case <-ctx.Done():
			break queue
		case jobs <- option:
		}
	}
	close(jobs)
	workers.Wait()
	if ctx.Err() != nil {
		return delays, operationError(ctx.Err())
	}
	return delays, nil
}

func testProxyDelay(ctx context.Context, client *http.Client, apiPort int, secret, option string) (int, error) {
	query := url.Values{"url": {delayTestURL}, "timeout": {"5000"}}
	address := fmt.Sprintf("http://127.0.0.1:%d/proxies/%s/delay?%s", apiPort, url.PathEscape(option), query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("内核 API 返回 HTTP %d", response.StatusCode)
	}
	var result struct {
		Delay int `json:"delay"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Delay, nil
}

func (r *Runner) Select(group, option string) error {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status != "running" {
		return r.store.SelectOption(group, option)
	}
	candidate, err := r.controllerSelection(group, option)
	if err != nil {
		return err
	}
	if err := r.writeControllerSelection(group, option); err != nil {
		return err
	}
	if err := r.store.SelectOptionInGroup(group, option, candidate.Options); err != nil {
		if candidate.Current != "" {
			return errors.Join(err, r.writeControllerSelection(group, candidate.Current))
		}
		return err
	}
	return nil
}

// These controller helpers are called with the runner mutex held.
func (r *Runner) controllerSelection(group, option string) (Group, error) {
	groups, err := r.controllerGroups()
	if err != nil {
		return Group{}, err
	}
	for _, candidate := range groups {
		if candidate.Name == group {
			for _, name := range candidate.Options {
				if name == option {
					return candidate, nil
				}
			}
			return Group{}, errors.New("节点不在策略组中")
		}
	}
	return Group{}, errors.New("策略组不可手动选择")
}

func (r *Runner) restoreSelectedOptions() error {
	groups, err := r.controllerGroups()
	if err != nil {
		return err
	}
	selected, err := r.store.SelectedOptions()
	if err != nil {
		return err
	}
	effective := make([]profile.SelectorGroup, 0, len(groups))
	for _, group := range groups {
		effective = append(effective, profile.SelectorGroup{Name: group.Name, Options: group.Options})
		for _, option := range group.Options {
			if option == selected[group.Name] {
				if err := r.writeControllerSelection(group.Name, option); err != nil {
					return err
				}
				break
			}
		}
	}
	return r.store.ReconcileSelectedOptions(effective)
}

func (r *Runner) writeControllerSelection(group, option string) error {
	body, err := json.Marshal(map[string]string{"name": option})
	if err != nil {
		return err
	}
	return r.request(http.MethodPut, "/proxies/"+url.PathEscape(group), strings.NewReader(string(body)), nil)
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

func (r *Runner) waitReady(done <-chan struct{}) error {
	return r.waitReadyUntil(done, 12*time.Second)
}

func (r *Runner) waitReadyUntil(done <-chan struct{}, timeout time.Duration) error {
	return r.waitReadyContext(context.Background(), done, timeout)
}

func (r *Runner) waitReadyContext(ctx context.Context, done <-chan struct{}, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		if r.request(http.MethodGet, "/version", nil, nil) == nil {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", r.state.Port), time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
		}
		select {
		case <-done:
			return errors.New("进程提前退出")
		case <-ctx.Done():
			return operationError(ctx.Err())
		case <-deadline.C:
			return errors.New("等待控制接口与代理端口超时")
		case <-tick.C:
		}
	}
}

func (r *Runner) request(method, path string, body *strings.Reader, out any) error {
	return r.requestWithClient(r.client, method, path, body, out)
}

func (r *Runner) requestWithClient(client *http.Client, method, path string, body *strings.Reader, out any) error {
	var reader *strings.Reader
	if body != nil {
		reader = body
	} else {
		reader = strings.NewReader("")
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", r.apiPort, path), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("内核 API 返回 HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func cleanEnv() []string {
	result := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "CLASH_") && key != "SAFE_PATHS" && key != "SKIP_SAFE_PATH_CHECK" {
			result = append(result, entry)
		}
	}
	return result
}

func writePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".runtime-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type tailWriter struct {
	mu   sync.Mutex
	data []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data = append(w.data, p...)
	if len(w.data) > 128<<10 {
		w.data = append([]byte(nil), w.data[len(w.data)-(128<<10):]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.data)
}
