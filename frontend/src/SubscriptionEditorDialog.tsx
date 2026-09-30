import { useEffect, useRef, useState } from 'react'
import { LinkSimple, X } from '@phosphor-icons/react'
import { localizeError, translate, type Language } from './i18n'

type Props = {
  url: string
  domain: string
  language: Language
  busy: boolean
  error: string
  onClose: () => void
  onSave: (url: string) => Promise<void>
}

export default function SubscriptionEditorDialog({ url, domain, language, busy, error, onClose, onSave }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const [draft, setDraft] = useState(url)
  const t = (text: string) => translate(language, text)

  useEffect(() => {
    const element = dialog.current!
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    element.showModal()
    input.current?.focus()
    input.current?.setSelectionRange(0, 0)
    return () => {
      element.close()
      document.body.style.overflow = previousOverflow
    }
  }, [])

  return <dialog className="subscription-dialog" ref={dialog} aria-labelledby="subscription-dialog-title" aria-describedby="subscription-dialog-domain" onCancel={event => {
    event.preventDefault()
    if (!busy) onClose()
  }}>
    <div className="subscription-dialog-heading">
      <div className="module-icon"><LinkSimple size={24} aria-hidden="true" /></div>
      <div className="subscription-dialog-title">
        <h2 id="subscription-dialog-title">{t('编辑订阅地址')}</h2>
        <p id="subscription-dialog-domain" title={domain}>{domain}</p>
      </div>
      <button className="subscription-dialog-close" type="button" aria-label={t('关闭弹窗')} disabled={busy} onClick={onClose}><X size={18} aria-hidden="true" /></button>
    </div>
    <form onSubmit={event => {
      event.preventDefault()
      if (!busy && draft.trim()) void onSave(draft.trim())
    }} aria-busy={busy}>
      <label className="field-label" htmlFor="subscription-edit-url">{t('订阅链接')}</label>
      <input className="text-field" ref={input} id="subscription-edit-url" type="url" required autoComplete="off" spellCheck={false} value={draft} disabled={busy} aria-invalid={error ? true : undefined} aria-describedby={error ? 'subscription-edit-error' : undefined} onChange={event => setDraft(event.target.value)} />
      {error && <p className="subscription-dialog-error" id="subscription-edit-error" role="alert">{localizeError(language, error)}</p>}
      <div className="subscription-dialog-actions">
        <button className="secondary-button" type="button" disabled={busy} onClick={onClose}>{t('取消')}</button>
        <button className="primary-button" type="submit" disabled={busy || !draft.trim()}>{t(busy ? '保存并更新中…' : '保存并更新')}</button>
      </div>
    </form>
  </dialog>
}
