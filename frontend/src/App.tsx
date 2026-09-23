import { useEffect, useRef, useState } from 'react'
import { ArrowClockwise, ArrowRight, CheckCircle, FileArrowUp, GlobeHemisphereWest, House, LinkSimple, PlugsConnected, Stack } from '@phosphor-icons/react'
import * as Runtime from '../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { Group, State } from '../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import type { Subscription } from '../bindings/github.com/mingo-liu/vela/internal/profile/models'

type Page = 'home' | 'proxies' | 'profiles'

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

export default function App() {
  const [page, setPage] = useState<Page>('home')
  const [state, setState] = useState<State>(empty)
  const [groups, setGroups] = useState<Group[]>([])
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
      setGroups(await Runtime.Groups() ?? [])
    } catch (error) {
      setNotice(message(error))
    } finally {
      setBusy(false)
    }
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
  const activeSubscription = subscriptions.find(subscription => subscription.active)
  const connected = state.systemProxyEnabled
  const statusText = connected ? '系统代理已开启' : state.status === 'starting' ? '正在启动' : state.status === 'stopping' ? '正在停止' : state.status === 'failed' ? '启动失败' : '系统代理已关闭'

  return <div className="app-layout">
    <aside className="sidebar" aria-label="主导航">
      <div className="brand"><div className="brand-icon"><PlugsConnected size={24} weight="bold" /></div><div><strong>Vela</strong><span>macOS 代理客户端</span></div></div>
      <nav className="navigation" aria-label="页面">
        {navigation.map(item => <button key={item.id} type="button" className={`nav-item${page === item.id ? ' active' : ''}`} aria-current={page === item.id ? 'page' : undefined} onClick={() => setPage(item.id)}><item.icon size={25} weight="regular" /><span>{item.label}</span></button>)}
      </nav>
      <div className="sidebar-footer"><span className={`sidebar-dot${connected ? ' connected' : ''}`} /><div><strong>{connected ? '已连接' : '未连接'}</strong><small>{connected ? '系统代理正在运行' : '系统代理已关闭'}</small></div></div>
    </aside>

    <main className="content">
      {page === 'home' && <>
        <div className="page-heading"><div><span className="eyebrow">OVERVIEW</span><h1>Home</h1><p>查看连接状态，快速管理系统代理。</p></div></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <section className="panel connection-panel" aria-labelledby="connection-title">
          <div className="panel-top"><div className="panel-icon"><PlugsConnected size={27} /></div><span className={`state-pill${connected ? ' connected' : ''}`}>{statusText}</span></div>
          <div className="connection-main"><div><span className="section-kicker">SYSTEM PROXY</span><h2 id="connection-title">{connected ? '连接已就绪' : '准备开始连接'}</h2><p>开启后自动启动内核，并接管当前网络服务的代理设置。</p></div><button className={`system-switch${connected ? ' on' : ''}`} type="button" role="switch" aria-label="系统代理" aria-checked={connected} disabled={busy || (!state.hasProfile && !connected)} onClick={() => void execute(() => Runtime.SetSystemProxy(!connected))}><span className="switch-track"><span className="switch-knob" /></span><span>{connected ? '开启' : '关闭'}</span></button></div>
          {!state.hasProfile && <button type="button" className="inline-link" onClick={() => setPage('profiles')}>先导入配置以启用系统代理 <ArrowRight size={17} /></button>}
          <div className="connection-meta"><div><span>本地代理</span><strong>127.0.0.1:{state.port}</strong></div><div><span>内核状态</span><strong>{running ? '运行中' : state.status === 'starting' ? '启动中' : '已停止'}</strong></div><div><span>配置文件</span><strong>{state.hasProfile ? '已导入' : '未导入'}</strong></div></div>
        </section>
        <div className="module-grid">
          <section className="panel module-card"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h3>代理节点</h3><p>{state.hasProfile ? `当前配置有 ${groups.length} 个可选择的策略组。` : '导入配置后查看可选节点。'}</p></div><button className="module-link" type="button" onClick={() => setPage('proxies')}>查看 Proxies <ArrowRight size={17} /></button></section>
          <section className="panel module-card"><div className="module-icon"><Stack size={24} /></div><div><h3>配置与订阅</h3><p>{state.hasProfile ? '管理已导入的配置，或更新订阅。' : '导入 YAML 文件或订阅地址以开始使用。'}</p></div><button className="module-link" type="button" onClick={() => setPage('profiles')}>查看 Profiles <ArrowRight size={17} /></button></section>
        </div>
      </>}

      {page === 'proxies' && <>
        <div className="page-heading"><div><span className="eyebrow">CONNECTIONS</span><h1>Proxies</h1><p>查看当前配置的节点；运行代理后可调整连接路径。</p></div><span className="heading-count">{groups.length} 个策略组</span></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <section className="panel page-panel"><div className="panel-heading"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h2>策略组</h2><p>{running ? '节点选择会立即应用到当前连接。' : '代理尚未运行，以下为配置中的可选节点。'}</p></div></div>
          {activeSubscription && <div className="proxy-source"><strong>当前订阅</strong><span>{activeSubscription.url}</span><small>剩余流量：{formatBytes(activeSubscription.total !== null && activeSubscription.upload !== null && activeSubscription.download !== null ? Math.max(0, activeSubscription.total - activeSubscription.upload - activeSubscription.download) : null)}　到期：{formatDate(activeSubscription.expiresAt)}</small></div>}
          {!state.hasProfile && <div className="empty-state"><GlobeHemisphereWest size={42} weight="light" /><h3>尚无配置</h3><p>前往 Profiles 导入订阅或本地配置。</p><button className="secondary-button" type="button" onClick={() => setPage('profiles')}>前往 Profiles <ArrowRight size={16} /></button></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length === 0 && <div className="empty-state"><CheckCircle size={42} weight="light" /><h3>没有可选节点</h3><p>当前配置未提供代理节点或手动策略组。</p></div>}
          {state.hasProfile && groups.length === 0 && nodeNames.length > 0 && <p className="no-groups">当前配置没有手动策略组，下方列出订阅节点。</p>}
          {groups.map(group => <div className="proxy-row" key={group.name}><div><strong>{group.name}</strong><small>{running ? `当前节点：${group.current}` : `${group.options?.length ?? 0} 个可选节点`}</small></div>{running ? <select aria-label={`选择 ${group.name} 节点`} value={group.current} disabled={busy} onChange={e => void select(group.name, e.target.value)}>{(group.options ?? []).map(option => <option key={option} value={option}>{option}</option>)}</select> : <div className="proxy-options" aria-label={`${group.name} 可选节点`}>{(group.options ?? []).map(option => <span key={option}>{option}</span>)}</div>}</div>)}
          {nodeNames.length > 0 && <div className="profile-nodes"><h3>当前配置节点 <small>{nodeNames.length} 个</small></h3><div className="proxy-options" aria-label="当前配置节点">{nodeNames.map(name => <span key={name}>{name}</span>)}</div></div>}
        </section>
      </>}

      {page === 'profiles' && <>
        <div className="page-heading"><div><span className="eyebrow">CONFIGURATION</span><h1>Profiles</h1><p>导入配置文件或订阅地址，管理代理来源。</p></div><span className={`heading-badge${state.hasProfile ? ' ready' : ''}`}>{state.hasProfile ? '配置已就绪' : '等待导入'}</span></div>
        {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
        <div className="profile-stack">
          <section className="subscription-section" aria-label="已保存的订阅">
            <div className="subscription-section-heading"><h2>已保存的订阅</h2><span>{subscriptions.length} 个订阅</span></div>
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
