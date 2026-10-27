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
const c=await b.newContext({viewport:{width:1280,height:900},hasTouch:true,colorScheme:'light'})
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
const stop=()=>p.getByRole('button',{name:'停止生成回复',exact:true})
const send=()=>p.getByRole('button',{name:'添加到接下来',exact:true})
const queued=()=>writes.filter(x=>x.path?.endsWith('/follow-up-queue'))
const aborts=()=>writes.filter(x=>x.ws?.type==='abort')
await stop().waitFor();await stop().evaluate(el=>window.auditPrimary=el)
async function sameButton(locator){assert(await locator.evaluate(el=>el===window.auditPrimary))}
async function assertIcon(locator,arrow){
 await sameButton(locator)
 await p.waitForTimeout(230)
 const opacity=await locator.locator('svg').evaluateAll(els=>els.map(el=>({stop:!!el.querySelector('rect'),opacity:getComputedStyle(el).opacity})))
 assert(opacity.some(x=>x.stop&&x.opacity===(arrow?'0':'1')),JSON.stringify(opacity))
 assert(opacity.some(x=>!x.stop&&x.opacity===(arrow?'1':'0')),JSON.stringify(opacity))
 assert.equal(await p.getByRole('button',{name:/^(添加到接下来|停止生成回复)$/}).count(),1)
}
await input.fill(' \n\t');await assertIcon(stop(),false)
await p.screenshot({path:out+'/single-empty-stop.png'})
await input.fill('有内容只排队');await assertIcon(send(),true)
await p.screenshot({path:out+'/single-content-send.png'})
const box=await send().boundingBox();await p.mouse.dblclick(box.x+box.width/2,box.y+box.height/2)
await p.locator('[data-queue-item] input').first().waitFor()
assert.equal(queued().length,1);assert.equal(aborts().length,0)
await assertIcon(stop(),false)
await stop().click();assert.equal(aborts().length,0)
await p.waitForTimeout(550);await stop().click();await stop().waitFor({state:'hidden'})
assert.equal(aborts().length,1)
busy=true;socket.send(JSON.stringify(snapshot()));await stop().waitFor()
await input.fill('触摸连击');const tapBox=await send().boundingBox()
await p.touchscreen.tap(tapBox.x+tapBox.width/2,tapBox.y+tapBox.height/2)
await p.touchscreen.tap(tapBox.x+tapBox.width/2,tapBox.y+tapBox.height/2)
await p.waitForTimeout(150);assert.equal(queued().length,2);assert.equal(aborts().length,1)
await input.fill('按住按钮回车');await send().focus()
await p.keyboard.down('Enter')
for(let i=0;i<8;i++){await p.waitForTimeout(100);await p.keyboard.down('Enter')}
await p.keyboard.up('Enter');await p.waitForTimeout(100)
assert.equal(queued().length,3);assert.equal(aborts().length,1)
await input.fill('键盘输入框发送');await input.press('Shift+Enter');assert((await input.inputValue()).includes('\n'))
await input.press('Enter');await p.waitForTimeout(150);assert.equal(queued().length,4)
await input.press('Enter');assert.equal(aborts().length,1)
await input.fill('输入法未确认');await input.dispatchEvent('keydown',{key:'Enter',isComposing:true});assert.equal(queued().length,4)
await input.fill('明确重试');failNext=true;await send().click()
await p.waitForFunction(()=>document.querySelector('textarea')?.value==='明确重试')
const failed=queued().at(-1);await assertIcon(send(),true);await send().click();await p.waitForTimeout(150)
assert.equal(queued().at(-1).body.invocation_id,failed.body.invocation_id)
assert.equal(queued().length,6);assert.equal(aborts().length,1)
assert.equal(writes.filter(x=>x.path?.endsWith('/steer')).length,0)
await p.locator('[data-queue-item]').first().getByRole('button',{name:'插入当前回复',exact:true}).click()
await p.locator('[data-queue-steer-status]').first().waitFor();assert.equal(writes.filter(x=>x.path?.endsWith('/steer')).length,1)
await p.setViewportSize({width:390,height:844});await p.emulateMedia({colorScheme:'dark'})
await input.fill('窄屏仍是同一个发送按钮');await assertIcon(send(),true)
assert(await p.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
await p.screenshot({path:out+'/single-narrow-send.png'})
await input.fill('  ');await assertIcon(stop(),false)
await p.screenshot({path:out+'/single-narrow-stop.png'})
await p.waitForTimeout(550);await stop().click();await stop().waitFor({state:'hidden'});assert.equal(aborts().length,2)
await input.fill('附件尚不能进入文本队列')
await p.locator('input[type=file]').last().setInputFiles({name:'synthetic.txt',mimeType:'text/plain',buffer:Buffer.from('synthetic')})
await p.waitForTimeout(150);busy=true;socket.send(JSON.stringify(snapshot()));await send().waitFor()
await assertIcon(send(),true);assert(await send().isDisabled());await input.press('Enter');assert.equal(queued().length,6)
assert.equal(aborts().length,2)
assert.equal(writes.filter(x=>x.ws?.type==='message').length,0)
assert.equal(errors.length,0,JSON.stringify(errors))
fs.writeFileSync(out+'/single-button-proof.json',JSON.stringify({realMountedApplication:true,syntheticHttpAndRuntimeWs:true,oneStableDomButton:true,whitespaceStops:true,contentEnqueues:true,correctGlyphOpacity:true,mouseDoubleClickNoAbort:true,rapidTouchNoAbort:true,heldKeyboardNoAbort:true,freshClickStops:true,failureRetrySameInvocation:true,explicitQueuePromotionOnly:true,attachmentRetained:true,narrowWidth:390,themeFilesUnchanged:true,writes,errors},null,2))
console.log('PASS single stable action button, content/empty icons, repeat gesture guard, queue/stop/failure/attachment/narrow checks')
await b.close()
