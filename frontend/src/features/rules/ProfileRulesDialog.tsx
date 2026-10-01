import { useEffect, useRef, useState } from 'react'
import { X } from '@phosphor-icons/react'
import { translate, type Language } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import { useRules } from './useRules'
import RulesPage from './RulesPage'

type Props = { id: string; title: string; runtime: RuntimeController; language: Language; visible: boolean; onClose: () => void }

export default function ProfileRulesDialog({ id, title, runtime, language, visible, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null)
  const controller = useRules(runtime, id, visible)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const t = (text: string) => translate(language, text)
  const close = () => {
    if (runtime.controlsBusy) return
    if (controller.dirty) setConfirmDiscard(true)
    else onClose()
  }
  useEffect(() => {
    const element = dialog.current!
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    element.showModal()
    element.focus({ preventScroll: true })
    return () => { element.close(); document.body.style.overflow = previousOverflow }
  }, [])
  return <dialog className="subscription-dialog profile-rules-dialog" ref={dialog} tabIndex={-1} aria-labelledby="profile-rules-title" onCancel={event => { event.preventDefault(); close() }}>
    <div className="subscription-dialog-heading">
      <div className="subscription-dialog-title"><h2 id="profile-rules-title">{t('编辑配置')}</h2><p title={title}>{id === '' ? t('本地配置') : title}</p></div>
      <button className="subscription-dialog-close" type="button" aria-label={t('关闭弹窗')} disabled={runtime.controlsBusy} onClick={close}><X size={18} aria-hidden="true" /></button>
    </div>
    {confirmDiscard && controller.dirty && <div className="rules-discard-confirm" role="alert"><p>{t('有未保存的规则修改，关闭将放弃这些修改。')}</p><div><button className="secondary-button" type="button" disabled={runtime.controlsBusy} onClick={() => setConfirmDiscard(false)}>{t('继续编辑')}</button><button className="secondary-button" type="button" disabled={runtime.controlsBusy} onClick={onClose}>{t('放弃修改并关闭')}</button></div></div>}
    <RulesPage runtime={runtime} controller={controller} language={language} local={id === ''} />
  </dialog>
}
