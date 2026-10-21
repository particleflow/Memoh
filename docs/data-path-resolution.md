# Data path contracts

This change consolidates two existing path decisions. It does not introduce a
global storage registry, move data, change mounts or permissions, or connect a
new runtime. It is a branch change until reviewed and deployed.

## Server attachment originals

`Config.Load` resolves non-empty `files.originals_root` through the same
`Config.resolvePaths` / `absPath` rules as workspace data and provider paths:

- The base for relative paths is the process working directory at load time,
  not the configuration file directory. A later `chdir` does not retarget the
  already loaded configuration. A different launch directory on restart can.
- `~` and `~/...` expand using the existing home-directory helper. Absolute
  paths retain their location, subject to the existing platform path cleaning.
- `MEMOH_ORIGINALS_ROOT` overrides the TOML setting before normalization,
  including first startup without a configuration file.
- The existing helper's failure behavior is retained: if `filepath.Abs` cannot
  resolve the working directory, it returns a cleaned path instead of adding a
  new configuration error. This patch does not change that shared contract.
- Empty roots retain the legacy storage selection. This patch does not change
  that default or validate a mount's storage identity.

Server paths use the Server OS's path semantics. Windows target paths are a
different boundary; a Windows drive path is not a Server originals root on Linux.

## Windows Pi attempts and sessions

`internal/agenvo/pi_batch_paths.go` is the versioned root-relative layout entry
used by both the Bash envelope writer and the recovery reader. Version 1 keeps
the existing directories under the registered target's `work_dir`:

| Data | Root-relative path |
| --- | --- |
| Attempt input, stdout, stderr and atomic receipt | `.memoh-computer/attempts/<attempt-id>/` |
| Exact Pi session and exclusive lock | `.memoh-computer/sessions/<pi-session-id>/` |

New operations persist `batch.progress.layout_version: 1` in their existing
schema 170 JSON details **before** remote mutation. Historical operations with
the field absent (zero) resolve to exactly the same version 1 paths. Unsupported
versions fail closed; they do not guess paths, dispatch a task, or release an
uncertain operation. Receipt format version 1 remains a separate contract.

The layout uses Bash separators independently of the Go server OS. The configured
Windows root, including backslashes or forward slashes, remains data and is
converted by the existing `cygpath` boundary. Attempt/session IDs are validated;
recovery only reads the fixed receipt/stdout/stderr filenames. Existing target
fingerprint checks still apply. This is not a root migration facility, a new
filesystem sandbox, or permission to change a registered target underneath an
operation.

No directories or old records are rewritten. Saved attempts, session IDs,
page offsets and chunk hashes remain usable across a Server restart. No SQL
migration is needed; no Agent runtime or context composition changes are made.

## Deferred coordination

The following findings remain unchanged and need separate decisions/tests:

- An empty originals root silently selects legacy storage.
- A wrong mount can cause creation of a new empty originals root; no storage
  identity marker is validated here.
- Media spool files use system temporary storage. The independent resumable
  file work owns that concern; this patch does not change its spool behavior.

Full Pi log collection and task-result coupling are also unchanged. This path
cleanup does not implement background terminal transport or make complete logs
a newly required product contract.

## Branch validation (2026-10-21)

- The new configuration tests first failed on the previous code for relative
  TOML roots, relative environment overrides and home expansion. They pass with
  the shared normalization. Two different load-time working directories and a
  subsequent working-directory change are covered, as are absolute/empty roots.
- `go test -race ./internal/config ./internal/agenvo ./internal/computertask`
  passed. A real Bash subprocess writes and reads synthetic attempt files for
  legacy and versioned operations, with both Windows separator forms passed
  through a synthetic `cygpath`. These are Linux-hosted fixtures, not a new
  Windows vendor-binary acceptance.
- Standard MCP/service tests persist version 1 before dispatch, restore a
  historical record with no version and existing 16 KiB chunk/offset state,
  and reject an unsupported layout without remote IO or releasing the slot.
- Targeted golangci-lint, Server build, and
  `bash scripts/check-agent-core-boundary.sh` passed. Runtime/contextview remain
  byte-identical to the recorded official baseline. No SQL migration was added.
- The rebuilt Server and actual Vue application ran at loopback ports 18080 and
  18082 with isolated PostgreSQL, synthetic provider and synthetic Agenvo.
  `HEAD /health` returned 200. Web completed a Pi task; the durable record was
  `completed`, `layout_version=1`, with schema 170 clean. This is a real runtime
  and browser path using synthetic external endpoints, not real model routing.
- In the same Web conversation, a 35-byte synthetic text attachment uploaded
  and downloaded with matching SHA-256. Its actual original was checked under
  the root resolved from a relative environment setting. The first browser
  script selected the unrelated sidebar file input; it was corrected to the
  chat composer input and the attachment flow then passed without JS errors.

The inspected screenshots show the current running build's
[completed task](validation/data-path-pi-web.png) and
[attachment conversation](validation/data-path-web.png). They establish the
browser interactions; backend path/hash and durable-state checks above provide
the storage evidence. Human QA and production validation have not been run for
this patch.
