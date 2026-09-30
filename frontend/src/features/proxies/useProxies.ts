import { useEffect, useRef, useState } from 'react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { RuntimeController } from '../../app/useRuntime'
import type { Page } from '../../app/types'
import type { Group } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { errorMessage as message } from '../../lib/errors'

export type SortMode = 'name' | 'delay'
const delayCacheDuration = 3 * 60 * 1000

export function useProxies(runtime: RuntimeController, visible: boolean, page: Page) {
  const { state, profileRevision, controlsBusy, setNotice, startAction, finishAction } = runtime
  const [groups, setGroups] = useState<Group[]>([])
  const [groupsRevision, setGroupsRevision] = useState(-1)
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [groupDelays, setGroupDelays] = useState<Record<string, Record<string, number | undefined>>>({})
  const [measuredGroups, setMeasuredGroups] = useState<Record<string, boolean>>({})
  const [testingGroup, setTestingGroup] = useState<string | null>(null)
  const [completedDelayTests, setCompletedDelayTests] = useState(0)
  const testingGroupRef = useRef<string | null>(null)
  const delayCache = useRef<Record<string, { options: string; testedAt: number }>>({})
  const [sortModes, setSortModes] = useState<Record<string, SortMode>>({})
  const [nodeNames, setNodeNames] = useState<string[]>([])
  const profileRevisionRef = useRef(profileRevision)
  profileRevisionRef.current = profileRevision

  useEffect(() => {
    if (!state.hasProfile) { setGroups([]); setGroupsRevision(profileRevision); return }
    if (!visible) return
    let active = true
    Runtime.Groups().then(value => { if (active) { setGroups(value ?? []); setGroupsRevision(profileRevision) } })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [visible, state.status, state.hasProfile, profileRevision])

  useEffect(() => {
    if (!state.hasProfile) { setNodeNames([]); return }
    if (!visible) return
    let active = true
    Runtime.NodeNames().then(value => { if (active) setNodeNames(value ?? []) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [visible, state.hasProfile, profileRevision])

  useEffect(() => {
    delayCache.current = {}
    setGroupDelays({})
    setMeasuredGroups({})
  }, [profileRevision])

  const select = async (group: string, option: string) => {
    startAction()
    setNotice('')
    try {
      await Runtime.Select(group, option)
    } catch (error) {
      setNotice(message(error))
    } finally {
      try { setGroups(await Runtime.Groups() ?? []) } catch (error) { setNotice(message(error)) }
      finishAction()
    }
  }

  const toggleGroup = (name: string, initiallyOpen: boolean) => {
    setExpandedGroups(current => ({ ...current, [name]: !(current[name] ?? initiallyOpen) }))
  }

  const testGroupDelay = async (group: string) => {
    if (testingGroupRef.current) return
    const options = JSON.stringify(groups.find(candidate => candidate.name === group)?.options ?? [])
    const revision = profileRevision
    testingGroupRef.current = group
    delayCache.current[group] = { options, testedAt: Date.now() }
    setTestingGroup(group)
    setNotice('')
    try {
      const delays = await Runtime.TestGroupDelay(group)
      if (profileRevisionRef.current === revision) {
        setGroupDelays(current => ({ ...current, [group]: delays ?? {} }))
        setMeasuredGroups(current => ({ ...current, [group]: true }))
      }
    } catch (error) {
      if (profileRevisionRef.current === revision) setNotice(message(error))
    } finally {
      testingGroupRef.current = null
      setTestingGroup(null)
      setCompletedDelayTests(count => count + 1)
    }
  }

  useEffect(() => {
    if (!visible || page !== 'proxies' || state.status !== 'running' || !state.hasProfile || controlsBusy || testingGroupRef.current || groupsRevision !== profileRevision) return
    const group = groups.find((candidate, index) => {
      if (!(expandedGroups[candidate.name] ?? index === 0) || !candidate.options?.length) return false
      const cached = delayCache.current[candidate.name]
      return !cached || cached.options !== JSON.stringify(candidate.options) || Date.now() - cached.testedAt >= delayCacheDuration
    })
    if (group) void testGroupDelay(group.name)
  }, [visible, page, state.status, state.hasProfile, controlsBusy, groups, groupsRevision, expandedGroups, completedDelayTests, profileRevision])

  const toggleGroupSort = (group: string) => {
    const next = (sortModes[group] ?? 'name') === 'name' ? 'delay' : 'name'
    setSortModes(current => ({ ...current, [group]: next }))
    if (next === 'delay' && !measuredGroups[group] && state.hasProfile) void testGroupDelay(group)
  }

  return { groups, nodeNames, expandedGroups, groupDelays, measuredGroups, testingGroup, sortModes, select, toggleGroup, testGroupDelay, toggleGroupSort }
}

export type ProxiesController = ReturnType<typeof useProxies>
