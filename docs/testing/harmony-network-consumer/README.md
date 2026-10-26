# Verified native network consumer integration

The independent 14-file handoff was materialized into this executor through the
official Library helper and inspected before applying. The attached README is
the specialized acceptance/integration checklist used for this step.

- Base: `7c96d6d2ca753c3f3cb17d237a1920f7633bfdcc`.
- Input patch SHA256:
  `48a53dc2606f23c01c3c7d0a3beefd54e7c301141632561928c4e441c9edc178`.
- Full reconstructed input tree:
  `37ecf303d139fc0615f7b5a3951150f22c228c2c`.
- [All 14 file hashes](input-checkpoint.json) and the complete tree matched
  using an independent Git index before working-tree application.
  [Consumer-side verification](input-verification.json).

No PR79 Web tree was imported. The product patch is byte-identical to the
reviewed handoff: no SDK-driven product fix was needed. The only extra source
registration edits update `SOURCE-PROVENANCE.json` and its exact expected count
in `check_source_subset.py`: three legitimate new sources, **131 entries / 140
files**. This is not recovery of the historical 19 unverified modifications.

## Product behavior and limits

The existing private signed 8192-byte seed optionally carries
`networkPolicy: {version:1,lan_addresses:[...]}`. Omission preserves the old path;
the policy is bound to the exact active canonical API base. It is passed through
the existing trusted configuration bridge and cleared with that lifecycle.
There is no new endpoint, credential, permission, WebStorage key or transport
service, and the public seed still has no policy or address.

Each existing foreground upload/download owns one `NativeTransferNetwork`.
Singleton DNS hints retain the canonical HTTPS URL and port, system trust and
disabled redirects. Existing side-effect-free JSON GETs (`users/me`, limits,
original receipt) may select/cache a candidate after controlled network errors.
They are not extra probes. This is explicitly not proof that a failed write was
unsent, nor the Windows dialer's strict TCP-only fallback semantics.

POST/PATCH/DELETE, HEAD and file-byte GET execute once through this helper.
Uncertain writes remain owned by the original receipt reconciler. TLS and all
HTTP responses, including 401/403/503, do not select another address. Existing,
unknown or unreadable proxy state and IP-literal canonical authorities preserve
platform routing. Shared WebView Fetch remains platform-routed. Do not claim
automatic strict routing for those cases.

## Executed official validation

The retained official Linux CLT26.0.0.851 / SDK26.0.0.105 compiled the actual
product patch through the existing `build_api26.py` wrapper and full
`hvigorw assembleHap` entry. This was one new-code compilation, not a repeat of
the distributed 1.0.12 source. `CompileArkTS` actually executed. Build and
qualification exit codes are **0**. All **105 native inputs**, packaged Web
resource digests, ABC and arm64/x86_64 ELF checks passed; SDK metadata remained
unchanged. All **166 compiler warnings** remain in [official-build.txt](official-build.txt).
No compiler, assertion, SDK or permission setting was weakened.

[Build verification](verification.json) records the input identity
`82c8630d4b8036f632c993aef271403e95d1b7bfe7a1e446d7d56c5582a5bbb6`.
The generated complete Web application also passed its normal build/typecheck.

The existing compiled-page upgrade fixture was reused to close the handoff's
ENOSPC-blocked frontend check. At the fixture's private loopback server,
Chromium390x844 loaded the newly compiled page with synthetic packaged config,
API and native replies. Both persistent old-HTTP and unbound legacy-login cases
retained login/drafts and forwarded the exact signed same-base policy through
`client.configure`. Unknown origin retained data and made no API request. One
login total, zero page errors. [DOM/bridge report](ui-result.json). This is not
visual/human QA, real DNS, TLS, ArkWeb or phone execution.

## Reused exact-source checks

The source hashes match the handoff, so its already completed checks were
retained and not blindly repeated:

- [61 policy/product Kit-boundary assertions](product-network.txt).
- [33 Python private-slot checks](private-slot.txt).
- [178 existing configuration checks](service-config.txt).
- [19 controlled 2 MiB upload scenarios](native-upload.txt), real disk/local
  HTTP and process death with simulated DNS/Kit boundaries.
- [18 MiB independent-process download recovery](native-download.txt), including
  final hash and scoped authorization/no-broad-fallback checks.

The obsolete unbound-auth unit's [baseline failure](legacy-unit-baseline.txt)
was preserved; it fails on both original and modified source and is not called
green or repaired by an unrelated login change.

## Artifact and field boundaries

The new private **compile-only** unsigned HAP is **37,229,071 bytes**, SHA256
`6f32b038dbfb3691269a88a72a3fc6a5a678c3cfc2fa8b324b22a576ab8da080`.
Its ZIP CRCs pass and its public seed remains empty. It deliberately retains
1.0.12 / 1000012 and is not a new upgrade or signing/distribution candidate.
The original distributed 1.0.12 HAP remains byte-identical, SHA256
`4d5c4c4ffe1a32d19321597281fdcb6c8f725511362aafb870704c2bceb82780`.
Do not inject the new field into the old package or relabel this check output.

No version increment, signing, release, registry publication, NAS deployment or
Windows operation occurred. Real RCP DNS/pooling, canonical Host/SNI/system TLS,
valid field endpoint, proxy/literal behavior, native lifecycle and signed-device
LAN/IPv6 handover remain separate acceptance. A successful official compiler
or Node adapter test is not that field evidence. Final source composition and
any future versioned delivery remain with the original unified/field owners.
