# Garnet composer and appearance

Base: `3d5f145c452dda5e17861d401de049b7ddded2e6`. Independent UI branch; no deployment, Agent core, binding backend, new protocol, service, management surface or HAP build.

## Behavior

The composer uses **one button**, as explicitly requested. While running, the existing content predicate (trimmed text, files or skills) selects the send arrow; empty or whitespace-only input selects the stop square. Sending calls the original follow-up path; stopping calls the original abort path. Unsupported queued attachments/skills remain in the composer. The original queue-item promotion, invocation gate and same-identity failure retry remain unchanged. Explicit `/steer` retains its meaning.

The same DOM button survives the icon/action change. Mouse multi-click continuation is ignored; after sending, rapid taps or keyboard repeats on the newly empty button extend a 500ms quiet window and cannot become abort. A fresh activation after that quiet window stops normally. No extra button, confirmation, protocol or request is added. First-send and attachment preparation behavior is retained.

Appearance retains all five schemes (`memoh`, `ocean`, `forest`, `rose`, `amber`) and their light/dark variants. Default and realtime system following, persistence keys and explicit user preferences are unchanged. The existing appearance row now names its three modes visibly and stacks at narrow width. First-run appearance adds the missing system choice. Dark brand foreground uses the existing background token so its icon remains readable on the light brand fill; no raw color or platform palette is added.

Wireframe: existing composer text/queue rows → existing model selector → one send/stop slot. Existing appearance row → existing segmented mode control; existing scheme selector. No additional page or panel.

## Verification

Environment: prepared Go server + embedded Channel, PostgreSQL/pgvector and Vite, current source at `http://127.0.0.1:18082`. Backend rebuilt from the base and healthy; real authenticated `users/me` read. Bot/session/model/queue HTTP and runtime WS data in the browser are synthetic, with no real model or device dispatch. This verifies the mounted application, event handling, outgoing request identities and rendering, not backend execution acceptance.

- Original baseline: 55 existing tests passed for queue/submission gate, rendering, send context and upload lifecycle. The current single-button delta reran only its 19 queue/submission tests, affected lint, token guard, typecheck and actual browser scenarios; no production build was repeated.
- Whole Web typecheck, affected ESLint and UI contract passed (three inherited contract warnings).
- Current Chromium delta: the same DOM button shows exactly one correct arrow/stop affordance. Real mouse double-click, rapid touchscreen taps and held Enter on the button never turn a cleared send into abort. A deliberate fresh empty click stops. Whitespace, textarea Enter/Shift+Enter/IME, failure with same-invocation retry, explicit queue-item promotion, busy/idle change and retained busy attachment passed. No WS message or immediate steer was introduced.
- 390×844 Chinese dark composer: the single button switches correctly between send and stop with no horizontal overflow.
- Actual appearance controls selected all 10 scheme/mode combinations. First default is system; browser OS emulation changes it live. Explicit dark ignores a subsequent light OS change, survives reload along with amber, then selecting system restores live following. Browser proof contains measured primary text, muted text and brand icon contrast. Primary text minimum 14.98:1, muted text minimum 5.19:1, brand icon minimum 3.60:1. Disabled-control contrast is not claimed.
- Original busy-square defect reproduced by temporarily running the exact base composer, then restored and rerun on the final modified source. Screenshots were inspected. Initial harness errors (dev-source API glob, incomplete fixture arrays and option accessible names) were corrected in the harness; they were not product failures.

Current evidence: [content → send](single-button/single-content-send.png), [empty → stop](single-button/single-empty-stop.png), [390px send](single-button/single-narrow-send.png), [390px stop](single-button/single-narrow-stop.png), [gesture/request proof](single-button/single-button-proof.json). Earlier `busy-*.png` and the earlier browser script describe the superseded split-button iteration and are historical only. The unchanged palette screenshots and ten-variant measurements remain in `theme-*.png` / `browser-proof.json`. The current regression script is `single-button/browser.mjs`.


## Platform handoff / remaining acceptance

Harmony source inspected: `78c89c278494f750ad45708837487b00e3a34192` (existing 1.0.18). Its builder preserves all five scheme selections and adds a separate warm-white mode from the centralized `ui_theme.json`. Preserve all of these and its existing native attachment, voice, login, media and explicit warm preference. Do not copy the whole phone branch into Web or rebuild the old HAP for unrelated Go-only changes.

The ArkWeb shell omits `.darkMode(WebDarkMode.Auto)`, whose default is off according to the [official OpenHarmony guide](https://github.com/openharmony/docs/blob/master/zh-cn/application-dev/web/web-set-dark-mode.md). Native owner must consume the accompanying source handoff so system changes reach the existing Web settings watcher and `client.theme` bridge. Force-dark conversion must remain disabled. Native shell status/navigation backgrounds continue to use the existing centralized contract, not per-client raw colors. Warm mode and the five schemes must remain selectable after applying the shared delta.

The phone builder uses exact text seams: simply copying the new shared files into its old archive would break the old appearance template seam. Apply the shared UI delta after the existing phone adaptations or update those precise seams. Native file/busy checks must include `pendingNativeAttachments` / `nativeAttachmentBusy`; retain its existing upload pause control. Generated-phone integration and ArkTS/device acceptance are separately reported by the source handoff, not inferred from this Web build.

The pre-bootstrap Harmony service-address form has hardcoded warm colors and loads before the shared settings store. This separate setup surface was identified; it needs coordination with its source owner before claiming all native setup surfaces follow all existing preferences. No service-binding/configuration code was changed here.

Desktop imports `@memohai/web/style.css`, shared routes/pages and settings store (`apps/desktop/src/renderer/src/main.ts`); it consumes these shared changes when its owner builds the candidate. No Electron/Windows executable, OS live change, native window chrome or physical Windows/phone acceptance was performed. Human QA remains unchecked. Final composition, exact-head CI, publication/deployment and existing field run verification remain with unified line `01a15840-f35b-757a-a2a0-e20479062007` and the original native owners.
