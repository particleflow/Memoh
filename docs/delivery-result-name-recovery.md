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

## Implemented: explicit historical basename mapping entry

Changing the old `source_ref` to add `.png` would change its delivery ID and
request/context digests. Do not do that, rename/copy the original, scan a
directory, infer a suffix, rerun Pi, regenerate the image or submit another task.
The project now provides a narrow owner preparation/dispatch seam and a finite
local entry. This is not a model tool, management UI or new HTTP route. It does
not automatically repair operations or authorize use against a field operation.

The trusted host calls `Service.PrepareProducerResultRecovery` using its existing
`HostComputerBinding`, original operation ID and logical source_ref, explicitly
selected flat physical basename, independently known original SHA-256/size, and
already recorded original HostBinding. Unknown original bytes or binding are
missing prerequisites; do not manufacture them from current configuration or
guess from a filename. The owner loads and reauthorizes the original completed
operation, exact original plan/request fingerprints, target snapshot, validated
completion receipt and result. Its current ledger projection carries absence,
an unknown-create fence, or the known original receipt without changing a row.

`Service.RecoverProducerResult` dispatches this metadata through the original
verified dedicated control pane to the existing producer's `-action recover-result`
stdin. It uses the existing packed Bash launcher and rechecks target, pane,
terminal and workspace ownership. A pane acceptance is not recovery evidence.
No task, Pi process, credential, endpoint or permission is created. Trusted local
host callers may use `transferclient.RecoverResultSource` with that prepared
envelope; the envelope is not a credential, and untrusted/model JSON is not an
authorized caller. The existing native producer locator/config/state are used.

The local entry has two modes determined by existing evidence, not a retry flag:

- Owner-confirmed existing receipt: only the original task-scoped receipt GET is
  permitted. No mapping, journal progress, producer feedback or HTTP mutation is
  performed. A mismatched/denied/unavailable receipt stays blocked/unknown.
- Owner-reported remote absence PLUS authoritative local queue/history absence
  under the original admission/job locks, no existing original manifest, verified
  original protected context/receipt/completion and exact original source hash:
  commit one mapping in the existing private queue registration. Remote absence
  alone is insufficient. Unknown/corrupt local state, a creating fence, canceled
  or conflicting existing record, or an in-flight original manifest stops this
  entry. It never advances an upload itself.

The existing NAS GET middleware calls `ReserveTransfer`, even for receipt reads.
Consequently a local receipt plus an absent/unknown NAS binding is **blocked
before GET** here; do not label that request read-only or use it to populate an
empty ledger. Owner-confirmed existing bindings make the reservation a no-op;
the normal guard still rechecks identity and current grants.

1. **Reconcile unknown results first.** Read the original operation and validated
   completion normally, then the private journal, exact manifest and any known
   upload receipt through their existing authorized boundaries. Use existing
   receipt GET/resolve and association readback for unknown outcomes. An OS
   refusal is unknown, not absence. An empty NAS ledger is not evidence that
   Create/Complete did not execute and never authorizes fresh registration.
2. **Explicitly bind a physical basename.** The entry persists an
   explicit mapping from the unchanged original source_ref/deliveryID to one
   exact safe basename. Bind operation, Bot/user/session/target/run, attempt,
   generation, immutable task RootHash, original HostBinding and target
   fingerprint, original request hash, raw producer-context SHA-256 and canonical
   PlanHash. Also bind the validated original completion receipt (attempt,
   generation, request hash, Pi session and original workspace/pane/terminal),
   receipt canonical digest and verified compact completion hash/size. It binds the original
   protected file identity, canonical MIME and independently verified exact
   source hash/size. Do not fabricate any proof absent from the original task.
3. **CAS before side effects.** Under existing durable job ownership, the first
   synchronized private journal frame commits the immutable mapping digest and
   metadata registration together, before indexing or HTTP. No second mapping
   store or synthetic source manifest is created. The same ID and identical complete tuple replay
   idempotently. Any conflicting filename, bytes, context, receipt, root, config,
   identity or attempt rejects; no overwrite or last-write-wins. Keep original
   context, request hash and logical declaration untouched. A path alone, JSON
   hash alone, uncommitted mapping or model output is not authority.
4. **Resume each interrupted stage with that same ID.** A process exit after
   proof validation but before registration leaves no mapping; an exit after the
   synchronized registration frame reopens exactly that mapping and journal ID.
   Both resume without Pi or HTTP. Complete corrupt frames or an unprovable
   empty/torn initial journal fail closed rather than being rebuilt as absence.
   Active checkpoint compaction retains the whole mapping. After
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

The mapping is stored only in the existing job journal, version **6**. Existing
unmapped jobs retain their previous versions and bytes. Older producer writers
must not handle mapped jobs: their strict decoder/version gate rejects the new
field/version. No global queue layout migration or old-job rebinding is added.
Each configured source reopen verifies the original protected context, receipt,
compact completion, identities and mapping. Existing resumable indexing and
Merkle reads verify actual content; reopening does not rehash the entire file.
The explicit recovery admission performs a streaming full hash first, within the
existing finite command deadline. Deadline/permission/hash failures do not admit
the mapping. No power-loss durability claim beyond existing fsynced journals is
made.

The reported field operation and its 2,107,300-byte PNG remain untouched.
OS-denied evidence, unknown upload state and unknown original configuration or
source hash remain real blockers. This code does not remove those prerequisites.

## New mapping-entry isolation evidence

Starting from `1b7200d768c1231179725cfa2f85c8c3452882fb`, targeted tests cover:

- Two real process exits, after proof validation and after durable registration;
  each resumes the original logical source_ref/deliveryID/HostBinding with
  131,109 real synthetic bytes, SHA-256
  `4f5ba56a0c5f8cd270f89b44aa4cfbf98f9df693967cea77d48628a8a3b5d028`.
  Original protected reads and journal checkpoint compaction retain the mapping.
  No payload copy, generated source manifest, HTTP request or Pi call occurs.
- Same-ID different name/hash, missing hash, traversal, wrong root/config/context,
  receipt/generation conflicts, unknown remote/local Create, existing manifest,
  source chmod revocation and link/hardlink rejection. Reopening after context,
  receipt, compact completion or source changes blocks before HTTP.
- Existing receipt readback against an isolated TLS fixture: GET=1, all HTTP
  mutations=0, original journal bytes unchanged. Revocation returns 403 without
  journal changes; absent NAS binding blocks before GET.
- Original owner load/complete-receipt validation, original control-pane command
  dispatch, owner/grant rejection and preservation of the original operation and
  unknown-create/known-receipt ledger facts. This uses a native transport fixture,
  not actual Herdr or Pi.
- Actual CLI decoder/config/private queue/feedback path, duplicate registration
  under the old logical ID, with no HTTP or private metadata in command output.

Command: `go test ./internal/transferclient ./internal/computertask
./cmd/transferqueue -run '^TestResultRecovery' -count=1 -v` on Linux.
Windows ACL/UNC/reparse behavior, actual production host activation and UI/field
acceptance remain unverified. The explicit entry is not called automatically by
task observation, and has not been invoked for any real operation.

## Reused explicit-filename differential evidence

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
