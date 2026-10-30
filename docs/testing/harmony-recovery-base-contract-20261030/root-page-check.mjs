import {chromium} from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import fs from 'node:fs'
import assert from 'node:assert/strict'
const e=new URL('./',import.meta.url),browser=await chromium.launch({headless:true,executablePath:'/usr/bin/chromium',args:['--no-sandbox']})
try{
 const page=await browser.newPage({viewport:{width:390,height:844}}),requests=[]
 await page.route('https://alpha.example/**',async route=>{const request=route.request(),url=new URL(request.url);requests.push({path:url.pathname,authorized:!!request.headers().authorization});await route.fulfill({status:200,contentType:'application/json',body:JSON.stringify(url.pathname.endsWith('/settings')?{}:[])})})
 assert(process.argv[2], 'Pass the current loopback Vite URL');await page.goto(process.argv[2]);await page.getByRole('heading',{name:'连接服务'}).waitFor({timeout:60000});assert.equal(requests.length,0)
 assert((await page.locator('.service-hint').first().textContent()).includes('根路径'))
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
 await page.screenshot({path:new URL('root-service-form.png',e).pathname})
 await page.locator('#service-api').fill('https://alpha.example/%2Fapi');await page.getByRole('button',{name:'保存并继续'}).click();await page.getByRole('alert').getByText('无法保存。',{exact:false}).waitFor();assert.equal(requests.length,0)
 await page.locator('#service-api').fill('https://alpha.example/');await page.getByRole('button',{name:'保存并继续'}).click();await page.locator('#username').waitFor({timeout:60000})
 const value=await page.evaluate(()=>JSON.parse(localStorage.getItem('memoh:harmony:service-config:v1')));assert.equal(value.apiOrigin,'https://alpha.example');assert.equal(value.resetPending,false)
 assert(requests.every(x=>!x.authorized));await page.screenshot({path:new URL('root-service-login.png',e).pathname})
 const result={currentGeneratedWeb:true,viewportWidth:390,initialNoRequests:true,encodedPathRejected:true,configuredRootPreserved:true,noImplicitApiPrefix:true,loginReachable:true,unauthorizedInitialRequests:true,device:false,actualNAS:false,requests}
 fs.writeFileSync(new URL('root-page-result.json',e),JSON.stringify(result,null,2)+'\n');console.log(JSON.stringify(result))
}finally{await browser.close()}
