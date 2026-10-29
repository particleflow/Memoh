# Native transfer background entitlement: stage 2

This standalone candidate adds a DATA_TRANSFER continuous-task owner to the
existing EntryAbility and native transfer owner. It does not create a transfer
service, queue, file protocol, credential store, or automatic retry loop.
The fixed stage-1 prerequisite is `4f7d7a84fddba50665f97b2cf4341e4139f37d14`.

The only manifest differences are `ohos.permission.KEEP_BACKGROUND_RUNNING`
and EntryAbility `backgroundModes: ["dataTransfer"]`. The permission is
[normal/system_grant](https://github.com/openharmony/docs/blob/master/en/application-dev/security/AccessToken/permissions-for-all.md#ohospermissionkeep_background_running).
The [official task guide](https://github.com/openharmony/docs/blob/master/en/application-dev/task-management/continuous-task.md)
identifies this mode for non-hosted network transfers, requires live-view
progress, and allows system cancellation when progress stalls. This source
uses the existing RCP engines. No notification permission prompt, device
battery configuration, signing, installation or setting change is performed.
Actual device notification eligibility/authorization remains unverified; a
publish failure pauses I/O and retains existing checkpoints.

## Ownership and state transitions

`EntryAbility.onCreate` binds `NativeTransferBackground` to the same process-local
`NativeTransferOwner`. Only an accepted upload run or download fetch calls
`begin`; document attach/detach, import, preview and picker operations do not.
A reattached observer joins the same work promise and does not acquire another
task. Upload/download share one system grant. The last finished operation,
account revocation or ability destruction releases it. Page destruction alone
still preserves accepted work. The account drain also waits for system cleanup.

Acquisition and notification/release are serialized. A late successful grant
is released if the final operation ended, logout occurred, or ability destruction
occurred while start was pending. A matching system cancellation pauses both
existing engines once. Cancellation before the task ID becomes available is
correlated with the returned ID before cleanup. A new explicit operation can
retry after a confirmed cancellation; no automatic acquisition follows it.
Acquisition rejection or uncertain cleanup blocks further acquisition on that
controller, avoiding a second grant while the outcome is unresolved. A new
ability binding is a separate controller, with the old cleanup drained first.

Notifications reuse the returned notification ID/slot and official type-8
system live-view download template. Progress is the byte-weighted percentage
of current active operations, with no estimate before all have reported an
actual checkpoint. Upload reports occur after validated server receipts and
atomic journal/directory sync. Download reports follow data fsync and atomic
checkpoint/directory sync. Identical byte values do not publish. There is no
heartbeat, idle extension timer, invented speed, deadline, or budget. Full bytes
mean transferred bytes; hash verification can still be pending. Saving through
the foreground picker has no DATA_TRANSFER guarantee.

Release waits for an in-flight publish, then cleans the owned grant and its
notification. A confirmed system cancellation skips stopping the already
canceled grant. Only official notification error `1600007` (nonexistent) is
accepted as successful notification cleanup; service/permission failures remain
uncertain. Application-triggered stop callbacks cannot revoke a newer operation.

## Verification and limits

`result.json` records current source hashes, commands, statuses and raw logs.
The controlled adapter runs production controller code and covers late grant,
shared ownership, byte progress/freeze, matching and unrelated cancellation,
logout, pending publication, denied request/notification, uncertain release,
ability destruction and exact SDK call arguments. The real-engine fixture uses
524301 bytes, actual temporary disk and controlled HTTP. It verifies that each
progress report already exists in durable journals, and that OS cancellation
retains the upload receipt/original and download part, with explicit resume.
Stage-1 account, single executor, pause/abort, unknown Create and page lifecycle
regressions are reused. This is not phone RCP/TLS or OS scheduling validation.

The declaration check uses only pinned official text from
[interface_sdk-js 8a86234f](https://github.com/openharmony/interface_sdk-js/tree/8a86234f0d542e5062e24e948652f5a484d90904).
`official-types.json` fixes hashes. It checks the source with TypeScript and
`skipLibCheck`, plus four deliberately invalid API inputs that must fail.
The selected declarations do not contain every transitive SDK dependency; this
is declaration-surface checking, not the official ArkTS compiler. Reference
files keep their Apache-2.0 headers and are included only in the recovery packet.
No SDK binaries or build tools were downloaded.

The original owner independently compiled exact stage 1 using official SDK
26.0.0.105/API26 (`default@CompileArkTS`, exit 0). Its evidence is
`libfile_30a741a4b4ac8191bacb47e451bcd5f8`; this does not qualify stage 2 or a
combined tree. Stage-2 official API26 compilation remains for the existing SDK
owner. No HAP, signing, installation, real notification/lockscreen/background,
rendered UI, process-death execution, human QA or deployment has been performed.
Continuous tasks do not establish execution after process death. Original
journals and authenticated explicit restore remain the process-restart path;
ordinary 24-hour upload receipt expiry and task lease rules are unchanged.

See `COMBINATION.md` for the exact page-network/transfer ownership seam rules.

## API26 correction after ea615a142

The existing SDK owner compiled exact `ea615a142` with official SDK26.0.0.105
and found two source errors (aggregate ERROR:3, two actionable locations).
Evidence: `libfile_6ea77beb2a6481919d9f3243efc6f1ca`. The correction changes only
`NativeTransferBackground.ets`: public `notificationSlotType: lease.slotType`
replaces the deprecated request field, and `throw error as BusinessError`
preserves the original cancellation error object/code/cause. Code1600007
remains the only ignored cleanup error. No lifecycle or state logic changes.

The targeted adapter now rejects the deprecated request field and checks exact
error identity/code/cause. The declaration check adds an invalid public-slot
negative case. `api26-fix-result.json` records current corrected hashes and
checks; the earlier result.json and inventory-refresh.json remain historical
ea615a142 evidence. The corrected source still needs the existing owner's
official narrow compile; the partial declaration checker does not validate
ArkTS's typed-throw restriction. No SDK download, new HAP or device action.
