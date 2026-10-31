# Independent conversation-presence admission review — 0b731eb9

2026-10-30. **Not admitted: P1 effective-model qualification and P2 direct-input
classification are reproducible.** Product source was not modified or merged.
No application build, CI dispatch, provider/credential request, paid generation,
field browser, migration or deployment was performed. The current navigation
release is independent.

Candidate: `0b731eb9e72de5abf6c6cf28b87a608360efedf3`, tree
`aa7020d1f14dea4155929ea8c9aeec5b6a1d5116`, parent
`1fdfcb2e1670d00f0b0fb8d7237a7a101d307cfb`, basecefdf0.
[PUBLICATION.json](https://github.com/particleflow/Memoh/blob/0b731eb9e72de5abf6c6cf28b87a608360efedf3/docs/testing/conversation-presence-20261030/PUBLICATION.json)
provides the original/public history mapping. Source1c1951f43746f5818c3bd1cd7091d87fb672d45b
has the reported18 race/17 regression/3 reasoning evidence;1fdfcb2e adds the two
revocation/formal-order corrections.0b731 has no product-code change from1fdf.
The reviewer read and reused those records without rerunning their suites or
claiming the redacted original local artifacts were independently obtained.

## P1 — upstream-managed route is accepted without an effective selected member

Candidate `conversation_presence_runtime.go:30–44` accepts the supported client
and `thinking_mode=none` as verified capability.
`conversation_presence_sdk.go:34–49` compares the model/route labels but does
not establish the effective selected member before returning a usable generator.

The pinned existing SDK's GitHub Copilot `AutoModel` is a concrete counterexample
to that inference. The SDK documents it as provider-managed selection and
explicitly omits `model` from the request when this routing mode is selected
(`provider/github/copilot/catalog.go:3–6`, `copilot.go:180–182` in the dependency
`github.com/felinics/twilight v0.7.2-0.20261002230120-f6d0ce1a0e8c`). Two requests
with that same alias have not been pinned to the same backing member. Merely
copying the label/config or setting a constant capability/route version cannot
establish the user's same-effective-model/off-or-low contract. This does not
assert that production Magpie uses that routing mode or that its served model
has changed; that field evidence remains unknown.

The [counterexample](effective-model-counterexample_test.go.txt) constructs an
enabled model and provider, frozen matching model/provider identifiers and an
explicit non-reasoning declaration. It uses the SDK's actual AutoModel constant,
not a production-name heuristic. The pinned-model control passes. The managed
route should remain unusable without a resolved member, but returns
`verified=true`, nonnil generator and `Usable=true`, reason
`already_non_reasoning`. No Generate, HTTP or credential method is invoked.
[Actual output](effective-model-counterexample.log.txt):1 pass/1 fail, exit1.
An earlier fixture left enable flags at struct defaults; it is retained locally
but is not the accepted final counterexample.

Original owner should ground qualification in the original provider/main route's
actual pinning/capability contract. If an effective member cannot be established,
fail closed for the optional auxiliary generation while preserving main. Do not
introduce a business-ID/name blacklist, select a different model, alter main
preferences or invent a successful served-model receipt. Keep same-model
construction and advertised off/low resolution for routes that are established.

## P2 — trusted non-user history prevents the first real direct input from being cold

`db/postgres/queries/conversation_presence.sql:2–4` counts every historical
`role=user` row as `previous_inputs`, regardless of trusted `input_source`.
Its separate `last_direct_input` projection does require `kind=user`.
`conversation_presence_postgres.go:126` then sets
`FirstDirectInput = previous_inputs==0 && !hasPrior`. With no earlier presence
receipt and only known schedule/result history, the first real direct user
input gets `FirstDirectInput=false`, zero last-direct time and no pending/run
continuation. The actual classifier returns `uncertain/continuity_unresolved`,
so no cold acknowledgement can be generated. This contradicts the module's
stated rule that machine-origin inputs do not suppress the direct user's cold
entry. It is not permission denial, history loss or a model failure.

The SQL was copied verbatim from the candidate, substituting only the three
parameters with synthetic fixture UUIDs. Four session-local temporary tables
shadow all queried relations; a pg_namespace guard asserts that every relation
is temporary. Only the existing tenant-scope function is used. No permanent
row/table/configuration is read or written. Explicit ROLLBACK or connection
closure removes all temporary objects and data; no background service was
started. This test is SQL/classification evidence, **not a real RLS/ACL test**.

- [Empty control](empty-control.sql): `previous_inputs=0`, last_directNULL;
  exit0/ROLLBACK.
- [Known schedule](known-schedule.sql) and [known result](known-task-result.sql):
  matching receiving Bot/session, nonempty TaskID and declared machine kind
  satisfy the existing Source.Scoped contract. Each actual query returns
  previous_inputs1/last_directNULL and fails the expected0 assertion, exit1.
- [Actual classifier check](classification-counterexample_test.go.txt): empty
  control passes; the two SQL-derived cases produce uncertain instead of cold,
  exit1. [Output](classifier-counterexample.log.txt).

Initial kind-only SQL projections were tightened to full valid scoped task
metadata before accepting the two final SQL counterexamples; initial records
are preserved locally, not misrepresented as full-provenance samples.

Original owner should distinguish known trusted machine origins from genuine
direct inputs in the facts query, using the existing provenance contract.
Retain a conservative policy for unknown/malformed legacy metadata; do not
rewrite old rows or make all uncertain history cold to pass this case. Keep
permission, actor/scope and pending/in-flight guards. Recheck only this new
projection/classifier seam and the nearest unknown/direct-origin control.

## Exact narrow execution / portable handoff

Copy the two `.go.txt` files into their named candidate packages:
`internal/conversationpresence/independent_review_test.go` and
`internal/agent/application/conversation_presence_independent_review_test.go`.
Use the repository's already prepared Go1.25.7 toolchain and existing caches.
The task used:

```sh
GOCACHE=/workspace/.memoh-cloud/cache/go-build GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /workspace/.memoh-cloud/go/bin/go test ./internal/conversationpresence -run '^TestIndependentFirstDirectAfterTrustedMachineHistory$' -count=1 -v
GOCACHE=/workspace/.memoh-cloud/cache/go-build GOMODCACHE=/workspace/.memoh-cloud/cache/go-mod GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off /workspace/.memoh-cloud/go/bin/go test ./internal/agent/application -run '^TestIndependentPresenceRequiresEffectiveModelIdentity$' -count=1 -v
```

Only the new named tests ran. No dependency download or system-Go replacement.
The SQL files are standalone BEGIN/temp-fixture/assertion/ROLLBACK scripts for
an existing authorized isolated development PostgreSQL environment with the
repository's tenant helper; use that environment's supported SQL client and
private configuration, not an inferred cross-environment path or production
connection. This task reused its existing read-config/loopback-only pgx helper,
not a new executable build. No credentials are in these files.

## Remaining reviewed boundaries and combination

- Main SDK configuration is captured after the original selection/credential
  resolution. Auxiliary config is a value copy with independent reasoning
  configuration and copied pointers; main preference stays unchanged. Source
  low/off mapping uses declared capabilities and the existing request adapters.
  The reported wire tests are reused; actual gateway served identity, effort,
  concurrency and cost remain unverified. P1 above narrows the missing contract.
- Auxiliary work waits for main transport WroteRequest, uses its own bounded
  context/capacity, and never makes main await its DB result. The one output
  owner drains already-ready formal events before sending feedback; formal/
  terminal events cancel and clear local feedback. Newer fence/generation/
  ownership/abort and permission checks protect claim/publication; revoked
  cleanup no longer requires the withdrawn grant. Existing author race/PG
  evidence is reused, not claimed as new full-client or production acceptance.
- Claims are durable and unique per episode; restart/uncertain claim is not
  retried into another generation. Failed/canceled output is not synthesized
  again. The native test's final custom RoundTripper rejects nonlocal hosts,
  and both main/nonstream clients use it; the retained earlier default-address
  failure is not a successful field inference. This review invokes no generator.
- Progress is a typed `conversation_ack` projected into the existing transient
  status surface, not text/another assistant turn. Formal persistence uses main
  terminal SDK messages. Status is not scanned into speech or model history;
  **audio acknowledgement integration is not implemented or inferred**.
- Migration0182 adds the receipt table, team-filtered queries, forced RLS and a
  unique claimed-episode index. Its down deletes receipts and is not an
  operational rollback. Preserve182/table/claims/latest business data; an
  existing182-compatible binary with presence disabled is the conservative
  forward recovery. Do not force schema181, delete claims or restore an old dump
  to make a rollback pass. Old181 startup/consumer compatibility is not proved.
- Presence is enabled by default unless explicitly disabled; a future qualified
  deployment can introduce an auxiliary request on cold inputs. This review
  made none and does not establish latency/cost or production capability.
- No product conflict with the current1fa navigation delta was found. A
  nonmutating merge-tree preflight reports a documentation conflict in
  `compatibility-incident-register.md`: the candidate's compiler entry also
  uses C13, already used by the current mounted-router incident; C14 is the
  pnpm incident. Preserve all records and assign a distinct stable ID during
  eventual combination. Do not overwrite the current entries.
- Main browser WS→actual application UI integration is still not demonstrated
  by the author's separate controlled-transcript screenshots. Human QA, real
  provider and ArkWeb acceptance remain open. Fix the confirmed source issues
  before doing a new necessary qualification batch; no unchanged full build.

Follow-up received only, not reviewed or combined here: queue POST identity
`c878a31e268119939330d9eed52050539636b306`, evidence
`29685958ec896489371a894b6854ee8b2e9df9c5`. No queue persistence claim is made.
