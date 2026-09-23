import { useEffect, useState } from 'react'
import * as Runtime from '../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { Group, State } from '../bindings/github.com/mingo-liu/vela/internal/mihomo/models'

const empty: State = { status: 'stopped', port: 7890, hasProfile: false, error: '', systemProxyEnabled: false }

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export default function App() {
  const [state, setState] = useState<State>(empty)
  const [groups, setGroups] = useState<Group[]>([])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [subscriptionURL, setSubscriptionURL] = useState('')

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
    if (state.status !== 'running') { setGroups([]); return }
    let active = true
    Runtime.Groups().then(value => { if (active) setGroups(value ?? []) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [state.status])

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
      await execute(() => Runtime.ImportProfile(contents))
    } catch (error) {
      setNotice(message(error))
    }
  }

  const importSubscription = async () => {
    if (await execute(() => Runtime.ImportSubscription(subscriptionURL))) {
      setSubscriptionURL('')
    }
  }

  const running = state.status === 'running'
  return <main className="shell">
    <header className="header"><div className="mark">V</div><div><h1>Vela</h1><p>macOS 本地代理</p></div></header>
    <section className="hero">
      <div><span className={`status ${state.status}`}>{state.status === 'running' ? '内核运行中' : state.status === 'starting' ? '正在启动' : state.status === 'stopping' ? '正在停止' : state.status === 'failed' ? '启动失败' : '内核已停止'}</span>
        <h2>让连接由你掌控</h2><p>导入 mihomo YAML 配置，启动本地 HTTP / SOCKS 代理。</p></div>
      <button className="primary" disabled={busy || !state.hasProfile} onClick={() => void execute(running ? Runtime.Stop : Runtime.Start)}>{running ? '停止代理' : '启动代理'}</button>
    </section>
    {(notice || state.error) && <div className="alert" role="alert">{notice || state.error}</div>}
    <div className="grid">
      <section className="card"><div className="card-head"><h3>配置或订阅</h3><span>{state.hasProfile ? '已导入' : '等待导入'}</span></div>
        <p>支持内联节点和规则的单份 mihomo YAML。导入或更新前请先停止内核。</p>
        <label className="file-button">选择 YAML 文件
          <input type="file" accept=".yaml,.yml,text/yaml" disabled={busy || running}
            onChange={e => { void importFile(e.target.files?.[0]); e.target.value = '' }} />
        </label>
        <div className="subscription">
          <label htmlFor="subscription-url">订阅地址</label>
          <input id="subscription-url" type="url" value={subscriptionURL} disabled={busy || running} autoComplete="off" spellCheck={false} placeholder="粘贴 HTTP / HTTPS 订阅地址" onChange={e => setSubscriptionURL(e.target.value)} />
          <div className="subscription-actions">
            <button disabled={busy || running || !subscriptionURL} onClick={() => void importSubscription()}>导入订阅</button>
            <button disabled={busy || running} onClick={() => void execute(Runtime.UpdateSubscription)}>更新已保存订阅</button>
          </div>
          <small>订阅地址保存在 macOS Keychain，界面不会回显。</small>
          {subscriptionURL.startsWith('http://') && <small className="http-note">此地址使用 HTTP，访问令牌会在网络上传输明文。</small>}
        </div>
      </section>
      <section className="card"><div className="card-head"><h3>系统代理</h3><span>{state.systemProxyEnabled ? '已接管' : '未接管'}</span></div>
        <div className="endpoint">127.0.0.1:{state.port}</div>
        <p>启动内核后可接管当前网络服务的 HTTP、HTTPS 和 SOCKS 代理。关闭时恢复启用前的设置。</p>
        <button className="proxy-button" disabled={busy || (!running && !state.systemProxyEnabled)} onClick={() => void execute(() => Runtime.SetSystemProxy(!state.systemProxyEnabled))}>{state.systemProxyEnabled ? '关闭系统代理' : '开启系统代理'}</button>
        <small className="proxy-note">同时使用 Clash Verge 时，两个应用可能争用系统代理设置。</small>
      </section>
    </div>
    <section className="card groups"><div className="card-head"><h3>策略组</h3><span>{groups.length} 个可选择</span></div>
      {!running && <p>启动内核后可以选择策略组节点。</p>}
      {running && groups.length === 0 && <p>当前配置没有手动选择的策略组。</p>}
      {groups.map(group => <div className="group" key={group.name}><div><strong>{group.name}</strong><small>当前：{group.current}</small></div>
        <select aria-label={`选择 ${group.name} 节点`} value={group.current} disabled={busy} onChange={e => void select(group.name, e.target.value)}>{(group.options ?? []).map(option => <option key={option} value={option}>{option}</option>)}</select></div>)}
    </section>
    <footer>关闭窗口后 Vela 留在菜单栏；选择“退出 Vela”会停止内核。</footer>
  </main>
}
