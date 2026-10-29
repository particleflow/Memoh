# Alibaba WSS → playback HTTP → native player seam

This is an evidence-only increment on `fab04e2c5ab8da6f36daf23549962b0277dab8a8`. It changes no production source, provider configuration, credentials, voice, UI controls, or deployment. The new test requires the streaming service/handler/player from `2b9c8fc24ff55bbaaa88cc112438006d82fe0513`; the executed composable also includes rendered-body `23fd2e5839423e8ca269c70d777a7a58a5e8fec9` and replay-gesture `fab04e2c5`. Those candidates retain their independent admission boundaries. Current cleanup references were read as Web `9690095b4a6575af9f63915b05e713de38c55bf6` and HAP `afa60196940d1e21bbf514d401c763a79f90f9d0`; this increment does not claim to execute an unknown unified build or HAP.

## New evidence

One opt-in race-enabled Go integration test runs three controlled cases, without rerunning the previous 7/14/19 suites:

1. **Original collector control:** real `audio.Service.Synthesize` → pinned Alibaba SDK `GenerateSpeech` receives 40 real MP3 frames. It has no result before the fixture releases the remaining frames and `task-finished`. The completed bytes equal the fixture.
2. **Streaming completion:** real registry → Alibaba SDK `StreamSpeech` → `BotAudioHandler.Playback` → loopback HTTP → actual generated SDK with `parseAs: stream` → actual `useChatReadAloud`/controller/MSE → Chromium native Audio. The client has 8,403 bytes, native `playing`, and nonzero decoded graph samples while the WSS fixture is still withholding EOF. After release, all 19,688 bytes have the expected SHA256 and the player ends normally.
3. **Interruption:** same chain and held first 40 frames. Typing in the fixture draft input synchronously invokes the existing composable interruption. The native player is paused, its source cleared, and the real HTTP handler observes cancellation. Only then does the fixture attempt late WSS frames. No further native play call or `playing` event occurs, the old source stays cleared, and there is no additional HTTP synthesis request.

See [adapter-http.json](evidence/adapter-http.json), [native-player.json](evidence/native-player.json), and [test log](evidence/test.log). Native promises and MSE handling are unchanged. There are two playback HTTP requests and three local WSS tasks including the collector control. There are **zero real cloud calls**. The `fetch` audit forwards each chunk immediately; retained copies are used only to compare the completion SHA256, never to prebuffer playback. No autoplay-policy override was used.

The synthetic identity, settings, DB reads, and upstream speech are fixture boundaries; authentication and production authorization were not revalidated here. Existing boundary tests remain separate evidence. The generated client, Alibaba adapter, service, handler, composable, controller and native player are not stubs. TLS uses the ephemeral fixture certificate as a root, not `InsecureSkipVerify`. Fixture listeners bind loopback and end with the test; this does not start the application, database or public services.

## Format and cancellation limits

The pinned Twilight version is `v0.7.2-0.20261002230120-f6d0ce1a0e8c`. The fixture validates the actual serialized `run-task` command: saved model override `qwen-audio-3.1-tts-flash`, fictional test voice, `streaming: duplex`, and SDK defaults `format: mp3`, `sample_rate: 22050` when the synthetic model omits those fields. This proves that code path with **known MP3 input**, not the encoding of an uncaptured real response. Config before/after is byte-preserved. Parent-reported current real binding is Aliyun WSS; historical per-request upstream identity and encoding remain unknown.

The pinned SDK blocks in `ReadMessage` while an idle upstream sends nothing. Context is checked before reads and while forwarding frames; it does not immediately close an already-blocked WSS read. This test proves immediate local player/HTTP cancellation and rejection of late input. It does **not** claim that the upstream synthesis task instantly stops, that provider billing stops, or that every socket is closed before the next upstream event. No SDK change is included.

## Reproduce only this seam

In the selected executor (Go 1.25.7, Node 25, Chromium 154; existing dependencies):

```sh
source /workspace/.memoh-cloud/env.sh
MEMOH_WSS_SEAM_BROWSER=1 \
MEMOH_WSS_SEAM_OUT="$PWD/docs/testing/chat-read-aloud/streaming/wss-seam/evidence" \
go test ./internal/handlers -run '^TestPlaybackAlibabaWSSNativePlayerSeam$' -race -count=1 -v
```

Without the opt-in variable, the test skips. The runner imports the executor's existing esbuild and Playwright, bundles the actual checkout, and launches `/usr/bin/chromium`. No package installation is required. `internal/handlers/testdata/read-aloud-wss-seam.mp3` is generated sine audio, not a cloned voice or provider sample:

```sh
ffmpeg -f lavfi -i sine=frequency=880:sample_rate=22050:duration=2.4 \
  -c:a libmp3lame -b:a 64k -write_xing 0 read-aloud-wss-seam.mp3
```

The test splits its 94 actual MPEG-2 Layer III frames and keeps the ID3 prefix with the first frame; it does not label arbitrary byte slices as MP3 frames. [ffprobe output](evidence/mp3-fixture.json) records the fixture codec.

## Admission boundary still open

This closes the missing controlled Alibaba-adapter/HTTP/player seam and demonstrates removal of whole-utterance waiting in the source candidate. It is not full application UI QA, human QA, HAP/ArkWeb validation, public-route deployment evidence, or an acoustic latency result. No application was started and no HAP was built/signed/deployed under the task restrictions.

**Actual audible onset within 500ms remains NOT PASSED.** The remaining field evidence is one correlated observation on the phone's ordinary public NAS route using the exact admitted Web/HAP/server versions: real upstream first binary bytes and encoding → client first body → ArkWeb decode/output → first audible sound, timed from the user's replay gesture or automatic read trigger. HTTP success, `playing`, graph samples, a MIME/default mapping, and local Chromium timing cannot replace that observation. A phone on the public Internet can supply it; neither LAN membership nor USB is required. Obtaining a new real synthesis/recording is pending parent authorization and field access; this increment makes no paid request.
