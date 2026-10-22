# Memory file references

Native memory can retain a reference to an existing Bot attachment without copying
its contents. Set `metadata.content_hash` to the lowercase SHA-256 returned by
`POST /bots/{bot_id}/media`. An explicit memory add can use `infer: false`:

```json
{
  "message": "The synthetic project notes are in the attached file.",
  "metadata": { "content_hash": "<64 lowercase hex characters>" },
  "infer": false
}
```

This PR does not automatically extract attachment references from conversations.
The input remains generic metadata for compatibility; only `content_hash` is
interpreted as a file reference. Arbitrary `file_id`, paths, URLs and nested
`metadata.file_refs` are not alternative locators.

Search and list responses add a **read-time** `file_refs` projection. The
`search_memory` tool includes that same projection without exposing arbitrary
metadata:

```json
{
  "file_refs": [{
    "status": "available",
    "content_hash": "<verified canonical hash>",
    "name": "<canonical stored basename>",
    "mime": "text/plain"
  }]
}
```

The provider opens the existing media asset under the **trusted request Bot** and
closes it without copying its content into memory. It checks the returned asset's
Bot and content identity. A metadata-supplied owner, Bot, filename, MIME, URL,
path or status cannot authorize another Bot or override the projection. The
basename and MIME come from the existing media service, not from memory metadata.
The basename is a display label for the stored content, not a new alias.

A malformed, missing, unreadable or unconfigured reference becomes only
`{"file_refs":[{"status":"unavailable"}]}`. It carries no untrusted locator,
owner or underlying error. A memory without `metadata.content_hash` has no new
field. Availability is never persisted and is checked again on every query;
duplicate hashes share a check only within one response. A successful check is
not an authorization grant. Fetching the file still uses the existing authenticated
`GET /bots/{bot_id}/media/{content_hash}`, including its current access and
integrity checks. The original may become unavailable between query and fetch.

The memory UI consumes only this validated projection. Available references open
the existing asset preview for the current Bot; unavailable references show a
localized message. Generic management metadata remains backward compatible, but
is not a URL or attachment source for this UI and is not copied into tool output.

PG and the derived Markdown retain the input hash through rebuilds. The actual
bytes stay with the native file module. Content changes produce another hash;
this does not implement a mutable document identity, cross-Bot reauthorization,
permanent forgetting, or changes to Bot-shared memory visibility. Message
`source_refs` retain their separate provenance contract.
