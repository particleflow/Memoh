import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
const out=process.env.GARNET_EVIDENCE_DIR
if(!out||!process.env.GARNET_TOKEN_FILE)throw new Error('GARNET_EVIDENCE_DIR and GARNET_TOKEN_FILE are required')
fs.mkdirSync(out,{recursive:true})
const token=fs.readFileSync(process.env.GARNET_TOKEN_FILE,'utf8')
const botId='10000000-0000-4000-8000-000000000001',sessionId='20000000-0000-4000-8000-000000000001'
const bot={id:botId,name:'Garnet UI synthetic',display_name:'Garnet UI synthetic',status:'ready',current_user_permissions:['manage'],chat_model_id:'model',runtime_type:'native'}
const session={id:sessionId,bot_id:botId,title:'共享界面验证（合成会话）',type:'chat',runtime_type:'native',channel_type:'local',created_at:'2026-10-27T09:00:00Z',updated_at:'2026-10-27T09:00:00Z'}
const b=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox']})
const c=await b.newContext({viewport:{width:1280,height:900},colorScheme:'light'})
await c.addInitScript(token=>{localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://garnet-ui.invalid/api',hapDownloadUrl:'',resetPending:false}));localStorage.setItem('token',token);localStorage.setItem('language','zh')},token)
const p=await c.newPage();p.setDefaultTimeout(15000)
const errors=[],writes=[],reads=[];let items=[],failNext=false,socket,seq=0,busy=true
function snapshot(){return {type:'runtime_snapshot',session_id:sessionId,epoch:'ui-epoch',seq:++seq,snapshot:{bot_id:botId,session_id:sessionId,epoch:'ui-epoch',seq,current_run_view:{run_id:'30000000-0000-4000-8000-000000000001',turn_id:'40000000-0000-4000-8000-000000000001',generation:'1',status:busy?'running':'completed',started_at:'2026-10-27T09:00:00Z',updated_at:'2026-10-27T09:00:00Z',messages:[{id:0,type:'text',content:'正在执行合成任务，用于验证发送排队与主题。'}],request_user_turn:{turn_id:'40000000-0000-4000-8000-000000000001',role:'user',text:'界面验证，请继续执行。',timestamp:'2026-10-27T09:00:00Z'}},updated_at:'2026-10-27T09:00:00Z'}}}
p.on('pageerror',e=>errors.push(e.message))
await p.routeWebSocket('**/api/bots/**',ws=>{socket=ws;ws.onMessage(message=>{const data=JSON.parse(message);writes.push({ws:data});if(data.type==='runtime_subscribe')ws.send(JSON.stringify(snapshot()));if(data.type==='abort'){busy=false;ws.send(JSON.stringify({type:'control_ack',control_id:data.control_id}));ws.send(JSON.stringify(snapshot()))}})})
await p.route('**/api/**',async r=>{
 const req=r.request(),u=new URL(req.url());if(!u.pathname.startsWith('/api/'))return r.continue();const path=u.pathname.slice(4); reads.push(path)
 const originalFulfill=r.fulfill.bind(r);r.fulfill=options=>originalFulfill({...options,headers:{...options.headers,'Access-Control-Allow-Origin':'*','Access-Control-Allow-Headers':'authorization,content-type','Access-Control-Allow-Methods':'GET,POST,PATCH,DELETE,OPTIONS,HEAD'}})
 if(req.method()==='OPTIONS')return r.fulfill({status:204})
 if(path==='/users/me'){const response=await fetch('http://127.0.0.1:18080/users/me',{headers:{Authorization:'Bearer '+token}});const me=await response.json();me.metadata={...me.metadata,onboarding_completed:true};return r.fulfill({json:me})}
 let body={items:[]}
 if(req.method()!=='GET'&&req.method()!=='HEAD'){
   writes.push({path,method:req.method(),body:req.postDataJSON()})
   if(path.endsWith('/follow-up-queue')){
     if(failNext){failNext=false;return r.fulfill({status:503,json:{code:'queue_admission_unavailable'}})}
     body={...req.postDataJSON(),item_id:'item-'+writes.length,status:'accepted',position:items.length+1};items.push(body)
   }else if(path.endsWith('/steer')){const id=path.split('/').at(-2);body=items.find(x=>x.item_id===id);body.kind='steer'}
   return r.fulfill({json:body})
 }
 if(path==='/bots')body={items:[bot]}
 else if(path===`/bots/${botId}`)body=bot
 else if(path.endsWith('/sessions'))body={items:[session]}
 else if(path.endsWith('/sessions/'+sessionId))body=session
 else if(path.endsWith('/queue'))body={follow_up:items.filter(x=>!x.kind),steer:items.filter(x=>x.kind),steer_supported:true}
 else if(path.endsWith('/events'))return r.fulfill({contentType:'text/event-stream',body:'event: ping\ndata: {"type":"ping"}\n\n'})
 else if(path==='/models')body=[{id:'model',name:'Synthetic model',model_id:'synthetic',type:'chat',client_type:'openai-completions'}]
 else if(path==='/users/me/runtimes'||path==='/providers')body=[]
 else if(path.includes('/settings'))body={chat_model_id:'model'}
 return r.fulfill({json:body})
})
await p.goto('http://127.0.0.1:18084/#/bot/'+botId)
await p.waitForFunction(()=>document.querySelector('#app')?.__vue_app__,{},{timeout:60000});await p.waitForTimeout(2000)
await p.getByText('共享界面验证（合成会话）',{exact:true}).first().click();await p.getByText('正在执行合成任务，用于验证发送排队与主题',{exact:false}).first().waitFor();await p.waitForTimeout(500)
const input=p.locator('textarea').first()
await input.fill('完成当前任务后，再处理这一条')
if(process.env.GARNET_BEFORE){
 await p.screenshot({path:out+'/before-busy.png'})
 fs.writeFileSync(out+'/before.json',JSON.stringify({source:'3d5f145c452dda5e17861d401de049b7ddded2e6',separateStop:await p.getByRole('button',{name:'停止生成回复',exact:true}).count(),sendRect:await p.getByRole('button',{name:'添加到接下来',exact:true}).locator('rect').count(),errors},null,2))
 await b.close();process.exit(0)
}
const stop=p.getByRole('button',{name:'停止生成回复',exact:true})
const send=()=>p.getByRole('button',{name:'添加到接下来',exact:true})
await stop.waitFor();assert.equal(await send().locator('rect').count(),0)
await p.screenshot({path:out+'/busy-desktop.png'})
const box=await send().boundingBox();await p.mouse.dblclick(box.x+box.width/2,box.y+box.height/2)
await p.locator('[data-queue-item] input').first().waitFor()
assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).length,1)
assert.equal(writes.filter(x=>x.ws?.type==='abort').length,0)
assert.equal(writes.filter(x=>x.path?.endsWith('/steer')).length,0)
assert(await p.getByRole('button',{name:'发送',exact:true}).isDisabled())
await input.press('Enter');assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).length,1)
await p.locator('[data-queue-item]').first().getByRole('button',{name:'插入当前回复',exact:true}).click()
await p.locator('[data-queue-steer-status]').waitFor()
assert.equal(writes.filter(x=>x.path?.endsWith('/steer')).length,1)
await input.fill('键盘排队');await input.press('Shift+Enter');assert((await input.inputValue()).includes('\n'))
await input.press('Enter');await p.waitForTimeout(200)
assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).length,2)
await input.fill('输入法尚未确认')
await input.dispatchEvent('keydown',{key:'Enter',code:'Enter',isComposing:true})
assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).length,2)
await input.fill('失败后重试同一条');failNext=true;await send().click()
await p.waitForFunction(()=>document.querySelector('textarea')?.value==='失败后重试同一条')
const failed=writes.filter(x=>x.path?.endsWith('/follow-up-queue')).at(-1)
await send().click();await p.waitForTimeout(250)
assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).at(-1).body.invocation_id,failed.body.invocation_id)
assert.equal(writes.filter(x=>x.ws?.type==='message'||x.ws?.type==='abort').length,0)
await p.setViewportSize({width:390,height:844});await p.emulateMedia({colorScheme:'dark'})
await input.fill('窄屏执行中也可以继续排队')
await p.waitForTimeout(300)
assert(await stop.isVisible());assert(await send().isEnabled())
assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
await p.screenshot({path:out+'/busy-narrow-dark.png'})
await stop.click();await p.waitForTimeout(250)
assert.equal(writes.filter(x=>x.ws?.type==='abort').length,1)
await stop.waitFor({state:'hidden'})
assert(await p.getByRole('button',{name:'发送',exact:true}).isEnabled())
// A file selected while idle must not become plain text in the busy queue.
await p.locator('input[type=file]').last().setInputFiles({name:'synthetic.txt',mimeType:'text/plain',buffer:Buffer.from('synthetic attachment')})
await p.waitForTimeout(200);busy=true;socket.send(JSON.stringify(snapshot()));await stop.waitFor()
assert(await send().isDisabled())
await input.press('Enter');assert.equal(writes.filter(x=>x.path?.endsWith('/follow-up-queue')).length,4)
busy=false;socket.send(JSON.stringify(snapshot()));await stop.waitFor({state:'hidden'})
// The mounted application settings store remains the only theme authority.
await p.goto('http://127.0.0.1:18084/#/settings/appearance')
await p.getByRole('radio',{name:'跟随系统',exact:true}).waitFor();assert(await p.getByRole('radio',{name:'暖白',exact:true}).isVisible())
assert.equal(await p.evaluate(()=>localStorage.getItem('theme')),'system')
await p.emulateMedia({colorScheme:'light'});await p.waitForFunction(()=>document.documentElement.classList.contains('light'))
await p.emulateMedia({colorScheme:'dark'});await p.waitForFunction(()=>document.documentElement.classList.contains('dark'))
const themes=[]
for(const [mode,label] of [['light','浅色'],['dark','深色']]){
 await p.getByRole('radio',{name:label,exact:true}).click()
 for(const [scheme,name] of [['memoh','Memoh'],['ocean','海洋'],['forest','森林'],['rose','玫瑰'],['amber','琥珀']]){
  const select=p.getByRole('combobox').filter({hasText:/Memoh|海洋|森林|玫瑰|琥珀/}).first()
  await select.click();await p.getByRole('option',{name:new RegExp(name+'$')}).click()
  await p.waitForFunction(s=>document.documentElement.dataset.colorScheme===s,scheme)
  await p.waitForTimeout(500)
  const sample=await p.evaluate(()=>{
    const el=document.createElement('div');el.className='bg-background text-foreground';document.body.append(el)
    const style=getComputedStyle(el)
    const canvas=document.createElement('canvas'),ctx=canvas.getContext('2d')
    const rgb=color=>{ctx.clearRect(0,0,1,1);ctx.fillStyle=color;ctx.fillRect(0,0,1,1);return [...ctx.getImageData(0,0,1,1).data].slice(0,3)}
    const background=rgb(style.backgroundColor),foreground=rgb(style.color)
    const lum=values=>values.map(x=>{x/=255;return x<=0.04045?x/12.92:((x+0.055)/1.055)**2.4}).reduce((sum,x,i)=>sum+x*[0.2126,0.7152,0.0722][i],0)
    const ratio=(a,b)=>(Math.max(a,b)+0.05)/(Math.min(a,b)+0.05)
    const a=lum(background),b=lum(foreground)
    el.style.backgroundColor='var(--brand)';el.style.color='var(--brand-foreground)'
    const brandContrast=ratio(lum(rgb(getComputedStyle(el).backgroundColor)),lum(rgb(getComputedStyle(el).color)))
    el.style.color='var(--muted-foreground)'
    const mutedContrast=ratio(a,lum(rgb(getComputedStyle(el).color)));el.remove()
    return {background,foreground,brandContrast,mutedContrast,contrast:(Math.max(a,b)+0.05)/(Math.min(a,b)+0.05),noHorizontalOverflow:document.documentElement.scrollWidth<=innerWidth}
  })
  assert(sample.contrast>=4.5);assert(sample.brandContrast>=3);assert(sample.mutedContrast>=4.5);assert(sample.noHorizontalOverflow)
  themes.push({mode,scheme,...sample})
  await p.screenshot({path:out+'/theme-'+mode+'-'+scheme+'.png'})
 }
}
await p.emulateMedia({colorScheme:'light'});await p.waitForTimeout(200)
assert(await p.evaluate(()=>document.documentElement.classList.contains('dark')))
await p.reload();await p.getByRole('radio',{name:'深色',exact:true}).waitFor()
assert.equal(await p.evaluate(()=>localStorage.getItem('theme')),'dark')
assert.equal(await p.evaluate(()=>localStorage.getItem('color-scheme')),'amber')
assert(await p.evaluate(()=>document.documentElement.classList.contains('dark')))
await p.getByRole('radio',{name:'跟随系统',exact:true}).click()
await p.waitForFunction(()=>document.documentElement.classList.contains('light'))
await p.emulateMedia({colorScheme:'dark'});await p.waitForFunction(()=>document.documentElement.classList.contains('dark'))
await p.getByRole('radio',{name:'暖白',exact:true}).click()
await p.waitForFunction(()=>document.documentElement.classList.contains('warm'))
await p.waitForTimeout(500)
await p.screenshot({path:out+'/theme-warm-amber.png'})
await p.setViewportSize({width:320,height:844});await p.waitForTimeout(300)
assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
await p.screenshot({path:out+'/theme-warm-320.png'})
await p.reload();await p.getByRole('radio',{name:'暖白',exact:true}).waitFor()
assert.equal(await p.evaluate(()=>localStorage.getItem('theme')),'warm')
await p.emulateMedia({colorScheme:'light'});await p.emulateMedia({colorScheme:'dark'});await p.waitForTimeout(200)
assert(await p.evaluate(()=>document.documentElement.classList.contains('warm')))
assert.equal(errors.length,0,JSON.stringify(errors))
fs.writeFileSync(out+'/browser-proof.json',JSON.stringify({environment:'running Vite + Go server and PG; synthetic bot/session/HTTP and WS run fixtures; authenticated real users/me read',url:'http://127.0.0.1:18084',realModelCalls:0,deviceCalls:0,productionWrites:0,writes,errors,themes,defaultSystem:true,liveSystemChanges:true,explicitPreferenceSurvivesReload:true,nativeWarmModeSurvivesReloadAndOsChange:true,doubleClickOneEnqueue:true,emptyEnterNoAbort:true,shiftEnterNewline:true,imeEnterNoSend:true,failedRetrySameInvocation:true,queuedItemExplicitPromotion:true,busyIdleSwitch:true,busyAttachmentRetained:true,narrowWidth:390},null,2))
console.log('PASS browser interactions and all 10 theme variants')
await b.close()
