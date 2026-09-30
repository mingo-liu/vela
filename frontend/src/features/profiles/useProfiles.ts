import { useEffect, useRef, useState } from 'react'
import * as Runtime from '../../../bindings/github.com/mingo-liu/vela/internal/desktop/runtimeservice'
import type { RuntimeController } from '../../app/useRuntime'
import type { Page } from '../../app/types'
import type { Subscription } from '../../../bindings/github.com/mingo-liu/vela/internal/profile/models'
import { usePolling } from '../../lib/usePolling'
import { errorMessage as message } from '../../lib/errors'

export function useProfiles(runtime: RuntimeController, visible: boolean, page: Page) {
  const { controlsBusy, setNotice, startAction, finishAction, execute, refreshState, hasPendingActions } = runtime
  const [subscriptionURL, setSubscriptionURL] = useState('')
  const [subscriptions, setSubscriptions] = useState<Subscription[]>([])
  const [editingSubscription, setEditingSubscription] = useState<string | null>(null)
  const [subscriptionEditError, setSubscriptionEditError] = useState('')
  const [deletingSubscription, setDeletingSubscription] = useState<string | null>(null)
  const [subscriptionDeleteError, setSubscriptionDeleteError] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (page !== 'profiles') {
      setEditingSubscription(null)
      setDeletingSubscription(null)
    }
  }, [page])

  usePolling(async isCurrent => {
    try {
      const value = await Runtime.Subscriptions()
      if (isCurrent()) setSubscriptions(value ?? [])
    } catch (error) {
      if (isCurrent()) setNotice(message(error))
    }
  }, visible && page === 'profiles' ? 30000 : null)

  const refreshSubscriptions = async () => {
    try {
      setSubscriptions(await Runtime.Subscriptions() ?? [])
    } catch (error) {
      setNotice(message(error))
    }
  }

  const importFile = async (file?: File) => {
    if (!file) return
    if (file.size > 2 * 1024 * 1024) {
      setNotice('配置文件不能超过 2 MiB')
      return
    }
    try {
      const contents = await file.text()
      if (await execute(() => Runtime.ImportProfile(contents))) await refreshSubscriptions()
    } catch (error) {
      setNotice(message(error))
    }
  }

  const importSubscription = async () => {
    if (await execute(() => Runtime.ImportSubscription(subscriptionURL))) {
      setSubscriptionURL('')
      await refreshSubscriptions()
    }
  }

  const updateSubscription = async (id: string) => {
    if (await execute(() => Runtime.UpdateSubscription(id))) await refreshSubscriptions()
  }

  const selectSubscription = async (id: string) => {
    if (await execute(() => Runtime.SelectSubscription(id))) await refreshSubscriptions()
  }

  const saveSubscriptionURL = async (id: string, url: string) => {
    if (controlsBusy) return
    setSubscriptionEditError('')
    if (await execute(() => Runtime.ReplaceSubscriptionURL(id, url), setSubscriptionEditError)) {
      setEditingSubscription(null)
      await refreshSubscriptions()
    }
  }

  const removeSubscription = async (subscription: Subscription) => {
    if (controlsBusy || hasPendingActions()) return
    startAction()
    setNotice('')
    setSubscriptionDeleteError('')
    try {
      await Runtime.RemoveSubscription(subscription.id)
      setSubscriptions(current => current.filter(item => item.id !== subscription.id))
      setDeletingSubscription(null)
      if (editingSubscription === subscription.id) setEditingSubscription(null)
      await refreshSubscriptions()
    } catch (error) {
      setSubscriptionDeleteError(message(error))
    } finally {
      await refreshState()
      finishAction()
    }
  }

  const editedSubscription = subscriptions.find(subscription => subscription.id === editingSubscription)
  const subscriptionToDelete = subscriptions.find(subscription => subscription.id === deletingSubscription)

  const openEditor = (id: string) => { setSubscriptionEditError(''); setEditingSubscription(id) }
  const openDeleteDialog = (id: string) => { setSubscriptionDeleteError(''); setDeletingSubscription(id) }

  return {
    subscriptions, subscriptionURL, setSubscriptionURL, fileInput,
    editedSubscription, subscriptionToDelete, subscriptionEditError, subscriptionDeleteError,
    closeEditor: () => setEditingSubscription(null),
    closeDeleteDialog: () => setDeletingSubscription(null),
    openEditor, openDeleteDialog, importFile, importSubscription,
    updateSubscription, selectSubscription, saveSubscriptionURL, removeSubscription,
  }
}

export type ProfilesController = ReturnType<typeof useProfiles>
