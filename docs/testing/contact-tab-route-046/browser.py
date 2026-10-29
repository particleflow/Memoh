import sys,json
from pathlib import Path
from urllib.parse import urlencode
sys.path.insert(0,'/workspace/bot-dialog-handoff');from results_harness import *
from playwright.sync_api import sync_playwright,expect
out=Path('/workspace/bot-dialog-handoff/contact-tab-route');mode=sys.argv[1]
f=json.loads((r/'run-anchor-fixture.json').read_text());rows=wait(lambda:http('/bots/'+bot+'/tasks?limit=100')['items']);task=next(x for x in rows if x['record_id']==f['original_task']['record_id']);d=task['destination']
q=urlencode({'session':d['session_id'],'task_bot':bot,'task_type':task['task_type'],'task_id':task['task_id'],'task_record':task['record_id'],'run':d['run_id']})
base='http://127.0.0.1:28110/bot/local-dialog'
with sync_playwright() as p:
 b=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':1440,'height':1000});c.add_init_script('localStorage.setItem("token",'+json.dumps(auth['access_token'])+');localStorage.setItem("language","en");');page=c.new_page();errors=[];writes=[]
 page.on('pageerror',lambda e:errors.append(str(e)))
 page.on('request',lambda req:writes.append({'method':req.method,'path':req.url.split('/api')[-1]}) if req.method not in ['GET','OPTIONS'] else None)
 page.goto(base,wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000)
 # Open via the actual directory so the original contact tab remains pinned.
 page.get_by_role('button',name='Tasks',exact=True).click();page.get_by_role('dialog').get_by_text(task['name'],exact=True).click()
 page.wait_for_url('**run='+d['run_id']);expect(page.locator('.dv-tab').filter(has_text='Local Dialog').first).to_be_visible();page.wait_for_timeout(800)
 page.locator('.dv-tab').filter(has_text='Local Dialog').first.click();page.wait_for_timeout(1000)
 observed={'mode':mode,'after_contact_tab':page.url,'tabs':page.locator('.dv-tab').all_text_contents(),'page_errors':errors}
 page.screenshot(path=str(out/(mode+'-contact.png')))
 if mode=='red':
  assert 'run='+d['run_id'] in page.url
  page.reload(wait_until='domcontentloaded');expect(page.locator('.dv-tab.dv-active-tab')).not_to_have_text('Local Dialog',timeout=20000)
  observed['after_refresh']=page.url;observed['old_task_reopened']=True
 else:
  assert page.url==base,page.url
  copied=page.url
  page.reload(wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=20000);expect(page.locator('.dv-tab.dv-active-tab')).to_contain_text('Local Dialog')
  observed['contact_refresh']=page.url
  page.locator('.dv-tab').filter(has_text='TASK_PROJECTION_V6').first.click();page.wait_for_url('**run='+d['run_id']);page.wait_for_timeout(700)
  observed['task_tab_anchor_restored']=page.url
  page.reload(wait_until='domcontentloaded');expect(page.get_by_text('TASK_PROJECTION_V6',exact=True).last).to_be_in_viewport(timeout=20000)
  expect(page.get_by_text('TASK_PROJECTION_V6',exact=True).last).to_be_visible(timeout=20000);page.get_by_text('Loading...',exact=True).filter(visible=True).wait_for(state='hidden',timeout=20000);page.wait_for_timeout(1500)
  expect(page.get_by_text('TASK_PROJECTION_V6',exact=True).last).to_be_visible(timeout=20000)
  observed['task_refresh_exact_turn']=True;page.screenshot(path=str(out/'green-task.png'))
  other=c.new_page();other.goto(copied,wait_until='domcontentloaded');other.locator('textarea:visible').wait_for(timeout=20000);expect(other.locator('.dv-tab.dv-active-tab')).to_contain_text('Local Dialog');observed['copied_contact_link']=other.url
 observed['requests']=writes
 (out/(mode+'-browser.json')).write_text(json.dumps(observed,indent=2));print(json.dumps(observed));b.close()
