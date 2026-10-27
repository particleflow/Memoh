# Bot creation and native workspace lifecycle evidence

This package records existing isolated cloud checks from 2026-10-27. Packaging
these results did not run tests, start services, create containers, or perform
production actions. The screenshots are actual application captures; they are
not human QA or NAS/mobile acceptance.

## Exact sources and combination

Repository: `particleflow/Memoh`, branch `fix/bot-workspace-lifecycle`.

| Role | Commit |
| --- | --- |
| Source baseline supplied as the production revision | `98ebb47ba8de52e88f4e27f130766f96d36ffd58` |
| Detect an incomplete workspace stream and check the Bot's actual state | `9b25ac329227920bcd7b7cb7a4cbb8cbcc4a4315` |
| Restore an interrupted create using its scoped request identity | `793a1283efc8770b4bfbefb050af9d24f2a18172` |

The second fix is a direct descendant of the first. Both are descendants of the
baseline. These checks do not establish validation of a later unified candidate.
There is no tracked Go, `go.mod`, `go.sum`, or workspace-template difference from
the baseline through `793a1283`.

The reused Server binary reports VCS revision `9b25ac329227920bcd7b7cb7a4cbb8cbcc4a4315`
and `vcs.modified=true`; the pending edits at that build were Web changes. It is
not represented as a clean binary built at `793a1283`. The bridge used for the
successful Runtime check was built at `793a1283efc8770b4bfbefb050af9d24f2a18172`
and also reports `vcs.modified=true` with local evidence files present. The Go
source comparison above is the limit of the source correspondence claim.

## Environment and official image

The real Web UI and Server ran on dedicated loopback ports in an owned cloud
workspace. PostgreSQL and pgvector used the independent Compose project
`bot-audit`, separate containers, ports and named volumes. The existing developer
database containers were not replaced or reset. The image-failure checks and the
successful check used the same isolated database. No model call was made.

The installer `scripts/install.sh` (`select_workspace_image`) and
`conf/app.docker.toml` specify the official `memohai/workspace:debian-latest`
reference. It was pulled, not rebuilt, and pinned to:

`docker.io/memohai/workspace@sha256:9bc968111563981d7ba41cb9eb7abec0b18c860b8b284ad8bdfd431b1ddcfd6b`

The selected platform was `linux/amd64`. Registry provenance identifies the
official source `https://github.com/felinics/Memoh`, version `v0.22.0`, commit
`58124cbe59b3341c7cb8cd33e0158f7a6444edb1`.
[image-provenance.json](image-provenance.json) records the index, platform and
provenance-blob digests. This is a registry provenance claim, not independent
signature verification or evidence that the image came from the user fork.

## Observed outcomes

| Case | Existing evidence and result |
| --- | --- |
| First successful create | Synthetic Bot `fc248e4d-16b6-42bb-ac93-72b33a9d5162` (`runtime-success-audit`) reached `ready` in approximately 15.4 seconds. Its workspace was `present/running`, generation 1 observed, `ever_ready=true`, with no last error. |
| Container and bridge | `workspace-fc248e4d-16b6-42bb-ac93-72b33a9d5162` was running; its `memoh.bot_id` label and database record referenced the same Bot. The API reported `runtime_backend=docker`, `task_running=true`, `container_path=/data`. Initialization, container record/task and gRPC reachability checks were all `ok`. |
| Data association | The file API wrote `/data/runtime-ownership-check.txt`; the API readback and an independent read inside that exact container returned identical content. The real Files UI displayed the file. Bootstrap files `AGENTS.md`, `MEMORY.md` and `PROFILES.md` were present. |
| Image unavailable | The real create UI displayed image preparation failure and offered Retry workspace / Continue later instead of ready. Repeated retries kept the existing Bot identity. |
| Existing failed Bot recovers | Bot `f7da99b3-6350-4dd8-b0c4-52c8582c4bb5` (`replay-audit-four`) had failed in `image_prepare`. After the isolated default image became available, the reconciler brought the same Bot to `ready/running`, desired and observed generation 2. No replacement Bot was created for this recovery. |
| Response lost before `bot_created` | Browser routing forwarded the real POST to the real Server, then withheld/aborted its response before any event reached the UI. The page showed a network failure. Refresh issued two observed POSTs with the same idempotency key and recovered the existing Bot. The database had one row for that test name. This is transport fault injection, not a simulated Runtime. |
| Leave while outcome is unknown, then return | Continue later left the interrupted flow for the Bot list. Opening creation again replayed the original request key, recovered the existing Bot, and offered workspace retry. After the Bot was known, leaving its failed workspace flow allowed a fresh blank create form. |
| Duplicate click and session scope | Targeted tests cover one request while already creating, retry/reload identity, account/backend isolation, retention of independent backend-scoped requests, ignoring an old account's response, and refusing an unrecoverable create when session storage is blocked. |

[runtime.json](runtime.json) contains allowlisted API, database-observation and
container evidence. It is a historical snapshot, not a current health assertion.
For this Docker backend, `/data` lived in the container's writable layer; it was
not a host data-directory bind mount. Survival after container deletion or
recreation, and containerd snapshot behavior, were not tested.

## Screenshots

- [Response interrupted before the first Bot event](screenshots/interrupted-before-first-event.png)
- [Same create recovered after refresh](screenshots/resumed-after-refresh.png)
- [Same create recovered after leaving and returning](screenshots/resumed-after-leaving.png)
- [Successful Runtime shown as Running](screenshots/runtime-running.png)
- [Real Files UI showing the ownership-check file](screenshots/runtime-files.png)

The successful Runtime screenshot also shows that no chat model is configured.
Workspace readiness and file access were verified; a model response was not.

## Existing checks and cleanup

The existing final targeted Web run passed 6 files / 60 tests; its normalized
output is [targeted-tests.txt](targeted-tests.txt). Earlier work also recorded the
four related Go packages (`internal/botworkspace`, `internal/bots`,
`internal/handlers`, `internal/workspace`) passing, a Web build passing, and
changed-file ESLint with zero errors. UI contract passed with three existing
warnings. Those results were not rerun while preparing this document, and are
not a claim of full-repository, remote CI or human QA completion.

The cloud daemon used `vfs`, which copied the large image for each workspace and
exhausted inode capacity as old failed test Bots recovered. The owned test Server
was stopped. The two created test containers were stopped, their `/data` and
instance metadata archived locally, and only those test containers removed.
The local data archives are deliberately not published: desktop files can
contain ephemeral session material. The existing developer database containers
and volumes were preserved and subsequently reported healthy. The workspace
retained about 12 GB and 800,000 free inodes after cleanup. Database `running`
fields in this package are the last reconciler observations before cleanup.

`manifest.json` lists screenshot hashes and the source hashes/normalization for
the published records. Screenshots are byte-identical copies of existing
captures. Browser state, tokens, environment variables, raw config, host paths,
private endpoints and local data archives are excluded.

## Account switching and remaining acceptance limits

Pending requests are scoped to the authenticated user and API base; another
scope does not replay their payload, and old responses cannot apply setup under
the new account. Login/logout clears pending drafts to avoid exposing them to
the next account. Therefore an unconfirmed create is **not guaranteed to resume
after logging out and back in**.

The existing Bot list is an authenticated, read-only way to find already-created
owned Bots. Onboarding already filters by `owner_user_id`, excludes deleting
Bots and adopts an existing ready/creating/failed Bot. A saved Bot ID can be
checked against that list and reused. A display name alone does not prove that
an unnamed or ambiguous Bot is the exact lost request; this client change does
not establish universal recovery after the request key was cleared. No new
service, user setting or production repair was added for that edge case.

The checks establish isolated Docker Runtime behavior. They do not establish NAS
containerd behavior, HAP installation/navigation on the phone, a production
create, a model response, or validation of the later unified release candidate.

## Minimal read-only production check for the designated writer

Use one existing Bot and return only these fields. An anonymized stable Bot alias
can be used across the outputs. Do not dump the private configuration, tokens,
environment, file contents, complete database rows or unrelated logs.

1. Running Server source/version or release identity; actual workspace and
   Runtime backend; for containerd, the namespace. Report field values only.
2. Whether the deployed composition registers the workspace service, injects
   workspace intents into Bot creation, and starts the reconciler. Provide the
   matching deployed-source references and, if available, the single sanitized
   startup confirmation for the reconciler. Source presence alone does not prove
   a running instance used that composition.
3. For the existing Bot: Bot status; intent present/absent, desired/observed
   generation, observed state, `ever_ready`, and last error code/phase (omit raw
   error text if it contains paths or addresses).
4. The existing container API's workspace/backend, instance identifier,
   `task_running`, image digest and `container_path`; compare its Bot label and
   database instance reference using the same stable alias.
5. Data ownership type: container writable layer, volume, bind mount or
   containerd snapshot. Return only the destination and a stable redacted source
   alias, not a private host path. Read-only file-list/stat through that Bot's
   bridge may confirm reachability; return status and match result, not contents.

These checks require no new Bot, container, model call or NAS configuration
change. A final production create and phone-side HAP flow remain separate,
explicitly authorized acceptance work.
