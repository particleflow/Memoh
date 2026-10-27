# Current shared observation entry

The active producer is `client/mobile/build_memoh_web.py:context_archive`.
It reads the complete archive pin and guarded overlays from
`client/config/memoh_web.lock.json`. The standalone observation preparer and
its unused task dialog/adapter have been removed from the current source tree.

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

## Completed legacy-pipeline retirement

The removed pipeline comprised `prepare_shared_observation.py`, `HarmonyTasks.vue`,
`harmony-tasks.ts`, `harmony-tasks-sdk.ts` and its exclusive `harmony_tasks.cjs`
regression. `tests/harmony/test_attachment_inputs.py` was also removed because
it imported only the retired preparer and read its obsolete lock object.
No current production entry called this pipeline. The complete pinned archive
already supplies shared task/result readers and scoped-attachment regressions;
the current activity/conversation overlays remain unchanged.

The unused `sharedObservation` lock object and five current manifest entries
are removed, with the exact reviewed-entry count updated from 167 to 162.
The three obsolete source headers disappeared with their implementations;
no replacement runtime logic or alternate entry was added. The original source
is recoverable from [the exact pre-cleanup checkpoint](https://github.com/particleflow/Memoh/tree/235539039409f595c3cb613ab6f9741b821b6e42).
The [old observation audit](../docs/harmony-observation-recovery.md) remains
necessary to distinguish that earlier source/validation boundary from this one.

The native materials recorder includes all files under the phone module directory
in its source identity. Removing unused modules and lock metadata therefore
changes the next formal source identity even when generated Web bytes are equal.
The recorded 1.0.19 HAP and its original build receipt are unchanged; neither is
rebound to this cleanup tree. Archive hashes, the 45+2 adaptation guard, native
identity/configuration, licenses and runtime code retain their existing checks.
