# Exact Qwen-Audio 3.1 protocol compatibility increment

Inspected 2026-10-30 UTC. Base: `2918613d4770f826c6a39e967127b012a2c29754`, containing native-input source `09acf7b00d560bac3f312522756e3749d7ddd0a3` and independent cursor fix `7a1784ce9a11ff31b76537431fb829debd25e87f`. This increment adds an exact-model loopback protocol test and corrects the adapter package comment; executable production statements are unchanged. No new runtime adapter, provider selection, dependencies or defaults are needed for the documented basic wire contract.

## Primary evidence and applicability

The parent supplied an **old whitelist**, not a newly verified current configuration: `alibabacloud-speech`, `qwen-audio-3.1-tts-flash`, `wss`; actual MIME remains unknown. No provider/SQL/secret read was attempted here.

The vendor's [WebSocket connection guide](https://help.aliyun.com/en/model-studio/qwen-audio-tts-websocket-api) explicitly names this exact model in its direct examples. Its run-task / multiple continue-task / finish-task exchange matches the existing adapter. One task retains the upstream synthesis context; the server buffers incomplete sentences and finish drains the final text. This is Qwen-Audio-TTS, not the distinct Qwen-TTS-Realtime event API. The package's historical CosyVoice name does not exclude this documented protocol.

The [client event contract](https://help.aliyun.com/en/model-studio/qwen-audio-tts-client-events) permits the adapter's configured voice, format, sample rate, volume, rate and pitch. SSML restricts multiple inputs, so the existing rejection remains. Optional instruction, language hints, seed, pronunciation corrections and other additional controls are **not** forwarded by this basic adapter; full optional-control support is not claimed. The documented input interval ceiling is 23 seconds. No invented empty-input heartbeat, silent retry or renewed task was added. Long generation/tool pauses can still encounter this upstream limit.

The [server event contract](https://help.aliyun.com/en/model-studio/qwen-audio-tts-server-events) places sentence metadata and this model's token usage in JSON events, with audio on binary messages. Completion requires the matching task-finished. The adapter's existing reader correctly separates those channels. The vendor pages are rolling documentation without an exposed immutable revision; the inspection date is the version provenance, not an invented SDK release pin.

## New isolated evidence

`internal/audio/adapter/cosyvoiceinput/qwen_audio_test.go` uses an owned loopback WebSocket, synthetic key/voice, exact model ID, and sentinel binary payloads. Its MP3 and WAV subcases verify:

- One configured run-task, two ordered Unicode text inputs, one final command, same task ID throughout; configuration is not substituted.
- Sentence begin/synthesis/end metadata and token usage never enter the audio channel.
- First binary output arrives before finish; an incomplete tail returns only after finish; clean completion includes the tail and rejects further input.
- Configured format determines the returned MIME.

`protocol-test.log` retains the targeted command output. Both cases first passed without a production logic change; there is no newly reproduced product red/green claim. Existing cancellation, identity, replay, middleware and browser evidence remains in `../native-input-20261030/`; those unchanged suites were not manually repeated. The normal commit hook still performs its mandatory affected-package checks.

These sentinel bytes are deliberately **not synthesized speech or a codec fixture**. They prove protocol order/coverage, not pronunciation, acoustic continuity or zero network underruns. Prior Chromium fixture timelines remain prior Chromium evidence, not Qwen or ArkWeb acceptance. The separate old eight-fixture issue is already resolved by the integration owner and is not investigated or reapplied here.

## Remaining exact boundary / unique writer steps

1. Under the writer's existing authorized team/RLS context, use the retained `../native-input-20261030/SAFE_FIELDS.sql` projection. Return only enabled flags, client type/model ID, endpoint scheme/host/path class, format/sample rate/SSML and voice/credential **presence booleans**. Do not return keys, raw voice IDs, complete provider responses or new credentials. Preserve the actual endpoint; this review does not authorize migration to the example domains.
2. Read the capability GET's identity and input MIME without generating speech. Current Web native input requires MP3 plus `MediaSource.isTypeSupported`; a WAV protocol fixture passing does not add browser WAV streaming support. If MIME/endpoint/config differs, stop at that exact incompatibility rather than switch settings or bill a fallback.
3. Only when separately arranged, observe one natural user reply on the installed target version: automatic first playback following a supported gesture, full spoken-text coverage/prosody, first audio and real gaps/underruns; cached Replay must make zero generation calls. Click/input/scope/background interruptions must stop motion and prevent stale work resuming. Do not generate a paid audition for this handoff.

Actual Qwen synthesis, current selected Bot configuration, physical listening, and ArkWeb activation/MSE/stream transport/background behavior remain unverified. No build, signing, deployment or phone installation occurred. The user's automatic-start, natural continuous speech and missing-word reports remain open pending actual product verification. The independent replay and cursor source patches remain frozen and separately consumable.
