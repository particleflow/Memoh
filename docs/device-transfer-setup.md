# Reusable device transfer setup (disabled by default)

This increment implements the actual database verifier and Server wiring. It does
not enroll a production device, change trust/network settings, install software,
or activate transfers. No separate service or per-file credential is introduced.
The existing Agenvo pairing/control connection remains unchanged.

## Existing storage and authentication boundaries

Migration 172 adds one purpose-specific credential ID, SHA-256 digest, immutable
connection/device/instance tuple digest, and optional expiry to `user_runtimes`.
There is no new registry, plaintext credential column or signing key. A random
256-bit secret authenticates the PC with `Authorization: Memoh-Transfer ...`.
It is distinct from the account JWT, Agenvo OAuth token and `mrk_` runtime token.
The same credential is reused for multiple files, retry and original receipt
recovery. The internal fifteen-minute task context does not expire the device.

Every credential read rechecks active owner membership/account, runtime, HTTP MCP
connection ownership/activation and the original device tuple. Unrelated Pi
configuration changes do not silently retarget the credential. Every task request
also reuses the current operation CAS, Bot/session/target/attempt permissions and
immutable reference binding; knowing an operation ID never grants access.
Transient verifier storage failures return 503, not an authorization revocation.

Rechecks observe fresh database snapshots, not an atomic lock held through file
I/O. Revocation cannot recall bytes already sent or a local publication already
committed. Async association rechecks before and after verification; existing
local publish/fence/resolve semantics still govern download cancellation. Files
are not retention-pinned. Business completion and file delivery remain separate.

## One-time owner setup, after explicit approval

1. Fully integrate migrations 171 and 172 and compatible execution/delivery writers.
   Do not mix old writers with the delivery ledger guard. Set
   `[files].device_transfers_enabled = true` in the existing Server configuration;
   `files.originals_root` remains the existing resolved root. Missing originals
   fails startup; there is no new NAS root or legacy fallback for this feature.
2. Verify the existing HTTPS origin and certificate trust on the selected PC.
   Neither the current HTTP deployment nor a synthetic CI certificate proves this.
   The finite client requires HTTPS and normal certificate validation.
3. The authenticated owner administers the *existing* Agenvo runtime at
   `/users/me/runtimes/{id}/transfer-credential`. `GET` returns only credential ID
   and optional expiry. `POST {"expected_id":""}` enrolls once; explicit rotation
   supplies the currently observed ID. Optional `expires_at` is a device policy,
   not a per-delivery timeout. `DELETE ?expected_id=<current ID>` revokes with CAS.
   Account and computer ownership are checked. Responses use `no-store`.
4. The secret is returned once over HTTPS and saved privately as `device.credential`
   in the resolved `HERDR_PLUGIN_CONFIG_DIR`, beside `transfer.json`. Do not place
   it in command arguments, logs, model context, source checkout or NAS originals.
   A lost enrollment response cannot recover the stored secret: read status then
   explicitly rotate with that ID. Automatic repeated enrollment is rejected.
5. Link the fixed reviewed binary directory using the existing official Herdr
   plugin mechanism. Herdr supplies config/state roots; queue state remains in
   `HERDR_PLUGIN_STATE_DIR`. The optional producer export root is resolved once
   from `transfer.json` and is separate from config/state. No default drive or
   production installation path is invented by this patch.

Initial enabling/enrollment, secret placement, binary installation and any TLS
trust change are separate precise production actions requiring approval. This
code performs none of them. The intended PC/runtime tuple, actual HTTPS trust and
private paths must be confirmed from the deployment, not synthetic fixtures.

## Upgrade, rollback and remaining producer work

Apply migration 172 before starting this Server build, even with transfers
disabled: generated runtime queries include the new columns. Down 172
removes credential fields and invalidates enrolled device credentials; disable
transfers first, then roll back. Re-upgrade requires explicit enrollment. No file
originals, queue receipts or production volume locations move. Down 171 has its
separate ledger compatibility requirements; do not erase delivery state to roll
back. Canonical schema and incremental migrations both include these fields.

The finite CLI, queue, HTTP adapter, destination publication and task association
are implemented. Producer integration is a separate increment: carry non-secret
operation/BatchProgress metadata and a closed logical artifact manifest to fixed
stdin enqueue, then invoke finite advance from an existing task-result event.
This patch does not claim that automatic producer handoff is already wired or
that the Windows package is installed. No new daemon/Agent core edits are needed.

Tests use synthetic credentials, isolated PostgreSQL databases, test HTTPS trust
and native Windows fixture directories. They are not production acceptance.
