import sys,json
from pathlib import Path
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
from playwright.sync_api import sync_playwright,expect
h.cfg['database']='memoh_schedule_release_181_20261029';out=Path('/workspace/bot-dialog-handoff/contact-tab-route')
def digest():
 return {table:h.sql("SELECT count(*)::text||':'||md5(coalesce(string_agg("+key+"::text,',' ORDER BY "+key+"),'')) FROM "+table) for table,key in [('bot_sessions','id'),('session_runs','run_id'),('bot_history_messages','id')]}
before=digest()
with sync_playwright() as p:
 b=p.chromium.launch(executable_path='/usr/bin/chromium',headless=True,args=['--no-sandbox']);c=b.new_context(viewport={'width':390,'height':844},color_scheme='dark');c.add_init_script('localStorage.setItem("token",'+json.dumps(h.auth['access_token'])+');localStorage.setItem("language","zh");localStorage.setItem("theme","dark");');page=c.new_page();errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:28110/bot/local-dialog',wait_until='domcontentloaded');page.locator('textarea:visible').wait_for(timeout=30000);page.wait_for_timeout(1500)
 assert page.url=='http://127.0.0.1:28110/bot/local-dialog';assert page.locator('html').evaluate('(e)=>e.classList.contains("dark")');expect(page.get_by_text('FIXED_NO_SESSION_CONTINUITY',exact=True)).to_be_visible()
 page.screenshot(path=str(out/'green-narrow-zh-dark.png'));b.close()
after=digest();assert before==after,(before,after)
result={'narrow_zh_dark_contact':True,'page_errors':errors,'before':before,'after':after,'scope':'existing isolated data, no new task or model call; table IDs/counts only, not every field digest'}
(out/'narrow-browser.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
