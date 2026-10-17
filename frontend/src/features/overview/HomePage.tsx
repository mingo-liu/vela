import { ArrowRight, GlobeHemisphereWest, Info, Stack } from '@phosphor-icons/react'
import { translate, localizeError, type Language } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import type { Page } from '../../app/types'
import type { Group } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { routingModes } from '../../app/routing'
import ExitIPCard from './ExitIPCard'

type Props = {
  runtime: RuntimeController
  groups: Group[]
  language: Language
  visible: boolean
  onNavigate: (page: Page) => void
}

export default function HomePage({ runtime, groups, language, visible, onNavigate }: Props) {
  const { state, notice, controlsBusy, operation, running, connected, profileRevision, setSystemProxy, setTun, setRoutingMode } = runtime
  const t = (text: string) => translate(language, text)
  const selectedRoutingMode = routingModes.find(option => option.value === state.routingMode) ?? routingModes[0]
  return <>
    <div className="page-heading"><h1>{t('首页')}</h1></div>
    {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
    <section className="panel connection-panel" aria-labelledby="connection-title">
      <div className="connection-main"><div><h2 id="connection-title">{connected ? t('连接已就绪') : t('准备开始连接')}</h2></div></div>
      <h3 className="connection-setting-title">{t('网络设置')}</h3>
      <div className="connection-modes" aria-label={t('网络设置')}>
        <button className={`mode-option${state.systemProxyEnabled ? ' on' : ''}`} type="button" role="switch" aria-label={t('系统代理')} aria-checked={state.systemProxyEnabled} disabled={(controlsBusy && !(state.systemProxyEnabled && operation?.cancellable)) || (!state.hasProfile && !state.systemProxyEnabled)} onClick={() => void setSystemProxy(!state.systemProxyEnabled)}><span><strong>{t('系统代理')}</strong></span><span className="switch-track"><span className="switch-knob" /></span></button>
        <button className={`mode-option${state.tunEnabled ? ' on' : ''}`} type="button" role="switch" aria-label={t('Tun 模式')} aria-checked={state.tunEnabled} disabled={(controlsBusy && !(state.tunEnabled && operation?.cancellable)) || !state.tunSupported || (!state.hasProfile && !state.tunEnabled)} onClick={() => void setTun(!state.tunEnabled)}><span><strong>{t('Tun 模式')}</strong></span><span className="switch-track"><span className="switch-knob" /></span></button>
      </div>
      <fieldset className="routing-settings" disabled={controlsBusy} aria-describedby="routing-description">
        <legend className="connection-setting-title">{t('代理模式')}</legend>
        <div className="routing-options">{routingModes.map(option => <label className={`routing-option${state.routingMode === option.value ? ' selected' : ''}`} key={option.value}>
          <input type="radio" name="routing-mode" value={option.value} checked={state.routingMode === option.value} onChange={() => void setRoutingMode(option.value)} />
          <span><strong>{t(option.label)}</strong></span>
        </label>)}</div>
        <p className="routing-description" id="routing-description" role="status">
          <Info size={17} aria-hidden="true" />
          <span><strong>{t(selectedRoutingMode.label)}</strong><span>{t(selectedRoutingMode.description)}</span></span>
        </p>
      </fieldset>
      {!state.hasProfile && <button type="button" className="inline-link" onClick={() => onNavigate('profiles')}>{t('先导入配置以启用连接')} <ArrowRight size={17} /></button>}
      <div className="connection-meta"><div><span>{t('本地代理')}</span><strong>127.0.0.1:{state.port}</strong></div><div><span>{t('内核状态')}</span><strong>{running ? t('运行中') : state.status === 'starting' ? t('启动中') : t('已停止')}</strong></div><div><span>{t('配置文件')}</span><strong>{state.hasProfile ? t('已导入') : t('未导入')}</strong></div></div>
    </section>
    <ExitIPCard visible={visible} connected={connected && running} groups={groups} language={language} profileRevision={profileRevision} routingMode={state.routingMode} />
    <div className="module-grid">
      <section className="panel module-card"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h3>{t('代理节点')}</h3></div><button className="module-link" type="button" onClick={() => onNavigate('proxies')}>{t('查看代理')} <ArrowRight size={17} /></button></section>
      <section className="panel module-card"><div className="module-icon"><Stack size={24} /></div><div><h3>{t('配置与订阅')}</h3></div><button className="module-link" type="button" onClick={() => onNavigate('profiles')}>{t('查看配置')} <ArrowRight size={17} /></button></section>
    </div>
  </>
}
