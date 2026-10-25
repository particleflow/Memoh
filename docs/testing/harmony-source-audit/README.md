# Historical Harmony source audit and login-race repair

Audit base: `bf44bdd907ce8a67c77de937d69a2e8db62ba7b6` on the existing
`recovery/harmony-source-20261023` branch. The actual complete shared-source pin
remains `51b19cbeb76e9803eb8d4b614cfabb03ef18cc73`, not an inferred newer SHA.
No HAP, SDK, native compilation, release, public upload or production change was
performed. Existing source ZIPs, SDK, private config and unsigned HAPs remain.

## What the historical count does and does not identify

The inaccessible historical snapshot was reported as 127 tracked paths and
114 source entries. Of 72 formerly excluded paths, 53 reportedly matched the
original ZIP and 19 had later cloud edits. Neither the path-to-edit mapping nor
the 19 later blobs was preserved in the available register. Personal Context
returned unavailable for this conversation. The count **19 remains unknown**;
this audit does not fabricate nineteen filenames or mark them unnecessary.

Both retained source ZIPs were rechecked for exact size, SHA-256 and ZIP CRC.
All 72 historical excluded paths and all 51 PR67 public paths are present.
For the 72 paths, current bytes at the audit base classify as **29 exact archive
copies, 37 text equivalents, and 6 restored files with later documented recovery
changes**. The two ZIPs themselves differ at six paths; that different set is
also listed in `archive-path-audit.json`. Neither six-file set is the historical
19-file set. The complete per-path table below includes checksums and commit
lineage in the JSON companion, without private file contents.

The six recovery-modified inherited files were the existing Web builder,
AppScope version metadata, NativeClient router, materials generator, file
adapter and Web packager. Their newer changes have explicit recovery commits;
they are not claimed to reproduce the missing historical snapshot.

## Functional findings

| Requirement | Current implementation and evidence | Audit result |
| --- | --- | --- |
| Normal upgrade retains login | Stable `begonia-client://app/index.html`, DOM storage enabled; `EntryAbility` does not clear Web storage. `harmony-entry.ts` clears identity only for a pending explicit service transition. `harmony-service-config.ts` leaves token untouched when API base is unchanged. | Restored and retained; device upgrade still unverified. 115 actual TS/ETS configuration/router checks pass. |
| Old requests cannot log out a new session | `api-client.ts` lacked a response-generation check although the auth-session counter and PR67 race regression were present. | Confirmed gap, repaired below. Membership in the historical 19 is unknown. |
| Bot list/create/settings and model choice | Existing shared `pages/bots/index.vue`, `new.vue`, `components/bot-settings.vue` and `useAgentModelCatalog.ts`. No phone-only editor or Provider model. | Byte-identical to pinned shared source and previously verified phone context. Existing compiled browser evidence covers create/cancel, duplicate submission, 403/404/500 handling and return to chat. No live Bot creation in this audit. |
| Downstream Bot/Agent hierarchy and detail | Existing `session-tasks.vue` and `useBotDelegations` select the server-returned specialist Bot/session; `useSessionTasks` supplies scoped readers and clears denied/stale content. | Same bytes as previously verified 9e6 phone context; no omitted phone component found. |
| Native content, progress and continuation | `useComputerTaskObservation`, `useNativeTask`, `native-task-content.vue`; existing generated SDK calls; interactive capability gates follow-up, retained request UUID prevents unknown-outcome resend. | Same bytes as prior verified context. Batch Pi remains unsupported; no new queue/control API added. |
| Truthful task results | `useComputerTaskDeliveries` validates each page and uses references only for committed entries; `session-task-results.vue` consumes the reader. Tail/read completion is distinct from task/business completion. | Same existing implementation. Older-server 404 is unsupported/error. Aggregating result pages remains an existing bounded-request/unbounded-total design limitation, not a new pagination feature. |
| Phone attachment upload | Existing `harmony-files.ts` gets upload limits, uploads the original and verifies receipt/readback; store send path supplies references after preparation. | Production adapter/store test passes against the current 51b context, including unknown write reconciliation, cancellation, no duplicate ready-message send and Bot isolation. No file bytes added to conversation payload. |
| Attachment reception/save | Existing `originalBlob` verifies size/hash; `saveNativeOriginal` routes fetch/save/abort. Explicit delegation URL is retained with no hash fallback on denial; task save uses the attachment owner Bot. | Actual frontend/native router tests pass. Real native transport/OS picker is not exercised this round; previous controlled native disk test is separate evidence. |
| Storage, permissions and app identity | Existing native shell, identity metadata, permissions, file bridge, config storage and licenses are unchanged by this repair. | No signing/identity migration, production service, Agent core or new transport service. |

`functional-source-comparison.json` records 21 exact module hashes: 20 are
unchanged from the previously verified 9e6 phone context. Twelve are identical
to their upstream shared module; the remaining unchanged ones retain existing
platform adaptations. The only new production-module difference is the response
session guard in `api-client.ts`. This is comparison evidence, not a rerun of
all previous hardware or business tests.

## Confirmed repair and validation

The existing `harmony_auth_race.cjs` failed on the current phone source:
`anonymous401CannotClearNewLogin` and
`priorSession401CannotClearSameTokenLogin` were false. The current request-token
comparison protected a different new token but missed an anonymous request
started before login, and a later login with identical token bytes.

The existing builder now captures the current token and existing
`authSessionGeneration()` when a request starts. A 401 only reaches the original
cleanup handler if both still match. It introduces no persistent auth state,
credential migration, endpoint or alternate login. A current-session 401 still
clears login, including the existing same-session missing-Bearer behavior;
network failures retain it. The existing test accepts an explicit current
checkout rather than silently reading the old compiled checkout. The platform
test preparer supplies the existing counter in the upstream unit-test mock.

- Existing race regression: failed before, passes after, including current 401
  cleanup and network-failure preservation. Raw before/after results are adjacent.
- Relevant API/login/error suites: **146 passed in four files**; complete phone
  Web typecheck and targeted API-client lint passed. No backend error protocol
  changed, and no backend build/test or native compilation is claimed.
- Configuration and native router harness: **115 checks passed**.
- Existing download/frontend bridge regression passed with actual TS/ETS and
  controlled SDK/transport/UI boundaries.
- Current-context upload preparation regression passed. Its initial default-
  sandbox invocation reported subprocess EPERM; the same authorized local test
  succeeded with execution approval. No external endpoint was used.
- Real existing Vite at `http://127.0.0.1:4181`, Chromium 390x844, Chinese:
  an anonymous request held before normal UI login cannot erase the new login;
  the real login store can establish a later session with the same token and
  reject the earlier response; reload retains login; a current 401 returns to
  login. One login HTTP call, zero page errors. The same-token case explicitly
  invokes the real store; it is not claimed as a second server authentication.
  [Result](result.json), [login retained](login-retained.png),
  [current 401 returns to login](current-401-login.png).
- Source manifest/integrity validation passes. No build output is fabricated.

To rerun without a new Web/HAP build, use the current generated-context
checkout and its existing dependencies (the old compiled checkout is retained):

```sh
HARMONY_TEST_CHECKOUT=/absolute/path/to/current-context node mobile/client/tests/stage2/harmony_auth_race.cjs
python3 docs/testing/harmony-source-audit/auth-race-browser.py http://127.0.0.1:4181 /tmp/harmony-auth-proof
```

Only synthetic HTTP and a fresh cloud Chromium profile were used. No user
browser, Windows, NAS, production account, model or signing material was used.
Human QA, signed upgrade/login persistence on-device and phone-to-NAS transfers
remain unperformed.

## Remaining source evidence gaps and existing limits

1. **Historical 19 edits:** file identities and original later content are still
   unavailable. They cannot be individually declared recovered/equivalent/not
   needed. The confirmed auth repair does not reduce that historical count.
2. **Label `5e55`:** the abbreviated lookup failed during this earlier audit.
   The subsequently supplied full SHA has now been fetched and verified through
   normal Git. The current source pin and full comparison are recorded in
   [the production alignment follow-up](production-5e55.md); this older audit
   still describes its actual 51b input.
3. **Known retained limits:** no automatic LAN/IPv6 identity migration, no
   cross-process file resume, no batch-Pi interactive continuation; native save
   still expects positive known file size. These are existing explicit limits,
   not newly identified members of the historical 19.
4. The retained unsigned HAP remains source `f6403de`; this source repair and
   earlier bf44 source changes have not been packaged. Signing/transfer/device
   acceptance remain separate work and did not block this audit/repair.

## Every formerly excluded path

Status describes verified bytes at the bf44 audit base, not the missing later
historical version. All paths are relative to `mobile/client/`.

| Path | Verified current relation to source checkpoint |
| --- | --- |
| `contracts/album_sync.json` | Exact archive bytes |
| `contracts/client_capabilities.json` | Text equivalent (line endings/trailing newline) |
| `contracts/folder_source_access.json` | Exact archive bytes |
| `contracts/harmony_native_assets.json` | Text equivalent (line endings/trailing newline) |
| `contracts/matrix_assets.json` | Exact archive bytes |
| `contracts/ui_theme.json` | Text equivalent (line endings/trailing newline) |
| `mobile/build_memoh_web.py` | Restored, then changed in recorded recovery commits |
| `mobile/harmony/app/AppScope/app.json5` | Restored, then changed in recorded recovery commits |
| `mobile/harmony/app/AppScope/resources/base/element/string.json` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/AppScope/resources/base/media/app_icon.svg` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/build-profile.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/code-linter.json5` | Exact archive bytes |
| `mobile/harmony/app/entry/build-profile.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/hvigorfile.ts` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/oh-package-lock.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/oh-package.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/cpp/CMakeLists.txt` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/cpp/authority_lock.h` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/cpp/file_uri.cpp` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/cpp/folder_publish.h` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/cpp/media_lease.h` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/cpp/raw_image.cpp` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/cpp/types/libbegoniaraw/index.d.ts` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/cpp/types/libbegoniaraw/oh-package.json5` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/AssetBackgroundScope.ets` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/ClientDiagnostics.ets` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/NativeClient.ets` | Restored, then changed in recorded recovery commits |
| `mobile/harmony/app/entry/src/main/ets/common/NativeExchangeDirectory.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/NativeMediaFile.ets` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/NativePhotoSelection.ets` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/NativeTransferPolicy.ets` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/ets/common/NativeVoice.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/RawImage.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/SharedWebPolicy.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/UiTheme.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/VoiceText.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/common/VoiceTextProtocol.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/entryability/ClientAbilityStage.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/entryability/EntryAbility.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/pages/SharedChat.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/ets/workers/VoiceRecognition.ets` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/module.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/resources/base/element/color.json` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/resources/base/element/string.json` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/resources/base/media/app_icon.svg` | Exact archive bytes |
| `mobile/harmony/app/entry/src/main/resources/base/profile/main_pages.json` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/entry/src/main/resources/base/profile/share_files.json` | Exact archive bytes |
| `mobile/harmony/app/hvigor/hvigor-config.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/hvigorfile.ts` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/app/oh-package.json5` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/raw-codec.lock.json` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/tests/authority_lock_runtime.cpp` | Exact archive bytes |
| `mobile/harmony/tests/folder_handoff_runtime.cpp` | Exact archive bytes |
| `mobile/harmony/tests/folder_publish_runtime.cpp` | Exact archive bytes |
| `mobile/harmony/tests/voice_text.cjs` | Exact archive bytes |
| `mobile/harmony/tools/build_api26.py` | Text equivalent (line endings/trailing newline) |
| `mobile/harmony/tools/materials.py` | Restored, then changed in recorded recovery commits |
| `mobile/harmony/tools/prepare_raw_codec.py` | Exact archive bytes |
| `mobile/memoh/HarmonyVoiceFan.vue` | Text equivalent (line endings/trailing newline) |
| `mobile/memoh/HarmonyVoiceInput.vue` | Exact archive bytes |
| `mobile/memoh/HarmonyVoicePlayer.vue` | Exact archive bytes |
| `mobile/memoh/harmony-files.ts` | Restored, then changed in recorded recovery commits |
| `mobile/memoh/harmony-voice-geometry.ts` | Text equivalent (line endings/trailing newline) |
| `mobile/memoh/harmony-voice.ts` | Exact archive bytes |
| `mobile/memoh/harmony.ts` | Exact archive bytes |
| `mobile/memoh/licenses/lucide.txt` | Exact archive bytes |
| `mobile/memoh/licenses/wavesurfer.txt` | Exact archive bytes |
| `mobile/memoh/voiceFan.css` | Exact archive bytes |
| `mobile/memoh/voiceFan.svg` | Text equivalent (line endings/trailing newline) |
| `mobile/package_memoh_web.py` | Restored, then changed in recorded recovery commits |
| `tools/artifact_paths.py` | Text equivalent (line endings/trailing newline) |
| `validation/harmony/tools/toolchain.py` | Text equivalent (line endings/trailing newline) |
