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
      controlsBusy: true, operation: null, cancellingConnection: false,
      setSystemProxy: noop, setTun: noop, setRoutingMode: noop, cancelConnection: noop,
      ...overrides,
    },
  }
}
function findCancel(element) {
  if (!React.isValidElement(element)) return undefined
  if (element.type === 'button' && element.props.children === 'Cancel startup') return element
  return React.Children.toArray(element.props.children).map(findCancel).find(Boolean)
}

test('startup can be cancelled while connection switches and settings are busy', () => {
  let cancelled = 0
  const input = props({ cancelConnection: () => { cancelled++ } })
  const html = renderToStaticMarkup(React.createElement(HomePage, input))
  assert.match(html, /aria-label="System proxy"[^>]*disabled=""/)
  assert.match(html, /aria-label="Tun mode"[^>]*disabled=""/)
  assert.match(html, /<fieldset[^>]*disabled=""/)
  const button = findCancel(HomePage(input))
  assert(button)
  assert.equal(button.props.disabled, false)
  button.props.onClick()
  assert.equal(cancelled, 1)
})

test('cancelling disables repeat requests and the action disappears after startup', () => {
  const html = renderToStaticMarkup(React.createElement(HomePage, props({ cancellingConnection: true })))
  assert.match(html, /disabled="">Cancelling…<\/button>/)
  const connected = props({ state: { status: 'running', systemProxyEnabled: true, hasProfile: true, port: 7890, routingMode: 'rule' }, connected: true, running: true })
  assert.doesNotMatch(renderToStaticMarkup(React.createElement(HomePage, connected)), /Cancel startup/)
  assert.doesNotMatch(renderToStaticMarkup(React.createElement(HomePage, props({ operation: { active: true } }))), /Cancel startup/)
  assert.match(renderToStaticMarkup(React.createElement(HomePage, { ...props(), language: 'zh-CN' })), /取消启动/)
})
