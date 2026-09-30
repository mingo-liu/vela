import { useEffect, useState } from 'react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { RuntimeController } from '../../app/useRuntime'
import type { CoreInfo } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import type { Settings } from '../../../bindings/github.com/mingo-liu/vela/internal/profile/models'
import type { Language } from '../../i18n'
import { errorMessage as message } from '../../lib/errors'

const defaultSettings: Settings = { mixedPort: 7890, routingMode: 'rule', autoConnect: false, autoConnectMode: 'system', subscriptionUpdateHours: 24, logLevel: 'profile', launchAtLogin: false, language: 'zh-CN' }

export function useSettings(runtime: RuntimeController) {
  const { state, setNotice, startAction, finishAction, execute } = runtime
  const [settings, setSettings] = useState<Settings>(defaultSettings)
  const [coreInfo, setCoreInfo] = useState<CoreInfo | null>(null)
  const [coreInfoError, setCoreInfoError] = useState('')
  const [portDraft, setPortDraft] = useState('7890')
  const language: Language = settings.language === 'en-US' ? 'en-US' : 'zh-CN'

  useEffect(() => { document.documentElement.lang = language }, [language])

  useEffect(() => setPortDraft(String(state.port)), [state.port])

  useEffect(() => {
    let active = true
    Runtime.Settings().then(value => { if (active) setSettings(value) })
      .catch(error => { if (active) setNotice(message(error)) })
    return () => { active = false }
  }, [])

  useEffect(() => {
    let active = true
    Runtime.CoreInfo().then(value => { if (active) setCoreInfo(value) })
      .catch(error => { if (active) setCoreInfoError(message(error)) })
    return () => { active = false }
  }, [])

  const updateSettings = async (action: () => Promise<Settings>) => {
    startAction()
    setNotice('')
    try {
      setSettings(await action())
    } catch (error) {
      setNotice(message(error))
      try { setSettings(await Runtime.Settings()) } catch { /* retain the last known settings */ }
    } finally {
      finishAction()
    }
  }

  const openConfigDirectory = async () => {
    setNotice('')
    try { await Runtime.OpenConfigDirectory() } catch (error) { setNotice(message(error)) }
  }

  const saveMixedPort = async () => {
    const port = Number(portDraft)
    if (!Number.isInteger(port) || port < 1024 || port > 65535) {
      setNotice('本地代理端口必须在 1024–65535 之间')
      return
    }
    if (await execute(() => Runtime.SetMixedPort(port))) setPortDraft(String(port))
  }

  return {
    settings, language, coreInfo, coreInfoError, portDraft, setPortDraft, openConfigDirectory, saveMixedPort,
    setLanguage: (language: string) => updateSettings(() => Runtime.SetLanguage(language)),
    setLaunchAtLogin: (enabled: boolean) => updateSettings(() => Runtime.SetLaunchAtLogin(enabled)),
    setAutoConnect: (enabled: boolean) => updateSettings(() => Runtime.SetAutoConnect(enabled)),
    setAutoConnectMode: (mode: string) => updateSettings(() => Runtime.SetAutoConnectMode(mode)),
    setSubscriptionUpdateHours: (hours: number) => updateSettings(() => Runtime.SetSubscriptionUpdateHours(hours)),
    setLogLevel: (level: string) => updateSettings(() => Runtime.SetLogLevel(level)),
  }
}

export type SettingsController = ReturnType<typeof useSettings>
