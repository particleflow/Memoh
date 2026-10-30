# Bot task directory from read-only execution history

Product: `3da802de31f9aede3dbe1124319942d9015abdac` on deployed Web
`2c0e277c2cb49b5a4a99b89a5e568ef9f413ab8a`.
Server reference: `3f361e38a0a30fe52248c9361ead008c052ef2ea`.

The existing Bot task directory was mounted only inside the writable composer
(`chat-pane.vue`, `v-if="!activeChatReadOnly"`). `chat/targets.ts` deliberately
keeps every non-contact session read-only. Consequently an authorized recipient
execution history could show its original messages and permit directory GETs,
but had no visible directory button. This is an entry omission, not a reason to
make execution history writable or resolve/create another main conversation.

The only product edit adds ten template lines: the same `BotTaskDirectory`
component in a read-only footer, using the pane-owned Bot and visibility. The
writable composer, directory contents, authorization, identity rechecks and
task/run/turn route stay unchanged. No fields, copy, models, schema, API or
official Agent code change.

## Narrow validation

- The actual pane-template boundary with the actual directory component fails
  before the fix (missing button: one failure, one pass) and passes both new
  cases after it. The second case has no Bot identity and offers no entry.
  The first opens the recipient directory, clicks the original occurrence and
  checks the exact recipient `task_bot`, task, record, session and run query.
  Pane setup/transcript children and API transport are fixtures in this test.
- Changed-file ESLint, Web vue-tsc, UI contract and diff checks pass. Existing
  directory, routing, Go and full-tree test suites were not rerun.
- Actual Chromium used the running full Vite application at
  `http://127.0.0.1:49955`, a compiled 2c0 Go server and a fresh, separately owned
  PostgreSQL container migrated to181. The directory handler/service/query are
  byte-identical to3f361. All Bots, sessions, delegation, completed run and two
  messages in this fixture are synthetic rows; no model or task was executed.
  No production/private database, NAS/PC connection, or denied metadata read
  was used.
- The real original application renders both messages with zero task triggers.
  The corrected application renders one trigger and no input. Directory and
  typed detail/session reads return200; clicking the received delegation opens
  its original execution, with recipient `task_bot` and the original message
  visible. Desktop and390x844/Chinese/dark runs pass, with zero page errors.
  The five fixture tables' full-row hashes/counts are equal across navigation.
- Cold fixture initialization is recorded separately: each final browser run
  calls the existing idempotent contact-resolution POST once before the task
  action. The five measured business tables stay equal in these final startups.
  Navigation after pressing
  Tasks has only GET requests and no attempted write. Earlier fixture setup
  created one synthetic contact before the measured runs; this is not evidence
  that arbitrary cold Bot entrances are write-free.

Initial harness attempts needed a raw-SFC import, completed onboarding metadata
in the new fixture, and a prepared contact. One invalid browser attempt overlapped
temporary native-template validation; only later serialized runs are accepted.
These setup failures are not product red/green results or field observations.

Screenshots were actually inspected:

- [Original read-only history, no entry](red-readonly.png)
- [Recipient directory: existing name and state](green-directory.png)
- [Original execution after the row click](green-task.png)
- [Chinese/dark narrow directory](green-narrow-directory.png)
- [Chinese/dark narrow original execution](green-narrow-task.png)

## Harmony consumer

`consumer-map.json` and `harmony-consumer.patch` target the actual final HAP34
composer from source `fcc39b31b1db11e7a9d7396e34c94d5b3301a778`, including its
official transfer-lifetime adaptation. The original embedded hash and each
official lifetime seam were verified before applying the identical ten-line
entry. Both new template/directory cases pass against this adapted consumer.
The patch preserves native file/voice/Back/diagnostics behavior and uses the
existing component, SDK and authorization. Its owner must consume the increment
and update the source mapping/lock; it is not already in distributed HAP34.
No native package build, signing, distribution, installation or ArkWeb test ran.

## Sole field writer handoff

The parent's latest field summary establishes the original delegation's list,
detail and message GETs200, matching execution identity and visible messages.
The original `F:/NAS-Assistant/tmp/recipient-task-3f361e38/OUTCOME.json` and
`navigation-detail.json` were not read in this environment. The visible row click
remains unaccepted on production2c0, where this footer is absent.

After the unified owner consumes/releases this fix, reuse the already open
recipient execution history. Do not switch/create/bind a main contact just to
reach the directory, force a hidden element, replay delivery, or touch metadata.
Press the now-visible Tasks button and the same original delegation row:

```
recipient Bot: 791af2c4-2d89-4c57-b863-5d5ff53ff9f7
task_type: bot_delegation
task_id = record_id: 1d742cc8-2760-44f7-a889-d66cd8461502
destination session: c736a091-4c7d-4196-9cd6-00588de2a0ae
destination run: 9dfd7812-592c-482c-8083-abacda7278d9
```

Require the directory and detail URL under the recipient Bot; route `task_bot`
must be that recipient, with the same task/record/session/run. Match authoritative
turn and original message IDs from the writer's existing raw receipt; those IDs
were not provided here and must not be invented. All authorization failures or
missing destinations remain genuine unavailable outcomes, with no fallback Bot.
Only this entry/row interaction is new field work; do not repeat old samples.

This handoff is not deployed/field accepted and does not establish parallel
execution, natural result return or overall Bot/Harmony acceptance.

- [ ] Human QA passed

## 9270 first-click field follow-up: unresolved

Reference: `9270da0252128a296d9a641235cdfd32ab44cfda`. The parent reports the
recipient button and row click are now visible and all original task/Bot/session/
run route identities match, but the first entrance did not finish loading.
One refresh of that deep link displayed the two original messages. This is
recovery evidence, not a successful first entrance. The original field JSON
files under `F:/NAS-Assistant/tmp/deploy-9270da02/` have not been read by this
owner, and the cause is not established.

Source comparison verifies that the pane, directory navigation, run anchor,
realtime controller and transcript are byte-identical between the admitted3da
product commit and9270. The ten-line entry does not introduce an asynchronous
controller. Three distinct gates need discrimination before changing code:

1. `pages/home/index.vue`: the outer workspace is masked while history identity,
   session selection or contact admission is pending.
2. `store/chat/transcript.ts`: a masked initial history load can have received
   HTTP200 but still await its commit callback before revealing the transcript.
   `store/chat/realtime.ts:259` waits for the initial runtime snapshot at that
   callback. This preserves the atomic history/runtime projection; the wait is
   not proof of this field failure's cause.
3. `composables/useTaskRunAnchor.ts`: a task pane remains pending until its
   authorized original turn can be located; `chat-pane.vue:18` separately masks
   that pane. Its identity GET has a15-second/cancellation bound; ordinary
   session/message initialization is a different lifecycle.

The server's `internal/handlers/runtime_ws.go` explicitly sends an authoritative
snapshot for every subscription, even with a cursor. Do not infer that a cached
cursor legitimately suppresses the first snapshot, remove the atomic wait,
replace an original run with latest, or call refresh success a fix.

The historical single-extract request below is fulfilled by the parent excerpt
in the next section. Do not repeat that unchanged file-read request.

Request one read of the already saved `ui-row-click.json`, `ui-final.json` and,
if necessary, `OUTCOME.json`: preserve pre-refresh timestamps/elapsed time, URL,
the exact visible loading node/text/selector, existing HTTP completion/pending/
abort/error records, page errors and any existing pane/loading/anchor values.
If already captured, include the original session's runtime subscribe/snapshot/
dropped/error/close frame metadata and its timing; do not include credentials or
message content. Mark absent fields unrecorded. Do not repeat the field click,
query private DB metadata, replay a result, or create a new workload to obtain it.

No new reproduction, product fix or test pass is claimed for this symptom.
After the evidence identifies a distinguishable transition, reproduce only that
transition in isolation, record its red/green result and smallest fix here.
Any new long-running fixture must retain its real executable tool session for
normal cleanup. The previous denied `/proc/29353/cwd` read and unverified cleanup
state remain separate; this investigation does not authorize another route to
that data or signalling those old processes.

### Incident extract received; one instrumented read-only capture proposed

The parent has now supplied the original writer's one-time extraction from
`navigation-incident-extract.json` (the raw file is not independently read here):

| Recorded UTC point | Established fact | Limit |
| --- | --- | --- |
| 09:13:59.119Z | Original-message selector timed out after15 seconds; `renderedMessages=[]`; snapshot showed loading | Original component request start/completion/pending/abort/error, gate state and WS timing were not recorded |
| Following that timeout | Manual read-only task-detail/session/messages probes returned200 | They are not the original component's completed requests |
| 09:14:50.062Z, after manual deep-link refresh | Captured related UI requests returned200; no pending request was saved | Not evidence of the first entrance's network lifecycle |
| 09:15:40.441Z | Both original messages were in the viewport and loading was false | Recovery after refresh; not first-click success or a measured first-load latency |

The saved exception came from the extension's `toggleTranslatePage`. It does not
establish a product cause. The receiving Bot/session/task/run identity matches;
the unresolved question is the initial loading lifecycle. No new reproduction or
product correction has occurred from this extract alone.

See [the single-capture handoff](capture-plan.md) and
[the Ego CDP observer](capture-ego-navigation.mjs). This is newly authorized,
instrumented observation of the same original record, not an unchanged repeated
click, another model workload or a reason to cancel snapshot consistency. The
observer remains diagnostic: local contract/DOM tests do not establish field
SDK compatibility or successful reproduction. The preflight update below
supersedes the initial missing-envelope/help condition.

### 60377 field preflight: envelope checked, click retention undocumented

The parent relays `navigation603-preflight/OUTCOME.json`: the sole writer checked
direct `{method, params, sessionId}` events, direct CDP/evaluation returns and
completed GET body retrieval. No production row was clicked. This owner did not
read that raw receipt. Official `page.click(selector, options)` help does not
promise event-buffer retention; it is not an actual tool rejection or a reason
to request new user authorization. Do not repeat those checks or the eight
already passed local tests.

The next bounded action is the [isolated click-buffer experiment](capture-buffer-contract.md)
in a newly owned disposable blank Ego page. One synthetic button click produces
only local Console/data-fetch markers and a local DOM count. Save the no-click
control and before/after batches, then close that owned page normally. No
production page, service, model, API or DB is involved. No fixture/server process
is launched. This experiment is prepared, not field executed.

The original-record capture must label its click boundary potentially incomplete
even if the experiment retains both marker classes. DOM gates and retained
positive events remain useful; absent events cannot prove the root cause. The
first-load product cause and actual field capture remain unresolved. Old C05
process cleanup/read denials remain untouched.

### d74 live precheck: observe unrendered ordinary history first

The parent relays `F:/NAS-Assistant/tmp/navigation-d74-live/OUTCOME.json`:
ordinary original session URL/no task query; Home unmasked; read-only/no input;
scroll `block`; eleven skeleton nodes, zero rows, neither original UI ID and
no anchor placeholder. No Tasks dialog/row click, refresh, manual API or model
followed. Spaces58/59 were cleaned with no in-flight writer. Raw JSON was not
independently read here. This supports historical hydration pending, not rows
already rendered and hidden by the task anchor. REST versus runtime remains
unknown. The prerequisite of already healthy history is superseded.

The same parent update confirms d74's isolated baseline/before/click/after markers
and DOM count1. Explicit official `page.goto('about:blank')` corrected the fresh
page assumption; the earlier nonblank start was not a permission rejection.
This sample covers its synthetic non-WS events only, not permanent retention or
the next navigation's WS delivery. Do not rerun it or the earlier eight tests.

The sole writer's next action is [one initial-load capture](capture-initial-history.md),
using [the observer](capture-initial-history.mjs). It enables observation on an
explicitly blank owned page before the one ordinary-history navigation; it
requires no rendered message, Home admission, DCL event or task button to sample
DOM. It does not refresh, submit work, query DB, send manual API/WS, create a
service or remove snapshot atomicity. Unexpected writes are observed outcomes,
not claimed prevented/undone.

Four new local contract tests pass, including unrendered history without DCL,
SSE versus finite pending reads, original completed-response metadata and denial
without fallback/re-navigation; targeted ESLint/syntax/diff checks pass. Old
product suites/contract experiments were not rerun. No field run/product fix or
incident red-green is claimed. Any product change waits for a distinguishable
actual transition; keep original failure/recovery/history and C05 cleanup intact.

`compatibility-register.increment.patch` is a C11-only documentation increment
against register source3a2e5d97, for the unified owner to apply to its current
register after checking context. It retains prior cases/history, avoids assigning
a possibly used new case ID and records both the blank-page lesson and revised
hydration investigation. It does not replace a newer whole register.

### a766 premature stop: distinguish native contact initialization

Parent relays `initial-history-a766/{OUTCOME.json,journal.jsonl}`: two naturally
initiated contact POSTs completed200; original component message GET completed
in27.572ms. Only early interactive/no-dock DOM was sampled before the observer's
HTTP-method guard stopped. No target runtime/socket had been recorded. Space62
closed/no in-flight writer. Raw files not read here; contact bodies/before-state
were not captured, so binding effects and actual caller remain unknown.

The [contact initialization follow-up](contact-initialization-followup.md) traces
9270's real registered account/Chat-permission resolver and its distinct existing,
first-adoption and creation branches. Creation can run SessionStart hooks;
HTTP200 does not prove a no-op. Public `/contact/bind` is actually closed403.
Normal bodyless contact admission is not a privilege expansion; method-name-only
stopping was too broad and lost useful history/runtime observation.

The collector now observes only exact manifested Bot/origin normal resolver
POSTs without query/body, saves completed response scope/identity metadata and
retains binding effects as unknown. Other mutation, invalid/changed scope,
non200/failure and actual read-denial conditions still stop without fallback,
rebinding or re-navigation. Its three new targeted tests pass; one first failed
on a766's old guard, so this is collector red/green only. Old8/4 tests and unchanged
builds were not rerun. No product/root-cause correction or field retry is claimed.

Minimal pending evidence is the two saved POST paths/request IDs/method/status/
terminal (initiator only if already present). No new page/API/DB is needed. Use
that excerpt to configure exact initialization scope; do not claim these two
actual requests match the resolver until their paths are known. The sole writer
remains free for distribution/HTTP work; this update does not take over the field.

### cefdf0 alignment: old resolver families established; effects still unknown

The parent relays the writer's existing-only publication observation:
`181448.409` (红玉) and `181448.417` (文本校验Bot), both POST
`.../sessions/contact`, completed200/loadingFinished in18.706/17.660ms.
Initiator/body were not captured; no new page/API/DB was used. The old excerpt
request is not renewed. This establishes normal resolver route family, not its
old binding effect. Literal UUID paths/origins were not forwarded here; names
cannot fill the exact manifest. The writer already has the actual saved rows.

Production Server is now reported at `cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f`,
schema181. The [source alignment and next single capture](cefdf0-source-alignment.md)
compares9270/cefdf0: route/contact authority, ordinary REST fetch, initial
snapshot wait and pane/anchor DOM are unchanged. Typed input-source propagation,
runtime projection/normalization and Web labels do change in that commit. Do
not infer loaded Web artifacts from the Server revision or call all Web source
unchanged. These deltas do not establish a hydration fix or cause.

544's collection contract remains usable. The only executable change this round
separates reviewed source, caller-reported Server/Web revisions (`null` when
unknown) and loaded static resource observations; it does not widen the guard
or change collection/permissions. Default reviewed source iscefdf0 rather than
mislabeling the next record9270. Exact scope is recipient T plus the two Bot
UUIDs from saved request paths, and existing verified API origins. Natural
resolver effects stay unknown; no history retargeting or new API is included.

Only source comparison and metadata lint/syntax/diff checks ran; old8/4/3 tests,
builds and field samples were not repeated. No field ownership/deployment/model/
DB action or product root-cause claim follows. The parent alone schedules the
next actual observation when its distribution/HTTP priority permits.
