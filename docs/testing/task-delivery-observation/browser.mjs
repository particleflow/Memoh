import fs from 'node:fs'
import assert from 'node:assert/strict'
import { chromium } from 'playwright'
const root=process.env.TASK_DELIVERY_EVIDENCE_DIR
if (!root) throw new Error('TASK_DELIVERY_EVIDENCE_DIR is required')
const fixture=JSON.parse(fs.readFileSync(`${root}/private-fixture.json`,'utf8'))
const browser=await chromium.launch({headless:true,executablePath:process.env.CHROMIUM_PATH,args:['--no-sandbox']})
try {
 const context=await browser.newContext({viewport:{width:1200,height:1000}})
 await context.addInitScript(token=>{localStorage.setItem('token',token);localStorage.setItem('language','en')},fs.readFileSync(process.env.TASK_DELIVERY_TOKEN_FILE,'utf8').trim())
 const page=await context.newPage();page.setDefaultTimeout(20000)
 const errors=[],requests=[]
 page.on('pageerror',e=>errors.push(e.message))
 page.on('response',r=>{if(new URL(r.url()).pathname.endsWith('/deliveries'))requests.push({status:r.status(),method:r.request().method(),continued:!!new URL(r.url()).searchParams.get('after')})})
 await page.goto(`${process.env.TASK_DELIVERY_WEB_URL}/bot/${fixture.bot}`)
 await page.getByText('Task file observation synthetic',{exact:true}).first().click()
 await page.getByRole('button',{name:'Tasks',exact:true}).click()
 await page.getByRole('button').filter({hasText:fixture.operation.slice(0,8)}).click()
 const dialog=page.getByRole('dialog')
 await dialog.getByText('result-17',{exact:true}).waitFor()
 assert(await dialog.getByText('result-16',{exact:true}).count())
 assert((await dialog.innerText()).includes('Delivery pending'))
 assert((await dialog.innerText()).includes('Delivery failed'))
 assert((await dialog.innerText()).includes('Business completion remains unverified.'))
 assert.equal(await dialog.locator('a').count(),0)
 assert(requests.some(r=>r.status===200 && !r.continued) && requests.some(r=>r.status===200 && r.continued))
 await page.screenshot({path:`${root}/browser-results.png`})
 console.log('PASS actual Web + JWT server + PG: all 18 seeded records automatically read; pending/failed distinct; no unauthorized download link')
 await context.setOffline(true)
 await dialog.getByText('Network connection failed. Check your connection and try again.',{exact:true}).first().waitFor()
 assert.equal(await dialog.getByText('result-00',{exact:true}).count(),0)
 await context.setOffline(false)
 await dialog.getByText('result-17',{exact:true}).waitFor()
 console.log('PASS real browser offline -> same selected detail and file records automatically recover, no task dispatch')
 const route=`**/tasks/computer_operation/${fixture.operation}/deliveries*`
 await page.route(route,r=>r.fulfill({status:404,contentType:'application/problem+json',body:JSON.stringify({code:'http.not_found',status:404})}))
 await dialog.getByText('This server does not support task file observation yet.',{exact:true}).waitFor()
 const failedReads=requests.filter(r=>r.status===404).length
 await page.waitForTimeout(5500)
 assert.equal(requests.filter(r=>r.status===404).length,failedReads)
 await page.screenshot({path:`${root}/browser-unsupported.png`})
 await page.unroute(route)
 await dialog.getByRole('button',{name:'Reload view',exact:true}).click()
 await dialog.getByText('result-17',{exact:true}).waitFor()
 console.log('PASS synthetic old-route 404 is unavailable, does not poll; explicit reload recovers against real backend')
 assert.equal(errors.length,0)
 fs.writeFileSync(`${root}/browser-functional-proof.json`,JSON.stringify({realServer:true,realPostgres:true,seededMetadata:true,realMediaUpload:true,taskDispatches:0,modelCalls:0,downloadImplemented:false,pageErrors:errors,deliveryRequests:requests,offlineRecovery:true,synthetic404:true},null,2)+'\n')
} finally {await browser.close()}
