# Restored Harmony runtime configuration and download integration

This is the historical restored-runtime integration audit, retained for its scoped download/configuration checks and original failed-stage evidence. Use [the current mobile entry](../mobile/README.md) and [1.0.19 qualification](testing/harmony-unified-1.0.19/README.md) for the active source, build status and package identity. The source pins, file counts and preparation failures below belong to this audit checkpoint.

This checkpoint follows `732f09d8f25893b63f5b2d5ba6d35c5f3d28fd15` on the
recovery branch. It retains the recovered native shell and overlay builder,
pinned to unified Memoh `40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`.

## Connected source boundaries

- `NativeClient.ets` dispatches the existing `NativeMediaDownload.fetch`, `save`,
  `abort` and `close` methods. Requests remain restricted to the packaged main
  document, version 1, and a configured, ready client. Configuration changes,
  logout/failure and detach close downloads and invalidate in-flight work.
- `client.configuration` clears the native in-memory origin before configuration
  UI starts. `client.configure` uses the existing locked, validated origin.
  Neither command migrates a token or selects a different network route.
- The existing Harmony builder starts `harmony-entry.ts`, waits for local
  configuration, then imports the upstream app and passes `apiOrigin` to its
  existing `setupApiClient`. Browser builds retain their normal entry. No
  endpoint is compiled into the Harmony distributable.
- The existing packager preserves `MemohClientConfig.ets` and generates
  `ServiceUrl.ets` from the reviewed Web validator using the platform parser
  already used by `SharedWebPolicy.ets`: `url.URL.parseURL`. Generated resources
  and validators are covered by the resource receipt. Fresh-checkout packaging
  creates its owned rawfile directory and supports upstream `.webmanifest`.

`apiOrigin` still means the explicit API base, including the `/api` suffix.
There is no automatic LAN/IPv6 selection, HostBinding migration, new service,
new routing configuration or cross-process download scheduler. Scoped original
references remain exact delegation paths; denial does not widen to Bot/hash.
At this audit checkpoint, the complete pinned 40f archive replaced the older
standalone observation preparation path. The current producer's archive and
overlays are documented in [the reproduction guide](../mobile/BUILD-REPRODUCTION.md).
The legacy helper/host/adapter and unused metadata have been [retired](../mobile/SHARED-OBSERVATION.md); their archived audit descriptions do not make them current build steps.

## Executed checks

The Harmony-target Web build passed frozen dependency installation, dependency
patch verification, full Vue type checking and production bundling. It produced
786 resources. See the [build receipt](testing/harmony-native-integration/build.json),
[typecheck log](testing/harmony-native-integration/typecheck.log.txt),
[build log](testing/harmony-native-integration/vue-build.log.txt) and
[resource receipt](testing/harmony-native-integration/resource-package.json).
Checkout prefixes, terminal control sequences and trailing whitespace are normalized in logs.

The packager completed and verified all resource hashes, preserved the runtime
configuration source, rejected a deliberately changed generated validator, then
verified the restored bytes. All 32 native relative imports resolve, including
the generated modules. The source inventory remains 116 entries / 125 public
source and documentation files under `mobile`; generated output is excluded by
explicit ignore rules. The checker still includes unexpected untracked source.

The existing configuration/real-router test passed 115 checks. The download
frontend test passed its identity, cancellation, late reply, logout and frame
checks. The real native download class passed controlled local HTTP and disk
checks with an 8 MiB original and 1 MiB failure cases: saved hash, bounded reads
and writes, one foreground range retry, invalid headers, 401/403/redirect denial,
cancellation, picker dismissal, cross-Bot refusal, short writes, disk failure,
cache mutation and descriptor cleanup. Scoped routes use different Bot, session
and delegation UUIDs and never issue a broad fallback request. The recovered
Node file adapter was corrected to honor the native source's explicit file
offset; its measurement timer now closes on failures. Production file IO code
was not replaced to accommodate a mock. See [download results](testing/harmony-native-integration/native-download-source-test.json)
and [source checks](testing/harmony-native-integration/source-checks.json).

System Chromium exercised the actual packaged Web at a temporary localhost
preview, 412 by 915 pixels: empty configuration, rejected private HTTP without
saving, explicit synthetic HTTPS configuration, empty-token login, and return
to service settings. No page exceptions occurred. Synthetic API interception
was enabled; zero such requests occurred. Screenshots were inspected and remain
in the private workspace. This is a browser preview, not native runtime or
backend acceptance. See [browser evidence](testing/harmony-native-integration/native-config-browser.json).

## Historical build blockers and remaining acceptance

At the blocked stage recorded by this audit, the official Linux x64 Command
Line Tools 26.0.0.851 and SDK API26 / 26.0.0.105 were absent from the saved assets. The previous official
entry visit failed with `net::ERR_CERT_AUTHORITY_INVALID`. No download retry,
certificate bypass or alternate browser was attempted in this checkpoint.
The supported acquisition entry remains
[Huawei developer downloads](https://developer.huawei.com/consumer/cn/download/).
The historical CLT SHA256 is
`ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`.
LibRaw 0.22.2 was subsequently materialized from the locked official source and
all 244 files verified; see [dependency input evidence](harmony-libraw-input.md).
The later [metadata bootstrap correction](harmony-materials-bootstrap.md)
generates and verifies the previously missing native resource inputs. No SDK/CLT
or dependency binary is published here.

This stage did not execute ArkTS/native C++ compilation or produce/sign a HAP.
Subsequent official compilation and package qualification are recorded by the
versioned build reports linked above; this old acquisition failure is no longer
a current compiler blocker. Harmony permission/TLS behavior, OS picker operation,
NAS authentication, phone-to-NAS-to-phone transfer and human QA were not established
by this audit. Full development services were not running for its browser preview,
which does not satisfy runtime acceptance. No old A11 package is offered as a new
result. The 19 historical later cloud modifications remain
unverified; this commit does not claim identity with the unavailable 5a7 snapshot.
Original ZIPs, private configuration/backups and all preceding commits remain
preserved.
