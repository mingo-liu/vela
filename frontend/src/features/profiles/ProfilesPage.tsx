import { ArrowClockwise, DownloadSimple, FileArrowDown, FileText, LinkSimple, PencilSimple, Trash } from '@phosphor-icons/react'
import { translate, localizeError, type Language } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import type { ProfilesController } from './useProfiles'
import { formatBytes, formatDate, subscriptionDomain } from './format'

type Props = {
  runtime: RuntimeController
  controller: ProfilesController
  language: Language
}

export default function ProfilesPage({ runtime, controller, language }: Props) {
  const { state, notice, controlsBusy, operation, running } = runtime
  const {
    subscriptions, subscriptionURL, setSubscriptionURL, fileInput,
    openEditor, openDeleteDialog, importFile, importSubscription,
    updateSubscription, selectSubscription,
  } = controller
  const t = (text: string) => translate(language, text)
  return <>
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
            const updating = operation?.active && operation.kind === 'subscription' && operation.target === subscription.id
            const refreshLabel = updating ? t(operation.phase === 'cancelling' ? '正在取消…' : operation.phase === 'applying' ? '正在应用配置…' : '正在获取更新…') : t('更新此订阅')
            return <article className={`panel subscription-card${subscription.active ? ' selected' : ''}`} key={subscription.id}>
              <div className="subscription-card-top">
                <button className={`subscription-refresh${updating ? ' updating' : ''}`} type="button" disabled={controlsBusy} aria-busy={updating || undefined} aria-label={refreshLabel} title={refreshLabel} onClick={() => void updateSubscription(subscription.id)}><ArrowClockwise size={17} /></button>
                <button className="subscription-refresh" type="button" disabled={controlsBusy} aria-label={t('编辑订阅地址')} title={t('编辑订阅地址')} onClick={() => openEditor(subscription.id)}><PencilSimple size={17} /></button>
                <button className="subscription-refresh subscription-delete" type="button" disabled={controlsBusy} aria-label={t('删除订阅')} title={t('删除订阅')} onClick={() => openDeleteDialog(subscription.id)}><Trash size={17} /></button>
              </div>
              <button className="subscription-select" type="button" aria-label={`${subscription.active ? t('当前订阅') : t('选择订阅')} ${domain}`} aria-pressed={subscription.active} disabled={controlsBusy || subscription.active} onClick={() => void selectSubscription(subscription.id)}>
                <span className="subscription-url" title={domain}><LinkSimple size={15} /><span>{domain}</span></span>
                <span className="subscription-usage"><span>{t('剩余')} <strong>{formatBytes(remaining, language)}</strong></span><span>{t('总量')} <strong>{formatBytes(subscription.total, language)}</strong></span></span>
                <span className="subscription-dates"><span>{t('到期')} {formatDate(subscription.expiresAt, language)}</span><span>{t('更新')} {formatDate(subscription.updatedAt, language)}</span></span>
              </button>
              {subscription.lastUpdateError && <small className="subscription-error" title={localizeError(language, subscription.lastUpdateError)}>{t('自动更新失败')}：{localizeError(language, subscription.lastUpdateError)}</small>}
            </article>
          })}
        </div>
      </section>
      <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><FileText size={24} /></div><div><h2>{t('本地配置')}</h2></div></div><button className="primary-button import-button file-button" type="button" disabled={controlsBusy || running} onClick={() => fileInput.current?.click()}>{t('选择 YAML 文件')} <FileArrowDown size={18} /></button><input ref={fileInput} className="file-input" type="file" accept=".yaml,.yml,text/yaml" tabIndex={-1} onChange={e => { void importFile(e.target.files?.[0]); e.target.value = '' }} /></section>
      <section className="panel profile-card"><div className="panel-heading"><div className="module-icon"><LinkSimple size={24} /></div><div><h2>{t('导入订阅')}</h2></div></div><label className="field-label" htmlFor="subscription-url">{t('订阅链接')}</label><input className="text-field" id="subscription-url" type="url" value={subscriptionURL} disabled={controlsBusy} autoComplete="off" spellCheck={false} placeholder={t('粘贴 HTTP / HTTPS 订阅地址')} onChange={e => setSubscriptionURL(e.target.value)} /><div className="profile-actions"><button className="primary-button import-button" type="button" disabled={controlsBusy || !subscriptionURL} onClick={() => void importSubscription()}>{t('导入订阅')} <DownloadSimple size={18} /></button></div>{subscriptionURL.startsWith('http://') && <small className="http-note">{t('此地址使用 HTTP，访问令牌会在网络上传输明文。')}</small>}</section>
    </div>
  </>
}
