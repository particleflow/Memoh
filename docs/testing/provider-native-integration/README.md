# Provider configuration and native task view integration

Product checkpoint: `f65dd2b5537d771be16f51e49e8b290ac3cf8468`, a conflict-free `cherry-pick -x` of `5dde73aa91fd3a307c3691ba09c84237012edbd2` onto `893eb55b50d46e5fd4d64ffb4a2769c4e6b6f75e` on PR79. The increment is exactly five Web files, +278/-0: Provider form, its tests and three locales. It changes no backend, SDK, dependencies, permissions, Agent core or database schema. The independent `ed4a5e69b9e469f4b0b801f5365322b42eab4a31` batch-restart regression branch is not included; batch recovery remains unresolved in this candidate.

The form exposes the existing `config.images_api.protocol` through the existing Provider update API. It preserves stored image policies, provider address and client type, and does not round-trip the masked API key. This does not validate any real provider/model, change a Bot's image-model binding or resolve unsupported legacy `omit`/`multipart` settings.

## Local checks on the combined product

| Check | Result |
| --- | --- |
| Three Provider + five task UI test files | exit 0; 8 files / 98 tests passed |
| Web and SDK typechecks | exit 0 for both |
| Affected Web ESLint | exit 0; zero errors, eight test-fixture warnings retained |
| UI contract | exit 0; three existing warnings retained |
| Web production build | exit 0; existing large-chunk warning retained |
| Official Agent core boundary | exit 0; runtime/contextview match `bf0318c5` |
| Isolated actual Provider page | PUT 200, GET and browser reload confirmed the selected protocol and preserved policies; zero provider probes/model calls |
| Existing actual task detail on the combined Web | exit 0; native-content entry rendered; no content or follow-up request |

Commands (run from the repository root with its existing dependencies):

```sh
NODE_OPTIONS=--no-experimental-webstorage pnpm exec vitest run \
  apps/web/src/pages/providers/components/provider-form-images.test.ts \
  apps/web/src/pages/providers/components/provider-form.test.ts \
  apps/web/src/pages/providers/components/provider-form-test-connection.test.ts \
  apps/web/src/pages/home/composables/useNativeTask.test.ts \
  apps/web/src/pages/home/components/native-task-content.test.ts \
  apps/web/src/pages/home/composables/useSessionTasks.test.ts \
  apps/web/src/pages/home/components/session-task-results.test.ts \
  apps/web/src/pages/home/components/session-tasks.test.ts
pnpm --filter @memohai/web typecheck
pnpm --filter @memohai/sdk typecheck
pnpm exec eslint \
  apps/web/src/pages/providers/components/provider-form.vue \
  apps/web/src/pages/providers/components/provider-form-images.test.ts \
  apps/web/src/pages/home/components/native-task-content.vue \
  apps/web/src/pages/home/composables/useNativeTask.ts
node scripts/check-ui-contract.mjs
pnpm --filter @memohai/web build
bash scripts/check-agent-core-boundary.sh
```

The browser reused the loopback server built for the unchanged 893 backend and the existing isolated synthetic PostgreSQL database, with the combined Web source served by Vite. It created one synthetic Provider through the actual API, then selected **OpenAI Images**, saved through the actual form, read it back and reloaded the page. No production identity, address, credential, file or model was used. The fixture's placeholder key is not an external credential. No Test Connection, model import, image generation, terminal-content read or follow-up submission was performed.

Screenshots: [saved Provider configuration](provider-saved.png), [existing task detail](combined-task-view.png). These are browser-automation evidence, not human or physical-device acceptance. The task detail truthfully shows unavailable connection / unknown status / unverified completion. Existing native prompt and disconnect semantics were tested at 893; see [that evidence](../native-task-content/README.md). They were not replayed for this form-only increment.

## Failures and limits retained

- `browser-first-failure.log`: the first browser harness timed out awaiting a save response. The synthetic Provider existed but was still unchanged on a subsequent GET. A read-only diagnostic also timed out locating the save button. The harness was adjusted to use the form submit control and an API-prefix-independent response predicate; product code was not changed.
- `browser.log` is deliberately **not** an all-green execution: Provider save/read/reload assertions passed, then selection of an earlier native fixture timed out. That fixture's temporary configuration had already been restored, and its operation was no longer returned in the authorized list. We did not restore the temporary configuration or resubmit work to satisfy the test. The follow-up read-only `task-view.log` selected an actually returned persisted batch fixture and passed. It validates combined rendering, not another native Agent execution.
- `browser-proof.json` retains `taskView: false` from that earlier combined script. `task-view-proof.json` records the later separate read-only check. No successful result overwrites a failed log.
- The synthetic Bot still has no usable workspace bridge; unrelated container/files requests can return 404/503. This is not default workspace provisioning acceptance.
- These are local checks. Exact-head remote CI and publication must be checked separately. This environment's previously rejected `gh` API authentication was not retried or replaced.

`evidence.json` records original and published hashes, byte sizes and explicit path/whitespace normalization for the readable logs. Screenshots contain only synthetic fixture data. Private configs, JWTs, raw server logs and database contents are excluded. No tests were rerun to produce the evidence-only documentation commit.

## Candidate publication handoff

Use the existing `.github/workflows/docker.yml`, ref `integrate/unified-7b-transfer-observation`, with `validated_sha` equal to the final pushed 40-character head (including this evidence commit). First check for an already running or completed publication at that exact SHA. The existing workflow accepts a feature ref, verifies input equals `GITHUB_SHA`, and dispatch forces all three images for `linux/amd64` and `linux/arm64`; it does not require merging main.

The expected candidate tags are `ghcr.io/particleflow/{server,web,workspace}:sha-<head>`. The workflow publishes `candidate-server`, `candidate-web`, and `candidate-workspace` artifacts containing image, digest and commit. Before installation, verify each manifest's exact revision and both platform entries. These are expected names, **not proof that images have been published**. This task cannot dispatch or inspect Actions through its rejected `gh` API; the parent can use its already connected authorized integration. No main/latest promotion, NAS migration/deployment or replacement of the deployed 91af images is performed by this increment.
