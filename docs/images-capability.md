# Explicit Images capability through platform tools

The platform can opt an existing image-model provider into an explicit Images
REST contract without changing the native Agent, its chat model, or its runtime.
The implementation is `internal/imagecapability`; the composition root wraps the
existing image tool registration through the existing `ToolProvider` interface.

## Configuration added by this change

The following **new** optional object uses the existing provider `config` map:

```json
{
  "images_api": {
    "protocol": "openai-images",
    "model_mode": "omit",
    "edits": false
  }
}
```

This is not a configuration supported by the previous fixed baseline. There is
no new UI control in this change. Existing provider-management authorization and
configuration persistence still apply. `base_url` and authentication come from
that provider's managed configuration, never tool arguments. The base URL must
include its API prefix, for example the public-format placeholder
`https://provider.example.invalid/v1`; the adapter appends `/images/generations`
or `/images/edits`. It does not inspect vendor/model names or guess endpoint
capabilities from a hostname.

- `protocol` must be `openai-images`.
- `model_mode` must explicitly be `configured` or `omit`. `configured` sends the
  selected image model's provider model ID. `omit` sends no model member/field and
  delegates model selection to the provider default. The returned receipt does
  not prove which underlying model the provider selected.
- `edits` defaults to false. Enable it only when the provider's editing protocol
  is supported independently of generation. An advertised edit tool is checked
  again at execution, so disabling edits rejects a stale call before HTTP.

`image_model_id`, enabled model/provider checks and image-output compatibility
remain required, including when the wire omits model. Only API-key
OpenAI-compatible client types are accepted; this adapter does not introduce an
OAuth flow. Invalid explicit configuration does not fall back to chat. With no
`images_api`, the original tool/schema/behavior is used unchanged. Registration
is a wrapper at the existing slot, not a second same-named tool.

Before explicit selection can be established, discovery retains the legacy
contract: a configured image ID exposes the legacy tool even when model/provider
lookup fails, and settings lookup failure keeps the legacy empty tool list.
Lookup failures are reported at execution. Legacy invocations recheck protocol
selection before execution; a recovered or changed explicit selection requires
fresh tool discovery instead of entering the legacy route. Known invalid
explicit configuration is rejected without legacy fallback.

## Tool and attachment contracts

`generate_image` accepts `prompt` and optional `size`. It makes one JSON POST
with `prompt`, `n: 1`, `response_format: b64_json`, optional `size`, and model only
under the configured policy.

When enabled, `edit_image` accepts `prompt`, one to four `images` content hashes,
an optional `mask` content hash and optional `size`. It makes a multipart POST:
`image` for one input, repeated `image[]` for multiple inputs, optional `mask`,
and the same prompt/n/response-format/model-policy fields. The implementation
supports this multipart contract. It does not implement a JSON `images` array
edit dialect or establish compatibility with another application's editing
contract. No real provider editing request has been validated by these tests.

Both tools reject unknown fields, including endpoint, key, model, Bot and session
overrides. Host admission supplies the Bot/user/session identity. The platform
rechecks current Bot chat permission, the persisted session-to-Bot binding and
delegation authority before effects, including before `media.Open`. A known hash
is not an authorization grant. Reads stay in that Bot's existing media scope and
are hash checked before transmission. This change adds no cross-Bot hash alias;
existing delegation-scoped attachment authorization remains a separate mechanism.

The adapter requests base64 bytes and never follows an output URL. It base64
decodes once, checks size and MIME magic bytes, and passes original bytes to
`media.Ingest`. It does not fully decode the image format or validate structural
integrity; MIME recognition alone does not establish a complete valid image.
Existing configured storage, content-addressed naming and attachment events are
reused. No additional file repository, SQL registration table or workspace path
is introduced. Input and output byte limits are 50 MiB; combined edit inputs
including a mask are also bounded by that limit. PNG, JPEG, WebP and GIF content
types are recognized; bytes are not transcoded.

A confirmed persisted result includes `state: completed`, `content_hash`,
`media_type`, `size_bytes`, and the existing `tool_call_id`. Only the established
live-conversation shortcut emits an attachment: it requires the existing
platform/reply-target/session rules. Internal subagents receive a file reference
without automatically sending to the parent conversation.

## Uncertainty and correlation

HTTP redirects, automatic retries and chat fallback are disabled for an explicit
Images invocation. The request has no replay body or asserted upstream
idempotency key. A timeout, disconnect, non-200 response, invalid/oversized result
or unsupported URL-only response returns `state: unknown`, `retryable: false`,
and no ready content hash. The upstream may already have generated/billed an
image. Persistence failure returns `state: generated` and
`storage_state: unconfirmed`, also without a ready reference. These are normal
structured tool receipts so the model can see uncertainty; they are **not
successful generation/delivery confirmations**. They instruct the caller not
to regenerate automatically. Provider error bodies, private URLs, keys and raw
image bytes are not included in failure receipts or adapter logs.

The native tool result/step flow already carries ToolCallID. Returning that same
ID supports association with the existing call record; it does not establish
provider receipt, actual underlying model identity or durable deduplication.
There is no new job/queue/ledger. The guarantee is **no automatic retry within
one invocation**. A new tool call or process restart is not deduplicated by this
adapter. Resolve unknown outcomes through operator/upstream verification before
deciding whether a new request is appropriate.

## Verification scope

Tests use synthetic provider HTTP endpoints and synthetic PNGs in temporary
roots of the existing media storage. Both supervisor and internal-subagent
RunConfig modes execute through unmodified `native.Agent.Generate`, real tool
assembly, SDK chat transport and tool execution. Fixtures explicitly declare
the synthetic model's existing SupportsToolCall capability; live attachment
fixtures supply ReplyTarget. No shortcut, runtime or authorization code is
changed to accommodate tests.

The suite covers omitted/configured model fields, generation and independent
multipart editing, original-byte/hash round trips, tool visibility, legacy
fallback/schema uniqueness, managed endpoint/auth selection, disabled editing,
foreign/malformed references, revoked authority before storage, private error
suppression, unknown response/timeout handling and persistence failure.

This is synthetic contract/integration evidence, not real paid-provider,
production chat, child-spawn admission, Web UI, NAS, PC or deployment acceptance.
No live provider configuration is written. The task explicitly excludes service
trials and broad UI/install/migration changes.
