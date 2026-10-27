# Garnet Assistant accepted-input recovery integration

Shared source: `f4ef1b5d17abdc322de8ca33963a6e45f00a00c1`. Compatible backend: `3d5f145c452dda5e17861d401de049b7ddded2e6` (parent reports Web/SDK/Swagger/mobile byte-identical; backend CI/deployment remain separate).

The existing reply hint consumes the original submitted decision and reliable failed run. Admission (202) is not completion: subsequent reads track the same invocation. A repeated click does not resubmit the accepted answer or start a whole-turn retry. A 409 is displayed once in the same hint. Missing historical run identity disables recovery rather than guessing.

The 40765 conversation-scoped Agent/sub-Agent/Bot component remains active; the removed global-sidebar mobile override is not restored. Existing login migration, native attachments/restart journals, strict TLS, directory settings and default-off numeric image timing remain unchanged. No server, permission or dependency additions.

## Verification

Generated phone context typecheck and production Web build passed. Eight affected test files cover 65 unique tests: the combined concurrent run passed 63 and hit an initial import timeout followed by one dependent keyboard failure in message-item.test.ts; the unchanged file then passed all 5 cases in isolation with the same 15-second timeout. Original failed and successful logs are retained; no assertion or timeout was relaxed. Affected ESLint: zero errors, 29 existing layout/test-prop warnings.

Compiled production Web at 390×844 with synthetic HTTP and the actual runtime WebSocket client proved: double click yields one POST; accepted/running/completed reads retain the same invocation; a separate 409 yields one inline feedback; no mount/reconnect POST, answer replay, whole-turn retry or login reset. The screenshots and numeric proof accompany this record. This used no real provider, production decision, phone or paid call.

The next generic release is 1.0.18 / 1000018. Public configuration remains empty; the existing Windows owner injects approved private configuration and signs. Configured/signed private output must not return to GitHub. SDK compile and artifact hashes are recorded separately after the build. Historical 19 later source modifications remain unverified; this integration does not claim restoration of the unreachable original commit.

## Official 1.0.18 build

Official SDK 26.0.0.105 `assembleHap`, release mode: success, 14.688 seconds, 33 tasks (24 executed, 9 up-to-date). The unsigned HAP is 35,652,954 bytes; SHA256 `1ed8e88bcc62bab3b11f06b8331b016fccec57c34a783b2d46ebbca8fad18bc7`. Formal source identity: `88b63a072e722d9725907252c0636bccc69790260e9a68484b5d0c696827a33a`, 138 bound inputs and 803 Web resources.

All 818 ZIP members passed CRC plus project resource/source digests. Application identity/permissions match the preserved prior package apart from version/build mode. The 8192-byte configuration slot is empty (no API origin, download URL, legacy binding or network policy). Explicit credential/private-authority and signing-material scans found no matches; this does not claim exhaustive detection of arbitrary encodings. The packet is generic and unsigned, not an installable signed delivery. No new SDK download or production recovery occurred.
