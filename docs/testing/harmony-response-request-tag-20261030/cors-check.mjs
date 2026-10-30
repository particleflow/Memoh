import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import http from 'node:http'
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { createRequire } from 'node:module'
const require=createRequire(import.meta.url)
const ts=require(new URL('../../../mobile/client/mobile/.build/memoh-web/checkout/node_modules/typescript',import.meta.url).pathname)
const source=fs.readFileSync(new URL('../../../mobile/client/mobile/memoh/harmony-diagnostics.ts',import.meta.url),'utf8')
const begin=source.indexOf('export function responseRequestTag('),end=source.indexOf('\ninterface Batch',begin)
assert(begin>=0&&end>begin)
const helperSource=source.slice(begin,end),helperJS=ts.transpileModule(helperSource,{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ESNext}}).outputText.replace('export function','function')
const tag='0123456789abcdef0123456789abcdef',body=Buffer.from('payload'),sha=b=>createHash('sha256').update(b).digest('hex')
let pageOrigin,apiOrigin,requests=0,preflights=0
const pageServer=http.createServer((req,res)=>{if(req.url==='/same'){res.setHeader('X-NAS-Request-ID',tag);res.end(body)}else{res.setHeader('content-type','text/html');res.end('<!doctype html><title>Controlled request-tag CORS fixture</title><body>Controlled local test</body>')}})
const apiServer=http.createServer((req,res)=>{
 res.setHeader('Access-Control-Allow-Origin',pageOrigin);res.setHeader('Access-Control-Allow-Credentials','true')
 if(req.method==='OPTIONS'){preflights++;res.setHeader('Access-Control-Allow-Methods','GET');res.setHeader('Access-Control-Allow-Headers','Authorization');res.writeHead(204);res.end();return}
 requests++;assert.equal(req.headers.authorization,'Bearer synthetic-cors-only')
 res.setHeader('X-NAS-Request-ID',req.url==='/duplicate'?[tag,tag]:req.url==='/invalid'?'A'.repeat(32):tag)
 res.setHeader('Access-Control-Expose-Headers',req.url==='/hidden'?'X-Request-ID':'X-NAS-Request-ID')
 res.end(body)
})
const listen=server=>new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
let browser
try{
 await listen(pageServer);await listen(apiServer);pageOrigin='http://127.0.0.1:'+pageServer.address().port;apiOrigin='http://127.0.0.1:'+apiServer.address().port
 browser=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox']})
 const page=await browser.newPage();await page.goto(pageOrigin);await page.addScriptTag({content:helperJS})
 const cases=[]
 for(const [name,target,expected]of[['same-origin',pageOrigin+'/same',true],['cross-hidden',apiOrigin+'/hidden',false],['cross-exposed',apiOrigin+'/exposed',true],['cross-duplicate',apiOrigin+'/duplicate',false],['cross-invalid',apiOrigin+'/invalid',false]]){
  const result=await page.evaluate(async url=>{const response=await fetch(url,{credentials:'include',headers:{Authorization:'Bearer synthetic-cors-only'}}),tag=responseRequestTag(response),bytes=new Uint8Array(await response.arrayBuffer());return{tagPresent:tag!==undefined,body:[...bytes]}},target)
  assert.equal(result.tagPresent,expected,name);assert.deepEqual(Buffer.from(result.body),body,name)
  cases.push({name,requestTagPresent:result.tagPresent,bodyBytes:result.body.length,bodySHA256:sha(Buffer.from(result.body))})
 }
 const result={browser:'controlled desktop Chromium, not ArkWeb/phone',helperSHA256:sha(helperSource),cases,apiGETs:requests,preflights,productionCORSChanged:false,credentials:'synthetic only',ephemeralServersClosedOnExit:true,actualNASOrSamplingEnabled:false}
 fs.writeFileSync(new URL('./cors-result.json',import.meta.url),JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result))
}finally{if(browser)await browser.close();await Promise.all([pageServer,apiServer].map(server=>new Promise(resolve=>server.close(resolve))))}
