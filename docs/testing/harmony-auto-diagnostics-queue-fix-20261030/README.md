# Scoped diagnostic queue continuation after2d35 review

This source-only patch addresses the one newly rejected queue-recovery behavior on2d35b6edd87038458747cbb8ca73152913675a7e. Independent review accepted the prior P1 business-error isolation and P2 no-second-POST controls; those controls and original evidence are retained. This new candidate still requires independent readmission. No HAP/version/signing/deployment or phone result is produced here.

## Exact failure and minimal correction

A is a retained attempted diagnostic object whose GET remains404. B is a valid same-scope batch with no prior attempt. Previously A throws DIAGNOSTIC_WRITE_UNCONFIRMED and flush exits before B:0 diagnostic POST,0 ACK,2 pending.

Only that internally generated deferred state now uses a module-private DiagnosticWriteUnconfirmed type. flush catches only that type, rechecks the original lease, retains A without POST/ACK/reset and continues to B. It does not classify SDK/server error text as authority to continue. Identity errors, permission denial, body/digest failure, invalid journal data and ordinary transport failures still reject the drain. An invalid or missing native claim reply is a journal integrity failure, not a deferred-write result; only the exact boolean false represents an already claimed/completed attempt. The canonical diagnostic body, native journal/claim/ACK implementation, generated SDK, bridge permission and original file transfer engine are unchanged.

No new queue, protocol, service, budget, timer, UI or permission was introduced. Existing natural resume events can later read A at its same path; a verified200 can ACK A. A404 never permits another POST. The conservative crash-between-reservation-and-dispatch boundary documented in the prior correction remains.

## Directed evidence

- Correct2d35 baseline:1 failing A/B case,4 passing negative guards,15 existing cases not rerun. Counts are explicitly0 POST/0 ACK/2 pending. The first raw run also recorded an incorrect test expectation that the SDK preserves a fetch exception message; the real throwOnError:false SDK normalizes it, and the corrected negative assertion still requires rejection and zero writes. Both logs are retained.
- Corrected A/B recovery: B performs exactly1 diagnostic POST and1 ACK, A stays unchanged/pending. When the same A bytes become readable, A receives its one ACK. Total remains1 POST/2 ACK/0 pending after another natural resume; B is not repeated.
-6 new queue cases pass /15 existing cases not rerun. Permission403, corrupt existing bytes, dispatch-time identity replacement, ordinary fetch failure and invalid claim reply all remain fatal and do not send B. The invalid-claim guard was separately red-tested during the narrow catch implementation before strict boolean handling was added; its log is retained.
- Current generated-context Web types and scoped lint pass. One new test fixture initially used a direct cast to a required bridge property; it is now modeled as optional and checked explicitly, without changing product behavior or weakening a runtime assertion. The initial type failure is preserved.
-193-source integrity passes; the builder's declared input hashes match the exact tested host modules. Native/app configuration, SDK and the previous official API26 qualification are unchanged, so no native compile or HAP build was repeated. Original P1/P2 and large-file matrices are reused.

The diagnostic FS requests in these tests are synthetic SDK/transport fixtures, not real NAS writes or business-file replay evidence. All parent-scope/identity checks remain. Preserved unsigned32 SHA256 f16df3de1d59e6c4008fad622d971ec7fdb1c84505c4bb9532e33f471f41845a and ZIP SHA256 a8d97a3814eda305d23445426f28f5abad911db1d1f802425b95757958553156 remain unchanged. Published32 still excludes the automatic diagnostic feature. Public-page identity bridge, real phone timing and HTTPS root cause remain unverified.
