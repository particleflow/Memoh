"""Actual generated client UI with synthetic native/API boundaries; not phone acceptance."""
import json, os, re, tempfile
from pathlib import Path
from urllib.parse import urlsplit
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as fixture

parent='22222222-2222-4222-8222-222222222222'
parent_row={'id':parent,'bot_id':fixture.bot_id,'type':'chat','title':'Synthetic parent'}
def api(path,request):
 if path==f'/bots/{fixture.bot_id}/sessions':return 200,{'items':[parent_row]}
 if path==f'/bots/{fixture.bot_id}/sessions/{parent}':return 200,parent_row
 if path.endswith('/settings'):return 200,{'chat_model_id':'synthetic-model'}
 if path=='/models':return 200,[{'id':'synthetic-model','name':'Synthetic model','model_id':'synthetic-model','type':'chat'}]
 if path.endswith('/messages'):return 200,{'items':[]}
 return None
fixture.extra_api_handler=api
bridge = r"""(() => {
window.__uploadCalls=[]; window.__staged=[];window.__localIntake=null;
const wav=new Uint8Array(44+38400),v=new DataView(wav.buffer),tag=(at,s)=>{for(let i=0;i<s.length;i++)wav[at+i]=s.charCodeAt(i)};
tag(0,'RIFF');v.setUint32(4,wav.length-8,true);tag(8,'WAVE');tag(12,'fmt ');v.setUint32(16,16,true);v.setUint16(20,1,true);v.setUint16(22,1,true);v.setUint32(24,16000,true);v.setUint32(28,32000,true);v.setUint16(32,2,true);v.setUint16(34,16,true);tag(36,'data');v.setUint32(40,wav.length-44,true);
window.begoniaNative={request:async raw=>{const q=JSON.parse(raw);
 window.__uploadCalls.push({command:q.command,scope:q.scope,bot:q.botId,photos:q.photos,handle:q.handle,name:q.name,size:q.size});
 if(q.command==='upload.select') {
  const result={handle:'a'.repeat(32),hash:'b'.repeat(64),size:17825915,name:'synthetic-chunked.bin',type:'application/octet-stream',resumable:true,scope:q.scope};
  window.__staged=[result];return JSON.stringify({ok:true,data:[result]});
 }
 if(q.command==='upload.list')return JSON.stringify({ok:true,data:q.handle?window.__staged.filter(f=>f.handle===q.handle):window.__staged});
 if(q.command==='upload.discard'){window.__staged=[];return JSON.stringify({ok:true});}
 if(q.command==='upload.stage.begin'){window.__localIntake={...q,handle:crypto.randomUUID().replaceAll('-',''),bytes:[]};return JSON.stringify({ok:true,data:{handle:window.__localIntake.handle}})}
 if(q.command==='upload.stage.append'){const d=window.__localIntake,b=Uint8Array.from(atob(q.data),c=>c.charCodeAt(0));if(q.offset!==d.bytes.length||b.length>65536)throw Error('bad local chunk');d.bytes.push(...b);return JSON.stringify({ok:true,data:{offset:d.bytes.length}})}
 if(q.command==='upload.stage.finish'){const d=window.__localIntake,hash=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',new Uint8Array(d.bytes)))).map(b=>b.toString(16).padStart(2,'0')).join('');return JSON.stringify({ok:true,data:{handle:d.handle,hash,size:d.size,name:d.name,type:d.type,resumable:true,scope:d.scope}})}
 if(q.command==='upload.run')return JSON.stringify({ok:false,error:'UPLOAD_CREATE_UNCERTAIN'});
 const capture={handle:q.handle,state:'recording',durationMs:1200,peaks:[0.2,0.5],text:'',speechError:''};
 if(q.command==='voice.start'||q.command==='voice.status')return JSON.stringify({ok:true,data:capture});
 if(q.command==='voice.stop')return JSON.stringify({ok:true,data:{...capture,state:'complete',bytes:wav.length,audible:true}});
 if(q.command==='voice.read')return JSON.stringify({ok:true,data:btoa(String.fromCharCode(...wav.subarray(q.offset,q.offset+65536)))});
 return JSON.stringify({ok:true});
}};
})()"""
try:
 dev = os.environ.get('HARMONY_TEST_DEV_URL')
 if dev:
  assert urlsplit(dev).hostname in ('127.0.0.1', 'localhost')
  fixture.base = dev.rstrip('/')
 out = Path(os.environ.get('HARMONY_TEST_EVIDENCE', str(fixture.OUT)))
 out.mkdir(parents=True, exist_ok=True)
 with tempfile.TemporaryDirectory(prefix='resume-ui-',dir=out) as profile,sync_playwright() as pw:
  fixture.current_dist=fixture.new_dist
  fixture.auto_configure=False
  context,page=fixture.start(pw,Path(profile),expected_entry='/@vite/client' if dev else None)
  socket_messages=[]
  context.route_web_socket('**/*', lambda socket: socket.on_message(lambda message: socket_messages.append(message)))
  context.add_init_script("if(location.protocol !== 'about:') localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
  context.add_init_script(bridge);page.reload(wait_until='domcontentloaded')
  fixture.login(page)
  restore = page.get_by_role('button',name='恢复未发送草稿附件',exact=True)
  restore.wait_for(state='visible')
  # Exercise the existing shared composer menu, not an isolated component.
  page.get_by_role('button',name='添加文件或切换 Agent',exact=True).click()
  page.get_by_role('menuitem',name='上传文件或图片',exact=True).click()
  page.get_by_text('synthetic-chunked.bin',exact=False).wait_for()
  assert page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.select' && x.photos===false)")
  assert not page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.run')")
  assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth')
  page.screenshot(path=str(out/'native-selected-390.png'))
  page.get_by_role('button',name='移除',exact=True).click()
  page.get_by_text('synthetic-chunked.bin',exact=False).wait_for(state='hidden')
  # An explicit restore retains the original pre-restart draft scope.
  page.evaluate("window.__staged=[{handle:'c'.repeat(32),hash:'d'.repeat(64),size:17825915,name:'synthetic-restored.bin',type:'application/octet-stream',resumable:true,scope:'draft:previous-process-view'}]")
  restore.click()
  page.get_by_text('synthetic-restored.bin',exact=False).wait_for()
  assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth')
  page.screenshot(path=str(out/'native-restored-390.png'))
  page.get_by_role('button',name='移除',exact=True).click()
  page.get_by_text('synthetic-restored.bin',exact=False).wait_for(state='hidden')
  assert page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.discard'&&x.scope==='draft:previous-process-view')")
  # Execute the existing paste/drop/recording entry points. The controlled
  # native boundary reports an unknown Create; no chat/model dispatch is allowed.
  restored_calls=page.evaluate('window.__uploadCalls')
  assert not any(c['command']=='upload.run' for c in restored_calls)
  file_entries=[]
  for entry in ['paste','drop','recording']:
   page.reload(wait_until='domcontentloaded')
   # A restored session displays its title, not the Bot title used by the login fixture.
   page.wait_for_function("!location.hash.includes('/login') && !!document.querySelector('#app').__vue_app__?.config.globalProperties.$pinia?._s.has('chat')")
   page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
   page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",parent)
   # The local fixture has no live runtime/history producer; supply its empty-history completion through the existing store.
   page.evaluate("([bot,id])=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('chat').chatView({botId:bot,sessionId:id,viewId:'synthetic'}).transcript.loadingMessages.value=false",[fixture.bot_id,parent])
   mic=page.get_by_role('button',name='按住录音',exact=True)
   try: mic.wait_for(timeout=8000)
   except Exception:
    page.screenshot(path=str(out/(entry+'-entry-debug.png')))
    (out/(entry+'-entry-debug.json')).write_text(json.dumps({'body':page.locator('body').inner_text()[:2400],'errors':fixture.page_errors,'api':fixture.api_events[-15:]},indent=2))
    raise
   try: page.wait_for_function("[...document.querySelectorAll('button')].some(b=>b.getAttribute('aria-label')==='按住录音'&&!b.disabled)",timeout=8000)
   except Exception:
    page.screenshot(path=str(out/(entry+'-disabled-debug.png')))
    (out/(entry+'-disabled-debug.json')).write_text(json.dumps({'body':page.locator('body').inner_text()[:2400],'errors':fixture.page_errors,'api':fixture.api_events[-25:]},indent=2))
    raise
   if entry=='recording':
    mic.scroll_into_view_if_needed();box=mic.bounding_box();page.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2);page.mouse.down()
    page.locator('.harmony-voice-overlay').wait_for();page.wait_for_function("window.__uploadCalls.some(q=>q.command==='voice.status')")
    anchor=page.locator('[data-motion="icon"][data-action="send"]');box=anchor.bounding_box()
    page.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2)
    page.get_by_role('button',name='松开发送',exact=True).wait_for()
    page.mouse.up()
   else:
    page.locator('textarea').last.evaluate("(el,kind)=>{const data=new DataTransfer();data.items.add(new File(['synthetic '+kind+' bytes'],kind+'.txt',{type:'text/plain'}));if(kind==='paste')el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:data,bubbles:true,cancelable:true}));else {const target=el.closest('div.flex-1.flex.flex-col.h-full.min-w-0.relative');for(const type of ['dragenter','dragover','drop'])target.dispatchEvent(new DragEvent(type,{dataTransfer:data,bubbles:true,cancelable:true}));}}",entry)
    page.get_by_text(entry+'.txt',exact=False).first.wait_for()
    page.locator('textarea').last.fill('Synthetic upload entry')
    page.locator('textarea').last.press('Enter')
   try: page.wait_for_function("window.__uploadCalls.some(q=>q.command==='upload.run')",timeout=12000)
   except Exception:
    page.screenshot(path=str(out/(entry+'-send-debug.png')))
    (out/(entry+'-send-debug.json')).write_text(json.dumps({'body':page.locator('body').inner_text()[:2400],'calls':page.evaluate('window.__uploadCalls'),'errors':fixture.page_errors,'socketMessages':socket_messages},indent=2))
    raise
   page.wait_for_timeout(200)
   entry_calls=page.evaluate('window.__uploadCalls')
   assert any(q['command']=='upload.stage.begin' for q in entry_calls)
   assert any(q['command']=='upload.stage.append' for q in entry_calls)
   assert any(q['command']=='upload.stage.finish' for q in entry_calls)
   assert not any(q['command']=='upload.consume' for q in entry_calls)
   file_entries.append({'entry':entry,'staged':True,'unknownNotConsumed':True})
   page.screenshot(path=str(out/(entry+'-native-intake-390.png')))
  fixture.logout(page)
  assert page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.close')")
  (out/'page-errors.json').write_text(json.dumps(fixture.page_errors,indent=2)+'\n')
  assert not fixture.page_errors, fixture.page_errors
  calls=page.evaluate('window.__uploadCalls')
  assert not any(c['command']=='upload.consume' for c in calls)
  # Vite heartbeats and the existing read-only runtime subscription are allowed;
  # no chat, continuation or task dispatch frame may escape the unknown upload.
  frames=[json.loads(m) for m in socket_messages if isinstance(m,str) and m.lstrip().startswith('{')]
  assert all(set(m)=={'type','session_id'} and m['session_id']==parent and m['type'] in ('runtime_subscribe','runtime_unsubscribe') for m in frames), frames
  report={'scope':__doc__,'candidate':fixture.candidate_label(),'environment':'generated Vite client' if dev else 'compiled client',
    'actualComposerFileMenuUsesNative':True,'restoreAndRemove':True,'restorePreservesOriginalDraftScope':True,
    'restoreDidNotUploadOrDispatch':True,'actualFileEntries':file_entries,'unknownNeverConsumed':True,'unknownNeverDispatched':True,'logoutClosesNative':True,'viewport':'390x844','pageErrors':fixture.page_errors,'nativeDevice':False}
  (out/'resumable-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));context.close()
finally: fixture.server.shutdown()
