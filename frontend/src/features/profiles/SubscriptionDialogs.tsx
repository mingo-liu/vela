import type { Language } from '../../i18n'
import type { ProfilesController } from './useProfiles'
import { subscriptionDomain } from './format'
import SubscriptionEditorDialog from './SubscriptionEditorDialog'
import SubscriptionDeleteDialog from './SubscriptionDeleteDialog'

type Props = { controller: ProfilesController; language: Language; busy: boolean }

export default function SubscriptionDialogs({ controller, language, busy }: Props) {
  const {
    editedSubscription, subscriptionToDelete, subscriptionEditError, subscriptionDeleteError,
    closeEditor, closeDeleteDialog, saveSubscriptionURL, removeSubscription,
  } = controller

  return <>
    {editedSubscription && <SubscriptionEditorDialog key={editedSubscription.id} url={editedSubscription.url} domain={subscriptionDomain(editedSubscription.url, language)} language={language} busy={busy} error={subscriptionEditError} onClose={closeEditor} onSave={url => saveSubscriptionURL(editedSubscription.id, url)} />}
    {subscriptionToDelete && <SubscriptionDeleteDialog key={subscriptionToDelete.id} domain={subscriptionDomain(subscriptionToDelete.url, language)} language={language} active={subscriptionToDelete.active} busy={busy} error={subscriptionDeleteError} onClose={closeDeleteDialog} onConfirm={() => removeSubscription(subscriptionToDelete)} />}
  </>
}
