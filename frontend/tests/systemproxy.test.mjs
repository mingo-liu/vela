import assert from 'node:assert/strict'
import test from 'node:test'
import { localizeError } from '../src/i18n.ts'

test('network monitoring failures retain actionable details in both languages', () => {
  for (const [chinese, english] of [
    ['检查系统代理失败', 'Could not check system proxy'],
    ['切换网络服务时启用系统代理失败', 'Could not enable system proxy on the new network service'],
  ]) {
    assert.equal(localizeError('en-US', `${chinese}: no default route`), `${english}: no default route`)
    assert.equal(localizeError('zh-CN', `${chinese}: no default route`), `${chinese}: no default route`)
  }
})
