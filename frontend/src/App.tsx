import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import Sidebar from './app/Sidebar'
import type { Page } from './app/types'
import { useRuntime } from './app/useRuntime'
import { useWindowVisible } from './lib/useWindowVisible'
import HomePage from './features/overview/HomePage'
import ProxiesPage from './features/proxies/ProxiesPage'
import { useProxies } from './features/proxies/useProxies'
import ProfilesPage from './features/profiles/ProfilesPage'
import SubscriptionDialogs from './features/profiles/SubscriptionDialogs'
import { useProfiles } from './features/profiles/useProfiles'
import DiagnosticsPage from './features/diagnostics/DiagnosticsPage'
import LogsPage from './features/logs/LogsPage'
import { useLogs } from './features/logs/useLogs'
import SettingsPage from './features/settings/SettingsPage'
import { useSettings } from './features/settings/useSettings'
import ProfileRulesDialog from './features/rules/ProfileRulesDialog'

export default function App() {
  const [page, setPage] = useState<Page>('home')
  const [ruleProfile, setRuleProfile] = useState<{ id: string; title: string } | null>(null)
  const visible = useWindowVisible()
  const runtime = useRuntime(visible)

  // Keep feature state mounted while navigating: selections, drafts, delay
  // caches and log filters have the same lifetime as the application window.
  const settings = useSettings(runtime)
  const proxies = useProxies(runtime, visible, page)
  const profiles = useProfiles(runtime, visible, page)
  const logs = useLogs(visible, page)
  const { language } = settings

  useEffect(() => Events.On('open-settings', () => setPage('settings')), [])
  useEffect(() => Events.On('open-logs', () => setPage('logs')), [])

  return <div className="app-layout">
    <Sidebar page={page} language={language} visible={visible} connected={runtime.connected} onNavigate={setPage} />
    <main className="content">
      {page === 'home' && <HomePage runtime={runtime} groups={proxies.groups} language={language} visible={visible} onNavigate={setPage} />}
      {page === 'proxies' && <ProxiesPage runtime={runtime} controller={proxies} language={language} onNavigate={setPage} />}
      {page === 'profiles' && <ProfilesPage runtime={runtime} controller={profiles} language={language} onEditRules={(id, title) => setRuleProfile({ id, title })} />}
      {page === 'diagnostics' && <DiagnosticsPage visible={visible} connected={runtime.connected && runtime.running} language={language} profileRevision={runtime.profileRevision} />}
      {page === 'logs' && <LogsPage controller={logs} language={language} />}
      {page === 'settings' && <SettingsPage runtime={runtime} controller={settings} />}
    </main>
    {page === 'profiles' && <SubscriptionDialogs controller={profiles} language={language} busy={runtime.controlsBusy} />}
    {ruleProfile && <ProfileRulesDialog key={ruleProfile.id} id={ruleProfile.id} title={ruleProfile.title} runtime={runtime} language={language} visible={visible} onClose={() => setRuleProfile(null)} />}
  </div>
}
