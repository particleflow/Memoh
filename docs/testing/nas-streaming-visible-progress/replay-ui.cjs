/* eslint-disable @typescript-eslint/no-require-imports -- Standalone Chromium fixture uses CommonJS runtime resolution. */
const {chromium}=require('playwright');const fs=require('fs');const assert=require('assert');
const out=process.env.STREAM_EVIDENCE_DIR||'/workspace/streaming-evidence';const mode=process.argv[2]||'green';
(async()=>{
 const browser=await chromium.launch({executablePath:'/usr/bin/chromium',headless:true,args:['--no-sandbox']});
 const report={mode,cases:[]};
 for(const kind of ['direct','result']){
  const context=await browser.newContext({viewport:mode.includes('narrow')?{width:390,height:844}:{width:1280,height:900}});const page=await context.newPage();const errors=[];
  page.on('pageerror',e=>errors.push(e.message));
  const login=await page.request.post('http://127.0.0.1:28080/auth/login',{data:{username:'admin',password:'admin123'}});const {access_token}=await login.json();
  const contact=await page.request.post('http://127.0.0.1:28080/bots/11111111-1111-4111-8111-111111111111/sessions/contact',{headers:{Authorization:'Bearer '+access_token}});assert.equal(contact.status(),200);const {id:sid}=await contact.json();
  let socket;let subscribed=false;
  const fixture=JSON.parse(fs.readFileSync(out+'/'+kind+'.json','utf8'));
  const retarget=e=>JSON.parse(JSON.stringify(e).replaceAll('stream-session-'+kind,sid).replaceAll('stream-bot','11111111-1111-4111-8111-111111111111'));
  await page.addInitScript(token=>{
   localStorage.setItem('token',token);window.__timing={wire:[],dom:[]};
   document.addEventListener('DOMContentLoaded',()=>new MutationObserver(()=>{
    const text=document.body.innerText;
    const prose=[...document.querySelectorAll('[data-chat-content] .prose')].map(e=>e.innerText).join('');
    if(prose.trim()&&!window.__timing.dom.some(e=>e.key==='first-visible-content'))window.__timing.dom.push({key:'first-visible-content',t:performance.now()});
    for(const key of ['First streamed fragment','; second fragment'])if(text.includes(key)&&!window.__timing.dom.some(e=>e.key===key))window.__timing.dom.push({key,t:performance.now()});
   }).observe(document.body,{subtree:true,childList:true,characterData:true}));
  },access_token);
  if(mode.includes('narrow'))await page.addInitScript(()=>{localStorage.setItem('language','zh');localStorage.setItem('theme','dark');localStorage.setItem('vueuse-color-scheme','dark')});
  await page.routeWebSocket('**/*',ws=>{
   if(!new URL(ws.url()).pathname.endsWith('/web/ws')){ws.connectToServer();return;}
   console.log('case',kind,'socket',new URL(ws.url()).pathname);
   socket=ws;ws.onMessage(raw=>{const m=JSON.parse(raw);console.log("client",m.type,m.session_id);if(m.type==='runtime_subscribe'&&m.session_id===sid){ws.send(JSON.stringify(retarget(fixture[0])));subscribed=true;}});
  });
  await page.goto('http://127.0.0.1:28082/bot/stream-fixture');
  await page.waitForFunction(()=>document.body.innerText.includes('Streaming fixture'));
  for(let i=0;i<100&&!subscribed;i++)await page.waitForTimeout(100);if(!subscribed){console.log('body',await page.locator('body').innerText(),errors);await page.screenshot({path:out+'/missing-sub.png'});}assert(subscribed,'runtime subscription missing');
  const send=async i=>{await page.evaluate(seq=>{window.__timing.wire.push({t:performance.now(),seq,measurement:'before-frame-release'})},fixture[i].seq);socket.send(JSON.stringify(retarget(fixture[i])));};
  await send(1);await page.waitForTimeout(150);
  const statusVisible=/Compacting context|压缩/.test(await page.locator('body').innerText());
  await page.screenshot({path:out+'/'+mode+'-'+kind+'-status.png'});
  await send(2);await page.waitForTimeout(150);
  let thinking=page.getByText(/Thinking|思考/,{exact:false});const reasoningVisible=await thinking.count()>0;
  if(reasoningVisible)await thinking.first().click();await page.waitForTimeout(100);
  const reasoningBodyVisible=(await page.locator('body').innerText()).includes('Provider reasoning summary');
  await page.screenshot({path:out+'/'+mode+'-'+kind+'-reasoning.png'});
  await send(3);await page.waitForFunction(()=>document.body.innerText.includes('First streamed fragment'));
  assert(!(await page.locator('body').innerText()).includes('; second fragment'));
  await page.screenshot({path:out+'/'+mode+'-'+kind+'-first.png'});
  await page.waitForTimeout(1000);await send(4);await page.waitForFunction(()=>document.body.innerText.includes('; second fragment'));
  assert(!(await page.locator('body').innerText()).includes('MACHINE_CONTEXT_MUST_NOT_RENDER'));
  await page.waitForTimeout(1000);await send(5);await send(6);await send(7);await page.waitForTimeout(150);
  await page.screenshot({path:out+'/'+mode+'-'+kind+'-final.png'});
  const timing=await page.evaluate(()=>window.__timing);const first=timing.dom.find(e=>e.key==='first-visible-content');const wire=timing.wire.find(e=>e.seq===fixture[3].seq);
  console.log('timing',kind,JSON.stringify(timing));
  report.cases.push({kind,url:page.url(),statusVisible,reasoningVisible,reasoningBodyVisible,firstDeltaReleaseToDOMms:first&&wire?first.t-wire.t:null,timing,pageErrors:errors});
  if(mode.startsWith('green')){assert(statusVisible);assert(reasoningBodyVisible)}else{assert(!statusVisible);assert(!reasoningBodyVisible)}
  assert.equal(errors.length,0);await context.close();
 }
 fs.writeFileSync(out+'/'+mode+'-ui-timing.json',JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));await browser.close();
})().catch(e=>{console.error(e);process.exit(1)});
