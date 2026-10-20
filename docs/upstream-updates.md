# Updating a customized distribution from upstream

Status: this procedure accompanies the GHCR candidate-build and promotion
workflows. Workflow files alone do not establish that any build, runtime
acceptance or release has succeeded. Record those results for each update.

## Branches and triggers

| Ref | Purpose |
| --- | --- |
| `upstream/main` | Remote-tracking ref fetched from `felinics/Memoh` |
| `origin/upstream-main` | Pure official commit last accepted into the customization |
| `origin/main` | Customized application and deployment files |
| `sync/upstream-<short-sha>` | Temporary branch merging one official update into customized main |

Run the procedure manually in the GitHub-connected cloud checkout when adopting
an official update. Keep the NAS as an image runtime. Retain merge ancestry so
later updates can distinguish official changes from customization. Each update
uses a PR; conflicts, failed checks or missing runtime evidence stop progress.
There is no automatic upstream merge or production deployment.

## Prepare the update

Verify a clean worktree, the intended customized `origin`, and the official
`upstream` remote. If absent, add it with
`git remote add upstream https://github.com/felinics/Memoh.git`. Do not overwrite
an existing remote with a different URL.

```sh
git fetch origin
git fetch upstream main
memoh_upstream_tip=$(git rev-parse upstream/main)
memoh_update_branch="sync/upstream-$(git rev-parse --short=12 upstream/main)"
git switch -c "$memoh_update_branch" origin/main
git merge --no-ff --no-commit "$memoh_upstream_tip"
```

If the merge conflicts, stop and resolve it in this branch. Review changes to
custom attachment handling, storage ownership, configuration and Docker files
explicitly. Never reset customized main to upstream or accept all conflict
sides without reviewing them.

Record the previous accepted upstream SHA, candidate upstream SHA and the
customized base SHA in the PR. On the first update, establish the prior official
baseline from verified ancestry. After reviewing private-data exclusions,
commit the merge, push the review branch and open a PR into customized `main`.
These are review steps, not commands executed by this documentation change.

## Required validation

Reuse the repository's Go CI, ESLint, runtime, installer and Docker PR checks
where applicable. Docker PR builds remain non-publishing. Check the workflow
path filters: a skipped check is not evidence that the changed behavior passed.
The Docker filters include `.dockerignore` and publishing-workflow changes.

Run the development environment, exercise the real UI and capture fresh
screenshots as required by `AGENTS.md`. Record the tested commit and results.
Before sending evidence to a public PR, inspect it for private text, endpoints,
credentials and attachments.

| Area | Required checks |
| --- | --- |
| Attachments | Native send waits for preparation; all files succeed before submission; cancellation, verification failure and unknown writes do not send; unknown writes are not automatically retried |
| File identity | SHA and complete read-back match the original; references preserve `content_hash`, Bot scope, storage key, name, MIME and size; reads/saves verify the original |
| Existing behavior | Login, ordinary chat, failed-send recovery and Bot workspace access work with unchanged authorization |
| Deployment | Compose has no production builds, backend images align, persistent mount paths are unchanged, and private configuration is excluded from the build context |
| Runtime data | Image replacement preserves existing data; verify attachment reads and workspace access after restart |

The custom attachment preparation contract is not implemented in this checkout
yet. Add its focused tests when the adapter is introduced; do not describe the
checks above as existing or passing tests. Bot writable-layer storage and the
absence of resumable large-file upload remain separate, unresolved limitations.

After the PR passes review and verification, merge it while retaining upstream
ancestry. Advance `upstream-main` to the exact accepted official SHA with a
normal fast-forward push. If that ref cannot fast-forward, stop and investigate.
Do not point it at the customized merge commit. Record this baseline alongside
the tested main commit.

## GHCR-only publication

Use the existing reusable Docker build for amd64 and arm64. The recommended
image paths are `ghcr.io/particleflow/server:main`,
`ghcr.io/particleflow/web:main` and `ghcr.io/particleflow/workspace:main`
(the existing Debian workspace variant). `main` is the single rolling channel.
Retain a full-commit `sha-<commit>` reference and the resulting manifest digest
for each image; normal deployment continues following the rolling channel.

Dispatch `Docker` on the selected review-branch commit with `validated_sha` set
to that full commit SHA. The guard rejects a changed HEAD. This builds candidate
`sha-<commit>` tags, records their manifest digests in the job summaries and
`candidate-*` artifacts, and leaves the rolling tags unchanged. Ordinary pushes
and PR builds do not publish. Candidate publication makes cloud runtime testing
possible before promoting a release.

Run the candidate digests together in the cloud and record the regression and
runtime acceptance results. Merge the accepted PR with ancestry retained. Then
dispatch `Promote Docker images` from customized `main`, supplying the validated
commit and the three tested manifest digests. The promotion guard requires that
commit to be an ancestor of main with an identical tree. It also checks the
revision annotation and both architectures on every candidate before moving any
tag. A later source change requires a new candidate and acceptance.

Promotion reuses those digests without rebuilding, and verifies the resulting
`:main` references. Moving several tags is not atomic: hold deployment unless
the entire promotion succeeds. `validated-image-references` records the release
for recovery. Retain the previous accepted digests until deployment acceptance.

The workflows use the existing `GITHUB_TOKEN` with `contents: read` and
`packages: write`; the prior caller already declared those scopes. Docker Hub
credentials and login steps are removed, and `id-token: write` is no longer
requested. Package access and repository policy must still permit the token to
publish. Do not add a PAT, package grant or broader token permission to work
around a denial. This update does not change package visibility or repository
security settings.

Use public packages for anonymous NAS pulls without a persistent deployment
credential. Verify package visibility and access before handing the image
references to deployment; a public source repository alone is not proof that
a newly created package can be pulled anonymously. If visibility or access
requires a separate grant, stop and report the exact requirement.

Apply a reviewed release using the existing configuration and the commands in
[Image deployment and updates](image-deployment.md). Rollback uses the recorded
digests temporarily and must account for database/workspace compatibility;
rolling tags resume after recovery. Publishing an image does not authorize
deploying it.
