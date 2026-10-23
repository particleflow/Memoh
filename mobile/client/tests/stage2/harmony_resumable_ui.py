"""Compiled candidate UI with a synthetic native bridge; no phone runtime."""
import json,tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as fixture
bridge=r"""(() => {
window.__uploadCalls=[]; window.__staged=[];
window.begoniaNative={request:async raw=>{const q=JSON.parse(raw); window.__uploadCalls.push({command:q.command,scope:q.scope,bot:q.botId});
 if(q.command==='upload.select') {const result={handle:'a'.repeat(32),hash:'b'.repeat(64),size:209715200,name:'synthetic-200MiB.bin',type:'application/octet-stream',resumable:true};window.__staged=[result];return JSON.stringify({ok:true,data:[result]})}
 if(q.command==='upload.list')return JSON.stringify({ok:true,data:window.__staged});
 if(q.command==='upload.discard'){window.__staged=[];return JSON.stringify({ok:true})}
 return JSON.stringify({ok:true}); }};
})()"""
try:
 with tempfile.TemporaryDirectory(prefix='resume-ui-',dir=fixture.OUT) as profile,sync_playwright() as pw:
  fixture.current_dist=fixture.new_dist
  context,page=fixture.start(pw,Path(profile))
  context.add_init_script(bridge);page.reload(wait_until='domcontentloaded')
  fixture.login(page)
  page.get_by_role('button',name='恢复本会话待传附件').wait_for(state='visible')
  # A controlled preexisting pending record imitates only the native boundary.
  page.evaluate("window.__staged=[{handle:'a'.repeat(32),hash:'b'.repeat(64),size:209715200,name:'synthetic-200MiB.bin',type:'application/octet-stream',resumable:true}]")
  page.get_by_role('button',name='恢复本会话待传附件').click()
  page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for()
  assert not page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.run')")
  assert page.evaluate('document.documentElement.scrollWidth <= window.innerWidth')
  page.get_by_role('button',name='移除',exact=True).click()
  page.get_by_text('synthetic-200MiB.bin',exact=False).wait_for(state='hidden')
  calls=page.evaluate('window.__uploadCalls')
  assert any(c['command']=='upload.discard' for c in calls)
  assert all(c.get('scope','').startswith('draft:') for c in calls if c['command'] in ['upload.list','upload.discard'])
  fixture.logout(page)
  assert page.evaluate("window.__uploadCalls.some(x=>x.command==='upload.close')")
  assert not fixture.page_errors
  report={'scope':__doc__,'candidate':fixture.candidate_label(),'compiledRestoreAndRemove':True,'restoreDidNotUploadOrDispatch':True,'draftScopeBound':True,'logoutClosesNative':True,'viewport':'390x844','pageErrors':fixture.page_errors,'nativeDevice':False}
  (fixture.OUT/'resumable-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));context.close()
finally: fixture.server.shutdown()
