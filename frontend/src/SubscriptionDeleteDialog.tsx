import { useEffect, useRef } from 'react'
import { Trash, X } from '@phosphor-icons/react'
import { localizeError, translate, type Language } from './i18n'

type Props = {
  domain: string
  language: Language
  busy: boolean
  error: string
  onClose: () => void
  onConfirm: () => Promise<void>
}

export default function SubscriptionDeleteDialog({ domain, language, busy, error, onClose, onConfirm }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const cancel = useRef<HTMLButtonElement>(null)
  const t = (text: string) => translate(language, text)

  useEffect(() => {
    const element = dialog.current!
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    element.showModal()
    cancel.current?.focus()
    return () => {
      element.close()
      document.body.style.overflow = previousOverflow
    }
  }, [])

  return <dialog className="subscription-dialog" ref={dialog} aria-labelledby="subscription-delete-title" aria-describedby="subscription-delete-domain subscription-delete-description" onCancel={event => {
    event.preventDefault()
    if (!busy) onClose()
  }}>
    <div className="subscription-dialog-heading">
      <div className="module-icon"><Trash size={24} aria-hidden="true" /></div>
      <div className="subscription-dialog-title">
        <h2 id="subscription-delete-title">{t('删除订阅')}</h2>
        <p id="subscription-delete-domain" title={domain}>{domain}</p>
      </div>
      <button className="subscription-dialog-close" type="button" aria-label={t('关闭弹窗')} disabled={busy} onClick={onClose}><X size={18} aria-hidden="true" /></button>
    </div>
    <p className="subscription-dialog-description" id="subscription-delete-description">{t('确定删除此订阅？当前配置会保留。')}</p>
    <form onSubmit={event => {
      event.preventDefault()
      if (!busy) void onConfirm()
    }} aria-busy={busy}>
      {error && <p className="subscription-dialog-error" role="alert">{localizeError(language, error)}</p>}
      <div className="subscription-dialog-actions">
        <button className="secondary-button" ref={cancel} type="button" disabled={busy} onClick={onClose}>{t('取消')}</button>
        <button className="primary-button" type="submit" disabled={busy}>{t(busy ? '正在删除…' : '删除订阅')}</button>
      </div>
    </form>
  </dialog>
}
