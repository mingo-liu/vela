import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import vm from 'node:vm'
import test from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ts from 'typescript'
import * as Icons from '@phosphor-icons/react'

const require = createRequire(import.meta.url)
const cache = new Map()
function load(filename) {
  if (cache.has(filename)) return cache.get(filename)
  // Exit IP fetching is independent of connection startup controls.
  if (filename.endsWith('/ExitIPCard.tsx')) return { __esModule: true, default: () => null }
  const output = ts.transpileModule(readFileSync(filename, 'utf8'), {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX, esModuleInterop: true },
  }).outputText
  const module = { exports: {} }
  const localRequire = name => {
    if (name === '@phosphor-icons/react') return Icons
    if (name.startsWith('.')) {
      const base = path.resolve(path.dirname(filename), name)
      const target = [base, base + '.ts', base + '.tsx'].find(existsSync)
      assert(target, `Cannot resolve ${name}`)
      return load(target)
    }
    return require(name)
  }
  vm.runInThisContext(`(function(require, module, exports) {${output}\n})`, { filename })(localRequire, module, module.exports)
  cache.set(filename, module.exports)
  return module.exports
}
const HomePage = load(fileURLToPath(new URL('../src/features/overview/HomePage.tsx', import.meta.url))).default
const noop = () => {}
function props(overrides = {}) {
  return {
    language: 'en-US', groups: [], visible: true, onNavigate: noop,
    runtime: {
      state: { status: 'starting', hasProfile: true, tunSupported: true, port: 7890, routingMode: 'rule' },
      controlsBusy: true, operation: null,
      setSystemProxy: noop, setTun: noop, setRoutingMode: noop, cancelStartup: noop, cancellingStartup: false,
      ...overrides,
    },
  }
}
test('startup keeps mutation controls busy and exposes a separate cancel action', () => {
  for (const language of ['en-US', 'zh-CN']) {
    const html = renderToStaticMarkup(React.createElement(HomePage, { ...props(), language }))
    assert.match(html, /role="switch"[^>]*disabled=""/)
    assert.match(html, /<fieldset[^>]*disabled=""/)
    assert.match(html, /Cancel startup|取消启动/)
    assert.match(html, /class="secondary-button startup-cancel" type="button">/)
  }
})

const OperationStatus = load(fileURLToPath(new URL('../src/app/OperationStatus.tsx', import.meta.url))).default
const operation = { id: 7, active: true, kind: 'delay', target: 'Auto', phase: 'testing', total: 20, completed: 6, cancellable: true }
const renderOperation = overrides => renderToStaticMarkup(React.createElement(OperationStatus, { operation, language: 'en-US', cancelling: false, onCancel: noop, ...overrides }))

test('delay task reports real counts; downloads remain indeterminate and private', () => {
  const html = renderOperation()
  assert.match(html, /6 \/ 20/)
  assert.match(html, /<progress[^>]*max="20"[^>]*value="6"/)
  const download = renderOperation({ operation: { ...operation, kind: 'subscription', target: 'private-subscription-id', phase: 'downloading', total: 1, completed: 0 } })
  assert.match(download, /Fetching updates/)
  assert.doesNotMatch(download, /private-subscription-id|max=|value=|0 \/ 1/)
  assert.equal(renderOperation({ operation: { ...operation, active: false } }), '')
})

test('apply and cancelling phases cannot cancel; clicks keep the displayed task ID', () => {
  for (const phase of ['applying', 'cancelling']) {
    const html = renderOperation({ operation: { ...operation, phase, cancellable: false } })
    assert.match(html, /disabled="">Cancel task/)
    assert.match(html, phase === 'applying' ? /Applying configuration/ : /Cancelling/)
  }
  assert.match(renderOperation({ cancelling: true }), /disabled="">Cancel task/)
  const calls = []
  const tree = OperationStatus({ operation, language: 'en-US', cancelling: false, onCancel: id => calls.push(id) })
  const button = React.Children.toArray(tree.props.children).find(child => child.type === 'button')
  button.props.onClick()
  assert.deepEqual(calls, [7])
})
