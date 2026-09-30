package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"sync"
	"time"
)

const delayTestURL = "https://www.gstatic.com/generate_204"

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
