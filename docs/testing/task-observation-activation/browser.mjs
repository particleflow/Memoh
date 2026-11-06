import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'
const root=process.env.EVIDENCE_DIR
assert(root && process.env.FIXTURE_PATH && process.env.TOKEN_PATH && process.env.WEB_URL, 'Set isolated fixture paths and WEB_URL')
fs.mkdirSync(root,{recursive:true})
const f=JSON.parse(fs.readFileSync(process.env.FIXTURE_PATH,'utf8'))
const response={operation_id:f.operation,state:'unknown',business_outcome:'unknown',native:{availability:'online',agent_status:'working',reference_match:'unknown'}}
const proof={realWeb:true,realServerPersistedTaskReads:true,nativeObservationTransport:'synthetic browser interception; no OAuth or MCP transport invoked',taskResponses:[],observationIDs:[],contentCalls:0,mutations:0,deviceCalls:0,pageErrors:[]}
let mode='online'
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']})
try {
 const context=await browser.newContext({viewport:{width:1200,height:980}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','en');localStorage.setItem('theme','light')},fs.readFileSync(process.env.TOKEN_PATH,'utf8').trim())
 const page=await context.newPage();page.setDefaultTimeout(20000)
 page.on('pageerror',e=>proof.pageErrors.push(e.message))
 page.on('response',r=>{const p=new URL(r.url()).pathname;if(p.includes('/tasks')&&!p.endsWith('/observation')&&!p.endsWith('/content'))proof.taskResponses.push({path:p,status:r.status()})})
 await page.route('**/api/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname
  const reply=(data,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(data)})
  if(path.endsWith('/observation')){
   assert.equal(req.method(),'GET');assert(path.includes('/tasks/computer_operation/'))
   proof.observationIDs.push(path.split('/').at(-2))
   if(mode==='disconnected') return route.abort('internetdisconnected')
   return reply({...response,native:{...response.native,availability:mode}})
  }
  if(path.endsWith('/content')){proof.contentCalls++;return reply({operation_id:f.operation,supported:false})}
  if(path.endsWith('/workspace-targets'))return reply({items:[]})
  if(/\/oauth\/|\/mcp\//.test(path)){proof.deviceCalls++;return reply({},503)}
  if(!['GET','HEAD'].includes(req.method())){proof.mutations++;return reply({},405)}
  return route.continue()
 })
 await page.goto(`${process.env.WEB_URL}/bot/${f.bot}`)
 await page.getByText('Task file observation synthetic',{exact:true}).first().click()
 async function openDetail(){await page.getByRole('button',{name:'Tasks',exact:true}).click();await page.getByRole('button').filter({has:page.locator('[data-slot=item-title]').filter({hasText:f.operation.slice(0,8)})}).click()}
 await openDetail()
 const panel=page.getByRole('region',{name:'Current computer observation',exact:true})
 await panel.waitFor();await page.waitForTimeout(15000)
 assert.equal(proof.observationIDs.length,0);assert.equal(proof.contentCalls,0)
 assert(proof.taskResponses.some(r=>r.path.endsWith(`/computer_operation/${f.operation}`)&&r.status===200))
 assert(proof.taskResponses.some(r=>r.path.endsWith('/deliveries')&&r.status===200))
 await page.screenshot({path:`${root}/summary.png`})
 console.log('PASS actual persisted summary and delivery HTTP200 reads for 15s: zero native observation/content requests')
 await panel.getByRole('button',{name:'Refresh computer',exact:true}).click()
 await panel.getByText('Computer connection available',{exact:true}).waitFor()
 mode='authentication_unavailable'
 await panel.getByRole('button',{name:'Refresh computer',exact:true}).click()
 await panel.getByText('Computer connection authentication unavailable',{exact:true}).waitFor()
 await panel.getByText(/Automatic checks are paused/).waitFor()
 const stopped=proof.observationIDs.length
 await page.waitForTimeout(11000);assert.equal(proof.observationIDs.length,stopped)
 await page.screenshot({path:`${root}/auth-paused.png`})
 console.log('PASS explicit observation only; HTTP200 authentication_unavailable visible and no further native reads across two polling intervals')
 mode='online';await panel.getByRole('button',{name:'Refresh computer',exact:true}).click()
 await panel.getByText('Computer connection available',{exact:true}).waitFor()
 assert.equal(await panel.getByText(/Automatic checks are paused/).count(),0)
 mode='disconnected';await panel.getByRole('button',{name:'Refresh computer',exact:true}).click()
 await panel.getByText('Computer connection available',{exact:true}).waitFor({state:'hidden'})
 await page.waitForTimeout(1000)
 assert.equal(await panel.getByText('Computer connection available',{exact:true}).count(),0)
 assert.equal(await panel.getByText(/Automatic checks are paused/).count(),0)
 mode='online'
 await panel.getByText('Computer connection available',{exact:true}).waitFor()
 await page.screenshot({path:`${root}/reconnected.png`})
 assert(proof.observationIDs.every(id=>id===f.operation))
 await page.keyboard.press('Escape');await openDetail();await panel.waitFor()
 const closed=proof.observationIDs.length;await page.waitForTimeout(5500)
 assert.equal(proof.observationIDs.length,closed)
 assert.equal(proof.contentCalls,0);assert.equal(proof.mutations,0);assert.equal(proof.deviceCalls,0);assert.deepEqual(proof.pageErrors,[])
 proof.passed=true
 console.log('PASS explicit auth recovery and transient disconnection resume GET for the same task, no input replay; reopen summary requires fresh observation opt-in')
} finally {fs.writeFileSync(`${root}/browser-proof.json`,JSON.stringify(proof,null,2)+'\n');await browser.close()}
