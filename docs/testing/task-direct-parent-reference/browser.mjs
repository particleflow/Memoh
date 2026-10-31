import fs from 'node:fs'
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const root=process.env.TASK_RELATION_OUTPUT
assert(root && process.env.TASK_RELATION_FIXTURE && process.env.TASK_RELATION_TOKEN_FILE && process.env.TASK_RELATION_WEB_URL, 'Supply isolated test paths and Web URL')
fs.mkdirSync(root,{recursive:true})
const f=JSON.parse(fs.readFileSync(process.env.TASK_RELATION_FIXTURE,'utf8'))
const proof={realWeb:true,realServerAndPostgresTaskResponses:true,syntheticSessions:true,taskPaths:[],mutations:0,deviceCalls:0,pageErrors:[]}
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH || '/usr/bin/chromium',args:['--no-sandbox']})
try {
 const context=await browser.newContext({viewport:{width:1200,height:900}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);if(!localStorage.getItem('language'))localStorage.setItem('language','en');if(!localStorage.getItem('theme'))localStorage.setItem('theme','light')},fs.readFileSync(process.env.TASK_RELATION_TOKEN_FILE,'utf8').trim())
 const page=await context.newPage();page.setDefaultTimeout(20000)
 page.on('pageerror',error=>proof.pageErrors.push(error.message))
 await page.route('**/api/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname
  const reply=(data,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(data)})
  if(path.includes('/tasks')){assert.equal(req.method(),'GET');proof.taskPaths.push(path);return route.continue()}
  if(path.endsWith('/workspace-targets'))return reply({items:[]})
  if(/\/oauth\/|\/mcp\//.test(path)){proof.deviceCalls++;return reply({},503)}
  if(!['GET','HEAD'].includes(req.method())){proof.mutations++;return reply({},405)}
  return route.continue()
 })
 await page.goto(`${process.env.TASK_RELATION_WEB_URL}/bot/${f.bot}`)
 await page.getByText(f.title,{exact:true}).first().click()
 await page.getByRole('button',{name:'Tasks',exact:true}).click()
 const child=page.getByRole('button').filter({has:page.locator('[data-slot=item-title]').filter({hasText:f.child.slice(0,8)})})
 const grand=page.getByRole('button').filter({has:page.locator('[data-slot=item-title]').filter({hasText:f.grandchild.slice(0,8)})})
 await grand.waitFor()
 assert((await child.innerText()).includes(`Session’s direct parent: ${f.root}`))
 assert((await grand.innerText()).includes(`Session’s direct parent: ${f.child}`))
 assert((await grand.innerText()).includes(`Owning session: ${f.grandchild}`))
 await page.screenshot({path:`${root}/list.png`})
 await grand.click()
 const relationship=page.locator('dl[aria-label="Session relationship"]')
 await relationship.waitFor()
 assert.deepEqual(await relationship.locator('dd').allTextContents(),[f.grandchild,f.child,f.root])
 assert((await page.getByRole('dialog').innerText()).includes('Execution status unknown'))
 await page.screenshot({path:`${root}/detail.png`})
 console.log('PASS actual Web + Server + PG: three-level owning/direct-parent/root references, unchanged unknown task state, no fabricated tree or completion')
 await page.setViewportSize({width:390,height:844})
 await page.evaluate(()=>{localStorage.setItem('language','zh');localStorage.setItem('theme','dark')})
 await page.reload()
 await page.getByRole('button',{name:'任务',exact:true}).click()
 await page.getByRole('button').filter({has:page.locator('[data-slot=item-title]').filter({hasText:f.grandchild.slice(0,8)})}).click()
 const translated=page.locator('dl')
 await translated.waitFor()
 assert.deepEqual(await translated.locator('dd').allTextContents(),[f.grandchild,f.child,f.root])
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
 assert.equal(await page.evaluate(()=>document.documentElement.classList.contains('dark')),true)
 await page.screenshot({path:`${root}/narrow-zh.png`})
 assert.equal(proof.mutations,0);assert.equal(proof.deviceCalls,0);assert.deepEqual(proof.pageErrors,[])
 assert(proof.taskPaths.includes(`/api/bots/${f.bot}/sessions/${f.root}/tasks/subagent/${f.grandchild}`))
 proof.passed=true
 console.log('PASS 390px Chinese dark mode: complete identifiers readable without overflow; all task responses from real server, zero model/device/mutation requests')
} finally {fs.writeFileSync(`${root}/browser-proof.json`,JSON.stringify(proof,null,2)+'\n');await browser.close()}
