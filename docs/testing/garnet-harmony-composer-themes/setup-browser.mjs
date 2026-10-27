import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
const out=process.env.GARNET_EVIDENCE_DIR;if(!out)throw new Error('GARNET_EVIDENCE_DIR is required');fs.mkdirSync(out,{recursive:true})
const b=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox']})
const results=[],errors=[]
try{
const c=await b.newContext({viewport:{width:390,height:844},colorScheme:'dark'})
const p=await c.newPage();p.setDefaultTimeout(30000);p.on('pageerror',e=>errors.push(e.message))
await p.goto('http://127.0.0.1:18084');await p.getByRole('heading',{name:'连接服务',exact:true}).waitFor()
assert.equal(await p.evaluate(()=>localStorage.getItem('theme')),'system')
await p.waitForFunction(()=>document.documentElement.classList.contains('dark'))
await p.emulateMedia({colorScheme:'light'});await p.waitForFunction(()=>document.documentElement.classList.contains('light'))
await p.emulateMedia({colorScheme:'dark'});await p.waitForFunction(()=>document.documentElement.classList.contains('dark'))
await p.screenshot({path:out+'/setup-system-dark.png'})
for(const mode of ['light','dark','warm']){
 for(const scheme of ['memoh','ocean','forest','rose','amber']){
  await p.evaluate(({mode,scheme})=>{localStorage.setItem('theme',mode);localStorage.setItem('color-scheme',scheme)},{mode,scheme})
  await p.reload();await p.getByRole('heading',{name:'连接服务',exact:true}).waitFor()
  await p.waitForFunction(mode=>document.documentElement.classList.contains(mode),mode)
  const sample=await p.locator('.service-setup').evaluate(el=>({background:getComputedStyle(el).backgroundColor,color:getComputedStyle(el).color,scheme:document.documentElement.dataset.colorScheme,width:document.documentElement.scrollWidth,noHorizontalOverflow:document.documentElement.scrollWidth<=innerWidth}))
  assert.equal(sample.scheme,scheme);assert(sample.noHorizontalOverflow)
  assert.equal(await p.evaluate(()=>localStorage.getItem('theme')),mode)
  results.push({mode,scheme,...sample})
  if(scheme==='forest')await p.screenshot({path:out+'/setup-'+mode+'-forest.png'})
 }
}
await p.emulateMedia({colorScheme:'light'});assert(await p.evaluate(()=>document.documentElement.classList.contains('warm')))
assert.equal(errors.length,0,JSON.stringify(errors))
fs.writeFileSync(out+'/phone-browser-proof.json',JSON.stringify({realGeneratedPhoneBootstrap:true,url:'http://127.0.0.1:18084',nativeBridge:false,arkWebDevice:false,hapCompiled:false,defaultSystem:true,liveSystemChange:true,explicitThemePreserved:true,allFiveSchemesPreserved:true,results,errors},null,2))
console.log('PASS generated phone setup: system/default/live + explicit light/dark/warm × 5 schemes; 390px; no JS errors')
}finally{await b.close()}
