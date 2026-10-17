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
const runtimeFixture = `
  window.fixtureState = {status:new URLSearchParams(location.search).has('modal')||new URLSearchParams(location.search).has('proxies')||new URLSearchParams(location.search).has('streaming')?'stopped':'starting',port:7890,hasProfile:true,error:'',systemProxyEnabled:false,tunEnabled:false,tunSupported:true,routingMode:'rule',configVersion:1}
  window.fixtureOperation = {id:0,active:false,cancellable:false}
  window.listeners = new Map()
  window.emit = (name,data) => { for(const callback of window.listeners.get(name) ?? []) callback({data}) }
  window.setOperation = value => { window.fixtureOperation = value; window.emit('operation-progress',value) }
  export async function State(){return window.fixtureState}
  export async function Operation(){return window.fixtureOperation}
  export async function SetSystemProxy(enabled){
    window.fixtureState = {...window.fixtureState,status:'stopped'}
    window.emit('runtime-state',window.fixtureState)
    return window.fixtureState
  }
  export const SetTun = SetSystemProxy
  export const SetRoutingMode = State
  export async function ExitIPInfo(){return {}}
  window.fixtureNodes = ['Bronze-台湾-01', 'Bronze-新加坡-01', 'Bronze-very-long-node-name-with-region-and-provider-details-01']
  export async function Groups(){return [{name:'Main',current:window.fixtureNodes[0],options:window.fixtureNodes}]}
  export async function NodeNames(){return window.fixtureNodes}
  export async function Select(){}
  export function TestGroupDelay(group){
    window.setOperation({id:window.fixtureOperation.id+1,kind:'delay',target:group,phase:'testing',completed:0,total:3,active:true,cancellable:true})
    return new Promise((resolve,reject) => {
      window.finishDelay = delays => {window.setOperation({...window.fixtureOperation,active:false,cancellable:false});resolve(delays)}
      window.cancelDelay = () => {window.setOperation({...window.fixtureOperation,active:false,cancellable:false});reject(new Error('操作已取消'))}
    })
  }
`

before(async () => {
  directory = await mkdtemp(path.join(tmpdir(), 'vela-operation-test-'))
  await writeFile(path.join(directory, 'index.html'), '<!doctype html><html><meta charset="utf-8"><div id="root"></div><script type="module" src="/entry.tsx"></script></html>')
  await writeFile(path.join(directory, 'entry.tsx'), `
    import React from ${JSON.stringify(require.resolve('react'))}
    import {createRoot} from ${JSON.stringify(require.resolve('react-dom/client'))}
    import {useRuntime} from ${JSON.stringify(path.join(frontend, 'src/app/useRuntime.ts'))}
    import {useProxies} from ${JSON.stringify(path.join(frontend, 'src/features/proxies/useProxies.ts'))}
    import HomePage from ${JSON.stringify(path.join(frontend, 'src/features/overview/HomePage.tsx'))}
    import ProxiesPage from ${JSON.stringify(path.join(frontend, 'src/features/proxies/ProxiesPage.tsx'))}
    import SubscriptionEditorDialog from ${JSON.stringify(path.join(frontend, 'src/features/profiles/SubscriptionEditorDialog.tsx'))}
    import ${JSON.stringify(path.join(frontend, 'src/style.css'))}
    const nodeNames = ['Bronze-台湾-01', 'Bronze-新加坡-01', 'Bronze-very-long-node-name-with-region-and-provider-details-01']
    const proxyController = {
      groups: [{name:'Main',current:nodeNames[0],options:nodeNames}], nodeNames,
      expandedGroups:{}, groupDelays:{Main:{[nodeNames[0]]:369,[nodeNames[1]]:552,[nodeNames[2]]:1280}},
      measuredGroups:{Main:true}, testingGroup:null, sortModes:{},
      select:()=>{},toggleGroup:()=>{},testGroupDelay:()=>{},toggleGroupSort:()=>{},
    }
    function StreamingProxies({runtime,language}){
      const controller = useProxies(runtime,true,'proxies')
      window.currentProxies = controller
      return <div className="app-layout"><aside className="sidebar"/><main className="content"><ProxiesPage runtime={runtime} controller={controller} language={language} onNavigate={()=>{}}/></main></div>
    }
    function Fixture(){
      const runtime = useRuntime(true)
      const language = new URLSearchParams(location.search).has('zh') ? 'zh-CN' : 'en-US'
      const modal = new URLSearchParams(location.search).has('modal')
      window.currentRuntime = runtime
      window.startDownload = () => runtime.execute(() => new Promise(resolve => {window.finishDownload = () => resolve(window.fixtureState)}))
      if(new URLSearchParams(location.search).has('streaming')) return <StreamingProxies runtime={runtime} language={language}/>
      if(new URLSearchParams(location.search).has('proxies')) return <div className="app-layout"><aside className="sidebar"/><main className="content"><ProxiesPage runtime={runtime} controller={proxyController} language={language} onNavigate={()=>{}}/></main></div>
      return <main className="content">
        <output data-testid="busy">{String(runtime.controlsBusy)}</output>
        <HomePage runtime={runtime} groups={[]} language={language} visible onNavigate={()=>{}}/>
        {modal && <SubscriptionEditorDialog url="https://example.invalid/sub" domain="example.invalid" language={language} busy={runtime.controlsBusy} error="" onClose={()=>{}} onSave={async()=>{}}/>}
      </main>
    }
    createRoot(document.getElementById('root')).render(<Fixture/>)
  `)
  await build({
    configFile: false, root: directory, publicDir: false, logLevel: 'silent',
    plugins: [{
      name: 'operation-fixture', enforce: 'pre',
      resolveId(source) {
        if (source.endsWith('/runtimeservice')) return '\0fixture-runtime'
        if (source === '@wailsio/runtime') return '\0fixture-events'
        if (source === 'react' || source.startsWith('react/')) return require.resolve(source)
      },
      load(id) {
        if (id === '\0fixture-runtime') return runtimeFixture
        if (id === '\0fixture-events') return 'export const Events={On(name,callback){const callbacks=window.listeners.get(name)??new Set();window.listeners.set(name,callbacks);callbacks.add(callback);return()=>callbacks.delete(callback)}}'
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
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve) })
  url = `http://127.0.0.1:${server.address().port}`
})
after(async () => {
  if (server) await new Promise(resolve => server.close(resolve))
  if (directory) await rm(directory, { recursive: true, force: true })
})

for (const [name, engine] of [['WebKit', webkit], ['Chromium', chromium]]) {
  test(`${name}: node names and latency share one compact row`, async () => {
    const browser = await engine.launch()
    const page = await browser.newPage()
    try {
      for (const width of [640, 1000, 1280]) {
        await page.setViewportSize({ width, height: 700 })
        await page.goto(url + '/?proxies&zh')
        const cards = page.locator('.proxy-node')
        await cards.first().waitFor()
        assert.equal(await cards.count(), 3)
        for (const card of await cards.all()) {
          const bounds = await card.boundingBox()
          const title = await card.locator('strong').boundingBox()
          const delay = await card.locator('.proxy-node-delay').boundingBox()
          assert(Math.abs(title.y + title.height / 2 - delay.y - delay.height / 2) < 1)
          assert(title.x + title.width <= delay.x - 8)
          assert(delay.x + delay.width <= bounds.x + bounds.width - 10)
          assert(bounds.height <= 50)
        }
        const longName = cards.locator('strong[title^="Bronze-very-long"]')
        assert(await longName.evaluate(element => element.scrollWidth > element.clientWidth && getComputedStyle(element).textOverflow === 'ellipsis'))
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
      }
    } finally { await browser.close() }
  })
  test(`${name}: completed latency probes appear before the group finishes`, async () => {
    const browser = await engine.launch()
    const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
    page.setDefaultTimeout(10000)
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    const card = node => page.locator('.proxy-node').filter({ has: page.locator(`strong[title="${node}"]`) })
    try {
      await page.goto(url + '/?streaming')
      await page.locator('.proxy-node').first().waitFor()
      const nodes = await page.evaluate(() => window.fixtureNodes)
      const testButton = page.getByRole('button', { name: 'Test node latency: Main', exact: true })
      await testButton.click()
      await page.waitForFunction(() => document.querySelectorAll('.proxy-node-delay.testing').length === 3)
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, completed: 1, delays: { [window.fixtureNodes[0]]: 37 } }))
      await card(nodes[0]).getByText('37 ms', { exact: true }).waitFor()
      assert.equal(await page.locator('.proxy-node-delay.testing').count(), 2)
      assert.equal(await page.evaluate(() => window.currentProxies.testingGroup), 'Main')
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, completed: 2, delays: { ...window.fixtureOperation.delays, [window.fixtureNodes[1]]: 0 } }))
      await card(nodes[1]).getByText('Failed', { exact: true }).waitFor()
      assert.equal(await page.locator('.proxy-node-delay.testing').count(), 1)
      // Failed probes must sort after successful measurements, even during testing.
      await page.locator('.proxy-group-action.sort').click()
      assert.equal(await page.locator('.proxy-node strong').first().getAttribute('title'), nodes[0])
      await page.evaluate(() => window.finishDelay({ [window.fixtureNodes[0]]: 37, [window.fixtureNodes[2]]: 83 }))
      await card(nodes[2]).getByText('83 ms', { exact: true }).waitFor()
      await page.waitForFunction(() => window.currentProxies.testingGroup === null)
      await card(nodes[1]).getByText('Failed', { exact: true }).waitFor()
      await testButton.click()
      await page.waitForFunction(() => document.querySelectorAll('.proxy-node-delay.testing').length === 3)
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, completed: 1, delays: { [window.fixtureNodes[0]]: 41 } }))
      await card(nodes[0]).getByText('41 ms', { exact: true }).waitFor()
      await page.evaluate(() => window.cancelDelay())
      await page.waitForFunction(() => window.currentProxies.testingGroup === null)
      await card(nodes[0]).getByText('41 ms', { exact: true }).waitFor()
      assert.equal(await page.locator('.proxy-node-delay.testing').count(), 0)
      await testButton.click()
      await page.waitForFunction(() => document.querySelectorAll('.proxy-node-delay.testing').length === 3)
      await page.evaluate(() => {
        window.fixtureState = { ...window.fixtureState, configVersion: 2 }
        window.emit('runtime-state', window.fixtureState)
      })
      await page.waitForFunction(() => window.currentRuntime.profileRevision === 2)
      await page.evaluate(() => {
        window.setOperation({ ...window.fixtureOperation, completed: 1, delays: { [window.fixtureNodes[0]]: 999 } })
        window.finishDelay({ [window.fixtureNodes[0]]: 999 })
      })
      await page.waitForFunction(() => window.currentProxies.testingGroup === null)
      assert.equal(await page.getByText('999 ms', { exact: true }).count(), 0)
      assert.equal(await page.locator('.proxy-node-delay.measured').count(), 0)
      assert.deepEqual(errors, [])
    } finally { await browser.close() }
  })
  test(`${name}: startup and task busy states work without extra controls`, async () => {
    const browser = await engine.launch()
    const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
    page.setDefaultTimeout(10000)
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    try {
      await page.goto(url)
      await page.waitForFunction(() => window.currentRuntime.state.status === 'starting')
      assert.equal(await page.getByRole('button', { name: 'Cancel startup', exact: true }).count(), 0)
      for (const control of await page.getByRole('switch').all()) {
        assert.equal(await control.isDisabled(), true)
      }
      await page.evaluate(() => {
        window.fixtureState = { ...window.fixtureState, status: 'stopped' }
        window.emit('runtime-state', window.fixtureState)
      })
      await page.waitForFunction(() => !window.currentRuntime.controlsBusy)
      await page.evaluate(() => {
        window.startDownload()
        window.setOperation({ id: 7, kind: 'delay', target: 'Auto', phase: 'testing', completed: 3, total: 12, active: true, cancellable: true })
      })
      await page.waitForFunction(() => window.currentRuntime.operation?.id === 7)
      assert.equal(await page.locator('.operation-status').count(), 0)
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, kind: 'subscription', phase: 'downloading', target: '', completed: 0, total: 1 }))
      await page.waitForFunction(() => window.currentRuntime.operation?.kind === 'subscription')
      assert.equal(await page.locator('.operation-status').count(), 0)
      assert.equal(await page.getByRole('progressbar').count(), 0)
      // Task completion must not clear the original pending action early.
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, active: false }))
      await page.waitForFunction(() => !window.currentRuntime.operation?.active)
      assert.equal(await page.getByTestId('busy').textContent(), 'true')
      await page.evaluate(() => window.finishDownload())
      await page.waitForFunction(() => !window.currentRuntime.controlsBusy)
      await page.evaluate(() => {
        window.setOperation({ id: 8, kind: 'subscription', phase: 'applying', active: true, cancellable: false, completed: 1, total: 1 })
      })
      await page.waitForFunction(() => window.currentRuntime.operation?.phase === 'applying')
      assert.equal(await page.locator('.operation-status').count(), 0)
      assert.equal(await page.getByTestId('busy').textContent(), 'true')
      await page.setViewportSize({ width: 640, height: 480 })
      await page.goto(url + '/?modal&zh')
      const dialog = page.getByRole('dialog')
      await dialog.waitFor()
      await page.evaluate(() => window.setOperation({ id: 9, kind: 'subscription', phase: 'downloading', active: true, cancellable: true, completed: 0, total: 1 }))
      await page.waitForFunction(() => window.currentRuntime.operation?.id === 9)
      assert.equal(await dialog.getByRole('progressbar').count(), 0)
      assert.equal(await dialog.locator('.operation-status').count(), 0)
      assert.equal(await dialog.getByLabel('订阅链接', { exact: true }).isDisabled(), true)
      await dialog.getByRole('button', { name: '保存并更新中…', exact: true }).waitFor()
      await page.evaluate(() => window.setOperation({ ...window.fixtureOperation, active: false }))
      await page.waitForFunction(() => !window.currentRuntime.controlsBusy)
      assert.equal(await dialog.getByLabel('订阅链接', { exact: true }).isEnabled(), true)
      assert.deepEqual(errors, [])
    } finally { await browser.close() }
  })
}
