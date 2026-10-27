# NAS originals as native task inputs

`computer_task_submit` accepts an optional `inputs` array of existing canonical
SHA256 media references. Each original must be readable in the bound Bot scope.
This is metadata on the existing task tool, not a new media endpoint, transport,
service, execution fallback, credential, or approval workflow. The normal task
authorization (read/write/exec, selected Runtime and existing policy) still runs.
The 16-reference bound is per request and is not an aggregate storage budget.

```json
{
  "kind": "herdr_pi_task",
  "task": "Read the supplied input and produce the requested result.",
  "inputs": ["<existing-lowercase-sha256>"]
}
```

No caller-provided NAS path, URL, filename, destination, source Bot override or
file length is accepted. The service opens the existing seekable media stream
and obtains its actual length/MIME in that same Bot scope. The immutable input
plan binds the original operation, Bot/user/session/target/run, attempt,
generation and target RootHash. Old requests omit the new field and retain their
original serialized fingerprint.

Before native dispatch, the existing Runtime file RPC client (files only) writes
the exact original stream under the original selected Windows target root:

```text
.memoh-computer/inputs/<original-attempt>/<sha256>
```

It does not interpret a NAS `/data` path as a Windows path, use the private
download publication cache as the task path, or make a local server-side copy.
It validates the existing drive/share-to-root directory entries and each fixed
child using the existing listing API (Stat alone follows links). No ancestor is
created; only the fixed metadata subdirectories inside the existing root can be
created. Existing conflicting files are preserved and cause rejection. The
atomic streaming writer checks source size/hash and fresh grant before EOF;
the service then reads back actual destination bytes and checks size/hash.

The generated production task envelope gives Pi host-prepared JSON file
references containing full Windows paths plus root-relative paths, size, MIME,
hash and original task identity. Before starting Pi, the existing Node runtime
verifies the immutable context digest, root fingerprint, canonical root,
ancestor/leaf links, regular-file/link-count and opened-handle identity, then
reads the whole file and checks size/hash again. Modified/missing inputs do not
start Pi. No source bytes are injected into the task prompt. This is scoped
application validation on an already trusted Runtime, not a hostile-host
filesystem sandbox: the existing file RPC has no atomic no-replace/root-handle
write port. A concurrently malicious host can mutate paths; this patch does not
claim to remove that inherited trust requirement or replace Windows acceptance.

## Recovery and authorization

If an atomic write completed but its response was lost, preparation records
`pi_inputs_unavailable` without starting Pi. An explicit unchanged submission
using the original `resume_operation_id` may reconcile actual bytes and proceed
under the original task, after current authorization and admission CAS. It does
not create another operation or resend an already verified file. Ordinary
duplicate submission and read-only observation never resume native input.
An active `preparing` stage retains its existing bounded work deadline and
3-second persistence tail. Another caller cannot take over its writes until
that deadline expires; a crashed process can then recover that same stage.
Cancellation, another user/session, changed request/target/root and paused
admission reject continuation. Original input/producer JSON, unknown historical
fields, numeric identities and NAS delivery ledgers remain preserved.

An interrupted uncommitted stream is aborted by the existing atomic writer; its
next authorized preparation retries that file from the original source. This
adapter does not add Range resume to Runtime WriteRaw. Existing media/download
Range and transferqueue receipt continuation are unchanged. Stored `verified`
is an observation, not a fresh permission: source/target authority is checked
again before native dispatch and during later workspace continuation.

## Cross-module seams

- Windows input host: `Service.WithInputFiles` resolves the original target via
  the existing workspace manager and returns `Client.FilesOnly()`. It exposes no
  exec or network tunnel. No changes are made to workspace-storage/computer-task
  selection or an official Herdr/Pi core.
- Source-session/cross-Bot owner: `openTaskInput` currently resolves **only the
  bound Bot's authorized media reference**. A hash alone from another Bot is
  insufficient. Establish the existing committed source-session association and
  expert read grant through that owner's contract before extending consumption;
  do not re-ingest/copy an original or add a caller-selected source Bot bypass.
- PC output: existing producer `artifact`/`artifacts`, flat `source_ref`, closed
  manifest, retained identity and NAS committed association remain unchanged.
  A JSON hash is not a committed output. The snapshot-only advancement seam is
  documented separately in [transfer-snapshot-advance.md](transfer-snapshot-advance.md).
- Web and HarmonyOS continue to use the existing media/identity contract. This
  patch adds no UI or client acceptance result for either platform.

## New isolation evidence and remaining acceptance

The formal tool entry streamed 196725 actual non-sparse bytes from the media
service through a trusted TLS gRPC peer into a physical destination file:
`8d00a1c8a9a85407e97ff48bd0549c0520d10f270f956146f58be9408543bc43`.
The generated production envelope ran in a real Bash child and a synthetic Pi
child read the full exact file, using an explicit Win32-to-Linux fixture map.
Three separate host processes recovered the original operation after a lost
atomic response: total file writes 1, total Pi dispatches 1, original identities
unchanged. Negative cases cover corruption, stream revocation, source/target
revocation, root/ancestor links, hard links before Pi, conflicting bytes,
changed context, wrong root/user/session/request, cancellation and admission.
Opaque future metadata and an integer beyond binary64 exactness are retained.

These results prove cloud isolation behavior only. No production NAS deployment,
real Herdr/Pi/model, Windows UNC/NTFS field acceptance, full development
application/UI screenshot, large-file throughput or client matrix was run here.
The sole field writer must use an authorized NAS original through the formal
task entry, capture actual native process and file hash/task identity, then use
the unchanged producer/consumer to observe PC output becoming NAS committed in
its original session. Web/HarmonyOS acceptance remains with their owners.
