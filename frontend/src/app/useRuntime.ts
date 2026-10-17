import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as Runtime from '../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { OperationProgress, State } from '../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { usePolling } from '../lib/usePolling'
import { errorMessage as message } from '../lib/errors'
import { createSnapshotReceiver } from '../lib/snapshot'

const empty: State = { status: 'stopped', port: 7890, hasProfile: false, error: '', systemProxyEnabled: false, tunEnabled: false, tunSupported: false, routingMode: 'rule', configVersion: 0 }

export function useRuntime(visible: boolean) {
  const [state, setState] = useState<State>(empty)
  const [stateReceiver] = useState(() => createSnapshotReceiver<State>(value => {
    setState(previous => (Object.keys(value) as (keyof State)[]).every(key => previous[key] === value[key]) ? previous : value)
  }))
  const [busy, setBusy] = useState(false)
  const pendingActions = useRef(0)
  const startAction = () => { pendingActions.current++; setBusy(true) }
  const finishAction = () => { pendingActions.current--; setBusy(pendingActions.current > 0) }
  const [operation, setOperation] = useState<OperationProgress | null>(null)
  const [operationReceiver] = useState(() => createSnapshotReceiver<OperationProgress>(setOperation))
  const controlsBusy = busy || operation?.active === true || state.status === 'starting' || state.status === 'stopping'
  const [notice, setNotice] = useState('')
  const cancellingOperation = useRef<number | null>(null)
  const [cancellingOperationID, setCancellingOperationID] = useState<number | null>(null)
  const cancellingConnection = useRef(false)
  const [cancellingStartup, setCancellingStartup] = useState(false)

  useEffect(() => Events.On('operation-progress', event => operationReceiver.receive(event.data as OperationProgress)), [operationReceiver])
  useEffect(() => Events.On('runtime-state', event => stateReceiver.receive(event.data as State)), [stateReceiver])

  usePolling(async isCurrent => {
    try { await operationReceiver.refresh(Runtime.Operation, isCurrent) } catch { /* recover on the next poll */ }
  }, visible ? operation?.active ? 1000 : 30000 : 120000)

  usePolling(async isCurrent => {
    try { await stateReceiver.refresh(Runtime.State, isCurrent) }
    catch (error) { if (isCurrent()) setNotice(message(error)) }
  }, visible ? 30000 : 120000)

  const execute = async (action: () => Promise<State>, onError = setNotice) => {
    startAction()
    setNotice('')
    try {
      stateReceiver.receive(await action())
      return true
    } catch (error) {
      const errorText = message(error)
      if (errorText) onError(errorText)
      await stateReceiver.refresh(Runtime.State, () => true).catch(() => {})
      return false
    } finally {
      finishAction()
    }
  }

  const cancelOperation = async (id: number) => {
    if (id === 0 || !operation?.active || !operation.cancellable || operation.id !== id || cancellingOperation.current === id) return
    cancellingOperation.current = id
    setCancellingOperationID(id)
    try {
      await Runtime.CancelOperation(id)
    } catch (error) {
      setNotice(message(error))
    } finally {
      await operationReceiver.refresh(Runtime.Operation, () => true).catch(() => {})
      if (cancellingOperation.current === id) {
        cancellingOperation.current = null
        setCancellingOperationID(null)
      }
    }
  }

  const cancelStartup = async () => {
    if (state.status !== 'starting' || cancellingConnection.current) return
    cancellingConnection.current = true
    setCancellingStartup(true)
    try {
      await execute(() => Runtime.SetSystemProxy(false))
    } finally {
      cancellingConnection.current = false
      setCancellingStartup(false)
    }
  }

  const refreshState = () => stateReceiver.refresh(Runtime.State, () => true).catch(() => {})

  return {
    state, operation, controlsBusy, notice, setNotice,
    cancelOperation, cancellingOperationID, cancelStartup, cancellingStartup,
    running: state.status === 'running',
    connected: state.systemProxyEnabled || state.tunEnabled,
    profileRevision: state.configVersion,
    startAction, finishAction, execute, refreshState,
    hasPendingActions: () => pendingActions.current > 0,
    setSystemProxy: (enabled: boolean) => execute(() => Runtime.SetSystemProxy(enabled)),
    setTun: (enabled: boolean) => execute(() => Runtime.SetTun(enabled)),
    setRoutingMode: (mode: string) => execute(() => Runtime.SetRoutingMode(mode)),
  }
}

export type RuntimeController = ReturnType<typeof useRuntime>
