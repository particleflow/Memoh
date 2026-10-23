# Prefixed media download and RawQuery verification

This test-only follow-up starts at PR #71's fixed head
`91149ad7ab5c0d24a57f10e7d9ff1df4b71cf51c`. That checkpoint, its shared API-base
contract and its three original logs remain unchanged. No production client,
handler, route, configuration, migration or Agent implementation changes here.

## Actual request coverage

The existing `api-base-prefix` fixture already uploads and associates a synthetic
25-byte file through the actual Server, stable-device registry verifier,
full-migration PostgreSQL/NOBYPASSRLS role and filesystem Media provider. The new
subtest reuses that same HTTPS listener and original file. `/api` is stripped by
the existing `http.StripPrefix` mount; authentication and file responses still
come from the real production routes, not mock responses.

The production `transferclient.Download` implementation now exercises:

1. Full GET through `/api/internal/computer-transfers/.../media/<hash>`.
2. Truncation of the downloaded private staging copy to seven bytes, fsync and
   close/reopen, then Range `bytes=7-` with the original `If-Range` ETag.
3. Full SHA-256 verification and exact byte comparison after the Range response.
4. A completed-offset call that sends HEAD and rechecks authorization, ETag,
   size and the local full hash.

All reads keep the same download delivery, operation, attempt and device context.
The original upload receipt/committed reference is retained by the parent fixture;
an additional real Resolve confirms it remains committed after these downloads.
The original file's bytes and content hash are checked again. There is no local
publication/rename or download delivery Commit, and no fake Linux publication
binding. The checkpoint callback records offsets in test memory; this is a
deliberate Range setup, **not** a durable queue, dropped response, process-kill,
restart or physical power-loss test.

## Query behavior, without extending the protocol

The production download library has no query argument. Its configured API base
still rejects queries. The media route selects content by the path hash and uses
Range/If-Range headers; it defines no query-based download selector.

Three explicit HTTP probes use the existing wire context and the same synthetic
device credential against the real verifier/handler. The observation wrapper
records method, stripped path, RawQuery, Range and If-Range immediately before
Server; it stores no authorization header or body and does not change responses.

- HEAD and Range GET carry a fixed non-secret query containing duplicate keys,
  mixed-case percent escapes, `+` versus `%20`, an empty value and a bare flag.
  Exact RawQuery bytes, ordering and escaping arrive unchanged. These unknown
  query keys are currently ignored by the real route; they are **not new API
  parameters**. HEAD returns 200 with no body; GET returns 206 with the correct
  Content-Range, content length, ETag, bytes and `Cache-Control: no-store`.
- A nonempty `token` query is rejected with 401 even with a valid credential in
  the header. The response retains `no-store`, has no original-file ETag and does
  not return original bytes. This proves the query was not discarded before the
  existing authorization guard. It does not put a real credential in any URL.

The raw probes are separate from the production-library calls. No public method,
query credential, endpoint alias or configuration field is added for them.

## Recorded result

Linux cloud workspace, 2026-10-23 UTC:

- Selected PG/race test: PASS, package **6.174 s**, prefix fixture 0.96 s,
  new download subtest 0.27 s; **zero SKIP**.
- Six named PASS lines include parent tests and the three query probes; they are
  not six independent transfers.
- Incremental integration-tag lint: exit 0 after documenting the one test-owned
  HTTPS request's fixed source at its call site. No broad lint exclusion.
- `git diff --check` and gofmt checks passed.

[The original 18-line output](evidence/file-api-base-download/postgres-prefix-download.txt)
is copied verbatim after complete inspection and secret/private-address marker
checks, with no redaction or reconstruction. SHA-256:
`81081f09565956ee1a8a09f69730f3acad80348b5f36355b955c19fc8fdc5e91`.
It is a readable local execution record, not a remote CI claim. Only this
extended focused fixture ran; PR #69's round-trip and PR #71's prior vector/full
package checks were not rerun. The final source differs from the tested source
only by a lint explanation comment and these evidence documents.

```sh
# Use TEST_POSTGRES_DSN for an existing disposable local PostgreSQL admin.
# The fixture creates/removes its own database and roles; never use production.
go test -p 1 -v -race -tags=integration -count=1 -timeout 90s \
  -run '^TestComputerDeliveryPostgresAuthorizationCASAndPreservation$/^api-base-prefix$' ./internal/db
golangci-lint run --build-tags=integration \
  --new-from-rev=91149ad7ab5c0d24a57f10e7d9ff1df4b71cf51c ./internal/db
```

TLS remains verified using only the test process's HTTPS fixture transport; no
OS trust, credential provisioning or production data is changed. Initial free
space was about 2.2 GiB in `/workspace` and 4.8 GiB in `/tmp`; the transferred
fixture is 25 bytes, not a throughput or large-file test.

There is no new Windows/native/mobile/UI acceptance, real Nginx run, LAN/IPv6
selector or old HostBinding migration. The separately recorded Windows Nginx
probe is reused with its own HTTP/synthetic-upstream boundary. No Memoh runtime
installation, real PC/NAS request, previously denied artifact retrieval or
legacy Media fix occurred. Human QA and screenshots remain absent. Actions were
previously rejected as disabled (HTTP 422); this follow-up performs no dispatch
retry or workflow enablement and does not claim remote CI passed.
