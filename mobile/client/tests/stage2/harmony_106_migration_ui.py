"""Actual archived v1.0.6 resources -> current resources, intercepted synthetic API; not HAP installation."""
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
  p.get_by_text('旧版没有保存服务地址。',exact=False).wait_for()
  assert f.fingerprint(p)==old_token,'old token changed before configuration'
  count=len(observed);p.wait_for_timeout(200);assert len(observed)==count,'new API request while configuration is absent'
  assert not any(x['phase']=='unconfigured' for x in observed),'API observed in unconfigured phase'
  # Fill the original server address locally. Even the matching address cannot
  # establish provenance for the old unbound token, so explicit login is required.
  phase='configured-not-logged-in';p.locator('#service-api').fill(legacy_api);p.get_by_role('button',name='保存并继续').click();p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')"),'legacy token survived explicit config transition'
  assert not any(x['authorized'] for x in observed if x['phase']=='configured-not-logged-in'),'authorized request before explicit login'
  backup=p.evaluate('(key)=>JSON.parse(localStorage.getItem(key))',backup_key)
  assert 'token' not in backup['entries'],'token in backup';assert json.loads(backup['entries']['chat-input-drafts'])=={'synthetic-draft':'keep locally'},'local draft backup changed'
  assert not p.evaluate("JSON.parse(localStorage.getItem('chat-input-drafts')||'{}')['synthetic-draft']"),'legacy draft activated for configured endpoint'
  assert 'synthetic-bot' not in p.evaluate("JSON.parse(localStorage.getItem('pinned-bot-ids')||'[]')"),'legacy pin activated for configured endpoint'
  phase='relogin';f.login(p);valid=f.fingerprint(p);assert f.counts['login']==2
  assert not any(x['path'].startswith('/auth/register') for x in observed)
  c.close();phase='restart';c,p=f.start(pw,Path(profile));f.wait_chat(p)
  assert f.fingerprint(p)==valid and f.counts['login']==2
  assert p.evaluate('(key)=>JSON.parse(localStorage.getItem(key)).resetPending',config_key) is False
  f.logout(p);c.close();c,p=f.start(pw,Path(profile));p.wait_for_selector('#username')
  assert not p.evaluate("localStorage.getItem('token')");c.close()
  assert not f.page_errors
  report={'from':'1.0.6-rc.resume-tasks276c0b54','to':f.candidate_label(),'actualOldCompiledResources':True,'persistentBrowserProfile':True,'oldEndpointBindingAbsent':True,'noAPIWhileUnconfigured':True,'sameEnteredLegacyAddressStillRequiresLogin':True,'oldTokenNeverReusedAfterConfig':True,'oldDraftsAndPinsRetainedOnlyInUnboundLocalBackup':True,'backupExcludesToken':True,'noAccountRegistration':True,'validLoginAndConfigRetainedAfterRestart':True,'explicitLogoutPersists':True,'loginCalls':f.counts['login'],'infrastructureAddressInEvidence':False,'externalAPIContact':False,'actualHAPOverwriteInstall':False,'nativeDevice':False,'pageErrors':f.page_errors}
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
