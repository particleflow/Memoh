# Read-aloud version coverage: Web 2c0e277c and HAP 1.0.34 source

This is a bounded source/coverage review. No product patch, paid playback,
application build, HAP signing, deployment or device operation was performed.
No HC-03/05 implementation was taken over. The original evidence inventory was
read before comparing versions.

## Exact coverage

| Item | Exact identity | What is established |
| --- | --- | --- |
| Previous cancellation review | ac7eba92fee9c2824f058b1394a6c55362b787e8; evidence c96c0ef29454ee7b2412f1c69ae79915c3644c07 | Six source cases and two Chromium media cases on the former Blob response implementation; no ArkWeb result |
| Requested current Web | 2c0e277c2cb49b5a4a99b89a5e568ef9f413ab8a; tree 881bd9e5cc03707b4a5d0fa1e037e7faf880063f | Exact Git source read and selected current-source cases executed; deployment image/browser cache was not read back in this review |
| Distributed HAP target, source | fcc39b31b1db11e7a9d7396e34c94d5b3301a778; tree c8f3aa7fe6d26b87ee68b0bfa9c96c180a7258c7 | Source declares cloud.begonia.workassistant, versionCode 1000034/versionName 1.0.34; final source mappings checked |
| HAP binary and installed phone | Not read back | Distribution is parent-reported. No independent package-assets/signature digest, installed-version or device behavior claim |

Machine-readable hashes and scope are in `coverage.json`. The GitHub commits were
resolved first, then exact source objects fetched for local byte comparison.
No unavailable field/environment path was guessed or used.

## What changed and what can be reused

The c96 result is not a blanket certificate for the current player. Between
ac7eba and current Web, automatic speech changed from completed-reply Blob
transport to incremental visible text, an 80 ms opening flush, stream transport,
MSE/Blob source selection, rendered-DOM observation and opt-in timing records.
The original replay menu also resumes an already blocked/paused source in the
same gesture. It must not start a second synthesis for that blocked source.

The Home visibility context/Dockview re-provider and typed task-anchor deadline
modules are byte-identical to ac7eba. Their old logical boundary evidence can
be reused. Source inspection confirms current scope/enable/draft watchers still
stop synchronously, consume interrupted runs, clear the flush timer and release
stream resources. Current timing instrumentation clears with interruption and
adds no management UI. The new source changes are not treated as a new acoustic
or ArkWeb measurement.

The current Web also adds active-tab to URL synchronization in Home. The pinned
HAP source maps that same change. A contact tab clears the former task URL; task
tab parameters remain links requiring existing authorization. This review did
not alter navigation or Bot permissions.

## Actual phone source mapping, without building

The early lock entry named sharedReadAloud still refers to the historical 9c
layer. It is not the final consumer source. The builder applies the later
shared-home-admission-final paired959 snapshot after those early overlays.
All embedded sources were checked against their own SHA guards.

Nine final mapped modules match Web 2c byte-for-byte: controller, composable,
audio-stream selector, visible-text parser, rendered-text reader, timing helper,
workspace-visibility key, task-anchor locator and task-history identity helper.
Their full hashes are in `coverage.json`.

The final phone Home differs from Web only in application-title branding; after
that exact title normalization it is equal, including visibility provision,
cancellation and active-tab URL handling. The phone workspace guard equals the
current Web provider source hash. Its whole old build context was not rebuilt.

The phone pane keeps native attachment/input adapters. The same exact speech
options block, bubble click, input, original replay and status hooks remain.
The builder's final attachment-lifetime source transform was replayed in a tiny
source-only tar context to compute the actual post-transform pane hash:
`694baf4e9a60793424c804108caa16c83cd23d246a31c4e572ae555282c5e682`.
It adds attachment detach ordering; it does not change the speech block. A native
microphone-pointer entry additionally invokes the existing stop function. The
message-item difference is a media-preview-origin argument, not speech state.
The mapped speech CSS insertion matches current Web: only playing bubbles orbit,
with reduced-motion and forced-colors static alternatives. This is source
mapping, not a readback of generated CSS/JS inside the distributed binary.

## WebView lifecycle boundary

SharedChat hosts packaged resources at the secure begonia-client://app origin.
It does not set an explicit mediaPlayGestureAccess override. Its native context
menu interception handles image saving, not text-bubble replay. System Back
first calls the existing __memohBack function; at the root it may move the
Ability to background while retaining the document/drafts/transfers.

EntryAbility foreground/background emit diagnostics and image-timing cleanup;
NativeClient foreground emits diagnostic-resume. No explicit readAloud.stop
bridge for Ability background was found. SharedChat disappearance closes its
native client/voice and transfer view, but native voice attachment/input cleanup
is not proof of HTMLAudio cleanup. Therefore Vue route/deactivation/unmount
coverage cannot establish which callbacks ArkWeb fires on background, lock,
resume or renderer replacement. This is an unmeasured lifecycle seam, not a
confirmed background-playback defect or a requested new lifecycle policy.

## Minimal affected validation executed

Seven existing current-source cases were selected, without changing product
files or installing dependencies:

- Two composable streaming cases: typing consumes the pending 80 ms flush;
  hiding aborts a manual streaming request and cancels its reader.
- Two WAV compatibility cases: stop cancels a pending fallback and old queue;
  stop between fallback completion and continuation revokes the late URL.
- Two timing integration cases: late audio cannot create/play a source after
  stop; blocked-source resume preserves the same source and stopped queue.
- One actual Home routing case: activating the contact tab clears the old task
  link while retaining unrelated query fields.

`streaming-selected.log`: 6 passed, 9 deliberately skipped.
`contact-tab-selected.log`: 1 passed, 23 deliberately skipped.
These are mocked-media/transport source tests; the synthetic NotAllowedError is
not a native browser policy result. No new Chromium or ArkWeb run is claimed.
The old 12+6 matrix and c96 six-plus-two cases were not rerun.

The current tree also contains the timing owner's previously executed local
browser evidence. Its source-contract controller/composable/audio/timing hashes
match the current modules. That evidence distinguishes MSE before-EOF decoding
from unsupported-MSE Blob fallback, which waits for EOF. It explicitly leaves
physical speaker onset unmeasured. We read/reuse that evidence, not rerun it or
call it a phone pass. No latency margin against 500 ms is claimed.

## Remaining actual-device items and handoff

The only field executor should follow `FIELD_STEPS.md`. The unmeasured items are:
installed package/resource identity; cold/resumed ArkWeb activation and real
long-press selection; actual response MIME/decoder branch; native stream abort;
click/input stopping under ongoing deltas; old-queue behavior over retained or
recreated documents; physical sound and matching single-bubble motion; actual
system reduced-motion/accessibility exposure; natural pause/end/error cleanup.

HC-03/05 remain with the Harmony owner. This review changes no native code or
field state and does not settle those cases. Do not infer installation from
distribution, ArkWeb from Chromium, or physical speaker sound/latency from a
native playing event, decoder append or audio graph sample.
