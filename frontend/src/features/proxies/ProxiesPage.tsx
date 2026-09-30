import { ArrowRight, CaretDown, CheckCircle, GlobeHemisphereWest, WifiMedium } from '@phosphor-icons/react'
import { translate, localizeError, type Language } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import type { Page } from '../../app/types'
import type { ProxiesController, SortMode } from './useProxies'

function SortModeIcon({ mode }: { mode: SortMode }) {
  return <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {mode === 'name' ? <>
      <path d="M2.5 11 7 3l4.5 8M4.2 8h5.6" />
      <path d="M2.5 14h9l-9 7h9" />
    </> : <>
      <path d="M5.5 3h4M7.5 3v2.5" />
      <circle cx="7.5" cy="12" r="6.5" />
      <path d="M7.5 8.5V12l2.4-2.4" />
    </>}
    <path d="M19 5v14m-3-3 3 3 3-3" />
  </svg>
}

type Props = {
  runtime: RuntimeController
  controller: ProxiesController
  language: Language
  onNavigate: (page: Page) => void
}

export default function ProxiesPage({ runtime, controller, language, onNavigate }: Props) {
  const { state, notice, controlsBusy } = runtime
  const { groups, nodeNames, expandedGroups, groupDelays, measuredGroups, testingGroup, sortModes, select, toggleGroup, testGroupDelay, toggleGroupSort } = controller
  const t = (text: string) => translate(language, text)
  return <>
    <div className="page-heading"><h1>{t('代理')}</h1></div>
    {(notice || state.error) && <div className="alert" role="alert">{localizeError(language, notice || state.error)}</div>}
    <section className="panel page-panel proxies-panel"><div className="panel-heading"><div className="module-icon"><GlobeHemisphereWest size={24} /></div><div><h2>{t('策略组')}</h2></div></div>
      {!state.hasProfile && <div className="empty-state"><GlobeHemisphereWest size={42} weight="light" /><h3>{t('尚无配置')}</h3><p>{t('前往配置页导入订阅或本地配置。')}</p><button className="secondary-button" type="button" onClick={() => onNavigate('profiles')}>{t('前往配置页')} <ArrowRight size={16} /></button></div>}
      {state.hasProfile && groups.length === 0 && nodeNames.length === 0 && <div className="empty-state"><CheckCircle size={42} weight="light" /><h3>{t('没有可选节点')}</h3><p>{t('当前配置未提供代理节点或手动策略组。')}</p></div>}
      {state.hasProfile && groups.length === 0 && nodeNames.length > 0 && <p className="no-groups">{t('当前配置没有手动策略组，下方列出订阅节点。')}</p>}
      <div className="proxy-group-stack">{groups.map((group, index) => {
        const expanded = expandedGroups[group.name] ?? index === 0
        const sortMode = sortModes[group.name] ?? 'name'
        const delays = groupDelays[group.name] ?? {}
        const options = [...(group.options ?? [])]
        if (sortMode === 'name') options.sort((a, b) => a.localeCompare(b, language, { numeric: true }))
        if (sortMode === 'delay') options.sort((a, b) => (delays[a] ?? Infinity) - (delays[b] ?? Infinity) || a.localeCompare(b, language, { numeric: true }))
        return <section className="proxy-group" key={group.name} aria-label={`${group.name} ${t('策略组')}`}>
          <div className="proxy-group-header">
            <button className="proxy-group-toggle" type="button" aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}>
              <span className="proxy-group-title"><strong>{group.name}</strong><small><span className="proxy-kind">{t('手动选择')}</span><span>{group.current || t('未选择')}</span></small></span>
            </button>
            <div className="proxy-group-actions">
              <button className={`proxy-group-action signal${testingGroup === group.name ? ' testing' : ''}`} type="button" aria-label={`${t('测试节点延迟')}: ${group.name}`} title={t('测延迟')} disabled={controlsBusy || testingGroup !== null} onClick={() => void testGroupDelay(group.name)}><WifiMedium size={30} weight="bold" aria-hidden="true" /></button>
              <button className="proxy-group-action sort" type="button" aria-label={`${group.name}: ${t(sortMode === 'name' ? '当前按名称排序，点击按延迟排序' : '当前按延迟排序，点击按名称排序')}`} title={sortMode === 'name' ? t('当前按名称排序，点击按延迟排序') : t('当前按延迟排序，点击按名称排序')} onClick={() => toggleGroupSort(group.name)}><SortModeIcon mode={sortMode} /></button>
            </div>
            <button className="proxy-group-expand" type="button" aria-label={`${expanded ? t('收起') : t('展开')} ${group.name}`} aria-expanded={expanded} aria-controls={`proxy-group-${index}`} onClick={() => toggleGroup(group.name, index === 0)}><span>{group.options?.length ?? 0} {t('个节点')}</span><CaretDown size={20} className={expanded ? 'expanded' : ''} /></button>
          </div>
          {expanded && <div className="proxy-node-grid" id={`proxy-group-${index}`}>{options.map(option => <button className={`proxy-node${option === group.current ? ' selected' : ''}`} type="button" key={option} aria-pressed={option === group.current} disabled={controlsBusy} onClick={() => void select(group.name, option)}><strong title={option}>{option}</strong><span className="proxy-node-meta"><span className="proxy-node-kind">{groups.some(candidate => candidate.name === option) ? t('策略组') : nodeNames.includes(option) ? t('代理节点') : t('内置节点')}</span><span className={`proxy-node-delay${testingGroup === group.name ? ' testing' : delays[option] ? ' measured' : ''}`}>{testingGroup === group.name ? <>{t('测速中')}<span className="delay-dots" aria-hidden="true"><i /><i /><i /></span></> : delays[option] ? `${delays[option]} ms` : measuredGroups[group.name] ? t('失败') : t('未测速')}</span></span></button>)}</div>}
        </section>
      })}</div>
      {groups.length === 0 && nodeNames.length > 0 && <div className="profile-nodes"><h3>{t('当前配置节点')} <small>{nodeNames.length} {t('个')}</small></h3><div className="proxy-options" aria-label={t('当前配置节点')}>{nodeNames.map(name => <span key={name}>{name}</span>)}</div></div>}
    </section>
  </>
}
