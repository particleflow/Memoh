# Native specialist delegation integration

Base: `69ef2fac2ceeaf68458f0f68bc30d0e8a31872ee`, whose six CI runs passed on attempt1. Product source: PR88 `d88e3bc2ff8333a2d54f424a57aa6cb2d600cd82`, retained by clean `-x` cherry-pick `912e85dec518f5a6930b45b0b88eecd62074a594`. The cherry-pick's complete tree equals the source tree `2759d4a1e7f34f90c6fd81040da8619823456a69`; no earlier branch history was merged. Documentation-only source `c840a8f706d35f4709a2a793bf5bb2977e80abdd` was separately reviewed and applied: one Markdown file, eight unavailable log links made explicit, original failure disclosures retained. No cleanup implementation or configuration branch was mixed in.

## Necessary change

The existing specialist planner and registration authority still selected only legacy Agenvo registrations, rejecting already-supported native `memoh` remote Runtime mounts. The source's two controlled red cases identify those separate gates. The planner now reuses existing native Runtime decoding and the owner-scoped activated registry, checks each Bot's own mount of the same Runtime, immutable target fingerprints and current permissions. History remains readable under its existing checks; no new legacy execution, OAuth, credential, grant, service, runtime or transport fallback is enabled. NAS selection named `native` is distinct from a registered remote Runtime's provider.

Account/device authorization remains necessary. After normal login and device connection, the host must carry and recover the same conversation authority for chat, queued work and explicit recovery; missing host context is a product failure, not a new user-maintained session permission. No new session or repeated authorization is prescribed by this fix. Real missing/revoked Bot/device permissions must still be rejected. The original operation, unknown state and source-conversation result route are not replaced or replayed.

## Differential verification

- This workspace ran only seven relevant top-level native planning/registration/permission/idempotency tests with race detection: 53 test/subtest PASS events, zero FAIL/SKIP. Both changed packages passed (core1.109s, delegation1.132s). These use synthetic hosts/registries; no real Runtime, Herdr/Pi, model or credentials were contacted.
- The source's broader three-package tests, race/vet/lint/build and red/green evidence are reused from [its record](../../validation/native-bot-delegation/README.md), not relabeled as repeated tests here. Its real local application check was startup/login only and did not validate the affected business flow.
- Official Agent runtime/contextview boundary still matches `bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.
- [Git-tree equality and actual test events](content-and-tests.json) prove that all Web, SDK, Swagger, migration and configuration directories are byte-identical to69ef. `mobile` is absent from both candidate trees: a zero diff there does not verify the separate Harmony branch or signed package. This increment changes no shared wire/API and does not modify that phone branch. No API/schema generation, Web rebuild or signed HAP recompilation was needed for this backend-only increment.

Command:

```sh
go test -race -json ./cmd/internal/core ./internal/botdelegation -run '^(TestNativeDelegationPlanUsesExistingDistinctMounts|TestNativeDelegationPlanFailsClosed|TestNativeRegistrationBindsDistinctBotsAndReconstructsScope|TestNativeRegistrationRejectsLiveChangesAndForgedIdentity|TestNativeHandoffReturnsToSourceAndNeverRedispatches|TestNativeTextAndOriginalReadsDoNotGrantExecution|TestLegacyRegistrationRemainsReadableButCannotPlanNewDevice)$' -count=1 -timeout=180s
```

## Version and acceptance boundary

| Component | This combination |
| --- | --- |
| Server | Adds the precise native specialist authorization/planning repair to the existing binding fix. |
| Web and SDK | Exactly69ef, including its one content/empty-sensitive send/stop button and existing palettes/system following. |
| Harmony / signed HAP1.0.19 | Parent reports1.0.19 already signed. No wire/shared API change or phone-source edit here; do not rebuild/re-sign it for this increment. The separate phone source SHA, package hashes/signing/device acceptance remain with the original client owner; absence of mobile in this tree is not phone-byte verification. |
| Windows / producer | Existing contracts and snapshots unchanged. This is not acceptance of unfinished Windows layout or a real PC task. |
| Bot contact structure | `review/private-bot-contact` is separate and excluded; no canonical-contact routing, internal-visibility rollout or historical session migration occurs here. |
| Storage / rollback | Schema177 and configuration are unchanged. Original histories, receipts, queues and originals remain. Application rollback uses the previous exact image digests on the same schema, with active/unknown-task checks; never generic migration down. |

The final integration SHA must pass its own required CI before a single candidate image release. No mirror dispatch or NAS deployment has occurred here. Parent-reported production remains3d5f;69ef was not released. Field verification is joint after the fixes, with the original authorized source/specialist context and original receipt/operation. No extra user session permission, paid model sample or blind unknown replay is requested. Human QA and actual affected Runtime/delegation/attachment return remain unconfirmed.

The original source-owner image-download proxy/SSRF failures and the separate contact-review local PG capacity/ownership failures remain distinct evidence. This backend synthetic race pass resolves neither environment issue and does not convert them into green acceptance. The earlier duplicate-publication lookup URL rejected by the GitHub tool is not retried through another URL; the original publisher owns legal lookup/dispatch and supplies the resulting run ID for manifest verification.
