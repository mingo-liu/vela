import type { Language } from '../../i18n'
import type { ProfilesController } from './useProfiles'
import { subscriptionDomain } from './format'
import type { OperationProgress } from '../../../bindings/github.com/mingo-liu/vela/internal/mihomo/models'
import SubscriptionEditorDialog from './SubscriptionEditorDialog'
import SubscriptionDeleteDialog from './SubscriptionDeleteDialog'

type Props = { controller: ProfilesController; language: Language; busy: boolean; operation: OperationProgress | null; cancelling: boolean; onCancelOperation: (id: number) => Promise<void> }

export default function SubscriptionDialogs({ controller, language, busy, operation, cancelling, onCancelOperation }: Props) {
  const {
    editedSubscription, subscriptionToDelete, subscriptionEditError, subscriptionDeleteError,
    closeEditor, closeDeleteDialog, saveSubscriptionURL, removeSubscription,
  } = controller

  return <>
    {editedSubscription && <SubscriptionEditorDialog key={editedSubscription.id} url={editedSubscription.url} domain={subscriptionDomain(editedSubscription.url, language)} language={language} busy={busy} operation={operation} cancelling={cancelling} onCancelOperation={onCancelOperation} error={subscriptionEditError} onClose={closeEditor} onSave={url => saveSubscriptionURL(editedSubscription.id, url)} />}
    {subscriptionToDelete && <SubscriptionDeleteDialog key={subscriptionToDelete.id} domain={subscriptionDomain(subscriptionToDelete.url, language)} language={language} active={subscriptionToDelete.active} busy={busy} operation={operation} cancelling={cancelling} onCancelOperation={onCancelOperation} error={subscriptionDeleteError} onClose={closeDeleteDialog} onConfirm={() => removeSubscription(subscriptionToDelete)} />}
  </>
}
