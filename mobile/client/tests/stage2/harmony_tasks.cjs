const fs=require('node:fs'),path=require('node:path'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),checkout=path.join(root,'mobile/.build/memoh-web/checkout')
const esbuild=require(path.join(checkout,'node_modules/.pnpm/esbuild@0.25.12/node_modules/esbuild'))
const vm=require('node:vm'),ts=require(path.join(checkout,'node_modules/typescript'))
const bundled=esbuild.buildSync({stdin:{contents:`export * from ${JSON.stringify(path.join(root,'mobile/memoh/harmony-tasks.ts'))}; export {setupApiClient} from ${JSON.stringify(path.join(checkout,'apps/web/src/lib/api-client.ts'))};`,resolveDir:checkout},bundle:true,platform:'node',format:'cjs',write:false,define:{'import.meta.env':'{}'},alias:{'@memohai/sdk/client':path.join(checkout,'packages/sdk/src/client.gen.ts'),'@memohai/sdk':path.join(checkout,'packages/sdk/src/index.ts')}}).outputFiles[0].text
let requests=[],result,status=200
const token='synthetic-only',storage=new Map([['token',token]]),m={exports:{}}
const fakeFetch=async req=>{requests.push(req);return new Response(JSON.stringify(result),{status,headers:{'content-type':'application/json','cache-control':'no-store'}})}
vm.runInNewContext(bundled,{module:m,exports:m.exports,require,URL,Headers,Request,Response,AbortController,TextEncoder,CustomEvent,DOMException,Date,Error,Map,Set,console,setTimeout,clearTimeout,globalThis:{fetch:fakeFetch},localStorage:{getItem:k=>storage.get(k),removeItem:k=>storage.delete(k)},window:{dispatchEvent:()=>true,location:{origin:'https://synthetic.invalid'}}})
const api=m.exports
api.setupApiClient({baseUrl:'https://synthetic.invalid/api',fetch:fakeFetch})
const bot='11111111-1111-4111-8111-111111111111',parent='22222222-2222-4222-8222-222222222222',id='33333333-3333-4333-8333-333333333333',target='44444444-4444-4444-8444-444444444444'
const row={task_type:'subagent',task_id:id,session_id:id,parent_session_id:parent,status:'unknown',summary:'Synthetic safe summary',created_at:'2026-10-21T10:00:00Z',updated_at:'2026-10-21T10:00:00Z',content_available:false,attachments_available:false,title:'HIDDEN_SENTINEL',metadata:{reasoning:'HIDDEN_SENTINEL'}}
const op={...row,task_type:'computer_operation',target_id:target,revision:'900719925474099312345',operation_kind:'herdr_pi_task',process_state:'unknown',pi_result:'unknown',business_state:'unknown',cancel_requested:true,status:'submitted'}
;(async()=>{
 const signal=new AbortController().signal
 result={items:[row,op],next_cursor:'opaque+/='}
 let page=await api.sessionTasks(bot,parent,'',signal)
 const req=requests.at(-1),url=new URL(req.url)
 assert.equal(url.pathname,`/api/bots/${bot}/sessions/${parent}/tasks`);assert.equal(url.search,'?limit=20')
 assert.equal(req.method,'GET');assert.equal(req.headers.get('authorization'),'Bearer '+token);assert.equal(req.cache,'no-store')
 assert.ok(!JSON.stringify(page).includes('HIDDEN_SENTINEL'));assert.equal(page.items[1].revision,op.revision)
 assert.equal(page.items[1].cancel_requested,true);assert.equal(api.taskStatus(page.items[1]),'已提交')
 assert.notEqual(api.taskKey(page.items[0]),api.taskKey(page.items[1]))
 result={items:[],next_cursor:'next'};page=await api.sessionTasks(bot,parent,'opaque+/=',signal);assert.equal(page.cursor,'next');assert.equal(new URL(requests.at(-1).url).searchParams.get('cursor'),'opaque+/=')
 result={items:[],next_cursor:'next'};await assert.rejects(api.sessionTasks(bot,parent,'next',signal))
 for(const bad of [{...row,parent_session_id:target},{...row,status:'completed'},{...row,content_available:'invalid'},{...row,attachments_available:true},{...row,task_type:'raw'},{...op,revision:123},{...op,cancel_requested:undefined},{...op,business_state:'completed'}]){result={items:[bad]};await assert.rejects(api.sessionTasks(bot,parent,'',signal))}
 result={...op,session_id:target};const detail=await api.sessionTaskDetail(bot,parent,op,signal);assert.equal(detail.session_id,target);assert.equal(new URL(requests.at(-1).url).pathname,`/api/bots/${bot}/sessions/${parent}/tasks/computer_operation/${id}`)
 result={...op,task_type:'subagent',status:'unknown'};await assert.rejects(api.sessionTaskDetail(bot,parent,op,signal))
 const final={text:'<img src="https://example.invalid/no-fetch" onerror="bad()">\n[link](https://example.invalid/) 中文',truncated:true,source:'pi_final_assistant',thinking:'HIDDEN_SENTINEL'}
 const done={...op,status:'process_exited',process_state:'exited',pi_result:'succeeded',content_available:true,result_summary:final,output:'HIDDEN_SENTINEL',reasoning:'HIDDEN_SENTINEL'}
 result=done;let parsed=await api.sessionTaskDetail(bot,parent,op,signal)
 assert.equal(parsed.content_available,true);assert.equal(parsed.result_summary.text,final.text);assert.equal(parsed.result_summary.truncated,true);assert.equal(parsed.business_state,'unknown');assert.ok(!JSON.stringify(parsed).includes('HIDDEN_SENTINEL'))
 for(const outcome of ['succeeded','failed','aborted','incomplete','not_started','invalid_protocol','output_limit','unknown','not_applicable','future_result',undefined]){
  result={...done,pi_result:outcome};parsed=await api.sessionTaskDetail(bot,parent,op,signal)
  assert.equal(parsed.pi_result,['future_result',undefined].includes(outcome)?'unknown':outcome)
  assert.equal(parsed.business_state,'unknown')
  if(['invalid_protocol','output_limit','unknown','not_applicable','future_result',undefined].includes(outcome))assert.equal(parsed.content_available,false)
 }
 for(const summary of [undefined,{...final,text:''},{...final,text:'😀'.repeat(513)},{...final,text:'x'.repeat(2049)},{...final,source:'raw_log'},{...final,truncated:undefined}]){
  result={...done,result_summary:summary};parsed=await api.sessionTaskDetail(bot,parent,op,signal);assert.equal(parsed.content_available,false);assert.equal(parsed.result_summary,undefined);assert.equal(parsed.status,'process_exited')
 }
 result={...done,result_summary:{...final,text:'😀'.repeat(512)}};parsed=await api.sessionTaskDetail(bot,parent,op,signal);assert.equal(new TextEncoder().encode(parsed.result_summary.text).length,2048)
 result={...done,content_available:false};assert.equal((await api.sessionTaskDetail(bot,parent,op,signal)).result_summary,undefined)
 result={...op,pi_result:undefined,content_available:undefined};parsed=await api.sessionTaskDetail(bot,parent,op,signal);assert.equal(parsed.pi_result,'unknown');assert.equal(parsed.content_available,false)
 result={items:[{...row,content_available:true,result_summary:final}]};parsed=await api.sessionTasks(bot,parent,'',signal);assert.equal(parsed.items[0].content_available,false)
 for(const code of [400,403,404,500]){status=code;result={code:'synthetic.problem'};await assert.rejects(api.sessionTasks(bot,parent,'',signal),e=>e.status===code)}
 status=200;result={items:[row]};const abort=new AbortController();abort.abort();await assert.rejects(api.sessionTasks(bot,parent,'',abort.signal))
 assert.ok(api.taskError(new api.TaskReadError(404)).includes('尚未部署'))
 const sdk=fs.readFileSync(path.join(checkout,'packages/sdk/src/types.gen.ts'),'utf8')
 const schema=JSON.parse(fs.readFileSync(path.join(checkout,'spec/swagger.json'),'utf8'))
 assert.deepEqual(schema.definitions['sessiontask.Task'].properties.task_type.enum,['subagent','computer_operation'])
 assert.deepEqual(Object.keys(schema.paths['/bots/{bot_id}/sessions/{session_id}/tasks']),['get'])
 assert.ok(sdk.includes('export type SessiontaskTask ='))
 assert.ok(fs.readFileSync(path.join(root,'mobile/memoh/harmony-tasks-sdk.ts'),'utf8').includes("from '@memohai/sdk'"))
 // Current picker remains local; task scope does not send a guessed target override.
 const pane=fs.readFileSync(path.join(checkout,'apps/web/src/pages/home/components/chat-pane.vue'),'utf8')
 assert.ok(pane.includes('harmonyTaskScopePending'));assert.ok(!requests.some(r=>new URL(r.url).searchParams.has('workspace_target_id')))
 status=401;result={code:'http.unauthorized'};await assert.rejects(api.sessionTasks(bot,parent,'',signal),e=>e.status===401);assert.equal(storage.has('token'),false)
 console.log(JSON.stringify({actualSdkClient:true,actualAuthWrapper:true,deployedGeneratedSdk:true,exactPaths:true,noStore:true,opaqueAndEmptyCursorPages:true,discriminatorIdentity:true,descendantOwnerPreserved:true,revisionStringExact:true,cancelNotStopped:true,malformedProjectionRejected:8,errors:[400,401,403,404,500],current401ClearsAuth:true,abort:true,noRawFallback:true,finalSummaryWhitelist:true,utf8ByteLimit2048:true,legacyFieldsAbsent:true,unknownEnumNormalizes:true,piSuccessNotBusinessCompletion:true,productionNetwork:false}))
})().catch(e=>{console.error(e);process.exitCode=1})
