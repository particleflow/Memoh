# Complete model catalog discovery (HC-04)

Baseline: `046023913592b00951d7f28fe54842080c5cdc51`.

The independent audit's `TestAuditPagedCatalogAcceptedAsComplete` demonstrated
that an OpenAI-compatible response advertising `has_more=true` still reached the
complete catalog apply port. Absent models can then be marked unavailable. The
audit did not establish that the deployed provider currently paginates or that a
real model was hidden.

## Narrow correction

The existing provider HTTP injection point now completes a model list before the
unchanged Twilight SDK converts it. Native Anthropic follows `has_more` and
`last_id` using `after_id`; native Google follows `nextPageToken` using
`pageToken`. Empty pages with a continuation are not terminal. All pages must
succeed. Duplicate effective model IDs, repeated/missing cursors, malformed
responses and cancellations return an error, never a successful prefix.

OpenAI's documented `/models` list has no pagination query parameters. Its
compatible endpoints advertising continuation therefore fail closed and retain
the previous catalog: this change does not guess `after`, `after_id`, or an
endpoint-specific cursor contract. Complete single-page lists, including empty
lists and `has_more=false`, retain their existing behavior. Nonempty unknown
`next_cursor` markers are also rejected rather than silently ignored.

The entire traversal uses the existing probe deadline and existing response byte
budget. There is no model allowlist, provider-name/hostname match, page-count
limit, new configuration, credential, migration, API or service. Cursors are
query values on the original origin/path; redirects and cookie sharing are
disabled. The adapter is used only for discovery, not model execution.

Automatic synchronization additionally rejects an SDK list missing a chat ID
present in its validated catalog read. Cancellation is checked before apply.
Existing per-provider in-flight deduplication, failure cooldown, transactional
provider configuration CAS, explicit user settings and stored catalog remain in
place. The database apply implementation is unchanged. This is not a guarantee
of a remote snapshot revision when the upstream supplies no snapshot token.

## Protocol sources

- [Anthropic model listing](https://platform.claude.com/docs/en/api/models/list):
  `after_id`, `has_more`, `last_id`.
- [Google model listing](https://ai.google.dev/api/models#method:-models.list):
  `pageToken`, `nextPageToken`; omitted continuation is terminal.
- [OpenAI model listing](https://developers.openai.com/api/reference/resources/models/methods/list):
  complete `data` envelope, no documented cursor query parameters.
- Repository-pinned Twilight version:
  `v0.7.2-0.20261002230120-f6d0ce1a0e8c`. Its Anthropic and Google list methods
  decode one response only; no SDK source or dependency version was changed.

## Reproduction and verification

`TestCatalogSDKReadsDocumentedPages` fails on the baseline with only `first` and
one request for both native protocols. The three incomplete-sync cases fail on
the baseline with one complete apply and three requests. The initial attempt
without local socket permission failed before exercising the product; the
network-enabled rerun establishes the actual red result.

The corrected scenarios cover complete multi-page discovery, an intermediate
empty page, later-page HTTP and decode failure, duplicate IDs, missing/repeated
cursors, cancellation, a shared deadline/aggregate byte limit, 131 pages,
URL-shaped opaque cursor isolation, mismatching consecutive catalog reads,
failure cooldown, concurrent refresh and retention of existing stored records.
The HTTP boundary also checks incomplete discovery is a failure, not HTTP 200
with an empty or partial list. Existing account-only, anonymous, revoked and
foreign-team regressions remain required.

Commands (use the configured Go toolchain):

```sh
go test -race ./internal/providers -count=1 -json
go test -race ./internal/handlers -run '^TestProvider(MediaCatalog|Account)' -count=1 -json
golangci-lint run --new-from-rev=046023913592b00951d7f28fe54842080c5cdc51 ./internal/providers ./internal/handlers
bash scripts/check-agent-core-boundary.sh
git diff --check
```

The frozen-source provider package run passed 55 top-level tests (120 passing
test events including subtests) under the race detector. The existing HTTP
account/catalog subset passed 6 top-level tests (199 passing test events,
including route-matrix subtests). After preserving the original media-client
timeout, the affected catalog/media HTTP subset passed again. These counts are
not additive unique business acceptance cases.

The frozen-source differential golangci-lint 2.10.1 run, Agent core boundary
guard and `git diff --check` also exited 0. Earlier lint failures were retained;
the final run was performed with all six changed Go source/test hashes frozen.

This work is isolated synthetic verification. It does not call a real provider,
import production models, access keys, change a user's provider, or deploy. Full
repository checks and live PostgreSQL/UI acceptance are not implied. The current
environment rejected the Docker socket read with `operation not permitted`;
the documented development stack and current real UI/screenshots could not be
verified. That action was stopped without changing socket permissions or using
another endpoint. The independent reviewer and integration owner must retain
this outstanding verification boundary.

## Independent-review follow-up: Anthropic terminal proof

Parent `dd10c889631405f7e6a94c937f3efc8614df054b` still treated an absent
Anthropic `has_more` as false. The independent v3 audit counterexample was
reproduced: page two without that field returned two models and no error.
Native Anthropic now requires the boolean on every page. Missing, null and
wrong-type flags return `ErrCatalogIncomplete` with no successful prefix;
explicit false remains terminal. Google's omitted continuation and ordinary
OpenAI single-page semantics are unchanged. Only this terminal-proof boundary
and its first/second-page variants are rerun; the previous 55-test package and
other independent findings are not rerun or requalified by this follow-up.
