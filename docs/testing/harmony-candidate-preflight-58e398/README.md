# Harmony native transfer candidate handoff preflight

The current phone source can form an independent **native transfer test
candidate** against the fixed server API below. The Web file owner's unfinished
implementation does not block that candidate. It still blocks a claim that the
final shared Web and phone file flows have been accepted together. This audit
does not build, publish, sign, or install a new HAP.

## Exact inputs and scope

| Input | Fixed identity |
| --- | --- |
| Phone product source | `760ed76e82da7b66c62e5d8a9ce01cc490a7b89d` |
| Shared Web and SDK lock | `cbcee15a17dc87fd02eb4c198fba0aa45eb3fa45` |
| Server source inspected | `58e39866057bf798b0789053d3215ddf4600c612` |
| Native source already officially compiled | `273415c900b39693e2060c991fa2696ebae8a607` |
| Working branch / PR | `feat/harmony-native-runtime-ui-20261025` / [PR82](https://github.com/particleflow/Memoh/pull/82) |

The server commit was fetched through the existing Git integration branch.
The parent task reports the corresponding NAS deployment. This cloud audit did
not query the private NAS or independently verify its running image.

[preflight.json](preflight.json) records equal Git tree/blob IDs for Web, SDK,
OpenAPI spec, all handlers, Media, transfer client, user runtime, and the three
producer source files. These scopes are **identical between cbcee and 58e398**.
The newer server changes concern native batch recovery; no phone SDK or route
change is required for this comparison. The 43 phone production adapters are
retained. The entire native app and the listed configuration, entry, upload and
download adapters are also byte-identical to the officially compiled 273415
source. No product change was necessary in this preflight.

## Existing NAS contract retained

| Phone use | Existing server contract and recovery boundary |
| --- | --- |
| Authentication / limits | Fresh `/users/me` identity and Bot-scoped `/bots/{bot}/media/limits`. API base includes its `/api` prefix. No PC runtime credential or task lease is substituted. |
| Create | `POST /bots/{bot}/media/uploads`, JSON `size_bytes`, `sha256`, `mime`; 201 receipt. Persist `create-unknown` before the single POST. A lost reply cannot be resolved by an invented lookup or another Create. |
| Resume / append | GET the original receipt; PATCH that ID with `Upload-Offset`, `Upload-Length`, `Upload-SHA256` and raw bytes. Native chunks are at most 256 KiB, within the existing 8 MiB server bound. Lost append response is reconciled by GET. Receipt 404 retains ID, offset and local bytes. |
| Complete / attach | A single complete POST, then GET the same ID after an uncertain response. Only `ready` with matching size, offset and content hash becomes an attachment reference. No uncommitted result is called delivered. |
| Lifetime / pause | Ordinary receipt TTL remains 24 hours. Pause retains the journal; explicit discard is separate. Chat confirmation consumes local state only, without deleting committed NAS originals. |
| Download | Existing Bot/hash or exact delegation path, fresh identity and scoped authorization, ranged bytes and persistent checkpoint, full size/hash before saving. Scoped 401/403 never falls back to a broader Bot/hash path. |

These continue through the existing Media service and configured original store.
The phone supplies neither a new storage root nor a new transport service. The
four byte intake paths are native selection, paste, drop, and completed
recording; restoration reuses the original journal. The shared file owner's
upload/download implementation is untouched.

Existing evidence remains valid for its stated scope: [39 real-disk/local-HTTP
upload cases and the 18 MiB independent-process download recovery](../harmony-native-transfer/README.md),
[official API26 native compilation](../harmony-native-transfer/native-build-verification.json),
and [current cbcee types, 160 selected shared tests and generated-page
interactions](../harmony-unified-cbcee15/README.md). Those checks were not rerun
here. They do not establish phone OS behavior or a live phone–NAS transfer.

## Retained artifacts and toolchain

Both files were read again and matched their full size, SHA-256, ZIP CRCs, empty
8192-byte stored configuration slot, app identity and permissions:

| Artifact | Bytes | SHA-256 | Use |
| --- | ---: | --- | --- |
| Generic 1.0.9, source `c9c005800` | 37,053,849 | `ebe3b6168ec013d1e78a2a523a155c07e2122000c98e99316ff88997ca948ae4` | Existing repository handoff; shared 8a, predates native transfer wiring. |
| Native compile check, source `273415c9` | 37,182,314 | `b544eeccf83d5a8e279b733649bf24bdca327d98e0acf2ebdeedda68c5e459df` | Private validation output only; shared 8a. Never label it the current cbcee candidate or distribute it as a new upgrade. |

Both still report `1.0.9 / 1000009`; neither represents 760ed. No current-source
HAP exists. Earlier 1.0.8 and 1.0.9 handoffs are preserved unchanged.

The existing official Linux CLT **26.0.0.851** and HarmonyOS **API26 SDK
26.0.0.105** remain available. The retained CLT ZIP is 2,348,063,309 bytes; its
completed-download record has SHA-256
`ab604bd92721d5cbcafd154e6461d46b9f1b105e7b89eab04a9d046681198082`.
This audit checked its continued presence/size, SDK metadata and exact local
Node, hvigor, ohpm and signing-tool file hashes; it did not rehash the entire SDK
archive or download anything. About 15 GiB remained free. No cleanup occurred.
The previously verified LibRaw and existing build caches remain in place.

## Configuration, TLS and upgrade identity

- Bundle remains `cloud.begonia.workassistant`, module `entry`, API26,
  `runtimeOS=HarmonyOS`; cloud `signingConfigs` is empty. The package permissions
  in the machine-readable report are unchanged, including `READ_IMAGEVIDEO`.
- Public `apiOrigin` and `hapDownloadUrl` remain empty and `legacyBinding` absent.
  Windows alone fills `resources/rawfile/client-web/memoh_client.json` before
  signing, using the existing [injection procedure](../harmony-service-migration/README.md).
  `client_config.py` SHA-256 remains
  `7b29495f70ae65aca4f3003e8b828e81b26fe4d489470b3588c1b9ad06a7eeb7`.
- Existing `memoh:harmony:service-config:v1` takes precedence over the package
  seed. The stable `begonia-client://app` WebView and `localStorage['token']`
  remain. The signed local seed can establish verified 1.0.6 legacy binding;
  unknown or mismatching state is preserved without sending old credentials to
  a new destination. Do not clear app data or change the existing identity.
- The Python and TS/native normalizers accept explicit LAN HTTPS and bracketed
  IPv6, require an API path ending `/api`, validate ports, and reject userinfo,
  query/fragment, encoded paths, dot segments and loopback/link-local addresses.
  A non-LAN HTTP compatibility path still exists; this is not a claim that HTTP
  has TLS. The existing private deployment must use its verified HTTPS base.
- Native `rcp.createSession()` uses no custom security override. The installed
  official SDK declaration specifies system CA validation by default. Native
  authenticated upload/download requests set `autoRedirect: false`. No new
  certificate bypass or custom trust store was added. Valid URL syntax alone
  does not prove certificate trust, hostname matching or reachability.
- There is one explicit API base. Automatic LAN-first/IPv6 failover and identity
  migration between endpoints are **not implemented by this source**. Do not
  claim that URL acceptance provides routing or silently move a token.

Private endpoint validity, the actual certificate chain/hostname, and network
reachability must be checked by the existing onsite owner against the intended
phone network. No private address or credential is needed in this public report.

## Fixed next-candidate procedure

1. The independent native candidate uses product base **760ed76e**, shared lock
   **cbcee15a**, and the inspected **58e39866** server contract. Before one new
   build, increment the existing app version to **1.0.10 / 1000010**, update its
   normal source inventory, and commit that final build input on the same
   branch. This is planned, not applied by this preparation. Record that exact
   resulting commit; do not relabel either 1.0.9 artifact.
2. Reuse the established Web packaging/materials and official `build_api26.py`
   wrapper with the retained SDK. The working native entry is
   `hvigorw assembleHap --mode module -p product=default -p module=entry@default -p buildMode=debug --no-daemon`,
   invoked through the wrapper, not a guessed standalone `CompileArkTS` task.
   Inspect the resulting identity, source/resource mapping, permissions,
   size/hash, CRC, empty deployment slot, and public content before handoff.
3. Preserve the generic unsigned file, checksums and source identity through
   the already authorized versioned `artifacts/harmony` repository path. Use
   its immutable commit for the original Windows writer. No Release retry,
   Library workaround or duplicate upload is part of this plan.
4. Windows checks the generic hash and runs the pinned existing injector with
   local file-path arguments only. Existing private deployment configuration
   and verified legacy package/binding stay on Windows. Record the configured
   derivative hash separately, then use the existing official signing flow.
   Profile, certificate, key alias/password and authorized devices are reused
   by the onsite owner; none are read or transferred to this cloud task.
5. Keep the same bundle and application identity. The earlier onsite record
   reports App ID `6917618151422994484`, one bound device and certificate
   validity through `2027-10-03`. These are historical onsite facts, not a fresh
   cloud signature/profile verification. The onsite signing step must check
   current identity, validity, permission coverage and signature normally.
6. Configured/signed derivatives stay in the existing private IPv6 distribution
   flow and never return to GitHub. Record each distinct hash. Then verify a
   normal overwrite install and retained login; exercise all four intake paths,
   interrupted/restarted bytes, denied delegation, corruption rejection and
   attachment references against the real NAS. No phone result is asserted here.

The final shared file head is needed before accepting **shared Web file
recovery and cross-end integration**, not to prepare or test this already wired
native candidate. If it arrives before the next source freeze, incorporate it
once and record the new exact lock before building. This preflight introduces
no alternate protocol, permission model, endpoint field or product change.
