import { useEffect, useRef, useState } from 'react'
import { ArrowClockwise, ArrowRight, CaretDown, CheckCircle, FileArrowUp, GlobeHemisphereWest, House, LinkSimple, PlugsConnected, Stack } from '@phosphor-icons/react'
import * as Runtime from '../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { Group, State } from '../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import type { Subscription } from '../bindings/github.com/mingo-liu/vela/internal/profile/models'

type Page = 'home' | 'proxies' | 'profiles'
type SortMode = 'name' | 'delay'

const empty: State = { status: 'stopped', port: 7890, hasProfile: false, error: '', systemProxyEnabled: false }
const navigation = [
  { id: 'home', label: 'Home', icon: House },
  { id: 'proxies', label: 'Proxies', icon: GlobeHemisphereWest },
  { id: 'profiles', label: 'Profiles', icon: Stack },
] as const

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function formatBytes(bytes: number | null): string {
  if (bytes === null) return '未提供'
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)) - 1, units.length - 1)
  return `${(bytes / 1024 ** (unit + 1)).toFixed(2)} ${units[unit]}`
}

function formatDate(value: string | null): string {
  if (!value) return '未提供'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '未提供' : date.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function SortModeIcon({ mode }: { mode: SortMode }) {
  return <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {mode === 'name' ? <>
      <path d="M2.5 11 7 3l4.5 8M4.2 8h5.6" />
      <path d="M2.5 14h9l-9 7h9" />
    </> : <>
      <path d="M5.5 3h4M7.5 3v2.5" />
      <circle cx="7.5" cy="12" r="6.5" />
      <path d="M7.5 8.5V12l2.4-2.4" />
    </>}
    <path d="M19 5v14m-3-3 3 3 3-3" />
  </svg>
}

function RoundTripIcon({ testing }: { testing: boolean }) {
  return <svg className={`roundtrip-icon${testing ? ' testing' : ''}`} width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path className="trip-out" d="M3 7.5h16m-3-3 3 3-3 3" />
    <path className="trip-back" d="M21 16.5H5m3-3-3 3 3 3" />
  </svg>
}

export default function App() {
  const [page, setPage] = useState<Page>('home')
  const [state, setState] = useState<State>(empty)
  const [groups, setGroups] = useState<Group[]>([])
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [groupDelays, setGroupDelays] = useState<Record<string, Record<string, number | undefined>>>({})
  const [measuredGroups, setMeasuredGroups] = useState<Record<string, boolean>>({})
  const [testingGroup, setTestingGroup] = useState<string | null>(null)
  const [sortModes, setSortModes] = useState<Record<string, SortMode>>({})
  const [nodeNames, setNodeNames] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [subscriptionURL, setSubscriptionURL] = useState('')
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [profileRevision, setProfileRevision] = useState(0)
  const fileInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    let active = true
    const refresh = async () => {
      try {
        const next = await Runtime.State()
        if (active) setState(next)
      } catch (error) {
        if (active) setNotice(message(error))
      }
    }
    void refresh()
    const timer = window.setInterval(refresh, 1500)
    return () => { active = false; window.clearInterval(timer) }
  }, [])

  useEffect(() => {
    if (!state.hasProfile) { setGroups([]); return }
    let active = true
    Runtime.Groups().then(value => { if (active) setGroups(value ?? []) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [state.status, state.hasProfile, profileRevision])

  useEffect(() => {
    if (!state.hasProfile) { setNodeNames([]); return }
    let active = true
    Runtime.NodeNames().then(value => { if (active) setNodeNames(value ?? []) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [state.hasProfile, profileRevision])

  useEffect(() => {
    setGroupDelays({})
    setMeasuredGroups({})
  }, [profileRevision])

  useEffect(() => {
    let active = true
    Runtime.Subscriptions().then(value => { if (active) setSubscriptions(value ?? []) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [])

  const refreshSubscriptions = async () => {
    try {
      setSubscriptions(await Runtime.Subscriptions() ?? [])
    } catch (error) {
      setNotice(message(error))
    }
  }

  const execute = async (action: () => Promise<State>) => {
    setBusy(true)
    setNotice('')
    try {
      setState(await action())
      return true
    } catch (error) {
      setNotice(message(error))
      setState(await Runtime.State().catch(() => state))
      return false
    } finally {
      setBusy(false)
    }
  }

  const select = async (group: string, option: string) => {
    setBusy(true)
    setNotice('')
    try {
      await Runtime.Select(group, option)
    } catch (error) {
      setNotice(message(error))
    } finally {
      try { setGroups(await Runtime.Groups() ?? []) } catch (error) { setNotice(message(error)) }
      setBusy(false)
    }
  }

  const toggleGroup = (name: string, initiallyOpen: boolean) => {
    setExpandedGroups(current => ({ ...current, [name]: !(current[name] ?? initiallyOpen) }))
  }

  const testGroupDelay = async (group: string) => {
    if (testingGroup) return
    setTestingGroup(group)
    setNotice('')
    try {
      const delays = await Runtime.TestGroupDelay(group)
      setGroupDelays(current => ({ ...current, [group]: delays ?? {} }))
      setMeasuredGroups(current => ({ ...current, [group]: true }))
    } catch (error) {
      setNotice(message(error))
    } finally {
      setTestingGroup(null)
    }
  }

  const toggleGroupSort = (group: string) => {
    const next = (sortModes[group] ?? 'name') === 'name' ? 'delay' : 'name'
    setSortModes(current => ({ ...current, [group]: next }))
    if (next === 'delay' && !measuredGroups[group] && state.hasProfile) void testGroupDelay(group)
  }

  const importFile = async (file?: File) => {
    if (!file) return
    if (file.size > 2 * 1024 * 1024) {
      setNotice('配置文件不能超过 2 MiB')
      return
    }
    try {
      const contents = await file.text()
      if (await execute(() => Runtime.ImportProfile(contents))) { await refreshSubscriptions(); setProfileRevision(value => value + 1) }
    } catch (error) {
      setNotice(message(error))
    }
  }

  const importSubscription = async () => {
    if (await execute(() => Runtime.ImportSubscription(subscriptionURL))) {
      setSubscriptionURL('')
      await refreshSubscriptions()
      setProfileRevision(value => value + 1)
    }
  }

  const updateSubscription = async (id: string) => {
    if (await execute(() => Runtime.UpdateSubscription(id))) { await refreshSubscriptions(); setProfileRevision(value => value + 1) }
  }

  const selectSubscription = async (id: string) => {
    if (await execute(() => Runtime.SelectSubscription(id))) { await refreshSubscriptions(); setProfileRevision(value => value + 1) }
  }

  const running = state.status === 'running'
  const connected = state.systemProxyEnabled
  const statusText = connected ? '系统代理已开启' : state.status === 'starting' ? '正在启动' : state.status === 'stopping' ? '正在停止' : state.status === 'failed' ? '启动失败' : ''

  return <div className="app-layout">
    <aside className="sidebar" aria-label="主导航">
      <div className="brand"><div className="brand-icon"><PlugsConnected size={24} weight="bold" /></div><div><strong>Vela</strong></div></div>
      <nav className="navigation" aria-label="页面">
        {navigation.map(item => <button key={item.id} type="button" className={`nav-item${page === item.id ? ' active' : ''}`} aria-current={page === item.id ? 'page' : undefined} onClick={() => setPage(item.id)}><item.icon size={25} weight="regular" /><span>{item.label}</span></button>)}
      </nav>
      <div className="sidebar-footer"><span className={`sidebar-dot${connected ? ' connected' : ''}`} /><div><strong>{connected ? '已连接' : '未连接'}</strong></div></div>
    </aside>

    <main className="content">
      {page === 'home' && <>
        <div className="page-heading"><h1>Home</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <section className="panel connection-panel" aria-labelledby="connection-title">
          <div className="panel-top"><div className="panel-icon"><PlugsConnected size={27} /></div>{statusText && <span className={`state-pill${connected ? ' connected' : ''}`}>{statusText}</span>}</div>
          <div className="connection-main"><div><span className="section-kicker">SYSTEM PROXY</span><h2 id="connection-title">{connected ? '连接已就绪' : '准备开始连接'}</h2></div><button className={`system-switch${connected ? ' on' : ''}`} type="button" role="switch" aria-label="系统代理" aria-checked={connected} disabled={busy || (!state.hasProfile && !connected)} onClick={() => void execute(() => Runtime.SetSystemProxy(!connected))}><span className="switch-track"><span className="switch-knob" /></span><span>{connected ? '开启' : '关闭'}</span></button></div>
          {!state.hasProfile && <button type="button" className="inline-link" onClick={() => setPage('profiles')}>先导入配置以启用系统代理 <ArrowRight size={17} /></button>}
          <div className="connection-meta"><div><span>本地代理</span><strong>127.0.0.1:{state.port}</strong></div><div><span>内核状态</span><strong>{running ? '运行中' : state.status === 'starting' ? '启动中' : '已停止'}</strong></div><div><span>配置文件</span><strong>{state.hasProfile ? '已导入' : '未导入'}</strong></div></div>
        </section>
        <div className="module-grid">
          <section className="panel module-card"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h3>代理节点</h3></div><button className="module-link" type="button" onClick={() => setPage('proxies')}>查看 Proxies <ArrowRight size={17} /></button></section>
          <section className="panel module-card"><div className="module-icon"><Stack size={24} /></div><div><h3>配置与订阅</h3></div><button className="module-link" type="button" onClick={() => setPage('profiles')}>查看 Profiles <ArrowRight size={17} /></button></section>
        </div>
      </>}

      {page === 'proxies' && <>
        <div className="page-heading"><h1>Proxies</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <section className="panel page-panel proxies-panel"><div className="panel-heading"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h2>策略组</h2></div></div>
          {!state.hasProfile && <div className="empty-state"><GlobeHemisphereWest size={42} weight="light" /><h3>尚无配置</h3><p>前往 Profiles 导入订阅或本地配置。</p><button className="secondary-button" type="button" onClick={() => setPage('profiles')}>前往 Profiles <ArrowRight size={16} /></button></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length === 0 && <div className="empty-state"><CheckCircle size={42} weight="light" /><h3>没有可选节点</h3><p>当前配置未提供代理节点或手动策略组。</p></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length > 0 && <p className="no-groups">当前配置没有手动策略组，下方列出订阅节点。</p>}
          <div className="proxy-group-stack">{groups.map((group, index) => {
            const expanded = expandedGroups[group.name] ?? index === 0
            const sortMode = sortModes[group.name] ?? 'name'
            const delays = groupDelays[group.name] ?? {}
            const options = [...(group.options ?? [])]
            if (sortMode === 'name') options.sort((a, b) => a.localeCompare(b, 'zh-CN', { numeric: true }))
            if (sortMode === 'delay') options.sort((a, b) => (delays[a] ?? Infinity) - (delays[b] ?? Infinity) || a.localeCompare(b, 'zh-CN', { numeric: true }))
            return <section className="proxy-group" key={group.name} aria-label={`${group.name} 策略组`}>
              <div className="proxy-group-header">
                <button className="proxy-group-toggle" type="button" aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}>
                  <span className="proxy-group-title"><strong>{group.name}</strong><small><span className="proxy-kind">Selector</span><span>{running ? '当前节点' : '预选节点'}：{group.current || '未选择'}</span></small></span>
                </button>
                <div className="proxy-group-actions">
                  <button className={`proxy-group-action${testingGroup === group.name ? ' testing' : ''}`} type="button" aria-label={`测试 ${group.name} 的节点延迟`} title="测延迟" disabled={busy || testingGroup !== null} onClick={() => void testGroupDelay(group.name)}><RoundTripIcon testing={testingGroup === group.name} /></button>
                  <button className="proxy-group-action sort" type="button" aria-label={`${group.name}：当前按${sortMode === 'name' ? '名称' : '延迟'}排序，点击切换为按${sortMode === 'name' ? '延迟' : '名称'}排序`} title={sortMode === 'name' ? '当前按名称排序，点击按延迟排序' : '当前按延迟排序，点击按名称排序'} onClick={() => toggleGroupSort(group.name)}><SortModeIcon mode={sortMode} /></button>
                </div>
                <button className="proxy-group-expand" type="button" aria-label={`${expanded ? '收起' : '展开'} ${group.name}`} aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}><span>{group.options?.length ?? 0} 个节点</span><CaretDown size={20} className={expanded ? 'expanded' : ''} /></button>
              </div>
              {expanded && <div className="proxy-node-grid" id={`proxy-group-${index}`}>{options.map(option => <button className={`proxy-node${option === group.current ? ' selected' : ''}`} type="button" key={option} aria-pressed={option === group.current} disabled={busy} onClick={() => void select(group.name, option)}><strong title={option}>{option}</strong><span className="proxy-node-meta"><span className="proxy-node-kind">{groups.some(candidate => candidate.name === option) ? '策略组' : nodeNames.includes(option) ? '代理节点' : '内置节点'}</span><span className={`proxy-node-delay${testingGroup === group.name ? ' testing' : delays[option] ? ' measured' : ''}`}>{testingGroup === group.name ? <>测速中<span className="delay-dots" aria-hidden="true"><i /><i /><i /></span></> : delays[option] ? `${delays[option]} ms` : measuredGroups[group.name] ? '失败' : '未测速'}</span></span></button>)}</div>}
            </section>
          })}</div>
          {groups.length === 0 && nodeNames.length > 0 && <div className="profile-nodes"><h3>当前配置节点 <small>{nodeNames.length} 个</small></h3><div className="proxy-options" aria-label="当前配置节点">{nodeNames.map(name => <span key={name}>{name}</span>)}</div></div>}
        </section>
      </>}

      {page === 'profiles' && <>
        <div className="page-heading"><h1>Profiles</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <div className="profile-stack">
          <section className="subscription-section" aria-label="已保存的订阅">
            {subscriptions.length === 0 && <div className="panel subscription-empty">还没有订阅。请在下方粘贴订阅地址导入。</div>}
            <div className="subscription-grid">
              {subscriptions.map(subscription => {
                const remaining = subscription.total !== null && subscription.upload !== null && subscription.download !== null
                  ? Math.max(0, subscription.total - subscription.upload - subscription.download) : null
                return <article className={`panel subscription-card${subscription.active ? ' selected' : ''}`} key={subscription.id}>
                  <div className="subscription-card-top">
                    <span className={`subscription-status${subscription.active ? ' active' : ''}`}>{subscription.active ? '当前' : '已保存'}</span>
                    <button className="subscription-refresh" type="button" disabled={busy || running} aria-label={subscription.active ? '更新此订阅' : '更新并设为当前配置'} title={subscription.active ? '更新此订阅' : '更新并设为当前配置'} onClick={() => void updateSubscription(subscription.id)}><ArrowClockwise size={17} /></button>
                  </div>
                  <button className="subscription-select" type="button" aria-label={`选择订阅 ${subscription.url}`} aria-pressed={subscription.active} disabled={busy || running || subscription.active} onClick={() => void selectSubscription(subscription.id)}>
                    <span className="subscription-url" title={subscription.url}><LinkSimple size={15} /><span>{subscription.url}</span></span>
                    <span className="subscription-usage"><span>剩余 <strong>{formatBytes(remaining)}</strong></span><span>总量 <strong>{formatBytes(subscription.total)}</strong></span></span>
                    <span className="subscription-dates"><span>到期 {formatDate(subscription.expiresAt)}</span><span>更新 {formatDate(subscription.updatedAt)}</span></span>
                  </button>
                </article>
              })}
            </div>
          </section>
          <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><FileArrowUp size={24} /></div><div><h2>本地配置</h2><p>导入包含节点和规则的 mihomo YAML 文件。</p></div></div><button className="file-button" type="button" disabled={busy || running} onClick={() => fileInput.current?.click()}>选择 YAML 文件 <FileArrowUp size={18} /></button><input ref={fileInput} className="file-input" type="file" accept=".yaml,.yml,text/yaml" tabIndex={-1} onChange={e => { void importFile(e.target.files?.[0]); e.target.value = '' }} /><small className="hint">导入新配置前，请先关闭系统代理。文件大小上限为 2 MiB。</small></section>
          <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><LinkSimple size={24} /></div><div><h2>导入订阅</h2><p>从 HTTP 或 HTTPS 地址导入配置。</p></div></div><label className="field-label" htmlFor="subscription-url">订阅链接</label><input className="text-field" id="subscription-url" type="url" value={subscriptionURL} disabled={busy || running} autoComplete="off" spellCheck={false} placeholder="粘贴 HTTP / HTTPS 订阅地址" onChange={e => setSubscriptionURL(e.target.value)} /><div className="profile-actions"><button className="primary-button" type="button" disabled={busy || running || !subscriptionURL} onClick={() => void importSubscription()}>导入订阅 <ArrowRight size={17} /></button></div><small className="hint">订阅地址保存在 macOS Keychain，并显示在上方的订阅卡片中。</small>{subscriptionURL.startsWith('http://') && <small className="http-note">此地址使用 HTTP，访问令牌会在网络上传输明文。</small>}</section>
        </div>
      </>}
      <footer>关闭窗口后 Vela 会留在菜单栏；退出应用时恢复系统代理设置。</footer>
    </main>
  </div>
}
