import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root = '/tmp/task-management-reconnect'
const f = JSON.parse(fs.readFileSync('/tmp/task-native-content/private-fixture.json', 'utf8'))
const path = `/api/bots/${f.bot}/sessions/${f.session}/tasks/computer_operation/${f.operation}`
const browser = await chromium.launch({headless:true, executablePath:'/usr/bin/chromium', args:['--no-sandbox']})
const proof = { realServer:true, postgres:true, syntheticNativePeer:true, contentHTTP:[], detailHTTP:[], taskMutations:0, pageErrors:[] }
try {
 const context = await browser.newContext({viewport:{width:1200,height:1000}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','en')}, fs.readFileSync('/tmp/web-observation-reconnect/private-token','utf8').trim())
 const page = await context.newPage(); page.setDefaultTimeout(20000)
 let failDetail = false
 await page.route(`**${path}`,route => failDetail ? route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({message:'synthetic temporarily unavailable'})}) : route.continue())
 page.on('pageerror', e=>proof.pageErrors.push(e.message))
 page.on('request', r=>{if(new URL(r.url()).pathname.includes('/tasks/') && r.method()==='POST')proof.taskMutations++})
 page.on('response', r=>{const p=new URL(r.url()).pathname;if(p===path)proof.detailHTTP.push(r.status());if(p===path+'/content')proof.contentHTTP.push(r.status())})
 await page.goto(`http://127.0.0.1:18402/bot/${f.bot}`)
 await page.getByText('Task file observation synthetic',{exact:true}).first().click()
 await page.getByRole('button',{name:'Tasks',exact:true}).click()
 await page.getByRole('button').filter({hasText:f.operation.slice(0,8)}).click()
 const panel = page.getByRole('region',{name:'Native Agent content and follow-up',exact:true})
 await panel.getByRole('button',{name:'Open terminal sample',exact:true}).click()
 await panel.locator('pre').filter({hasText:'SYNTHETIC_READONLY_RUNNING'}).waitFor()
 assert.equal(await panel.locator('script').count(),0)
 console.log('PASS actual Server/JWT/PG task identity -> native agent.get/read -> escaped unverified terminal sample')
 failDetail = true
 await page.waitForResponse(r=>new URL(r.url()).pathname===path && r.status()===503)
 await panel.waitFor({state:'hidden'})
 const readsAtPause = proof.contentHTTP.length
 await page.waitForTimeout(11000)
 assert.equal(proof.contentHTTP.length, readsAtPause)
 console.log('PASS parent task 503 clears native content and prevents child reads while authorization is unavailable')
 await page.screenshot({path:`${root}/parent-unavailable.png`})
 failDetail = false
 await page.waitForResponse(r=>new URL(r.url()).pathname===path && r.status()===200)
 await panel.locator('pre').filter({hasText:'SYNTHETIC_READONLY_RUNNING'}).waitFor()
 assert(proof.contentHTTP.length > readsAtPause)
 assert.equal(proof.taskMutations,0)
 console.log('PASS same operation resumes already-opened sample after fresh parent response; no open click or business dispatch')
 await page.screenshot({path:`${root}/same-task-recovered.png`})
 await page.setViewportSize({width:390,height:844})
 await page.evaluate(()=>{localStorage.setItem('language','zh');document.documentElement.classList.add('dark')})
 await panel.scrollIntoViewIfNeeded()
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
 await page.screenshot({path:`${root}/same-task-narrow-dark.png`})
 await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'Tasks',exact:true}).click()
 await page.getByRole('button').filter({hasText:f.operation.slice(0,8)}).click()
 await panel.getByRole('button',{name:'Open terminal sample',exact:true}).waitFor()
 assert.equal(await panel.locator('pre').count(),0)
 console.log('PASS close/reopen requires explicit sample opening; narrow/dark rendering has no horizontal overflow')
 assert.deepEqual(proof.pageErrors,[])
 fs.writeFileSync(`${root}/browser-proof.json`,JSON.stringify(proof,null,2))
} finally { await browser.close() }
