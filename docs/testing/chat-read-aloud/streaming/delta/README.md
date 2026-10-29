# Visible-body follow-up and format admission audit

Parent candidate: `2b9c8fc24ff55bbaaa88cc112438006d82fe0513`.
Unified integration owner: `01a15840-f35b-757a-a2a0-e20479062007`.
This follow-up is a source candidate for independent review. Deployment and the
500 ms physical audible-onset requirement remain **NOT PASSED / pending**.

## Independent format audit: no decoder added without verified input

Read-only inspection used exact HAP source
`12faf4c06db01d8fffc29aad3a247b4a85e884e7`, including its client configuration,
Web source lock, `SharedChat.ets`, `SharedWebPolicy.ets`, and
`HarmonyVoicePlayer.vue`. File hashes and findings are in
[evidence/format-audit.json](evidence/format-audit.json). Only configuration key
names and public source metadata were retained; no credential/origin values were
exported. No HAP tree, source lock or native configuration was changed.

The source does not contain the saved field TTS provider/model format, a real
upstream response capture, or ArkWeb engine/codec probe. The attachment voice
player's WaveSurfer `load`/`loadBlob` is not a streaming-read-aloud decoder. The
Web origin/API policy is not evidence of MSE, codec, autoplay or audible output.
The historical source lock also explicitly did not independently verify backend
playback readiness; its presence cannot be treated as a field acceptance receipt.
A narrowly scoped Personal Context lookup reported unavailable; it recovered no
missing field record. No provider was called to fill this gap.

The installed Twilight OpenAI-compatible adapter merges saved provider/model
configuration and defaults `response_format` to `mp3` if absent. Crucially, its
`DoStream` supplies `contentTypeForFormat(cfg.ResponseFormat)` rather than the
actual upstream `Content-Type`. Therefore even the current playback endpoint's
advertised MIME is **not independent proof of the received Magpie encoding**.
The schema's `wav`/`pcm` options and test fixtures are not saved field settings.
There is no verified PCM sample rate, sample representation, endianness, channel
count or framing. No WAV/PCM decoder, sample-rate guess, format/voice rewrite,
new provider, or model guess is included.

A separate read-only in-memory Chromium capability query performed zero network
requests and no audio playback. Chromium 154 reports `audio/mpeg` MSE support,
`audio/wav` MSE=false while ordinary media-element playback is only `maybe`, and
MSE=false for the advertised `audio/pcm` and `audio/opus` strings. See
[evidence/readonly-chromium-capabilities.json](evidence/readonly-chromium-capabilities.json).
This is a capability query, not a rerun of the old audio fixture and not an
ArkWeb result. MSE does not mandate any particular codec; a device runtime probe
and actual encoded-byte validation remain necessary. See the primary
[W3C MSE specification](https://www.w3.org/TR/media-source-2/#goals).

## Product visible semantics and actual DOM integration

The product now passes its existing pane-scoped message lookup and scroll root
into `useChatReadAloud`. `message-item.vue` marks only the existing assistant text
render roots and their block IDs with nonvisual `data-speech-*` attributes. No
button, label, chrome, text-stream, attachment or history path is added or changed.

Manual replay reads those actual rendered text roots. Automatic collection uses
the same reader, a post-render message watcher and a scoped `MutationObserver`.
The observer follows typewriter/async-renderer DOM changes after final source has
arrived; only replies already observed live retain eligibility. History baseline,
input/visibility interruptions, permission masks, errors and unmount still consume
or stop the run. The observer disconnects at unmount. Late final DOM can append
its unseen tail; identical DOM never replays the prior prefix.

Whitespace and nonprinting layout spacer normalization make DOM block changes
stable without deleting visible characters. Unresolved reference/table/fence
syntax holds the subsequent suffix until the renderer can settle it. Literal
brackets/pipes return when genuinely displayed in the final body. Plain code
preserves its visible brackets, Markdown-looking strings and naked URLs. The
source parser is only a boundary/markup discriminator; it does not replace custom
renderer display text or silently remove generated visible titles.

| Product renderer / body | Speech source and protected boundary |
| --- | --- |
| `MdText` | Actual split CJK/Latin span text, with no inserted character-level markup. |
| `MdLink` | Actual rich label; href/title, portal tooltip and site-icon metadata are never read. Workspace/icon side-effect helpers alone are mocked in component tests. |
| `ChatCodeBlock` / `CodePanel` / `CodeBlock` | Actual `<pre>/<code>` content before/after highlighting; copy controls excluded. Uses the real shared registration router, including the product's special `text` fence dispatch. |
| Markstream tables | Actual header/cell/row text, with structural spacing. Prospective table pipes are held until classification settles. |
| Blockquote / admonition | Actual quote/title/body display, retaining following prose. |
| `MdFootnoteReference` / anchor | Visible numeric reference and definition text retained; href and decorative arrow metadata excluded. |
| Math | Actual display is authoritative. Literal fallback remains visible text; real KaTeX HTML is read without hidden MathML/TeX annotations or zero-width layout spacers. |
| `ThemedMermaidBlock` / vendor renderer | Real inspected `data-markstream-mode`/pending contracts hold temporary source and later paragraphs. Preview reads SVG/foreign-object labels; hidden source, SVG title/desc and controls are excluded. Final DOM updates remain eligible. |
| HTML / CSS | Actual computed `display`/`visibility`, hidden ancestors and disconnected roots are respected. Visually present `aria-hidden` text is retained; off-root process cards and captions are excluded. |

Mermaid pending/preview tests use DOM fixtures matching the inspected vendor
contract. They do not claim actual Mermaid layout, ArkWeb graph rendering or a
physical-device UI pass. The actual shared UI component pin matches the product gitlink
`f69402a55dd87a59c10722fe11507ad8fd7048f5`. The shared Markdown, link, text, code, footnote,
quote, table and admonition components are exercised in jsdom. A real KaTeX
markup fixture separately verifies annotation/layout exclusion; the isolated
Markdown runtime currently shows its literal inline-math fallback, which is also
covered so the body after it is not lost.

## Difference-only verification and admission boundary

- **19 new tests passed**, in `speech-visible-delta.test.ts` and
  `useChatReadAloud.dom.test.ts`; exact output is in `evidence/tests.log`.
  Cases include inline-link token splits, late reference/footnote definitions,
  table reclassification, inline/fenced code, literal brackets, multiple text
  blocks and persisted IDs, late raw DOM after final source, delayed diagram
  labels, actual CSS visibility, manual DOM replay, post-final observer updates,
  interruption and observer teardown. Four scheduling cases use the real
  composable with a controller spy: they do not relabel mocked audio as native
  playback evidence.
- Targeted ESLint passed for the changed TypeScript and two Vue consumers.
  Scoped strict `vue-tsc` passed for the production TypeScript modules. This is
  not a full application/HAP typecheck, build or UI verification.
- The actual component run reports jsdom's missing canvas `getContext` warning;
  it is retained in the log and is not presented as a canvas/diagram layout pass.
- The old 14 frontend + 3 backend cases, MP3 native probe, and backend/audio stream
  implementation are retained at their original scope. They were not rerun here.
  Byte-equality reuse checks are recorded in `evidence/verification.json`.
- No services, provider calls, paid auditions, clone changes, HAP signing/build,
  package publication/deployment, or changes to Agent core/permissions were performed.

**Integration is not yet admitted.** The unified owner must review this increment
with its parent and include the new module plus two nonvisual consumer hooks in
the current Web/native source map. Actual app/HAP interaction evidence remains
absent under the instruction prohibiting service startup/deployment. Current
Magpie encoding and ArkWeb decoder/autoplay/output capability still require
existing field configuration/capture or newly authorized device evidence.

Minimum missing evidence: the existing saved `response_format`; actual upstream
response MIME and first bytes/container metadata (not the SDK-inferred MIME);
current ArkWeb engine/MSE result and successful incremental decoding for that
same encoding; and a monotonic user-trigger-to-acoustic/loopback first-output
measurement for manual and automatic paths. HTTP/`playing`/decode activity cannot
replace that final audible measurement. The 500 ms result stays unverified.
