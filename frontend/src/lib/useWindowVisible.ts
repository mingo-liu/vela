import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import * as Runtime from '../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'

export function useWindowVisible() {
  const [nativeVisible, setNativeVisible] = useState<boolean | null>(null)
  const [documentVisible, setDocumentVisible] = useState(!document.hidden)

  useEffect(() => {
    let active = true
    let receivedEvent = false
    const unsubscribe = Events.On('window-visibility', event => {
      receivedEvent = true
      if (active) setNativeVisible(event.data as boolean)
    })
    const changed = () => setDocumentVisible(!document.hidden)
    document.addEventListener('visibilitychange', changed)
    void Runtime.WindowVisible().then(value => { if (active && !receivedEvent) setNativeVisible(value) }).catch(() => {})
    return () => { active = false; unsubscribe(); document.removeEventListener('visibilitychange', changed) }
  }, [])

  // Native show/hide events are authoritative in the desktop webview.
  // Browser visibility remains the fallback when the native source is absent.
  return nativeVisible ?? documentVisible
}
