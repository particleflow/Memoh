// Fixed literals for the existing Web console or ArkWeb controller.runJavaScript.
// No network/recording/playing action. Native caller must retain its existing
// trusted-document/origin and diagnostic access checks. Not a new JS bridge.
export const arm = `(() => { sessionStorage.setItem('memoh.speechTiming','1'); return JSON.stringify({armed:true,monotonicClock:typeof performance.now==='function',cryptoIDs:typeof crypto?.getRandomValues==='function',mseAvailable:typeof MediaSource!=='undefined',acousticRecording:false}); })()`
export const take = `JSON.stringify(window.__memohSpeechTiming?.snapshot() ?? [])`
export const clear = `(() => { sessionStorage.removeItem('memoh.speechTiming'); window.__memohSpeechTiming?.clear(); return 'cleared'; })()`
