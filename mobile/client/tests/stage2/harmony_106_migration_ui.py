"""Actual v1.0.6 -> candidate with independently verified legacy binding.

Requires both the real old resources and a candidate provisioned with that
verified binding. Never declare an unbound-state preservation check to be a
completed login migration. Synthetic HTTP only; not HAP installation.
"""
import json, tempfile, re
from pathlib import Path
from playwright.sync_api import sync_playwright
import harmony_auth_persistence as f
old=f.legacy_source('HARMONY_LEGACY_106_ROOT')
# Read only to fill the migration form in a disposable local browser profile.
# Never emit, screenshot or persist this infrastructure value in test evidence.
legacy_api=json.loads((old/'config/memoh_client.json').read_text())['apiOrigin']
f.auto_configure=False
config_key='memoh:harmony:service-config:v1'
backup_key='memoh:harmony:unbound-state:v1'
observed=[]
original_intercept=f.intercept
phase='legacy'
def intercept(route):
 r=route.request
 if '/api/' in r.url:
  observed.append({'phase':phase,'authorized':bool(r.headers.get('authorization')),'path':r.url.split('/api',1)[1].split('?',1)[0]})
 return original_intercept(route)
f.intercept=intercept
try:
 with tempfile.TemporaryDirectory(prefix='106-migration-',dir=f.OUT) as profile,sync_playwright() as pw:
  f.current_dist=old/'mobile/.build/memoh-web/official-dist'
  c,p=f.start(pw,Path(profile));f.login(p);old_token=f.fingerprint(p)
  assert p.evaluate('(key)=>localStorage.getItem(key)',config_key) is None
  p.evaluate("localStorage.setItem('chat-input-drafts',JSON.stringify({'synthetic-draft':'keep locally'}));localStorage.setItem('pinned-bot-ids',JSON.stringify(['synthetic-bot']))")
  c.close();phase='unconfigured';f.current_dist=f.new_dist
  c,p=f.start(pw,Path(profile))
  p.get_by_role('heading',name='连接服务').wait_for()
  assert f.fingerprint(p)==old_token,'old token changed before configuration'
  count=len(observed);p.wait_for_timeout(200);assert len(observed)==count,'new API request while configuration is absent'
  assert not any(x['phase']=='unconfigured' for x in observed),'API observed in unconfigured phase'
  # The candidate's independently verified binding, not the typed URL alone,
  # must authorize retaining the original session at the original API base.
  phase='same-origin-migration';p.locator('#service-api').fill(legacy_api)
  assert p.get_by_role('button',name='保存并继续').is_enabled(),'candidate has no verified legacy binding; migration remains incomplete'
  p.get_by_role('button',name='保存并继续').click();f.wait_chat(p)
  valid=f.fingerprint(p);assert valid==old_token and f.counts['login']==1
  assert p.evaluate("JSON.parse(localStorage.getItem('chat-input-drafts')||'{}')['synthetic-draft']")=='keep locally'
  assert 'synthetic-bot' in p.evaluate("JSON.parse(localStorage.getItem('pinned-bot-ids')||'[]')")
  assert not any(x['path'].startswith('/auth/register') for x in observed)
  c.close();phase='restart';c,p=f.start(pw,Path(profile));f.wait_chat(p)
  assert f.fingerprint(p)==valid and f.counts['login']==1
  assert p.evaluate('(key)=>JSON.parse(localStorage.getItem(key)).resetPending',config_key) is False
  f.logout(p);c.close();c,p=f.start(pw,Path(profile));p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')");c.close()
  assert not f.page_errors
  report={'from':'1.0.6-rc.resume-tasks276c0b54','to':f.candidate_label(),'actualOldCompiledResources':True,'persistentBrowserProfile':True,'oldRuntimeEndpointKeyAbsent':True,'noAPIWhileUnconfigured':True,'verifiedSameOriginPreservesLoginDraftsAndPins':True,'noAccountRegistration':True,'validLoginAndConfigRetainedAfterRestart':True,'explicitLogoutPersists':True,'loginCalls':f.counts['login'],'infrastructureAddressInEvidence':False,'externalAPIContact':False,'actualHAPOverwriteInstall':False,'nativeDevice':False,'pageErrors':f.page_errors}
  (f.OUT/'106-migration-ui.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report))
except Exception as error:
 # Browser errors may echo a form value; never print the private legacy address.
 from urllib.parse import urlsplit
 message=str(error)
 for value in (legacy_api,urlsplit(legacy_api).netloc,urlsplit(legacy_api).hostname):
  if value:message=message.replace(value,'[redacted]')
 message=re.sub(r'eyJ[A-Za-z0-9._-]+','[redacted-token]',message)
 print(json.dumps({'passed':False,'phase':phase,'errorType':type(error).__name__,'message':message[:1200]}))
 raise SystemExit(1) from None
finally:f.server.shutdown()
