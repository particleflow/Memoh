const fs=require('fs'),vm=require('vm'),assert=require('node:assert/strict'),path=require('path')
const ts=require(process.env.HARMONY_TYPESCRIPT || '../../../mobile/client/tests/.isolated/ci-web/checkout/node_modules/typescript')
const calls=[]
class Request{constructor(url,method,headers,body){Object.assign(this,{url,method,headers,body})}}
const imports={
 '@hms.collaboration.rcp':{Request},
 '@ohos.web.webview':{WebviewController:{setHostIP:(...args)=>calls.push(['set',...args]),clearHostIP:(...args)=>calls.push(['clear',...args])}},
 '@ohos.net.connection':{getDefaultNet:async()=>({getAddressesByName:async host=>{calls.push(['resolve',host]);return [{address:'192.0.2.8',family:1},{address:'2001:db8::8',family:2}]}})},
}
const moduleProbe={exports:{}}
vm.runInNewContext(ts.transpileModule(fs.readFileSync(path.join(__dirname,'probe.ts'),'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText,{exports:moduleProbe.exports,require:n=>imports[n],URL,Error,Number})
;(async()=>{
 const api=moduleProbe.exports,body=new ArrayBuffer(4),origin='https://nas.example:18443/api/bots/original/media/uploads'
 const request=api.pinnedRequest(origin,'192.0.2.8','Bearer synthetic-only',body)
 assert.equal(request.url,origin);assert.equal(request.method,'POST');assert.equal(request.body,body)
 assert.deepEqual(JSON.parse(JSON.stringify(request.headers)),{authorization:'Bearer synthetic-only'})
 assert.equal(request.configuration.security.remoteValidation,'system');assert.equal(request.configuration.transfer.autoRedirect,false)
 const rule=request.configuration.dns.dnsRules[0];assert.equal(rule.host,'nas.example');assert.equal(rule.port,18443);assert.deepEqual(Array.from(rule.ipAddresses),['192.0.2.8'])
 assert.equal(request.configuration.dns.happyEyeballOnDnsRule,false);assert.equal(request.configuration.proxy,undefined)
 for(const bad of ['http://nas.example/api','https://[2001:db8::1]/api','https://192.0.2.8/api','https://user:pass@nas.example/api'])assert.throws(()=>api.pinnedRequest(bad,'192.0.2.8','',body))
 assert.equal(api.connectFailureCandidate(1007900007,true),true);assert.equal(api.connectFailureCandidate(1007900007,false),false)
 for(const code of [1007900028,1007900035,1007900052,1007900055,1007900056,1007900060,401,403,500])assert.equal(api.connectFailureCandidate(code,true),false)
 assert.deepEqual(Array.from(await api.originalV6('nas.example')),['2001:db8::8'])
 api.hostCacheProbe('nas.example','192.0.2.8')
 assert.deepEqual(calls,[['resolve','nas.example'],['set','nas.example','192.0.2.8',1],['clear','nas.example']])
 console.log(JSON.stringify({passed:true,scope:'Isolated API-object/typed-error probe with Node Kit seams; no native transport, TLS handshake, HTTP request or WebView engine executed',canonicalURLUnchanged:true,originalPortRetained:true,headersAndBodyUnchanged:true,systemValidation:true,redirectsDisabled:true,proxyNotOverridden:true,onlyTypedConnectFailureCandidate:true,timeoutTLSHTTPUnknownNoFallback:true,literalAuthorityNotAssumedSupported:true,IPv6FilteredFromOriginalHost:true,webviewHostCacheOnly:true,productWired:false}))
})().catch(e=>{console.error(e);process.exitCode=1})
