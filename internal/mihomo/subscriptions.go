package mihomo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mingo-liu/vela/internal/profile"
)

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
