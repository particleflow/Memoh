# Harmony system back: layered return

Baseline: `c530567d2c8f35547e2f1cc550859f3da016b77f`, PR82's existing
`feat/harmony-native-runtime-ui-20261025` branch. Shared source remains
`9bb6fc3d84387012b4e2f5daffcc95d81ece5db4`; the 43 reviewed phone patches,
network consumer, identity, permissions, empty deployment slot and version are retained.

## Defect and minimal correction

The only native entry page, `SharedChat.ets`, had no `onBackPress` handler.
System back therefore used the native root-page default before Web overlays or
Vue route owners could react. No per-page settings change can wire this event.

The new host adapter in `harmony-back.ts` calls existing owners in this order:

1. Media lightbox close; otherwise the top Reka layer's detail-back action or
   Escape dismissal. Task detail returns to the list before closing the dialog.
   A saving owner's existing dismissal veto remains effective.
2. Service configuration's existing cancel action, when editing an existing service.
3. The active settings surface / chat panel's existing back affordance. It does
   not click a still-mounted chat panel behind the settings overlay or arbitrary
   message chevrons. Cold settings subpages fall back to the settings list.
4. Login, chat and onboarding roots return unhandled. The native host backgrounds
   the ability without destroying the Web document or native transfer owners.

The native synchronous handler coalesces only its pending JavaScript dispatch,
checks the existing page generation, and does not run ArkWeb history, reload,
close clients, clear drafts, cancel transfers, or change login/service storage.
Unavailable/failed/unresponsive renderer states retain the system default. A
rejected dispatch records a fixed diagnostic without exiting a potentially open
Web layer; it releases the pending guard so another back can be attempted.

No shared sidebar/run-list, recording/animation, server, permission, signing,
configuration protocol or dependency was changed. The builder only registers one
phone-owned module. Existing source CI includes the new regressions; no new
workflow, SDK download or HAP build was added to CI.

## Runtime evidence

`http://127.0.0.1:4183`, actual generated Harmony Vue application, Chromium
390x844, synthetic authenticated API and native boundary. `verify_ui.py` exercises
12 flows recorded in [ui-result.json](ui-result.json), with one login, no page
errors, no model calls or management/transfer writes. Draft text and pending
attachments survive return. This is not a screenshot-only component preview.

Reviewed screenshots from the current run:

- [Image attachment preview](attachment-preview.png): the synthetic one-pixel
  white image and its existing close control; return closes the lightbox.
- [Text preview dialog](text-preview-dialog.png): the existing Reka preview owner.
- [Open navigation drawer](drawer-open.png): nested menu returns without closing
  the sheet; the next back closes the sheet.
- [Settings content](settings-providers.png) → [settings list](settings-list.png)
  → [retained chat draft/attachment](chat-returned.png).
- [Task detail](task-detail.png) → [task list retained in dialog](task-list-returned.png);
  the second return dismisses the dialog. No task was submitted or rerun.

Unit regression: 12 cases. The task-detail case failed before the inner-layer
correction; the original red log is preserved. Native-method seam regression:
9 scenarios (pending coalescing, handled/root behavior, stale/disappeared page,
failed dispatch, inactive/failed/unresponsive renderer). These are synthetic SDK
seams, not execution by ArkWeb or a real system gesture.

## Reproduction and SDK basis

Run `python3 scripts/check-harmony-source.py`, install the existing pinned Web
dependencies as the current `harmony-source-ci.yml` does, then in the generated
context run `pnpm --filter @memohai/web typecheck`,
`pnpm test --run apps/web/src/lib/harmony-back.test.ts`, and
`pnpm exec eslint apps/web/src/lib/harmony-back.ts apps/web/src/lib/harmony-entry.ts apps/web/src/lib/harmony-back.test.ts`.
At repo root run `node docs/testing/harmony-system-back/verify_native.cjs`.
`python3 docs/testing/harmony-system-back/verify_ui.py` requires the current
local Vite server; its API and bridge boundaries are synthetic and isolated.

Retained official Linux CLT26.0.0.851 / SDK26.0.0.105 API26 declarations:
`ets/component/common.d.ts` declares `onBackPress?(): void | boolean` and its
true/false ownership semantics; `ets/api/@ohos.web.webview.d.ts` declares
`runJavaScript(script): Promise<string>`; `ets/api/application/UIAbilityContext.d.ts`
declares `moveAbilityToBackground(): Promise<void>` (phones supported since API12).
No invented native API or asynchronous `onBackPress` return is used.

## Remaining acceptance

Actual left/right edge gestures, IME consumption order, ArkWeb event dispatch,
real native picker/viewer dismissal, OS background transfer scheduling and device
foreground restoration remain unverified. The original 1.0.12 rollback is intact.
This source fix is to be combined with the independently owned microphone fix
before one release candidate, version increment, onsite injection/signing and
phone acceptance; this task does none of those release actions.

## Official compile checkpoint

The existing `build_memoh_web.py` → `package_memoh_web.py` → `materials.py` →
`build_api26.py --entry hvigor` pipeline completed for the corrected source.
`CompileArkTS` actually ran in `assembleHap`; build and qualification exit 0.
The 166 existing ArkTS warnings are retained in [official-build.txt](official-build.txt).
[verification.json](verification.json) binds all 106 current inputs to source
identity `a65777ac451d2bdda34bd94c77731297eb00fd10d21ac906f8a3510134a3d520`.
The 43 existing adaptations and public 8192-byte empty configuration are verified.

The private compile-only HAP is 37,232,788 bytes, SHA256
`cef4f606c8b92716174ca945ecc2cd6ecc12cab67624f5ae274f422071bed82b`.
ZIP CRC, source metadata, compiled ABC/back symbols and Web entry are verified;
`module.json` is byte-identical to the retained rollback. No binary is committed,
shared, signed or published. Version stays 1.0.12/1000012 because the final combined
bugfix release and its version increment belong to the unified release writer.

Local logs: [unit green](unit.txt), [task-detail red before correction](task-detail-red.txt),
[native seams](native-seams.txt), [Web types](web-typecheck.txt),
[lint](eslint.txt), [source registration](source-check.txt).
Local checks do not imply remote CI passed; exact-head Actions results must be
checked after push. Human QA remains unchecked.
