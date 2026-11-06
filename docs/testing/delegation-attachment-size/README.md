# Delegation attachment byte-count propagation

Base: `219a722da3b8e4a316164011dd106350dfab2c9e`.

`media.Service.Ingest` measures the persisted original and returns its byte count.
The Images tool emits that count in `size_bytes`/attachment `size`. In contrast,
`Media.Stat`/`Describe` can return a size-less locator derived from the content key;
zero there means unknown metadata, not a proven empty or corrupt original.

The delegation host used that locator's zero in two places: `outboundRefs` built
the expert's message asset link from a fresh `Media.Stat`, and `inputAttachments`
rebuilt the manager's return attachment from `AttachmentSet.Describe`. The message
persistence path copies these supplied values. Thus a new nonempty generated
original could acquire zero-sized expert and manager attachment links even when
the original and its hash were intact. This is a propagation defect, distinct
from whether a size-less locator is allowed by its own contract.

The fix is confined to the existing delegation host. When its locator lacks a
size, it measures the same authorized original through the existing verified,
seekable Media reader. It does not trust a size supplied through a tool argument.
Returning an unchanged input keeps the original source Bot owner and creates no
copy or alias. Current Bot/session/team/reference checks remain in place; return
reads retain per-seek delegation authorization, and outbound capture rechecks
current authority after I/O before returning a link. Reader errors or revocation
are errors, never an invented size or an instruction to regenerate.

This can add existing original-verification I/O when metadata is unknown; it does
not call a model/provider or add a storage service, database column, cache or
Agent runtime change. The generic Media locator/Describe contract is unchanged.
There is no scan/backfill of old history and no production write or image replay.
Only fresh canonical synthetic originals are exercised; no legacy media import or
repair path is changed or reproduced.

## Targeted evidence

A valid synthetic 2×2 PNG is ingested into fresh per-Bot originals. Its measured
size is 74 bytes. On the fixed base, both a new output and an unchanged returned
input produce zero in `outboundRefs` and the return attachment despite the
original being present. The red test exits 1. With the fix, both carry 74 bytes,
even when the event claims a different size. Hash, owner and scoped URL stay the
same, with no destination-scoped copy.

Additional cases reject revoked access, revocation during original open, wrong
team/session, undelegated reference and missing original before a reference is
published. The existing original-route cases are included as the affected
regression, not a full business/model rerun.

```sh
go test -race ./cmd/internal/core -run '^TestDelegation(AttachmentSize|HostRestoresOriginalRoute|LegacyAndChangedReturnRoutes|LocalChannelCompatibility)' -count=1 -v
golangci-lint run --new-from-rev=219a722da3b8e4a316164011dd106350dfab2c9e ./cmd/internal/core
bash scripts/check-agent-core-boundary.sh
git diff --check
```

These are isolated source/Media tests, not a repeat of the field generation or
proof that existing production rows have changed. For exact field attribution,
the owner needs only the same reference's tool `size_bytes`, emitted attachment
`size`, expert message-asset `size_bytes`, and manager message-asset `size_bytes`,
plus the original delegation/target-run association. No prompt, image bytes,
provider configuration or credential is needed. A size-less download locator by
itself cannot establish which persisted message link had size zero.
