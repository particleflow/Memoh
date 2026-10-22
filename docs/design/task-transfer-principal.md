# Task-scoped file adapter and bootstrap proposal

This candidate remains unregistered in production. It adds no credential issuer,
device registration, daemon, listener, Agent change, filesystem root or security
configuration. Its HTTP verifier is an injected port; tests provide synthetic
verified principals. Account JWTs and outbound Agenvo/model credentials are not
execution identities.

## Implemented adapter

`VerifiedTransfer` binds the existing Bot/user/session/run/target, operation,
device/instance, attempt/generation and one delivery to an exact direction,
content hash, byte count and MIME. It has a purpose-specific audience, an opaque
credential selector, issue time and expiry (at most 15 minutes). The selector is
not a secret or authority. Only a trusted verifier may return this value.

`ComputerTransferHandler` accepts `Memoh-Transfer` authorization only in
`/internal/computer-transfers/{operation}/{delivery}`. Under that prefix it mounts
the existing file protocol paths and Commit/Resolve handlers. `NewTask` selects
this namespace; `New` retains the original API. A missing verifier returns 503,
including for a valid account JWT. Invalid credentials return 401; binding or
current permission failures return 403. Routing/auth responses are no-store.
The ordinary account file routes and global JWT policy are not loosened.

The adapter establishes the trusted context after verification, reuses the
transactional operation/target/permission checks, and compares all URL/body/file
selectors. A download cannot select another hash. An upload must match the exact
file grant. Raw bytes still use Media; no file payload, log or credential is
saved in the operation row. Range responses use the existing stream and full
integrity verification, then recheck the task before exposing bytes.

Ledger v3 adds a reserved file binding and upload receipt association using the
existing `SaveComputerDelivery` revision CAS. Reserved means authorized scope,
not delivered: Resolve projects it as absent. Existing per-account upload quota
is preserved because the UploadStore owner remains the account ID, not a new
per-attempt identity. Cross-delivery receipt reuse is rejected even for the same
account and content hash. Upload Commit requires its bound receipt to be ready.

Before Create, CAS saves a creation fence. After Media persists a receipt, CAS
attaches its ID before the response is acknowledged. A known receipt makes a
repeated scoped Create idempotent. Definite pre-creation quota/validation/busy
refusals release the fence. Storage errors, crashes or uncertain DB outcomes
cannot prove non-creation: they retain an unknown fence and never blindly create
another receipt. A crash between Media creation and binding still requires
explicit reconciliation; this does not invent enumeration or change PR8's
protocol. The queue's existing create_unknown does not automatically replay it.

Async validation keeps the verified principal, never its raw credential. The
injected verifier rechecks revocation/expiry at request admission and after the
scan. Database rights are checked separately in fresh transaction snapshots.
Expiry or revocation during scanning leaves the existing lease pending; it
cannot publish an association. A later authorized request can use the existing
bounded lease-reclaim mechanism. No credential is minted/refreshed automatically.

Authorization is a snapshot: revocation cannot retract bytes already sent,
an admitted filesystem operation or an already-published local file. A lost
response can follow a durable mutation. Reconciliation retains that uncertainty;
cancel never claims to undo a published winner.

## Compatibility and verification

Migration 171 is required; its existing monotonic-version guard already rejects
v3-to-v2 downgrade or ledger removal. No new table or migration is introduced.
Current execution CAS preserves the entire ledger. Old delivery writers reject
v3. Do not run an old delivery writer against upgraded operations, remove the
guard, or activate this adapter before the full candidate is integrated. Legacy
entries cannot silently acquire incoming PC authority. Existing old-lease tests
from #19 remain in the integration base.

HTTP tests use the actual adapter with a synthetic verifier and real Media
uploads. PostgreSQL tests exercise a NOBYPASSRLS connection, upload/Commit/Resolve,
lost acknowledgement, execution CAS preservation and the v3 downgrade guard.
Windows tests use real NTFS publication plus the actual HTTP adapter and PG,
including credential revocation during the held scan. Their originals are
preseeded; they do not test Unix upload persistence on Windows. No manual account
JWT context is injected into the new composition.

## Proposed minimum bootstrap, requiring separate approval

No bootstrap in this section is implemented or enabled. A production verifier,
issuer, approval UI and host credential provider are still missing. This is a
concrete approval proposal, not a claim that a safe existing channel was found.

1. **Identify the server.** Use the already-approved HTTPS ingress and normal
   certificate/hostname validation. If that route is absent, stop this step and
   separately approve its exact certificate/trust/network configuration. HTTP
   is unencrypted; there is no insecure fallback or new port in this proposal.
2. **Approve a particular PC invocation.** A finite action in the existing Herdr
   plugin requests a short-lived pairing transaction from Memoh over HTTPS. It
   displays only a public user code. The user signs in to the existing Memoh UI
   on a trusted browser, checks that code on the intended PC, and approves the
   existing device/instance/operation/attempt and exact file manifest. The PC
   agent never receives the user's account token. Device names supplied by the
   requester are not proof; human confirmation of the code on the intended host
   is essential. This is delegation, not hardware/device attestation.
3. **Deliver narrow authority.** The PC redeems its private pairing secret over
   HTTPS after approval. The issuer returns a short-lived opaque file grant,
   scoped exactly as above; no wildcard Bot/file access. Server-side grant hash,
   expiry and revocation epoch belong to the existing operation ledger/CAS,
   not a new service or registry. The current patch does not write those fields.
4. **Store only the grant on PC.** Propose a generic Windows Credential Manager
   entry owned by the current user, initially limited to the logon session;
   journal/config/log/terminal output contain only a logical grant ID. Explicit
   expiry/deletion is still required: Windows session persistence is not a TTL.
   Finite plugin invocations can read the current scoped grant. This protects
   against accidental plaintext storage, not a malicious process with the same
   user authority. No existing user ACL needs to be changed for this proposal.
5. **Revoke and expire.** The owner can revoke the grant using the operation CAS.
   The verifier checks that epoch and expiry on each request and after scanning;
   account/target permission revocation remains independently effective. Expired
   credentials pause transfer; receipt, snapshot and delivery IDs remain stable.
   Reauthorization does not rerun the business attempt.

The pairing shape follows the user-code/device-secret separation of
[RFC 8628](https://www.rfc-editor.org/rfc/rfc8628.html), but there is no existing
Memoh device-authorization server to reuse. A future implementation needs its
own reviewed endpoints, rate limits, bounded polling and expiry. It must stay
inside finite existing plugin actions and the current Server; a timeout stores
only protected pairing state or expires it, without installing a retry daemon.

The proposed Windows storage uses the documented
[generic credential and persistence modes](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw)
and [current-user credential set](https://learn.microsoft.com/en-us/windows/win32/api/wincred/nf-wincred-credwritew).
Those APIs are not called by this patch.

The minimum mode requires renewed user approval when its short grant expires.
Unattended long-running renewal would require a separately approved device key
or renewal capability, its storage, maximum lifetime and revocation policy; it
must not be silently added to the initial pairing approval.

Separate approval items are: (a) exact plugin package/install and Windows user;
(b) exact HTTPS/trust/network changes, only if needed; (c) limited pairing and
grant issuance with the named scope/lifetime; (d) session credential storage and
deletion; and, only if requested later, (e) persistent unattended renewal identity.
There is no omnibus "full access" approval and no full account token on the PC
execution host.
