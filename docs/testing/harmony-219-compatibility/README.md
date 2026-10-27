# Harmony 1.0.21 compatibility with server 219/schema179

Reviewed product base `6e6c336ea24f628da86b4eb86794e28e64f08e3c` (qualified 1.0.21), shared baseline `feffaf66be4bc8999479fc76bd6a87be1a3b3789`, target `219a722da3b8e4a316164011dd106350dfab2c9e`. NAS deployment/schema179 is parent-reported; no production/device access was performed by this review.

## Exact compatibility result

Only three Web/SDK files differ upstream: `useNativeTaskResume.ts`, its test, and `types.gen.ts`. Existing generic media upload/download endpoints, attachment URL/receipt references, sidebar/contact/history navigation, native transfer and image timing inputs are unchanged. Task `inputs` is an optional new list of existing authorized NAS hashes; omission preserves the old task fingerprint. It does not replace the phone-to-NAS transfer protocol. Input preparation and snapshot advancement are server/Runtime work; the server's new cloud tests do not establish phone transfer acceptance.

The cancel observation response adds optional `applied`, `completed_at`, `error_class`, `server_source`. These do not change the original cancel request/ack scope or authoritative run state; old readers may ignore them. Missing observations stay unknown. `applied` is only the observed return; `completed_at` is not run completion, and `server_source` is not an authenticated actor. No new cancel UI is added by this patch.

**One confirmed 1.0.21 limitation:** its active native task resume reader rejects the new `inputs` key. On the exact new upstream test against the old reader, 31 cases passed and one failed because the expected explicit resume request was never sent. It does not silently drop hashes or create another operation, but the new input-bearing task cannot be explicitly resumed through that old client. This is narrower than a general upload/download/navigation incompatibility.

## Source correction and validation

Consume exactly the upstream reader/test delta: allow `inputs` only as an array of lowercase 64-character SHA256 strings and preserve the complete original JSON in the same authorized operation's resume request. Add the optional resume and cancel fields to the existing generated SDK declarations; no endpoint, protocol, operation identity or generic retry is introduced. The two Web modules and two changed declarations match 219 verbatim.

The original red result is preserved. After composition: all32 resume tests, complete generated Web typecheck and changed-source lint pass. Exact45+2 adapter checks and170 source entries pass. Native upload/download, configuration/TLS/login, committed references, original-session navigation and image timing source inputs remain byte-identical to the old branch. Existing1.0.21 unsigned HAP and handoff ZIP hashes were checked unchanged; no signed private artifact was accessed or rewritten.

## Handoff boundary

1.0.21 remains compatible with the unchanged existing chat/media/navigation contracts and additive cancel observations. It does not include the new input-bearing resume fix. A future package is needed to expose that particular corrected feature, using the next unused version after the unified source is locked; there is no reason to rebuild merely to match a server SHA or schema number. This task builds no HAP, changes no version/signature/download index and deploys nothing. Physical phone connection, native TLS/file behavior and actual input task execution remain unverified. Do not call mocked resume tests phone/production acceptance.
