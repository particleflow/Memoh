# Mobile URL validation and retained typecheck evidence

This small follow-up starts from `14bb9b85004477b8a6628572e1dfbee480a9bf5c`.
It aligns the Python build-input validator with the existing TypeScript URL
path and normalization rules. Encoded paths, dot segments, doubled separators,
non-`/api` API paths and malformed ports now fail in Python as they do in TS.
Both validators reject empty ports and port zero, and admit only 1..65535.
Python now returns the same normalized host, IPv6 representation, default-port
omission and single-trailing-slash removal for the tested accepted inputs.
The optional installer URL is normalized too, without requiring `/api`.

No new configuration field or route selector is introduced. `apiOrigin` remains
the existing complete API base; `hapDownloadUrl` remains an installer location.
No saved configuration or credential is migrated by this change. Existing
changed-origin authentication invalidation remains in place.

## Shared vectors and explicit policy differences

The fixture is an unchanged copy of
[`api-base-v1.json` at `91149ad7`](https://github.com/particleflow/Memoh/blob/91149ad7ab5c0d24a57f10e7d9ff1df4b71cf51c/internal/transferclient/testdata/api-base-v1.json)
under the existing repository license. Its SHA-256 is
`ec9b39d5b16b2655e225cba9eb4e97483001e21b9fdd45ad095074b7e0a68c59`.
The [shared endpoint contract](https://github.com/particleflow/Memoh/blob/91149ad7ab5c0d24a57f10e7d9ff1df4b71cf51c/docs/file-service-endpoint-contract.md)
allows client-specific destination restrictions. Tests execute both actual
validators against all 30 vectors and 22 additional normalization/path/port
cases. They explicitly record these existing differences, rather than claiming
complete transfer-client compatibility:

| Case | Shared transfer contract | Mobile behavior retained |
| --- | --- | --- |
| Root or arbitrary API prefix | Root and nested prefixes supported | Both validators require an unencoded path ending in `/api`; `/memoh/api` is valid, root and `/gateway` are not. |
| Public HTTP | Rejected; HTTPS only | Existing explicit public HTTP compatibility remains in both validators. Newly admitted LAN endpoints still require HTTPS. |
| Documentation IPv6 `2001:db8::10` | Accepted as a syntax example | TS accepts the example; Python's existing `ipaddress` destination restriction rejects it. This address-policy difference is recorded, not broadened by this path/port fix. |

The fixture does not establish LAN preference, reachability or same-NAS
identity. Automatic LAN-first/IPv6-fallback selection and HostBinding/owner
migration remain unimplemented. Accepting a URL never transfers a token to a
second address. TLS certificate validation remains required.

The scoped-download test now uses different Bot, session and delegation UUIDs
and asserts the exact resulting URL at both adapter and native admission
boundaries. This exposes segment mixups that equal UUID fixtures could hide.
Authorization denial still has no ordinary Bot/hash fallback.

## Focused checks

Using the already available Node 24.19.0, TypeScript 6.0.3 and Python 3.12.14:

- Cross-language validator test: 30 shared vectors plus 22 additional cases.
- Python endpoint tests: five methods passed.
- Existing configuration/storage tests: 74 checks passed.
- Download routing tests: distinct UUIDs, ten malformed scopes, five invalid
  native URLs, scoped 401/403/redirect rejection and account-change rejection.
- Strict no-emit typecheck of the service URL/configuration modules and ESLint
  of the service URL module passed.
- Source-manifest and syntax checks passed; reviewed source hashes retain
  their previous checkpoint hashes.

Reproduce from the repository root (set `HARMONY_TEST_TYPESCRIPT` to an installed
TypeScript package path if it is not available through normal Node resolution):

```sh
node tests/harmony/endpoint_parity.test.cjs
python3 tests/harmony/test_service_endpoint.py -v
node mobile/client/tests/unit/service-config.test.cjs
node tests/harmony/download_scope.test.cjs
python3 mobile/check_source_subset.py
```

## Existing 145/147 diagnostics, without another full build

All four original typecheck logs were still present. The attachment-before and
attachment-after logs are byte-identical: 47,514 bytes, SHA-256
`86b1d2623c298f87d9c750f0274d5533d5f4789725d8064e8cfd1d8ca7c6386b`.
The `84ca0e5` control-adapter and `14bb9b8` candidate-adapter logs are also
byte-identical: 47,777 bytes, SHA-256
`e0ccb3b46c81286bc9e63b5afd1c2b3cdae7574a7d46624bed0b85a57c915178`.
Thus the old/new adapter diagnostic diff is empty, including multiline details.

Published copies replace only the local dependency checkout prefix with
`<dependency-root>/`; the unmodified originals are preserved in private evidence.
Identical logs are deduplicated into [upstream-145.txt](testing/harmony-url-parity/upstream-145.txt)
and [adapters-147.txt](testing/harmony-url-parity/adapters-147.txt).
The [145-to-147 diff](testing/harmony-url-parity/upstream-to-adapters.diff)
contains exactly two extra diagnostics: missing `createApiFetch` and
`authSessionGeneration` exports expected from the unavailable overlay.
The [evidence manifest](testing/harmony-url-parity/results.json) records original
and published hashes, counts and the fixed Web/UI/adapter input commits.
This is retained historical evidence, not a new full typecheck of this follow-up.

No full Web/HAP build was rerun to collect these logs. Full Web typechecking
still fails. The Library helper's hosted-apps `tools/list` HTTP 401 still blocks
both source ZIPs; no alternate materialization route was attempted. Missing
overlay/native-shell inputs still block complete UI caller forwarding,
`ServiceUrl.ets` generation, HAP build and real runtime verification. No new
phone-to-NAS-to-phone acceptance, screenshots or human QA is claimed.
