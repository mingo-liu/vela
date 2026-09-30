package mihomo

import (
	"sync"
)

// Logs returns the recent core output, including output from a stopped core.
func (r *Runner) Logs() string {
	r.mu.Lock()
	logs := r.logs
	r.mu.Unlock()
	return logs.String()
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
