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
function load(relative) {
  const filename = fileURLToPath(new URL(relative, import.meta.url))
  return loadFile(filename)
}
function loadFile(filename) {
  if (cache.has(filename)) return cache.get(filename)
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
      return loadFile(target)
    }
    return require(name)
  }
  vm.runInThisContext(`(function(require, module, exports) {${output}\n})`, { filename })(localRequire, module, module.exports)
  cache.set(filename, module.exports)
  return module.exports
}
const RulesPage = load('../src/features/rules/RulesPage.tsx').default
const { OriginalRulesView } = load('../src/features/rules/RulesPage.tsx')
const ProfilesPage = load('../src/features/profiles/ProfilesPage.tsx').default
const { localizeError } = load('../src/i18n.ts')
const noop = () => {}
const defaults = {
  runtime: { controlsBusy: false, connected: true, state: { routingMode: 'rule' } },
  language: 'en-US',
  controller: {
    rules: [
      { id: 'first', type: 'DOMAIN', domain: 'www.example.com', target: 'Auto', enabled: true },
      { id: 'second', type: 'DOMAIN-SUFFIX', domain: 'example.com', target: 'REJECT', enabled: false },
      { id: 'third', type: 'DOMAIN', domain: 'old.example.com', target: 'Missing', enabled: true },
    ],
    targets: ['DIRECT', 'REJECT', 'Auto'], originalRules: ['MATCH,DIRECT'], active: true, loaded: true, error: '', dirty: true, saved: false,
    update: noop, move: noop, reset: noop, save: noop, add: noop, remove: noop, retry: noop,
  },
}
const render = overrides => renderToStaticMarkup(React.createElement(RulesPage, { ...defaults, ...overrides }))

test('rules page exposes priority, editing, unavailable targets and accessible actions', () => {
  const html = render()
  assert(html.indexOf('www.example.com') < html.indexOf('value="example.com"'))
  assert.match(html, /Exact domain/)
  assert.match(html, /Domain suffix/)
  assert.match(html, /Custom rules take priority over subscription rules and survive updates/)
  assert.match(html, /Missing \(Unavailable\)/)
  assert.match(html, /rule is retained but inactive/)
  assert.match(html, /aria-label="Move up 1\. www\.example\.com"[^>]*disabled/)
  assert.match(html, /aria-label="Move down 3\. old\.example\.com"[^>]*disabled/)
  assert.match(html, /aria-label="Delete 2\. example\.com"/)
  assert.match(html, /custom-rule disabled/)
  assert.match(html, /Unsaved changes/)
  assert.match(html, /Saved rules apply immediately to new connections/)
  assert.match(html, /Subscription rules \(1\)/)
  assert.match(html, /These custom rules belong only to this profile/)
})

test('inactive subscription editor explains isolation instead of claiming a live reload', () => {
  const html = render({ controller: { ...defaults.controller, active: false } })
  assert.match(html, /Saving will not change the current profile/)
  assert.match(html, /Saved rules will apply when this subscription is selected/)
  assert.doesNotMatch(html, /Saved rules apply immediately to new connections/)
})

test('loading errors disable mutation; mode and disconnected states explain when rules apply', () => {
  const html = render({
    runtime: { controlsBusy: false, connected: false, state: { routingMode: 'global' } },
    controller: { ...defaults.controller, loaded: false, error: '无法读取自定义规则' },
  })
  assert.match(html, /Custom rules only apply in Rule mode/)
  assert.match(html, /Could not read custom rules/)
  assert.match(html, /Saved rules will apply when you next connect/)
  assert.match(html, /disabled="">Save rules/)
  assert.match(html, /id="new-rule-domain"[^>]*disabled/)
})

test('empty and saved states render in Chinese and new rule errors translate', () => {
  const html = render({ language: 'zh-CN', controller: { ...defaults.controller, rules: [], dirty: false, saved: true } })
  assert.match(html, /尚无自定义规则/)
  assert.match(html, /规则已保存/)
  assert.match(html, /disabled="">保存规则/)
  assert.equal(localizeError('en-US', '应用自定义规则失败: 自定义规则目标无效'), 'Could not apply custom rules: Invalid custom rule destination')
})

test('original subscription rules are read-only and large lists are bounded', () => {
  const rules = Array.from({ length: 501 }, (_, index) => `DOMAIN,example${index}.com,DIRECT`)
  const html = renderToStaticMarkup(React.createElement(OriginalRulesView, { rules, loaded: true, language: 'en-US' }))
  assert.match(html, /read-only/)
  assert.match(html, /Subscription updates refresh this list/)
  assert.match(html, /Filter original rules/)
  assert.match(html, /Showing the first 500 rules only/)
  assert.match(html, /DOMAIN,example499.com,DIRECT/)
  assert.doesNotMatch(html, /DOMAIN,example500.com,DIRECT/)
  assert.equal((html.match(/<li>/g) ?? []).length, 500)
  assert.doesNotMatch(html, /type="checkbox"|contenteditable|<select|Save rules/)
})

test('every subscription card has a config editor and address editor; local entry stays separate', () => {
  const controller = {
    subscriptions: [{ id: 'one', url: 'https://example.invalid/sub', active: true, total: null, upload: null, download: null, expiresAt: null, updatedAt: null, lastUpdateError: '' }],
    subscriptionURL: '', setSubscriptionURL: noop, fileInput: { current: null }, openEditor: noop, openDeleteDialog: noop,
    importFile: noop, importSubscription: noop, updateSubscription: noop, selectSubscription: noop,
  }
  const runtime = { controlsBusy: false, operation: null, running: false, notice: '', state: { hasProfile: true, error: '' } }
  const html = renderToStaticMarkup(React.createElement(ProfilesPage, { runtime, controller, language: 'en-US', onEditRules: noop }))
  assert.match(html, /aria-label="Edit configuration example.invalid"/)
  assert.match(html, /aria-label="Edit subscription URL"/)
  // An active subscription must not be presented as an editable local profile.
  assert.equal((html.match(/title="Edit configuration"/g) ?? []).length, 1)
  const local = renderToStaticMarkup(React.createElement(ProfilesPage, { runtime, controller: { ...controller, subscriptions: [] }, language: 'en-US', onEditRules: noop }))
  assert.match(local, />Edit configuration /)
})
