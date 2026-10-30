# Automatic diagnostics113bf review corrections

**Subsequent independent review:** the P1/P2 controls below passed on2d35, but unknown A blocked an unattempted batch B. That candidate was not admitted. See [the source-only queue correction](../harmony-auto-diagnostics-queue-fix-20261030/README.md); new readmission is still pending. Original logs below remain historical evidence.

Source-only corrections to113bf54c4a66020a4d662d40fdbbdfe11291a7e4. The prior automatic-diagnostics candidate was NOT admitted after independent review found P1 and P2. Its original evidence remains unchanged. This correction awaits the same independent review; it is not packaged, deployed or tested on a phone.

## P1: diagnostic failures must not change original reads

The controlled113bf case throws only from randomUUID: originalBlob rejects before its media GET, even though the source is a valid7-byte payload. A separate event-sink throw also replaces the business result. Both failed before the fix.

originalBlob now isolates diagnostic creation and every synchronous observation call. A broken observer is disabled for that read; only one fixed INIT/EVENT failure code is written to the local console. The Error, body, filename, URL and credentials are never passed to the logger. Logging failure is itself nonfatal. The original request, body length, SHA256 and business error/cancellation path remain authoritative. This console evidence is local, not a new persistent or remote diagnostic channel.

## P2: unknown write is not cleared by404

The exact controlled counterexample is reproduced: an SDK diagnostic POST stores bytes and loses its response; recovery GET404 permits a second POST in113bf. The counter records2 diagnostic write requests and0 ACKs. It does not establish a business-file replay, a real NAS write, or two completed server operations.

The existing diagnostic PUT row now carries one optional local-envelope boolean, writeAttempted. Before the first SDK POST, the existing native PUT command durably claims that attempt. Its boolean reply is atomic: concurrent claimants cannot both POST. No new native command, remote API, service, credential, file location, budget, permission or SDK operation is added. Canonical uploaded body/whitelist/content hash are unchanged; this phase is never included in the uploaded diagnostic body.

A retained attempt followed by404 stays unconfirmed: no second POST, no ACK and no reset/recreation of the object. Existing ready/online/foreground recovery reads that same object again; an exact200 body/hash/size can ACK it. A torn/failed local marker prevents POST; a newline-committed marker after an uncertain fsync remains sticky across restart. Retrying an old PUT cannot clear it. This deliberately favors nonduplication: a crash after reserving an attempt but before actual network dispatch can also leave it pending;404 alone cannot safely authorize another attempt. Retention remains the original current/previous journal bounds, not indefinite storage.

The changed JS and native boolean reply must be combined in a future matched HAP. Do not hot-swap only the JS into an older shell. Published32 does not contain this diagnostic feature and is unchanged.

## Narrow verification

- Original113bf plus4 new review cases:4 failed; raw red log preserved.
- Final changed-code cases only:6 passed /9 existing cases deliberately not rerun. Includes7-byte successful media GET/hash despite init/event failure, preservation of a real hash failure, unknown-write404 with POST held at1/ACK0, later same-object verification to ACK1, failed durable marker with zero POST, concurrent first-attempt claim with one POST.
- One intermediate concurrency test initially counted2 ACK calls because its fake journal did not model the existing native ACK idempotence. The adapter now counts only an actual pending-to-ACK transition, matching native ack(); its initial failed log is retained. The POST assertion was not weakened.
- Real file-backed owned journal tests cover torn marker, restart, unknown fsync, sticky phase, other-scope refusal and concurrent native claim; actual NativeClient also validates the fifth optional envelope field and boolean reply under its existing frame/ready guards. Kit and transfer-owner dependency boundaries are adapted; this is not phone storage evidence.
- Existing SDK26.0.0.105/API26 default@CompileArkTS passes in12.916s; no PackageHap task. Current generated-context types and scoped lint pass. Four composed SDK/host files match tested bytes,193-source integrity passes. Official generated SDK, dispatch identity lease, bridge permission string, existing transfer engine and app permission declarations are unchanged.

No new HAP, signing, NAS/Windows action, model call, real HTTP bearer or phone capture occurred. Preserved unsigned32 SHA256 f16df3de1d59e6c4008fad622d971ec7fdb1c84505c4bb9532e33f471f41845a and handoff ZIP SHA256 a8d97a3814eda305d23445426f28f5abad911db1d1f802425b95757958553156 remain unchanged. The public download page still has no identity bridge; HTTPS latency/root cause and actual phone timing remain unverified.
