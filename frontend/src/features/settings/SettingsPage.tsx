import { FolderOpen } from '@phosphor-icons/react'
import { translate, localizeError } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import { routingModes } from '../../app/routing'
import type { SettingsController } from './useSettings'

type Props = {
  runtime: RuntimeController
  controller: SettingsController
}

export default function SettingsPage({ runtime, controller }: Props) {
  const { state, notice, controlsBusy, connected, setRoutingMode } = runtime
  const {
    settings, language, coreInfo, coreInfoError, portDraft, setPortDraft, openConfigDirectory, saveMixedPort,
    setLanguage, setLaunchAtLogin, setAutoConnect, setAutoConnectMode, setSubscriptionUpdateHours, setLogLevel,
  } = controller
  const t = (text: string) => translate(language, text)
  return <>
    <div className="page-heading"><h1>{t('设置')}</h1></div>
    {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
    <div className="settings-stack">
      <section className="panel settings-card" aria-labelledby="language-settings-title">
        <h2 id="language-settings-title">{t('语言')}</h2>
        <label className="setting-row" htmlFor="interface-language"><span><strong>{t('界面语言')}</strong><small>{t('切换后立即生效')}</small></span><select id="interface-language" value={language} disabled={controlsBusy} onChange={event => void setLanguage(event.target.value)}><option value="zh-CN">简体中文</option><option value="en-US">English</option></select></label>
      </section>
      <section className="panel settings-card" aria-labelledby="startup-settings-title">
        <h2 id="startup-settings-title">{t('启动')}</h2>
        <button className={`setting-row setting-switch${settings.launchAtLogin ? ' on' : ''}`} type="button" role="switch" aria-checked={settings.launchAtLogin} disabled={controlsBusy} onClick={() => void setLaunchAtLogin(!settings.launchAtLogin)}><span><strong>{t('登录时启动')}</strong><small>{t('登录 macOS 后打开 Vela')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
        <button className={`setting-row setting-switch${settings.autoConnect ? ' on' : ''}`} type="button" role="switch" aria-checked={settings.autoConnect} disabled={controlsBusy} onClick={() => void setAutoConnect(!settings.autoConnect)}><span><strong>{t('启动后自动连接')}</strong><small>{t('有可用配置时，在 Vela 启动后连接')}</small></span><span className="switch-track"><span className="switch-knob" /></span></button>
        <label className="setting-row" htmlFor="auto-connect-mode"><span><strong>{t('自动连接方式')}</strong><small>{t('下次启动时使用')}</small></span><select id="auto-connect-mode" value={settings.autoConnectMode} disabled={controlsBusy} onChange={event => void setAutoConnectMode(event.target.value)}><option value="system">{t('系统代理')}</option><option value="tun" disabled={!state.tunSupported}>{t('Tun 模式')}</option></select></label>
        <label className="setting-row" htmlFor="subscription-update-hours"><span><strong>{t('订阅自动更新')}</strong><small>{t('自动刷新已保存的订阅')}</small></span><select id="subscription-update-hours" value={settings.subscriptionUpdateHours} disabled={controlsBusy} onChange={event => void setSubscriptionUpdateHours(Number(event.target.value))}><option value={0}>{t('关闭')}</option><option value={6}>{t('每 6 小时')}</option><option value={12}>{t('每 12 小时')}</option><option value={24}>{t('每 24 小时')}</option></select></label>
      </section>
      <section className="panel settings-card" aria-labelledby="proxy-settings-title">
        <h2 id="proxy-settings-title">{t('代理设置')}</h2>
        <fieldset className="settings-routing" disabled={controlsBusy}><legend>{t('代理模式')}</legend><div className="routing-options">{routingModes.map(option => <label className={`routing-option${state.routingMode === option.value ? ' selected' : ''}`} key={option.value}><input type="radio" name="settings-routing-mode" value={option.value} checked={state.routingMode === option.value} onChange={() => void setRoutingMode(option.value)} /><span><strong>{t(option.label)}</strong><small>{t(option.description)}</small></span></label>)}</div></fieldset>
        <div className="setting-row port-setting"><label htmlFor="mixed-port"><strong>{t('本地代理端口')}</strong>{connected && <small>{t('保存后将按当前网络设置重新连接')}</small>}</label><div className="port-editor"><div className="port-field"><span className="port-address">127.0.0.1:</span><input id="mixed-port" type="number" min="1024" max="65535" step="1" inputMode="numeric" value={portDraft} disabled={controlsBusy} onChange={event => setPortDraft(event.target.value)} /></div><button className="secondary-button" type="button" disabled={controlsBusy || portDraft === String(state.port)} onClick={() => void saveMixedPort()}>{t('保存')}</button></div></div>
      </section>
      <section className="panel settings-card" aria-labelledby="core-settings-title">
        <h2 id="core-settings-title">{t('内核')}</h2>
        <label className="setting-row" htmlFor="log-level"><span><strong>{t('日志级别')}</strong><small>{t('重新载入配置或下次连接时生效')}</small></span><select id="log-level" value={settings.logLevel} disabled={controlsBusy} onChange={event => void setLogLevel(event.target.value)}><option value="profile">{t('跟随配置')}</option><option value="silent">{t('静默')}</option><option value="error">{t('错误')}</option><option value="warning">{t('警告')}</option><option value="info">{t('信息日志')}</option><option value="debug">{t('调试')}</option></select></label>
      </section>
      <section className="panel settings-card" aria-labelledby="info-settings-title">
        <h2 id="info-settings-title">{t('信息')}</h2>
        <div className="setting-row"><span><strong>{t('内核信息')}</strong></span><span className="setting-value" title={coreInfoError ? localizeError(language, coreInfoError) : undefined}>{coreInfo ? `${coreInfo.name} ${coreInfo.version}` : coreInfoError ? t('无法读取') : t('读取中…')}</span></div>
        <div className="setting-row"><span><strong>{t('配置目录')}</strong></span><button className="secondary-button" type="button" onClick={() => void openConfigDirectory()}>{t('打开目录')} <FolderOpen size={16} /></button></div>
      </section>
    </div>
  </>
}
