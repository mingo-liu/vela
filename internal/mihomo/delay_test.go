package mihomo

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

func TestDelayResultsArriveBeforeSlowProbesAndSnapshotsStayIndependent(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"Choose":{"type":"Selector","all":["Fast","Failed","Slow"]}}}`))
		case "/proxies/Fast/delay":
			_, _ = w.Write([]byte(`{"delay":37}`))
		case "/proxies/Failed/delay":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/proxies/Slow/delay":
			select {
			case <-release:
				_, _ = w.Write([]byte(`{"delay":83}`))
			case <-request.Context().Done():
			}
		default:
			t.Errorf("unexpected controller request: %s", request.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	defer releaseSlow()
	address, _ := url.Parse(server.URL)
	runner := NewRunner(profile.NewStore(t.TempDir()), nil, "", "", 7890, nil, nil)
	runner.apiPort, _ = strconv.Atoi(address.Port())
	runner.state.Status = "running"
	updates := make(chan OperationProgress, 16)
	runner.SetOperationObserver(func(progress OperationProgress) { updates <- progress })
	type result struct {
		delays map[string]int
		err    error
	}
	finished := make(chan result, 1)
	go func() { delays, err := runner.TestGroupDelay("Choose"); finished <- result{delays, err} }()
	var partial OperationProgress
	deadline := time.After(2 * time.Second)
waitPartial:
	for {
		select {
		case progress := <-updates:
			if progress.Completed == 2 {
				partial = progress
				break waitPartial
			}
		case <-deadline:
			t.Fatal("completed probes were not published while the slow probe was pending")
		}
	}
	if len(partial.Delays) != 2 || partial.Delays["Fast"] != 37 || partial.Delays["Failed"] != 0 || !partial.Active {
		t.Fatalf("unexpected partial results: %+v", partial)
	}
	select {
	case <-finished:
		t.Fatal("test completed before the slow probe was released")
	default:
	}
	snapshot := runner.Operation()
	snapshot.Delays["Fast"] = 999
	if runner.Operation().Delays["Fast"] != 37 || partial.Delays["Fast"] != 37 {
		t.Fatal("mutating a read snapshot changed runner or event data")
	}
	releaseSlow()
	select {
	case result := <-finished:
		if result.err != nil || len(result.delays) != 2 || result.delays["Fast"] != 37 || result.delays["Slow"] != 83 {
			t.Fatalf("final method response changed: %+v, %v", result.delays, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delay test did not finish")
	}
	if _, exists := partial.Delays["Slow"]; exists {
		t.Fatal("a later probe mutated the earlier event snapshot")
	}
	final := runner.Operation()
	if final.Active || len(final.Delays) != 3 || final.Delays["Slow"] != 83 {
		t.Fatalf("final snapshot lost completed results: %+v", final)
	}
	_, id, err := runner.beginOperation("delay", "Other", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.finishOperation(id)
	if len(runner.Operation().Delays) != 0 {
		t.Fatal("a new task retained old delay results")
	}
	runner.recordDelayResult(final.ID, "Stale", 100, 1)
	if len(runner.Operation().Delays) != 0 {
		t.Fatal("a stale worker modified a newer task")
	}
	runner.CancelOperation(id)
	runner.recordDelayResult(id, "Cancelled", 0, 1)
	if len(runner.Operation().Delays) != 0 {
		t.Fatal("cancelled probes were published as failures")
	}
}
