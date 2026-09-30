import { useEffect, useState } from 'react'
import { ArrowClockwise } from '@phosphor-icons/react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { ConnectionSnapshot, Rule } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { localizeError, translate, type Language } from '../../i18n'
import { usePolling } from '../../lib/usePolling'

function bytes(value: number): string {
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)) - 1, units.length - 1)
  return `${(value / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`
}

export default function Diagnostics({ connected, language, profileRevision, visible }: { connected: boolean; language: Language; profileRevision: number; visible: boolean }) {
  const t = (value: string) => translate(language, value)
  const [tab, setTab] = useState<'connections' | 'rules'>('connections')
  const [snapshot, setSnapshot] = useState<ConnectionSnapshot | null>(null)
  const [rules, setRules] = useState<Rule[]>([])
  const [filter, setFilter] = useState('')
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    if (!connected) { setSnapshot(null); setRules([]); setError('') }
  }, [connected])

  usePolling(async isCurrent => {
    try {
      if (tab === 'connections') {
        const value = await Runtime.Connections()
        if (isCurrent()) setSnapshot(value)
      } else {
        const value = await Runtime.Rules()
        if (isCurrent()) setRules(value ?? [])
      }
      if (isCurrent()) setError('')
    } catch (cause) {
      if (isCurrent()) setError(cause instanceof Error ? cause.message : String(cause))
    }
  }, !connected || !visible ? null : tab === 'connections' ? 3000 : 0, `${tab}:${profileRevision}:${refreshKey}`)

  const query = filter.trim().toLowerCase()
  const connections = (snapshot?.connections ?? []).filter(item => {
    const metadata = item.metadata
    return !query || [metadata.host, metadata.destinationIP, metadata.process, metadata.processPath, item.rule, item.rulePayload, ...(item.chains ?? [])].some(value => value?.toLowerCase().includes(query))
  })
  const visibleRules = rules.filter(item => !query || [item.type, item.payload, item.proxy].some(value => value.toLowerCase().includes(query)))

  return <>
      <div className="page-heading"><h1>{t('诊断')}</h1></div>
      <section className="panel page-panel diagnostics-panel">
        <div className="diagnostics-toolbar">
          <div className="diagnostics-tabs" role="tablist" aria-label={t('诊断视图')}>
            <button type="button" role="tab" aria-selected={tab === 'connections'} className={tab === 'connections' ? 'active' : ''} onClick={() => setTab('connections')}>{t('连接')}</button>
            <button type="button" role="tab" aria-selected={tab === 'rules'} className={tab === 'rules' ? 'active' : ''} onClick={() => setTab('rules')}>{t('规则列表')}</button>
          </div>
          <div className="diagnostics-actions"><input className="text-field" type="search" aria-label={t('筛选连接或规则')} placeholder={t('筛选连接或规则')} value={filter} onChange={event => setFilter(event.target.value)} /><button className="secondary-button" type="button" disabled={!connected} onClick={() => setRefreshKey(value => value + 1)}><ArrowClockwise size={16} />{t('刷新')}</button></div>
        </div>
        {!connected ? <div className="empty-state"><h3>{t('连接代理后查看诊断信息')}</h3></div> : <>
          {error && <div className="alert" role="alert">{localizeError(language, error)}</div>}
          {tab === 'connections' ? <>
            <div className="diagnostics-summary"><span>{t('活动连接')}：<strong>{snapshot?.total ?? 0}</strong></span><span>{t('代理上传')}：<strong>{bytes(snapshot?.uploadTotal ?? 0)}</strong></span><span>{t('代理下载')}：<strong>{bytes(snapshot?.downloadTotal ?? 0)}</strong></span></div>
            {(snapshot?.total ?? 0) > 500 && <p className="diagnostics-note">{t('仅显示前 500 条连接')}</p>}
            {connections.length === 0 ? <div className="empty-state"><h3>{t('暂无匹配的活动连接')}</h3></div> : <div className="diagnostics-list">{connections.map(item => <article className="diagnostics-row" key={item.id}>
              <div><strong>{item.metadata.host || item.metadata.destinationIP || t('未知目标')}</strong><small>{item.metadata.destinationIP}{item.metadata.destinationPort ? `:${item.metadata.destinationPort}` : ''} · {item.metadata.network || 'TCP'}</small></div>
              <div><span>{item.metadata.process || item.metadata.processPath || t('未知应用')}</span><small>{item.rule}{item.rulePayload ? ` · ${item.rulePayload}` : ''}</small></div>
              <div><span>{(item.chains ?? []).join(' → ') || t('直连')}</span><small>↑ {bytes(item.upload)} · ↓ {bytes(item.download)}</small></div>
            </article>)}</div>}
          </> : <>
            <div className="diagnostics-summary"><span>{t('规则总数')}：<strong>{rules.length}</strong></span></div>
            {visibleRules.length === 0 ? <div className="empty-state"><h3>{t('暂无匹配的规则')}</h3></div> : <div className="diagnostics-list">{visibleRules.map(item => <div className="diagnostics-row diagnostics-rule" key={item.index}><span className="diagnostics-index">{item.index + 1}</span><strong>{item.type}</strong><span title={item.payload}>{item.payload || '—'}</span><span>{item.proxy}</span></div>)}</div>}
          </>}
        </>}
      </section>
  </>
}
