package mihomo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestSubscriptionDownloadKeepsStateResponsiveAndCancelsWithoutWrites(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(strconv.FormatBool(connected), func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				close(started)
				select {
				case <-request.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			dir := t.TempDir()
			store := profile.NewStore(dir)
			runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, "", 7890, nil, nil)
			address := server.URL + "?token=private"
			if connected {
				proxyURL, _ := url.Parse(server.URL)
				runner.state.Port, _ = strconv.Atoi(proxyURL.Port())
				runner.state.Status, runner.state.SystemProxyEnabled = "running", true
				address = "http://subscription.invalid/config?token=private"
			}
			result := make(chan error, 1)
			go func() { _, err := runner.ImportSubscription(address); result <- err }()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("download did not start")
			}
			responsive := make(chan struct{})
			go func() { runner.Snapshot(); _, _ = runner.Subscriptions(); close(responsive) }()
			select {
			case <-responsive:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("download blocked state or catalog reads")
			}
			progress := runner.Operation()
			if !progress.Active || !progress.Cancellable || progress.Kind != "subscription" || strings.Contains(progress.Target, "private") {
				t.Fatalf("unexpected progress: %+v", progress)
			}
			if runner.CancelOperation(progress.ID + 1) {
				t.Fatal("stale cancellation accepted")
			}
			if !runner.CancelOperation(progress.ID) {
				t.Fatal("cancellation rejected")
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrOperationCancelled) {
					t.Fatalf("cancel error: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancel did not stop the download")
			}
			items, err := runner.Subscriptions()
			if err != nil || len(items) != 0 || store.Exists() || runner.Operation().Active {
				t.Fatalf("cancel changed saved state: %+v, %v", items, err)
			}
		})
	}
}

func TestDelayProbesDoNotBlockDisconnect(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/proxies" {
			_, _ = w.Write([]byte(`{"proxies":{"Choose":{"type":"Selector","all":["A","B","C","D","E","F","G","H","I"]}}}`))
			return
		}
		once.Do(func() { close(started) })
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	address, _ := url.Parse(server.URL)
	proxy := &testSystemProxy{}
	runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, proxy, nil)
	runner.apiPort, _ = strconv.Atoi(address.Port())
	runner.state.Status, runner.state.SystemProxyEnabled = "running", true
	proxy.enabled = true
	result := make(chan error, 1)
	go func() { _, err := runner.TestGroupDelay("Choose"); result <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("probes did not start")
	}
	disconnected := make(chan error, 1)
	go func() { _, err := runner.SetSystemProxy(false); disconnected <- err }()
	select {
	case err := <-disconnected:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("disconnect waited for probes")
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrOperationCancelled) {
			t.Fatalf("cancel error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("probes did not stop")
	}
	if runner.Snapshot().Status != "stopped" || runner.Operation().Active {
		t.Fatal("disconnect or task did not finish")
	}
}

func TestOperationCancellationGuardsApplyAndNewTasks(t *testing.T) {
	runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, nil, nil)
	ctx, id, err := runner.beginOperation("subscription", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.beginApply(ctx, id); err != nil {
		t.Fatal(err)
	}
	if runner.CancelOperation(id) || ctx.Err() != nil {
		t.Fatal("transactional apply was interrupted")
	}
	runner.finishOperation(id)
	ctx, next, err := runner.beginOperation("delay", "Choose", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.finishOperation(next)
	if runner.CancelOperation(id) || ctx.Err() != nil {
		t.Fatal("stale cancel affected a newer task")
	}
	if !runner.CancelOperation(next) || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("current task was not cancelled")
	}
}

func TestCancelledUpdatePreservesProfileAndSelection(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	dir := t.TempDir()
	store := profile.NewStore(dir)
	body := "proxy-groups:\n  - name: Choose\n    type: select\n    proxies: [DIRECT, REJECT]\n"
	urls := &testURLStore{value: `{"subscriptions":[{"id":"saved","url":"` + server.URL + `","active":true}]}`}
	subs := profile.NewSubscriptions(store, urls)
	if err := store.Import(body); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSubscription("saved", body); err != nil {
		t.Fatal(err)
	}
	if err := store.SelectOption("Choose", "REJECT"); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(store, subs, dir, "", 7890, nil, nil)
	result := make(chan error, 1)
	go func() { _, err := runner.UpdateSubscription("saved"); result <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download did not start")
	}
	runner.CancelOperation(runner.Operation().ID)
	select {
	case err := <-result:
		if !errors.Is(err, ErrOperationCancelled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("update did not cancel")
	}
	data, _ := store.Load()
	selected, _ := store.SelectedOptions()
	if string(data) != body || selected["Choose"] != "REJECT" {
		t.Fatalf("cancel modified profile or selection: %q, %v", data, selected)
	}
}

func TestTemporaryDelayCoreDoesNotStopReplacementConnection(t *testing.T) {
	binary := os.Getenv("VELA_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set VELA_TEST_MIHOMO for temporary-core lifecycle integration")
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer proxyServer.Close()
	defer close(release)
	address, _ := url.Parse(proxyServer.URL)
	dir := t.TempDir()
	store := profile.NewStore(dir)
	body := "proxies:\n  - name: Slow\n    type: http\n    server: 127.0.0.1\n    port: " + address.Port() + "\nproxy-groups:\n  - name: Choose\n    type: select\n    proxies: [Slow]\nrules: ['MATCH,DIRECT']\n"
	if err := store.Import(body); err != nil {
		t.Fatal(err)
	}
	port, _ := freePort()
	runner := NewRunner(store, profile.NewSubscriptions(store, &testURLStore{}), dir, binary, port, &testSystemProxy{}, nil)
	t.Cleanup(runner.Close)
	result := make(chan error, 1)
	go func() { _, err := runner.TestGroupDelay("Choose"); result <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("temporary-core probe did not start")
	}
	if _, err := runner.SetSystemProxy(true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrOperationCancelled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delay cleanup did not finish")
	}
	state := runner.Snapshot()
	if state.Status != "running" || !state.SystemProxyEnabled {
		t.Fatalf("delay cleanup stopped replacement: %+v", state)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticUpdatesWaitForInteractiveTaskAndStopAfterClose(t *testing.T) {
	dir := t.TempDir()
	store := profile.NewStore(dir)
	urls := &testURLStore{value: `{"subscriptions":[{"id":"saved","url":"http://127.0.0.1:1/never","active":false}]}`}
	subs := profile.NewSubscriptions(store, urls)
	runner := NewRunner(store, subs, dir, "", 7890, nil, nil)
	_, taskID, err := runner.beginOperation("delay", "Choose", 0)
	if err != nil {
		t.Fatal(err)
	}
	runner.UpdateDueSubscriptions(time.Now(), time.Hour)
	items, _ := runner.Subscriptions()
	if items[0].LastAttemptAt != nil || items[0].LastUpdateError != "" {
		t.Fatal("background updates interfered with the interactive task")
	}
	runner.finishOperation(taskID)
	runner.Close()
	runner.UpdateDueSubscriptions(time.Now(), time.Hour)
	items, _ = runner.Subscriptions()
	if items[0].LastAttemptAt != nil || items[0].LastUpdateError != "" {
		t.Fatal("background update ran after shutdown")
	}
	if _, err := runner.ImportSubscription(items[0].URL); !errors.Is(err, ErrRunnerClosed) {
		t.Fatalf("download started after shutdown: %v", err)
	}
}
