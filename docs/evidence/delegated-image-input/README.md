# Delegated reference image input

Base: `4d23244c683aca4c436efec4db375921fdf60472`.

## Proven source gap and minimal change

`delegate_to_bot` already accepts `attachments: [{content_hash: "..."}]`.
The host derives request identity from the original tool call, checks the source
conversation link and both current Bot grants, and persists the declared input
references. The specialist session is a separate chat session, not a same-Bot
subagent. Its prepared host context restores the durable delegation scope.

The existing `delegationAssetLoader` can read those exact source-owned originals
under current authorization. Before this fix, the Images reference tool bypassed
that loader and called `media.Open(targetBot, hash)`, which could not read the
source-owned original. The corrected path injects the same host loader through
the Images provider's normal setter mechanism. No configuration can repair that
bypassed reader. No Media alias, source copy, broad grant, new credentials,
endpoint, queue, workspace storage or official Agent core change is added.

The Images tool still checks current conversation access, reads all original
input bytes, verifies SHA256 and size/MIME constraints before provider dispatch,
then persists returned output through the existing target-Bot Media service.
A scoped denial never falls back to another owner. Existing same-Bot ownership
behavior remains the default when the standalone provider has no host opener.

## Existing ordering and configuration

The direct Web composer awaits `uploadComposerAttachments` before calling the
chat store. `uploadMedia` requires the original upload receipt to become ready,
then reads the original back and verifies the complete hash before returning a
reference. Upload failures prevent that send. This ordering already exists;
this patch does not add another upload or scheduler.

Delegation attaches only explicit references in its input array, not hashes
mentioned solely in prose. The source session must be linked to the asset, and
the input must remain in the durable delegation scope. Existing target Bot image
model configuration and the existing `images_api.edits` capability are still
required for reference transformation; no model ID or provider is invented.

The conversation gateway prepares attachments before invoking the target model,
but can fall back if preparation fails or the chat model lacks image-input
capability. Existing upload ordering therefore does not prove the actual target
model received the image. This change specifically repairs the Images tool's
scoped reference reader, not every possible file-reading tool or a manual editor.

## Isolated evidence and limits

On the unchanged base, an authorized single delegated image failed with
`input image reference is unavailable for this Bot`, with zero Images HTTP calls.
The same fixture passes after binding the existing loader.

`TestDelegatedImageReferenceUsesScopedOriginal` covers success, uncertain upstream
response, undeclared reference, revoked grants, missing host scope, removed source
session link, revocation after opening and inherited same-Bot subagent scope.
It asserts exact original input bytes on synthetic JSON Images HTTP, complete
input reads before dispatch, no target alias before returned output is ingested,
no HTTP on denied input, and one POST without automatic retry on uncertainty.
The subagent case reconstructs persisted ancestry; it does not spawn a new Agent
or claim a complete live delegation/model loop. Existing native-spawn regression
coverage is separate.

Commands (Go 1.25.7; local caches in the task workspace):

```sh
go test -json ./internal/imagecapability
go test -json ./cmd/internal/core -run '^Test(ImageCapability|DelegatedImage)' -count=1
go vet ./internal/imagecapability ./cmd/internal/core
go test -race ./cmd/internal/core -run '^TestDelegatedImageReferenceUsesScopedOriginal$' -count=1
```

Validation outcomes and lint/aggregate boundaries are recorded in the adjacent
`validation.json`. No full-repository test pass, CI, real provider call, production
configuration change, UI acceptance, phone/Windows/NAS deployment or resolution
of a particular live incident is claimed.

For the reported incident, the minimal remaining field evidence is whether the
actual delegation tool input declared the original hash, whether the persisted
input scope retained it, and the specialist's actual first reading tool/error
category. Do not infer HTTP403 from a user's description, replay a generation,
read credentials or access a paused user-controlled page to obtain that evidence.
