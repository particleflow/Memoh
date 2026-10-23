# Web management task drilldown checkpoint

Product: `ff1400fef7eb1710ca060904e91637a53a0dc8f3`.
Base remains frozen at `dfc7cf5104c382ecc622061ce43c3dcd733b7998`.
This is a Web-only follow-up. No API, SDK, migration, task dispatch, provider,
permission, runtime or contextview code changed.

## Actual gap and resulting behavior

The existing specialist handoff API already supplies an authorized
`target_bot_id` / `target_session_id`, but the Web dialog could only inspect its
current source session. A handoff with a target session now opens that session's
existing read-only `/tasks` API, using the same task body and reader. Back goes
from task detail to specialist task list, then to the original handoff detail.
The source execution list and delegation cursor remain separate.

The parent handoff continues to be checked while the downstream view is open.
Losing that relation, changing Bot/session/selection/identity, closing the host,
or logging out clears and aborts downstream reads. A late response cannot restore
content. Access is still enforced by the backend on every read; the navigation
button does not grant access. No task session is invented for an accepted handoff
that has not created one.

`BotDelegationTasks.tasksAction` is an optional host callback. Web supplies it;
other hosts omitting it remain read-only with no new button. The existing optional
`deliveryAction` is unchanged. The extracted `SessionTaskResults` body has no
DialogRoot dependency; its host still owns accessibility, navigation and scope.

## Real service and browser acceptance

The existing isolated server, PostgreSQL database and Vite process were reused.
Backend binary SHA-256 is recorded in [checks](web-management-checks.json).
Its source is `7e6bc3a8` (the remaining base commits only added evidence).
The browser used loopback port 18102, API port 18100. No production server,
private user file, real PC or paid provider was contacted.

- The actual Web creation form created exactly two new Bot records. Workspace
  provisioning initially failed and required the fixture recovery below; this
  is **not** a clean default-install creation pass. Both records eventually
  became `ready` without creating replacements.
- The Web settings toggle performed a real PUT, API readback and page reload.
  [Proof](web-management-settings-proof.json). Subsequent synthetic-model setup
  used the normal full settings PUT, which reset that toggle to its default;
  the final PG snapshot therefore does not assert the earlier toggle stays on.
- One source WebSocket invocation ran the real `delegate_to_bot`, then the
  specialist's real `spawn_agent`, then delivered the final response to the
  source conversation. Only the model's SSE replies were deterministic test
  substitutes. Task rows were **not preseeded**. There were six model requests:
  source dispatch, source acknowledgement, specialist spawn, child reply,
  specialist final reply, source follow-up. See [model calls](web-management-model-calls.json)
  and [PG proof](web-management-pg-business-proof.json).
- The browser opened this real handoff, its downstream child session, task
  detail, and back navigation. Observation issued no POST. It also exercised a
  genuine browser network-offline transition and read-only recovery, plus the
  420px Chinese/dark layout. [HTTP/UI proof](web-management-observe-proof.json).
- An explicit **isolated lost-ack fault injection** changed only the completed
  handoff's delivery state to `unknown` and incremented its revision. Its real
  result and existing durable delivery invocation were retained. A browser
  double click sent one actual delivery POST and received `accepted`. The three
  source/target run rows and all six model calls stayed unchanged: zero specialist
  reruns. This does not claim an actual network ACK loss was reproduced.
  [Retry proof](web-management-retry-proof.json), [final PG](web-management-pg-final.json).

The in-process memory queue was used with PG persistence; this is not a Valkey
restart/delivery-consumer recovery test. The earlier independent PG gates remain
base evidence and were not needlessly rerun for these frontend changes.

## Checks and visible evidence

All commands and exit codes are in [checks](web-management-checks.json).
Original text outputs are committed, not just hashes:

- [45 component assertions, 2 files](web-management-ui-final.log), including
  downstream success, repeated click, back, late response after scope/identity/
  relation loss, and separate unsupported/denied/offline errors. Component
  protocol fixtures do not claim an end-to-end authentication refresh test.
- [Web typecheck: exit 0](web-management-web-type-final.log).
- [Changed-file ESLint: 0 errors, 2 test-component warnings](web-management-lint-final.log).
- [UI contract: exit 0, 3 inherited warnings and existing baselines](web-management-ui-contract.log).
- [Web build: exit 0, existing large-chunk warning retained](web-management-build-final.log).
- [Official runtime/contextview boundary: unchanged](web-management-core-boundary.log).

Actual browser screenshots, visually inspected:

| Evidence | Demonstrates |
| --- | --- |
| [Handoff](web-management-handoff-detail.png) | Actual specialist final reply and new navigation |
| [Downstream list](web-management-downstream-list.png) | Actual spawned child identity; honest unknown status |
| [Downstream detail](web-management-downstream-detail.png) | No invented final content or downloads |
| [Narrow/dark/Chinese](web-management-downstream-narrow-dark-zh.png) | Shared layout and localized boundary |
| [Network failure](web-management-network-error.png) | Cleared content; reload is a read |
| [Delivery response](web-management-delivery-accepted.png) | Accepted delivery remains separate from business completion |
| [Settings](web-management-settings-saved.png) | Saved toggle during the earlier workspace-failure phase |

[Manifest](web-management-manifest.json) records the exact committed copies.
Text logs are original output with synthetic/local fixture data; the private
server logs, config, token and credentials are not published. Exact scans against
known local secrets found zero matches; this is not a general secret-detector
claim. Screenshots were checked separately. Evidence is kept as prefixed files
because creating a new evidence directory failed with ENOSPC; writing to the
existing directory succeeded. No disk permissions or limits were changed.

## Failures retained and fixture recovery

1. The first browser script used “Create Bot” instead of the actual “New Bot”
   button, timed out before submission, and PG confirmed no Bot was created.
   A later read-only recovery script similarly used “Reload” instead of “Reload
   view”; correcting that selector did not resubmit business work.
2. Docker initially attempted a write in the read-only default buildx cache.
   An isolated writable test cache fixed that. The fixture used an already cached
   image and added only Docker's init binary; no large image pull occurred.
3. Workspace provisioning returned `workspace_setup_failed` because creating its
   socket directory on the full overlay failed. Previous generated Web output
   was copied to scratch and hash-verified before removal; this did not recover
   overlay space. No source, database, original, volume or global cache was pruned.
4. Only the new test workspace root was configured in scratch; existing
   directories were not moved. The cached Alpine fixture could not execute the
   configured glibc bridge (`tini` exit 127). A previously built static bridge
   was reused after confirming zero source diff for its bridge packages. Only the
   two exact, never-started fixture container shells were removed, without `-v`.
5. The current host attempted UDS while the Docker fixture exposed TCP. A private
   UDS bridge process restored the fixture path. This is an explicit test setup
   accommodation, **not a product Docker bootstrap fix or default-readiness pass**.
   A graceful local-server stop exceeded the first 20-second observation and
   exited at its 30-second shutdown deadline; no replacement started until the
   previous process was confirmed stopped.

Raw failure logs and recovery manifests remain in the cloud checkpoint directory
`/tmp/web-management-checks`; none of the above failures is counted as a pass.
The product commit also has a verified bundle there.

## Remaining product and release limits

- The `/tasks` subagent projection still returns recorded identity with `unknown`
  execution and unavailable content. The real child executed successfully in the
  fixture, but this API does not project that fact. Live logs, true queue positions,
  and immutable per-turn results are not implemented by this frontend change.
- Handoff final summaries are available; task/handoff attachment download actions
  remain absent. This is not an artifact delivery acceptance or a new media path.
- No cancel, resume, business retry or new scheduler has been invented. The only
  existing mutation exercised is **result delivery retry**.
- Harmony receives no new management capability automatically. It can opt into
  the shared presentation through its own authorized host integration later.
- Ordinary creation still needs a healthy supported workspace bootstrap; the
  fixture's transport mismatch should be diagnosed independently.
- OAuth recovery, legacy media P1 and production release holds remain unchanged.
  No production invocation, merge, promotion or deployment was performed.
- Remote Actions were not dispatched or re-enabled; local passes are not remote
  CI results. Human QA is not claimed.
