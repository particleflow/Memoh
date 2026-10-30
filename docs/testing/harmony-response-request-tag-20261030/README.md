# Harmony optional response correlation (2026-10-30)

## Fixed scope

Base source: `fcc39b31b1db11e7a9d7396e34c94d5b3301a778` (1.0.34 / 1000034).
Shared Web/SDK base remains `7a5f2bb2f7eda2a00c3af6ef6f64281c0b45475f`, with the existing Home.vue `2c0e277c2cb49b5a4a99b89a5e568ef9f413ab8a` increment. No source lock, SDK, configuration, version, permissions, signing identity, artwork or application UI change.

Only the existing media/delegation original-file Response hook adds optional `requestTag` to its `headers` event. `responseRequestTag` reads exactly `X-NAS-Request-ID`; only 32 lowercase hexadecimal characters are accepted. This header name/format is the explicit diagnostic contract. No endpoint, user, Bot, file, device, port or current sample is hardcoded into product code.

Missing, CORS-hidden, inaccessible, invalid and Fetch-combined duplicate values are omitted. A failure before Response has no headers event or tag. The event retains the existing document, sequence and request context. `headers` still means JavaScript received Response, not wire headers, TTFB or first body byte. No complete header collection is recorded.

The JS event type, emitter and strict batch validator and the ArkTS journal whitelist accept this optional field only on `headers`. Untagged retained batches remain valid. Queue/unknown-write/PUT/ACK/cursor, authorization, business retry, original byte reading and integrity validation are unchanged.

The native response forwarder already comma-joins array-valued headers. A controlled counterexample found case-alias duplicate keys were overwritten into one apparently valid value. The four-line native change omits only this diagnostic header when more than one case-insensitive key exists; all other forwarding, status, logical URL and body behavior remain unchanged. The installed SDK declares RCP ResponseHeaders string / string[] values without a key-case normalization guarantee, and ArkWeb setHeaderByName has an overwrite flag. No wire-level duplicate behavior or real device result is inferred from the adapter test.

## Narrow evidence

- [JS red](js-red.log.txt): original34 has 5 failures / 6 passes for the new contract; [green](js-green.log.txt): 11 pass, 24 unchanged tests intentionally skipped. Covers real generated SDK original/delegation reads and byte/hash preservation, missing/hidden/duplicate/invalid/read-error values, pre-Response failure, emitter context and strict validation.
- [Native journal red](native-red.log.txt) / [green](native-green.log.txt): new legal field rejected before change; accepted/persisted/reloaded/ACKed afterward, legacy untagged body accepted, wrong phase/type/value and raw header collection rejected. Owned ETS is transpiled with Kit adapters and real temporary file I/O; not a device test.
- [Native forwarding red](native-forward-red.log.txt) / [green](native-forward-green.log.txt): case-alias overwrite reproduced then omitted; array duplicates stay combined and invalid, unrelated headers unchanged; no requests sent.
- Official API26 `default@CompileArkTS` using the retained SDK 26.0.0.105: [log](api26.log.txt). This is a narrow compilation, not assembleHap or a new package. Existing SDK warnings remain recorded.
- Generated mobile Web full typecheck: [log](types.log.txt). One test getter fixture initially produced TS2352, retained in [failure log](types-before-fixture-fix.log.txt); fixed by using a genuine Response with a throwing property getter, without assertion casts or product changes. Scoped ESLint: [log](lint.log.txt).
- [Source check](source-check.log.txt): 193 inputs / 209 tracked source files / seven approved PNGs. The historical 19 later changes remain explicitly unverified; this work does not claim their recovery.
- [Changed source hash map](source-map.json) and [unchanged existing HAP proof](unchanged-build-boundaries.json). No new HAP, package version, signing, upload of binaries or deployment.

Commands (from the generated checkout for Web; repository root for native/source):

```sh
pnpm exec vitest run apps/web/src/lib/harmony-diagnostics-api.test.ts --project web --testTimeout=15000 -t 'request tag:'
pnpm --filter @memohai/web typecheck
pnpm exec eslint apps/web/src/lib/harmony-diagnostics.ts apps/web/src/lib/harmony-files.ts apps/web/src/lib/harmony-diagnostics-api.test.ts
node mobile/client/tests/stage2/harmony_diagnostics_journal.cjs --request-tag
node mobile/client/tests/stage2/harmony_diagnostics_journal.cjs --request-tag-forward
python3 mobile/check_source_subset.py
hvigorw default@CompileArkTS --mode module -p product=default -p module=entry@default -p buildMode=release --no-daemon
```

## Supported CORS visibility and remaining boundaries

[Controlled Chromium results](cors-result.json), reproducible with [the local script](cors-check.mjs), exercise the exact extracted product helper. Two ephemeral loopback origins use an existing standard credentials/Authorization CORS flow and synthetic credentials only. Same-origin and explicitly exposed cross-origin tags are read; cross-origin hidden, duplicate and invalid tags are absent. Every response retains the same seven body bytes and SHA256. No CORS bypass or relaxed browser security is used. Run from this directory with `node cors-check.mjs`; the test writes its result only to this directory.

Local inherited `internal/server/server.go:73-78` exposes `X-Request-ID`, not `X-NAS-Request-ID`. This is source inspection, not a production readback. The new response header therefore requires the same-origin path or explicit server exposure through the already supported CORS mechanism. This patch does not add exposure or change the custom-scheme CORS policy. NAS sampling remains off / not applied; its producer namespace and termination window belong to the original HTTPS owner.

No real NAS, actual ArkWeb runtime, phone, public download page identity bridge or live throughput was tested. The controlled browser test is not a full application/phone UI test. Only narrow source and isolation verification was requested for this phase; no new UI was introduced. The published34 binary does not contain this change. A later approved combined package must include JS and ArkTS together. Matching tags alone do not establish producer anonymity, TCP F correlation, first-byte timing, transfer speed or resolution of the HTTPS slowdown. No new service, permission, real secret transport or paid/model request is involved.
