# Harmony 1.0.9 generic unsigned handoff

Source: `c9c00580052d09d5dce273e958ec384bef2ebed3`.
Shared Web/SDK: `8a2422935916f56b5bacfac2f991824d1cb9bd9f`.
Bundle: `cloud.begonia.workassistant`; version: `1.0.9 / 1000009`.
File: `memoh-harmony-1.0.9-1000009-c9c005800-api26-unsigned.hap`.
Size: **37,053,849 bytes**. SHA-256:
`ebe3b6168ec013d1e78a2a523a155c07e2122000c98e99316ff88997ca948ae4`.

This is the generic **unsigned** API26 package, not an installable signed release.
The public configuration resource is empty. Existing app identity, permissions,
login persistence and Windows injection slot are retained. The shared fa77 UI and
final unified SDK are included, plus the minimal fix that hides the service
address control after router login so it cannot cover the task action.

## Windows handoff

1. Receive this exact repository file and check `SHA256SUMS` and byte length.
2. Use [client_config.py from the source commit](https://github.com/particleflow/Memoh/blob/c9c00580052d09d5dce273e958ec384bef2ebed3/mobile/client/mobile/client_config.py)
   with the existing private Windows configuration and independently verified
   legacy binding. Follow the unchanged [local injection procedure](../../../docs/testing/harmony-service-migration/README.md).
   The tool hash and source lock are recorded in [build-identity.json](build-identity.json).
3. Inject into a distinct private unsigned output, then use the existing official
   signing/profile flow and the established private IPv6 distribution. Keep
   existing certificate/device bindings; do not clear app data or login storage.
4. Record configured and signed hashes separately. Neither derivative, private
   configuration, legacy binding nor signing material may return to GitHub.

[Build, package, routing and migration evidence](../../../docs/testing/harmony-unified-8a24229/README.md)
is available. The injector was exercised on this actual HAP with synthetic
configuration; only the reserved resource changed and all ZIP offsets/CRCs were
preserved. Real Windows injection/signing, phone overwrite-install and live
Windows/NAS acceptance remain the original onsite tasks. The 1.0.8 artifact stays
available unchanged; no release or tag is created for this handoff.
