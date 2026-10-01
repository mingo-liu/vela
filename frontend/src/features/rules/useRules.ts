import { useRef, useState } from 'react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { CustomRule } from '../../../bindings/github.com/mingo-liu/vela/internal/profile/models'
import type { RuntimeController } from '../../app/useRuntime'
import { errorMessage } from '../../lib/errors'
import { usePolling } from '../../lib/usePolling'

export function useRules(runtime: RuntimeController, profileID: string, visible: boolean) {
  const [rules, setRules] = useState<CustomRule[]>([])
  const [savedRules, setSavedRules] = useState<CustomRule[]>([])
  const [targets, setTargets] = useState<string[]>(['DIRECT', 'REJECT'])
  const [originalRules, setOriginalRules] = useState<string[]>([])
  const [active, setActive] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [readError, setReadError] = useState('')
  const [editError, setEditError] = useState('')
  const [saved, setSaved] = useState(false)
  const [dirty, setDirty] = useState(false)
  const dirtyRef = useRef(false)
  const [refreshKey, setRefreshKey] = useState(0)

  usePolling(async isCurrent => {
    try {
      const snapshot = await Runtime.RuleEditor(profileID)
      if (!isCurrent()) return
      setSavedRules(snapshot.rules ?? [])
      if (!dirtyRef.current) setRules(snapshot.rules ?? [])
      setTargets(snapshot.targets ?? ['DIRECT', 'REJECT'])
      setOriginalRules(snapshot.originalRules ?? [])
      setActive(snapshot.active)
      setLoaded(true)
      setReadError('')
    } catch (cause) { if (isCurrent()) { setLoaded(false); setReadError(errorMessage(cause)) } }
  }, visible ? 30000 : null, `${profileID}:${runtime.profileRevision}:${refreshKey}`)

  const change = (items: CustomRule[]) => {
    setRules(items)
    dirtyRef.current = true
    setDirty(true)
    setSaved(false)
    setEditError('')
  }
  const update = (id: string, patch: Partial<CustomRule>) => change(rules.map(rule => rule.id === id ? { ...rule, ...patch } : rule))
  const move = (index: number, direction: number) => {
    const next = [...rules]
    const destination = index + direction
    if (destination < 0 || destination >= next.length) return
    ;[next[index], next[destination]] = [next[destination], next[index]]
    change(next)
  }
  const reset = () => {
    setRules(savedRules)
    dirtyRef.current = false
    setDirty(false)
    setSaved(false)
    setEditError('')
  }
  const save = async () => {
    setSaved(false)
    setEditError('')
    if (await runtime.execute(() => Runtime.SaveProfileRules(profileID, rules), setEditError)) {
      dirtyRef.current = false
      setDirty(false)
      setSaved(true)
      setRefreshKey(value => value + 1)
    }
  }
  return { rules, targets, originalRules, active, loaded, error: readError || editError, dirty, saved, update, move, reset, save,
    retry: () => setRefreshKey(value => value + 1),
    add: (type: string, domain: string, target: string) => change([...rules, { id: crypto.randomUUID(), type, domain, target, enabled: true }]),
    remove: (id: string) => change(rules.filter(rule => rule.id !== id)),
  }
}

export type RulesController = ReturnType<typeof useRules>
