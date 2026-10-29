// Synthetic contract/UI verification only. Every API and WebSocket is intercepted.
// Run with READ_ALOUD_ORIGIN pointing to the current Vite app. No provider call.
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import {createHash} from 'node:crypto'
import assert from 'node:assert/strict'
const origin = process.env.READ_ALOUD_ORIGIN || 'http://127.0.0.1:18954'
const out = process.env.READ_ALOUD_EVIDENCE || '/tmp/memoh-native-back-browser'
fs.mkdirSync(out, { recursive: true })
const botId = '10000000-0000-4000-8000-000000000001', sessionId = '20000000-0000-4000-8000-000000000001'
const bot = { id: botId, name: 'primary-bot', display_name: 'Synthetic primary Bot', status: 'ready', current_user_permissions: ['manage'], chat_model_id: 'synthetic-chat', runtime_type: 'native' }
const contactId = '30000000-0000-4000-8000-000000000003'
const session = { id: sessionId, bot_id: botId, title: '仅合成音频 · 无模型调用', type: 'chat', runtime_type: 'native', channel_type: 'local', created_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z' }
// A playable, quiet 8-second PCM WAV. This verifies media events, not TTS quality.
function wav() {
  const rate = 8000, frames = rate * 8, data = Buffer.alloc(44 + frames * 2)
  data.write('RIFF', 0); data.writeUInt32LE(data.length - 8, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(rate, 24); data.writeUInt32LE(rate * 2, 28); data.writeUInt16LE(2, 32); data.writeUInt16LE(16, 34)
  data.write('data', 36); data.writeUInt32LE(frames * 2, 40)
  for (let i = 0; i < frames; i++) data.writeInt16LE(Math.round(Math.sin(i * Math.PI / 20) * 50), 44 + i * 2)
  return data
}
const browser = await chromium.launch({ headless: true, executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--autoplay-policy=document-user-activation-required'] })
let debugPage
const proof = { synthetic: true, realProviderCalls: 0, playbackRequests: [], writes: [], pageErrors: [], directoryReads:[], checks: [] }
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, hasTouch: true, colorScheme: 'light' })
  await context.addInitScript(() => {
    localStorage.setItem('token', 'synthetic-only'); localStorage.setItem('language', 'zh'); localStorage.setItem('memoh:harmony:service-config:v1', JSON.stringify({version:1,apiOrigin:'https://api.example.invalid/api',hapDownloadUrl:'',resetPending:false}))
    window.nativeRequests = []
    window.begoniaNative = { request: async raw => {
      const r = JSON.parse(raw); window.nativeRequests.push({command:r.command,photos:r.photos})
      if (r.command === 'voice.start' || r.command === 'voice.status') return JSON.stringify({ok:true,data:{handle:r.handle,state:'recording',durationMs:100,peaks:[0.2],text:'',speechError:''}})
      if (r.command.startsWith('upload.')) return JSON.stringify({ok:true,data:[]})
      if (r.command === 'download.fetch') {
        if (!r.url.startsWith('https://api.example.invalid/api/bots/') || r.authorization !== 'Bearer synthetic-only') return JSON.stringify({ok:false,error:'MEDIA_DOWNLOAD_HTTP_403'})
        return JSON.stringify({ok:true,data:{operation:r.operation,hash:r.hash,size:r.size}})
      }
      return JSON.stringify({ok:true,data:true})
    }}
    const NativeAudio = window.Audio
    window.auditSpeechPlayers = []
    window.Audio = class extends NativeAudio { constructor(...args) { super(...args); window.auditSpeechPlayers.push(this) } }
  })
  const page = await context.newPage(); debugPage = page; page.setDefaultTimeout(20000)
  page.on('pageerror', error => { proof.pageErrors.push(error.message); console.log('PAGEERROR_STACK', error.stack, 'after', proof.checks.at(-1)) })
  page.on('console', msg => { if (msg.type() === 'error') console.log('CONSOLE', msg.text()) })
  page.on('requestfailed', req => console.log('REQUESTFAILED', req.url(), req.failure()?.errorText))
  page.on('response', res => { if (res.status() >= 400) console.log('RESPONSE', res.status(), res.url()) })
  let socket, seq = 0, busy = false, run = 1, delayNext = false, explicit = false
  const audioHash=createHash('sha256').update(wav()).digest('hex')
  const directoryTask={record_id:'40000000-0000-4000-8000-000000000004',task_id:'opaque-task:windows:original',task_type:'computer_operation',name:'Synthetic original task',state:'running',created_at:'2026-10-28T10:00:00Z',destination:{bot_id:botId,session_id:sessionId,run_id:'run:'+sessionId,turn_id:'turn:'+sessionId}}
  const messageBlocks=(selected = sessionId)=>[{ id:0,type:'text',content:selected===contactId?'Synthetic contact assistant':'Synthetic task assistant' },...(explicit?[{id:1,type:'attachments',attachments:[{type:'audio',mime:'audio/wav',name:'synthetic.wav',bot_id:botId,content_hash:audioHash,size:wav().length}]}]:[])]
  function snapshot(selected = sessionId) {
    return { type: 'runtime_snapshot', session_id: selected, epoch: 'speech-fixture', seq: ++seq, snapshot: {
      bot_id: botId, session_id: selected, epoch: 'speech-fixture', seq,
      current_run_view: { run_id: `run:${selected}`, turn_id: `turn:${selected}`, generation: '1', status: busy ? 'running' : 'completed',
        started_at: '2026-10-28T10:00:00Z', updated_at: '2026-10-28T10:00:00Z',
        messages: messageBlocks(selected),
        request_user_turn: { turn_id: `turn:${selected}`, role: 'user', text: (selected === contactId?'Synthetic fixed contact history':'Synthetic retained task history'), timestamp: '2026-10-28T10:00:00Z' } },
      updated_at: '2026-10-28T10:00:00Z' } }
  }
  await page.routeWebSocket('**/api/bots/**', ws => { socket = ws; ws.onMessage(message => {
    const data = JSON.parse(message)
    if (data.type === 'runtime_subscribe') ws.send(JSON.stringify(snapshot(data.session_id || sessionId)))
  }) })
  await page.route('https://fonts.googleapis.com/**', route => route.fulfill({ contentType: 'text/css', body: '' }))
  await page.route('**/api/**', async route => {
    const req = route.request(), pathname = new URL(req.url()).pathname
    if (!pathname.startsWith('/api/')) return route.continue()
    if (req.method() === 'OPTIONS') return route.fulfill({status:204,headers:{'Access-Control-Allow-Origin':'*','Access-Control-Allow-Headers':'authorization,content-type','Access-Control-Allow-Methods':'GET,POST,OPTIONS'}})
    const path = pathname.slice(4),requestedSession=new URL(req.url()).searchParams.get('session_id') || (path.includes(contactId)?contactId:sessionId)
    if(path===`/bots/${botId}/tasks` || path===`/bots/${botId}/tasks/computer_operation/${directoryTask.record_id}`) {
      proof.directoryReads.push({path,authenticated:req.headers()['authorization']==='Bearer synthetic-only'})
      return route.fulfill({json:path.endsWith('/tasks')?{items:[directoryTask]}:directoryTask})
    }
    if (path.endsWith('/sessions/contact')) { proof.writes.push({path,method:req.method()});return route.fulfill({json:req.method()==='POST'?{...session,id:contactId,title:'Synthetic fixed contact'}:{session:{...session,id:contactId,title:'Synthetic fixed contact'},candidates:[]}}) }
    if (path.includes('/media/') && req.method()==='GET') return route.fulfill({contentType:'audio/wav',body:wav()})
    if (path.endsWith('/tts/playback')) {
      proof.playbackRequests.push(req.postDataJSON())
      if (delayNext) { delayNext = false; await new Promise(resolve => setTimeout(resolve, 500)) }
      return route.fulfill({ contentType: 'audio/wav', body: wav() }).catch(() => {})
    }
    if (!['GET', 'HEAD'].includes(req.method())) { proof.writes.push({ path, method: req.method() }); return route.fulfill({ json: {} }) }
    let body = { items: [] }
    if (path === '/users/me') body = { id: '11111111-1111-4111-8111-111111111111', username: 'synthetic', role: 'admin', metadata: { onboarding_completed: true } }
    else if (path === '/bots') body = { items: [bot] }
    else if (path === `/bots/${botId}`) body = bot
    else if (path.endsWith('/messages')) body = { items: busy ? [] : [
      { id: `history-user:${requestedSession}`, turn_id: `turn:${requestedSession}`, role: 'user', text: (requestedSession===contactId?'Synthetic fixed contact history':'Synthetic retained task history'), timestamp: '2026-10-28T10:00:00Z' },
      { id: `history-assistant:${requestedSession}`, turn_id: `turn:${requestedSession}`, role: 'assistant', messages: messageBlocks(requestedSession), timestamp: '2026-10-28T10:00:00Z' },
    ] }
    else if (path.endsWith('/sessions')) body = { items: [session,{...session,id:contactId,title:'Synthetic fixed contact'}] }
    else if (path.endsWith(`/sessions/${sessionId}`)) body = session
    else if (path.endsWith(`/sessions/${contactId}`)) body = {...session,id:contactId,title:'Synthetic fixed contact'}
    else if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: 'event: activity_ready\ndata: {"type":"activity_ready","cache_invalidation":true}\n\n' })
    else if (path.endsWith('/queue')) body = { follow_up: [], steer: [], steer_supported: true }
    else if (path === '/models') body = [{ id: 'synthetic-chat', name: 'Synthetic', model_id: 'synthetic', type: 'chat', client_type: 'openai-completions' }]
    else if (path === '/users/me/runtimes' || path === '/providers') body = []
    else if (path.includes('/settings')) body = { chat_model_id: 'synthetic-chat', tts_model_id: '' }
    return route.fulfill({ json: body })
  })
  await page.setViewportSize({width:390,height:844})
  await page.goto(`${origin}/#/bot/${botId}?session=${sessionId}`)
  await page.locator('textarea').waitFor({timeout:60000})
  await page.waitForFunction(id=>location.hash.includes('session='+id),sessionId)
  await page.waitForTimeout(700)
  await page.locator('textarea').fill('Synthetic task draft retained')
  const initial=await page.evaluate(async()=> {
    const {useWorkspaceTabsStore}=await import('/src/store/workspace-tabs.ts')
    return useWorkspaceTabsStore().api.panels.map(p=>({id:p.id,session:p.params?.sessionId}))
  })
  assert.ok(initial.some(p=>p.session===sessionId))
  await page.getByRole('button',{name:'任务',exact:true}).click()
  await page.locator('[role="dialog"]').waitFor()
  await page.getByText('Synthetic original task',{exact:true}).waitFor()
  proof.directoryVisibleRows=await page.locator('[role=dialog] ul').innerText()
  assert.equal(proof.directoryVisibleRows.trim().split(/\n+/).length,2,'list row contains only name and localized actual status')
  await page.screenshot({path:`${out}/task-directory.png`})
  assert.equal(await page.evaluate(()=>window.__memohBack()),true)
  await page.locator('[role="dialog"]').waitFor({state:'hidden'})
  assert.ok((await page.evaluate(()=>location.hash)).includes('session='+sessionId))
  proof.checks.push('modal consumes back before explicit task route')
  await page.screenshot({path:`${out}/task-before.png`})
  await page.evaluate(async()=> {
    const {default:router}=await import('/src/router.ts'),{useChatStore}=await import('/src/store/chat-list.ts')
    window.auditNavigation=[]
    const replace=router.replace.bind(router),chat=useChatStore(),select=chat.selectBot
    router.replace=(to)=>{window.auditNavigation.push({owner:'route',to});return replace(to)}
    chat.selectBot=(...args)=>{window.auditNavigation.push({owner:'shared-contact',bot:args[0]});return select(...args)}
  })
  assert.equal(await page.evaluate(()=>window.__memohBack()),true)
  await page.waitForFunction(id=>!location.hash.includes('session=') && !!document.querySelector('textarea'),contactId)
  await page.waitForFunction(async id=> {
    const {useWorkspaceTabsStore}=await import('/src/store/workspace-tabs.ts')
    return useWorkspaceTabsStore().api.activePanel?.params?.sessionId===id
  },contactId)
  const after=await page.evaluate(async()=> {
    const {useWorkspaceTabsStore}=await import('/src/store/workspace-tabs.ts')
    return {panels:useWorkspaceTabsStore().api.panels.map(p=>({id:p.id,session:p.params?.sessionId})),token:localStorage.getItem('token'),navigation:window.auditNavigation,hash:location.hash}
  })
  assert.ok(after.panels.some(p=>p.session===sessionId),'original task tab survives contact return')
  assert.ok(after.panels.some(p=>p.session===contactId),'server-owned contact tab is opened')
  assert.equal(after.token,'synthetic-only')
  assert.equal(after.navigation[0].owner,'route','route write precedes shared selection; no selectBot-before-router race')
  assert.equal(after.navigation.filter(n=>n.owner==='route').length,1)
  assert.equal(await page.evaluate(()=>window.__memohBack()),false,'actual contact root retains ordinary background behavior')
  await page.getByText('Synthetic fixed contact history',{exact:true}).waitFor()
  assert.equal(await page.getByText('Synthetic retained task history',{exact:true}).evaluateAll(nodes=>nodes.some(el=>el.getClientRects().length>0)),false,'old task history cannot appear under the contact')
  await page.screenshot({path:`${out}/contact-after.png`})
  proof.checks.push('one route-owned contact return, original task tab and login retained; actual contact root unhandled')
  await page.getByRole('button',{name:'任务',exact:true}).click()
  await page.getByRole('button',{name:/Synthetic original task/}).click()
  await page.waitForFunction(async id=> {const {useWorkspaceTabsStore}=await import('/src/store/workspace-tabs.ts');return useWorkspaceTabsStore().api.activePanel?.params?.sessionId===id},sessionId)
  await page.waitForFunction(()=>document.querySelector('textarea')?.value==='Synthetic task draft retained')
  assert.equal(await page.locator('textarea').inputValue(),'Synthetic task draft retained')
  proof.checks.push('original explicit task deep link reopens its retained draft without rebinding')
  await page.getByText('Synthetic task assistant',{exact:true}).waitFor()
  proof.exactTaskAnchor=await page.evaluate(async()=>{const {useWorkspaceTabsStore}=await import('/src/store/workspace-tabs.ts');return useWorkspaceTabsStore().api.activePanel.params.taskAnchor})
  assert.equal(proof.exactTaskAnchor.runId,'run:'+sessionId)
  assert.equal(proof.exactTaskAnchor.turnId,'turn:'+sessionId)
  assert.equal(proof.exactTaskAnchor.recordId,directoryTask.record_id)
  assert.ok((await page.evaluate(()=>location.hash)).includes('task_record='))
  proof.checks.push('original run/turn/record anchor survives route-to-Dockview and its exact task history is visible')
  proof.navigation=after.navigation;proof.panels=after.panels
  assert.ok(proof.directoryReads.some(r=>r.path.endsWith('/'+directoryTask.record_id)))
  assert.ok(proof.directoryReads.every(r=>r.authenticated),'new generated directory SDK retains account auth and configured /api base')
  proof.checks.push('new directory SDK list/detail use original record identity and account auth; displayed row is only name/state; click reopens original task')
  assert.deepEqual(proof.pageErrors,[])
  assert.ok(proof.writes.every(r=>r.method==='POST'&&r.path.endsWith('/sessions/contact')),'only existing contact resolution writes; no binding, task or model submission')
  fs.writeFileSync(`${out}/result.json`,JSON.stringify(proof,null,2));console.log(JSON.stringify(proof,null,2))
} catch(error) {
  if(debugPage){fs.writeFileSync(`${out}/failure.txt`,await debugPage.locator('body').innerText());await debugPage.screenshot({path:`${out}/failure.png`})}
  fs.writeFileSync(`${out}/failure.json`,JSON.stringify(proof,null,2));throw error
} finally {await browser.close()}
