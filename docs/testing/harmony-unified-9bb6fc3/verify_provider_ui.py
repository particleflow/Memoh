import json,sys,tempfile,os
from pathlib import Path
from playwright.sync_api import sync_playwright
ROOT=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(ROOT/'mobile/client/tests/stage2'))
import harmony_auth_persistence as f
OUT=Path(os.environ['HARMONY_TEST_EVIDENCE']);OUT.mkdir(parents=True,exist_ok=False)
f.base='http://127.0.0.1:4183';f.auto_configure=False
provider_id='77777777-7777-4777-8777-777777777777'
kind='openai-completions';requests=[]
def provider():return {'id':provider_id,'name':'Synthetic existing shared provider','client_type':kind,'enable':True,'config':{'base_url':'https://provider.example/v1','api_key':'synthetic-not-a-real-key'}}
def api(path,request):
 if path.startswith(('/transcription','/speech','/provider-templates')):
  requests.append({'path':path,'method':request.method})
  assert request.method=='GET','No mutations or test calls allowed'
  if path=='/transcription-providers':return 200,[provider()]
  if path=='/transcription-providers/'+provider_id:return 200,provider()
  if path=='/transcription-providers/meta':return 200,[{'provider':'openai-transcription','display_name':'Native transcription','config_schema':{'fields':[{'key':'base_url','type':'string','title':'API Base','order':1},{'key':'api_key','type':'secret','title':'API Key','order':2}]},'transcription_models':[]}]
  return 200,[]
 return None
f.extra_api_handler=api
results=[]
try:
 with sync_playwright() as pw:
  for kind in ['openai-completions','openai-responses']:
   with tempfile.TemporaryDirectory(prefix='provider-',dir=OUT) as profile:
    context,page=f.start(pw,Path(profile),expected_entry='/@vite/client')
    context.add_init_script("if(location.protocol!=='about:')localStorage.setItem('memoh:harmony:service-config:v1',JSON.stringify({version:1,apiOrigin:'https://alpha.example/api',hapDownloadUrl:'',resetPending:false}))")
    context.add_init_script("window.begoniaNative={request:async raw=>JSON.stringify({ok:true,data:JSON.parse(raw).command==='upload.list'?[]:undefined})}")
    page.reload(wait_until='domcontentloaded');f.login(page)
    page.goto(f.base+'/#/settings/providers?tab=voice&voiceProvider=transcription:'+provider_id,wait_until='domcontentloaded')
    base=page.locator('#transcription-provider-base_url')
    try:base.wait_for(timeout=12000)
    except Exception:
     page.screenshot(path=str(OUT/'provider-debug.png'))
     (OUT/'provider-debug.json').write_text(json.dumps({'body':page.locator('body').inner_text(),'url':page.url,'requests':requests,'api':f.api_events,'errors':f.page_errors},indent=2))
     raise
    assert base.input_value()=='https://provider.example/v1'
    key=page.locator('#transcription-provider-api_key');assert key.get_attribute('type')=='password'
    assert key.input_value()=='synthetic-not-a-real-key'
    page.wait_for_timeout(450)
    base.scroll_into_view_if_needed()
    box=base.bounding_box();assert box and 0<=box['x'] and box['x']+box['width']<=390
    page.screenshot(path=str(OUT/(kind+'-390.png')),animations='disabled')
    page.reload(wait_until='domcontentloaded');base.wait_for()
    assert base.input_value()=='https://provider.example/v1' and key.get_attribute('type')=='password'
    assert page.locator('#transcription-provider-name').input_value()==provider()['name']
    results.append({'clientType':kind,'sameProviderId':provider_id,'nativeSchemaFieldsVisible':True,'maskedKey':True,'reloadRetainsConfig':True})
    context.close()
 assert not f.page_errors
 (OUT/'provider-ui.json').write_text(json.dumps({'cases':results,'requests':requests,'managementMutations':0,'modelCalls':0,'pageErrors':f.page_errors,'scope':'Actual generated Harmony Vite UI, Chromium390x844 with synthetic authenticated API and native seams. No real ASR, provider configuration, phone or NAS.'},indent=2)+'\n')
finally:f.server.shutdown()
