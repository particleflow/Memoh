# Images SDK through the existing Bot image capability

The platform can explicitly select the Images protocol for an existing provider.
The Bot still selects its model through the native `image_model_id` setting.
`internal/imagecapability` implements the existing ToolProvider port; the
composition root replaces the original image tool in its existing slot. The
Agent runtime, SDK core, chat model and provider discovery are unchanged.

## Managed configuration

The optional object below belongs to the existing provider `config` map, with
its existing persistence and management authorization. It is an addition to the
original Memoh baseline, not an upstream configuration capability.

```json
{
  "images_api": {
    "protocol": "openai-images",
    "edits": true
  }
}
```

`protocol` explicitly selects the OpenAI-compatible Images SDK. `edits` defaults
to false and enables reference-image input only when the provider supports that
operation. No separate service, model catalog, image editor or configuration
store is introduced. There is no new UI control in this change.

The provider's existing managed `base_url` and credentials supply the endpoint
and authentication. The URL includes its API prefix, for example
`https://provider.example.invalid/v1`. The selected enabled model must have
`image-output` compatibility and belong to an enabled API-key OpenAI-compatible
provider. Its configured model ID is always used. No vendor, model, private
address, filesystem path or device is selected by a literal in product code.
Tool arguments cannot override endpoint, credentials, model, identity or protocol.

With no `images_api`, the original tool schema and behavior are retained; there
is only one `generate_image`. Until explicit selection is known, legacy discovery
still exposes a configured image ID even if model/provider lookup fails. Those
lookup errors remain execution errors. Known invalid explicit configuration never
falls back to a potentially billable chat request. Legacy tools recheck selection
at execution so a recovered lookup cannot accidentally enter the old route.

### Existing configuration compatibility

The pinned Twilight SDK serializes a model field. It cannot express the earlier
adapter's omitted-model policy without rewriting the SDK's request. This adapter
therefore does not silently turn omission into an empty or explicit model:

- Existing `model_mode: configured` generation configuration remains accepted.
- Existing `model_mode: configured`, `edits: true`,
  `edit_encoding: json-images` configuration remains accepted with identical
  JSON request semantics.
- `model_mode: omit`, explicit `edit_encoding: multipart`, and the old implicit
  multipart combination (`model_mode: configured`, `edits: true`, no encoding)
  fail during configuration resolution before any image HTTP request. Their
  persisted configuration is left intact. An administrator must verify the
  native image-model binding and JSON support before removing the obsolete
  policy fields. There is no automatic rewrite, fallback or regeneration.
- Invalid or null legacy policy values also fail closed.

New configurations need only the protocol and optional reference-input switch;
there is no extra model-selection workflow. `service_default` is not a wire field.
Supporting omitted models again would require an explicit SDK capability decision,
not a hidden transport rewrite.

## SDK and attachment contracts

The already pinned Twilight commit is
`f6d0ce1a0e8cfa0fca105b11ba90c705f7dd2396`. Its OpenAI Images provider owns request
encoding and response parsing via `sdk.GenerateImage` and `sdk.EditImage`.
`generate_image` accepts `prompt` and optional `size`. It requests one image with
`response_format: b64_json` and the configured model. When enabled, `edit_image`
accepts the same text/size plus one to four existing `images` content hashes.
The host supplies authorized original bytes as data URIs through `sdk.ImageInput.URL`;
the SDK sends JSON `images[].image_url` to `/images/edits`. No mask or alternative
encoding strategy is exposed. This is reference-image input, not a manual editor.

Current Bot chat permission, persisted session ownership and delegation authority
are checked before effects and before each `media.Open`. A hash alone does not
grant access. Original bytes remain in the Bot's existing media scope and their
hash is checked before transmission. Foreign/missing references, disabled edits
and unsupported tool fields are rejected before dispatch. There is no new
cross-Bot alias, task-specific artifact ledger, raw path or remote-image fetch.

The adapter accepts a single base64 byte result, checks size and MIME magic, and
passes unchanged bytes to the existing `media.Ingest`. It does not fully decode
the image format or establish structural integrity. Existing storage, content
hashes and attachment events are reused. The pre-existing 50 MiB input/output
and combined-input guards remain resource bounds for an individual request,
not a task-count quota. PNG, JPEG, WebP and GIF are recognized without transcoding.

A persisted result carries `state: completed`, `content_hash`, `media_type`,
`size_bytes` and the existing `tool_call_id`. Only the established live-conversation
shortcut emits an attachment; internal subagents report references and the
supervisor can call the ordinary `send` tool. Gateway model labels are ignored
and do not prove the actual underlying model/version.

## HTTP and uncertainty boundaries

The SDK uses the existing provider HTTP client and timeout. A small transport
boundary preserves the previous safety behavior: one request, no redirects,
no replay body, strict HTTP 200 and bounded response reads (including unknown
content lengths). It contains no request/response JSON or multipart encoder.
Non-200 bodies are not read; private provider responses never enter tool errors
or adapter logs. No retry loop or upstream idempotency claim is added.

Timeout, disconnect, non-200, malformed/oversized output or URL-only output
returns `state: unknown`, `retryable: false`, without a completed hash. The
upstream may already have generated/billed an image. Authorization or persistence
failure after returned bytes yields `state: generated`,
`storage_state: unconfirmed`. These are normal structured receipts to avoid
error-triggered retries, not successful delivery confirmations. Never regenerate
automatically to recover file delivery.

ToolCallID provides correlation only. There is no durable cross-call or
cross-process deduplication, raw-response ledger, queue or image job service.
Completed hashes can be reused through existing authorized delivery. A missing
receipt does not prove no upstream work happened; unknown outcomes require
operator/upstream verification before deciding on a new request.

## Verification scope

Synthetic HTTP tests assert the SDK's exact configured-model JSON for generation
and one/multiple reference images, original-byte/hash persistence, rejected mask
and policy overrides, unchanged legacy schema/behavior, disabled editing,
revocation, foreign/missing references, HTTP limits/timeouts and one-POST unknown
outcomes. Configuration regressions cover retained and rejected legacy policies.

Native-loop tests use the unmodified runtime and SDK chat transport. The separate
composition-root test exercises actual `spawn_agent` admission, child session,
production DI authorizer, child image tool, hash report and supervisor `send`.
It covers generation and reference input plus denial/unknown cases. Its query
ports and storage ledger are synthetic; it is not a PostgreSQL durability claim.

These tests do not establish real Magpie/paid-model compatibility, NAS/PC/mobile
acceptance, production configuration or deployment. Runtime/UI and CI evidence
for each fixed revision belong in its validation record; earlier results must
not be relabeled as checks of this SDK increment.
