# Harmony1.0.36: scoped input-source projection

This increment consumes the admitted input-source fix0ec25b7e0b0de46349cc1555ba86248062d9c1ea
from unified cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f
(tree ce11e378aeffab4152c954ab256b7ff62284e36d). It changes the actual generated
phone production inputs, rather than relabeling the1.0.35 package. The exact
10 shared production and2 SDK files are listed in [source-mapping.json](source-mapping.json).
The pinned consumer map is the upstream
[consumer-map.json](https://github.com/particleflow/Memoh/blob/cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f/docs/testing/session-input-source-review-656/consumer-map.json).

Assistant segments inherit only the corresponding user-turn source within the
receiving Bot/session. Missing, foreign, invalid or unknown historical sources
remain unknown. Source metadata is not an authorization grant. The phone does
not invent a source or downgrade automated-task checkpoints.

## Exact phone adaptations

- Preserve the existing optional native media origin callback argument in
  message-item.vue; incoming source-label markup and behavior remain unchanged.
- Add only chat.inputSources in each existing phone locale. Keep phone auth,
  attachment and all unrelated locale keys; do not revive removed recovery UI.
- Keep all382 existing SDK operation bodies. Add the admitted source types;
  omit the unused delivery/recover function export absent from this phone SDK.
  Normal automatic result delivery is server-owned. No manual recovery UI is added.
- Preserve the existing native prepareAttachments option in chat/types.ts. The
  initial generated-context typecheck exposed three missing-option errors after
  the upstream file replaced that host adaptation; restore the original field
  without changing source semantics. The initial failure log is retained.
- All other mapped inputs match the authoritative upstream SHA256 exactly.
  Other paired entries and all native transfer/auth/configuration sources remain.

The existing HC-03 retryAt/Retry-After/original-receipt behavior, HC-05 safe API
root, optional requestTag, readonly task-directory footer, contact/history links,
read-aloud, native attachment/background queues, login migration, approved
artwork, identity and permission scope are retained from the qualified35 source.

## New verification

[projection-red.log.txt](projection-red.log.txt) records the old35 consumer losing
assistant source on the real runtime_snapshot reducer path. The same supplied
reproducer passes on the new mapped phone context in
[projection-green.log.txt](projection-green.log.txt). Its printed candidate name
is the original reproducer fixture identifier65649801; the checked production
file bytes are the admitted0ec hashes in source-mapping.json.

[segments-green.log.txt](segments-green.log.txt) passes exact-segment/foreign-actor,
missing-upsert/prior-frame and receiving-scope-change checks.
[seam-verification.json](seam-verification.json) checks all12 incoming and phone
hashes, exact adapted differences, and preservation of other paired entries.
The scripts execute production TypeScript from the generated phone context:

```sh
node docs/testing/harmony-unified-1.0.36/runtime-source-repro.mjs <checkout> <checkout>/node_modules/typescript
node docs/testing/harmony-unified-1.0.36/projection-combination-check.mjs <checkout> <checkout>/node_modules/typescript
python3 mobile/check_source_subset.py
```

Prior author/independent source, security and UI evidence is reused from the
[admitted review](https://github.com/particleflow/Memoh/blob/cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f/docs/testing/session-input-source-review-656/README.md).
These local source tests do not constitute physical ArkWeb, authenticated live
WebSocket, screenshot, model execution or real-phone verification. The current
cloud build has no physical phone. No such acceptance is claimed or fabricated.

The final actual generated context passes vue-tsc and production Vite build.
All47 host-adaptation names are preserved, all162 paired entries match their
source hashes, and802 resources were generated. Original chunk-size warnings
remain recorded; no budgets or verification checks were loosened.

## Consumption and rollback

1.0.36 is a generic API26 release unsigned handoff. Its8192-byte private
configuration slot stays empty. Only the original Windows writer injects the
existing private configuration, signs and verifies with the original profile,
certificate and device scope. Do not return configured/signed private output to
GitHub. No new keys, permissions, addresses, services or SDK downloads are needed.

The matching new NAS versioncef must be deployed by the separate field owner.
Automated-task resumev4 is incompatible with the old9270 backend. Do not strip
input_source, rewrite checkpoint versions, clear queues or restore an old dump to
force compatibility. Keep35 and history as recovery evidence; this is not a
promise that a backend downgrade afterv4 writes is safe.35 does not include the
input-source fix. Phone installation/TLS/throughput remain unverified here.
