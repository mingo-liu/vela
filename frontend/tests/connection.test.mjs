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
      setSystemProxy: noop, setTun: noop, setRoutingMode: noop,
      ...overrides,
    },
  }
}
test('startup keeps mutation controls busy without an extra cancel button', () => {
  for (const language of ['en-US', 'zh-CN']) {
    const html = renderToStaticMarkup(React.createElement(HomePage, { ...props(), language }))
    assert.match(html, /role="switch"[^>]*disabled=""/)
    assert.match(html, /<fieldset[^>]*disabled=""/)
    assert.doesNotMatch(html, /Cancel startup|取消启动|startup-cancel/)
  }
})

const ProxiesPage = load(fileURLToPath(new URL('../src/features/proxies/ProxiesPage.tsx', import.meta.url))).default

test('node cards show latency without a node type badge', () => {
  const node = 'Bronze-台湾-01'
  const runtime = { state: { hasProfile: true, error: '' }, notice: '', controlsBusy: false }
  const controller = {
    groups: [{ name: 'Main', current: node, options: [node] }], nodeNames: [node],
    expandedGroups: {}, groupDelays: { Main: { [node]: 388 } }, measuredGroups: { Main: true },
    testingGroup: null, sortModes: {}, select: noop, toggleGroup: noop, testGroupDelay: noop, toggleGroupSort: noop,
  }
  for (const language of ['en-US', 'zh-CN']) {
    const html = renderToStaticMarkup(React.createElement(ProxiesPage, { runtime, controller, language, onNavigate: noop }))
    assert.match(html, /388 ms/)
    assert.match(html, /aria-pressed="true"/)
    assert.doesNotMatch(html, /proxy-node-kind|Proxy nodes|代理节点/)
  }
})
