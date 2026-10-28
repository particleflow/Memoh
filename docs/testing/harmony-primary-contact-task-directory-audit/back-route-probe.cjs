// Source-only probe: empty synthetic DOM, no API, device or persistent storage.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict')
const path=require('node:path'),{createRequire}=require('node:module')
const root=process.env.HARMONY_SOURCE_ROOT||process.cwd(),context=process.env.HARMONY_GENERATED_CONTEXT
if(!context)throw new Error('Set HARMONY_GENERATED_CONTEXT to the existing generated checkout')
const ts=createRequire(path.join(context,'apps/web/package.json'))('typescript')
const source=fs.readFileSync(path.join(root,'mobile/client/mobile/memoh/harmony-back.ts'),'utf8')
const js=ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2023,module:ts.ModuleKind.CommonJS}}).outputText
const sandbox={exports:{},require:()=>({isKeyboardElementVisible:()=>true}),document:{querySelectorAll:()=>[],querySelector:()=>null,getElementById:()=>null},window:{}}
vm.runInNewContext(js,sandbox)
const cases=[]
for(const query of [{},{session:'original-task-session'}]){
 const calls=[],router={currentRoute:{value:{name:'bot',path:'/bot/original-bot',query}},replace:x=>calls.push(x)}
 const consumed=sandbox.exports.handleHarmonyBack(router)
 cases.push({query,consumed,routeCalls:calls})
 assert.equal(consumed,false);assert.equal(calls.length,0)
}
console.log(JSON.stringify({synthetic:true,scope:'no modal/header back action; direct task deep link',observed:cases,conclusion:'current native back does not distinguish task-session URL from plain Bot root; actual ArkWeb behavior not tested'},null,2))
