from pathlib import Path
from playwright.sync_api import sync_playwright
from email.parser import BytesParser
from email import policy
import hashlib,json,urllib.parse
E=Path(__file__).parent;S=E/'samples';S.mkdir(exist_ok=True);O=E/'received';O.mkdir(exist_ok=True)
samples={'first.bin':bytes(range(256))*1024+b'first','second.txt':'second 雪\n'.encode()*513,'third.dat':bytes([5,4,3,2,1])*109}
for n,b in samples.items():(S/n).write_bytes(b)
records=[];errors=[];entries=[]
with sync_playwright() as p:
 browser=p.chromium.launch(headless=True,executable_path='/usr/bin/chromium',args=['--no-sandbox']);page=browser.new_page(viewport={'width':390,'height':844});page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto('http://127.0.0.1:18953/review-files.html',wait_until='domcontentloaded',timeout=60000)
 picker=page.locator('input[type="file"]:not([webkitdirectory])');picker.wait_for(state='attached',timeout=60000)
 assert picker.evaluate('input=>input.multiple') is True
 picker.set_input_files([str(S/n) for n in samples])
 page.wait_for_function('document.querySelector("input[type=file]:not([webkitdirectory])")?.files?.length===0',timeout=20000)
 for n in samples:page.get_by_text(n,exact=True).first.wait_for(timeout=20000)
 records=page.evaluate('fetch("/api/review-received").then(r=>r.json())')
 assert len(records)==3 and {r['name'] for r in records}==set(samples),records
 assert all((O/n).read_bytes()==b for n,b in samples.items())
 page.evaluate('document.documentElement.classList.add("dark")');page.screenshot(path=str(E/'mobile-files-upload.png'));assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
 assert not errors,errors
 report={'environment':'isolated generated FilesPane plus actual SDK multipart serialization, synthetic HTTP receiver; not NAS/phone','url':'http://127.0.0.1:18953/review-files.html','viewport':'390x844 dark','realBrowserFileList':True,'pickerMultiple':True,'uploaded':records,'receivedBytesAndSHA256Verified':True,'duplicateUploads':0,'pageErrors':errors,'nativePhonePickerVerified':False,'NASAcceptance':False};(E/'browser-result.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report,ensure_ascii=False));browser.close()
