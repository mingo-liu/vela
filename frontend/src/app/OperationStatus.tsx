import type { OperationProgress } from '../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { translate, type Language } from '../i18n'

type Props = {
  operation: OperationProgress | null
  language: Language
  cancelling: boolean
  onCancel: (id: number) => Promise<void>
}

export default function OperationStatus({ operation, language, cancelling, onCancel }: Props) {
  if (!operation?.active) return null
  const t = (text: string) => translate(language, text)
  const delay = operation.kind === 'delay'
  const determinate = delay && operation.total > 0
  const label = cancelling || operation.phase === 'cancelling' ? '正在取消…'
    : operation.phase === 'applying' ? '正在应用配置…'
      : delay ? '正在测试节点延迟…' : '正在获取更新…'
  return <section className="operation-status" aria-label={t('任务进度')}>
    <div className="operation-description"><strong>{t(delay ? '节点测速' : '订阅下载')}{delay && operation.target ? ` · ${operation.target}` : ''}</strong><span role="status">{t(label)}</span></div>
    {determinate && <span className="operation-count">{operation.completed} / {operation.total}</span>}
    <button className="secondary-button" type="button" disabled={!operation.cancellable || cancelling} onClick={() => void onCancel(operation.id)}>{t('取消任务')}</button>
    <progress aria-label={t(delay ? '测速进度' : '下载进度')} max={determinate ? operation.total : undefined} value={determinate ? operation.completed : undefined} />
  </section>
}
