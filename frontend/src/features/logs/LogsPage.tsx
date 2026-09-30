import { ListBullets } from '@phosphor-icons/react'
import { translate, localizeError, type Language } from '../../i18n'
import { logLevels, type LogsController } from './useLogs'

type Props = {
  controller: LogsController
  language: Language
}

export default function LogsPage({ controller, language }: Props) {
  const { logsError, selectedLogLevel, logList, followLogs, logLines, visibleLogLines, selectLogLevel } = controller
  const t = (text: string) => translate(language, text)
  return <>
    <div className="page-heading"><h1>{t('日志')}</h1></div>
    {logsError && <div className="alert" role="alert">{localizeError(language, logsError)}</div>}
    <section className="panel page-panel logs-panel" aria-label={t('内核日志')}>
      <div className="logs-toolbar"><label htmlFor="logs-level">{t('日志级别')}</label><select id="logs-level" value={selectedLogLevel} onChange={event => selectLogLevel(event.target.value)}>{logLevels.map(level => <option key={level.value} value={level.value}>{t(level.label)}</option>)}</select></div>
      {logLines.length === 0 ? <div className="empty-state"><ListBullets size={42} weight="light" /><h3>{t('暂无日志')}</h3><p>{t('连接内核后，日志会显示在这里。')}</p></div>
        : visibleLogLines.length === 0 ? <div className="empty-state"><h3>{t('没有符合当前级别的日志')}</h3></div>
          : <div className="log-list" ref={logList} role="log" aria-live="off" onScroll={event => { const list = event.currentTarget; followLogs.current = list.scrollHeight - list.scrollTop - list.clientHeight < 32 }}>{visibleLogLines.map((entry, index) => <div className="log-entry" key={index}><span className={`log-level log-level-${entry.level}`}>{t(logLevels.find(level => level.value === entry.level)?.label ?? '其他')}</span><code>{entry.line}</code></div>)}</div>}
    </section>
  </>
}
