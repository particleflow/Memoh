# Shared delegation observation and result delivery

The existing `HarmonyTasks.vue` HTML-dialog host reuses the unchanged shared body,
reader and optional delivery composable from
`be6862497a58a01ef8c6f4168f02d5624cd44061`. Source/output hashes and SDK function
names are pinned under `sharedObservation` in `client/config/memoh_web.lock.json`.
`client/mobile/prepare_shared_observation.py` consumes those fixed Git objects;
no second task queue, status mapping, poller or backend protocol is introduced.

The generated compatibility adapter extracts exactly two GET declarations and
the original delivery POST from the pinned SDK, using the baseline authenticated
client. It never imports the whole newer SDK or requests another specialist run.

The held Web builder binds the host capability with the existing predicates:

```vue
:allow-delivery="!activeChatReadOnly && hasBotPermission(currentBot?.current_user_permissions, 'chat')"
```

Absent/false `allowDelivery` keeps a host read-only. Disabled hosts, unsaved
computer choice, missing identity or missing parent session provide no query or
mutation scope. Closing, pagehide, identity changes and scope/capability changes
clear the local reader/action. Backend authorization is still required; the
client predicate grants no permission. Duplicate pending delivery clicks share
one action. Aborting local observation cannot undo an accepted server action.

A successful delivery acknowledgement is not supervisor processing or business
completion; fresh GET state remains authoritative. Old servers without the route
show an explicit unsupported message and stop polling. A scoped session denial
is distinct from unsupported, empty tasks or a PC-offline state. There are no
fake cancel/continue/followup controls, and no Bot/media/hash fallback for scoped
attachments. Final text is displayed as plain text, not raw logs or reasoning.

The source subset includes the integration helper and host but not the inherited
Web builder or complete shell. See `BUILD-REPRODUCTION.md` for the missing inputs.
