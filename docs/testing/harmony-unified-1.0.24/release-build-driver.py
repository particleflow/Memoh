"""Invoke the existing official release configuration; reuse project integrity helpers.
No SDK, compiler, project script, signing configuration or verification rule is patched.
"""
from pathlib import Path
import hashlib,json,os,subprocess,sys,zipfile,shutil
CLIENT=Path('/workspace/harmony-recovery-20261023/image-probe-source/mobile/client')
TOOLS=CLIENT/'mobile/harmony/tools';sys.path.insert(0,str(TOOLS))
import build_api26 as checks
from materials import verify_snapshot,verify_rawfile_resources,evidence_output
BASE=CLIENT/'mobile/harmony';PROJECT=BASE/'app'
OUT=evidence_output(CLIENT/'docs/evidence/development-runs/unified-1.0.24-final-release',['api26-build.log','build-report.json']);OUT.mkdir(parents=True,exist_ok=True)
CLT=Path('/workspace/harmony-recovery-20261023/toolchains/commandline-tools/command-line-tools')
report={'phase':'preflight','exitCode':None,'qualificationExitCode':None,'buildMode':'release','signed':False,'published':False,'officialApi26Compile':False,'hapResourceDigestsVerified':False,'driverSHA256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def save(): (OUT/'build-report.json').write_text(json.dumps(report,indent=2)+'\n')
try:
 snapshot=json.loads((BASE/'materials/source-build.json').read_text());owned=verify_snapshot(snapshot);checks.verify_raw_dependency()
 report.update(sourceIdentitySha256=snapshot['sourceSha256'],expectedSourceFiles=snapshot['files'])
 metadata=CLT/'sdk/default/openharmony/ets/oh-uni-package.json';sdk=json.loads(metadata.read_text());assert str(sdk['apiVersion'])=='26' and sdk['version']=='26.0.0.105'
 node=CLT/'tool/node/bin/node';hvigor=CLT/'bin/hvigorw';ohpm=CLT/'bin/ohpm'
 with node.open('rb') as stream:assert stream.read(4)==b'\x7fELF'
 profile=json.loads((PROJECT/'build-profile.json5').read_text());assert not profile['app']['signingConfigs'];assert 'release' in [m['name'] for m in profile['app']['buildModeSet']]
 inputs={str(p):digest(p) for p in [node,hvigor,ohpm,metadata]};report.update(sdkVersion=sdk['version'],toolchainMetadataSHA256=inputs)
 cache=Path('/workspace/harmony-recovery-20261023/repo/mobile/client/validation/harmony/.build')
 environment=dict(os.environ,PATH=str(node.parent)+os.pathsep+str(CLT/'bin')+os.pathsep+os.environ.get('PATH',''),HVIGOR_USER_HOME=str(cache/'hvigor-user'),OHPM_HOME=str(cache/'ohpm-home'),npm_config_cache=str(cache/'npm-cache'))
 command=[str(hvigor),'assembleHap','--mode','module','-p','product=default','-p','module=entry@default','-p','buildMode=release','--no-daemon']
 report.update(phase='official_build',command=command);save()
 with (OUT/'api26-build.log').open('w') as log:
  dependency=subprocess.run([str(ohpm),'install','--all'],cwd=PROJECT,env=environment,stdout=log,stderr=subprocess.STDOUT);report['dependencyExitCode']=dependency.returncode;assert dependency.returncode==0,'Existing native dependency resolution failed'
  result=subprocess.run(command,cwd=PROJECT,env=environment,stdout=log,stderr=subprocess.STDOUT)
 report.update(exitCode=result.returncode,officialApi26Compile=result.returncode==0,buildLogSHA256=digest(OUT/'api26-build.log'),phase='post_build_integrity');save();assert result.returncode==0,'Official release assembleHap failed'
 verify_snapshot(snapshot);checks.verify_raw_dependency();assert {p:digest(Path(p)) for p in inputs}==inputs,'Official SDK entry/metadata drift'
 package_path=CLIENT/'mobile/.build/memoh-web/resource-package.json';package=json.loads(package_path.read_text());hap=PROJECT/'entry/build/default/outputs/default/entry-default-unsigned.hap'
 with zipfile.ZipFile(hap) as z:
  assert z.testzip() is None
  checks.verify_hap_rawfiles(z,verify_rawfile_resources(package))
  abc={n:{'bytes':len(z.read(n)),'sha256':hashlib.sha256(z.read(n)).hexdigest()} for n in z.namelist() if n.endswith('.abc')};assert abc
  native={}
  for architecture,machine in [('arm64-v8a',183),('x86_64',62)]:
   n='libs/'+architecture+'/libbegoniaraw.so';data=z.read(n);assert data[:6]==b'\x7fELF\x02\x01' and int.from_bytes(data[18:20],'little')==machine;native[n]={'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest(),'elfMachine':machine}
  assert z.read('resources/rawfile/raw-codec-license.txt')==(PROJECT/'entry/src/main/resources/rawfile/raw-codec-license.txt').read_bytes()
  module=json.loads(z.read('module.json'));assert module['app']['bundleName']=='cloud.begonia.workassistant' and module['app']['versionCode']==1000024 and module['app']['versionName']=='1.0.24'
  assert module['app']['buildMode']=='release' and module['app']['debug'] is False
  assert not module['module'].get('skillProfiles')
  assert json.loads(z.read('resources/rawfile/source-build.json'))==snapshot
  assert not any(n.endswith('/contract.json') or n.endswith('/coverage-matrix.json') or '/demo-result.' in n or '/skills/' in n for n in z.namelist())
  slot=z.read('resources/rawfile/client-web/memoh_client.json');config=json.loads(slot);assert len(slot)==8192 and config['apiOrigin']==config['hapDownloadUrl']=='' and config.get('legacyBinding') is None and 'networkPolicy' not in config
  old=Path('/workspace/harmony-recovery-20261023/evidence/private-1.0.12-52609f7/memoh-harmony-1.0.12-1000012-c604c7da01-api26-unsigned.hap');assert digest(old)=='4d5c4c4ffe1a32d19321597281fdcb6c8f725511362aafb870704c2bceb82780'
  with zipfile.ZipFile(old) as p:previous=json.loads(p.read('module.json'))
  for m in [module,previous]:
   for key in ['versionName','versionCode','debug','buildMode']:m['app'].pop(key,None)
  assert module==previous,'Identity/permissions/platform metadata changed beyond version and requested build mode'
 verify_snapshot(snapshot)
 report.update(qualificationExitCode=0,hapResourceDigestsVerified=True,crcVerified=True,sourceMatchesBuild=True,identityPermissionsUnchanged=True,publicConfigEmpty=True,publicConfigBytes=len(slot),hapAbc=abc,nativeLibraries=native,resourcePackageSHA256=digest(package_path),unsignedIntermediate={'path':str(hap.relative_to(CLIENT)),'sha256':digest(hap),'bytes':hap.stat().st_size},phase='complete')
 target=Path('/workspace/harmony-recovery-20261023/evidence/unified-1.0.24/garnet-assistant-1.0.24-1000024-api26-release-unsigned.hap');shutil.copy2(hap,target);report['preservedPrivateHAP']=str(target)
except Exception as error:
 report.update(qualificationExitCode=1,failurePhase=report['phase'],errorType=type(error).__name__,error=str(error));raise
finally:
 save();print(json.dumps({k:report.get(k) for k in ['phase','exitCode','qualificationExitCode','officialApi26Compile','buildMode','sdkVersion','unsignedIntermediate','error']}),flush=True)
