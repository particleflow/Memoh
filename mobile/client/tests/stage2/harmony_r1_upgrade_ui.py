"""Swap frozen r1 and current compiled resources at one browser origin; not an HAP install."""
import json, tempfile
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f
old=f.legacy_source('HARMONY_LEGACY_R1_ROOT')/'mobile/.build/memoh-web/official-dist'
try:
 with tempfile.TemporaryDirectory(prefix='r1-upgrade-',dir=f.OUT) as profile,sync_playwright() as pw:
  f.current_dist=old;c,p=f.start(pw,Path(profile));p.get_by_role('heading',name='连接服务').wait_for();p.locator('#service-api').fill('https://alpha.example/api');p.get_by_role('button',name='保存并继续').click();f.login(p)
  token=f.fingerprint(p);config=p.evaluate("localStorage.getItem('memoh:harmony:service-config:v1')")
  assert config and f.counts['login']==1;c.close()
  f.current_dist=f.new_dist;c,p=f.start(pw,Path(profile));f.wait_chat(p)
  assert f.fingerprint(p)==token and f.counts['login']==1
  assert p.evaluate("localStorage.getItem('memoh:harmony:service-config:v1')")==config
  c.close();c,p=f.start(pw,Path(profile));f.wait_chat(p);assert f.fingerprint(p)==token
  assert f.counts['login']==1
  f.revoked=True;p.reload(wait_until='domcontentloaded');p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')")
  f.revoked=False;f.login(p);f.clock_offset=7200
  p.reload(wait_until='domcontentloaded');p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')")
  c.close();c,p=f.start(pw,Path(profile));p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')")
  report={'from':'1.0.8-rc.local-config-r1','to':'1.0.9-dev.contracts6ddf61dc','sameBrowserOrigin':True,'samePersistentProfileAcrossProcesses':True,'configurationPreserved':True,'validTokenPreservedWithoutLogin':True,'restartAfterUpgrade':True,'liveAPI':False,'actualHAPOverwriteInstall':False,'nativeDevice':False,'pageErrors':f.page_errors}
  report['to']=f.candidate_label()
  report.update(actualEntryResourceAsserted=True,revokedCredential401Cleared=True,expiredSyntheticJWTCleared=True,expiredCredentialNotRestoredAfterRestart=True)
  (f.OUT/'r1-upgrade-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));c.close()
finally:f.server.shutdown()
