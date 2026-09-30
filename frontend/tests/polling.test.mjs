import assert from 'node:assert/strict'
import { setImmediate } from 'node:timers/promises'
import test from 'node:test'
import { startPolling } from '../src/lib/polling.ts'
import { createSnapshotReceiver } from '../src/lib/snapshot.ts'

class Clock {
  now = 0
  sequence = 0
  tasks = new Map()
  setTimeout = (callback, delay) => {
    const id = ++this.sequence
    this.tasks.set(id, { at: this.now + delay, callback })
    return id
  }
  clearTimeout = id => this.tasks.delete(id)
  async advance(duration) {
    const until = this.now + duration
    for (;;) {
      const next = [...this.tasks].sort((a, b) => a[1].at - b[1].at).find(([, task]) => task.at <= until)
      if (!next) break
      const [id, task] = next
      this.now = task.at
      this.tasks.delete(id)
      task.callback()
      await setImmediate()
    }
    this.now = until
  }
}

function deferred() {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

test('slow requests never overlap and hidden windows stop scheduled refreshes', async () => {
  const clock = new Clock()
  const response = deferred()
  let calls = 0
  let updates = 0
  const polling = startPolling(async isCurrent => {
    calls++
    await response.promise
    if (isCurrent()) updates++
  }, 100, clock)
  await clock.advance(1000)
  assert.equal(calls, 1)
  polling.setInterval(null)
  response.resolve()
  await setImmediate()
  await clock.advance(1000)
  assert.equal(calls, 1)
  assert.equal(updates, 0)
  assert.equal(clock.tasks.size, 0)
  polling.stop()
})

test('resuming during a pending request refreshes immediately after it settles', async () => {
  const clock = new Clock()
  const old = deferred()
  const results = []
  let calls = 0
  const polling = startPolling(async isCurrent => {
    const number = ++calls
    if (number === 1) await old.promise
    if (isCurrent()) results.push(number)
  }, 100, clock)
  await clock.advance(0)
  polling.setInterval(null)
  polling.setInterval(100)
  await clock.advance(0)
  assert.equal(calls, 1)
  old.resolve()
  await setImmediate()
  await clock.advance(0)
  assert.equal(calls, 2)
  assert.deepEqual(results, [2])
  polling.stop()
})

test('one-shot views refresh on demand without repeated timers', async () => {
  const clock = new Clock()
  let calls = 0
  const polling = startPolling(async () => { calls++ }, 0, clock)
  await clock.advance(1000)
  assert.equal(calls, 1)
  assert.equal(clock.tasks.size, 0)
  polling.refresh()
  await clock.advance(0)
  assert.equal(calls, 2)
  polling.stop()
})

test('failed reads retry and disposal rejects pending results', async () => {
  const clock = new Clock()
  const pending = deferred()
  let calls = 0
  let applied = false
  const polling = startPolling(async isCurrent => {
    if (++calls === 1) throw new Error('temporary read failure')
    await pending.promise
    applied = isCurrent()
  }, 100, clock)
  await clock.advance(100)
  assert.equal(calls, 2)
  polling.stop()
  pending.resolve()
  await setImmediate()
  assert.equal(applied, false)
  assert.equal(clock.tasks.size, 0)
})

test('events and command responses win over an older fallback snapshot', async () => {
  const old = deferred()
  const values = []
  const receiver = createSnapshotReceiver(value => values.push(value))
  const refresh = receiver.refresh(() => old.promise, () => true)
  receiver.receive('running')
  old.resolve('starting')
  await refresh
  assert.deepEqual(values, ['running'])
  await receiver.refresh(async () => 'stopped', () => true)
  assert.deepEqual(values, ['running', 'stopped'])
  await receiver.refresh(async () => 'obsolete', () => false)
  assert.deepEqual(values, ['running', 'stopped'])
})

test('quick mount and unmount does not start background reads', async () => {
  const clock = new Clock()
  let calls = 0
  const polling = startPolling(async () => { calls++ }, 100, clock)
  polling.stop()
  await clock.advance(1000)
  assert.equal(calls, 0)
})
