# Restored client Web build against the unified candidate

Current follow-up: [runtime configuration and native download source integration](harmony-native-integration.md). Web packaging is verified; native compilation and phone acceptance remain blocked.


This follows source restoration commit `163774003827af2ab3fc1785f9324e2c571884b4`
and pins Memoh `40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8` with UI
`f69402a55dd87a59c10722fe11507ad8fd7048f5`. The existing recovered overlay builder
is retained. Obsolete Web/SDK type-repair patches already superseded by the
unified upstream are removed; current template owners and attachment scope
fields are preserved. No official Agent core is modified.

The recovered attachment adapter now preserves `metadata.delegation_url` for
verified image reads and saving originals. Scoped reads use the existing SDK
API base and authorization header, reject redirects, and never fall back to
ordinary Bot/hash access. The native-save call forwards the exact scope to the
previously reviewed native download adapter. The existing asset panel forwards
that metadata too. This establishes the caller contract; the older native
router still needs the corresponding command dispatch.

## Recorded checks

The existing native-pnpm Web build completed with Node 24.19.0, Python 3.12.14
and pinned pnpm 10.27.0. Frozen dependency installation, patched-dependency
verification, full `vue-tsc` and Vite production build all exited zero.
There are 783 packaged Web resources, with hashes in the
[build receipt](testing/harmony-unified-40f/build.json). The
[typecheck log](testing/harmony-unified-40f/typecheck.log.txt) and
[build log](testing/harmony-unified-40f/vue-build.log.txt) replace only the local
checkout prefix. Build-size warnings remain informational.

The recovered actual-adapter/patched-store test passed for upload limits,
single-file multipart, hash/receipt/readback, Bot isolation, unknown-write
reconciliation, cancellation before model dispatch and explicit references.
Added scoped cases verify the distinct Bot/session/delegation path, denied
401/403/redirect responses without broad fallback, malformed-scope rejection
and exact native-save arguments. These use controlled transport boundaries.

System Chromium opened the actual production build at a local preview URL,
at 412 by 915 pixels. The login page rendered two empty inputs with no page
exceptions. One external font stylesheet request was observed; no NAS account
login or live transfer was attempted. The screenshot and raw browser evidence
remain in the private workspace. This is login-page smoke evidence, not full
runtime, phone or human acceptance.

## Native build boundary

No HAP compiler ran. The official Linux CLT/SDK is absent, and one normal visit
to the [official download entry](https://developer.huawei.com/consumer/cn/download/)
failed with `net::ERR_CERT_AUTHORITY_INVALID`. No certificate bypass, alternate
browser or repeated attempt was used. Required historical toolchain identity:
Linux x64 CLT 26.0.0.851, SDK API26 / 26.0.0.105.

Native configuration/download dispatch, `ServiceUrl.ets` generation and the
runtime configuration/packager boundary remain the next source integration
step. The original archives, preceding commits and private backups remain
preserved. Historical 19 later cloud edits remain unverified; these alignment
changes do not claim to reproduce the unavailable 5a7 snapshot.
