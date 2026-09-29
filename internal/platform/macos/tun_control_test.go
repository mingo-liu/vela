//go:build darwin

package macos

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
	"go.yaml.in/yaml/v3"
)

func testTunControl(t *testing.T) *tunControl {
	t.Helper()
	data, err := profile.CompileForMode([]byte("rules: ['MATCH,DIRECT']\n"), 17890, 19090, strings.Repeat("a", 64), true, profile.RoutingRule)
	if err != nil {
		t.Fatal(err)
	}
	// Darwin Unix socket paths have a small length limit.
	dir, err := os.MkdirTemp("/tmp", "vela-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	control, err := newTunControl(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(control.client.CloseIdleConnections)
	return control
}

func TestTunControlRejectsPrivilegedOperations(t *testing.T) {
	c := testTunControl(t)
	cases := []struct{ method, path, body string }{
		{"POST", "/restart", ""}, {"POST", "/upgrade", ""},
		{"PUT", "/configs?force=true", `{"path":"/etc/passwd"}`},
		{"PATCH", "/configs", `{"mode":"rule","allow-lan":true}`},
		{"PATCH", "/configs", `{"tun":{"enable":false}}`},
		{"PUT", "/configs?force=true", `{"payload":"listeners: [{name: evil, type: mixed, port: 80}]"}`},
		{"PUT", "/proxies/test", `{"name":"DIRECT","extra":1}`},
		{"GET", "/proxies/test/delay?url=http://127.0.0.1/&timeout=5000", ""},
		{"GET", "/configs", ""}, {"GET", "/logs", ""},
		{"PATCH", "/configs", `{"mode":"rule"} {"mode":"direct"}`},
	}
	for _, tt := range cases {
		t.Run(tt.method+tt.path+tt.body, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+c.token)
			response := httptest.NewRecorder()
			c.ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status %d", response.Code)
			}
		})
	}
	req := httptest.NewRequest("GET", "/version", nil)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unauthenticated request accepted")
	}
}

func TestTunControlRecompilesReloadAndPreservesOperations(t *testing.T) {
	c := testTunControl(t)
	listener, err := net.Listen("unix", c.socket)
	if err != nil {
		t.Fatal(err)
	}
	var received struct{ method, path, body, token string }
	upstream := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		received = struct{ method, path, body, token string }{r.Method, r.URL.RequestURI(), string(data), r.Header.Get("Authorization")}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":true}`)
	})}
	go upstream.Serve(listener)
	defer upstream.Close()
	calls := []struct{ method, path, body string }{
		{"GET", "/version", ""}, {"GET", "/connections", ""}, {"GET", "/proxies", ""},
		{"PATCH", "/configs", `{"mode":"global"}`},
		{"PUT", "/proxies/a%2Fb", `{"name":"DIRECT"}`},
		{"GET", "/proxies/a%2Fb/delay?url=https%3A%2F%2Fwww.gstatic.com%2Fgenerate_204&timeout=5000", ""},
	}
	for _, tt := range calls {
		req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		req.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		c.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", tt.method, tt.path, w.Code, w.Body.String())
		}
		if received.token != "Bearer "+c.coreToken || received.token == "Bearer "+c.token {
			t.Fatal("GUI token reached privileged core")
		}
	}
	payload := `mixed-port: 80
allow-lan: true
external-controller: 0.0.0.0:9999
external-controller-unix: /tmp/evil.sock
secret: attacker
mode: global
tun: {enable: false}
rule-providers:
  remote:
    type: http
    behavior: domain
    url: https://example.com/rules
    path: profile.yaml
rules: ['MATCH,DIRECT']
`
	body, _ := json.Marshal(map[string]string{"payload": payload})
	req := httptest.NewRequest("PUT", "/configs?force=true", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("reload failed: %s", w.Body.String())
	}
	var envelope struct{ Payload string }
	if err := json.Unmarshal([]byte(received.body), &envelope); err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	yaml.Unmarshal([]byte(envelope.Payload), &config)
	if config["external-controller"] != nil || config["external-controller-unix"] != c.socket || config["secret"] != c.coreToken || config["mixed-port"] != c.mixed || config["allow-lan"] != false || config["mode"] != "global" {
		t.Fatalf("unmanaged config: %v", config)
	}
	if config["tun"].(map[string]any)["enable"] != true {
		t.Fatal("TUN disabled by reload")
	}
	if strings.Contains(envelope.Payload, "profile.yaml") {
		t.Fatal("unmanaged provider path survived")
	}
}

func TestTunSnapshotRejectsEscapingSymlink(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Mkdir(filepath.Join(source, "rules"), 0700)
	os.Symlink(outside, filepath.Join(source, "rules", "local.yaml"))
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	config := []byte("rule-providers:\n  local:\n    type: file\n    path: rules/local.yaml\n")
	if err := snapshotTunRules(config, root, dest); err == nil {
		t.Fatal("symlink escape accepted")
	}
	os.Remove(filepath.Join(source, "rules", "local.yaml"))
	os.WriteFile(filepath.Join(source, "rules", "local.yaml"), []byte("payload: [example.com]"), 0600)
	if err := snapshotTunRules(config, root, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(dest, "rules", "local.yaml"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("unsafe snapshot: %v %v", info, err)
	}
}

func TestTunSnapshotProxyProviderIsConfined(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "providers"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(source, "providers", "nodes.yaml")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	config := []byte("proxy-providers:\n  nodes:\n    type: file\n    path: providers/nodes.yaml\n")
	if err := snapshotTunRules(config, root, dest); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("proxies: []"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := snapshotTunRules(config, root, dest); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "providers", "nodes.yaml")); err != nil || string(data) != "proxies: []" {
		t.Fatalf("snapshot = %q, %v", data, err)
	}
}

func TestTunSnapshotsLocalProviderCredentials(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "providers"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "client.crt"), []byte("certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	provider := "proxies:\n  - name: local\n    type: socks5\n    server: example.com\n    port: 1080\n    certificate: client.crt\n"
	if err := os.WriteFile(filepath.Join(source, "providers", "nodes.yaml"), []byte(provider), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	config := []byte("proxy-providers:\n  nodes:\n    type: file\n    path: providers/nodes.yaml\n")
	if _, err := snapshotTunResources(config, root, dest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "providers", "nodes.yaml"))
	if err != nil || strings.Contains(string(data), "client.crt") || !strings.Contains(string(data), "credentials/") {
		t.Fatalf("provider credentials not rewritten: %s, %v", data, err)
	}
}

func TestPrivateControllerWithRealCore(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO for private controller integration")
	}
	c := testTunControl(t)
	compiled, err := c.compile([]byte("rules: ['MATCH,DIRECT']\n"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	yaml.Unmarshal(compiled, &config)
	// Exercise the production Unix control transport without changing host routes.
	config["tun"] = map[string]any{"enable": false}
	config["mixed-port"] = 0
	compiled, err = yaml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(c.socket)
	path := filepath.Join(dir, "runtime.yaml")
	if err := os.WriteFile(path, compiled, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-d", dir, "-f", path)
	logs := &tunLog{}
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + dir}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		req := httptest.NewRequest("GET", "/version", nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		c.ServeHTTP(w, req)
		if w.Code == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("private controller unavailable: %s; %s; config: %s", w.Body.String(), logs.String(), compiled)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, mode := range []string{"global", "direct", "rule"} {
		req := httptest.NewRequest("PATCH", "/configs", strings.NewReader(`{"mode":"`+mode+`"}`))
		req.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		c.ServeHTTP(w, req)
		if w.Code != 204 {
			t.Fatalf("mode %s: %d %s", mode, w.Code, w.Body.String())
		}
	}
}

func TestTunSnapshotsCredentialsAndPreservesInlineKeys(t *testing.T) {
	source, dest := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(source, "client.pem"), []byte("certificate fixture"), 0600)
	os.WriteFile(filepath.Join(source, "key1"), []byte("private key fixture"), 0600)
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	inline := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{255}, 32))
	config := map[string]any{"proxies": []any{
		map[string]any{"name": "tls", "type": "http", "certificate": filepath.Join(source, "client.pem"), "private-key": "key1"},
		map[string]any{"name": "wg", "type": "wireguard", "private-key": inline},
	}}
	raw, _ := yaml.Marshal(config)
	compiled, err := snapshotTunResources(raw, root, dest)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	yaml.Unmarshal(compiled, &result)
	path := result.Proxies[0]["certificate"].(string)
	if !strings.HasPrefix(path, "credentials/") {
		t.Fatal("credential path was not isolated")
	}
	if !strings.HasPrefix(result.Proxies[0]["private-key"].(string), "credentials/") {
		t.Fatal("base64-looking key filename was not copied")
	}
	if result.Proxies[1]["private-key"] != inline {
		t.Fatal("inline key changed")
	}
	got, err := os.ReadFile(filepath.Join(dest, path))
	if err != nil || string(got) != "certificate fixture" {
		t.Fatalf("snapshot: %q %v", got, err)
	}
	config["proxies"] = []any{map[string]any{"certificate": "/etc/passwd"}}
	raw, _ = yaml.Marshal(config)
	if _, err := snapshotTunResources(raw, root, dest); err == nil {
		t.Fatal("absolute path outside data directory accepted")
	}
}

func TestTunClientStopsThroughConnection(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "vela-stop-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket, marker := filepath.Join(dir, "service.sock"), filepath.Join(dir, "tun-stop-test")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		var request tunRequest
		if err := json.NewDecoder(conn).Decode(&request); err != nil {
			finished <- err
			return
		}
		io.WriteString(conn, "READY\n")
		close(ready)
		_, err = io.Copy(io.Discard, conn)
		if err == nil {
			_, err = io.WriteString(conn, "STOP\n")
		}
		finished <- err
	}()
	client := make(chan error, 1)
	go func() { client <- runTunClient([]string{dir, filepath.Join(dir, "runtime.yaml"), marker}, socket) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("client not ready")
	}
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-client:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not stop")
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("client did not remove stop marker")
	}
}
