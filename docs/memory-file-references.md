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
    "status": "located",
    "content_hash": "<canonical hash locator>",
    "name": "<canonical stored basename>",
    "mime": "text/plain"
  }]
}
```

The provider calls the shared media `Locate` capability under the **trusted
request Bot**. It uses the existing `PrefixLister` directory metadata only: no
payload read, hash scan, migration, durability repair or memory-side original
copy. A metadata-supplied owner, Bot, filename, MIME, path or status cannot
authorize another Bot or override the projection. The basename and MIME come
from the located storage key, not from memory metadata. The basename is a display
label, not a new alias. Providers without a metadata capability return unavailable;
there is deliberately no fallback to `Open` or the legacy `Stat` path, which can
open content when persistence is not configured.

`located` means an exactly scoped canonical locator was found. It does **not**
prove readability, content integrity, authorization for a future operation, or
committed/durable publication. Even a corrupt file may have a valid locator.
Actual retrieval must still use the authenticated
`GET /bots/{bot_id}/media/{content_hash}` and its unchanged integrity checks. That
read may use `Durable.Open`'s existing legacy copy-on-read migration. This change
creates no memory-side original copy; it does not claim all file reads are free
of storage side effects.

A malformed, missing, inaccessible-directory or unsupported reference becomes only
`{"file_refs":[{"status":"unavailable"}]}`. It carries no untrusted locator,
owner or underlying error. A memory without `metadata.content_hash` has no new
field. Visibility is queried again on every request; duplicate hashes share a
lookup only within one response. The original may become unavailable between
query and fetch.

The memory UI consumes only this validated projection. Located references open
the existing asset preview for the current Bot; unavailable references show a
localized message. Generic management metadata remains backward compatible, but
is not a URL or attachment source for this UI and is not copied into tool output.

PG and the derived Markdown retain the input hash through rebuilds. The actual
bytes stay with the native file module. Content changes produce another hash;
this does not implement a mutable document identity, cross-Bot reauthorization,
permanent forgetting, or changes to Bot-shared memory visibility. Message
`source_refs` retain their separate provenance contract.
