import { useEffect, useState } from 'react'
import { ArrowClockwise, Eye, EyeSlash, MapPin } from '@phosphor-icons/react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { ExitIPInfo, Group } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import { localizeError, translate, type Language } from '../../i18n'
import { usePolling } from '../../lib/usePolling'

const REFRESH_SECONDS = 300

function display(value: string | undefined, fallback: string) {
  return value?.trim() || fallback
}

export default function ExitIPCard({ connected, groups, language, profileRevision, routingMode, visible }: {
  connected: boolean
  visible: boolean
  groups: Group[]
  language: Language
  profileRevision: number
  routingMode: string
}) {
  const t = (value: string) => translate(language, value)
  const selectedGroups = routingMode === 'global'
    ? groups.filter(group => group.name === 'GLOBAL')
    : groups.filter(group => group.name !== 'GLOBAL')
  const selection = routingMode === 'direct' ? t('直连')
    : selectedGroups.length ? selectedGroups.map(group => `${group.name} · ${group.current || t('未选择')}`).join(' / ')
      : t('未选择')
  const [info, setInfo] = useState<ExitIPInfo | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [hidden, setHidden] = useState(false)
  const [flagFailed, setFlagFailed] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)
  const [nextRefresh, setNextRefresh] = useState(0)
  const [now, setNow] = useState(Date.now())

  useEffect(() => setFlagFailed(false), [info?.countryCode])

  useEffect(() => {
    setInfo(null)
    setError('')
    setLoading(false)
    setNextRefresh(0)
  }, [connected, profileRevision, routingMode, selection])

  usePolling(async isCurrent => {
    setLoading(true)
    try {
      const result = await Runtime.ExitIPInfo()
      if (isCurrent()) { setInfo(result); setError('') }
    } catch (cause) {
      if (isCurrent()) {
        setInfo(null)
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    } finally {
      if (isCurrent()) { setLoading(false); setNextRefresh(Date.now() + REFRESH_SECONDS * 1000) }
    }
  }, connected && visible ? REFRESH_SECONDS * 1000 : null, JSON.stringify([profileRevision, refreshKey, routingMode, selection]))

  usePolling(async isCurrent => {
    if (isCurrent()) setNow(Date.now())
  }, connected && visible ? 1000 : null)

  const remaining = Math.max(0, Math.ceil((nextRefresh - now) / 1000))
  const missing = t('未提供')
  const location = [info?.city, info?.region].filter(Boolean).join(', ')
  const coordinates = info ? `${info.latitude.toFixed(2)}, ${info.longitude.toFixed(2)}` : ''
  const flagCode = info && /^[a-z]{2}$/i.test(info.countryCode) ? info.countryCode.toLowerCase() : ''

  return <section className="panel exit-ip-card" aria-labelledby="exit-ip-title">
    <div className="exit-ip-heading">
      <div className="exit-ip-heading-label"><span className="exit-ip-icon"><MapPin size={26} weight="regular" /></span><div><h2 id="exit-ip-title">{t('出口 IP 信息')}</h2><p title={selection}>{t('当前选择')}：{selection}</p></div></div>
      <button className="exit-ip-action" type="button" aria-label={t('刷新 IP 信息')} title={t('刷新 IP 信息')} disabled={!connected || loading} onClick={() => setRefreshKey(value => value + 1)}><ArrowClockwise size={24} /></button>
    </div>
    {!connected ? <div className="exit-ip-empty">{t('连接代理后显示出口 IP 信息')}</div> : error ? <div className="exit-ip-empty" role="status">{t('IP 信息查询失败')}：{localizeError(language, error)}</div> : loading && !info ? <div className="exit-ip-empty" role="status">{t('正在查询出口 IP…')}</div> : info ? <>
      <div className="exit-ip-body">
        <div className="exit-ip-primary">
          <strong className="exit-ip-country">{flagCode && !flagFailed ? <img className="exit-ip-flag" src={`https://cdn.ipwhois.io/flags/${flagCode}.svg`} alt="" onError={() => setFlagFailed(true)} /> : <span className="exit-ip-country-code">{display(info.countryCode, '--')}</span>}{display(info.country, missing)}</strong>
          <div className="exit-ip-line"><span>IP</span><code>{hidden ? '•••.•••.•••.•••' : info.ip}</code><button className="exit-ip-visibility" type="button" aria-label={t(hidden ? '显示 IP 地址' : '隐藏 IP 地址')} title={t(hidden ? '显示 IP 地址' : '隐藏 IP 地址')} onClick={() => setHidden(value => !value)}>{hidden ? <EyeSlash size={19} /> : <Eye size={19} />}</button></div>
          <div className="exit-ip-line"><span>ASN</span><span>{info.asn ? `AS${info.asn}` : missing}</span></div>
        </div>
        <dl className="exit-ip-details">
          <div><dt>ISP</dt><dd>{display(info.isp, missing)}</dd></div>
          <div><dt>ORG</dt><dd>{display(info.organization, missing)}</dd></div>
          <div><dt>{t('位置')}</dt><dd>{display(location, missing)}</dd></div>
          <div><dt>{t('时区')}</dt><dd>{display(info.timezone, missing)}</dd></div>
        </dl>
      </div>
      <div className="exit-ip-footer"><span>{t('自动刷新')}：{remaining}s</span><span>{info.countryCode}{coordinates && `, ${coordinates}`}</span></div>
    </> : null}
  </section>
}
