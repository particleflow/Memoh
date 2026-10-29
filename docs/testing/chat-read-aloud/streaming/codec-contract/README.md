# Qwen codec and MIME source contract

The current known saved configuration already requests **MP3**, not WAV. No codec change is justified by that evidence, and writing the same default again would not improve latency. The WAV performance measurement was a controlled compatibility case; it does not prove that the field voice uses WAV. This audit closes the source/configuration decision now, without waiting for a phone recording or a new paid playback.

This is an evidence/test/configuration-handoff increment only: **zero production source changes**. Unified deployment source `b5e7aff6040d1efce36ee51a0c5d3f8915dbddf3` is untouched. Prior timing increment `0f057a4e22f614418dddbffdde0be53075dfb4ca` remains separate for a later unified batch. Do not merge that parent merely to consume this evidence-only commit.

## Source chain, in order

1. The bot's configured speech-model record is resolved by `internal/audio/service.go:resolveSpeechParams`. Configuration is shallow-merged in this order: provider → model → per-call override. Later keys win. `StreamPlayback` supplies no per-call override, so playback cannot silently override the saved codec. The model record's `ModelID` is passed as `sdk.SpeechModel.ID`.
2. `internal/audio/registry.go` selects the saved `alibabacloud-speech` factory, passing the saved API key and WSS URL. It does not force WAV, MP3 or a different voice. The schema already exposes model field `format` with `mp3`, `wav`, `pcm`, `opus` options.
3. Pinned Twilight `v0.7.2-0.20261002230120-f6d0ce1a0e8c`, `provider/alibabacloud/speech/config.go`: the default is `format=mp3`, `sample_rate=22050`. Only a nonempty string `config["format"]` replaces the format. Qwen does not read `response_format` or `codec`. These latter keys do not select its output format. A literal empty/null/non-string model value can mask the provider's format during merging and then fall back to the SDK default; the existing UI strips empty/null values, so clearing the UI instead inherits the provider's value.
4. `speech.go:DoStream` parses that merged config, sets `cfg.Model` from the model record's ID, and passes it to `ws.go:sendRunTask`. The serialized WSS `parameters.format` is exactly `cfg.Format`, alongside the independently preserved `voice` and sample rate. The UI enum supplies the canonical lowercase `mp3`; raw arbitrary values are not normalized by the serializer.
5. The SDK forwards incoming binary audio incrementally. Its `SpeechStreamResult.ContentType` is a **static mapping of the requested format**: `mp3→audio/mpeg`, `wav→audio/wav`, `pcm→audio/pcm`, `opus→audio/opus`. WSS binary frames have no per-frame HTTP audio Content-Type. This adapter does not inspect received audio or read an upstream codec declaration to derive MIME. Unrecognized strings fall back to the SDK's generic `audio/mpeg` MIME while remaining unchanged in the WSS request; that behavior is not evidence that an invalid format was accepted.
6. `BotAudioHandler.Playback` forwards that MIME as the HTTP Content-Type and flushes nonempty chunks. There is no filename-based or Qwen-model-name-based conversion to WAV. The generated browser SDK returns the response stream; the composable takes the HTTP Content-Type.
7. `prepareSpeechSource` maps the `audio/mp3` alias to `audio/mpeg` for capability lookup, then calls the browser's actual `MediaSource.isTypeSupported(mime)`. Supported MIME uses streaming MSE; unsupported MIME or absent MSE uses the same-response native Blob compatibility path. It does not sniff the body to select the branch.

**Blob does not mean WAV.** A browser lacking MSE support for MP3 can also take Blob for a correct `audio/mpeg` response. Thus changing a codec that is already MP3 cannot fix that capability condition. Conversely, a properly selected WAV correctly receives WAV MIME and can enter the known EOF-waiting compatibility branch. WAV compatibility is retained; this audit does not call that branch streaming.

## Existing configuration evidence and MIME verdict

The previously supplied, non-sensitive parent evidence is preserved at [qwen-wss-audit.json in the earlier source](https://github.com/particleflow/Memoh/blob/fab04e2c5ab8da6f36daf23549962b0277dab8a8/docs/testing/chat-read-aloud/streaming/resume-gesture/evidence/qwen-wss-audit.json). It reports Alibaba WSS, model `qwen-audio-3.1-tts-flash`, and no explicit format/codec/sample-rate/stream setting. The original Windows files were relayed by the parent, not independently opened in this executor; no fresh field configuration was read in this step.

For **that saved state**, the code necessarily serializes `format=mp3`, with `audio/mpeg` MIME. The default full-adapter contract was already exercised in f523 and is reused. No source path was found that turns this absent format into WAV MIME. There is currently **no evidence of an MP3→WAV MIME classification bug**.

The static MIME contract does not prove the bytes actually returned in a historical cloud request. If an upstream response violated its requested format, the SDK would not detect that discrepancy. No such field sample exists here, so relabeling audio, stripping codec parameters, hardcoding MP3, or adding a decoder would be speculative. The safe signature/branch metadata in the independent 0f057 increment is a later diagnostic aid, not a prerequisite for this source/configuration conclusion.

## New local contract cases

Only the previously untested configuration precedence seam was exercised. Real `audio.Service` → real registry → pinned native Alibaba WSS adapter connects to an ephemeral loopback TLS fixture, with the existing known MP3/WAV bytes. Certificate validation uses the fixture root, not disabled verification. Each case preserves the synthetic model's voice, uses one local WSS task, yields the first binary chunk before withheld EOF and completes with the exact expected byte hash:

| Provider format | Model config | Serialized WSS format | SDK MIME |
|---|---|---|---|
| wav | `format: mp3` | mp3 | audio/mpeg |
| mp3 | `format: wav` | wav | audio/wav |
| wav | `response_format: mp3` only | wav | audio/wav |

[Raw contract result](evidence/configuration-wss-contract.json), [targeted race test log](evidence/b5-test.log), and [source/fixture checks](evidence/source-contract.json) are included. The test was applied to an isolated **exact b5 worktree**, not the deployment worktree; no database, application service or real cloud request was used. New fixtures are byte-copies of the previously reviewed sine assets so this increment can run without importing 0f057/f523/eae history or their product files. There are no newly cloned or provider-generated voice samples.

No old 14/19/7/9 suites, f523 full-chain test, or 0f057 four-profile timing test were repeated. Their existing evidence is reused: known MP3 reached native MSE before EOF; the known WAV compatibility path waited for EOF. The new cases verify the concrete configuration-to-wire/MIME contract, not real provider codec compliance or phone acoustics.

To run only this seam on the b5 source with this test and its two fixtures added:

```sh
source /workspace/.memoh-cloud/env.sh
MEMOH_CODEC_CONTRACT=1 MEMOH_CODEC_CONTRACT_OUT="$PWD/codec-contract-evidence" \
  go test ./internal/audio -run '^TestAlibabaPlaybackCodecConfigurationContract$' -race -count=1 -v
```

## Exact minimal configuration handoff

[Machine-readable configuration decision](minimal-config-diff.json):

- **Current known absent-format state:** configuration difference is empty (`[]`). It already requests MP3. No field write is recommended to “fix WAV” without contrary configuration evidence.
- **Only if a later/current read shows explicit or inherited WAV:** set the existing **model's** `config.format` to lowercase `"mp3"`. This is sufficient to override provider WAV; keep model ID, cloned voice, sample rate, credentials and all other config values. No second request, new synthesis endpoint, re-clone or provider switch is necessary.

For an explicit model value the logical change is:

```diff
- "format": "wav"
+ "format": "mp3"
```

For inherited provider WAV, add only `"format":"mp3"` to the existing model config. The model editor already supports this choice: schema fallback selects the provider's model schema even for the custom Qwen ID; the enum supplies exactly `mp3`; Save configuration emits the existing config with that selection and calls `PUT /speech-models/{id}`. Its save path does not invoke the separate synthesis/test callback. Existing UI normalization drops empty/null values; nonempty cloned voice values remain.

For a strictly one-key API difference, the sole writer must deep-copy the **complete current model config**, set `format` on that copy, and send the full config using the existing PUT endpoint. `UpdateSpeechModel` replaces the entire config; a partial `{"config":{"format":"mp3"}}` payload could erase the voice and is **not** the proposed request. Omit the optional name field to retain the current name. Read back and compare the other values locally without exporting secrets. The JSON `set` object in the handoff is a logical difference, not a standalone PUT body.

Official [Qwen client events](https://help.aliyun.com/en/model-studio/qwen-audio-tts-client-events) define MP3 as a supported/default format independently of voice, including cloned voices; [server events](https://help.aliyun.com/en/model-studio/qwen-audio-tts-server-events) deliver audio frames within the same WSS task. This supports the existing single-request codec option. It does not guarantee native MSE support on every device or a real model's first block within 500ms.

This step is complete: the source default, explicit override, UI save route, MIME origin and minimal conditional configuration difference are determined and locally verified. No production branch repair is justified by the current evidence. b5 deployment, 0f057's later independent review, and final concentrated device/acoustic acceptance remain separate; none was modified or made a prerequisite for completing this audit.
