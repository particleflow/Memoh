// Actual ArkTS binding sources with mocked Network Kit seams; no device,
// radio, SDK or service is used here. Only public-API call shapes are asserted.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(api,diag){
 const m={exports:{}},file=path.join(root,'mobile/harmony/app/entry/src/main/ets/common/AppNetBinding.ets')
 const output=ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
 const require_=id=>{if(id==='@kit.NetworkKit')return{connection:api};if(id==='./ClientDiagnostics')return diag;return{}}
 vm.runInNewContext(output,{require:require_,module:m,exports:m.exports,Map,Set,Promise,JSON,Number,String,Array,Error},{filename:file})
 return m.exports}
function world(){
 const events=[],nets=[],bindings=[],connections=[]
 let setAppNetError=false,probeError=false
 const diag={ClientDiagnostics:{event:(area,component,phase,detail)=>events.push({area,component,phase,detail})}}
 const api={
  NetBearType:{BEARER_CELLULAR:0,BEARER_WIFI:1},
  getAllNets:async()=>{if(probeError){probeError=false;throw Error('synthetic SDK secret')}return nets.map(net=>({netId:net.netId}))},
  getNetCapabilities:async handle=>{const net=nets.find(item=>item.netId===handle.netId);if(!net)throw Error('net gone');return{bearerTypes:net.bearers.slice()}},
  getConnectionProperties:async handle=>{const net=nets.find(item=>item.netId===handle.netId);if(!net)throw Error('net gone');return net.properties},
  setAppNet:async handle=>{if(setAppNetError){setAppNetError=false;throw Error('controlled setAppNet failure')}bindings.push(handle.netId)},
  getAppNetSync:()=>({netId:0}),
  createNetConnection:specifier=>{
   const made={specifier,handlers:{},registered:0,unregistered:0,
    on(type,cb){(made.handlers[type]=made.handlers[type]||[]).push(cb)},
    register(cb){made.registered++;cb&&cb()},
    unregister(cb){made.unregistered++;cb&&cb()}}
   connections.push(made);return made}}
 const add=(netId,bearers,ipv6)=>nets.push({netId,bearers,properties:{isIPv6LinkValid:ipv6}})
 const fire=(conn,type,data)=>(conn.handlers[type]||[]).forEach(cb=>cb(data))
 const plain=value=>JSON.parse(JSON.stringify(value))
 const snapshot=mod=>JSON.parse(mod.AppNetBinding.dump()[0].slice('GarnetAppNet:'.length))
 const flush=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0))}
 return{events,bindings,connections,diag,api,add,fire,snapshot,flush,
  failNextSetAppNet(){setAppNetError=true},failNextProbe(){probeError=true},
  setIpv6(netId,ipv6){nets.find(item=>item.netId===netId).properties={isIPv6LinkValid:ipv6}},
  drop(netId){const at=nets.findIndex(item=>item.netId===netId);nets.splice(at,1)}}
}
;(async()=>{
 // Pure decision table: Wi-Fi IPv6 wins, cellular IPv6 follows, no IPv6 or no
 // network keeps the system default, foreign bearers are never bound.
 const pure=load(world().api,world().diag)
 const plain=value=>JSON.parse(JSON.stringify(value))
 const fact=(netId,wifi,cellular,ipv6)=>({netId,wifi,cellular,ipv6})
 assert.deepEqual(plain(pure.decideAppNet([fact(101,true,false,true),fact(102,false,true,true)])),{action:'bind',netId:101,reason:'wifi-ipv6'})
 assert.deepEqual(plain(pure.decideAppNet([fact(101,true,false,false),fact(102,false,true,true)])),{action:'bind',netId:102,reason:'cellular-ipv6'})
 assert.deepEqual(plain(pure.decideAppNet([fact(101,true,false,false),fact(102,false,true,false)])),{action:'default',netId:0,reason:'no-ipv6-network'})
 assert.deepEqual(plain(pure.decideAppNet([])),{action:'default',netId:0,reason:'no-network'})
 assert.deepEqual(plain(pure.decideAppNet([fact(103,false,true,true)])),{action:'bind',netId:103,reason:'cellular-ipv6'})
 assert.deepEqual(plain(pure.decideAppNet([fact(101,true,false,false),fact(110,true,false,true)])),{action:'bind',netId:110,reason:'wifi-ipv6'})
 assert.deepEqual(plain(pure.decideAppNet([fact(104,false,false,true)])),{action:'default',netId:0,reason:'no-ipv6-network'})
 assert.equal(pure.needCellularRequest([fact(101,true,false,false)]),true)
 assert.equal(pure.needCellularRequest([fact(101,true,false,true)]),false)
 assert.equal(pure.needCellularRequest([fact(103,false,true,false)]),false)
 assert.equal(pure.needCellularRequest([]),false)

 // Wi-Fi with IPv6 binds directly; losing it releases to the system default.
 const wifi=world(),wifiMod=load(wifi.api,wifi.diag)
 wifi.add(101,[1],true)
 wifiMod.AppNetBinding.start();await wifi.flush()
 assert.deepEqual(wifi.bindings,[101])
 assert.equal(wifi.connections.length,1)
 assert.deepEqual(wifi.snapshot(wifiMod),{version:1,started:true,boundNetId:101,action:'bind',reason:'wifi-ipv6',nets:[{netId:101,wifi:true,cellular:false,ipv6:true}]})
 wifi.drop(101);wifi.fire(wifi.connections[0],'netLost',{netId:101});await wifi.flush()
 assert.deepEqual(wifi.bindings,[101,0])
 assert.deepEqual(wifi.snapshot(wifiMod),{version:1,started:true,boundNetId:0,action:'default',reason:'no-network',nets:[]})
 wifiMod.AppNetBinding.stop()
 assert.equal(wifi.connections[0].unregistered,1)
 assert.deepEqual(wifi.bindings,[101,0])
 assert.ok(wifi.events.every(event=>event.area==='NETWORK'&&event.component==='app-net'))
 assert.ok(wifi.events.some(event=>event.phase==='decision'&&event.detail.includes('action=bind/net=101/reason=wifi-ipv6')))

 // Wi-Fi without IPv6: cellular is activated by request, bound when it comes
 // up, released when Wi-Fi regains IPv6; a total IPv6 loss returns to default.
 const mixed=world(),mixedMod=load(mixed.api,mixed.diag)
 mixed.add(101,[1],false)
 mixedMod.AppNetBinding.start();await mixed.flush()
 assert.deepEqual(mixed.bindings,[])
 assert.equal(mixed.connections.length,2)
 const cellular=mixed.connections[1]
 assert.deepEqual(JSON.parse(JSON.stringify(cellular.specifier)),{netCapabilities:{bearerTypes:[0]}})
 assert.equal(cellular.registered,1)
 mixed.add(102,[0],true)
 mixed.fire(cellular,'netAvailable',{netId:102});await mixed.flush()
 assert.deepEqual(mixed.bindings,[102])
 assert.deepEqual(mixed.snapshot(mixedMod).nets,[{netId:101,wifi:true,cellular:false,ipv6:false},{netId:102,wifi:false,cellular:true,ipv6:true}])
 assert.ok(mixed.events.some(event=>event.phase==='decision'&&event.detail.includes('action=bind/net=102/reason=cellular-ipv6')))
 mixed.setIpv6(102,false)
 mixed.fire(mixed.connections[0],'netConnectionPropertiesChange',{netHandle:{netId:102},connectionProperties:{}});await mixed.flush()
 assert.deepEqual(mixed.bindings,[102,0])
 assert.equal(mixed.snapshot(mixedMod).action,'default')
 assert.equal(mixed.snapshot(mixedMod).reason,'no-ipv6-network')
 assert.equal(cellular.unregistered,0)
 mixed.setIpv6(101,true)
 mixed.fire(mixed.connections[0],'netCapabilitiesChange',{netHandle:{netId:101},netCap:{bearerTypes:[1]}});await mixed.flush()
 assert.deepEqual(mixed.bindings,[102,0,101])
 assert.equal(cellular.unregistered,1)
 assert.deepEqual(mixed.snapshot(mixedMod),{version:1,started:true,boundNetId:101,action:'bind',reason:'wifi-ipv6',nets:[{netId:101,wifi:true,cellular:false,ipv6:true},{netId:102,wifi:false,cellular:true,ipv6:false}]})
 mixedMod.AppNetBinding.stop()
 assert.deepEqual(mixed.bindings,[102,0,101,0])
 assert.equal(mixed.connections[0].unregistered,1)

 // A failed bind keeps the app on its previous path and retries on the next
 // network event; SDK error text never reaches the journal.
 const flaky=world(),flakyMod=load(flaky.api,flaky.diag)
 flaky.add(101,[1],true)
 flaky.failNextSetAppNet()
 flakyMod.AppNetBinding.start();await flaky.flush()
 assert.deepEqual(flaky.bindings,[])
 assert.ok(flaky.events.some(event=>event.phase==='bind-failed'&&event.detail.includes('net=101')))
 flaky.fire(flaky.connections[0],'netCapabilitiesChange',{netHandle:{netId:101},netCap:{bearerTypes:[1]}});await flaky.flush()
 assert.deepEqual(flaky.bindings,[101])
 flaky.failNextProbe()
 flaky.fire(flaky.connections[0],'netCapabilitiesChange',{netHandle:{netId:101},netCap:{bearerTypes:[1]}});await flaky.flush()
 assert.deepEqual(flaky.bindings,[101])
 assert.ok(flaky.events.some(event=>event.phase==='evaluate-failed'))
 assert.ok(!JSON.stringify(flaky.events).includes('synthetic SDK secret'))

 // The ability lifecycle wires start/stop and the read-only dump surface.
 const ability=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/entryability/EntryAbility.ets'),'utf8')
 assert.match(ability,/AppNetBinding\.start\(\)/)
 assert.match(ability,/AppNetBinding\.stop\(\)/)
 assert.match(ability,/AppNetBinding\.dump\(\)/)
 console.log(JSON.stringify({decisionTable:true,wifiIpv6Preferred:true,cellularActivationByRequest:true,boundNetLostReleases:true,ipv6ReturnRebindsWifi:true,cellularRequestReleasedWithWifiIpv6:true,stopUnbindsAndUnregisters:true,bindFailureRetried:true,sdkErrorsNotJournaled:true,device:false}))
})().catch(error=>{console.error(error);process.exitCode=1})
