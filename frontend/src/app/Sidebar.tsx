import { GearSix, GlobeHemisphereWest, House, ListBullets, Stack } from '@phosphor-icons/react'
import velaIcon from '../../../build/appicon.png'
import { translate, type Language } from '../i18n'
import type { Page } from './types'
import TrafficMonitor from './TrafficMonitor'

const navigation = [
  { id: 'home', label: '首页', icon: House },
  { id: 'proxies', label: '代理', icon: GlobeHemisphereWest },
  { id: 'profiles', label: '配置', icon: Stack },
  { id: 'diagnostics', label: '诊断', icon: ListBullets },
  { id: 'logs', label: '日志', icon: ListBullets },
  { id: 'settings', label: '设置', icon: GearSix },
] as const

type Props = {
  page: Page
  language: Language
  visible: boolean
  connected: boolean
  onNavigate: (page: Page) => void
}

export default function Sidebar({ page, language, visible, connected, onNavigate }: Props) {
  const t = (text: string) => translate(language, text)
  return (
    <aside className="sidebar" aria-label={t('主导航')}>
      <div className="brand"><img className="brand-icon" src={velaIcon} alt="" /><div><strong>Vela</strong></div></div>
      <nav className="navigation" aria-label={t('页面')}>
        {navigation.map(item => <button key={item.id} type="button" className={`nav-item${page === item.id ? ' active' : ''}`} aria-current={page === item.id ? 'page' : undefined} onClick={() => onNavigate(item.id)}><item.icon size={25} weight="regular" /><span>{t(item.label)}</span></button>)}
      </nav>
      <TrafficMonitor language={language} visible={visible} />
      <div className="sidebar-footer"><span className={`sidebar-dot${connected ? ' connected' : ''}`} /><div><strong>{connected ? t('已连接') : t('未连接')}</strong></div></div>
    </aside>
  )
}
