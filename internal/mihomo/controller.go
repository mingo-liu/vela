package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type CoreInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TrafficTotals struct {
	Upload    int64  `json:"uploadTotal"`
	Download  int64  `json:"downloadTotal"`
	Interface string `json:"interface"`
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

func (r *Runner) reloadConfig(compiled []byte) error {
	body, err := json.Marshal(map[string]string{"payload": string(compiled)})
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, Transport: r.client.Transport}
	return r.requestWithClient(client, http.MethodPut, "/configs?force=true", strings.NewReader(string(body)), nil)
}

// patchRoutingMode changes only routing. Connection mode and managed listeners stay active.
func (r *Runner) patchRoutingMode(mode string) error {
	body, err := json.Marshal(map[string]string{"mode": mode})
	if err != nil {
		return err
	}
	return r.request(http.MethodPatch, "/configs", strings.NewReader(string(body)), nil)
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
