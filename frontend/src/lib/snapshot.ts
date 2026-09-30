// An in-flight fallback read must not overwrite an event or command response
// received after that read started.
export function createSnapshotReceiver<T>(apply: (value: T) => void) {
  let revision = 0
  const receive = (value: T) => { revision++; apply(value) }
  return {
    receive,
    async refresh(read: () => Promise<T>, isCurrent: () => boolean) {
      const startedAt = revision
      const value = await read()
      if (isCurrent() && startedAt === revision) receive(value)
    },
  }
}
