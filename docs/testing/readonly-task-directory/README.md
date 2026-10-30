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
