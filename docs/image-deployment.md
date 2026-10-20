# Image deployment and updates

Use the root `docker-compose.yml` for production. It pulls images and has no
build steps. `docker/docker-compose.yml` adds source builds; `devenv/` is for
development in a cloud checkout.

## Select the published images

The root Compose file accepts two optional variables in the deployment `.env`:

| Variable | Default | Services |
| --- | --- | --- |
| `MEMOH_SERVER_IMAGE` | `memohai/server:latest` | migrate, server, channel |
| `MEMOH_WEB_IMAGE` | `memohai/web:latest` | web |

For a customized distribution, set both variables to its approved, published
rolling image references. Leaving them unset selects the official images and
does not include fork changes. All three backend services share one image
reference so migrations, Server and Channel use the same build.

Keep a rolling tag for normal updates. Record the previously deployed image
digests for recovery instead of fixing normal deployment to an old release.
The workspace image is selected separately by `default_image` in the existing
runtime configuration. Keep that setting and existing workspaces unchanged
unless an update explicitly requires a compatible workspace image.

## Keep production independent of the source checkout

The deployment directory contains `docker-compose.yml`, the existing
`config.toml`, `.env`, and `conf/providers/` runtime configuration. Preserve any
existing optional certificate files and overrides. The provider directory is
configuration, not an application source checkout; preserve customized files.
The server image also includes the stock provider presets.

Build and test in the cloud checkout or GitHub Actions. Transfer only the
reviewed deployment files to the deployment directory. Existing configuration
and secrets stay on the deployment host and must never be included in a source
commit or release artifact.

Keep the existing Compose project name, named volume definitions, bind mount
paths and configuration locations. In the standard manifest, persistent volumes
are `postgres_data`, `pgvector_data`, `containerd_data`, `memoh_data` and
`server_cni_state`. An installation with custom mounts must retain them.

For deployments with existing host directories, the optional
`MEMOH_POSTGRES_DATA_DIR`, `MEMOH_PGVECTOR_DATA_DIR`,
`MEMOH_CONTAINERD_DATA_DIR`, `MEMOH_CNI_STATE_DIR` and `MEMOH_DATA_DIR` variables
select absolute host paths for those mounts. Store their real values only in
the deployment `.env`; `MEMOH_CONFIG` already selects the runtime config file.
Unset variables preserve the original named volumes. Confirm the existing data
locations before setting them: changing a mount source does not migrate data.

The stock configuration uses containerd. An installation using the Docker
workspace backend must retain its backend configuration, socket access and
host-valid bridge/workspace mounts. Binding the Server data directory alone
does not make Bot writable-layer files independently persistent. This manifest
does not migrate workspace storage or change the PostgreSQL major version.

The official installer already pulls images and removes its fresh temporary
clone. It also resolves a release and rewrites image tags, and obtains files
from the official repository. It is not the update mechanism for a customized
rolling-image deployment.

## Review and apply an update

First verify the candidate images together in the cloud, including migration,
login, chat, attachments and workspace behavior. Record the deployment's
current configuration and image digests, and take a restorable data backup
before applying migrations. A tag can move; record the candidate digests from
the successful pull as well.

After approval, run from the existing deployment directory, retaining the
installation's existing project name, profiles and override files:

```sh
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.yml pull
docker compose -f docker-compose.yml up -d --no-build --pull never
docker compose -f docker-compose.yml ps
```

The commands above show the base manifest. Include the existing profiles and
overrides in every command when applicable. `--no-build` prevents host builds;
`--pull never` uses the images just pulled. Verify health, login, a real chat
attachment round trip and existing workspace data after the update.

Retain the previous images until acceptance. For recovery, the same two image
variables accept recorded `repository@sha256:...` references. Check database
and workspace compatibility before using old binaries; reverting an image
does not undo a database migration. Restore the approved rolling references
when normal updates resume. Do not remove data volumes as part of an update.

## Follow upstream from Git

Follow the [upstream update procedure](upstream-updates.md) from the cloud
checkout. It separates the accepted official baseline from customized `main`,
prepares a review branch for each update, and requires regression checks before
publishing a rolling image. The procedure works without GitHub's fork Sync
button. Upstream synchronization remains a reviewed Git operation. Image
publication and promotion require explicit workflow dispatches.

The Docker workflow reuses the official multi-architecture builder and publishes
GHCR candidates only on explicit dispatch with the selected commit SHA. The
separate promotion workflow moves the exact accepted digests to `:main`; it does
not rebuild them. PR and ordinary main-branch builds do not publish. Both jobs
use the existing `GITHUB_TOKEN` with `contents: read` and `packages: write`,
without Docker Hub credentials or an OIDC permission. The workflows still need
successful remote execution and runtime acceptance before an image is usable
for production; adding their files does not perform a release or deployment.
