// Isolated native decoder probe. No HTTP listener, application service, provider,
// paid synthesis, autoplay override or fabricated audible-output measurement.
import { build } from '/workspace/Memoh/node_modules/.pnpm/node_modules/esbuild/lib/main.js'
import { chromium } from '/opt/codex/cua_node/lib/node_modules/playwright/index.mjs'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import assert from 'node:assert/strict'
const root = process.cwd()
const out = process.env.SPEECH_FIXTURE_OUT ?? '/tmp/nas-speech-native'
fs.mkdirSync(out, { recursive: true })
const mp3 = path.join(out, 'sine.mp3')
execFileSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=44100:duration=2', '-c:a', 'libmp3lame', '-b:a', '128k', '-write_xing', '0', mp3])
// markstream-vue re-exports this exact parser. Resolve the re-export directly
// for this isolated probe so unrelated optional diagram/chemistry workers are not bundled.
const parser = createRequire(fs.realpathSync(path.join(root, 'apps/web/node_modules/markstream-vue/package.json'))).resolve('stream-markdown-parser')
const bundle = await build({ entryPoints: [path.join(root, 'apps/web/src/pages/home/composables/chat-read-aloud.ts')], bundle: true, write: false, format: 'iife', globalName: 'SpeechProbe', platform: 'browser', alias: { '@': path.join(root, 'apps/web/src'), 'markstream-vue': parser } })
const browser = await chromium.launch({ executablePath: '/usr/bin/chromium', headless: true, args: ['--no-sandbox'] })
try {
  const page = await browser.newPage()
  page.on('pageerror', error => console.error('pageerror', error.message))
  page.on('console', event => { if (event.type() === 'error') console.error('console', event.text()) })
  await page.setContent('<button id="manual">Manual fixture</button><button id="automatic">Live reply fixture</button><p id="state"></p>')
  await page.addScriptTag({ content: bundle.outputFiles[0].text })
  const bytes = [...fs.readFileSync(mp3)]
  await page.evaluate(({ bytes }) => {
    window.probe = { states: [], calls: [], metrics: [], eof: false, nonzero: false }
    const player = new Audio()
    let context, analyser, node, controller, start, mode, feedTimer, audioFeed
    let pumpGeneration = 0
    function measure() {
      if (!analyser) return
      const samples = new Float32Array(analyser.fftSize)
      analyser.getFloatTimeDomainData(samples)
      if (samples.some(sample => Math.abs(sample) > 0.001) && !window.probe.nonzero) {
        window.probe.nonzero = true
        window.probe.metrics.push({ kind: 'firstNonzeroDecodedSamples', ms: performance.now() - start, beforeEOF: !window.probe.eof, mode, contextBaseLatencyMS: context.baseLatency * 1000, contextOutputLatencyMS: context.outputLatency * 1000 })
      }
      requestAnimationFrame(measure)
    }
    controller = new SpeechProbe.ChatReadAloud({
      async synthesize(text, signal) {
        window.probe.calls.push({ text, ms: performance.now() - start, mode })
        let index = 0
        const generation = ++pumpGeneration
        return { contentType: 'audio/mpeg', stream: new ReadableStream({
          start(feed) {
            audioFeed = feed
            function push() {
              if (signal.aborted || generation !== pumpGeneration) return
              if (index >= bytes.length) { window.probe.eof = true; feed.close(); return }
              const end = Math.min(index + 4096, bytes.length)
              feed.enqueue(new Uint8Array(bytes.slice(index, end)))
              if (!index) window.probe.metrics.push({ kind: 'firstFixtureBytes', ms: performance.now() - start, mode })
              index = end
              feedTimer = setTimeout(push, 180)
            }
            feedTimer = setTimeout(push, 40)
          },
          cancel() { clearTimeout(feedTimer); window.probe.metrics.push({ kind: 'streamCanceled', mode }) },
        }) }
      },
      createPlayer: () => player,
      createURL: source => URL.createObjectURL(source),
      revokeURL: url => URL.revokeObjectURL(url),
      onState(state) { window.probe.states.push({ ...state, ms: performance.now() - start, mode }); document.querySelector('#state').textContent = state.status },
    })
    async function run(nextMode) {
      controller.stop(); window.probe.nonzero = false; window.probe.eof = false
      mode = nextMode; start = performance.now()
      context ??= new AudioContext()
      if (!analyser) { node = context.createMediaElementSource(player); analyser = context.createAnalyser(); node.connect(analyser); analyser.connect(context.destination); requestAnimationFrame(measure) }
      await context.resume()
      if (mode === 'manual') controller.replay({ key: 'manual', text: '手动朗读显示正文。' })
      else {
        const live = new SpeechProbe.LiveSpeechReplies()
        const message = { id: 'live', turnId: 'turn', runtimeRunId: 'run', role: 'assistant', timestamp: '', streaming: true, messages: [{ id: 0, type: 'text', content: '自动第一句。请看[文档](https://hidden.test/secret)' }] }
        for (const entry of live.update([message], true, false)) controller.enqueue(entry)
      }
    }
    document.querySelector('#manual').onclick = () => run('manual')
    document.querySelector('#automatic').onclick = () => run('automatic')
    window.stopProbe = () => { controller.stop(); ++pumpGeneration; clearTimeout(feedTimer) }
  }, { bytes })
  for (const mode of ['manual', 'automatic']) {
    await page.click(`#${mode}`)
    try { await page.waitForFunction(mode => window.probe.metrics.some(item => item.kind === 'firstNonzeroDecodedSamples' && item.mode === mode), mode, { timeout: 5000 }) } catch (error) { const debug = await page.evaluate(() => window.probe); fs.writeFileSync(path.join(out, 'native-failed.json'), JSON.stringify(debug, null, 2)); console.error(JSON.stringify(debug)); throw error }
    await page.evaluate(() => window.stopProbe())
    await page.waitForTimeout(150)
  }
  const result = await page.evaluate(() => window.probe)
  const observed = result.metrics.filter(item => item.kind === 'firstNonzeroDecodedSamples')
  assert.equal(observed.length, 2)
  assert(observed.every(item => item.beforeEOF))
  assert(result.calls.some(item => item.mode === 'automatic' && item.text === '自动第一句。'))
  assert(result.calls.every(item => !item.text.includes('hidden.test')))
  result.acceptance = { providerCalls: 0, applicationServiceStarted: false, nativeDecoderBeforeEOF: true, physicalAudibleMeasurement: false, realMagpie500msPassed: false, note: 'Analyser samples prove decoder/output-graph activity only, not physical speaker onset. Fixture timing is not provider timing.' }
  fs.writeFileSync(path.join(out, 'native-result.json'), JSON.stringify(result, null, 2))
  await page.screenshot({ path: path.join(out, 'isolated-native-fixture.png') })
  console.log(JSON.stringify({ observed, acceptance: result.acceptance, out }, null, 2))
} finally { await browser.close() }
