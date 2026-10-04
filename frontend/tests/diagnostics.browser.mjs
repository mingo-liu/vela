import assert from 'node:assert/strict'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { after, before, test } from 'node:test'
import { build } from 'vite'
import react from '@vitejs/plugin-react'
import { chromium, webkit } from 'playwright'

const frontend = fileURLToPath(new URL('..', import.meta.url))
const require = createRequire(import.meta.url)
let directory, server, url

before(async () => {
  directory = await mkdtemp(path.join(tmpdir(), 'vela-diagnostics-test-'))
  await writeFile(path.join(directory, 'index.html'), '<!doctype html><html><meta charset="utf-8"><div id="root"></div><script type="module" src="/entry.tsx"></script></html>')
  await writeFile(path.join(directory, 'entry.tsx'), `
    import React from ${JSON.stringify(require.resolve('react'))}
    import { createRoot } from ${JSON.stringify(require.resolve('react-dom/client'))}
    import Diagnostics from ${JSON.stringify(path.join(frontend, 'src/features/diagnostics/DiagnosticsPage.tsx'))}
    import ${JSON.stringify(path.join(frontend, 'src/style.css'))}
    const params = new URLSearchParams(location.search)
    const count = Number(params.get('count') ?? 10000)
    window.fixtureRules = Array.from({ length: count }, (_, index) => ({
      index, type: ['DomainSuffix', 'Domain', 'IPCIDR'][index % 3],
      payload: index % 3 === 2 ? '192.0.' + (index % 256) + '.0/24' :
        (index % 10 === 0 ? 'needle-' : 'host-') + index + '.example.com' +
        (params.has('long') && index % 7 === 0 ? '.very-long-domain'.repeat(20) : ''),
      proxy: ['DIRECT', 'REJECT', 'Auto'][index % 3],
    }))
    const root = createRoot(document.getElementById('root'))
    window.refreshFixture = () => root.render(<div className="app-layout"><aside className="sidebar"/><main className="content"><Diagnostics connected language="zh-CN" profileRevision={1} visible /></main></div>)
    window.refreshFixture()
  `)
  await build({
    configFile: false, root: directory, publicDir: false, logLevel: 'silent',
    plugins: [{
      name: 'diagnostics-fixture', enforce: 'pre',
      resolveId(source) {
        if (source.endsWith('/runtimeservice')) return '\0fixture-runtime'
        if (source === 'react' || source.startsWith('react/')) return require.resolve(source)
      },
      load(id) {
        if (id === '\0fixture-runtime') return 'export async function Rules(){return window.fixtureRules}; export async function Connections(){return {connections:[],total:0,uploadTotal:0,downloadTotal:0}}'
      },
    }, react()],
    build: { outDir: path.join(directory, 'dist'), emptyOutDir: true },
  })
  server = createServer(async (request, response) => {
    try {
      const pathname = new URL(request.url, 'http://localhost').pathname
      const filename = path.join(directory, 'dist', pathname === '/' ? 'index.html' : pathname)
      response.setHeader('Content-Type', filename.endsWith('.js') ? 'text/javascript' : filename.endsWith('.css') ? 'text/css' : 'text/html')
      response.end(await readFile(filename))
    } catch { response.statusCode = 404; response.end() }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  url = `http://127.0.0.1:${server.address().port}`
})

after(async () => {
  if (server) await new Promise(resolve => server.close(resolve))
  if (directory) await rm(directory, { recursive: true, force: true })
})

async function settled(page) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
}

for (const [name, engine] of [['WebKit', webkit], ['Chromium', chromium]]) {
  test(`${name}: virtual rules preserve search, scrolling and dynamic heights`, async t => {
    const browser = await engine.launch()
    const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
    page.setDefaultTimeout(10000)
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    try {
      await page.goto(url)
      await page.getByRole('tab', { name: '规则列表' }).click()
      const list = page.getByRole('list', { name: '规则列表' })
      const rows = list.getByRole('listitem')
      const input = page.getByRole('searchbox')
      await page.waitForFunction(() => document.querySelector('[aria-setsize="10000"]'))

      await t.test('bounds DOM size and reaches the last rule with the keyboard', async () => {
        assert((await rows.count()) < 40)
        assert.equal(await rows.first().getAttribute('aria-posinset'), '1')
        await list.focus()
        await page.keyboard.press('PageDown')
        await page.waitForFunction(() => document.querySelector('[role=list]').scrollTop > 0)
        await page.keyboard.press('End')
        await page.waitForFunction(() => document.querySelector('[aria-posinset="10000"]'))
        assert((await rows.count()) < 40)
        assert.equal(await rows.last().getAttribute('aria-setsize'), '10000')
        await page.keyboard.press('Home')
        await page.waitForFunction(() => document.querySelector('[role=list]').scrollTop === 0)
      })

      await t.test('searches offscreen rules and resets scroll, including empty results', async () => {
        await list.evaluate(element => { element.scrollTop = element.scrollHeight })
        await input.fill('host-9997.example.com') // A rule not mounted at the top.
        await page.waitForFunction(() => document.querySelector('[aria-setsize="1"]'))
        assert.equal(await rows.count(), 1)
        assert.match(await rows.first().textContent(), /9998.*host-9997\.example\.com/)
        assert.equal(await list.evaluate(element => element.scrollTop), 0)
        await input.fill('no-such-rule')
        await page.getByRole('heading', { name: '暂无匹配的规则' }).waitFor()
        assert.equal(await list.count(), 0)
        await input.fill('')
        await page.waitForFunction(() => document.querySelector('[aria-setsize="10000"]'))
        assert.equal(await rows.first().getAttribute('aria-posinset'), '1')
        assert((await rows.count()) < 40)
      })

      await t.test('refreshes changed and shorter data without keeping stale row sizes', async () => {
        await page.evaluate(() => { window.fixtureRules = [{ index: 0, type: 'DomainSuffix', payload: 'changed.example.com'.repeat(25), proxy: 'DIRECT' }] })
        await page.getByRole('button', { name: '刷新', exact: true }).click()
        await page.waitForFunction(() => document.querySelector('[aria-setsize="1"]'))
        await settled(page)
        assert.equal(await rows.count(), 1)
        assert((await rows.first().boundingBox()).height > 43)
        const heights = await list.evaluate(element => ({ client: element.clientHeight, scroll: element.scrollHeight }))
        assert(Math.abs(heights.client - heights.scroll) <= 2)
      })

      await t.test('remeasures wrapped rules on resize without overlap or clipping', async () => {
        await page.goto(`${url}/?long=1`)
        await page.getByRole('tab', { name: '规则列表' }).click()
        await page.waitForFunction(() => document.querySelector('[aria-setsize="10000"]'))
        for (const width of [1000, 640, 600, 1200]) {
          await page.setViewportSize({ width, height: 700 })
          await settled(page)
          const geometry = await rows.evaluateAll(elements => elements.map(element => {
            const box = element.getBoundingClientRect()
            const content = [...element.children].map(child => child.getBoundingClientRect().bottom)
            return { top: box.top, bottom: box.bottom, height: box.height, contentBottom: Math.max(...content) }
          }))
          assert(geometry.length > 1 && geometry.length < 40)
          assert(geometry.some(row => row.height > 43), `expected wrapped rows at width ${width}`)
          for (let index = 0; index < geometry.length; index++) {
            assert(geometry[index].contentBottom <= geometry[index].bottom + 1, 'row content clipped')
            if (index) assert(geometry[index].top >= geometry[index - 1].bottom - 1, 'rows overlap after resize')
          }
        }
        await list.evaluate(element => { element.scrollTop = element.scrollHeight / 2 })
        await page.waitForFunction(() => Number(document.querySelector('[role=listitem]')?.getAttribute('aria-posinset')) > 1000)
        assert((await rows.count()) < 40)
      })
      assert.deepEqual(errors, [])
    } catch (error) {
      error.message += `\nPage errors: ${JSON.stringify(errors)}`
      throw error
    } finally { await browser.close() }
  })
}
