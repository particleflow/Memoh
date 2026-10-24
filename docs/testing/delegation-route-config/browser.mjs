import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from '/opt/codex/runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright/index.mjs'
const root=process.env.DELEGATION_TEST_ROOT
const fixture=process.env.DELEGATION_EXISTING_FIXTURE
const state=JSON.parse(fs.readFileSync(`${root}/probe-private.json`, 'utf8'))
const browser=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox']})
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','en')},fs.readFileSync(`${fixture}/private-token`,'utf8').trim())
 const page=await context.newPage();page.setDefaultTimeout(15000)
 const errors=[];const responses=[]
 page.on('pageerror',e=>errors.push(e.message))
 page.on('response',r=>{if(new URL(r.url()).pathname.includes('/delegations')) responses.push({status:r.status(),method:r.request().method()})})
 await page.goto('http://127.0.0.1:18102/bot/web-management-supervisor')
 await page.getByText('Delegation route-config normal',{exact:true}).first().click()
 await page.getByRole('button',{name:'Tasks',exact:true}).click()
 await page.getByRole('tab',{name:'Specialist Bot handoffs',exact:true}).click()
 await page.getByRole('button').filter({hasText:state.cases[0].id.slice(0,8)}).click()
 const dialog=page.getByRole('dialog')
 await dialog.getByText(/Execution failed/).first().waitFor()
 await page.screenshot({path:`${root}/browser-failed.png`})
 const content=await dialog.innerText()
 assert(content.includes('Execution failed'));assert(!content.includes('Execution completed'))
 assert.equal(errors.length,0)
 fs.writeFileSync(`${root}/browser-proof.json`,JSON.stringify({page_errors:errors,execution_failed_visible:true,no_completed_label:true,delegation_reads:responses},null,2))
 console.log('PASS real Web/server/PG failed delegation detail; accepted delivery is not shown as completed execution')
} finally {await browser.close()}
