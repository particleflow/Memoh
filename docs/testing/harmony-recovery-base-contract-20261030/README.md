# Harmony HC-03 / HC-05 recovery and API-base follow-up

## Identity and scope

This is an independent successor to `51ccf9222ed9e2312aa3ddfaad03084cfeaacaa3`. That requestTag commit is not amended. No HAP build, version bump, signing, deployment, permission or credential change is included. Version remains 1.0.34 / 1000034; [existing package proof](package-unchanged.json) records the unchanged original unsigned bytes.

The consumer materialized the original audit ZIP using the current official Library helper, verified 66,548 bytes, SHA256 `ff0f181ea3f1d7d6c5ab8cf38f0f1a7cbb25700b63ef7e0942fa80c1fb032c4d` and all ZIP CRCs. [Consumption proof](audit-consumption.json) matches the audited uploader/URL sources at `fcc39b31` byte-for-byte to parent51cc. The later package revision was not downloaded: its owner explicitly confirmed HC-03 / HC-05 inputs unchanged and permitted reuse of this verified original. No claims about newly added HC-06 are made here.

## HC-03: recover the original receipt by progress and server timing

Original counterexample: the fourth committed-but-503 chunk stopped with `UPLOAD_RETRY_LIMIT`, despite a healthy original receipt with confirmed progress. [Local red](recovery-red.log.txt) reproduces this using the actual ETS through the retained Node Kit-boundary worker and real HTTP/files. It is not evidence of device corruption, duplicate creation or a real TLS failure.

The owned `NativeResumableUpload.ets` now:

- Removes the per-upload cumulative recovery counter and the separate fixed429 retry count. It does not substitute a larger count or introduce another configurable-budget framework.
- Reconciles only the original receipt before another PATCH after409/429/5xx or an ambiguous PATCH exception. Confirmed progress resets the delay, not identity. Create and complete remain single-send; unknown Create, unavailable original receipt and unresolved complete retain their existing non-replay rules.
- Persists optional `retryAt` / `retryDelay` in the existing version1 journal before waiting. Old records without these fields remain readable; invalid new numbers are rejected. Tokens remain absent from this journal.
- Retains the existing250ms initial backoff, doubles it while no confirmed progress occurs, and obeys the server's Retry-After seconds or HTTP-date as a lower bound. A larger cooldown is not truncated into an earlier retry. When it exceeds the actual receipt's expiry, waiting ends at expiry with the original journal/source retained and **no early network retry**. Receipt lifetime is not extended or replaced by a PC lease.
- Bounds active recovery by the actual receipt expiry and existing authorized owner/epoch. No count exhausts a valid progressing transfer, and a nonprogressing transfer cannot busy-loop indefinitely. The only timer clamp is the portable2147483647ms scheduling slice; a longer wait rechecks the same real deadline.
- Allows only the already-admitted official RCP transient network codes to repeat a failed read/no-progress transport attempt. An unclassified PATCH exception may reconcile the original receipt; if no progress was confirmed, the original error is rethrown. Permission, TLS certificate, local integrity and invalid receipt errors are not broadly swallowed. HTTP401/403 are not retried or rerouted.
- Wakes a pending timer on pause/revocation/abort/end. Pause retains the checkpoint; explicit abort keeps the existing cancellation semantics. Page detach and background ownership remain in the existing owner; no second queue or transport service is introduced.

[14 initial green cases](recovery-green.log.txt), [one additional expiry case](recovery-expiry-green.log.txt), and the RCP header-shape follow-up below cover3/4/8 dispersed committed failures, four lost replies, four conflicts, Retry-After seconds/date, two-hour cooldown cancellation, real process death/restart during cooldown, receipt expiry without a busy loop, immediate pause,403, and full bytes through root/safe-prefix API paths. Successful cases assert one Create, one complete, full source/hash, same receipt and rejection of another identity. Committed-fault cases assert no repeated confirmed offset. The long-cooldown/expiry case sends no receipt GET or PATCH before the cooldown and stops with the pending source intact.

Official RCP ResponseHeaders also permits a single value as string[]. After the initial implementation commit42f6c002, a focused [red counterexample](recovery-array-red.log.txt) confirmed `[Retry-After]` was being ignored. The minimal follow-up unwraps only a single-element array; it does not reinterpret duplicate lists. [Final string/date/array shape evidence](retry-after-shapes-green.log.txt) verifies the legal single-array cooldown and rechecks the HTTP-date boundary. The initial14-case log's `headersCooldownRespected` summary flag only covered numeric/restart cases, underreporting date coverage; the date assertion itself already passed. The reporter is corrected for future runs, and the original log is retained. There are16 distinct recovery scenarios, not16 plus duplicated cases. Final official API26 compilation below covers the corrected native implementation.

Reproduce from repository root:

```sh
node docs/testing/harmony-recovery-base-contract-20261030/recovery-check.cjs
# Run one new boundary without repeating the other cases:
node docs/testing/harmony-recovery-base-contract-20261030/recovery-check.cjs --case retry-after-beyond-expiry
```

This uses local synthetic HTTP, not a device or real credentials. Existing unknown-Create/404/complete/hash, background lifetime and requestTag evidence is reused; it is not counted as newly rerun.

## HC-05: use the configured API base, without appending api

The public GitHub integration supplied the fixed [Go contract](upstream-api-base.go.txt) at `046023913592b00951d7f28fe54842080c5cdc51` and [Windows vectors](upstream-api-base-v1.json) at `c879d1622eeadcf02ec4b16590ade2cee6e1ac85` (`memoh.file-api-base.v1`). [Source provenance](upstream-source-provenance.json) records their actual hashes. The local recovery checkout did not contain the Go file; it was not falsely treated as read locally.

The TS URL validator and Python private-injection validator no longer require an `/api` suffix. Root and arbitrary safe API prefixes are accepted as configured; no fallback, automatic `/api` insertion, implicit origin switch or credential migration is added. The native ServiceUrl is still generated from the one TS source by the unchanged packager. Existing host, credential, encoding, dot-segment, port, query/fragment and LAN-TLS rules remain intact.

Two real consumer seams also needed root handling: `NativeTransferNetwork.resource` and the existing SDK diagnostic lease had appended `/` to URL.pathname `/`, accidentally requiring `//`. Both now compare against the same trimmed configured prefix. The SDK phone source's SHA is updated in its existing paired mapping; its upstream/source lock and generated endpoint schema are unchanged. The existing form text is corrected to describe root or configured prefix instead of falsely requiring `/api`.

[Local red](base-red.log.txt) is `SERVICE_URL_API_PATH`. [Green](base-green.log.txt) verifies:

- Eight root/prefix/port/IPv6/LAN examples in JS, native and Python; ten malformed/unsafe inputs rejected by JS/native; no alternative authority accepted.
- All30 Windows vectors in JS/native, with the **existing explicit public-HTTP compatibility exception reported**, not hidden. The HTTPS root/path cases now agree; this change does not claim every host/TLS policy is identical across clients. The pre-existing public HTTP legacy/migration support and stricter reserved-address rules are not removed or broadened into a new routing policy. Private LAN HTTP is still rejected, and physical-path/real-secret HTTP authorization is unchanged.
- Root configuration survives a read/restart and equivalent trailing-slash save with login intact. An explicitly different API base uses the existing identity reset; an unbound old login cannot be attached to a typed address.
- The existing Web resource guard accepts the configured root/prefix, rejects another authority, and the native request guard dispatches the exact original URL.

[Two actual generated-SDK tests](sdk-base.log.txt) assert exact root/prefix `/users/me` and Bot-scoped FSDownload URLs, retained authorization, byte equality and cross-authority rejection before dispatch. The35 unrelated tests are intentionally skipped in this run. [106 original URL/storage checks](service-config-focused.log.txt) pass via the explicit `--config-only` selector; default script assertions are not removed.

```sh
node docs/testing/harmony-recovery-base-contract-20261030/base-check.cjs
node mobile/client/tests/stage2/harmony_service_config.cjs --config-only
pnpm exec vitest run apps/web/src/lib/harmony-diagnostics-api.test.ts --project web --testTimeout=15000 -t 'API base:'
```

## Compilation, actual page and retained failures

Committed text logs remove terminal color escapes and trailing whitespace only; original raw logs remain preserved in the task evidence directory.

- [Official API26 CompileArkTS](api26.log.txt): BUILD SUCCESSFUL using retained SDK26.0.0.105. Narrow task only, no assembleHap. Existing warnings remain in the log.
- [Full generated Web typecheck](types.log.txt) and [scoped lint](lint.log.txt) pass. A new SDK test initially treated SDK data:unknown as ArrayBuffer; the [TS2769 failure](types-before-fixture-fix.log.txt) is retained. A runtime instanceof guard fixed that test without changing the SDK or weakening assertions.
- [Current generated page](root-page-result.json) ran on `http://127.0.0.1:5173/` via the existing Vite development command and desktop Chromium390px. Initial form issued no API request, encoded path failed, root save reached the actual login page and persisted exactly the configured root. This is browser application evidence, **not ArkWeb/native installation/TLS or full backend health**. [Form screenshot](root-service-form.png), [login screenshot](root-service-login.png); both were inspected. Synthetic storage/endpoints only, no real login or paid call. [Reproducer](root-page-check.mjs) accepts the current loopback Vite URL as its argument.
- Full old stage2 script still fails due to its missing ClientDiagnostics bridge adapter: [before](service-config-baseline.log.txt), [after](service-config.log.txt). Its default test remains intact. The older unit script has an obsolete unbound-login save expectation: [before](service-config-unit-baseline.log.txt), [after](service-config-unit-with-dependency.log.txt). Both were reproduced from parent51cc source, not dismissed by assumption. Neither is claimed green; neither required changing product identity behavior. The UI test's old `/not-api` negative fixture is changed to a genuinely malformed encoded-separator path, since safe prefixes are now valid.
- [193 source inputs](source-check.log.txt), [exact changed hash/mapping proof](source-map.json) and [privacy scan](privacy-scan.json) are retained. No SDK/CLT, private endpoint, real token, signing material or private configuration is published.

Only HC-03 / HC-05 source remediation is handed off. No new HAP has been built, no live NAS/Windows/phone was touched, no production transfer was retried, and HTTPS throughput/phone acceptance remains unverified. New source must pass the original independent reviewers and a later matched package phase before device acceptance.
