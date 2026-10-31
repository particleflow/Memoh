# Reusable device identity for delivery

This increment supersedes the bootstrap proposals in `task-transfer-principal.md`
that required a separate grant or native plugin attestation for each file. A
trusted, paired private PC can reuse one configured Memoh device credential.
The native Runtime invokes the configured finite helper; invocation acceptance
is not proof of completion. No Agent core modification is required.

`DeviceTransferVerifier` authenticates the configured credential to a stable
connection/device/instance tuple and rechecks revocation. It does not accept an
account JWT or an Agenvo OAuth/control token. The implementation remains an
injected port, disabled when absent; this candidate supplies synthetic fixtures,
not production credentials or a production credential database.

`Memoh-Transfer-Context` contains bounded, nonsecret task/file lookup selectors.
They are not claims of authority. The existing transaction checks current Bot
permissions, session ownership, operation fields, attempt/generation, target
fingerprint and the authenticated device's connection/device/instance. The
existing v3 ledger binds the immutable reference with revision CAS. Changing a
selector cannot select another target or rewrite a prior delivery. No new store,
registry, pairing flow, file-token issuer or manual per-file approval is added.

Each HTTP request gets a fresh internal `VerifiedTransfer` context, valid at most
15 minutes. This deadline bounds asynchronous verification; it does not expire
the device credential. Credential lifetime, rotation and revocation belong to
the configured verifier. Checks before/after scans and before responses retain
their snapshot semantics: revocation cannot retract bytes already sent or an OS
publication already in flight. Unknown outcomes must use the same delivery and
receipt, never a new execution attempt. PR #27's capacity retry fix is retained.

The device credential permits file activity only for operations assigned to its
current target and approved by the existing Bot/task permissions. This trusts the
authorized PC, rather than attempting to attest a hostile host. Device enrollment
must bind the correct existing connection and instance; request metadata cannot
choose that tuple. The caller user can differ from the device owner, provided the
existing target mount and Bot permissions authorize that user's operation.

Production remains disabled. Initial installation, the concrete Memoh device
credential provider and its persistent verifier record, private credential
storage, HTTPS hostname/trust and any rotation policy require explicit approval
before configuration. Reusing control transport does not make its credentials
valid for inbound Memoh file requests. HTTPS validation is mandatory; no HTTP or
certificate-verification bypass is supplied. No real PC/NAS was contacted.
