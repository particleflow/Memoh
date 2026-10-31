# Shared task source integration

This increment applies `820f0a52d4bc856a3b64376c637c6ee2eb6e3fd8`
(parent `0894a6eb8ff69c8796e2da5450fa172d4cd5ef81`) to unified
`df411494e79696ed69cd3d21a3df08ea875d585e`. It preserves that candidate's
directory selection/execution snapshots, native ANSI file frames, UI and all
other accumulated changes. Only the source commit's 14-file patch is imported;
the old source baseline is not substituted for the unified tree.

## Boundaries verified by the combination

- `target.work_dir` and the existing producer `transfer.json.source_root` still
  identify the same configured execution root. RootHash and the exact config
  bytes' HostBinding are unchanged. A selected child CWD does not rebind either.
- Authorized source handles are separate from private config, credentials,
  queue/state and retained publication handles. Queue filesystem/owner/DACL,
  locking and flush checks are unchanged. No old queue is migrated or rebound.
- The collector verifies the original attempt context and declaration, consumes
  only the matching closed manifest after registration, and projects advisory
  feedback only into that same attempt. The executable workspace remains
  writable; no second source copy, credential or network service is introduced.
- Source references remain flat logical filenames. The combination regression
  `TestWorkspaceSourceCombinationKeepsArtifactRoot` verifies a configured UNC
  root keeps its RootHash, producer locator and source reference; it stops at
  the first synthetic uncertain workspace creation, with zero Pi inputs.
  Selected and explicit child CWDs with declared artifacts are rejected before
  an operation claim or native call. This is an **existing functional gap**:
  selecting a child directory for execution does not yet support collecting
  an artifact relative to that child. No path is silently prepended, no file is
  copied to the root, and no unsupported task is presented as delivered.

See [task source checks](../../delivery-task-source.md) and
[execution snapshots](EXECUTION.md) for the underlying contracts. The retained
source recovery fixture exercises 131109 real bytes across independent local
processes and a loopback TLS protocol fixture, preserving one Create, original
receipt/task/hash/binding and source bytes. It is not a Windows SMB test or a
production Server/PG acceptance claim.

## Component and deployment contract

| Component | Required version / unchanged boundary |
| --- | --- |
| Unified Server/Web/workspace | Use the exact final PR79 commit and its own CI; no migration or HTTP/SDK change (schema 177). |
| Producer CLI/plugin | Descriptor remains 0.10.1; a binary built from 0894/old 0.10.1 does **not** include 820f. The new bundle's source_commit and binary hash must identify the final candidate. NAS images alone do not update the PC executable. |
| Windows endpoint | Existing `eb3e3c39abfc1e7e0b7024e62db30bee9871c3cf` root/config-pair contract is retained; no endpoint source changes are imported. Actual installed revision/access must be checked onsite. |
| Herdr/Pi/Agent | Existing CLI contract and plugin Herdr >= 0.9.5; no official core changes or new task execution protocol. |
| Shared Web/Harmony | Existing directory HTTP contract/SDK unchanged; no HAP rebuild in this increment. Field HAP 1.0.17 is not evidence of a newly installed producer. |

Publisher handoff (not executed by this integration task): existing `docker.yml`,
ref `integrate/unified-7b-transfer-observation`, `validated_sha` equal to the
exact final successful commit. The workflow derives only the candidate
`sha-<full SHA>` tags under existing GHCR names; there is no tag input. Do not
dispatch against a moved branch head, promote main/latest or infer deployment
from Docker PR CI. The sole field writer owns dispatch/deployment coordination.

No new migration: preserve schema 177, the current approved Compose/TLS/network,
config, directories, credentials, receipts and originals. Application rollback
may restore the previous compatible schema 177 candidate; never use generic
`migrate down`. A producer rollback is conditional on the original immutable
config/state pair and queue format; preserve all unresolved queue evidence and
do not overwrite transfer.json to redirect it.

## Remaining onsite acceptance

1. Verify the existing Server-visible `[container.storage].root` and existing
   Windows-readable UNC `share_root` identify the same intended workspace
   storage directory. Neither value is guessed or written here; media originals
   and unrelated mapped drives are not automatically an execution grant.
2. Verify that the Windows host's authorized OS identity can use that existing
   UNC, including SMB handle identity, directory flush, permissions/revocation
   and reconnect. The implementation deliberately rejects mapped-drive aliases
   as source roots and retains private local NTFS for queue/config/state.
3. Prepare a new matching host/target/source/config pair without editing old
   tasks or old transfer.json/HostBinding. Check the exact new producer bundle.
   Only a root-aligned artifact task is supported; child-CWD artifacts remain
   the explicit gap above. Verify real execution and result collection share
   the same root, hash and immutable locator pair.
4. Validate reconnect/recovery of the original task only after its existing
   workspace/pane identity is established. Begonia/UAC connectivity is a field
   prerequisite, not a cloud integration test. No Pi replay, new workspace,
   recollection or field model invocation occurs in these isolated checks.

Human UI/Windows/UNC acceptance remains incomplete. This source integration
does not create a release, install a PC binary, configure NAS storage or mark
F07/F08 complete.

## Differential validation in the integration workspace

- Initial source-contract subset passed, including actual 131109-byte process
  recovery, scope/identity checks and revocation before network access.
- Complete affected transferclient and CLI regressions passed (6.454s/0.448s).
- New root/child-CWD combination and retained resume-snapshot checks passed
  (0.031s); these controlled fixtures do not execute Pi or access a real UNC.
- The first staged check reported 12 diagnostics in the incoming source:
  formatting, test file access/modes, branch style, spelling/unused parameter
  and signed file-descriptor conversion. The integration uses root-relative
  fixture I/O and a negative-descriptor check, fixes formatting/style and retains
  every assertion. No new lint exclusion, suppression or rule change is added.
- Final required staged aggregate passed with the original golangci-lint 2.10.1
  rule set (4.87s), then complete computertask/transferclient packages
  (11.746s/6.730s). Official runtime/contextview boundary matches bf0318c5.
- Windows/amd64 test-binary cross-compilation passed (not native execution).
- The raw logs in `unc-combination/` preserve both the first failure and subsequent success. No full
  old UI/HAP build, model call, production DB or physical device action is run.
  This is source validation; final exact-head CI and onsite acceptance remain
  distinct gates, and human QA remains unchecked.

## First Windows combination failure and fixture correction

The initial integrated head cd9133f3 failed Native run 40453237386/job122020556849,
step 6, in legacy feedback-reader tests. The new 131109-byte process recovery
itself passed. No test timed out and no production permission was changed.
The test helper still used privateHostRoot/openPrivateFile to read an advisory
that the new source API intentionally creates under source permissions.

A new shared-source feedback assertion reproduced that private-storage error
on Linux before the helper correction (`feedback-reader-red.log.txt`). The
helper now uses the pinned original source attempt, verifies the expected plan
hash, and opens only the fixed feedback filename through the source capability.
Existing DecodeFeedback, denial classification, unchanged-block, secret/content
exclusion, original delivery IDs and queue assertions all remain. The previously
failing test families and the 131109-byte recovery passed afterward
(`feedback-reader-green.log.txt`). The follow-up changes tests only; no product
ACL, queue validator, rule, timeout, retry or workflow is loosened. The first
Windows failure remains linked and extracted alongside the red/green evidence;
only a later exact-head native success can complete this gate.
