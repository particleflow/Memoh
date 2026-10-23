"""Actual compiled mic gestures/Spinner with synthetic native reply; no real ASR or microphone."""
import json,tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f
parent='22222222-2222-4222-8222-222222222222'
parent_row={'id':parent,'bot_id':f.bot_id,'type':'chat','title':'Synthetic parent'}
def api(path,request):
 if path==f'/bots/{f.bot_id}/sessions':return 200,{'items':[parent_row]}
 if path==f'/bots/{f.bot_id}/sessions/{parent}':return 200,parent_row
 if path.endswith('/settings'):return 200,{'chat_model_id':'synthetic-model'}
 if path=='/models':return 200,[{'id':'synthetic-model','name':'Synthetic model','model_id':'synthetic-model','type':'chat'}]
 if path.endswith('/messages'):return 200,{'items':[]}
 return None
f.extra_api_handler=api
bridge=r"""(()=>{
window.__voiceCalls=[];
window.begoniaNative={request:async raw=>{const q=JSON.parse(raw);window.__voiceCalls.push(q.command);
const data={handle:q.handle,state:'recording',durationMs:1200,peaks:[0.2,0.5,0.1],text:'',speechError:''};
if(q.command==='voice.start'||q.command==='voice.status')return JSON.stringify({ok:true,data});
if(q.command==='voice.transcribe')return await new Promise(resolve=>{window.__finishASR=()=>resolve(JSON.stringify({ok:true,data:{...data,state:'complete',text:'Synthetic transcription',bytes:96044,audible:true}}))});
return JSON.stringify({ok:true,data:q.command==='upload.list'?[]:undefined});}};
})()"""
try:
 with tempfile.TemporaryDirectory(prefix='voice-style-',dir=f.OUT) as profile,sync_playwright() as pw:
  f.current_dist=f.new_dist;c,p=f.start(pw,Path(profile));c.add_init_script(bridge);p.reload(wait_until='domcontentloaded');f.login(p)
  p.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
  p.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
  mic=p.get_by_role('button',name='按住录音',exact=True);mic.wait_for()
  p.evaluate("([bot,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').chatView({botId:bot,sessionId:id,viewId:'synthetic'}).transcript.loadingMessages.value=false",[f.bot_id,parent])
  mic.wait_for();p.wait_for_function("[...document.querySelectorAll('button')].some(b=>b.getAttribute('aria-label')==='按住录音'&&!b.disabled)")
  mic.scroll_into_view_if_needed();box=mic.bounding_box();p.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2);p.mouse.down()
  p.locator('.harmony-voice-overlay').wait_for();p.wait_for_function("window.__voiceCalls.includes('voice.status')")
  anchor=p.locator('[data-motion="icon"][data-action="text"]');b=anchor.bounding_box();p.mouse.move(b['x']+b['width']/2,b['y']+b['height']/2)
  p.get_by_role('button',name='松开转字',exact=True).wait_for();p.mouse.up()
  busy=p.get_by_role('button',name='正在转字',exact=True);busy.wait_for();p.wait_for_function('typeof window.__finishASR==="function"')
  assert busy.get_attribute('aria-busy')=='true'
  assert busy.locator('svg[role="status"][aria-label="Loading"]').count()==1
  assert busy.is_disabled()
  p.screenshot(path=str(f.OUT/'voice-spinner-390.png'))
  p.evaluate('window.__finishASR()');mic.wait_for(timeout=15000)
  p.wait_for_function("[...document.querySelectorAll('textarea')].some(el=>el.value.includes('Synthetic transcription'))")
  assert p.evaluate("window.__voiceCalls.filter(x=>x==='voice.transcribe').length") == 1
  box=mic.bounding_box();p.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2);p.mouse.down();p.locator('.harmony-voice-overlay').wait_for()
  p.mouse.move(5,5);p.get_by_role('button',name='松开取消',exact=True).wait_for();p.mouse.up();mic.wait_for()
  p.wait_for_function("window.__voiceCalls.includes('voice.cancel')")
  assert not f.page_errors
  report={'candidate':f.candidate_label(),'actualCompiledUI':True,'viewport':'390x844','actualPointerCaptureAndGesture':True,'sharedSpinnerBusyDisabled':True,'transcriptionOnlyAfterCompletion':True,'cancelStillCallsNative':True,'nativeBridge':'synthetic','realASR':False,'nativeDevice':False,'pageErrors':f.page_errors}
  (f.OUT/'voice-style-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));c.close()
finally:f.server.shutdown()
