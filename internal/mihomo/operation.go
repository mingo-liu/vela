package mihomo

import (
	"context"
	"errors"
	"time"
)

var ErrOperationCancelled = errors.New("操作已取消")

// OperationProgress describes a cancellable download or node delay test.
// Subscription addresses and controller credentials are never included.
type OperationProgress struct {
	ID          uint64 `json:"id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Phase       string `json:"phase"`
	Completed   int    `json:"completed"`
	Total       int    `json:"total"`
	Active      bool   `json:"active"`
	Cancellable bool   `json:"cancellable"`
}

func (r *Runner) SetOperationObserver(observer func(OperationProgress)) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	r.onProgress = observer
}

func (r *Runner) Operation() OperationProgress {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	return r.taskProgress
}

// CancelOperation does not wait on the connection or state locks. A stale UI
// action cannot cancel a newer task, and transactional writes cannot be cancelled.
func (r *Runner) CancelOperation(id uint64) bool {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if !r.taskProgress.Active || !r.taskProgress.Cancellable || (id != 0 && id != r.taskProgress.ID) {
		return false
	}
	r.taskCancel()
	r.taskProgress.Cancellable = false
	r.taskProgress.Phase = "cancelling"
	r.emitProgress()
	return true
}

func (r *Runner) beginOperation(kind, target string, total int) (context.Context, uint64, error) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if r.taskProgress.Active {
		return nil, 0, errors.New("已有任务正在执行，请稍后重试")
	}
	timeout := 25 * time.Second
	phase := "downloading"
	if kind == "delay" {
		timeout = 2 * time.Minute
		phase = "testing"
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	r.taskCancel = cancel
	r.taskProgress = OperationProgress{ID: r.taskProgress.ID + 1, Kind: kind, Target: target, Phase: phase, Total: total, Active: true, Cancellable: true}
	r.emitProgress()
	return ctx, r.taskProgress.ID, nil
}

func (r *Runner) beginApply(ctx context.Context, id uint64) error {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if err := ctx.Err(); err != nil {
		return operationError(err)
	}
	if id != r.taskProgress.ID || !r.taskProgress.Active {
		return errors.New("任务已结束")
	}
	r.taskProgress.Phase = "applying"
	r.taskProgress.Completed = 1
	r.taskProgress.Cancellable = false
	r.emitProgress()
	return nil
}

func (r *Runner) advanceOperation(id uint64, total int) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if id == r.taskProgress.ID && r.taskProgress.Active {
		r.taskProgress.Total = total
		r.taskProgress.Completed++
		r.emitProgress()
	}
}

func (r *Runner) finishOperation(id uint64) {
	r.taskMu.Lock()
	defer r.taskMu.Unlock()
	if id != r.taskProgress.ID {
		return
	}
	r.taskCancel()
	r.taskProgress.Active = false
	r.taskProgress.Cancellable = false
	r.emitProgress()
}

// emitProgress is called with taskMu held; observers must not call Runner.
func (r *Runner) emitProgress() {
	if r.onProgress != nil {
		r.onProgress(r.taskProgress)
	}
}

func operationError(err error) error {
	if errors.Is(err, context.Canceled) {
		return ErrOperationCancelled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("操作超时，请重试")
	}
	return err
}
