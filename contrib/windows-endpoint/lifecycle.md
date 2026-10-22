# Unified endpoint lifecycle contract (not implemented)

Automatic recovery must work without an open chat, repeated result queries or a
user clicking a transfer action. This is owned by the **one Windows endpoint's
lifecycle coordinator**, alongside official Herdr and Agenvo process management.
It does not belong to a separately installed transfer server or a duplicate task
queue. This PR has no such coordinator yet and reports
`lifecycle.automaticRecovery:false` / `idleQueueWake:missing` explicitly.

## What the inspected hosts actually provide

| Source | Available trigger | Limit |
| --- | --- | --- |
| [Herdr plugin startup](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/docs/next/website/src/content/docs/plugins.mdx#startup-hooks), [implementation](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/app/api/plugins/runtime.rs) | One-shot after session restoration/API readiness; again for a new server during live handoff. Existing plugin config/state environment is supplied. | No rerun on client attach, config reload, linking or enabling a plugin. Asynchronous failure does not stop Herdr. The hook is not a supervised daemon. |
| [Herdr event hooks](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/api/schema/events.rs) | Workspace/worktree/tab/pane events, including pane exit and Agent status changes. | No idle timer, queue-next-due event, Windows network-restored event or guaranteed event replay in this hook contract. Pane exit does not prove a closed delivery manifest exists. Concurrent plugin commands are capped at 32; a hook can fail rather than wait. |
| [Agenvo connector](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/packages/connector/src/main.ts) | In-process WebSocket reconnect/backoff and heartbeat; status snapshots. | Does not launch/supervise Herdr or call transfer actions. 401/403/426 and terminal socket closes stop network retries and need attention. Restarting the connector must not erase those states. |
| [Agenvo Herdr events](https://github.com/Xuanwo/agenvo/blob/a89415e557c415faf5626354f8e87233eb69d877/apps/herdr/src/herdr-events.ts) | Reconnect native subscriptions and publish `agenvo.resync_required`. | A remote observer notification, not a local transfer wake. A missed subscription needs resync; it cannot be the sole durable wake source. |
| [PR47 plugin](https://github.com/particleflow/Memoh/blob/6ddf61dcd8199758e4110398645c556a3d6d7375/contrib/herdr/memoh-delivery/herdr-plugin.toml) | Finite `collect-result`, `advance-results` and storage-check actions via the official host. | Current manifest contains no startup or event hook. No independent timer or worker is registered. |
| [PR47 nextDue](https://github.com/particleflow/Memoh/blob/6ddf61dcd8199758e4110398645c556a3d6d7375/internal/transferclient/host_action.go), [bounded advancement](https://github.com/particleflow/Memoh/blob/6ddf61dcd8199758e4110398645c556a3d6d7375/internal/transferclient/producer.go) | Existing queue chooses an eligible persisted job, preserves IDs/backoff, serializes workers. `advance-results` performs up to 64 steps on one job and exits within two minutes. | Nothing invokes it when the endpoint is idle. `idle` also covers jobs not yet due; blocked/create-unknown jobs are deliberately skipped. Repeated task queries are a current trigger, not unattended recovery. |

Therefore startup/event hooks can accelerate recovery, but **cannot alone provide
reliable unattended retry**. Putting an endless loop in a one-shot hook would not
give it supervision and would hide the missing endpoint lifecycle owner. No
upstream Agent, Herdr or connector modification is required to resolve this gap.

## Minimum next implementation boundary

The next delivery milestone should add a single internal coordinator to this
same endpoint product. It can initially run in the ordinary interactive user
session; OS login/startup integration is a separate, still unselected deployment
decision. It must be explicit that closing that coordinator or logging out stops
its wake guarantee until the product is started again. No startup/task-scheduler
registration, elevated service or permission change is included here.

The coordinator owns only process lifetime and **wake opportunities**, not
business tasks or transfer state. A wake invokes the existing finite transfer
action through the approved Herdr plugin command path. It never implements
upload/download, allocates delivery/operation IDs, edits a journal, chooses a new
business attempt, or rewrites queue retry budgets. The existing queue remains the
only authority for due selection, leases, retry counters and committed receipts.

Required wake sources, coalesced into at most one outstanding finite invocation
per existing endpoint identity:

1. After endpoint start and Herdr API/plugin readiness, run a bounded reconciliation
   pass on existing durable work. A Herdr startup hook may request the same wake;
   it must not start another coordinator or recreate a task.
2. After an official host restart/live handoff, revalidate the actual session and
   plugin mapping, then resync and wake. PID existence or an old status file is
   insufficient. Herdr session restoration is not proof that old processes or
   business operations survived.
3. After durable admission/completion events, request a wake. Existing pane/Agent
   events can be hints, but the transfer component must validate closed metadata.
4. While the endpoint is running with no open chat, a bounded periodic wake must
   remain available to cover missed events and elapsed backoff. A one-shot timer
   may use an exposed next-due timestamp; a conservative bounded interval is a
   fallback only after error classification is available. The queue checks its
   persisted due times, so this is not a second transfer scheduler.
5. Network/relay availability recovery can request an early wake; it must not
   reset existing queue due times or repeatedly restart a working connector.

Each invocation has an execution/output limit, no overlapping instance, bounded
coalescing, and backoff on host contention. Queue or process exit results are
observations; uncertain completion requires the existing inspect/reconcile
protocol. Update/stop first prevents new wakes, then waits for or cancels the
finite invocation within its documented budget and preserves unknown outcomes.
It must not stop a shared Herdr server or unrelated panes as a cleanup shortcut.

## Interface dependency before implementing unattended wakes

At the inspected SHA, the transfer CLI returns bounded public state but often
collapses an error to `pending_or_unavailable` or `unavailable`. `idle` does not
distinguish an empty queue, a future due time or all jobs being blocked. The
coordinator must not infer authorization or retryability from exit code 1, scan
private queue journals, or treat indefinitely repeated invocation as recovery.

Request the following **bounded public contract from the transfer owner**, without
implementing or copying it in this branch:

- A read-only/versioned queue readiness summary or an equivalent enriched finite
  action result: eligible work / idle-until (when known) / blocked / unknown;
  configuration identity/revision agreement; compact counts with no source paths,
  credentials, payloads or full logs. It must not alter due times or retry budget.
- Stable classes for retryable transport/host-busy versus authentication,
  permission/private-NTFS, configuration/schema mismatch, capacity and uncertain
  commit. Preserve per-delivery proof and let the existing component own the
  distinction; the coordinator only decides whether it may schedule another wake.
- A documented allowed invocation path and completion/result correlation for the
  existing `advance-delivery` action, including multi-session coordination and
  behavior when Herdr restarts while an action is running.

For authentication, revoked authorization, NTFS permission or incompatible config,
surface `needs_attention` and suspend the affected wake/restart circuit until the
existing owner restores it. Do not copy OAuth, repair ACLs, change trust or keep
restarting. Connector auth and transfer-device auth remain separate states under
one device identity; one being connected does not make the other ready.

## Required acceptance for that next milestone

Use an isolated clean Windows environment with a qualified component bundle.
Admit a synthetic transfer, close every chat/UI client, introduce a bounded
network outage, restore connectivity and observe delivery progress with the same
durable IDs **without any result-query requests**. Repeat across host/coordinator
process crashes, missed event hints and elapsed retry due times. Verify one active
worker, no duplicate business attempt, state preservation and a visible suspended
state for authentication/permission failures. The coordinator should recover on
next explicit product start; restart-on-login is not promised until selected and
authorized separately. Process termination is not proof of physical power-loss
recovery. Capacity/log/receipt limits and their evolution remain with the transfer
owner, not this coordinator.
