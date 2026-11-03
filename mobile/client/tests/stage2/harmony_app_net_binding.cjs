// Actual ArkTS binding sources with mocked Network Kit seams; no device,
// radio, SDK or service is used here. Only public-API call shapes are asserted.
const fs=require('node:fs'),path=require('node:path'),vm=require('node:vm'),assert=require('node:assert/strict')
const root=path.resolve(__dirname,'../..'),ts=require(process.env.HARMONY_TEST_TYPESCRIPT||path.join(root,'mobile/.build/memoh-web/checkout/node_modules/typescript'))
function load(api,diag){
 const m={exports:{}},file=path.join(root,'mobile/harmony/app/entry/src/main/ets/common/AppNetBinding.ets')
 const output=ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.CommonJS}}).outputText
 const require_=id=>{if(id==='@kit.NetworkKit')return{connection:api};if(id==='./ClientDiagnostics')return diag;return{}}
 vm.runInNewContext(output,{require:require_,module:m,exports:m.exports,Map,Set,Promise,JSON,Number,String,Array,Error,setTimeout,clearTimeout},{filename:file})
 return m.exports}
function world(){
 const events=[],nets=[],bindings=[],connections=[]
 let setAppNetError=false,probeError=false,stalled=false,registerReject=null
 const diag={ClientDiagnostics:{event:(area,component,phase,detail)=>events.push({area,component,phase,detail})}}
 const api={
  NetBearType:{BEARER_CELLULAR:0,BEARER_WIFI:1},
  getAllNets:async()=>{if(stalled){return new Promise(()=>{})}if(probeError){probeError=false;throw Error('synthetic SDK secret')}return nets.map(net=>({netId:net.netId}))},
  getNetCapabilities:async handle=>{const net=nets.find(item=>item.netId===handle.netId);if(!net)throw Error('net gone');return{bearerTypes:net.bearers.slice()}},
  getConnectionProperties:async handle=>{const net=nets.find(item=>item.netId===handle.netId);if(!net)throw Error('net gone');return net.properties},
  setAppNet:async handle=>{if(setAppNetError){setAppNetError=false;throw Error('controlled setAppNet failure')}bindings.push(handle.netId)},
  getAppNetSync:()=>({netId:0}),
  createNetConnection:specifier=>{
   const made={specifier,handlers:{},registered:0,unregistered:0,
    on(type,cb){(made.handlers[type]=made.handlers[type]||[]).push(cb)},
    register(cb){made.registered++;cb&&cb(registerReject&&registerReject(made)?{code:2100002}:undefined)},
    unregister(cb){made.unregistered++;cb&&cb()}}
   connections.push(made);return made}}
 const add=(netId,bearers,ipv6)=>nets.push({netId,bearers,properties:{isIPv6LinkValid:ipv6}})
 const fire=(conn,type,data)=>(conn.handlers[type]||[]).forEach(cb=>cb(data))
 const plain=value=>JSON.parse(JSON.stringify(value))
 const snapshot=mod=>JSON.parse(mod.AppNetBinding.dump()[0].slice('GarnetAppNet:'.length))
 const flush=async()=>{for(let i=0;i<4;i++)await new Promise(resolve=>setTimeout(resolve,0))}
 return{events,bindings,connections,diag,api,add,fire,snapshot,flush,
  failNextSetAppNet(){setAppNetError=true},failNextProbe(){probeError=true},
  stall(){stalled=true},
  failRegister(predicate){registerReject=predicate},allowRegister(){registerReject=null},
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

 // First-load gate: whenSettled resolves only after the first evaluation has
 // applied its decision, never later than its timeout when the probe stalls.
 const gate=world(),gateMod=load(gate.api,gate.diag)
 gate.add(101,[1],true)
 let gateOpened=false
 void gateMod.AppNetBinding.whenSettled(5000).then(()=>{gateOpened=true})
 gateMod.AppNetBinding.start()
 assert.equal(gateOpened,false)
 await gate.flush()
 assert.equal(gateOpened,true)
 let gateAgain=false
 void gateMod.AppNetBinding.whenSettled(5000).then(()=>{gateAgain=true})
 await new Promise(resolve=>setTimeout(resolve,0))
 assert.equal(gateAgain,true)
 const stalled=world(),stalledMod=load(stalled.api,stalled.diag)
 stalled.stall()
 let gateEscaped=false
 void stalledMod.AppNetBinding.whenSettled(10).then(()=>{gateEscaped=true})
 stalledMod.AppNetBinding.start()
 await new Promise(resolve=>setTimeout(resolve,30))
 assert.equal(gateEscaped,true)

 // Rebound notification: binding a network different from the previously
 // bound one notifies listeners exactly once; re-evaluations that keep the
 // same binding and plain unbinds never re-notify.
 const rebound=world(),reboundMod=load(rebound.api,rebound.diag)
 const rebinds=[]
 reboundMod.AppNetBinding.onRebound(netId=>rebinds.push(netId))
 rebound.add(101,[1],false)
 reboundMod.AppNetBinding.start();await rebound.flush()
 assert.deepEqual(rebinds,[])
 rebound.add(102,[0],true)
 rebound.fire(rebound.connections[1],'netAvailable',{netId:102});await rebound.flush()
 assert.deepEqual(rebinds,[102])
 rebound.fire(rebound.connections[0],'netCapabilitiesChange',{netHandle:{netId:102},netCap:{bearerTypes:[0]}});await rebound.flush()
 assert.deepEqual(rebinds,[102])
 reboundMod.AppNetBinding.stop();await rebound.flush()
 assert.deepEqual(rebinds,[102])
 const dropped=[]
 const lateListener=netId=>dropped.push(netId)
 const late=world(),lateMod=load(late.api,late.diag)
 late.add(101,[1],false)
 lateMod.AppNetBinding.start();await late.flush()
 lateMod.AppNetBinding.onRebound(lateListener)
 lateMod.AppNetBinding.offRebound(lateListener)
 late.add(102,[0],true)
 late.fire(late.connections[1],'netAvailable',{netId:102});await late.flush()
 assert.deepEqual(late.bindings,[102])
 assert.deepEqual(dropped,[])

 // Registration failures reach the journal instead of vanishing; a refused
 // cellular activation request is not held, so a later evaluation retries it.
 const refused=world(),refusedMod=load(refused.api,refused.diag)
 refused.failRegister(conn=>!!conn.specifier)
 refused.add(101,[1],false)
 refusedMod.AppNetBinding.start();await refused.flush()
 assert.ok(refused.events.some(event=>event.phase==='cellular-register-failed'&&event.detail.includes('errorCode=2100002')))
 assert.equal(refused.connections.filter(conn=>conn.specifier).length,1)
 refused.allowRegister()
 refused.fire(refused.connections[0],'netCapabilitiesChange',{netHandle:{netId:101},netCap:{bearerTypes:[1]}});await refused.flush()
 assert.ok(refused.events.every(event=>!JSON.stringify(event).includes('2100002')||event.phase==='cellular-register-failed'))
 assert.equal(refused.connections.filter(conn=>conn.specifier).length,2)
 const blind=world(),blindMod=load(blind.api,blind.diag)
 blind.failRegister(conn=>!conn.specifier)
 blind.add(101,[1],true)
 blindMod.AppNetBinding.start();await blind.flush()
 assert.ok(blind.events.some(event=>event.phase==='register-failed'&&event.detail.includes('errorCode=2100002')))
 assert.deepEqual(blind.bindings,[101])

 // Foreground refresh re-evaluates without waiting for a system network event.
 const foreground=world(),foregroundMod=load(foreground.api,foreground.diag)
 foreground.add(101,[1],true)
 foregroundMod.AppNetBinding.start();await foreground.flush()
 foreground.drop(101)
 foregroundMod.AppNetBinding.refresh();await foreground.flush()
 assert.deepEqual(foreground.bindings,[101,0])
 assert.equal(foreground.snapshot(foregroundMod).reason,'no-network')

 // Regression floor for the field answer "pure cellular works": a
 // cellular-only world binds that network during the first evaluation (so the
 // settle gate opens already bound), holds no extra activation request, and a
 // foreground refresh neither rebinds nor detours from it.
 const solo=world(),soloMod=load(solo.api,solo.diag)
 solo.add(102,[0],true)
 let soloOpened=false
 void soloMod.AppNetBinding.whenSettled(5000).then(()=>{soloOpened=true})
 soloMod.AppNetBinding.start();await solo.flush()
 assert.ok(soloOpened)
 assert.deepEqual(solo.bindings,[102])
 assert.equal(solo.snapshot(soloMod).reason,'cellular-ipv6')
 assert.equal(solo.connections.length,1)
 soloMod.AppNetBinding.refresh();await solo.flush()
 assert.deepEqual(solo.bindings,[102])
 soloMod.AppNetBinding.stop();await solo.flush()
 assert.deepEqual(solo.bindings,[102,0])

 // The ability lifecycle wires start/stop, the settle-gated first page load,
 // the foreground refresh and the read-only dump surface; the chat page joins
 // the rebound notification so a first connection that lost the binding race
 // retries once when the binding lands.
 const ability=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/entryability/EntryAbility.ets'),'utf8')
 assert.match(ability,/AppNetBinding\.start\(\)/)
 assert.match(ability,/AppNetBinding\.stop\(\)/)
 assert.match(ability,/AppNetBinding\.dump\(\)/)
 assert.match(ability,/AppNetBinding\.whenSettled\(/)
 assert.match(ability,/AppNetBinding\.refresh\(\)/)
 const page=fs.readFileSync(path.join(root,'mobile/harmony/app/entry/src/main/ets/pages/SharedChat.ets'),'utf8')
 assert.match(page,/AppNetBinding\.onRebound\(/)
 assert.match(page,/AppNetBinding\.offRebound\(/)
 console.log(JSON.stringify({decisionTable:true,wifiIpv6Preferred:true,cellularActivationByRequest:true,boundNetLostReleases:true,ipv6ReturnRebindsWifi:true,cellularRequestReleasedWithWifiIpv6:true,stopUnbindsAndUnregisters:true,bindFailureRetried:true,sdkErrorsNotJournaled:true,firstLoadGatedOnSettle:true,reboundNotifiedOnce:true,registerFailuresJournaled:true,foregroundRefresh:true,cellularOnlyFloor:true,device:false}))
})().catch(error=>{console.error(error);process.exitCode=1})
