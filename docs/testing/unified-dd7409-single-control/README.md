# Binding and single-control bug-fix candidate

This checkpoint combines verified base `dd7409e7157b440a5a62337b61bc4d6fac9cae76` with product commit `7d543f57465c185a8039853d87ea26278848332d`. The seven changed Web product files are byte-identical to final single-button source PR85 `2422db2011231d5a4e538aa0fd7b573d133cb350`; no split-button product state is included. The complete original source history is retained.

## Included and excluded

- The base retains the trusted host binding through federation callbacks, answer/approval workspace preparation and explicit recovery. Its six remote CI runs passed on attempt1. Source-owner seven local image-download proxy/SSRF failures remain recorded; normal runner passes did not erase or establish the cause of those failures.
- One existing composer button sends/queues when content exists and stops when empty, with the correct glyph and a repeated-gesture guard. Existing queue-item steering, retry identity, files, voice and permissions remain intact. Five Web palettes and system following remain; no new service, permission, model or execution protocol.
- Source single-button/palette browser evidence is reused. This combination additionally passed34 directed send/queue tests, whole Web types and a production build. Existing build chunk-size warnings are not assertion failures. No unchanged old CI or HAP build was repeated.
- Bot contact/navigation/internal-specialist-visibility work is deliberately **not included**. Its independent review commit is `5c45c97f3c47c56788ad7a137b4627c4fd66052c` on `review/private-bot-contact`, with impact assessment and explicit PostgreSQL environment failures at `docs/testing/private-bot-conversations/README.md`. It must not be deployed by implication with these confirmed bug fixes.
- Harmony single-button source is PR86 `d213ee7842fc76527de712cd5b2d825a5697a613`; it is not copied wholesale into the server/Web tree. No HAP build/sign/install occurred here. Parent-reported production remains3d5f/HAP1.0.18.

No schema or configuration migration, root/path relocation, historical session merge/deletion, queue rewrite or production operation. Schema stays177. Application rollback follows the original exact-digest workflow on the same schema, preserving unknown operations; do not use generic migration down. Docker PR is a check, not published release images. Exact combined-head CI and subsequent joint field acceptance are separate from source completion.

## Scoped cleanup and resource ownership

| Resource | Owner/dependency | Actual disposition |
| --- | --- | --- |
| Split-button product and obsolete after-screenshots/scripts | Superseded source iteration | Product is replaced by the final one-button delta. Obsolete split-button after-images and executable browser script are omitted. Original failure and current palette/gesture evidence are retained; Git source history is not rewritten. |
| Loopback18091 production Web preview, headless Chromium | This task's synthetic browser validation | Preview processes were terminated; port18091 is closed. Browser contexts closed normally. No field browser or user device was used. |
| Temporary net-source patch | This integration's consumed handoff | Removed after the exact source/product relationship was recorded in Git. |
| `apps/web/dist` in this task checkout | Regenerable local build output | Rebuilt from the bug-fix branch after leaving the isolated contact review tree, so no contact-enabled build is presented as this candidate. Not tracked or deployed. |
| `/tmp/unc-integration` tool/module/build caches and evidence | This integration and its existing checkpoints | Still used for verification and rollback evidence; not deleted as redundant and not a resident product service. No global prune or shared dependency cleanup. |
| Existing stopped development PostgreSQL volume | Original cloud development environment | Preserved unchanged. No production database or account was used. |
| Failed recovery copy under `/tmp/unified-7b-77-78/local-memory-diagnosis/pg-recovery/cluster` | This task's attempt to restore its existing synthetic PG dependency | Copy ownership/hash validation could not finish; the old recovery container attempted startup and failed directory access, then was explicitly stopped. Copy cleanup remains incomplete due filesystem ownership; no chmod/chown, alternate-access workaround, new credentials or original-volume deletion was performed. It is not accepted runtime evidence. |
| Existing running development pgvector container | Shared original development dependency | Left running and unchanged; isolated-test database creation failed with SQLSTATE53100. No replacement service was created. |

Therefore "zero residue" is not claimed: valid historical evidence/caches remain required, and the failed PG copy has an explicit cleanup blocker. Production and irreversible cleanup are outside this task. Human/device QA remains unchecked; no deployment or paid model operation is claimed.
