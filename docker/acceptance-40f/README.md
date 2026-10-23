# One-time synthetic acceptance of candidate 40f0a69

This standalone Compose configuration runs the already published candidate; it
does not rebuild or promote it. Product revision remains
`40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`. Exact registry references are in
[images.json](images.json), from [Docker run 39960411440](https://github.com/particleflow/Memoh/actions/runs/39960411440).
Server and Web use digest references. The same Workspace image is imported into
the isolated Server's containerd and used by actual Bot workspaces, not a dummy
Compose service. Both application architectures were published; host runtime
compatibility must still be tested.

## Scope and deployment gates

Only configuration development and static validation are complete. Nothing in
this directory is production deployment approval or resolution of the existing
legacy-media release hold. Use **synthetic data only**, with a fresh database,
credentials and originals. Never copy an existing config, database, OAuth grant,
provider key, workspace, upload receipt or user file. Never combine this file
with the root/devenv Compose files or a deployment's existing `.env`.

The root Compose uses fixed production names and host runtime privileges. The
existing `scripts/acceptance/delegation_fixture.py` instead builds a source server,
generates credentials and uses host Docker; it is not an image-only launcher.
This template reuses the deployment's migrate-up command, split Server/Channel,
Web upstreams and embedded containerd, with a dedicated project and explicit
binds. It reuses the dev stack's cached workspace import mechanism without its
build/source mounts. No application, Agent core, image or workflow is changed.

Before running anything, the operator must confirm:

1. A dedicated **new** root ending in `memoh-acceptance-40f0a69`, outside any
   production tree, with enough capacity for fresh PostgreSQL data, an imported
   workspace image, overlay snapshots and synthetic originals. Inspect free space
   first; this is several GiB, not just the template's size. An existing directory,
   symlink, Compose project of the same name, or occupied port is a stop condition.
2. Acceptance-only admin, PostgreSQL, JWT, RPC and encryption values, supplied
   through the private configuration below. This change generates no credentials.
   Establish their lifecycle at deployment; do not borrow production values.
3. The **privileged nested containerd** permission in the separate
   `workspace-permissions.yaml`. The base file is unprivileged and cannot be
   claimed to run full workspaces. The override does not grant approval and must
   not be applied automatically. It deliberately adds no host PID/network,
   Docker socket, CDI or host device mounts. Nested cgroups without host PID
   sharing are not yet host-tested; stop on failure rather than adding privileges.
   Privileged containers are not a security isolation boundary from the host.
   Separate paths/internal networking do not clear the legacy-media risk on a
   shared production host. A suitable host and that risk decision remain required.
4. An unused loopback port and a supported way for the tester to reach that host.
   Only `127.0.0.1` Web is published; API goes through `/api` on the same origin.
   No LAN/IPv6 ingress, firewall, TLS, reverse proxy or tunnel change is included.

## Prepare private inputs after those gates

Use Docker Compose v2 (static check used v2.40.3) and Python 3.11+ for checks.
Copy `acceptance.env.example` **outside the repository** and fill its three
values. Pin an already cached PostgreSQL **18** image by digest. Reusing a
PostgreSQL image is allowed; reusing its running container or data is not.
`pull_policy: never` makes a missing cache fail instead of downloading.

Set `ACCEPTANCE_ROOT` to the agreed absolute path and verify it does not exist;
create that directory and only these subdirectories, with a private parent:

```text
memoh-acceptance-40f0a69/
  config/config.toml       private copy of config.toml.example
  config/pg-password       same value as config.toml [postgres].password
  postgres/               NEW empty PG18 data directory
  originals/              NEW empty originals and upload receipts
  data/                   NEW empty workspace data
  containerd/             NEW empty embedded image/snapshot store
  cni/                    NEW empty nested runtime CNI state
  evidence/               private acceptance outputs
```

Create the directories before starting; all binds use `create_host_path: false`.
Do not recurse into or change permissions of any existing directory. Fill the
five `REPLACE_WITH_ACCEPTANCE_ONLY_...` values in the private TOML. Memoh does
not expand environment references in TOML. Use correctly escaped TOML strings;
the encryption value must encode exactly 32 bytes in standard base64. Write the
matching PG password into `config/pg-password` (single line). Keep both files
mode `0600` under the private root. Do not print them or paste rendered private
configuration into issues. Check placeholder removal and equality locally:

```python
import base64, os, pathlib, tomllib
root = pathlib.Path(os.environ['ACCEPTANCE_ROOT'])
assert root.is_absolute() and root.resolve() == root
assert root.name == 'memoh-acceptance-40f0a69'
text = (root / 'config/config.toml').read_text()
assert 'REPLACE_WITH_' not in text
c = tomllib.loads(text)
assert c['postgres']['password'] == (root / 'config/pg-password').read_text().rstrip('\r\n')
assert len(base64.b64decode(c['auth']['agent_credentials_encryption_key'], validate=True)) == 32
assert c['postgres']['host'] == 'postgres' and c['postgres']['database'] == 'acceptance'
print('Private input consistency checked; no secret values printed')
```

## Exact cached image startup procedure (not executed here)

Run from this directory. Set `ACCEPTANCE_ENV` to the private env file. Export
`ACCEPTANCE_ROOT` separately for the Python check above; its value must match the
env file. Do not inherit production Compose options. After permission review:

```bash
dc() {
  docker compose --env-file "$ACCEPTANCE_ENV" --project-name memoh-acceptance-40f0a69 \
    -f compose.yaml -f workspace-permissions.yaml "$@"
}
dc config --quiet
dc config --images
```

Use `docker image inspect` on each exact reference in `images.json` and the
selected PG18 reference. Record local image IDs, architecture, RepoDigests and
application `org.opencontainers.image.revision`; require the listed digests and
40f revision, and PG_MAJOR=18 for PostgreSQL. Missing/mismatching images stop
startup. The workspace tag below must resolve to the same local image ID as its
exact digest. If absent, add **only that tag to that verified cached image**;
if it already points elsewhere, stop rather than replacing it. No pull/build.

```bash
# Database gets only the NEW empty acceptance/postgres directory.
dc up -d --pull never postgres
dc run --rm --no-deps migrate
# Require migration exit 0. Do not migrate down/force or point at another DB.
dc up -d --no-deps --pull never server
# Observe dc ps for at most 120 seconds; require Server healthy before continuing.
# Timeout/unhealthy means stop and inspect this fixture's private logs.
dc exec -T server ctr version
# Host Docker's cache and embedded containerd's store are separate.
# Stream the existing cached image; no intermediate multi-GiB tar is needed.
set -o pipefail
docker image save ghcr.io/particleflow/workspace:sha-40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8 \
  | dc exec -T server ctr --namespace acceptance-40f images import -
dc exec -T server ctr --namespace acceptance-40f images list
dc up -d --no-deps --pull never channel web
dc ps --all
```

Do not create a Bot until import and health checks pass. Require the exact
workspace tag in namespace `acceptance-40f`; missing import fails because the
workspace pull policy is `never`. Preserve the exported local image ID and
compare it to the imported manifest's config digest using `ctr content get`.
A Docker archive import may change the manifest/index digest; do not claim its
rewrapped digest equals the published multi-platform index. Verify revision and
config identity, not tag spelling alone. Data remains in the isolated root.

## Bounded acceptance and remaining endpoints

After actual startup, record health, login and screenshot evidence separately
from the static checks. Use Web's normal provider-step **Skip** for this small
fixture; do not configure a dummy paid provider to bypass onboarding. Create a
synthetic Bot and require its workspace to become ready from the imported image.
Open its settings, save a harmless name and read it back.

Through the same Web origin, use the authenticated documented API to upload a
small synthetic text file (below the configured 1 MiB limit) with
`POST /api/bots/{bot_id}/media`. Require 201, content_hash and ready availability;
then GET/HEAD `/api/bots/{bot_id}/media/{content_hash}`, compare full SHA-256,
test Range/If-Range, and verify anonymous rejection. Use only this Bot's returned
reference; do not probe another deployment or symlinks. Attach the reference in
the UI and verify the uploaded original can be downloaded. Upload/download does
not invoke a model or prove a model response. Tokens stay in the browser or a
private local credential file, never command arguments/logs. Existing schema
and `scripts/acceptance/bot_delegation.py` document the API; do **not** run that
larger source/SQL/model harness against this template unchanged.

This proves Web/API/original roundtrip only once actually executed. Chat replies,
supervisor-to-specialist delegation and returned model-generated attachments
still require a separately agreed synthetic provider fixture or a real provider.
No provider, MCP, PC or phone is registered here. External channels, pgvector
semantic search, Redis/failover recovery, Windows/Pi, phone source/TLS and paid
model acceptance remain outside this fixture. Sessions use one Server with the
memory backend; do not claim distributed/restart task recovery. The internal
network intentionally does not permit provider/PC egress; do not relax it silently.

## Stop and preserve / rollback

There is no production replacement to roll back. Check Compose ownership labels
and `dc ps --all` against the captured acceptance container IDs, then use only:

```bash
dc stop web channel server migrate postgres
dc rm -f web channel server migrate postgres
```

This removes those stopped acceptance containers, not bind data, images or the
project network. Keep the entire root and evidence for inspection. The isolated
network may be removed later only by its recorded ID after checking its Compose
project label and that it has no attached containers. Data removal is a separate,
explicit decision naming this exact root; no deletion command is supplied.
Never use `down -v`, a wildcard removal, a migration down, or a global prune.
No production volume, container, tag or configuration is part of these commands.

## Static verification

Run `python3 docker/acceptance-40f/validate.py` from the repo. It uses public
synthetic placeholders and only `docker compose config`, never the daemon.
It checks digest wiring, mounts, missing inputs, loopback/internal networking,
image pull prohibition, explicit privilege delta, TOML and workspace identity.
It does not prove that Docker, cgroups, application config loading, migration,
onboarding, file delivery or a real client works on the deployment host.
See [STATIC_CHECKS.md](STATIC_CHECKS.md) for the recorded results and limitations.
