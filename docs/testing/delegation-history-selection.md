# Delegation history after source selection changes

Base: `dbff0e472c616f7241635a58e6085442f30a7d2a`. This is a backend list
projection repair, independent of contact-navigation PR89 and cleanup PR90.

## Confirmed failure and boundary

The field report confirmed an owned completed delegation in the requested
source conversation, with matching user/team/ancestry and a valid target chat.
Its saved source selection was the native workspace; the current selection
differed. There were no output references. The API returned HTTP200 with empty
items because the list reused execution authorization and omitted ErrDenied.
Private account, conversation, device and deployment values are not included here.

Before the repair, two controlled tests reproduced empty items: an original
native delegation after selecting a registered native remote Runtime, and the
same case with an output reference. The test fixture initially used incorrect
Record field access; that compilation error was corrected before these semantic
red results. No execution, retry or historical data rewrite was used to recover it.

The list retains the existing full projection where authorized. If that projection
is denied, it rechecks current login, complete owned source ancestry, original
root, both Bots' live chat grants and the original target chat's owner/team/Bot.
Only the existing reference and durable state fields may then be returned, without
summary or attachment capabilities. Non-authorization read errors still propagate.

Source-list admission and cursor checks remain unchanged. Execution, delegated
continuation, delivery retry, detail and attachment reads
keep the original selection/device checks. The common ownership check is factored
without adding ancestor queries to the successful execution path. No public API,
SDK, migration, configuration, permission or official Agent core changes are added.

## Verification

- New history-reference, seven live denial cases, original-file separation,
  normal-result compatibility, unchanged unknown and durable-read error tests pass
  with race detection. Original rows/revision, task/session/run references and
  dispatch counters remain unchanged. Original-file opens and legacy MCP calls
  remain zero in the changed-selection case.
- JWT/Echo HTTP regression returns the original target Bot/session in the list,
  while anonymous access stays401, original binding-protected actions stay404,
  and live revoked access is rejected. The prior JWT handoff regression also passes.
- Targeted race tests and affected-package vet pass. These are controlled service,
  identity, storage and HTTP fixtures, not PostgreSQL or real-device acceptance.
- The staged repository aggregate passes: affected-package Go lint166.84s,
  complete tests for both selected packages9.29s, total176.39s. Gci/gofumpt/
  goimports and diff checks pass. An initial formatter attempt selected the
  system's unrelated `go` executable; it was rerun with the project Go toolchain,
  without disabling any formatter. Exact-head remote CI belongs to the handoff.
- No NAS deployment, model call, new HAP or field acceptance occurs here.

The Web relation reader already consumes these existing IDs and follows the
original target session; no new UI or global Bot discovery is introduced.
The earlier completed-delegation desktop/narrow browser evidence is reused as a
client-contract check, not relabeled as a current live Server/PG verification.
