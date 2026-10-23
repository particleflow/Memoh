# Static configuration check record

Product source and published image revision:
`40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`.
Configuration changes are a separate branch and do not replace that revision.

Tools: Docker Compose v2.40.3; Python 3.11+ standard-library TOML parser.
Scope: local cloud checkout; synthetic placeholder inputs; no Docker daemon
operation, containers, credentials, provider or production data used.

Command: `python3 docker/acceptance-40f/validate.py`

Final exit code: **0**. Captured command output:

```text
PASS: Compose, exact candidate digests, scoped binds, no build/pull, loopback, explicit privilege boundary
PASS: all three missing deployment inputs rejected
PASS: TOML syntax, workspace import identity, isolated paths, placeholder-only secrets
NOT RUN: image startup/import, migrations, HTTP/UI/file roundtrip or deployment
```

The check rendered the base and base+permission override using only
`docker compose config --format json`, compared the single privilege delta,
then separately omitted each required environment input and required all three
Compose validations to fail. No deployment inputs or passwords were generated.
It parsed the TOML example and checked service DNS names, originals/workspace
paths, placeholder credentials and the imported workspace tag.

Two initial checker failures were retained here rather than counted as passes:
`KeyError: 'privileged'`, then `KeyError: 'create_host_path'`. Compose's canonical
JSON omits default-false properties. The checker was corrected to accept that
canonical representation while still rejecting true in the base configuration.
These were assertion representation errors, not application runtime results.

`git diff --check` passed. Changes are confined to this configuration directory;
no application, generated API, migration, workflow or Agent core change is made.
The already-published image evidence is linked in images.json; no build, CI
dispatch, registry mutation, merge, promotion or deployment was performed for
this configuration check.

The cloud root filesystem had 0 free space and `/tmp` about 460 MiB free.
Only small configuration files were written. This is enough for the recorded
static checks, not a claim that this environment can fit another running stack.
No cache/data cleanup or build was attempted.

## Unexecuted gates

- Deployment location, dedicated empty root, cached PostgreSQL 18 identity,
  storage budget and acceptance-only credential lifecycle.
- Explicit nested runtime privilege decision; cgroup/overlayfs compatibility
  without host PID sharing. Privileged Docker is not host isolation.
- Server/Channel/Web health, fresh migration, exact cached workspace import,
  Bot readiness, login/settings, HTTP hash/Range and UI download acceptance.
- Real providers, model-produced returns, Windows/Pi, phones, TLS/ingress,
  pgvector/Redis and recovery behavior. No paid API or real PC task was invoked.
- Existing legacy-media release hold remains. This template neither fixes nor
  retries that blocked action, and does not authorize a production replacement.

Runtime/UI/screenshots were deliberately not run under the explicit static-only
scope for this task. These checks are configuration evidence, not a runtime QA
substitute; human QA remains pending.
