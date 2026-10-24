import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'
const root=process.env.TASK_DELIVERY_EVIDENCE_DIR
if (!root) throw new Error('TASK_DELIVERY_EVIDENCE_DIR is required')
const fixture=JSON.parse(fs.readFileSync(`${root}/private-fixture.json`,'utf8'))
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']})
try {
 const context=await browser.newContext({viewport:{width:1200,height:1000}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','zh');localStorage.setItem('theme','dark')},fs.readFileSync(process.env.TASK_DELIVERY_TOKEN_FILE,'utf8').trim())
 const page=await context.newPage();page.setDefaultTimeout(20000)
 const errors=[];page.on('pageerror',e=>errors.push(e.message))
 await page.goto(`${process.env.TASK_DELIVERY_WEB_URL}/bot/${fixture.bot}`)
 await page.getByText('Task file observation synthetic',{exact:true}).first().click()
 await page.getByRole('button',{name:'任务',exact:true}).click()
 await page.getByRole('button').filter({hasText:fixture.operation.slice(0,8)}).click()
 await page.getByText('result-17',{exact:true}).waitFor()
 await page.setViewportSize({width:390,height:844})
 // The host's theme controls remain unchanged. Verify the shared body in dark.
 await page.waitForFunction(()=>document.documentElement.classList.contains('dark'))
 await page.waitForTimeout(700)
 await page.screenshot({path:`${root}/browser-narrow-zh.png`})
 assert.equal(errors.length,0)
 const proof=await page.getByRole('dialog').evaluate(el=>({width:el.getBoundingClientRect().width, viewport:innerWidth, scrollWidth:el.scrollWidth, clientWidth:el.clientWidth, dark:document.documentElement.classList.contains('dark'), zh:el.textContent.includes('任务文件')}))
 assert(proof.width<=proof.viewport);assert(proof.scrollWidth<=proof.clientWidth);assert(proof.zh && proof.dark)
 fs.writeFileSync(`${root}/browser-narrow-proof.json`,JSON.stringify({...proof,pageErrors:errors},null,2)+'\n')
 console.log('PASS actual 390px Chinese dark task file view; no horizontal overflow or page errors')
} finally {await browser.close()}
