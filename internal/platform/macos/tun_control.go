//go:build darwin

package macos

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
	"go.yaml.in/yaml/v3"
)

// The GUI token authenticates only this restricted API. The privileged core is
// reachable solely through a Unix socket inside its root-owned session directory.
type tunControl struct {
	prepare                  func([]byte) ([]byte, error)
	token, socket, coreToken string
	port, mixed              int
	client                   *http.Client
	reloadMu                 sync.Mutex
	slots                    chan struct{}
}

func newTunControl(compiled []byte, dir string) (*tunControl, error) {
	var config struct {
		Controller string `yaml:"external-controller"`
		Secret     string `yaml:"secret"`
		Mixed      int    `yaml:"mixed-port"`
	}
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		return nil, err
	}
	host, portText, err := net.SplitHostPort(config.Controller)
	if err != nil || host != "127.0.0.1" {
		return nil, errors.New("invalid managed controller")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1024 || port > 65535 || !profile.ValidMixedPort(config.Mixed) {
		return nil, errors.New("invalid managed port")
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	control := &tunControl{token: config.Secret, socket: filepath.Join(dir, "api.sock"), coreToken: hex.EncodeToString(token), port: port, mixed: config.Mixed, slots: make(chan struct{}, 16)}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", control.socket)
	}}
	control.client = &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return control, nil
}

func (c *tunControl) compile(data []byte) ([]byte, error) {
	var settings struct {
		Mode string `yaml:"mode"`
	}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	if settings.Mode == "" {
		settings.Mode = profile.RoutingRule
	}
	compiled, err := profile.CompileWithLogLevel(data, c.mixed, c.port, c.coreToken, true, settings.Mode, profile.LogFromProfile)
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		return nil, err
	}
	delete(config, "external-controller")
	config["external-controller-unix"] = c.socket
	return yaml.Marshal(config)
}

func decodeControlJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 6*profile.MaxConfigSize+1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func (c *tunControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		c.reloadMu.Lock()
		defer c.reloadMu.Unlock()
	}
	body, path, err := c.request(r)
	if err != nil {
		http.Error(w, "request is not permitted", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, "http://localhost"+path, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.coreToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(req)
	if err != nil {
		http.Error(w, "core unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	// Do not forward redirects or internal error details/paths to the caller.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		http.Error(w, "core request failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(response.Body, 8<<20))
}

func (c *tunControl) request(r *http.Request) ([]byte, string, error) {
	denied := errors.New("operation not permitted")
	path := r.URL.Path
	if r.Method == http.MethodGet && (path == "/version" || path == "/connections" || path == "/proxies" || path == "/rules") && r.URL.RawQuery == "" {
		return nil, path, nil
	}
	if path == "/configs" {
		switch r.Method {
		case http.MethodPatch:
			if r.URL.RawQuery != "" {
				return nil, "", denied
			}
			var config struct {
				Mode string `json:"mode"`
			}
			if err := decodeControlJSON(r, &config); err != nil || !profile.ValidRoutingMode(config.Mode) {
				return nil, "", denied
			}
			data, err := json.Marshal(config)
			return data, path, err
		case http.MethodPut:
			if r.URL.RawQuery != "force=true" {
				return nil, "", denied
			}
			var config struct {
				Payload string `json:"payload"`
			}
			if err := decodeControlJSON(r, &config); err != nil {
				return nil, "", err
			}
			compiled, err := c.compile([]byte(config.Payload))
			if err != nil {
				return nil, "", err
			}
			if c.prepare != nil {
				compiled, err = c.prepare(compiled)
				if err != nil {
					return nil, "", err
				}
			}
			data, err := json.Marshal(map[string]string{"payload": string(compiled)})
			return data, "/configs?force=true", err
		}
	}
	// Work on the escaped path so a proxy name containing '/' cannot change the
	// selected API operation. Forward a newly constructed canonical path.
	escaped := r.URL.EscapedPath()
	if strings.HasPrefix(escaped, "/proxies/") {
		name := strings.TrimPrefix(escaped, "/proxies/")
		if r.Method == http.MethodGet && strings.HasSuffix(name, "/delay") {
			name = strings.TrimSuffix(name, "/delay")
			if strings.Contains(name, "/") || name == "" {
				return nil, "", denied
			}
			decoded, err := url.PathUnescape(name)
			if err != nil || len(decoded) > 4096 {
				return nil, "", denied
			}
			values := r.URL.Query()
			if len(values) != 2 || values.Get("url") != "https://www.gstatic.com/generate_204" || values.Get("timeout") != "5000" {
				return nil, "", denied
			}
			return nil, "/proxies/" + url.PathEscape(decoded) + "/delay?" + values.Encode(), nil
		}
		if r.Method == http.MethodPut && r.URL.RawQuery == "" && name != "" && !strings.Contains(name, "/") {
			decoded, err := url.PathUnescape(name)
			if err != nil || len(decoded) > 4096 {
				return nil, "", denied
			}
			var selected struct {
				Name string `json:"name"`
			}
			if err := decodeControlJSON(r, &selected); err != nil || selected.Name == "" || len(selected.Name) > 4096 {
				return nil, "", denied
			}
			data, err := json.Marshal(selected)
			return data, "/proxies/" + url.PathEscape(decoded), err
		}
	}
	return nil, "", denied
}

// Copy file providers as regular files into the private directory. os.Root
// prevents an untrusted source symlink from escaping the user's data directory.
func snapshotTunRules(compiled []byte, root *os.Root, dest string) error {
	type fileProvider struct {
		Type string `yaml:"type"`
		Path string `yaml:"path"`
	}
	var config struct {
		Rules   map[string]fileProvider `yaml:"rule-providers"`
		Proxies map[string]fileProvider `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		return err
	}
	var total int
	providers := make([]fileProvider, 0, len(config.Rules)+len(config.Proxies))
	for _, provider := range config.Rules {
		providers = append(providers, provider)
	}
	for _, provider := range config.Proxies {
		providers = append(providers, provider)
	}
	for _, provider := range providers {
		if provider.Type != "file" {
			continue
		}
		data, err := readRootFile(root, provider.Path, 16<<20)
		if err != nil {
			return fmt.Errorf("读取本地提供器文件失败: %w", err)
		}
		total += len(data)
		if total > 64<<20 {
			return errors.New("本地规则总大小超过限制")
		}
		path := filepath.Join(dest, provider.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func readRootFile(root *os.Root, path string, limit int64) ([]byte, error) {
	f, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("文件类型或大小无效")
	}
	// Confinement alone does not protect against hard links to files owned by
	// another account. Read only owner-readable files belonging to this user.
	dirInfo, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	fileStat, fileOK := info.Sys().(*syscall.Stat_t)
	dirStat, dirOK := dirInfo.Sys().(*syscall.Stat_t)
	if !fileOK || !dirOK || fileStat.Uid != dirStat.Uid || info.Mode().Perm()&0400 == 0 {
		return nil, errors.New("文件所有者或读取权限无效")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("文件过大")
	}
	return data, nil
}

// Preserve local client certificates, SSH keys and custom ZeroTier planets as
// bounded regular-file snapshots. Never let the core follow paths back into a
// user-writable directory. Inline key material is left untouched.
func snapshotTunResources(compiled []byte, source *os.Root, dest string) ([]byte, error) {
	if err := snapshotTunRules(compiled, source, dest); err != nil {
		return nil, err
	}
	var config map[string]any
	if err := yaml.Unmarshal(compiled, &config); err != nil {
		return nil, err
	}
	budget := int64(16 << 20)
	var visit func(any, string) error
	visit = func(value any, protocol string) error {
		switch node := value.(type) {
		case map[string]any:
			if kind, ok := node["type"].(string); ok {
				protocol = kind
			}
			for key, value := range node {
				text, ok := value.(string)
				if ok && (key == "certificate" || key == "private-key" || key == "planet") && text != "" && !strings.Contains(text, "\n") {
					if key == "private-key" && (protocol == "wireguard" || protocol == "masque") {
						continue // These protocols require inline base64 keys.
					}
					path := text
					if filepath.IsAbs(path) {
						relative, err := filepath.Rel(source.Name(), path)
						if err != nil || !filepath.IsLocal(relative) {
							return errors.New("节点文件必须位于 Vela 数据目录内")
						}
						path = relative
					}
					if !filepath.IsLocal(path) {
						return errors.New("节点文件路径无效")
					}
					data, err := readRootFile(source, path, 2<<20)
					if errors.Is(err, os.ErrNotExist) {
						continue
					} // e.g. an inline WireGuard key
					if err != nil {
						return fmt.Errorf("无法读取节点 %s 文件: %w", key, err)
					}
					budget -= int64(len(data))
					if budget < 0 {
						return errors.New("节点文件总大小超过限制")
					}
					sum := sha256.Sum256(data)
					target := fmt.Sprintf("credentials/%x", sum)
					if err := os.MkdirAll(filepath.Join(dest, "credentials"), 0700); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join(dest, target), data, 0600); err != nil {
						return err
					}
					node[key] = target
				} else if err := visit(value, protocol); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range node {
				if err := visit(child, protocol); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(config["proxies"], ""); err != nil {
		return nil, err
	}
	if err := visit(config["proxy-providers"], ""); err != nil {
		return nil, err
	}
	if providers, ok := config["proxy-providers"].(map[string]any); ok {
		for _, value := range providers {
			provider, ok := value.(map[string]any)
			if !ok || provider["type"] != "file" {
				continue
			}
			path, ok := provider["path"].(string)
			if !ok || !filepath.IsLocal(path) {
				return nil, errors.New("本地节点提供器路径无效")
			}
			data, err := os.ReadFile(filepath.Join(dest, path))
			if err != nil {
				return nil, err
			}
			var content map[string]any
			if err := yaml.Unmarshal(data, &content); err != nil || content == nil {
				continue // URI and Base64 provider files have no local file fields.
			}
			if err := visit(content["proxies"], ""); err != nil {
				return nil, err
			}
			updated, err := yaml.Marshal(content)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dest, path), updated, 0600); err != nil {
				return nil, err
			}
		}
	}
	return yaml.Marshal(config)
}
