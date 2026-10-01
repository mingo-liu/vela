import { useState, type FormEvent } from 'react'
import { ArrowDown, ArrowUp, Plus, Trash } from '@phosphor-icons/react'
import { localizeError, translate, type Language } from '../../i18n'
import type { RuntimeController } from '../../app/useRuntime'
import type { RulesController } from './useRules'

type Props = { runtime: RuntimeController; controller: RulesController; language: Language; local?: boolean }

export default function RulesPage({ runtime, controller, language, local = false }: Props) {
  const t = (text: string) => translate(language, text)
  const { rules, targets, originalRules, active, loaded, error, dirty, saved, update, move, reset, save, add, remove, retry } = controller
  const [tab, setTab] = useState<'custom' | 'original'>('custom')
  const sourceLabel = t(local ? '配置原有规则' : '订阅原有规则')
  const [type, setType] = useState('DOMAIN-SUFFIX')
  const [domain, setDomain] = useState('')
  const [target, setTarget] = useState('DIRECT')
  const busy = runtime.controlsBusy || !loaded
  const targetLabel = (value: string) => value === 'DIRECT' ? t('直连') : value === 'REJECT' ? t('阻断') : value
  const typeOptions = <><option value="DOMAIN">{t('完整域名')}</option><option value="DOMAIN-SUFFIX">{t('域名后缀')}</option></>
  const addRule = (event: FormEvent) => {
    event.preventDefault()
    if (busy || !domain.trim() || !targets.includes(target) || rules.length >= 500) return
    add(type, domain.trim(), target)
    setDomain('')
  }
  return <>
    <div className="diagnostics-tabs rules-editor-tabs" role="tablist" aria-label={t('配置规则')}><button id="custom-rules-tab" type="button" role="tab" aria-selected={tab === 'custom'} aria-controls="custom-rules-view" className={tab === 'custom' ? 'active' : ''} onClick={() => setTab('custom')}>{t('自定义规则')} ({rules.length})</button><button id="original-rules-tab" type="button" role="tab" aria-selected={tab === 'original'} aria-controls="original-rules-view" className={tab === 'original' ? 'active' : ''} onClick={() => setTab('original')}>{sourceLabel} ({originalRules?.length ?? 0})</button></div>
    {error && <div className="alert" role="alert">{localizeError(language, error)} {!loaded && <button className="secondary-button" type="button" onClick={retry}>{t('重试')}</button>}</div>}
    {active && runtime.state.routingMode !== 'rule' && <p className="rules-notice">{t('当前为全局或直连模式，自定义规则仅在规则模式下生效。')}</p>}
    {!active && loaded && <p className="rules-notice">{t('正在编辑未使用的订阅，保存不会切换当前配置；选用此订阅后生效。')}</p>}
    {tab === 'original' ? <OriginalRulesView rules={originalRules ?? []} loaded={loaded} language={language} local={local} /> : <section id="custom-rules-view" role="tabpanel" aria-labelledby="custom-rules-tab" className="custom-rules-panel">
      <div className="panel-heading"><div><h2>{t('分流优先级')}</h2><p>{t('从上到下匹配，第一条命中的规则生效；自定义规则优先于订阅规则，订阅更新后保留。')}</p></div></div>
      <p className="rules-help">{t('完整域名只匹配自身；域名后缀匹配自身及其所有子域名。例如 example.com 也匹配 www.example.com。')}</p>
      <p className="rules-help">{t('这些自定义规则仅属于此配置，不会影响其他订阅。')}</p>
      {!loaded ? <p role="status">{t('读取中…')}</p> : rules.length === 0 ? <div className="rules-empty">{t('尚无自定义规则，添加后可指定网站直连、代理或阻断。')}</div> : <ol className="custom-rules-list">{rules.map((rule, index) => {
        const available = targets.includes(rule.target)
        const label = `${index + 1}. ${rule.domain}`
        return <li className={`custom-rule${!rule.enabled ? ' disabled' : ''}`} key={rule.id}>
          <div className="custom-rule-top"><span className="rule-order">{index + 1}</span><label className="rule-enable"><input type="checkbox" checked={rule.enabled} disabled={busy} onChange={event => update(rule.id, { enabled: event.target.checked })} />{t('启用')}</label><div className="rule-row-actions"><button className="secondary-button" type="button" aria-label={`${t('上移')} ${label}`} title={t('上移')} disabled={busy || index === 0} onClick={() => move(index, -1)}><ArrowUp size={16} /></button><button className="secondary-button" type="button" aria-label={`${t('下移')} ${label}`} title={t('下移')} disabled={busy || index === rules.length - 1} onClick={() => move(index, 1)}><ArrowDown size={16} /></button><button className="secondary-button" type="button" aria-label={`${t('删除')} ${label}`} title={t('删除')} disabled={busy} onClick={() => remove(rule.id)}><Trash size={16} /></button></div></div>
          <div className="rule-fields"><label><span>{t('匹配类型')}</span><select className="text-field" value={rule.type} disabled={busy} onChange={event => update(rule.id, { type: event.target.value })}>{typeOptions}</select></label><label><span>{t('域名')}</span><input className="text-field" value={rule.domain} disabled={busy} autoCapitalize="none" autoComplete="off" spellCheck={false} onChange={event => update(rule.id, { domain: event.target.value })} /></label><label><span>{t('目标')}</span><select className="text-field" value={rule.target} disabled={busy} onChange={event => update(rule.id, { target: event.target.value })}>{!available && <option value={rule.target}>{rule.target} ({t('不可用')})</option>}{targets.map(value => <option key={value} value={value}>{targetLabel(value)}</option>)}</select></label></div>
          {!available && <p className="rule-unavailable">{t('此配置缺少此策略组，规则保留但暂不生效。请选择其他目标。')}</p>}
        </li>
      })}</ol>}
      <form className="rule-add-form" onSubmit={addRule}>
        <h3>{t('添加规则')}</h3>
        <div className="rule-fields"><label htmlFor="new-rule-type"><span>{t('匹配类型')}</span><select id="new-rule-type" className="text-field" value={type} disabled={busy} onChange={event => setType(event.target.value)}>{typeOptions}</select></label><label htmlFor="new-rule-domain"><span>{t('域名')}</span><input id="new-rule-domain" className="text-field" placeholder="example.com" value={domain} disabled={busy} autoCapitalize="none" autoComplete="off" spellCheck={false} onChange={event => setDomain(event.target.value)} /></label><label htmlFor="new-rule-target"><span>{t('目标')}</span><select id="new-rule-target" className="text-field" value={target} disabled={busy} onChange={event => setTarget(event.target.value)}>{!targets.includes(target) && <option value={target}>{target} ({t('不可用')})</option>}{targets.map(value => <option key={value} value={value}>{targetLabel(value)}</option>)}</select></label></div>
        <div className="rule-add-actions"><small>{t('仅填写域名，不含 https://、路径或通配符；国际化域名请使用 Punycode。')}</small><button className="secondary-button" type="submit" disabled={busy || !domain.trim() || !targets.includes(target) || rules.length >= 500}><Plus size={16} />{t('添加规则')}</button></div>
      </form>
      <div className="rules-save-bar"><span role="status">{dirty ? t('有未保存的修改') : saved ? t('规则已保存') : `${rules.length} / 500 ${t('条规则')}`}</span><div><button className="secondary-button" type="button" disabled={busy || !dirty} onClick={reset}>{t('撤销修改')}</button><button className="primary-button" type="button" disabled={busy || !dirty} onClick={() => void save()}>{runtime.controlsBusy ? t('正在保存…') : t('保存规则')}</button></div></div>
      <p className="rules-help">{active && runtime.connected ? t('保存后立即应用于新连接；已有连接可能继续使用原路径。') : active ? t('保存后将在下次连接时生效。') : t('保存后将在选用此订阅时生效。')}</p>
    </section>}
  </>
}


export function OriginalRulesView({ rules, loaded, language, local = false }: { rules: string[]; loaded: boolean; language: Language; local?: boolean }) {
  const t = (text: string) => translate(language, text)
  const [filter, setFilter] = useState('')
  const filtered = rules.map((rule, index) => ({ rule, index })).filter(item => item.rule.toLowerCase().includes(filter.trim().toLowerCase()))
  return <section id="original-rules-view" role="tabpanel" aria-labelledby="original-rules-tab" className="original-rules-panel">
    <p className="rules-help">{t(local ? '原有规则来自本地配置，仅供查看。需要调整分流时，请添加优先匹配的自定义规则。' : '原有规则来自配置文件，仅供查看；订阅更新会刷新此列表。需要调整分流时，请添加优先匹配的自定义规则。')}</p>
    <input className="text-field" type="search" aria-label={t('筛选原有规则')} placeholder={t('筛选原有规则')} value={filter} onChange={event => setFilter(event.target.value)} />
    {!loaded ? <p role="status">{t('读取中…')}</p> : <>
      <p className="rules-help">{filtered.length} {t('条规则')}{filtered.length > 500 && ` · ${t('仅显示前 500 条规则')}`}</p>
      {filtered.length === 0 ? <p className="rules-empty">{t('暂无匹配的规则')}</p> : <ol className="original-rules-list">{filtered.slice(0, 500).map(item => <li key={item.index}><span>{item.index + 1}</span><code>{item.rule}</code></li>)}</ol>}
    </>}
  </section>
}
