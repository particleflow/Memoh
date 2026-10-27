# Native Runtime file-transfer configuration

File-transfer authentication and native Runtime control authentication are
separate purposes. The existing Server verifier uses the activated, non-revoked
owned native Runtime (`provider=memoh`), its configured Herdr instance and current
account/team access. Legacy Agenvo registrations cannot enroll or verify a new
file-transfer credential. Old credential rows and original HostBinding values
remain stored; they are not converted into native authorization.

The current binding and operation checks are specified by the
[native Runtime to Herdr contract](native-runtime-herdr.md#file-principal-handoff).
The existing owner-only `/users/me/runtimes/{id}/transfer-credential` interface
returns credential metadata on GET; enrollment/rotation/revocation use the
existing expected-ID compare-and-swap. A Runtime key (`mrk_...`), account JWT,
model token or old OAuth token must never be supplied as a file credential.
A lost enrollment response requires a status read and explicit rotation, not an
automatic second enrollment.

The finite producer references the owner's existing `transfer.json`, private
credential file and config/state directory pair. Native task collection invokes
the approved producer EXE directly with the pinned pair; it does not require
re-enabling the retired Herdr delivery hook. Current host-side configuration,
queue-isolation limits and unpassed acceptance gates are documented in the
Windows endpoint candidate's exact source manifest. No secret placement,
credential movement, installation, trust change or enabling action is performed
by this document.

Keep populated queues, receipts, device triples, raw HostBinding bytes and old
unknown task locators under their original identities. A native control
connection does not authenticate an old queue under a new principal. Changing a
Pi path or installing the new endpoint cannot supply that mapping. No queue
re-rooting, credential substitution or model/task replay is a cleanup step.

Retain the installed schema and ledger records during application rollback.
Do not run historical migration downs to remove retirement residue: removing
credential columns or delivery state invalidates original recovery evidence.
Tests in disposable PostgreSQL/HTTPS/Windows fixtures establish only their
recorded source and synthetic boundaries. Production HTTPS, native owner/file
mapping, returned files and PC/NAS recovery require the designated writer's
separate acceptance evidence.
