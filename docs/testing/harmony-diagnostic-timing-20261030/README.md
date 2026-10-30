# Formal-client diagnostic timing semantics after1.0.33

Source-only correction based on exact published33 source8da01566dba0c79f7922e5979ff9a64e0a4e3c14. The published33 HAP/ZIP are preserved and still contain the old timing placement. This patch has not been packaged, signed, deployed or measured on a phone; it requires independent review.

## Confirmed old boundary errors

In harmony-files.ts, ordinary media uses the original generated getBotsByBotIdMediaByContentHash call with parseAs=arrayBuffer. The generated client awaits the approved fetch, response interceptors and response.arrayBuffer before returning result.data/result.response. Therefore the old headers event after that SDK result could not describe response-header availability. The delegated path already observed headers before its arrayBuffer call. Both paths recorded load only after readOriginalBlob returned, which includes integrity verification and Blob work.

The same original SDK method/body parser/authenticated configured fetch are retained. The ordinary per-call fetch observation now wraps that approved fetch exactly as the existing optional image-timing adapter does; delegation retains createApiFetch and its original scoped path/redirect rule. The wrapper forwards the same Request/init and returns the same Response without reading, cloning, replaying or substituting its body. There is no new transport algorithm, endpoint or permission.

## Exact event contract

| Existing event | Observation in this source | It does NOT establish |
| --- | --- | --- |
| send | Logical original read entry | TCP send time |
| headers | Response/headers become available at the original approved JS fetch boundary, before SDK body parsing | Native/on-wire header arrival or first body byte |
| load | The complete decoded ArrayBuffer is available to this reader, before length/SHA/Blob work | On-wire EOF, first byte, wire byte count or public throughput |
| validation | Successful length/SHA256 and original cancellation check, before Blob construction | Image decode/paint or overall business completion |
| loadend | The original read operation returns or fails, including later Blob work | Network-body completion |

No first-byte field is observed or synthesized. A lower adapter can buffer data before it yields a Response; in that case headers/load can be simultaneous at JS visibility. A zero interval is unknown lower-layer transfer timing, not zero network time or infinite speed. Body size is decoded payload length. Client scheduling, bridge and SDK costs mean these differences alone are not public-network throughput. Old33 traces cannot be retroactively reinterpreted as the corrected boundaries.

The whitelist's ability to accept progress is not evidence that this automatic read path emitted it. It still emits no per-chunk progress, no TCP/IPv6 correlation and no new fields. The existing explicitly armed image ResourceTiming collector remains separate; its card timestamp is an observation start, not proof of display.

## Controlled differential evidence

The tests execute the actual owned originalBlob and generated SDK using real Node Request/Response/ReadableStream and WebCrypto SHA; only the approved transport and diagnostic sink/clock are controlled. No real NAS/model request or throughput measurement is performed.

Two exact33 regressions are red: ordinary media lacks headers at the before-body boundary; delegation lacks body completion while SHA is blocked. Both become green. Each receives the same7 bytes as3+4 chunks with explicit gates: response visibility10, first controlled chunk30 (no event emitted), decoded body70, successful verification110. The observed body-read interval60 and verification interval40 are controlled test-clock values only. Both preserve exactly one original GET, original synthetic authorization and the correct media or delegated URL; SHA verifies the real payload.

A third buffered-fetch case passes: no headers are invented while the lower adapter is unresolved; headers and load become visible together at70, verification at110. No firstByte/progress value is produced. The original21 cases, native/unknown-write/journal matrix and official SDK qualification are reused rather than rerun. Two new streaming cases and the additional buffered case are recorded in separate passing logs. Current generated-context types, scoped lint and193-source integrity pass; declared builder input hashes match the tested modules. Native code, generated SDK, identity lease, deferred queue/unknown-write logic and bridge/permission declarations are unchanged. Only a semantic comment is added to the diagnostic event vocabulary.

The initial delegated test fixture omitted its global-fetch adapter: the original delegated createApiFetch path attempted the reserved synthetic example.test host and failed DNS, not NAS. That unqualified test log is retained. The fixture now adapts the original global fetch as well, and races setup against the read outcome so failures drain the stream/hash gates without dangling rejections. Corrected red/green logs have no unhandled errors. No credential other than synthetic test data was used.

Published33 unsigned HAP SHA25644ae980003bbdea6b22674463b414b11020dcd7eb0054c5f72ec9fbd65b464a8 and ZIP SHA256b808301b58fe6885af9ef58b3f4d9713c25943032269b2df52d72cf2ea3e5403 remain unchanged. Public-page identity bridge, real phone timing, HTTPS root cause and5MB/s acceptance remain unverified. No HAP build/deployment or change to the stable large-file transmission engine occurred.
