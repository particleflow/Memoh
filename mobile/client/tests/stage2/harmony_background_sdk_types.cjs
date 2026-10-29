// Declaration-surface check against pinned official .d.ts text. Not ArkTS/SDK compilation.
const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),os=require('node:os'),crypto=require('node:crypto')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const refs=process.env.HARMONY_BACKGROUND_OFFICIAL_TYPES
assert.ok(refs,'Set HARMONY_BACKGROUND_OFFICIAL_TYPES to the pinned reference-only declarations; no SDK is downloaded by this test')
const manifest=JSON.parse(fs.readFileSync(path.resolve(root,'../../docs/testing/harmony-transfer-background/official-types.json')))
for(const [file,hash] of Object.entries(manifest.files)) assert.equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(refs,file))).digest('hex'),hash,file)
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'harmony-background-types-'))
try{
const source=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/common/NativeTransferBackground.ets'),'utf8')
fs.writeFileSync(path.join(temp,'kits.d.ts'),`
type int=number;type long=number;
declare module '@kit.AbilityKit' {
 import wantAgent,{WantAgent} from '@ohos.app.ability.wantAgent';
 export {wantAgent,WantAgent};
 export namespace common {type UIAbilityContext=import('@official/UIAbilityContext').default;}
}
declare module '@kit.BackgroundTasksKit' {export {default as backgroundTaskManager} from '@ohos.resourceschedule.backgroundTaskManager';}
declare module '@kit.NotificationKit' {export {default as notificationManager} from '@ohos.notificationManager';}
declare module '@kit.BasicServicesKit' {export {BusinessError} from '@ohos.base';}
`)
fs.writeFileSync(path.join(temp,'ClientDiagnostics.ts'),'export class ClientDiagnostics { static failure(_area:string,_component:string,_phase:string,_error:Error):void{} }')
const entry=path.join(temp,'NativeTransferBackground.ts'),opts={target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS,moduleResolution:ts.ModuleResolutionKind.Node10,strict:true,noEmit:true,skipLibCheck:true,ignoreDeprecations:'6.0',baseUrl:refs,paths:{'@ohos.*':['./@ohos.*'],'@official/UIAbilityContext':['./application/UIAbilityContext.d.ts']}}
function check(input){fs.writeFileSync(entry,input);const program=ts.createProgram([entry,path.join(temp,'kits.d.ts')],opts);return ts.getPreEmitDiagnostics(program)}
const errors=check(source);if(errors.length){console.error(ts.formatDiagnosticsWithColorAndContext(errors,{getCanonicalFileName:f=>f,getCurrentDirectory:()=>temp,getNewLine:()=> '\n'}));assert.fail('official declaration surface did not typecheck')}
const red=[]
for(const [label,a,b] of [['invalid modes',"['dataTransfer']",'[123]'],['invalid notification id','id: lease.notificationId','id: "wrong"'],['invalid callback task id','this.canceledDuringStart.add(info.id)','this.canceledDuringStart.add(info.reason.toString())'],['invalid WantAgent request code','requestCode: 0','requestCode: "wrong"']]){assert.ok(source.includes(a),label);const diagnostics=check(source.replace(a,b));assert.ok(diagnostics.length>0,label+' was not checked');red.push({label,rejected:true,codes:[...new Set(diagnostics.map(d=>d.code))]})}
const files=['@ohos.resourceschedule.backgroundTaskManager.d.ts','@ohos.notificationManager.d.ts','notification/notificationRequest.d.ts','notification/notificationContent.d.ts','notification/notificationTemplate.d.ts','@ohos.app.ability.wantAgent.d.ts','wantAgent/wantAgentInfo.d.ts','application/UIAbilityContext.d.ts','application/Context.d.ts','application/BaseContext.d.ts','bundleManager/AbilityInfo.d.ts','@ohos.app.ability.Want.d.ts','@ohos.app.ability.common.d.ts','@ohos.base.d.ts','ability/want.d.ts']
const hashes=Object.fromEntries(files.map(f=>[f,crypto.createHash('sha256').update(fs.readFileSync(path.join(refs,f))).digest('hex')]))
console.log(JSON.stringify({passed:true,officialCommit:'8a86234f0d542e5062e24e948652f5a484d90904',referenceOnly:true,officialDeclarationSurface:true,skipLibCheck:true,transitiveSDKDeclarationsComplete:false,ArkTSCompiler:false,red,sha256:hashes}))
}finally{fs.rmSync(temp,{recursive:true,force:true})}
