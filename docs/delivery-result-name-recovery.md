# Exact result filenames and historical recovery boundary

## Implemented: future declarations

The existing `source_ref` is both the logical identity input and the exact
physical basename opened by the producer. Previously its syntax excluded dots,
so a declaration such as `windows_photo_a0_result` could not name the actual
`windows_photo_a0_result.png`. A zero Pi exit did not establish a closed manifest.
The closure and collector correctly did not guess a suffix.

Future requests can explicitly declare `windows_photo_a0_result.png` in the
existing `artifact`/`artifacts` shape. The Server and installed project producer
adapter both need this change. The filename is immutable inside the original
operation/attempt/generation/RootHash plan and original request fingerprint.
The closure reads only that exact basename after execution completion, binds
the original task and declaration into the manifest, and records exact bytes
and SHA-256. The collector compares the original attempt context before consuming
that manifest. HostBinding, protected source opens, private queue storage, source
file identity, indexing and original receipt reconciliation remain in force.
No Agent core, new service, protocol version, configuration, migration or UI is
added. Completion is still separate from committed delivery.

The allowed extension syntax changes filename validation only. It does not
change the delivery ID calculation or serialize a new field. Old declarations
and journal records retain their literal names and IDs. Queue/receipt IDs still
reject dots. Paths, ADS, device stems with extensions, links/reparse points and
case aliases remain forbidden. Subdirectory outputs remain unsupported.

## Not implemented: an already-completed operation with a different basename

Changing the old `source_ref` to add `.png` would change its delivery ID and
request/context digests. Do not do that, rename/copy the original, scan a
directory, infer a suffix, rerun Pi, regenerate the image or submit another task.
There is currently no verified safe mapping repair entry point. The following
is a design prerequisite for a separate implementation, not a callable API or
authorization to change an existing operation.

1. **Reconcile unknown results first.** Read the original operation and validated
   completion normally, then the private journal, exact manifest and any known
   upload receipt through their existing authorized boundaries. Use existing
   receipt GET/resolve and association readback for unknown outcomes. An OS
   refusal is unknown, not absence. An empty NAS ledger is not evidence that
   Create/Complete did not execute and never authorizes fresh registration.
2. **Explicitly bind a physical basename.** A future repair must persist an
   explicit mapping from the unchanged original source_ref/deliveryID to one
   exact safe basename. Bind operation, Bot/user/session/target/run, attempt,
   generation, immutable task RootHash, original HostBinding and target
   fingerprint, original request hash, raw producer-context SHA-256 and canonical
   PlanHash. Also bind the validated original completion receipt (attempt,
   generation, request hash, Pi session and original workspace/pane/terminal),
   receipt digest and verified compact completion hash/size. Bind the original
   protected file identity, canonical MIME and independently verified exact
   source hash/size. Do not fabricate any proof absent from the original task.
3. **CAS before side effects.** Under the existing operation/job fencing and
   durable ownership, commit an immutable canonical mapping digest before local
   closure/queue admission. The same ID and identical complete tuple replay
   idempotently. Any conflicting filename, bytes, context, receipt, root, config,
   identity or attempt rejects; no overwrite or last-write-wins. Keep original
   context, request hash and logical declaration untouched. A path alone, JSON
   hash alone, uncommitted mapping or model output is not authority.
4. **Resume each interrupted stage with that same ID.** Before the mapping is
   committed, no manifest/admission/HTTP effect is allowed. After it is committed,
   reconcile that exact digest before creating or consuming any closure. A crash
   between closure and registration replays the same mapping and journal ID;
   after registration reconcile that journal, never another delivery. After
   unknown Create/Complete/Commit use original receipt/outcome readback. If Create
   is unknown without a resolvable receipt, stay unknown; do not POST again.
   A committed NAS entry must match the original tuple and bytes before it can
   become a reference in the original session. A mapping does not bypass
   cancellation, current grants or server reservation/revision checks.
5. **Retain source protection at every restart.** Open only the explicitly bound
   basename under the original pinned task root. Reject symlinks, reparse points,
   hardlinks, root/config changes, revocation, replacement or hash/size changes.
   Keep all private local queue protections. Confirm only the exact existing
   metadata owned by that task; never delete the artifact. A missing/unreadable
   proof or unsupported native boundary blocks that recovery action.

The reported field operation and its 2,107,300-byte PNG are untouched by this
patch. Its OS-denied manifest read and possible upload outcome remain unresolved;
no local fixture below establishes their state. A future repair needs the above
verified entry point and targeted crash/conflict tests before activation.

## Isolated differential evidence

Baseline: `565c0b4b3386d87d057234efe6828479d35fcee0`. The PC owner's existing
`photo-closure-contract-repro.py` evidence (old 0894 and candidate 62d: two
no-suffix mismatch failures, two exact-name hash/size controls) is reused, not
rerun or relabeled as field proof.

New tests first rejected explicit `.png`/`.pdf` declarations and the exact-name
Bash launch on the unchanged baseline. After the syntax/source registration
change, focused tests cover:

- Actual generated Bash envelope, synthetic Pi JSON fixture, valid 3,629-byte
  PNG SHA-256 `a39279b774d8d7856de528fbb4aaaa71862ff05c90866357e6234f35e4196be5`,
  validated compact completion receipt, unchanged original context, closed
  manifest and exact bytes. Duplicate launch does not rerun the fixture Pi or
  recollect. This is not the field PNG or an actual model call.
- Two real test processes: first durably registers an explicit `.png` filename
  and exits; the second reopens its original task, delivery ID, source identity
  and HostBinding, replays idempotently, indexes and verifies 196,731 real
  synthetic bytes, SHA-256
  `0b47937f6daa62dd80ce7a807ee1295a53035a6044e12a9ce19a9d35316f3891`.
  No source payload copy or HTTP request occurs. Same-ID filename/hash conflicts
  reject and manifest consumption keeps the original file.
- Foreign operation/attempt/generation/RootHash/declaration rejection, strict
  filename syntax and case aliases, unchanged historical delivery ID, and native
  Unix link/hardlink/revocation/path/stream boundaries for the new filename.
- Native task-entry fixture preserves the explicit filename and original plan
  through completion. With no committed ledger it remains pending and produces
  no file reference or imported bytes; task dispatch stays one.

Command: `go test ./internal/deliverymeta ./internal/agenvo ./internal/transferclient
./internal/computertask -run '^TestExplicit' -count=1 -v` (run on Linux).
The HTTP-blocking test client retains the production `*http.Transport`/TLS
validation boundary; no custom RoundTripper bypass is permitted.

This evidence covers project adapter behavior only. Native Windows/UNC opens,
actual Herdr/Pi, production NAS commit and original-session readback require the
unique field writer after integration. Web and Harmony receive only already
committed existing references; this patch is not new end-to-end acceptance for
either client. Full application/UI screenshots and human acceptance have not
been performed here. No deployment or production restoration is authorized by
these isolated checks.
