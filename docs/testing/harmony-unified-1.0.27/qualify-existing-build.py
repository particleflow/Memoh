from pathlib import Path
import json,hashlib,zipfile,sys,subprocess,shutil
E=Path(__file__).parent;R=Path('/workspace/harmony-recovery-20261023/image-probe-source');C=R/'mobile/client';sys.path.insert(0,str(C/'mobile/harmony/tools'));import build_api26 as checks
from materials import verify_snapshot,verify_rawfile_resources
raw=json.loads((C/'docs/evidence/development-runs/unified-1.0.27-final-release/build-report.json').read_text());assert raw['officialApi26Compile']and raw['exitCode']==0 and raw['errorType']=='AssertionError'
snapshot=json.loads((C/'mobile/harmony/materials/source-build.json').read_text());verify_snapshot(snapshot);checks.verify_raw_dependency();sha=lambda b:hashlib.sha256(b).hexdigest()
for p,h in raw['toolchainMetadataSHA256'].items():assert sha(Path(p).read_bytes())==h
clt=Path('/workspace/harmony-recovery-20261023/toolchains/commandline-tools/command-line-tools');probe=[str(clt/'tool/node/bin/node'),str(clt/'sdk/default/openharmony/ets/build-tools/ets-loader/bin/ark/ts2abc.js'),'--target-api-version','26','--target-api-sub-version','Release'];version=subprocess.check_output(probe,text=True).strip();assert version=='24.0.0.0'
hap=C/'mobile/harmony/app/entry/build/default/outputs/default/entry-default-unsigned.hap';old=Path('/workspace/harmony-recovery-20261023/evidence/unified-1.0.26/garnet-assistant-1.0.26-1000026-api26-release-unsigned.hap')
with zipfile.ZipFile(hap)as z,zipfile.ZipFile(old)as p:
 assert z.testzip()is None;module=json.loads(z.read('module.json'));previous=json.loads(p.read('module.json'));assert module['module']['virtualMachine']=='ark'and previous['module']['virtualMachine']=='ark24.0.0.0'
 assert z.read('ets/modules.abc')[:8]==b'PANDA\0\0\0';abi=z.read('ets/modules.abc')[12:16];assert abi==p.read('ets/modules.abc')[12:16]==bytes([24,0,0,0])
 assert module['app']['versionCode']==1000027 and module['app']['versionName']=='1.0.27';assert module['module']['abilities'][0]['startWindowIcon']=='$media:startup_art'
 for m in [module,previous]:
  for k in ['versionName','versionCode']:m['app'].pop(k)
  m['module'].pop('virtualMachine');m['module']['abilities'][0].pop('startWindowIcon');m['module']['abilities'][0].pop('startWindowIconId')
 assert module==previous,'Any other identity, permission, SDK or platform change is forbidden'
 package=json.loads((C/'mobile/.build/memoh-web/resource-package.json').read_text());checks.verify_hap_rawfiles(z,verify_rawfile_resources(package));assert json.loads(z.read('resources/rawfile/source-build.json'))==snapshot
 for architecture,machine in [('arm64-v8a',183),('x86_64',62)]:
  b=z.read('libs/'+architecture+'/libbegoniaraw.so');assert b[:6]==b'\x7fELF\x02\x01'and int.from_bytes(b[18:20],'little')==machine
 assert z.read('resources/rawfile/raw-codec-license.txt')==(C/'mobile/harmony/app/entry/src/main/resources/rawfile/raw-codec-license.txt').read_bytes()
verify_snapshot(snapshot);target=E/'garnet-assistant-1.0.27-1000027-api26-release-unsigned.hap';assert not target.exists();shutil.copyfile(hap,target)
qualified={**raw,'phase':'complete','qualificationExitCode':0,'originalQualificationFailureRetained':True,'metadataDifference':{'module.virtualMachine':{'old':'ark24.0.0.0','actual':'ark','officialReadOnlyProbe':version,'ABCVersionBytes':list(abi)},'approvedStartupIconChanged':True,'allOtherIdentityPermissionsAndMetadataEqualTo26':True},'noPackageModificationOrRecompile':True};(E/'post-build-qualified.json').write_text(json.dumps(qualified,indent=2)+'\n');print(json.dumps(qualified['metadataDifference']))
