# Windows configuration injection and Harmony upgrade

The generic cloud package now reserves an empty local configuration resource.
Windows fills that resource from its existing private configuration **before**
the original official signing step. The private configured/signed output stays
in the existing personal distribution flow; it must never return to GitHub.
The phone reads the signed local resource automatically. No address entry is
required for a correctly prepared package.

## Confirmed defects

The installed `775b494d` 1.0.7 package had no injection slot, and the signing task
confirmed that its payload was unchanged during Windows signing (apart from
`.pages.info`). The required Windows configuration step was missing. Its setup
logic also classified a missing new config key as an origin change, scheduling
session cleanup on the first save. Page copy incorrectly inferred that the old
version had never stored an address and claimed a backup had already been made.

The original signing task inspected its actual 1.0.6 package: endpoint in the
compiled `MEMOH_ORIGIN`, persistent WebView origin `begonia-client://app`, and
login key `localStorage['token']`. It did not read the user's token. There is no
old runtime endpoint key to invent. Exact onsite matching found no known old
address in the public 775b or signed 1.0.7 payload. The older private 1.0.6
payload does contain it; that finding alone does not prove public exposure.

## Existing configuration, one receiving slot

`mobile/client/mobile/client_config.py` owns the existing configuration format
and the local injection command. The Web builder adds
`resources/rawfile/client-web/memoh_client.json` to the HAP: an **8192-byte,
ZIP_STORED, space-padded JSON resource**. Public builds require empty `apiOrigin`
and `hapDownloadUrl`, and no legacy binding; a nonempty source configuration
fails the build. The existing native resource manifest includes its exact byte
length and keeps its runtime check. No native API, preference store, server or
alternate auth mechanism is added.

Windows uses its private `windows-binding-handoff.json` evidence to prepare the
following structure locally. Values below are placeholders, not deployable
configuration:

```json
{
  "apiOrigin": "<verified existing API base>",
  "hapDownloadUrl": "<existing personal distribution URL or empty string>",
  "productName": "Garnet-Assistant",
  "legacyBinding": {
    "schemaVersion": 1,
    "bundleName": "cloud.begonia.workassistant",
    "versionCode": 1000006,
    "sourceHapSHA256": "<SHA256 of the actual locally inspected old HAP>",
    "apiBaseSHA256": "<SHA256 of UTF-8 public_url(old API base, 'apiOrigin')>"
  }
}
```

`public_url` is the existing URL normalizer in the same Python file. The old API
base used for the digest must come from the signing task's verified old-package
binding, not from the new destination or an address typed on the phone. This
command validates that supplied binding and old package identity/hash; it does
not extract or independently rediscover the old compiled endpoint. The trusted
local signing step attests the configuration. For an entirely new installation
without old account data, `legacyBinding` may be omitted or null. Do not omit it
when delivering an upgrade that must preserve an unbound old login.

Run with local file paths only (never place private JSON or URLs in arguments):

```sh
python client_config.py --hap generic-unsigned.hap \
  --expected-sha256 PUBLIC_GENERIC_HAP_SHA256 \
  --config private-config.json --legacy-hap verified-old-106.hap \
  --output private-configured-unsigned.hap
```

The tool refuses wrong source/legacy hashes, identity mismatches, a missing slot,
a signed or already configured input, unknown fields, wrong binding, and an
existing output. It preserves the input and creates a distinct output. Only the
reserved payload and its standard ZIP CRC fields change; all member names,
order, offsets, padding, size and unrelated bytes remain intact. It then checks
all ZIP CRCs and every unrelated member. The original 775b package is correctly
rejected for its missing slot; do not patch its JS/ABC or disable integrity checks.

Retain the returned safe receipt (size, SHA256, one modified payload) privately,
then use the existing official signing/profile verification. Base build metadata
remains the generic build's provenance; the receipt and final signature identify
the configured derivative. Do not claim the derivative has the original public
hash. No account password, cookie, token or signing key belongs in this config.

## Phone state transitions

| State | Result |
| --- | --- |
| Existing valid persistent config | Takes priority before reading the packaged seed; upgrades preserve login and user settings. |
| Old login/data with verified matching legacy binding | Automatically saves the existing config key without reset, retains token/drafts/pins, then opens the application at the verified base. |
| New installation with configured local seed | Saves config without reset and opens login directly. |
| Unknown old state, invalid/mismatching seed, or failed write | No API request, credential transfer, data clearing or invented backup; original data remains. |
| Deliberate change of an already configured service | Existing pending-reset/auth cleanup runs before authenticated application imports. Download-only changes preserve login. |

A typed URL cannot authorize legacy migration. No raw private address or real
binding digest is needed in this cloud checkout. The earlier compile-verifier
proposal is superseded by this Windows-only configuration step.

## Verification and remaining acceptance

- [154 configuration/native bridge checks](unit-result.json).
- [31 injection checks](injection-result.json), including ZIP layout and payload
  preservation, refusal without mutation, and the actual old 775b missing slot.
- [Automatic migration browser checks](verified-binding-result.json): actual
  Harmony entry/shared UI at `http://127.0.0.1:4181`, Chromium 390x844, Chinese
  locale, synthetic old storage and intercepted HTTP. Automatic matching-base
  migration, mismatch/write-failure preservation with no API requests, persistent
  config priority, process restart and new install were exercised. Zero login
  HTTP calls during retained-session migration and zero page errors.
- Screenshots: [automatic retained session](verified-login-retained.png),
  [mismatching deployment preserved](different-base-retained.png). Agent inspected;
  not human QA.
- [Existing configuration flows](browser-result.json), [Web typecheck](typecheck.txt),
  targeted ESLint and source inventory are recorded alongside these tests.

These checks do not establish an actual HAP overwrite-install, native WebView
resource fetch, NAS authentication or phone runtime acceptance. The optional
`harmony_106_migration_ui.py` now requires automatic migration from actual old
resources; it has not run in cloud because those real old resources are absent.
Use the original Windows owner for private injection/signing and personal IPv6
delivery, then verify 1.0.6/1.0.7 to 1.0.8 on the existing phone without clearing
application data. The previous HAP stays preserved under its original identity.
