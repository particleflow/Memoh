from pathlib import Path
import sys,json,tempfile
from urllib.parse import urlsplit,parse_qs
from playwright.sync_api import sync_playwright
import os
ROOT=Path(__file__).resolve().parents[3]/'mobile/client'
sys.path.insert(0,str(ROOT/'tests/stage2'))
import harmony_auth_persistence as f
E=Path(os.environ['HARMONY_WORKSPACE_TEST_OUT']).resolve();E.mkdir(parents=True,exist_ok=True)
f.new_dist=f.current_dist=Path(os.environ['HARMONY_WORKSPACE_TEST_DIST']).resolve()
pc='44444444-4444-4444-8444-444444444444'
offline='55555555-5555-4555-8555-555555555555'
base=f'/users/me/runtimes/{pc}/workspace-storage'
selected='.'; paths={'existing-project','archive'}; events=[]; reads=[]; mode='ok'
def api(path,request):
 global selected
 if path=='/users/me/runtimes':
  return 200,[{'id':pc,'name':'Synthetic PC','provider':'memoh','online':True},{'id':offline,'name':'Offline PC','provider':'memoh','online':False},{'id':'legacy','name':'Legacy transport','provider':'agenvo','online':True}]
 if path.startswith('/users/me/runtimes/') and '/workspace-storage' in path:
  assert path.startswith(base),'Do not read another or offline Runtime'
  assert f.valid_token(request.headers.get('authorization','').removeprefix('Bearer '))
  if request.method=='GET':
   reads.append(path)
   if mode=='forbidden':return 403,{'status':403,'code':'http.forbidden','detail':'PRIVATE_DIAGNOSTIC'}
   if mode=='missing':return 404,{'status':404,'code':'http.not_found','detail':'PRIVATE_DIAGNOSTIC'}
   query=parse_qs(urlsplit(request.url).query).get('path',['.'])[0]
   return 200,{'path':query,'selected_path':selected,'windows_path':'\\\\nas.example.test\\share\\'+pc+('' if selected=='.' else '\\'+selected.replace('/','\\')),'directories':[{'name':p.split('/')[-1],'path':p} for p in sorted(paths) if ('/' in p and p.rsplit('/',1)[0]==query) or ('/' not in p and query=='.')]}
  body=request.post_data_json;events.append({'method':request.method,'path':path,'body':body,'mode':mode})
  if mode=='unavailable':return 503,{'status':503,'code':'workspace.storage_unavailable','detail':'PRIVATE_DIAGNOSTIC'}
  if mode=='conflict':return 409,{'status':409,'code':'http.conflict','detail':'PRIVATE_DIAGNOSTIC'}
  if request.method=='POST':paths.add(body['path']);return 204,None
  if request.method=='PUT':selected=body['path'];return 204,None
 return None
f.extra_api_handler=api
try:
 with tempfile.TemporaryDirectory(dir=E) as profile,sync_playwright() as pw:
  context,page=f.start(pw,Path(profile));f.login(page);identity=f.fingerprint(page)
  page.goto(f.base+'/#/settings');page.get_by_text('工作区存储',exact=True).click()
  choose=page.get_by_role('button',name='选择 NAS 目录',exact=True)
  choose.first.wait_for();assert choose.count()==2 and choose.nth(1).is_disabled()
  assert page.get_by_text('Legacy transport',exact=True).count()==0
  page.screenshot(path=str(E/'storage-entry-390.png'))
  choose.first.click();dialog=page.get_by_role('dialog');dialog.wait_for()
  page.get_by_role('button',name='existing-project',exact=True).wait_for()
  assert not events
  page.get_by_role('button',name='existing-project',exact=True).click()
  # Existing phone back cancels the existing form, not the HAP or account.
  assert page.evaluate('window.__memohBack()') is True
  dialog.wait_for(state='hidden');assert not events and selected=='.'
  choose.first.click();page.get_by_role('button',name='existing-project',exact=True).click()
  page.get_by_role('button',name='保存',exact=True).click();dialog.wait_for(state='hidden')
  assert selected=='existing-project' and len(events)==1 and events[0]['method']=='PUT'
  page.reload();choose.first.click();page.get_by_label('新建目录',exact=True).fill('mobile-created')
  page.get_by_role('button',name='创建',exact=True).click()
  page.get_by_role('button',name='existing-project',exact=True).locator('svg').click()
  page.get_by_role('button',name='mobile-created',exact=True).wait_for()
  page.screenshot(path=str(E/'storage-dialog-390.png'))
  assert len(events)==2 and events[1]['method']=='POST'
  page.get_by_role('button',name='取消',exact=True).click();dialog.wait_for(state='hidden')
  assert selected=='existing-project' and 'existing-project/mobile-created' in paths
  mode='unavailable';choose.first.click();page.get_by_label('新建目录',exact=True).fill('must-not-create')
  page.get_by_role('button',name='创建',exact=True).click()
  page.get_by_text('NAS 工作区存储暂不可用。',exact=True).wait_for()
  assert page.get_by_role('button',name='保存',exact=True).is_disabled()
  assert 'existing-project/must-not-create' not in paths
  assert 'PRIVATE_DIAGNOSTIC' not in dialog.inner_text()
  page.screenshot(path=str(E/'storage-error-390.png'))
  page.get_by_role('button',name='取消',exact=True).click();dialog.wait_for(state='hidden')
  for denied in ['forbidden','missing']:
   mode=denied;count=len(events);choose.first.click();dialog.wait_for()
   page.wait_for_function('document.querySelector("[role=dialog]") && !document.querySelector("#nas-directory-name")')
   assert page.get_by_role('button',name='保存',exact=True).is_disabled()
   page.get_by_role('button',name='取消',exact=True).click();dialog.wait_for(state='hidden')
   assert len(events)==count
  mode='conflict';choose.first.click();page.get_by_role('button',name='archive',exact=True).click()
  count=len(events);page.get_by_role('button',name='保存',exact=True).click()
  page.wait_for_function('document.querySelector("[role=dialog]") && ![...document.querySelectorAll("button")].find(b=>b.textContent.trim()==="保存").disabled')
  assert len(events)==count+1 and selected=='existing-project'
  page.get_by_role('button',name='取消',exact=True).click();dialog.wait_for(state='hidden')
  assert page.evaluate('window.__memohBack()') is True
  page.wait_for_function('location.hash==="#/settings"')
  assert page.get_by_text('工作区存储',exact=True).is_visible()
  assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
  assert f.fingerprint(page)==identity and f.counts['login']==1 and not f.page_errors,f.page_errors
  writes=[x for x in f.api_events if x['method'] not in ['GET','HEAD','OPTIONS'] and x['path']!='/auth/login']
  assert all(x['path'].startswith(base) for x in writes)
  result={'fixedSharedCommit':'56b38d39fadda858963b36e597dcabd3a0fb0a1d','viewport':[390,844],'api':'synthetic exact account contract; not real NAS/RPC/SMB','events':events,'selectedPath':selected,'cancelWithoutSave':True,'nativeBackCancelsDialog':True,'nativeBackReturnsSettings':True,'offlineNotRead':True,'permission403And404DisableSave':True,'unavailable503DisablesWrites':True,'conflict409NotAutoRetried':True,'newDirectoryNotRemovedOnCancel':True,'savedSelectionReadAfterRefresh':True,'noUnexpectedBusinessWrites':True,'loginRetained':True,'pageErrors':f.page_errors,'actualPhone':False,'realLAN':False,'executionRootCutover':False,'arbitraryNASRootAccess':False}
  (E/'browser-storage-flow.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
  context.close()
finally:f.server.shutdown()
