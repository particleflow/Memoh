# Pi JSON cloud validation

This is a separate candidate after PR #6, not a production or vendor-binary
acceptance. No Windows/NAS process, provider credential or real user task was
used. Runtime and contextview remain byte-for-byte at official
`bf0318c5e363b62dd67ddf3fe8855a5b7c8b0046`.

## Completed locally

- `go test -race -p 1 ./internal/agenvo ./internal/computertask -count=1` passed.
  Standard Go MCP SDK over Streamable HTTP covers the trusted binding, PC-A
  isolation, single submission, receipt recovery after terminal-frame eviction,
  more than 64 KiB of output imported in pages, hash-ready refs, revocation,
  cancellation, lost dispatch response, no redispatch, unrelated receipt,
  control-pane identity change, corrupt content, storage failure without private
  cause leakage, oversize preflight and output-limit semantics.
- The generated Bash envelope ran against synthetic local utilities: stdin
  injection vectors stayed data, real process exit 7 and ordinary stderr were
  retained, receipt rename and hashes matched, duplicate attempts did not run,
  and a stale exact-session lock was not stolen. This was Linux Bash with
  synthetic host/path commands, not Windows Git Bash or real Pi.
- JSONL fixtures cover exit-0 semantic errors, abort/length/nonzero, early error
  followed by retry success, missing lifecycle/final records, wrong session or
  provider, partial LF framing, Unicode separators and bounded previews.
- Affected-package golangci-lint, server build, regenerated Swagger/SDK, SDK
  TypeScript check, core boundary script and diff whitespace checks passed.
- The first race attempt exhausted the temporary filesystem during compilation;
  only old derived Go cache entries were removed, and the subsequent full race
  command passed. The environment's global pnpm attempted an unnecessary install
  into a read-only home; the repository's already-installed `openapi-ts` generator
  and TypeScript compiler were used directly, successfully. No dependencies or
  permissions were changed.

## Actual application and browser

Cloud development server `http://127.0.0.1:18080` was rebuilt from this change;
HEAD `/health` returned 200. Existing Vue development UI at
`http://127.0.0.1:18082/bot/acceptance-a` and isolated PostgreSQL schema170 were
reused. The registered synthetic target was configured through the existing owner
API with `execution_mode=pi-json-v1`, without creating a credential or grant.

A new session was opened through the real UI, a synthetic Pi task submitted,
and the runtime called `computer_targets`, `computer_task_submit`, then
`computer_task_result`. The final source rerun produced one batch submission,
eight fixed chunk reads, zero close operations, and no browser page errors.
The prior browser run used a separate synthetic session before the metadata
field cleanup; it was not replayed. Each run submitted once.

The final persisted result was `completed`, `pi_succeeded`, exit 0,
`process_stopped=true`, with the synthetic `42 中文 ✓` output and verified refs:

- stdout: `a1bf19db79e1942dbf25bc9ea0e1cc2f3a2537471a44e41f732eaf32e79d0026`
- stderr: `e14a185c9a2dc66b90cc4b320afb150b83045718b702c15b873b7d4605632376`

![Real cloud UI with synthetic Pi JSON result](web.png)

The screenshot was captured and visually inspected after the final server
rebuild. The provider/relay deliberately return synthetic records; this proves
application wiring and evidence handling, not natural-language routing by GLM,
real Pi/Magpie interoperability or independent completion of a user's task.

### Empty-output correction after the initial checkpoint

Source review found that the actual media spool rejects zero-byte assets. The
initial synthetic asset fake did not expose this. The correction verifies the
receipt's empty SHA-256/size and returns explicit empty-output flags without
inventing a media ref. A new standard MCP test uses the real `media.Service`
with `localfs.NewOriginals` and required persistence. Empty stderr succeeds,
empty stdout is invalid protocol, and a never-started invocation is not_started.
The affected-package race suite and lint passed again.

The rebuilt server was exercised through another new Web session with the real
media service and synthetic relay/provider. It returned `pi_succeeded`, exit 0,
`stderr_empty=true`, no stderr ref, and ready stdout hash
`9e5f6eee1e86379988ad897b211908ab8ada8a275a8af995b9f4d4b012429908`.
No browser errors occurred. No media code or migration was changed.

![Current Web verifies empty stderr without a false artifact ref](empty-stderr.png)

## Remaining acceptance

Independent review, GitHub CI and controlled Windows Git Bash + unchanged Pi
1.2.0 + existing Magpie acceptance are still required before release. No new
migration is introduced: operation details and media refs reuse schema170.
See [the protocol contract](../../computer-task-pi-json.md) for control-channel
restart/ID-reuse limitations, non-sandbox trust boundaries, output limits and
honest cancellation semantics. No production images for this iteration have
been published and no NAS deployment was performed by this task.
