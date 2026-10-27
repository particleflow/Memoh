# Current shared observation entry

The active producer is `client/mobile/build_memoh_web.py:context_archive`.
It reads the complete archive pin and guarded overlays from
`client/config/memoh_web.lock.json`; it does not call the legacy standalone
observation preparer or inject the old `HarmonyTasks.vue` dialog.

Current activity/conversation sources are the `sharedActivity` and
`sharedConversationMonitor` overlays. Their local shared component, reader,
route and test modules are listed by the builder's `inputs()` function and
verified against the lock. Existing session-task/result views come from that
composed shared Web source, with the original phone attachment/test adaptations.
See [the 1.0.19 alignment record](../docs/testing/harmony-unified-1.0.19/README.md)
and [the active reproduction guide](BUILD-REPRODUCTION.md).

Observation uses the existing authenticated shared routes and server-returned
scope. It does not add a second task queue, execution service or permission.
A successful delivery acknowledgement is not supervisor processing or business
completion. Scoped attachment references must preserve their exact delegation
path; an owner Bot/hash URL is not an authorization fallback.

## Legacy source awaiting owner retirement

`client/mobile/prepare_shared_observation.py`, `client/mobile/memoh/HarmonyTasks.vue`,
`client/mobile/memoh/harmony-tasks.ts` and `harmony-tasks-sdk.ts` are still tracked.
The standalone helper reads `sharedObservation`; the separate stage2
`harmony_tasks.cjs` directly tests the legacy task adapter. These references do
not make the helper part of the current producer. Its header's claim that the
real builder calls it is stale.

This is an identified cleanup handoff, not a compatibility exception or a
claim of zero residual code. The original native owner must retire the helper,
legacy host/adapter and dependent tests together with their source-manifest
entries, exact-count guard and lock metadata, or identify a concrete remaining
consumer. Do not change the active archive/overlay guard or reconstruct missing
historic source merely to make that cleanup pass. The
[old observation audit](../docs/harmony-observation-recovery.md) records the
transition and confirms the legacy host was already outside the shipped build.
