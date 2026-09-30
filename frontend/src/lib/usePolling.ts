import { useEffect, useRef } from 'react'
import { startPolling } from './polling'

export function usePolling(refresh: (isCurrent: () => boolean) => Promise<void>, interval: number | null, refreshKey?: unknown) {
  const latest = useRef(refresh)
  latest.current = refresh
  const initialInterval = useRef(interval)
  const poller = useRef<ReturnType<typeof startPolling> | null>(null)

  useEffect(() => {
    const current = startPolling(isCurrent => latest.current(isCurrent), initialInterval.current)
    poller.current = current
    return () => { current.stop(); poller.current = null }
  }, [])

  useEffect(() => { poller.current?.setInterval(interval) }, [interval])
  useEffect(() => { poller.current?.refresh() }, [refreshKey])
}
