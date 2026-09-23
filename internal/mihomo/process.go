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
	proxyGeneration uint64
}

func NewRunner(store *profile.Store, subs *profile.Subscriptions, dataDir, binary string, port int, systemProxy SystemProxy, onChange func(State)) *Runner {
	transport := &http.Transport{Proxy: nil}
	r := &Runner{store: store, subs: subs, dataDir: dataDir, binary: binary, state: State{Status: "stopped", Port: port, HasProfile: store.Exists()}, client: &http.Client{Timeout: 2 * time.Second, Transport: transport}, logs: &tailWriter{}, onChange: onChange, systemProxy: systemProxy}
	if systemProxy != nil {
		if err := systemProxy.Recover(); err != nil {
			r.state.Error = fmt.Sprintf("上次系统代理恢复失败: %v", err)
		}
	}
	return r
}

func (r *Runner) Snapshot() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.state
	s.HasProfile = r.store.Exists()
	return s
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
	r.state.Error = ""
	r.emit()
	return r.state, nil
}

func (r *Runner) ImportSubscription(address string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.subs.Import(context.Background(), address); err != nil {
		return r.state, err
	}
	return r.state, nil
}

func (r *Runner) Subscriptions() ([]profile.Subscription, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	return r.subs.List()
}

func (r *Runner) UpdateSubscription(id string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return r.state, errors.New("请先停止内核再更新订阅")
	}
	if err := r.subs.Update(context.Background(), id); err != nil {
		return r.state, err
	}
	r.state.HasProfile, r.state.Error = true, ""
	r.emit()
	return r.state, nil
}

func (r *Runner) SelectSubscription(id string) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
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
		previousConfig, err = profile.Compile(previousProfile, r.state.Port, r.apiPort, r.secret)
		if err != nil {
			return r.state, err
		}
		previousSubscriptions, err = r.subs.List()
		if err != nil {
			return r.state, err
		}
	}
	if err := r.subs.Select(context.Background(), id); err != nil {
		return r.state, err
	}
	if running {
		if err := r.reloadSelectedProfile(previousProfile, previousConfig, previousSubscriptions); err != nil {
			return r.state, err
		}
	}
	r.state.HasProfile, r.state.Error = true, ""
	r.emit()
	return r.state, nil
}

// reloadSelectedProfile keeps the current process and system proxy in place.
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
	compiled, err := profile.Compile(raw, r.state.Port, r.apiPort, r.secret)
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
	if selected, err := r.store.SelectedOptions(); err == nil {
		for group, option := range selected {
			_ = r.selectController(group, option)
		}
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return r.state, nil
	}
	if r.binary == "" {
		return r.fail(errors.New("未找到 mihomo 内核；请先运行资源准备任务"))
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
		r.apiPort = port
		compiled, err := profile.Compile(raw, r.state.Port, port, r.secret)
		if err != nil {
			return r.fail(err)
		}
		configPath := filepath.Join(r.dataDir, "runtime.yaml")
		if err := writePrivate(configPath, compiled); err != nil {
			return r.fail(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		check := exec.CommandContext(ctx, r.binary, "-t", "-d", r.dataDir, "-f", configPath)
		check.Env = cleanEnv()
		_, err = check.CombinedOutput()
		cancel()
		if err != nil {
			return r.fail(fmt.Errorf("内核配置校验失败（%v）；请检查节点与规则内容", err))
		}
		cmd := exec.Command(r.binary, "-d", r.dataDir, "-f", configPath)
		cmd.Env = cleanEnv()
		cmd.Stdout, cmd.Stderr = r.logs, r.logs
		if err := cmd.Start(); err != nil {
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
		if err := r.waitReady(done); err != nil {
			_ = cmd.Process.Kill()
			<-done
			r.cmd = nil
			return r.fail(fmt.Errorf("内核未就绪: %w", err))
		}
		if selected, err := r.store.SelectedOptions(); err == nil {
			for group, option := range selected {
				_ = r.selectController(group, option)
			}
		}
		r.state.Status = "running"
		r.emit()
		return r.state, nil
	}
	return r.fail(errors.New("控制端口无法分配"))
}

func (r *Runner) ensureGeoIPDatabase(profile []byte) error {
	if !bytes.Contains(bytes.ToUpper(profile), []byte("GEOIP,")) {
		return nil
	}
	for _, name := range []string{"Country.mmdb", "geoip.db", "geoip.metadb"} {
		if _, err := os.Stat(filepath.Join(r.dataDir, name)); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(r.binary), "Country.mmdb"))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("缺少 GeoIP 数据库，请重新打包应用")
	}
	if err != nil {
		return fmt.Errorf("读取 GeoIP 数据库失败: %w", err)
	}
	return writePrivate(filepath.Join(r.dataDir, "Country.mmdb"), data)
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
		r.emit()
		s := r.state
		r.mu.Unlock()
		return s, nil
	}
	cmd, done := r.cmd, r.done
	r.state.Status = "stopping"
	r.emit()
	r.mu.Unlock()
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	r.mu.Lock()
	if r.cmd == cmd {
		r.cmd = nil
	}
	r.state.Status, r.state.Error = "stopped", ""
	r.emit()
	s := r.state
	r.mu.Unlock()
	return s, nil
}

func (r *Runner) SetSystemProxy(enabled bool) (State, error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	if !enabled {
		return r.Stop()
	}
	if r.systemProxy == nil {
		return r.Snapshot(), errors.New("当前平台不支持系统代理")
	}
	if _, err := r.Start(); err != nil {
		return r.Snapshot(), err
	}
	r.mu.Lock()
	if r.state.SystemProxyEnabled {
		state := r.state
		r.mu.Unlock()
		return state, nil
	}
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
		sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
		return groups, nil
	}
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
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups, nil
}

func (r *Runner) NodeNames() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.store.NodeNames()
}

func (r *Runner) TestGroupDelay(group string) (delays map[string]int, err error) {
	r.operationMu.Lock()
	defer r.operationMu.Unlock()
	r.mu.Lock()
	running := r.state.Status == "running"
	r.mu.Unlock()
	if !running {
		state, err := r.Start()
		if err != nil {
			return nil, err
		}
		if state.Status != "running" {
			return nil, errors.New("内核尚未就绪，无法测延迟")
		}
		defer func() {
			_, stopErr := r.Stop()
			err = errors.Join(err, stopErr)
		}()
	}
	r.mu.Lock()
	apiPort, secret, transport := r.apiPort, r.secret, r.client.Transport
	r.mu.Unlock()

	groups, err := r.Groups()
	if err != nil {
		return nil, err
	}
	var options []string
	found := false
	for _, candidate := range groups {
		if candidate.Name == group {
			options = candidate.Options
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("策略组不存在")
	}

	client := &http.Client{Timeout: 6 * time.Second, Transport: transport}
	delays = make(map[string]int)
	var mu sync.Mutex
	var workers sync.WaitGroup
	limit := make(chan struct{}, 8)
	for _, option := range options {
		workers.Add(1)
		limit <- struct{}{}
		go func() {
			defer workers.Done()
			defer func() { <-limit }()
			delay, err := testProxyDelay(client, apiPort, secret, option)
			if err == nil && delay > 0 {
				mu.Lock()
				delays[option] = delay
				mu.Unlock()
			}
		}()
	}
	workers.Wait()
	return delays, nil
}

func testProxyDelay(client *http.Client, apiPort int, secret, option string) (int, error) {
	query := url.Values{"url": {delayTestURL}, "timeout": {"5000"}}
	address := fmt.Sprintf("http://127.0.0.1:%d/proxies/%s/delay?%s", apiPort, url.PathEscape(option), query.Encode())
	request, err := http.NewRequest(http.MethodGet, address, nil)
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.Status != "running" {
		return r.store.SelectOption(group, option)
	}
	if err := r.selectController(group, option); err != nil {
		return err
	}
	return r.store.SelectOption(group, option)
}

// selectController is called with the runner mutex held.
func (r *Runner) selectController(group, option string) error {
	var response struct {
		Proxies map[string]struct {
			Type string   `json:"type"`
			All  []string `json:"all"`
		} `json:"proxies"`
	}
	if err := r.request(http.MethodGet, "/proxies", nil, &response); err != nil {
		return err
	}
	p, ok := response.Proxies[group]
	if !ok || p.Type != "Selector" {
		return errors.New("策略组不可手动选择")
	}
	valid := false
	for _, candidate := range p.All {
		valid = valid || candidate == option
	}
	if !valid {
		return errors.New("节点不在策略组中")
	}
	body, err := json.Marshal(map[string]string{"name": option})
	if err != nil {
		return err
	}
	return r.request(http.MethodPut, "/proxies/"+url.PathEscape(group), strings.NewReader(string(body)), nil)
}

func (r *Runner) Close() {
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
	deadline := time.NewTimer(12 * time.Second)
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
		if !strings.HasPrefix(key, "CLASH_") && key != "SAFE_PATHS" {
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
	if len(w.data) > 16<<10 {
		w.data = append([]byte(nil), w.data[len(w.data)-(16<<10):]...)
	}
	return len(p), nil
}
