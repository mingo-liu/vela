import { useEffect, useRef, useState } from 'react'
import { ArrowClockwise, ArrowRight, CaretDown, CheckCircle, FileArrowUp, FolderOpen, GearSix, GlobeHemisphereWest, House, LinkSimple, ListBullets, Stack, WifiMedium } from '@phosphor-icons/react'
import { Events } from '@wailsio/runtime'
import velaIcon from '../../build/appicon.icon/Assets/vela_icon.svg'
import * as Runtime from '../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { CoreInfo, Group, State } from '../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import type { Subscription } from '../bindings/github.com/mingo-liu/vela/internal/profile/models'
import type { Settings } from '../bindings/github.com/mingo-liu/vela/internal/profile/models'
import { translate, localizeError, type Language } from './i18n'

type Page = 'home' | 'proxies' | 'profiles' | 'logs' | 'settings'
type SortMode = 'name' | 'delay'
type LogLevel = 'all' | 'error' | 'warning' | 'info' | 'debug' | 'other'

const empty: State = { status: 'stopped', port: 7890, hasProfile: false, error: '', systemProxyEnabled: false, tunEnabled: false, tunSupported: false, routingMode: 'rule' }
const routingModes = [
  { value: 'rule', label: '规则', description: '按配置规则分流' },
  { value: 'global', label: '全局', description: '使用 GLOBAL 策略组' },
  { value: 'direct', label: '直连', description: '全部直连' },
] as const
const navigation = [
  { id: 'home', label: '首页', icon: House },
  { id: 'proxies', label: '代理', icon: GlobeHemisphereWest },
  { id: 'profiles', label: '配置', icon: Stack },
  { id: 'logs', label: '日志', icon: ListBullets },
  { id: 'settings', label: '设置', icon: GearSix },
] as const

const logLevels = [
  { value: 'all', label: '全部级别' },
  { value: 'error', label: '错误' },
  { value: 'warning', label: '警告' },
  { value: 'info', label: '信息日志' },
  { value: 'debug', label: '调试' },
  { value: 'other', label: '其他' },
] as const

function logLevel(line: string): Exclude<LogLevel, 'all'> {
  const level = line.match(/\blevel\s*=\s*["']?(error|fatal|warning|warn|info|debug|trace)\b/i)?.[1]
    ?? line.match(/^\s*(?:\[[^\]]+\]\s*)?\[?(error|fatal|warning|warn|info|debug|trace)\]?\s*[:\s]/i)?.[1]
  switch (level?.toLowerCase()) {
    case 'fatal': case 'error': return 'error'
    case 'warn': case 'warning': return 'warning'
    case 'info': return 'info'
    case 'trace': case 'debug': return 'debug'
    default: return 'other'
  }
}

const defaultSettings: Settings = { mixedPort: 7890, routingMode: 'rule', autoConnect: false, autoConnectMode: 'system', logLevel: 'profile', launchAtLogin: false, language: 'zh-CN' }

function formatBytes(bytes: number | null, language: Language): string {
  if (bytes === null) return translate(language, '未提供')
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)) - 1, units.length - 1)
  return `${(bytes / 1024 ** (unit + 1)).toFixed(2)} ${units[unit]}`
}

function formatDate(value: string | null, language: Language): string {
  if (!value) return translate(language, '未提供')
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? translate(language, '未提供') : date.toLocaleString(language, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function subscriptionDomain(address: string, language: Language): string {
  try {
    return new URL(address).hostname || translate(language, '未知域名')
  } catch {
    return translate(language, '未知域名')
  }
}

function SortModeIcon({ mode }: { mode: SortMode }) {
  return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
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
  const [settings, setSettings] = useState<Settings>(defaultSettings)
  const [coreInfo, setCoreInfo] = useState<CoreInfo | null>(null)
  const [coreInfoError, setCoreInfoError] = useState('')
  const [logs, setLogs] = useState('')
  const [logsError, setLogsError] = useState('')
  const [selectedLogLevel, setSelectedLogLevel] = useState<LogLevel>('all')
  const logList = useRef<HTMLDivElement>(null)
  const followLogs = useRef(true)
  const [portDraft, setPortDraft] = useState('7890')
  const [profileRevision, setProfileRevision] = useState(0)
  const fileInput = useRef<HTMLInputElement>(null)
  const language: Language = settings.language === 'en-US' ? 'en-US' : 'zh-CN'
  const t = (text: string) => translate(language, text)
  const message = (error: unknown) => error instanceof Error ? error.message : String(error)

  useEffect(() => { document.documentElement.lang = language }, [language])

  useEffect(() => Events.On('open-settings', () => setPage('settings')), [])
  useEffect(() => Events.On('open-logs', () => setPage('logs')), [])

  useEffect(() => {
    if (page !== 'logs') return
    let active = true
    const refresh = async () => {
      try {
        const value = await Runtime.Logs()
        if (active) { setLogs(value); setLogsError('') }
      } catch (error) {
        if (active) setLogsError(message(error))
      }
    }
    void refresh()
    const timer = window.setInterval(refresh, 1500)
    return () => { active = false; window.clearInterval(timer) }
  }, [page])

  useEffect(() => {
    if (page === 'logs' && followLogs.current && logList.current) {
      logList.current.scrollTop = logList.current.scrollHeight
    }
  }, [logs, page, selectedLogLevel])

  useEffect(() => setPortDraft(String(state.port)), [state.port])

  useEffect(() => {
    let active = true
    Runtime.Settings().then(value => { if (active) setSettings(value) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [])

  useEffect(() => {
    let active = true
    Runtime.CoreInfo().then(value => { if (active) setCoreInfo(value) })
      .catch(error => { if (active) setCoreInfoError(message(error)) })
    return () => { active = false }
  }, [])

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

  const updateSettings = async (action: () => Promise<Settings>) => {
    setBusy(true)
    setNotice('')
    try {
      setSettings(await action())
    } catch (error) {
      setNotice(message(error))
      try { setSettings(await Runtime.Settings()) } catch { /* retain the last known settings */ }
    } finally {
      setBusy(false)
    }
  }

  const openConfigDirectory = async () => {
    setNotice('')
    try { await Runtime.OpenConfigDirectory() } catch (error) { setNotice(message(error)) }
  }

  const saveMixedPort = async () => {
    const port = Number(portDraft)
    if (!Number.isInteger(port) || port < 1024 || port > 65535) {
      setNotice('本地代理端口必须在 1024–65535 之间')
      return
    }
    if (await execute(() => Runtime.SetMixedPort(port))) setPortDraft(String(port))
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
    }
  }

  const updateSubscription = async (id: string) => {
    if (await execute(() => Runtime.UpdateSubscription(id))) { await refreshSubscriptions(); setProfileRevision(value => value + 1) }
  }

  const selectSubscription = async (id: string) => {
    if (await execute(() => Runtime.SelectSubscription(id))) { await refreshSubscriptions(); setProfileRevision(value => value + 1) }
  }

  const running = state.status === 'running'
  const connected = state.systemProxyEnabled || state.tunEnabled
  const logLines = logs.split(/\r?\n/).filter(line => line.trim() !== '')
  const visibleLogLines = logLines.map(line => ({ line, level: logLevel(line) })).filter(entry => selectedLogLevel === 'all' || entry.level === selectedLogLevel)

  return <div className="app-layout">
    <aside className="sidebar" aria-label={t('主导航')}>
      <div className="brand"><img className="brand-icon" src={velaIcon} alt="" /><div><strong>Vela</strong></div></div>
      <nav className="navigation" aria-label={t('页面')}>
        {navigation.map(item => <button key={item.id} type="button" className={`nav-item${page === item.id ? ' active' : ''}`} aria-current={page === item.id ? 'page' : undefined} onClick={() => setPage(item.id)}><item.icon size={25} weight="regular" /><span>{t(item.label)}</span></button>)}
      </nav>
      <div className="sidebar-footer"><span className={`sidebar-dot${connected ? ' connected' : ''}`} /><div><strong>{connected ? t('已连接') : t('未连接')}</strong></div></div>
    </aside>

    <main className="content">
      {page === 'home' && <>
        <div className="page-heading"><h1>{t('首页')}</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
        <section className="panel connection-panel" aria-labelledby="connection-title">
          <div className="connection-main"><div><h2 id="connection-title">{connected ? t('连接已就绪') : t('准备开始连接')}</h2></div></div>
          <h3 className="connection-setting-title">{t('网络设置')}</h3>
          <div className="connection-modes" aria-label={t('网络设置')}>
            <button className={`mode-option${state.systemProxyEnabled ? ' on' : ''}`} type="button" role="switch" aria-label={t('系统代理')} aria-checked={state.systemProxyEnabled} disabled={busy || (!state.hasProfile && !state.systemProxyEnabled)} onClick={() => void execute(() => Runtime.SetSystemProxy(!state.systemProxyEnabled))}><span><strong>{t('系统代理')}</strong><small>{t('让遵循系统代理设置的应用连接')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
            <button className={`mode-option${state.tunEnabled ? ' on' : ''}`} type="button" role="switch" aria-label={t('Tun 模式')} aria-checked={state.tunEnabled} disabled={busy || !state.tunSupported || (!state.hasProfile && !state.tunEnabled)} onClick={() => void execute(() => Runtime.SetTun(!state.tunEnabled))}><span><strong>{t('Tun 模式')}</strong><small>{state.tunSupported ? t('首次使用或 Tun 服务、内核更新后授权') : t('当前平台暂不支持')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
          </div>
          <fieldset className="routing-settings" disabled={busy}>
            <legend className="connection-setting-title">{t('代理模式')}</legend>
            <div className="routing-options">{routingModes.map(option => <label className={`routing-option${state.routingMode === option.value ? ' selected' : ''}`} key={option.value}>
              <input type="radio" name="routing-mode" value={option.value} checked={state.routingMode === option.value} onChange={() => void execute(() => Runtime.SetRoutingMode(option.value))} />
              <span><strong>{t(option.label)}</strong><small>{t(option.description)}</small></span>
            </label>)}</div>
          </fieldset>
          {!state.hasProfile && <button type="button" className="inline-link" onClick={() => setPage('profiles')}>{t('先导入配置以启用连接')} <ArrowRight size={17} /></button>}
          <div className="connection-meta"><div><span>{t('本地代理')}</span><strong>127.0.0.1:{state.port}</strong></div><div><span>{t('内核状态')}</span><strong>{running ? t('运行中') : state.status === 'starting' ? t('启动中') : t('已停止')}</strong></div><div><span>{t('配置文件')}</span><strong>{state.hasProfile ? t('已导入') : t('未导入')}</strong></div></div>
        </section>
        <div className="module-grid">
          <section className="panel module-card"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h3>{t('代理节点')}</h3></div><button className="module-link" type="button" onClick={() => setPage('proxies')}>{t('查看代理')} <ArrowRight size={17} /></button></section>
          <section className="panel module-card"><div className="module-icon"><Stack size={24} /></div><div><h3>{t('配置与订阅')}</h3></div><button className="module-link" type="button" onClick={() => setPage('profiles')}>{t('查看配置')} <ArrowRight size={17} /></button></section>
        </div>
      </>}

      {page === 'proxies' && <>
        <div className="page-heading"><h1>{t('代理')}</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
        <section className="panel page-panel proxies-panel"><div className="panel-heading"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h2>{t('策略组')}</h2></div></div>
          {!state.hasProfile && <div className="empty-state"><GlobeHemisphereWest size={42} weight="light" /><h3>{t('尚无配置')}</h3><p>{t('前往配置页导入订阅或本地配置。')}</p><button className="secondary-button" type="button" onClick={() => setPage('profiles')}>{t('前往配置页')} <ArrowRight size={16} /></button></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length === 0 && <div className="empty-state"><CheckCircle size={42} weight="light" /><h3>{t('没有可选节点')}</h3><p>{t('当前配置未提供代理节点或手动策略组。')}</p></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length > 0 && <p className="no-groups">{t('当前配置没有手动策略组，下方列出订阅节点。')}</p>}
          <div className="proxy-group-stack">{groups.map((group, index) => {
            const expanded = expandedGroups[group.name] ?? index === 0
            const sortMode = sortModes[group.name] ?? 'name'
            const delays = groupDelays[group.name] ?? {}
            const options = [...(group.options ?? [])]
            if (sortMode === 'name') options.sort((a, b) => a.localeCompare(b, language, { numeric: true }))
            if (sortMode === 'delay') options.sort((a, b) => (delays[a] ?? Infinity) - (delays[b] ?? Infinity) || a.localeCompare(b, language, { numeric: true }))
            return <section className="proxy-group" key={group.name} aria-label={`${group.name} ${t('策略组')}`}>
              <div className="proxy-group-header">
                <button className="proxy-group-toggle" type="button" aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}>
                  <span className="proxy-group-title"><strong>{group.name}</strong><small><span className="proxy-kind">{t('手动选择')}</span><span>{group.current || t('未选择')}</span></small></span>
                </button>
                <div className="proxy-group-actions">
                  <button className={`proxy-group-action signal${testingGroup === group.name ? ' testing' : ''}`} type="button" aria-label={`${t('测试节点延迟')}: ${group.name}`} title={t('测延迟')} disabled={busy || testingGroup !== null} onClick={() => void testGroupDelay(group.name)}><WifiMedium size={30} weight="bold" aria-hidden="true" /></button>
                  <button className="proxy-group-action sort" type="button" aria-label={`${group.name}: ${t(sortMode === 'name' ? '当前按名称排序，点击按延迟排序' : '当前按延迟排序，点击按名称排序')}`} title={sortMode === 'name' ? t('当前按名称排序，点击按延迟排序') : t('当前按延迟排序，点击按名称排序')} onClick={() => toggleGroupSort(group.name)}><SortModeIcon mode={sortMode} /></button>
                </div>
                <button className="proxy-group-expand" type="button" aria-label={`${expanded ? t('收起') : t('展开')} ${group.name}`} aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}><span>{group.options?.length ?? 0} {t('个节点')}</span><CaretDown size={20} className={expanded ? 'expanded' : ''} /></button>
              </div>
              {expanded && <div className="proxy-node-grid" id={`proxy-group-${index}`}>{options.map(option => <button className={`proxy-node${option === group.current ? ' selected' : ''}`} type="button" key={option} aria-pressed={option === group.current} disabled={busy} onClick={() => void select(group.name, option)}><strong title={option}>{option}</strong><span className="proxy-node-meta"><span className="proxy-node-kind">{groups.some(candidate => candidate.name === option) ? t('策略组') : nodeNames.includes(option) ? t('代理节点') : t('内置节点')}</span><span className={`proxy-node-delay${testingGroup === group.name ? ' testing' : delays[option] ? ' measured' : ''}`}>{testingGroup === group.name ? <>{t('测速中')}<span className="delay-dots" aria-hidden="true"><i /><i /><i /></span></> : delays[option] ? `${delays[option]} ms` : measuredGroups[group.name] ? t('失败') : t('未测速')}</span></span></button>)}</div>}
            </section>
          })}</div>
          {groups.length === 0 && nodeNames.length > 0 && <div className="profile-nodes"><h3>{t('当前配置节点')} <small>{nodeNames.length} {t('个')}</small></h3><div className="proxy-options" aria-label={t('当前配置节点')}>{nodeNames.map(name => <span key={name}>{name}</span>)}</div></div>}
        </section>
      </>}

      {page === 'profiles' && <>
        <div className="page-heading"><h1>{t('配置')}</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
        <div className="profile-stack">
          <section className="subscription-section" aria-label={t('已保存的订阅')}>
            {subscriptions.length === 0 && <div className="panel subscription-empty">{t('还没有订阅。请在下方粘贴订阅地址导入。')}</div>}
            <div className="subscription-grid">
              {subscriptions.map(subscription => {
                const remaining = subscription.total !== null && subscription.upload !== null && subscription.download !== null
                  ? Math.max(0, subscription.total - subscription.upload - subscription.download) : null
                const domain = subscriptionDomain(subscription.url, language)
                return <article className={`panel subscription-card${subscription.active ? ' selected' : ''}`} key={subscription.id}>
                  <div className="subscription-card-top">
                    <button className="subscription-refresh" type="button" disabled={busy || running} aria-label={subscription.active ? t('更新此订阅') : t('更新并设为当前配置')} title={subscription.active ? t('更新此订阅') : t('更新并设为当前配置')} onClick={() => void updateSubscription(subscription.id)}><ArrowClockwise size={17} /></button>
                  </div>
                  <button className="subscription-select" type="button" aria-label={`${subscription.active ? t('当前订阅') : t('选择订阅')} ${domain}`} aria-pressed={subscription.active} disabled={busy || subscription.active} onClick={() => void selectSubscription(subscription.id)}>
                    <span className="subscription-url" title={domain}><LinkSimple size={15} /><span>{domain}</span></span>
                    <span className="subscription-usage"><span>{t('剩余')} <strong>{formatBytes(remaining, language)}</strong></span><span>{t('总量')} <strong>{formatBytes(subscription.total, language)}</strong></span></span>
                    <span className="subscription-dates"><span>{t('到期')} {formatDate(subscription.expiresAt, language)}</span><span>{t('更新')} {formatDate(subscription.updatedAt, language)}</span></span>
                  </button>
                </article>
              })}
            </div>
          </section>
          <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><FileArrowUp size={24} /></div><div><h2>{t('本地配置')}</h2></div></div><button className="primary-button import-button file-button" type="button" disabled={busy || running} onClick={() => fileInput.current?.click()}>{t('选择 YAML 文件')} <FileArrowUp size={18} /></button><input ref={fileInput} className="file-input" type="file" accept=".yaml,.yml,text/yaml" tabIndex={-1} onChange={e => { void importFile(e.target.files?.[0]); e.target.value = '' }} /></section>
          <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><LinkSimple size={24} /></div><div><h2>{t('导入订阅')}</h2></div></div><label className="field-label" htmlFor="subscription-url">{t('订阅链接')}</label><input className="text-field" id="subscription-url" type="url" value={subscriptionURL} disabled={busy} autoComplete="off" spellCheck={false} placeholder={t('粘贴 HTTP / HTTPS 订阅地址')} onChange={e => setSubscriptionURL(e.target.value)} /><div className="profile-actions"><button className="primary-button import-button" type="button" disabled={busy || !subscriptionURL} onClick={() => void importSubscription()}>{t('导入订阅')} <ArrowRight size={18} /></button></div>{subscriptionURL.startsWith('http://') && <small className="http-note">{t('此地址使用 HTTP，访问令牌会在网络上传输明文。')}</small>}</section>
        </div>
      </>}
      {page === 'logs' && <>
        <div className="page-heading"><h1>{t('日志')}</h1></div>
        {logsError && <div className="alert" role="alert">{localizeError(language, logsError)}</div>}
        <section className="panel page-panel logs-panel" aria-label={t('内核日志')}>
          <div className="logs-toolbar"><label htmlFor="logs-level">{t('日志级别')}</label><select id="logs-level" value={selectedLogLevel} onChange={event => { followLogs.current = true; setSelectedLogLevel(event.target.value as LogLevel) }}>{logLevels.map(level => <option key={level.value} value={level.value}>{t(level.label)}</option>)}</select></div>
          {logLines.length === 0 ? <div className="empty-state"><ListBullets size={42} weight="light" /><h3>{t('暂无日志')}</h3><p>{t('连接内核后，日志会显示在这里。')}</p></div>
            : visibleLogLines.length === 0 ? <div className="empty-state"><h3>{t('没有符合当前级别的日志')}</h3></div>
              : <div className="log-list" ref={logList} role="log" aria-live="off" onScroll={event => { const list = event.currentTarget; followLogs.current = list.scrollHeight - list.scrollTop - list.clientHeight < 32 }}>{visibleLogLines.map((entry, index) => <div className="log-entry" key={index}><span className={`log-level log-level-${entry.level}`}>{t(logLevels.find(level => level.value === entry.level)?.label ?? '其他')}</span><code>{entry.line}</code></div>)}</div>}
        </section>
      </>}
      {page === 'settings' && <>
        <div className="page-heading"><h1>{t('设置')}</h1></div>
        {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
        <div className="settings-stack">
          <section className="panel settings-card" aria-labelledby="language-settings-title">
            <h2 id="language-settings-title">{t('语言')}</h2>
            <label className="setting-row" htmlFor="interface-language"><span><strong>{t('界面语言')}</strong><small>{t('切换后立即生效')}</small></span><select id="interface-language" value={language} disabled={busy} onChange={event => void updateSettings(() => Runtime.SetLanguage(event.target.value))}><option value="zh-CN">简体中文</option><option value="en-US">English</option></select></label>
          </section>
          <section className="panel settings-card" aria-labelledby="startup-settings-title">
            <h2 id="startup-settings-title">{t('启动')}</h2>
            <button className={`setting-row setting-switch${settings.launchAtLogin ? ' on' : ''}`} type="button" role="switch" aria-checked={settings.launchAtLogin} disabled={busy} onClick={() => void updateSettings(() => Runtime.SetLaunchAtLogin(!settings.launchAtLogin))}><span><strong>{t('登录时启动')}</strong><small>{t('登录 macOS 后打开 Vela')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
            <button className={`setting-row setting-switch${settings.autoConnect ? ' on' : ''}`} type="button" role="switch" aria-checked={settings.autoConnect} disabled={busy} onClick={() => void updateSettings(() => Runtime.SetAutoConnect(!settings.autoConnect))}><span><strong>{t('启动后自动连接')}</strong><small>{t('有可用配置时，在 Vela 启动后连接')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
            <label className="setting-row" htmlFor="auto-connect-mode"><span><strong>{t('自动连接方式')}</strong><small>{t('下次启动时使用')}</small></span><select id="auto-connect-mode" value={settings.autoConnectMode} disabled={busy} onChange={event => void updateSettings(() => Runtime.SetAutoConnectMode(event.target.value))}><option value="system">{t('系统代理')}</option><option value="tun" disabled={!state.tunSupported}>{t('Tun 模式')}</option></select></label>
          </section>
          <section className="panel settings-card" aria-labelledby="proxy-settings-title">
            <h2 id="proxy-settings-title">{t('代理设置')}</h2>
            <fieldset className="settings-routing" disabled={busy}><legend>{t('代理模式')}</legend><div className="routing-options">{routingModes.map(option => <label className={`routing-option${state.routingMode === option.value ? ' selected' : ''}`} key={option.value}><input type="radio" name="settings-routing-mode" value={option.value} checked={state.routingMode === option.value} onChange={() => void execute(() => Runtime.SetRoutingMode(option.value))} /><span><strong>{t(option.label)}</strong><small>{t(option.description)}</small></span></label>)}</div></fieldset>
            <div className="setting-row port-setting"><label htmlFor="mixed-port"><strong>{t('本地代理端口')}</strong>{connected && <small>{t('保存后将按当前网络设置重新连接')}</small>}</label><div className="port-editor"><div className="port-field"><span className="port-address">127.0.0.1:</span><input id="mixed-port" type="number" min="1024" max="65535" step="1" inputMode="numeric" value={portDraft} disabled={busy} onChange={event => setPortDraft(event.target.value)} /></div><button className="secondary-button" type="button" disabled={busy || portDraft === String(state.port)} onClick={() => void saveMixedPort()}>{t('保存')}</button></div></div>
          </section>
          <section className="panel settings-card" aria-labelledby="core-settings-title">
            <h2 id="core-settings-title">{t('内核')}</h2>
            <label className="setting-row" htmlFor="log-level"><span><strong>{t('日志级别')}</strong><small>{t('重新载入配置或下次连接时生效')}</small></span><select id="log-level" value={settings.logLevel} disabled={busy} onChange={event => void updateSettings(() => Runtime.SetLogLevel(event.target.value))}><option value="profile">{t('跟随配置')}</option><option value="silent">{t('静默')}</option><option value="error">{t('错误')}</option><option value="warning">{t('警告')}</option><option value="info">{t('信息日志')}</option><option value="debug">{t('调试')}</option></select></label>
          </section>
          <section className="panel settings-card" aria-labelledby="info-settings-title">
            <h2 id="info-settings-title">{t('信息')}</h2>
            <div className="setting-row"><span><strong>{t('内核信息')}</strong></span><span className="setting-value" title={coreInfoError ? localizeError(language, coreInfoError) : undefined}>{coreInfo ? `${coreInfo.name} ${coreInfo.version}` : coreInfoError ? t('无法读取') : t('读取中…')}</span></div>
            <div className="setting-row"><span><strong>{t('配置目录')}</strong></span><button className="secondary-button" type="button" onClick={() => void openConfigDirectory()}>{t('打开目录')} <FolderOpen size={16} /></button></div>
          </section>
        </div>
      </>}
    </main>
  </div>
}
