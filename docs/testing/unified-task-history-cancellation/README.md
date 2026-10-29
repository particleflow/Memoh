# Cancelable task-history identity reads on the 5a42 release

Integration base: `5a42d013e801cb6229ee029113e3a63782bc3e13`.
Source increment: `76471e0ef3ab9470dcf17c1d85ddb27cece14109`, parent
`8d48dfe7c7e84849c7986608f89a114b7bf85d75`, tree
`420b8a280a89f1a506b5740e141adce39254df92`.
The officially materialized source ZIP SHA256 is
`8d713ff5532a5481dfbc8d7b6828037b344858409e034cf8a16f169e2c02128d`.
All 20 manifest members and the original Git bundle were verified locally.

## Review and actual combination

Only four product files and three existing test files change. The typed task
lookup retains current permission, task/record/run, destination Bot/session and
turn checks. Its read-only request is bounded to 15 seconds and rejects timeout
or caller cancellation without granting history access. Even an adapter that
ignores transport abort cannot keep its caller pending or authorize a late
result. Abort listeners and the deadline are released on settlement.

The pane cancels admission on task/session change, hiding, deactivation and
disposal. Its controller/generation fence allows a new selection to proceed
while an old transport is pending. An old completion cannot release the new
selection's slot or replace its status. Home likewise cancels superseded or
off-route reads and retains its existing route/generation checks.

Six files applied cleanly. The sole Home import conflict was resolved as:

```ts
import { computed, nextTick, onScopeDispose, provide, ref, watch } from 'vue'
```

Every other added/deleted line matches the exact source increment. The existing
Home visibility provider, Dockview re-provider and both speech admission gates
remain present. No template, style, label, management field or button is added.
No API/SDK, backend, migration, Windows or official Agent core changes occur.

## Reused evidence and narrow integration checks

Reuse the author's 36 targeted cases, changed-file ESLint, UI contract check and
actual isolated Chromium/PostgreSQL scenario. That scenario allowed the new
task to become visible in 1.5598 seconds while the old identity GET was held;
it reported abort, no late overwrite, timeout preserving the task URL, no page
errors and zero model calls. This is author evidence, not field acceptance or
a new full-tree browser run. Its 61 runs and 2 unknown records were retained.

Two additional integration seams ran on the combined tree, using actual Home,
the actual identity helper and speech composable, with simulated transport and
audio:

- A superseded identity read aborts while a new task opens; the Home mask stops
  speech, and the late result neither reopens the old task nor restarts audio.
- Leaving for settings aborts the masked task; returning to contact remains
  visible despite the old request's late 403, with no renewed playback.

Both passed, Vitest 3.22 seconds, test body 70ms, exit 0. The initial fixture lacked
the pre-existing refreshBots mock and per-case playback reset. Only that fixture
was repaired; its failed log is retained separately, not labeled a product red.
Combined Home ESLint passed. The author's 36 cases and the old full CI/builds
were not rerun. No new complete-tree CI or native phone result is claimed.

## Consumers, deployment and remaining limits

`source-consumer-map.json` lists the exact four combined product hashes. Harmony
must consume the combined increment while preserving its Back, artwork, login
and attachment adaptations. It must not overwrite 5a42 speech gates with the
older source snapshot. The existing SDK and 5a42 Server remain the contract; only
Web resources and a future matched HAP need these client changes.

The package currently deployed in the field does not automatically gain this
candidate. No image/HAP build, production publication, NAS/Windows operation or
paid call is part of this integration. Schema 179, history, receipt and unknown
identities are unchanged. Retain the 5a42 compatible Server consumer during any
client rollback; do not down-migrate or overwrite newer data with an old dump.

This fixes a separately reproduced reliability defect. The original NAS loading
report's cause remains unknown. General Bot/session/message initialization is
not given a new global deadline here. Physical ArkWeb cancellation, real network
behavior, accessibility and human QA remain unverified.
