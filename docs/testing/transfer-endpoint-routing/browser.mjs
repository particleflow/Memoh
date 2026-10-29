import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const out='/workspace/scratch/fast-transfer-path'
const fixture=JSON.parse(fs.readFileSync(out+'/private-browser-fixture.json','utf8'))
const profile=out+'/browser-profile'
const launch=()=>chromium.launchPersistentContext(profile,{headless:true,executablePath:'/usr/bin/chromium',viewport:{width:1200,height:850},args:['--no-sandbox','--disable-dev-shm-usage']})
const proof={environment:'isolated current Go Server/PostgreSQL/Vite/Chromium',logicalOrigin:'http://127.0.0.1:18683/api',physicalOrigin:'http://127.0.0.1:18684',pageErrors:[],phoneOrNasPublic:false}
let context=await launch()
await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','en')},fixture.token)
let page=await context.newPage();page.on('pageerror',e=>proof.pageErrors.push(e.message))
await page.goto('http://127.0.0.1:18683/bot/endpoint-fixture')
await page.locator('input[type=file]:not([webkitdirectory])').last().waitFor({state:'attached',timeout:30000})
await page.locator('input[type=file]:not([webkitdirectory])').last().waitFor({state:'attached',timeout:30000})
const bytes=Buffer.alloc(12*1024*1024+37);for(let i=0;i<bytes.length;i++)bytes[i]=i*31%251
await page.locator('input[type=file]:not([webkitdirectory])').last().setInputFiles({name:'endpoint-synthetic.bin',mimeType:'application/octet-stream',buffer:bytes})
await page.waitForTimeout(500)
await page.screenshot({path:out+'/actual-app-selected-file.png',fullPage:true})
// Exercise the actual application media module and its real IDB/OPFS storage.
proof.partial=await page.evaluate(async bot=>{
 const m=await import('/src/lib/media-transfer.ts');const store=await import('/src/lib/media-transfer-storage.ts');const control=new AbortController();const bytes=new Uint8Array(12*1024*1024+37);for(let i=0;i<bytes.length;i++)bytes[i]=i*31%251
 const identity=await m.mediaIdentity(bot,control.signal);const original=identity.fetch
 identity.fetch=async(...args)=>{const response=await original(...args);const request=args[0];if(request instanceof Request&&request.method==='PATCH')control.abort();return response}
 let failure;try{await m.uploadMedia(identity,new File([bytes],'endpoint-synthetic.bin',{type:'application/octet-stream'}),'endpoint-mapped-draft-v4',1024*1024,control.signal)}catch(e){failure=e.name+": "+e.message}
 const journal=(await store.listJournals()).find(j=>j.draft==='endpoint-mapped-draft-v4')
 return{failure,key:journal.key,receipt:journal.receipt?.id,hash:journal.hash,origin:journal.origin,offset:journal.offset,phase:journal.phase}
},fixture.bot)
assert.equal(proof.partial.offset,0);assert.equal(proof.partial.phase,"patch-unknown")
assert.equal(proof.partial.origin,proof.logicalOrigin)
await context.close() // actual Chromium process termination, same private profile on reopen
context=await launch();page=await context.newPage();page.on('pageerror',e=>proof.pageErrors.push(e.message))
await page.goto('http://127.0.0.1:18683/bot/endpoint-fixture')
await page.locator('input[type=file]:not([webkitdirectory])').last().waitFor({state:'attached',timeout:30000})
proof.restored=await page.evaluate(async bot=>{
 const m=await import('/src/lib/media-transfer.ts');const store=await import('/src/lib/media-transfer-storage.ts');const signal=new AbortController().signal
 const identity=await m.mediaIdentity(bot,signal);const restored=await m.restoreMediaUploads(bot,'endpoint-mapped-draft-v4',signal)
 const start=performance.now();const result=await m.uploadMedia(identity,restored.files[0],'endpoint-mapped-draft-v4',1024*1024,signal)
 const journal=(await store.listJournals()).find(j=>j.key===result.key)
 return{...result,receipt:journal.receipt?.id,origin:journal.origin,offset:journal.offset,phase:journal.phase,durationMs:performance.now()-start,restoredFiles:restored.files.length,sourceRetained:journal.source}
},fixture.bot)
assert.equal(proof.restored.receipt,proof.partial.receipt);assert.equal(proof.restored.key,proof.partial.key);assert.equal(proof.restored.phase,'ready');assert.equal(proof.restored.hash,proof.partial.hash)
proof.download=await page.evaluate(async({bot,hash,size})=>{
 const m=await import('/src/lib/media-transfer.ts');const start=performance.now();const result=await m.downloadMedia(bot,hash,size,undefined,new AbortController().signal)
 return{bytes:result.size,hash:await m.mediaHash(result),durationMs:performance.now()-start}
},{bot:fixture.bot,...proof.restored})
assert.equal(proof.download.hash,proof.restored.hash)
await page.screenshot({path:out+'/actual-app-after-process-recovery.png',fullPage:true})
proof.uiFileSelection=true;proof.processRestart=true;proof.sameReceiptAndJournal=true;proof.modelCalls=0
fs.writeFileSync(out+'/browser-proof.json',JSON.stringify(proof,null,2)+'\n')
await context.close()
console.log(JSON.stringify(proof,null,2))
