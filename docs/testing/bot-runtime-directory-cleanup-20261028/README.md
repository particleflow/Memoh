# Empty Bot runtime directory cleanup on Linux

Fix: `6235839bbc3162bad8c83fbefea45dc64dd9bd50` on
`fix/bot-workspace-lifecycle`, based on
`ba602dc5e7ad62323245cb6cae6040ea54d045bd` (the existing lifecycle evidence commit).
The earlier behavior fixes remain ancestors: `9b25ac329227920bcd7b7cb7a4cbb8cbcc4a4315`
and `793a1283efc8770b4bfbefb050af9d24f2a18172`. This fix can be reviewed independently
as four Go files; it does not deploy or validate a later unified candidate.

## Defect and bounded correction

`buildWorkspaceContainerSpec` creates the host directory returned by
`Manager.socketDir(botID)`: `<configured-data-root>/run/<bot-id>`. `Manager.Delete`
previously removed the Runtime container and returned without removing this
directory. A successful Bot deletion could therefore leave its empty bind-mount
source behind. This matches the reported symptom; the production artifact is
held by the designated writer and was not copied into this package.

After a successful container deletion, or a Runtime not-found result, the Linux
Manager now attempts one directory-only removal of that exact Bot directory.
It returns cleanup failures before `CleanupBotContainer` deletes the container
record, so a not-found Runtime cannot mask a non-empty or unsafe host path.

The removal boundary is intentionally narrow:

- The Bot ID must be a nonzero canonical UUID; names, traversal and alternate UUID
  encodings are refused.
- The configured data root must be dedicated, not the filesystem root. Each
  ancestor and the fixed `run` parent are opened with `O_DIRECTORY|O_NOFOLLOW` and
  pinned by descriptor. A symlink at any of these levels is refused, including a
  configured root that is an alias.
- `unlinkat(parentFD, botUUID, AT_REMOVEDIR)` is the only removal. The kernel
  rejects regular files, symlinks, FIFOs and non-empty directories. There is no
  recursive deletion, wildcard, sibling scan or shared-root removal.
- Runtime deletion failure or data-export failure leaves the directory intact.
  Missing directories are successful, allowing the same cleanup method to run
  repeatedly. Non-empty/unsafe paths remain and report failure through the
  existing workspace teardown error boundary.

The implementation is Linux-specific. Other platforms keep their previous
behavior instead of using a weaker path-based deletion fallback. Stale socket
files or arbitrary contents are not pruned by this fix.

## Reproduction and safety tests

[red.log](red.log) is the new regression test run against the original
`manager.go`: after cleanup, the empty Bot directory still existed, so the test
failed with exit 1. The patched Manager was restored immediately afterward.
This reproduction used a real temporary filesystem and a fake Runtime adapter;
it is not represented as a real container test.

[green.log](green.log) records the passing run:

```text
go test -race ./internal/workspace ./internal/botworkspace -count=1
```

Eight new test functions cover successful cleanup and repeat calls, missing
Runtime cleanup and repeat calls, non-empty retention followed by an explicit
retry, failed Runtime deletion, failed data preservation, leaf/run/root/ancestor
symlinks, files/FIFOs, invalid UUIDs, the filesystem root, missing parents, and
preservation of sibling directories, shared parents and backup data.

The patched Server built successfully. `source-files.json` records the verified
Go file hashes. The executable was built before the fix commit was created and
reports baseline `ba602dc5` with `vcs.modified=true`; it is not described as a
clean executable stamped with the later fix SHA. No frontend source changed and
no frontend rebuild was needed.

## Real isolated Web and Runtime check

The check ran the current patched Server and real Web UI on dedicated loopback
ports, with new dedicated databases in the owned cloud PostgreSQL/pgvector
services and a new dedicated data root. Earlier developer and lifecycle test
databases were preserved. It reused the already-pulled official workspace
image, without rebuilding it:

`docker.io/memohai/workspace@sha256:9bc968111563981d7ba41cb9eb7abec0b18c860b8b284ad8bdfd431b1ddcfd6b`

The image's previously recorded official source/provenance is in the
[earlier lifecycle package](../bot-workspace-lifecycle-20261027/image-provenance.json).
The platform was Linux/amd64 and the actual backend was Docker.

1. Create the synthetic `Run Cleanup Audit` Bot through onboarding, without a
   model. It became ready as Bot `a8d4b0c9-5061-4215-a7bf-92f3a11b310a` with a
   running container whose Bot label matched that ID.
2. Independently verify that its runtime directory exists, is empty and is not a
   symlink. Place an unrelated sibling directory and backup marker outside that
   target as scope guards.
3. Open General → Delete Bot and confirm once. The browser observed exactly one
   DELETE response, HTTP 202. The Server completed `bot.delete` in approximately
   3.62 seconds. **202 is acceptance, not completion**: the first immediate list
   still contained the Bot while deletion was running.
4. Read back independently: zero Bot, workspace-intent and container-record rows;
   no Docker instance; no exclusive runtime directory. The shared `run` root,
   sibling and backup marker remained. No manual directory or container removal
   was performed.
5. An authenticated list GET returned `200 {"items":[]}`. A fresh login showed
   the empty real Bot list after completion. A restored pre-deletion browser
   snapshot was not used to claim an immediately empty list.

The [confirmation capture](screenshots/confirm-delete.png) and
[settled list capture](screenshots/settled-empty-list.png) are original screenshots
from this run and were inspected. They are not human QA.
[outcome.json](outcome.json) retains both the immediate accepted response and the
later independent completion checks. The owned test Server and Web were stopped
after evidence capture; the dedicated test databases and scope guards remain.

## Limits and production handoff

No NAS deployment or production Bot operation was performed in this work.
The cloud test does not establish ZFS behavior, host data-path hashes, persistence
after a container is rebuilt, repeated HTTP DELETE semantics, or phone/HAP
acceptance. Repeat calls to the internal cleanup method were tested; that is not
a substitute for repeated endpoint deletion. The earlier account-switching
recovery limitation remains unchanged.

Before applying this patch to the production symptom, the designated writer only
needs to confirm the residual directory's redacted relationship:
`<the configured data root>/run/<the deleted Bot UUID>`, created by `socketDir`.
No private absolute path or config dump is required. If it is a different
directory, this patch must not be described as cleaning that other path.

After the unified source includes this fix, any authorized production retest
must separately read back the same Bot's lifecycle/Runtime records, instance,
and exact directory after a normal product deletion. The previous writer-owned
zero-residue failure is not retroactively changed into a pass by this cloud run.

`manifest.json` records the source hashes and normalization of this package.
Screenshots are byte-identical; logs replace temporary host paths with templates.
Browser state, credentials, configuration, raw environment, private endpoints
and production evidence are excluded.
