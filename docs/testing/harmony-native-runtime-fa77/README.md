# Harmony shared Runtime UI alignment

The later [fixed PR84 compatibility review](../harmony-native-runtime-pr84/README.md)
checks the now-published native server/SDK against this phone checkpoint. The
earlier missing-server statements below describe fa77, not PR84. No new phone
component or split SDK pin was needed; the shared lock still awaits the unified
combination.

This source-only checkpoint prepares the existing phone UI against unified
commit `fa77a073fe6380d297474a6053a1445201bcab83` on the independent branch
`feat/harmony-native-runtime-ui-20261025`. Source preparation verified 6,198 Git
blobs and the fixed UI revision `f69402a55dd87a59c10722fe11507ad8fd7048f5`;
all 43 existing phone adaptations applied. See the
[source lock](../../../mobile/client/config/memoh_web.lock.json) and
[existing builder](../../../mobile/client/mobile/build_memoh_web.py).

No new HAP was built. The signed/distributed 1.0.8 delivery remains based on
source `ee00ff76a07ac27e08f4b4f58fd1354f0ecbeef3`, with its generic unsigned
artifact preserved at distribution commit
`cdedf9239f0c93c41a0e679b570778ac999eadbe`. Public packages must retain empty
configuration; [Windows-local injection before signing](../harmony-service-migration/README.md)
remains unchanged. Configured private derivatives must never return to GitHub.

## Runtime identity and existing consumers

In the inspected contract, a native Windows Runtime is a remote computer with
`provider: "memoh"`, `kind: "remote"`, and the existing runtime/target IDs.
`kind: "native"` and `target_id: "native"` identify the NAS workspace. They must
not be reused for Windows simply because its implementation is called native.
The [runtime service](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/internal/userruntime/service.go)
accepts the existing Memoh provider and projects connection state/capabilities.

| Surface | Existing consumption and boundary |
| --- | --- |
| Computer registry and Bot access | [use-computer-access.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/components/computer/use-computer-access.ts) reads account runtimes and uses the existing Bot-scoped mount/unmount endpoints without an Agenvo-only filter. |
| PC selection | [chat-pane.vue](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/components/chat-pane.vue) queries targets per Bot and retains selection per chat view. Missing/revoked targets do not silently become another PC. Folder-bound sessions let the server derive the target instead of sending a conflicting target override. |
| Tasks and logs | [useSessionTasks.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/composables/useSessionTasks.ts), `useComputerTaskObservation.ts`, and `useNativeTask.ts` use the existing authenticated Bot/session/task routes. Identity changes retire requests and clear content; no provider-specific phone protocol is needed. |
| Results and transfer state | [useComputerTaskDeliveries.ts](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/home/composables/useComputerTaskDeliveries.ts) validates `version`, `operation_id`, `files`, `next_after`, and `has_more`. Only `committed` references become attachments; the last page does not establish task completion. |
| Phone attachments | [harmony-files.ts](../../../mobile/client/mobile/memoh/harmony-files.ts) retains the owning Bot and scoped delegation URL where present, with the existing original-byte validation and native save bridge. The phone does not connect to a new Windows transfer endpoint. |

Do not remove the current Agenvo availability/capability presentation conditions
without a server contract change. At this fixed revision, Memoh runtimes report
their connection's online flag and handshake capabilities; Agenvo supplies its
additional availability states. A live connection alone does not authorize
Herdr task execution, follow-up, or transfer capabilities.

## Confirmed shared changes since the phone's 68be baseline

These are existing upstream changes to reuse, not a new mobile implementation:

- `computer-task-checkpoints.vue` separates receipt state, history continuity,
  collection offsets, and current original availability. Unknown business outcome
  remains unknown.
- `session-task-results.vue` displays owning, direct-parent, and aggregation-root
  session references without rewriting ownership.
- `useComputerTaskObservation.ts` requires explicit observation before device
  reads; an HTTP 200 `authentication_unavailable` result also stops automatic
  polling.
- `useNativeTask.ts` and `native-task-content.vue` expose authentication pauses
  and preserve the existing follow-up gate. An unknown request outcome retains
  its original request UUID rather than submitting a replacement.

These modules are present at the fixed unified commit. Their presence does not
establish that the new Windows native-to-Herdr adapter has been integrated.

## Dependencies on the server and Windows owners

1. Preserve the existing account runtime identity, Bot grants, remote target ID,
   and authorization checks. Do not introduce a phone-specific permission model.
2. Fix the native-to-Herdr contract before changing its consumer. Existing task
   readers expect `task_type: "computer_operation"`, a matching `operation_id`,
   the current task status projection, and the `native` observation object. Any
   change requires an exact server/SDK revision; no alternate status contract is
   assumed here.
3. Continue to gate content and input through `supported` and
   `followup_available`. Existing modes are `current_agent` and `bound_session`;
   unknown outcomes must not trigger resubmission.
4. Reuse the existing delivery ledger and committed references. Native host
   output must not become attachment bytes embedded in the conversation or a
   second phone transfer service.
5. The Windows host owner must fix its installation/enrollment entry. The
   [current shared command generator](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/apps/web/src/pages/runtimes/command.ts)
   emits the existing `@memohai/runtime@latest` enrollment and service commands;
   this audit does not invent another installer, heartbeat, or synchronization
   configuration path.

## Verification status

### Native Runtime contract received after the source checkpoint

The native owner supplied the following contract on 2026-10-25, but has not yet
provided a pushed, verified implementation revision. PR79 still points to
`fa77a073fe6380d297474a6053a1445201bcab83` at this review. This section records
consumer compatibility and concrete dependencies; it does not claim deployment.

| Contract | Current shared/phone consumer | Required integration |
| --- | --- | --- |
| Windows `provider=memoh`, `kind=remote`; `runtime_id` is registration, `target_id` is a Bot mount | `use-computer-access.ts` grants by runtime ID and revokes by target ID; `chat-pane.vue` preserves the selected target and session snapshot, without matching by host name/provider | Keep new native IDs distinct from old Agenvo IDs. Do not rewrite historical selections, queues or HostBinding. |
| `task_type=computer_operation`, `task_id=operation_id`; operation kinds `foreground_bash` / `herdr_pi_task` | Existing SDK and scoped task readers already use these fields; observation/content/deliveries reject a mismatched operation ID | No alternate phone mapping is needed. Operation kind is not a substitute for a reported runtime capability. |
| Separate status, process state, Pi result, business state and summary | Existing task DTO has `business_state: unknown`; observation DTO uses `business_outcome`. `session-task-results.vue` displays cancel intent separately, and `computer-task-checkpoints.vue` never infers business success from Pi completion | Preserve the two DTO field locations; do not rename one into the other. `revision`, attempt and generation remain strings. |
| Only NAS-committed references count as delivered | `useComputerTaskDeliveries.ts` validates version/direction/hash/size/MIME and exposes only committed references; native saving uses the owning Bot | Runtime/transfer credentials and producer principal remain server/host responsibilities, not phone attachment credentials. |
| Unknown follow-up outcome must not resubmit | `useNativeTask.ts` persists the request UUID before POST and blocks another send until an authoritative receipt resolves it | Keep the existing server capability gate; a new native provider does not enable batch Pi follow-up. |
| Native Herdr owner configuration | No `native-herdr` endpoint, generated SDK method or configuration page exists at fa77 | Await the fixed endpoint/SDK and owner-readable response. Reuse the runtime page; do not manually edit generated SDK or reuse the Agenvo registration form. |
| Admission GET/PUT with request ID, expected revision, paused; offline management without OAuth probe | SDK methods exist, but the page does not consume them. The current service delegates both methods to Agenvo; admission target lookup uses `OwnedAgenvoRuntime`, and PUT calls `Probe` before CAS | The native server branch must land before exposing this write for Memoh runtimes. Reusing the current PUT would not meet the supplied native contract. |
| Native transfer-credential GET/POST/DELETE | SDK methods exist, but the current transfer SQL restricts reads and changes to `provider='agenvo'`; no phone page enrolls transfer credentials | Await native owner/team/activation/revocation enforcement and native identity projection. Do not pass a Runtime `mrk` credential as an `mft` transfer credential. |

The supplied native configuration body requires `instance_id`, `session`,
`work_dir`, `bash_path`, `herdr_cli`, and `herdr_config_root`. Optional `agent`
contains `execution_mode=pi-json-v1`, `provider`, `model`, `config_dir`, and
optional `path`. These belong to the forthcoming existing-runtime configuration
endpoint, not a second phone settings schema. Its precise response/error DTO and
the native capability/availability projection still need the owner's fixed SDK.

For native transfer identity, the supplied contract sets both `connection_id`
and `device_id` to the registered runtime UUID, with the configured Herdr
`instance_id`. It must not be inferred from the old Agenvo selector. The phone
continues to consume task IDs and committed NAS references; it does not mint,
translate or persist that producer principal or device credential.

The task DTO retains `revision` as a string but the task header does not display
it; the observation checkpoint displays its string revision directly. The
delivery reader currently does not consume the page's revision field and does
not infer task completion or implement CAS from it. No cross-page snapshot or
freeze rule is introduced without a server contract requiring one.

Source evidence: [admission dispatch](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/internal/userruntime/service.go#L55),
[current admission lookup/probe](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/internal/agenvo/admission.go#L40),
[current transfer SQL](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/internal/db/postgres/sqlc/runtime_transfer_credentials.sql.go#L19),
and [task DTO](https://github.com/particleflow/Memoh/blob/fa77a073fe6380d297474a6053a1445201bcab83/packages/sdk/src/types.gen.ts#L4453).
This review changed documentation only. It did not repeat the passing tests,
create credentials, change shared product files, or build another HAP.

### Completed checks

The original phone run was **89 passed / 4 failed out of 93** in
[targeted-tests.txt](targeted-tests.txt). Replaying the exact original 1.0.8
checkout reproduces the same four failures ([baseline log](attachment-tests-original-108.txt)).
The four relevant attachment modules are byte-identical between that checkout
and the fa77 phone context ([comparison](attachment-test-context-comparison.json)).
These failures were inherited test-platform mismatches, not evidence of a new
product regression:

- Two assertions expect a browser download anchor; the phone uses a button and
  its existing native `download.fetch → download.save → download.abort` bridge.
- Two assertions expect an image immediately after `nextTick`; the phone waits
  for the media SDK, actual byte length/SHA verification and image decoding.
  The shared fixture has no media SDK mock or valid image byte/hash pair.
- The test's `native` label means a `<dialog>` DOM container, not an OS bridge.
  The corrected phone fixture explicitly supplies the OS boundary in both hosts.

The builder now replaces only this known, SHA-guarded attachment test block with
[phone assertions](../../../mobile/client/tests/stage2/harmony_task_attachments.vitest.ts).
All other shared tests remain intact. The original shared components already
passed **8/8** ([log](upstream-attachment-tests-final.txt)); their Web assertions
are preserved in the locked source. In the generated phone context, the actual
shared component, SDK request construction, SHA/size checks and save adapter run
with synthetic HTTP and OS responses. Assertions cover the owning Bot, committed
versus pending references, preview versus explicit save, no browser fallback on
403, corrupted bytes, Blob retention during polling and revocation on scope change.
No product path was mocked out or changed to satisfy a browser-only assertion.

The corrected attachment file passes **12/12** ([log](phone-attachment-tests.txt));
the original nine-file targeted group now passes **97/97**
([log](targeted-tests-phone-fixed.txt)). The four additional cases exercise corrupt
or denied original reads and native save denial in both DOM hosts. The earlier
[no-project typecheck log](phone-attachment-typecheck.txt) did **not** check the
application: its default tsconfig has an empty files list and project references.
That passing claim is withdrawn. Recreating the entire generated context and
running the existing `pnpm --filter @memohai/web typecheck` command
(`vue-tsc --noEmit -p tsconfig.app.json`) reproduces
[five test-fixture errors](full-typecheck-before.txt). Typing the fetch mock and
explicitly narrowing the known fixture indexes fixes all five without changing
compiler options or removing assertions. The [full application check now passes](full-typecheck-after.txt),
as do [all 12 attachment checks](attachment-tests-type-fixed.txt).
[Test ESLint](attachment-eslint-type-fixed.txt) has zero errors and three
multi-component test-fixture warnings. The generated
context comparison confirms that **only the test file changed**: all 43 product
adaptations remain byte-identical, plus one test adapter. Source inventory now
contains 126 entries / 135 tracked files; the added entry is a new test, not a
claim to recover any of the 19 unverified historical edits.

- [18 delivery-reader checks](delivery-pages-tests.txt) passed, including partial
  page failure and malformed/unauthorized responses. The passing targeted checks
  include scope changes, late responses, task relations, explicit observation,
  authentication stops and unknown follow-up handling.
- [Typecheck](typecheck.txt), [targeted ESLint](eslint.txt), and the source inventory
  passed. The full [UI contract guard](ui-contract.txt) still reports four inherited
  findings in `HarmonyVoiceInput.vue` / `HarmonyVoicePlayer.vue` (spinner/alpha
  styling). Its output is byte-identical to the [previous 1.0.8 guard](ui-contract-previous-108.txt).
  No unrelated voice rewrite, baseline edit or suppression was made.
- [Actual phone-adapted Vue UI](specialist-browser-result.json) at
  `http://127.0.0.1:4181`, Chromium 390x844, Chinese locale, synthetic HTTP and OS
  bridge: select the online Memoh Runtime, disable the offline PC, isolate another
  Bot's targets, traverse specialist/child tasks, show exact receipt revision and
  partial collection offset, pause polling on authentication loss, explicitly
  resume the same task, open/save scoped original attachments, navigate authorized
  history and retain login after reload. Zero task dispatch/follow-up writes,
  zero page errors. This fixture exercises the existing API contract; it does not
  prove the new Windows transport implements it.
- Screenshots inspected by the agent: [native PC selection](native-runtime-selection-390.png),
  [partial collection](collection-checkpoints-390.png), [other Bot isolation](other-bot-isolated-390.png),
  [attachment preview](attachment-preview-390.png), [dark narrow history](expert-history-390.png).
- [Preservation hashes](preservation-check.json) confirm unchanged login, injection,
  native attachment adapters, empty public config and application identity/version.
  No SDK download, HAP compilation or new binary upload occurred.

Run the existing source-context preparation for the pinned lock, then the focused
Vitest files listed in the logs. `HARMONY_TEST_BASE=http://127.0.0.1:4181 python
 docs/testing/harmony-native-runtime-fa77/browser.py` exercises the disposable,
intercepted browser fixture; it never uses a real user profile or live API.

No fixed new native-to-Herdr integration contract, actual Windows execution,
phone overwrite-install, live NAS round trip, or human acceptance is claimed.
Build the next generic HAP only after the interface and validated source are
fixed; do not rebuild or relabel the already verified 1.0.8 package.

## Clean-checkout license hash correction

[Byte comparison](license-byte-review-before.json) found one inherited manifest
mismatch: `misans-package.txt` had 201 CRLF line endings in the retained working
copy, while Git already stored LF. Normalizing those line endings proves the
license wording identical. The manifest now hashes the Git LF bytes and retains
the original CRLF hash separately. No license term, source SDK, product adapter
or HAP changed. The previous local source check passed against its CRLF working
copy; that was not evidence that a fresh Git checkout would pass.

## Generated-context CI gate

The existing Go/ESLint/Docker PR workflows do not include this PR's recovery
base branch. Their skipped governance jobs were never a remote validation pass.
The [Harmony Source CI workflow](../../../.github/workflows/harmony-source-ci.yml)
now targets pull requests into `recovery/harmony-source-20261023` and checks out
the exact PR head, without stored checkout credentials or deployment permissions.

It runs [the source/context check](../../../scripts/check-harmony-source.py),
using the existing source preparer and builder. The 43 adaptation hashes in
[the baseline](generated-web-baseline.json) were reproduced with the builder
from `9fdff87bd8f65d048537ca4cfda77bcdaeae3674` and the fixed fa77 source. The
additional attachment test adapter is checked separately. Only the existing
public source archives and pinned pnpm Web dependencies are needed; no SDK,
HAP build, signing, private configuration or production service is involved.

The blocking commands run inside the complete generated checkout:

```sh
pnpm --filter @memohai/web typecheck
pnpm test --run --testTimeout=15000 apps/web/src/pages/home/components/session-task-results.test.ts
pnpm exec eslint apps/web/src/pages/home/components/session-task-results.test.ts
```

There is no `continue-on-error`. Local logs and the GitHub check for a specific
head are separate evidence; a pending or skipped remote job is not a pass.
The review commit `51de6332ffaa255b98f000e0fb1a54961967e834` was cherry-picked as
`1bbc67ed9ad60543ffc88ca60ada56b3aa838331` after the initial local fix. Its explicit
fixture guards are retained verbatim, together with the later documentation
and original-versus-Git license hash evidence.

The local replay of this gate used the new isolated checkout and the reviewed
fixture: [source check](ci-local-source-check.json), [full types](ci-full-typecheck.txt),
[12 attachment checks](ci-attachment-tests.txt), and [lint](ci-attachment-eslint.txt)
passed (lint: zero errors, three fixture warnings). This is local evidence,
not the status of the remote check.

The first remote head `f3d91b05259d3fbcb6a3e2b61eb96842efdc6f46` passed
[Generated Harmony Web](https://github.com/particleflow/Memoh/actions/runs/40186259058),
but its contribution-policy test failed. Adding the new workflow increases the
ordinary PR workflow inventory from 9 to 10; the exact count assertion has been
updated while retaining every read-only permission and dependency assertion.
The local environment lacks Ruby, so the complete policy suite must be verified
by its existing remote runner; no local policy-suite pass is claimed.
