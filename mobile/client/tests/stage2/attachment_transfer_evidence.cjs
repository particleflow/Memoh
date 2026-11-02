// E8 evidence page generator (visual reproducibility for the transfer ring).
// Reads the REAL overlay sources: the ring SVG is extracted verbatim from
// HarmonyTransferRing.vue and driven by the transpiled harmony-transfer-progress.ts;
// the badge/indicator DOM mirrors the component bindings exactly
// (dashoffset = C*(1-fraction), spin class when the fraction is null).
// Usage: node tests/stage2/attachment_transfer_evidence.cjs [output.html]
// Then open the page and drive it from the console:
//   window.__drive(1966080, 3145728, 1572864, 3145728)  // mid-transfer + speed after ~1 s
//   window.__finish()                                    // completed state
//   window.__drive(1, 0, null, null, 'uploading')        // indeterminate native-path ring
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
const root = path.resolve(__dirname, '../..')
const ts = require(process.env.HARMONY_TEST_TYPESCRIPT || path.join(root, 'mobile/.build/memoh-web/checkout/node_modules/typescript'))
const sources = path.join(root, 'mobile/memoh')

const vue = fs.readFileSync(path.join(sources, 'HarmonyTransferRing.vue'), 'utf8')
const ringSvg = vue.slice(vue.indexOf('<svg'), vue.indexOf('</svg>') + 6)

const code = ts.transpileModule(fs.readFileSync(path.join(sources, 'harmony-transfer-progress.ts'), 'utf8'),
  { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const mod = { exports: {} }
vm.runInNewContext(code, {
  exports: mod.exports, module: mod, require: () => ({ shallowReactive: v => v }), console,
  Map, Set, Promise, Error, Date, Math, Number,
})
const progress = mod.exports
if (typeof progress.formatTransferSpeed !== 'function') throw new Error('transpile failed')

const wrapper = `(function () {
  const module = { exports: {} }
  const exports = module.exports
  const require = () => ({ shallowReactive: value => value })
${code}
  window.progress = module.exports
})()`

const page = `<!doctype html><html><head><meta charset="utf8"><title>E8 evidence</title>
<style>
  body { font-family: system-ui, sans-serif; background: #f7f5f2; margin: 0; padding: 24px; color: #1c1917; }
  .row { display: flex; gap: 12px; align-items: flex-start; margin: 18px 0; }
  .assistant { width: 76%; }
  .bubble { background: #ffffff; border: 1px solid #e0d9d0; border-radius: 14px; padding: 10px 14px; max-width: 34rem; }
  .userwrap { justify-content: flex-end; }
  .userwrap .col { display: flex; flex-direction: column; align-items: flex-end; }
  .user { background: #eee7de; border-radius: 14px; padding: 10px 14px; }
  .cards { display: flex; gap: 8px; flex-wrap: wrap; margin: 8px 0; }
  .card { position: relative; width: 118px; height: 118px; border-radius: 10px; border: 1px solid #e0d9d0; background: #fbfaf8; overflow: hidden; display: flex; align-items: center; justify-content: center; color: #78716c; }
  .card.transferring { border-color: rgba(29, 102, 74, .55); }
  .card.failed { border-color: rgba(190, 46, 46, .6); }
  .badge { position: absolute; bottom: 4px; right: 4px; background: #fbfaf8; border-radius: 4px; padding: 2px; color: #1c1917; }
  .badge svg { width: 16px; height: 16px; display: block; }
  .badge.ok { color: #059669; } .badge.fail { color: #dc2626; }
  .indicator { display: inline-flex; align-items: center; gap: 6px; color: #78716c; font-size: 12px; font-variant-numeric: tabular-nums; margin-top: 6px; }
  .indicator svg { width: 18px; height: 18px; }
  .indicator .speed:empty { display: none }
  @keyframes harmony-transfer-ring-rotate { to { transform: rotate(360deg) } }
  .harmony-transfer-ring-spin { animation: harmony-transfer-ring-rotate 0.9s linear infinite; transform-origin: center; }
  .caption { font-size: 12px; color: #a8a29e; margin: 2px 0 0; }
  h2 { font-size: 14px; margin: 26px 0 4px; color: #57534e; }
</style></head><body>
<h2>Bot 附件（下载，流式字节实测）：传输中 — 消息气泡后圆环按进度填满 + 速度</h2>
<div class="row assistant"><div>
  <div class="bubble">这张是刚拍好的样张，附件正在传输。</div>
  <div class="cards"><div class="card transferring" id="dcard"><span style="font-size:11px">IMG_2417.jpg</span><span class="badge" id="dbadge"></span></div></div>
  <div class="indicator" id="dind"></div>
</div></div>
<h2>用户上传（浏览器 XHR 字节）：传输中 + 已完成对比</h2>
<div class="row userwrap"><div class="col">
  <div class="cards">
    <div class="card transferring" id="ucard"><span style="font-size:11px">report.pdf</span><span class="badge" id="ubadge"></span></div>
    <div class="card" id="rcard"><span style="font-size:11px">notes.txt</span><span class="badge ok"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 6 9 17l-5-5"/></svg></span></div>
  </div>
  <div class="user">两份资料发你</div>
  <div class="indicator" id="uind"></div>
</div></div>
<h2>失败态（图标区分，无文字描述）</h2>
<div class="row"><div class="cards">
  <div class="card failed"><span style="font-size:11px">scan_002.jpg</span><span class="badge fail"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><path d="M12 8v4"/><path d="M12 16h.01"/></svg></span></div>
</div></div>
<p class="caption" id="phase">phase: idle</p>
<script>
${wrapper}
</script>
<script>
const RING = ${JSON.stringify(ringSvg)};
const progress = window.progress;
const C = 2 * Math.PI * 8;
function ringMarkup(fraction, rate) {
  // Exactly the component bindings: fill arc when fraction is known, spin arc
  // when it is null; formatted speed after the ring when a rate exists.
  const fill = fraction != null
    ? '<circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-dasharray="' + C.toFixed(3) + '" stroke-dashoffset="' + (C * (1 - fraction)).toFixed(3) + '" transform="rotate(-90 10 10)"></circle>'
    : '<circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-dasharray="' + (C * 0.25).toFixed(3) + ' ' + C.toFixed(3) + '" transform="rotate(-90 10 10)"></circle>';
  const open = RING.slice(0, RING.indexOf('<circle'))
    .replace('class="size-4"', 'class="size-4' + (fraction == null ? ' harmony-transfer-ring-spin' : '') + '"')
  return open + '<circle cx="10" cy="10" r="8" fill="none" stroke="currentColor" stroke-width="2.5" opacity="0.25"></circle>' + fill + '</svg>'
    + '<span class="speed">' + progress.formatTransferSpeed(rate || 0) + '</span>';
}
function paint() {
  const dView = progress.attachmentTransferView(window.__download);
  const dMessage = progress.messageTransferView([window.__download]);
  document.getElementById('dbadge').innerHTML = dView.phase === 'transferring' ? ringMarkup(dView.fraction) : '';
  document.getElementById('dcard').className = 'card' + (dView.phase === 'transferring' ? ' transferring' : '');
  const dind = document.getElementById('dind');
  dind.innerHTML = dMessage.active ? ringMarkup(dMessage.fraction, dMessage.rate) : '';
  const uView = progress.attachmentTransferView(window.__upload);
  const uMessage = progress.messageTransferView([window.__upload]);
  document.getElementById('ubadge').innerHTML = uView.phase === 'transferring' ? ringMarkup(uView.fraction) : '';
  document.getElementById('ucard').className = 'card' + (uView.phase === 'transferring' ? ' transferring' : '');
  const uind = document.getElementById('uind');
  uind.innerHTML = uMessage.active ? ringMarkup(uMessage.fraction, uMessage.rate) : '';
  document.getElementById('phase').textContent = 'phase: ' + dView.phase + ' / ' + uView.phase;
}
window.__download = { content_hash: 'd'.repeat(64), metadata: {} };
window.__upload = { metadata: { harmony_transfer: 'uploading' } };
window.__drive = function (downloadLoaded, downloadTotal, uploadLoaded, uploadTotal, uploadPhase) {
  progress.noteDownloadBytes(window.__download.content_hash, downloadLoaded, downloadTotal);
  window.__upload.metadata.harmony_transfer = uploadPhase || 'uploading';
  if (uploadLoaded != null) progress.noteUploadBytes(window.__upload, uploadLoaded, uploadTotal);
  else progress.clearUploadBytes(window.__upload);
  paint();
};
window.__finish = function () {
  progress.clearDownloadBytes(window.__download.content_hash);
  window.__upload.metadata.harmony_transfer = 'ready';
  progress.clearUploadBytes(window.__upload);
  paint();
};
paint();
</script></body></html>`
const output = path.resolve(process.argv[2] || path.join(root, 'mobile/.build/memoh-web/attachment-transfer-evidence.html'))
fs.mkdirSync(path.dirname(output), { recursive: true })
fs.writeFileSync(output, page)
console.log(JSON.stringify({ evidencePage: output, ringSvgBytes: ringSvg.length,
  drive: [
    'window.__drive(1966080, 3145728, 1572864, 3145728)',
    'window.__drive(2359296, 3145728, 3145728, 3145728)',
    'window.__drive(3145728, 3145728, 3145728, 3145728)',
    'window.__finish()',
    "window.__drive(1, 0, null, null, 'uploading')",
  ] }))
