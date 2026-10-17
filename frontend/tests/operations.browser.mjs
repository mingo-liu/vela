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
  window.fixtureState = {status:'starting',port:7890,hasProfile:true,error:'',systemProxyEnabled:false,tunEnabled:false,tunSupported:true,routingMode:'rule',configVersion:1}
  window.fixtureOperation = {id:0,active:false,cancellable:false}
  window.calls = {cancel:[],disconnect:[]}
  window.listeners = new Map()
  window.emit = (name,data) => { for(const callback of window.listeners.get(name) ?? []) callback({data}) }
  window.setOperation = value => { window.fixtureOperation = value; window.emit('operation-progress',value) }
  export async function State(){return window.fixtureState}
  export async function Operation(){return window.fixtureOperation}
  export async function CancelOperation(id){
    window.calls.cancel.push(id)
    if(id !== window.fixtureOperation.id || !window.fixtureOperation.cancellable) return false
    window.setOperation({...window.fixtureOperation,phase:'cancelling',cancellable:false})
    await new Promise(resolve => {window.finishCancel = resolve})
    return true
  }
  export async function SetSystemProxy(enabled){
    window.calls.disconnect.push(enabled)
    window.fixtureState = {...window.fixtureState,status:'stopped'}
    window.emit('runtime-state',window.fixtureState)
    return window.fixtureState
  }
  export const SetTun = SetSystemProxy
  export const SetRoutingMode = State
  export async function ExitIPInfo(){return {}}
`

before(async () => {
  directory = await mkdtemp(path.join(tmpdir(), 'vela-operation-test-'))
  await writeFile(path.join(directory, 'index.html'), '<!doctype html><html><meta charset="utf-8"><div id="root"></div><script type="module" src="/entry.tsx"></script></html>')
  await writeFile(path.join(directory, 'entry.tsx'), `
    import React from ${JSON.stringify(require.resolve('react'))}
    import {createRoot} from ${JSON.stringify(require.resolve('react-dom/client'))}
    import {useRuntime} from ${JSON.stringify(path.join(frontend, 'src/app/useRuntime.ts'))}
    import OperationStatus from ${JSON.stringify(path.join(frontend, 'src/app/OperationStatus.tsx'))}
    import HomePage from ${JSON.stringify(path.join(frontend, 'src/features/overview/HomePage.tsx'))}
    import SubscriptionEditorDialog from ${JSON.stringify(path.join(frontend, 'src/features/profiles/SubscriptionEditorDialog.tsx'))}
    import ${JSON.stringify(path.join(frontend, 'src/style.css'))}
    function Fixture(){
      const runtime = useRuntime(true)
      const language = new URLSearchParams(location.search).has('zh') ? 'zh-CN' : 'en-US'
      const modal = new URLSearchParams(location.search).has('modal')
      window.currentRuntime = runtime
      window.startDownload = () => runtime.execute(() => new Promise(resolve => {window.finishDownload = () => resolve(window.fixtureState)}))
      const cancelling = runtime.cancellingOperationID === runtime.operation?.id
      return <main className="content">
        <output data-testid="busy">{String(runtime.controlsBusy)}</output>
        <OperationStatus operation={runtime.operation} language={language} cancelling={cancelling} onCancel={runtime.cancelOperation}/>
        <HomePage runtime={runtime} groups={[]} language={language} visible onNavigate={()=>{}}/>
        {modal && <SubscriptionEditorDialog url="https://example.invalid/sub" domain="example.invalid" language={language} busy={runtime.controlsBusy} operation={runtime.operation} cancelling={cancelling} onCancelOperation={runtime.cancelOperation} error="" onClose={()=>{}} onSave={async()=>{}}/>}
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
  test(`${name}: startup and task cancellation remain usable while busy`, async () => {
    const browser = await engine.launch()
    const page = await browser.newPage({ viewport: { width: 1000, height: 700 } })
    page.setDefaultTimeout(10000)
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    try {
      await page.goto(url)
      await page.getByRole('button', { name: 'Cancel startup', exact: true }).click()
      await page.waitForFunction(() => window.calls.disconnect.length === 1 && !window.currentRuntime.controlsBusy)
      assert.deepEqual(await page.evaluate(() => window.calls.disconnect), [false])
      assert.equal(await page.getByRole('button', { name: 'Cancel startup', exact: true }).count(), 0)
      await page.evaluate(() => {
        window.startDownload()
        window.setOperation({ id: 7, kind: 'delay', target: 'Auto', phase: 'testing', completed: 3, total: 12, active: true, cancellable: true })
      })
      const progress = page.getByRole('progressbar', { name: 'Delay test progress' })
      await progress.waitFor()
      assert.equal(await progress.getAttribute('value'), '3')
      await page.getByRole('button', { name: 'Cancel task', exact: true }).click()
      await page.getByRole('status').filter({ hasText: 'Cancelling…' }).waitFor()
      assert.equal(await page.getByRole('button', { name: 'Cancel task', exact: true }).isDisabled(), true)
      assert.deepEqual(await page.evaluate(() => window.calls.cancel), [7])
      // Completing cancellation must not clear the original pending action early.
      await page.evaluate(() => { window.setOperation({ ...window.fixtureOperation, active: false }); window.finishCancel() })
      await page.waitForFunction(() => window.currentRuntime.cancellingOperationID === null)
      assert.equal(await page.getByTestId('busy').textContent(), 'true')
      await page.evaluate(() => window.finishDownload())
      await page.waitForFunction(() => !window.currentRuntime.controlsBusy)
      await page.evaluate(() => {
        window.setOperation({ id: 8, kind: 'subscription', phase: 'applying', active: true, cancellable: false, completed: 1, total: 1 })
      })
      await page.getByText('Applying configuration…', { exact: true }).waitFor()
      assert.equal(await page.getByRole('button', { name: 'Cancel task', exact: true }).isDisabled(), true)
      assert.deepEqual(await page.evaluate(() => window.calls.cancel), [7])
      await page.evaluate(() => window.currentRuntime.cancelOperation(7))
      assert.deepEqual(await page.evaluate(() => window.calls.cancel), [7])
      await page.setViewportSize({ width: 640, height: 480 })
      await page.goto(url + '/?modal&zh')
      const dialog = page.getByRole('dialog')
      await dialog.waitFor()
      await page.evaluate(() => window.setOperation({ id: 9, kind: 'subscription', phase: 'downloading', active: true, cancellable: true, completed: 0, total: 1 }))
      await dialog.getByRole('button', { name: '取消任务', exact: true }).click()
      await dialog.getByText('正在取消…', { exact: true }).waitFor()
      assert.deepEqual(await page.evaluate(() => window.calls.cancel), [9])
      await page.evaluate(() => { window.setOperation({ ...window.fixtureOperation, active: false }); window.finishCancel() })
      await page.waitForFunction(() => window.currentRuntime.cancellingOperationID === null)
      assert.deepEqual(errors, [])
    } finally { await browser.close() }
  })
}
