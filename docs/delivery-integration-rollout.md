# Generic delivery integration candidate

This candidate combines the reviewed delivery ancestry through
`ddb09be456002ea81466fb1560d1f6b6850b99dd` with cleaned main
`851e8da4284c1f01e7e95f9547b16e7250bf553b` using an ordinary merge.
PRs #31/#34/#39/#41/#42/#44/#46 are ancestors, not replayed cherry-picks.
It retains the main branch's compact Pi completion, explicit diagnostic log
collection, Media cold-scan/Range behavior and removal of specialized image
workflows. It adds no image task routing or new scheduling service.

The combined compact completion and 16-file declaration originally exceeded the
existing Windows launch limit. The fixed Memoh-owned projector is now gzip-packed
in its envelope and decoded by the already required Node built-in `zlib` before
evaluation. Only the build's embedded projector enters this code string; task
input never does. Its source, module identity and worker semantics are unchanged.
The original protocol/fault tests exercise the packed module. This changes no
Agent installation and requires no new host dependency. Source/config identities,
immutable queue snapshots, finite actions and delivery IDs remain unchanged.

## Review and test boundaries

- Core boundary: compare official runtime/contextview using
  `bash scripts/check-agent-core-boundary.sh`; compare all other Agent files to
  the cleaned main parent as well.
- Integrated service tests cover compact results without a log store, delivery
  progress after completion, strict feedback binding and concurrent ledger CAS.
- Original transfer fixtures cover resume, unknown outcomes, cancellation,
  native private handles/locks/publication and bounded retention. New metadata
  errors retain their evidence and do not starve independent valid manifests.
- PostgreSQL fixtures create and drop isolated databases. They exercise the full
  new database migration chain, canonical init alone, and an explicit
  170 -> 171 -> 172 upgrade with preexisting device/operation rows. Upgrade does
  not enroll a credential or alter the existing control token; ledger-erasing
  old writes fail after 171.
- Linux Server/PG upload tests and Windows NTFS HTTPS-fixture tests are distinct.
  Windows Server/PG download-publication tests are also distinct from real
  installed Herdr/PC/NAS acceptance. CI links and the exact candidate/ZIP hashes
  belong in the integration PR.

## Deployment sequence to execute separately

1. Review the exact merge SHA and its CI/package digests. Back up the existing
   database and release configuration. Retain the original NAS paths and volumes;
   this integration introduces no production directory move. Confirm the deployed
   database is clean schema 170 and inventory all operation-writing processes.
2. Quiesce old operation writers before enabling any delivery ledger. Using the
   existing configuration and candidate `memoh-server` binary, inspect
   `memoh-server migrate version`, run `memoh-server migrate up`, and inspect
   `memoh-server migrate version` again. This candidate must end at clean 172:
   171 installs the operation ledger guard; 172 adds empty device file-credential
   fields. Migration 173 from the independent cross-Bot line is not included.
   Do not use `force`, skip migration numbers, or permit mixed old/new writers.
3. Start the candidate application consistently across writers, retaining existing
   Bot/target grants and connection/device/instance mappings. Verify account and
   device authorization, no-store responses, existing Media references and an
   ordinary compact computer result. Missing file credentials remain unavailable;
   application migration does not create them or turn control tokens into file
   identities. Existing connection/OAuth faults remain separate prerequisites.
4. For the selected already authorized device, first read owner-only
   `GET /users/me/runtimes/{id}/transfer-credential`. The specific initial
   enrollment/rotation, private credential storage, plugin installation and any
   new trust/security configuration are separate unexecuted actions. When those
   actions are resolved, the owner setup endpoint can enroll once; a lost setup
   response requires a status read and explicit current-ID rotation, not blind
   re-enrollment. Do not place the owner account JWT on the PC.
5. Install the fixed bundle through existing Herdr facilities. Its supplied
   config/state locations remain private local NTFS. The existing approved
   `source_root` must match the registered execution root; an authorized native
   SMB UNC task root is separate from queue storage. Keep the credential in the separate
   private `device.credential` file, never argv/logs/model context. Use normal
   HTTPS hostname/certificate validation and the already approved channel; the
   code does not repair ACLs, alter trust, open ports or create a daemon.
6. Exercise one original operation with a declared generic artifact, then bounded
   multiple files, upload interruption/restart and Range/hash-checked download.
   Confirm original delivery/receipt identities, NAS committed references,
   cancellation/unknown races and revocation; native command acceptance alone is
   not delivery. Preserve source files and unresolved evidence. Real route testing
   remains an authorized project goal, not a newly imposed global approval gate.

## Recovery boundary

Stop finite delivery triggers and resolve active/unknown associations before
changing the release. Keep database/queue evidence and NAS originals. Dropping
172 removes file-credential fields and dropping 171 removes the ledger guard;
structural down tests do not make that safe after enrollment or committed work.
Prefer retaining the schema with a compatible guard-aware writer, or restore a
coordinated reviewed backup. Do not let an older writer silently erase a ledger,
reset delivery IDs, replay Pi, or delete unknown/blocked data to recover capacity.
