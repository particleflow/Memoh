from playwright.sync_api import sync_playwright
from pathlib import Path
import base64,json,urllib.parse
E=Path(__file__).parent
png=base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6r9sAAAAASUVORK5CYII=')
with sync_playwright() as p:
 browser=p.chromium.launch(headless=True,executable_path='/usr/bin/chromium',args=['--no-sandbox'])
 page=browser.new_page(viewport={'width':1180,'height':780});requests=[];errors=[]
 page.on('pageerror',lambda e:errors.append(str(e)))
 def api(route):
  path=urllib.parse.urlsplit(route.request.url).path
  denied=path.endswith('a'*64);requests.append({'method':route.request.method,'scoped': '/delegations/' in path,'denied':denied})
  route.fulfill(status=404 if denied else 200,headers={'Cache-Control':'no-store'},content_type='application/problem+json' if denied else 'image/png',body=b'{"code":"session.not_found"}' if denied else png)
 page.route('**/api/bots/**',api)
 page.goto('http://127.0.0.1:18952/review-inaccessible.html',wait_until='domcontentloaded',timeout=60000)
 page.wait_for_selector('[data-variant="content"] [role="status"]',timeout=30000)
 assert page.locator('[data-variant="content"]').inner_text().split()==['original.png','附件不可访问']
 assert page.locator('[data-variant="attachment"] [role="status"]').count()==1
 assert page.locator('[data-variant="content"] button,[data-variant="attachment"] button').count()==0
 page.wait_for_function('document.querySelector("[data-variant=available] img")?.naturalWidth>0')
 failed=lambda:sum(r['denied'] for r in requests)
 initial=failed();assert initial==2,requests
 page.screenshot(path=str(E/'desktop-inaccessible.png'))
 page.evaluate('window.fixture.openFailed()');page.wait_for_selector('[role="dialog"] [role="status"]')
 assert failed()==initial+1,requests
 page.get_by_role('button',name='Next',exact=True).click();page.wait_for_function('document.querySelector("[role=dialog] img")?.naturalWidth>0')
 page.get_by_role('button',name='Previous',exact=True).click();page.wait_for_selector('[role="dialog"] [role="status"]')
 assert page.locator('[role="dialog"] img').count()==0
 page.get_by_role('button',name='Close',exact=True).click();page.wait_for_selector('[role="dialog"]',state='hidden')
 page.evaluate('window.fixture.openFailed()');page.wait_for_selector('[role="dialog"] [role="status"]')
 assert failed()==initial+1,requests
 page.set_viewport_size({'width':390,'height':844});page.evaluate('document.documentElement.classList.add("dark")');page.screenshot(path=str(E/'mobile-gallery-inaccessible.png'))
 page.get_by_role('button',name='Close',exact=True).click();page.wait_for_selector('[role="dialog"]',state='hidden')
 page.screenshot(path=str(E/'mobile-dark-inaccessible.png'))
 assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
 assert page.evaluate('window.fixture.snapshot()===window.fixture.original')
 assert all(r['method']=='GET' and r['scoped'] for r in requests),requests
 assert not errors,errors
 report={'environment':'isolated generated Harmony components; real authenticated originalBlob/hash/Blob path with synthetic HTTP responses; not full server or phone','url':'http://127.0.0.1:18952/review-inaccessible.html','requests':requests,'initialDeniedReaders':initial,'explicitFirstGalleryDeniedReader':1,'failedNavigationAndReopenRetries':0,'broaderBotHashFallbacks':0,'writes':0,'referenceJSONUnchanged':True,'healthyImageDecoded':True,'desktopAnd390pxDark':True,'pageErrors':errors}
 (E/'browser-result.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report));browser.close()
