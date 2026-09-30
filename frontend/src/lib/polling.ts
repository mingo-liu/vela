export type PollingScheduler = {
  setTimeout: (callback: () => void, delay: number) => number
  clearTimeout: (timer: number) => void
}

const browserScheduler: PollingScheduler = {
  setTimeout: (callback, delay) => window.setTimeout(callback, delay),
  clearTimeout: timer => window.clearTimeout(timer),
}

// Keep at most one request in flight, even when visibility changes. isCurrent
// rejects results started before a pause, resume, interval change or disposal.
export function startPolling(
  refresh: (isCurrent: () => boolean) => Promise<void>,
  initialInterval: number | null,
  scheduler: PollingScheduler = browserScheduler,
) {
  let interval = initialInterval
  let active = true
  let pending = false
  let generation = 0
  let rerun = false
  let timer: number | undefined

  const clear = () => { if (timer !== undefined) scheduler.clearTimeout(timer); timer = undefined }
  const schedule = (delay: number) => { clear(); timer = scheduler.setTimeout(() => { timer = undefined; void run() }, delay) }
  const run = async () => {
    if (!active || interval === null || pending) return
    pending = true
    const version = generation
    try {
      await refresh(() => active && version === generation)
    } catch {
      // Refresh callbacks present errors themselves. Keep the recovery timer alive.
    } finally {
      pending = false
      if (active && interval !== null && (interval > 0 || rerun)) {
        schedule(rerun ? 0 : interval)
        rerun = false
      }
    }
  }

  if (interval !== null) schedule(0)
  return {
    setInterval(next: number | null) {
      if (!active || next === interval) return
      interval = next
      generation++
      clear()
      rerun = pending && next !== null
      if (!pending && next !== null) schedule(0)
    },
    refresh() {
      if (!active || interval === null) return
      generation++
      clear()
      rerun = pending
      if (!pending) schedule(0)
    },
    stop() { active = false; generation++; clear() },
  }
}
