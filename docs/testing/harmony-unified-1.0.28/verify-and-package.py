from pathlib import Path
import json,hashlib,zipfile,re,subprocess,shutil,io,base64
from PIL import Image
E=Path(__file__).parent;R=Path('/workspace/harmony-recovery-20261023/image-probe-source');C=R/'mobile/client';hap=E/'garnet-assistant-1.0.28-1000028-api26-release-unsigned.hap';sha=lambda b:hashlib.sha256(b).hexdigest()
report=json.loads((C/'docs/evidence/development-runs/unified-1.0.28-final-release/build-report.json').read_text());assert report['phase']=='complete' and report['qualificationExitCode']==0
assert not subprocess.check_output(['git','status','--porcelain'],cwd=R);source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=R,text=True).strip()
patterns=[rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',rb'gh[pousr]_[A-Za-z0-9]{30,}',rb'github_pat_[A-Za-z0-9_]{40,}',rb'AKIA[A-Z0-9]{16}',rb'https?://[^\s/]+:[^\s/@]+@',rb'https?://(?:192\.168\.|10\.[0-9]+\.|\[f[cd][0-9a-f]{2}:)']
findings=[];art=R/'mobile/client/mobile/harmony/artwork';expected=json.loads((art/'provenance.json').read_text());water=json.loads((art/'watermark-derivation.json').read_text());hit={'whale':[],'koi':[]};features={n:[]for n in ['/bots/{bot_id}/tasks','/tts/playback','contactNavigationPending','__garnetImageTiming']}
with zipfile.ZipFile(hap)as z:
 assert z.testzip()is None
 for n in z.namelist():
  b=z.read(n)
  assert not n.lower().endswith(('.p12','.pfx','.p7b','.pem','.keystore'))
  for i,p in enumerate(patterns):
   if re.search(p,b):findings.append({'member':n,'category':i})
  for key in features:
   if key.encode()in b:features[key].append(n)
  for key,e in zip(hit,expected['approvedSources']):
   if sha(b)==e['sha256']:hit[key].append(n)
 assert not findings,findings
 assert hit['koi'],hit
 icon=z.read('resources/base/media/app_icon.png');im=Image.open(io.BytesIO(icon));assert im.size==(512,512)
 assert (C/'mobile/harmony/app/AppScope/resources/base/media/app_icon.png').read_bytes()==(art/'approved/whale.png').read_bytes()==(C/'mobile/harmony/app/entry/src/main/resources/base/media/app_icon.png').read_bytes()
 assert all(features.values()),features
 module=json.loads(z.read('module.json'));app=module['app'];assert app['versionName']=='1.0.28'and app['versionCode']==1000028 and app['bundleName']=='cloud.begonia.workassistant'
 slot=z.read('resources/rawfile/client-web/memoh_client.json');cfg=json.loads(slot);assert len(slot)==8192 and cfg['apiOrigin']==cfg['hapDownloadUrl']==''and cfg.get('legacyBinding')is None and'networkPolicy'not in cfg
 assert module['module']['abilities'][0]['startWindowIcon']=='$media:startup_art'
 css=b'\n'.join(z.read(n)for n in z.namelist()if n.endswith('.css'))
 uris=re.findall(rb'data:image/png;base64,([A-Za-z0-9+/=]+)',css);hashes={sha(base64.b64decode(x,validate=True))for x in uris};assert {e['sha256']for e in water['derived']}<=hashes
 for e in water['derived']:assert Image.open(art/e['path']).getchannel('A').getextrema()==(0,6)
 snap=json.loads(z.read('resources/rawfile/source-build.json'));assert snap['sourceSha256']==report['sourceIdentitySha256']
record={'productName':'Garnet Assistant','versionName':'1.0.28','versionCode':1000028,'sourceCommit':source,'sourceParent':'0b353a47c03356e86742dbc188fad5d7a3ddff3c','sourceIdentitySHA256':report['sourceIdentitySha256'],'effectiveSharedCommit':'5a42d013e801cb6229ee029113e3a63782bc3e13','compatibleBackendCommit':'5a42d013e801cb6229ee029113e3a63782bc3e13','consumerMapFiles':15,'consumerMapSHA256Verified':True,'hapName':hap.name,'hapBytes':hap.stat().st_size,'hapSHA256':sha(hap.read_bytes()),'bundleName':app['bundleName'],'sdkVersion':report['sdkVersion'],'releaseMode':True,'officialApi26Compile':True,'signed':False,'privateConfigurationInjected':False,'publicConfigurationEmpty':True,'publicConfigBytes':8192,'crcVerified':True,'formalInputs':len(snap['files']),'resourceDigestsVerified':True,'identityPermissionsRetained':True,'approvedArtworkCommit':'bd0b3933d24d944973fdca8e7d22c6f6b2a507bc','artworkMatches':hit,'compiledWhaleIcon':{'sourceBytesUnchanged':True,'officialCompiledSize':[512,512],'compiledBytes':len(icon),'compiledSHA256':sha(icon),'HAPByteIdenticalToSource':False},'watermarkMaxAlpha':6,'featurePresence':features,'packageScan':{'findings':findings,'exhaustiveEncodingClaim':False},'phoneAcceptance':False,'NASDeployedByThisTask':False,'SDKDownloaded':False,'newBuildCount':1,'compilerVMLabels':report['compilerVMLabels'],'prior27QualificationFailureRetained':True}
(E/'verification.json').write_text(json.dumps(record,indent=2)+'\n');H=E/'handoff';H.mkdir(exist_ok=False)
for p in [hap,C/'mobile/client_config.py',C/'mobile/image_timing_readback.py',E/'verification.json']:shutil.copyfile(p,H/p.name)
(H/'source-lock.json').write_text(json.dumps(record,indent=2)+'\n');(H/'HANDOFF.txt').write_text('Garnet Assistant 1.0.28 / 1000028 RELEASE UNSIGNED\nSource '+source+'\nCompatible release '+record['compatibleBackendCommit']+'\nVerify SHA256SUMS, then existing Windows owner injects the already approved private configuration and verified legacy binding with client_config.py, and signs with the existing identity/profile/device scope. Preserve login/data; never return configured or signed private output to GitHub. No SDK or signing material included.\nContains shared5a42 fixed Bot contact, persisted tasks, exact run/turn history destination, Home/Dockview read-aloud admission, task-link Back and approved whale/koi artwork. Prior native transfers and login migration retained. Cloud isolated route checks are not physical phone acceptance or complete Bot autonomy/unknown reconciliation. No model called or NAS deployed by cloud build. Previous1.0.26 and1.0.27 stay intact.\n')
(H/'SHA256SUMS').write_text(''.join(sha(p.read_bytes())+'  '+p.name+'\n'for p in sorted(H.iterdir())))
out=E/'garnet-assistant-1.0.28-unified-unsigned-handoff.zip';assert not out.exists()
with zipfile.ZipFile(out,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=9)as z:
 for p in sorted(H.iterdir()):z.write(p,p.name)
with zipfile.ZipFile(out)as z:assert z.testzip()is None and sha(z.read(hap.name))==record['hapSHA256']
r={'filename':out.name,'size':out.stat().st_size,'sha256':sha(out.read_bytes()),'hapName':hap.name,'hapBytes':record['hapBytes'],'hapSHA256':record['hapSHA256'],'sourceCommit':source,'unifiedCommit':record['effectiveSharedCommit'],'compatibleBackendCommit':record['compatibleBackendCommit'],'publicConfigurationEmpty':True,'signed':False,'zipCRCAndInnerHash':True,'members':[p.name for p in sorted(H.iterdir())]};(E/'handoff-verification.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
