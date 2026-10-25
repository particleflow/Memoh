# Harmony alignment to deployed shared 1502199

Shared pin: `150219908c316d75272b77c65148436fac3b7074`.
Shared tree: `743218605011997cd5f6f0cdb2eb0d54fbd97dfe`.
Recovery base: `b468eaca54fb3705ce20040a671adc93cf0e0d39` on the existing
`recovery/harmony-source-20261023` branch. This source-only update does not
include any later expert-run projection or guessed candidate.

## Exact source and adapter change

The exact commit was fetched through the existing Git remote. All 5,892 main
archive file objects/modes matched its Git tree. The existing source generator
reproduced the combined archive from verified public cache inputs. UI revision
`f69402a55dd87a59c10722fe11507ad8fd7048f5`, pnpm resolutions and workspace
configuration are unchanged. [Source verification](source-verification.json).

Relative to 5e55, six shared Web paths change: the API client, auth-session
counter, API tests, native task reader, parent task reader, and reconnect tests.
The native task reader now keeps the user's opened-sample choice across a
transient parent-detail failure, clears unavailable output, and resumes reads
only after parent authorization is revalidated. Identity/task/visibility changes
still retire the old sample; permission denials do not create a polling loop.

Upstream now contains the e26 response-ownership guard and session counter.
Their duplicate phone injections are removed. The generated API client remains
byte-identical to the previous adapted client; the counter only changes its
local variable name to the shared name. It occurs once, advances once per
notification, and the response check occurs once. Host patches decrease from
44 to 43. The existing `createApiFetch` export remains because both
`harmony-files.ts` and `harmony-download.ts` use it for native file I/O, as do
phone tests. It is not only a test export. No alternate login mechanism is added.

The four affected shared production modules equal exact upstream, except for
that existing API export. Native configuration/login persistence, attachment
bridges, Bot pages, app metadata and permissions retain their prior bytes.
[Context comparison](context-comparison.json), [module hashes](upstream-module-equivalence.json).
The source lock, inherited-source hashes and notice source reference are updated.
No official Agent implementation or separate service is changed.

## Executed checks

- **103 tests / six files passed**: API/auth-session, native continuation,
  parent reconnect, computer observation and the task component. New REST/SSE
  login-race tests use the real upstream generation helper. Reconnect tests
  cover transient failures, 401/403/404 read suspension, identity/task/close
  changes, aborted late reads and preservation of uncertain request UUIDs.
  [Log](targeted-tests.txt).
- Complete phone Web typecheck and targeted six-file lint: exit 0.
  [Typecheck](typecheck.txt), [lint](lint.txt).
- Existing phone auth-race regression passed with the current generated source.
  [Result](phone-auth-race.txt).
- Existing native download/frontend command-router regression passed with
  production TS/ETS and controlled transport/OS boundaries.
  [Result](native-download-router.txt).
- The fixture preparer now adapts four attachment tests only. A fresh generated
  fixture run matches the validation checkout, and the API test stays exactly
  upstream instead of injecting a constant-generation stub.
- All 1,454 generated Web/SDK non-test files match the reused Vite checkout;
  no duplicate server was started. Source-manifest checks pass: 125 entries,
  134 public files and 19 unidentified historical later changes still unverified.

The existing Vite at `http://127.0.0.1:4181` served the real shared UI with phone
adapters in 390x844 Chromium. Synthetic API interception isolated it from
production. Login-race/reload/current-401 browser checks passed after switching
to the shared counter; one login HTTP call, no page errors.
[Result](auth-result.json), [login retained](auth-login-retained.png),
[current-session 401](auth-current-401-login.png).

The focused parent-detail browser check also passed: a synthetic 503 clears the
sample and pauses content reads; a successful parent refresh automatically
restores the opened sample without a second click. After a single synthetic
unknown-result continuation, another disconnect/reconnect retains the same
UUID and leaves both input and submit disabled. Exactly one POST occurred;
login remains and task/business success is not inferred from the sample.
[Result](parent-reconnect-browser.json), [parent unavailable](parent-unavailable.png),
[parent recovered](parent-recovered.png), [unknown request retained](unknown-after-reconnect.png).
All screenshots were inspected. The first two attempts exposed incorrect
browser-harness assumptions about cleared response text and a disabled input;
only those assertions were corrected, with no product edit. The initial raw
logs are retained and their hashes/reasons are [recorded](browser-harness-corrections.json).

## Package and deployment boundary

No production deployment, Web production build, HAP compilation, public binary
upload, signing or Library operation was performed. The verified unsigned HAP
remains source `f2488597aba37ef8c3557cc3f156b78d870c82c9`, shared 5e55, version
1.0.7 / 1000007, 37,013,036 bytes, SHA-256
`b7eee63f9751ae5d7772299fd22e410ef129c39571269eb5f7bbf9a43d842f0d`.
Its unchanged size/hash was checked; this source synchronization is not in it.
The SDK, existing source archives, package and private configuration are retained.

These checks do not constitute Harmony-device runtime/upgrade, live Bot/Agent
execution, native picker/TLS, Windows receiving/signing, phone/NAS transfer or
human acceptance. The later package must identify its eventual exact source;
this update does not assert that a phone or HAP has already been updated.
