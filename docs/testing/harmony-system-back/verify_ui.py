"""Current generated phone UI; synthetic API/native bridge, not a phone gesture test."""
import json,sys,tempfile,os,base64
from pathlib import Path
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(ROOT/'mobile/client/tests/stage2'))
import harmony_auth_persistence as f
OUT=Path(os.environ.get('HARMONY_TEST_EVIDENCE',str(Path(__file__).parent)))
OUT.mkdir(parents=True,exist_ok=True)
f.base='http://127.0.0.1:4183';f.auto_configure=False
results=[]
def back(page):return page.evaluate('window.__memohBack()')
def case(name):results.append(name);print(name,flush=True)
try:
 with tempfile.TemporaryDirectory(dir=OUT) as profile,sync_playwright() as pw:
  context,page=f.start(pw,Path(profile),expected_entry='/@vite/client')
  context.add_init_script("if(location.protocol!=='about:')localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
  context.add_init_script("window.__nativeCalls=[];window.begoniaNative={request:async raw=>{const q=JSON.parse(raw);window.__nativeCalls.push(q);return JSON.stringify({ok:true,data:q.command==='upload.list'?[]:undefined})}}")
  page.reload(wait_until='domcontentloaded');page.locator('#username').wait_for()
  assert back(page)==False;assert page.locator('#username').is_visible();case('login-root-unhandled')
  f.login(page);token=f.fingerprint(page)
  assert back(page)==False;case('chat-root-unhandled')
  page.locator('textarea').first.fill('Synthetic retained draft')
  page.locator('input[type=file]').last.set_input_files({'name':'synthetic-preview.png','mimeType':'image/png','buffer':base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=')})
  preview=page.locator('[role=button]').filter(has=page.locator('img')).last
  preview.wait_for();preview.click()
  lightbox=page.locator('[data-keyboard-scope=mediaLightbox]');lightbox.wait_for()
  page.screenshot(path=str(OUT/'attachment-preview.png'))
  assert back(page);lightbox.wait_for(state='hidden');case('attachment-preview-dismissed')
  assert page.locator('textarea').first.input_value()=='Synthetic retained draft'
  assert page.locator('input[type=file]').last.count()==1

  page.locator('header button').first.click()
  sheet=page.locator('[data-slot="sheet-content"]');sheet.wait_for()
  page.screenshot(path=str(OUT/'drawer-open.png'))
  sheet.locator('[aria-haspopup=menu]').first.click()
  menu=page.locator('[role=menu]');menu.wait_for()
  assert back(page);menu.wait_for(state='hidden')
  assert sheet.is_visible();case('nested-menu-only-dismissed')
  assert back(page)==True;sheet.wait_for(state='hidden');case('drawer-dismissed-no-navigation')
  page.goto(f.base+'/#/settings',wait_until='domcontentloaded')
  page.wait_for_function("location.hash==='#/settings' && !!document.querySelector('[data-settings-content-shell]')")
  page.screenshot(path=str(OUT/'settings-list.png'))
  page.get_by_text('服务商',exact=True).click()
  page.wait_for_function("location.hash.startsWith('#/settings/providers')")
  page.screenshot(path=str(OUT/'settings-providers.png'))
  assert back(page);page.wait_for_function("location.hash==='#/settings'");case('settings-content-to-list')
  assert back(page);page.wait_for_function("!location.hash.startsWith('#/settings')");case('settings-list-to-chat')
  page.screenshot(path=str(OUT/'chat-returned.png'))
  assert page.locator('textarea').first.input_value()=='Synthetic retained draft'
  assert page.locator('[role=button]').filter(has=page.locator('img')).count()>=1
  assert not page.evaluate("window.__nativeCalls.some(q=>['upload.cancel','upload.release','download.cancel'].includes(q.command))")
  case('draft-and-pending-attachment-retained')
  page.locator('input[type=file]').last.set_input_files({'name':'synthetic-note.txt','mimeType':'text/plain','buffer':b'Synthetic attachment content'})
  page.locator('[role=button]').filter(has_text='synthetic-note').last.click()
  dialog=page.locator('[data-slot=dialog-content]');dialog.wait_for()
  page.screenshot(path=str(OUT/'text-preview-dialog.png'))
  assert back(page);dialog.wait_for(state='hidden');case('text-preview-dialog-dismissed')

  page.goto(f.base+'/#/settings/bots/new',wait_until='domcontentloaded')
  page.wait_for_function("!!document.querySelector('[data-settings-content-shell]')")
  assert back(page);page.wait_for_function("!location.hash.startsWith('#/settings/bots/new')")
  case('cold-settings-drill-back')

  session='22222222-2222-4222-8222-222222222222'
  child='33333333-3333-4333-8333-333333333333'
  row={'id':session,'bot_id':f.bot_id,'type':'chat','title':'Synthetic parent'}
  task={'task_type':'subagent','task_id':child,'session_id':child,'parent_session_id':session,'direct_parent_session_id':session,'status':'running','summary':'Synthetic child','created_at':'2026-10-26T00:00:00Z','updated_at':'2026-10-26T00:00:00Z','content_available':False,'attachments_available':False}
  def api(path,request):
   if path.endswith('/sessions'):return 200,{'items':[row]}
   if path==f'/bots/{f.bot_id}/sessions/{session}':return 200,row
   if path.endswith('/tasks'):return 200,{'items':[task]}
   if path.endswith('/tasks/subagent/'+child):return 200,task
   if path.endswith(('/delegations','/messages')):return 200,{'items':[]}
   return None
  f.extra_api_handler=api
  page.goto(f.base+'/#/',wait_until='domcontentloaded')
  page.wait_for_function("document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.has('workspace-tabs')")
  page.evaluate("id=>document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('workspace-tabs').openSessionChat({sessionId:id,title:'Synthetic parent'})",session)
  page.get_by_role('button',name='任务',exact=True).click()
  task_dialog=page.locator('[role=dialog]');task_dialog.get_by_role('button').filter(has_text='33333333').click()
  task_dialog.locator('[data-slot=dialog-view-header] button:has(svg.lucide-chevron-left)').wait_for()
  page.screenshot(path=str(OUT/'task-detail.png'))
  assert back(page)
  task_dialog.get_by_role('button').filter(has_text='33333333').wait_for()
  assert task_dialog.is_visible();case('task-detail-to-list-with-modal-retained')
  page.screenshot(path=str(OUT/'task-list-returned.png'))
  assert back(page);task_dialog.wait_for(state='hidden');case('task-list-dismissed-on-second-back')
  assert f.fingerprint(page)==token and f.counts['login']==1
  assert not f.page_errors
  assert not [q for q in f.api_events if q['method'] not in ['GET','HEAD','OPTIONS'] and q['path']!='/auth/login']
  (OUT/'ui-result.json').write_text(json.dumps({'cases':results,'loginCalls':f.counts['login'],'pageErrors':f.page_errors,'scope':'Actual generated phone UI in Chromium390x844, synthetic API/native boundaries; system gesture/ArkWeb/device not verified'},indent=2)+'\n')
  context.close()
except Exception:
 try:
  page.screenshot(path=str(OUT/'failure.png'))
  (OUT/'failure.json').write_text(json.dumps({'url':page.url,'body':page.locator('body').inner_text()[:15000],'errors':f.page_errors},ensure_ascii=False,indent=2))
 except Exception:pass
 raise
finally:f.server.shutdown()
