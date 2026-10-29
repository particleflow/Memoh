# Local fast-path integration above 370922

This is a local source candidate, not an image, package, deployment or network-policy activation.

## Fixed provenance

- Integration parent: `370922014a2435b83d4a2c33cd0b0611bc85de11`, tree `916311fc228447ae941dbecaf3fdd492f4bc61ff`. Its six natural CI runs succeeded on attempt 1; none was rerun for this integration.
- Source parent: `b928247f9c0d185de4aa1f255742d843d26ea664`, tree `04fb3bc57062acb43accbed426423e94c5f8e30a`.
- Shared source chain: `808cf8010a50b5f7d291221249591d000e7d85aa` → `9e968f8cd0e21bea78339f206585a66a3ca281ef` → `d1f2c0b943423c5899b18e28b3fd5d9bb56143a1` → `68c01296981f2d0c173226c5a3590ca4ad59e0fa` → `abb2f6970d78cd433e5f055719ad866efcd45fba` → `b928247f9c0d185de4aa1f255742d843d26ea664`.

The actual merge had no conflicts. All 15 production paths equal the final source blobs. All 50 paths changed between b5 and the integration parent, including timing and PR93, remain byte-identical to that parent. The source's 87 changed paths, including its original tests and evidence, remain exact. The only integration additions are this record and two seam tests. No migration, configuration, workflow or native ETS file changed.

## New checks and reused evidence

`apps/web/src/lib/fastpath-combination.test.ts` adds two passing SDK transport seam checks:

1. After an authenticated v2 profile, Files upload maps to the physical endpoint while timing-enabled playback stays on the original logical API. The original bearer, trace header, timing response, streaming bytes and abort signal remain intact.
2. A mapped Files transport failure is not replayed. A PR93 task-history read retains its original session/operation and unknown status on the logical API; a subsequent 403 remains a 403.

Direct Vitest: 2/2 passed; ESLint on the new test: exit 0. A source guard compared the final product blobs, prior integration paths, original storage implementation and untouched migration/configuration/workflow paths. Its first draft used a nonexistent journal filename and failed before producing a report; the corrected guard checks the actual `media-transfer-storage.ts` and passes. This was an audit-script path error, not a product failure.

Reuse the original author's endpoint validation, original receipt/byte/hash recovery, eight focused routing checks, 71 affected Web checks, types/lint, actual application/browser evidence, and upstream-import tests. Do not describe those as rerun on this merge. No new full build, full test matrix, CI, application browser session or phone test ran here. The two new tests exercise the combined SDK wrapper; they do not re-prove PR93's server authorization, which is byte-identical and has its own prior evidence.

## Preserved execution and recovery boundaries

The route adapter changes dispatch authority, not the logical API origin, owner, task, HostBinding, journal key, source, receipt, chunking, queue format or recovery algorithm. `media-transfer-storage.ts` is unchanged. No new operation or retry is introduced. HTTPS verifies the physical authority with system trust; redirects, identity mismatch and proxy conflicts still refuse rather than select another endpoint.

Omitted policy and supported v1 retain the original route. Existing strict v1-only consumers reject v2; that is explicit refusal, not capability negotiation or a safe reason to reset queues. Installed Windows 57's support has not been established by this source merge. Keep its original policy/path until its actual paired producer is established; do not rewrite `transfer.json`, HostBinding or queue identity. Existing v1 queues are not migrated here.

The native three-file input is still the original owner's complete `a158c56488ed193ec3e9f2d57480ad4f16a58068` packet, not part of this NAS/Web merge. Its separate generic fetch/resource bridge is still owned by that module. Do not enable `fileRouting: 'platform'` until both approved transport and resource adapters own that mapping below their original identity checks. No HAP was built, and an existing HAP does not acquire these changes from this merge.

An ordinary HTTPS browser still blocks an HTTP physical endpoint. The author's non-loopback HTTP page experiment passed Files/preview, but Web Locks and OPFS were absent and the old secure-origin journal was not accessible at the new document origin; persistent upload refused before creating another receipt. Those are real platform restrictions, not removed guards or an accepted replacement for the original durable path. The original source and journal remained preserved. External provider/CDN downloads remain on their original supplied URLs; this is not an all-traffic or upstream-provider rewrite.

## Handoff

The exact final map is [final-path-combination.json](../transfer-http-entry/final-path-combination.json). Share the eight changed Web production files and two generated SDK files, together with the final commit/blob map, with the existing Harmony bridge owner. Server-side policy and producer changes are paired source inputs, not proof of installed-consumer acceptance. No new endpoint configuration, HTTP credential transmission, CORS policy, TLS setting, service or public exposure was applied.

Schema remains 180. Recovery must preserve schema180/history and compatible queue/receipt consumers; this merge adds no schema or data migration. Reverting its source does not prove an older strict client can consume an already-enabled v2 policy. Any later activation must keep the original safe route or explicit refusal for unmatched consumers. No production rollback was executed.
