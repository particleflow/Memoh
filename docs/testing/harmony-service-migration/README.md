# Harmony service migration checkpoint

This is a source checkpoint after the installed 1.0.7 / `775b494d` report. It
is **not a completed 1.0.6 migration or a newly built HAP**. The previous HAP
and its distribution commit remain unchanged.

## Confirmed defect and repair

The old setup code treated `old == null` as an origin change. Saving the first
address therefore scheduled auth/draft cleanup, even when the entered address
was the original service. Its message also inferred that no old address had
been saved from the presence of `token` alone, and claimed a backup before any
backup had been made.

The existing configuration module now distinguishes:

| State | Result |
| --- | --- |
| Initial installation, with no account data | Save the address without scheduling session cleanup. Existing appearance/language preferences are retained. |
| Existing valid service configuration | Same API base retains login; an explicit different base uses the existing pending-reset transition. |
| Independently established legacy binding | The configuration function retains token and data only for the exact normalized API base. A different base is rejected before any write. This branch is currently tested with synthetic evidence; deployment verification is not connected yet. |
| Unbound old state or invalid configuration | Preserve original storage and any previous recovery snapshot. Do not overwrite config, clear login, create credential copies, or import authenticated application code. Setup explains the unresolved binding and disables save. |

Classification is read-only. A typed address alone cannot establish a legacy
binding. No new persistent config key, server, transport, permission, account,
default endpoint or alternate auth mechanism has been added.

## Actual old-package evidence and remaining input

The original signing task reported a read-only inspection of its actual 1.0.6
package: its service address was a compiled `MEMOH_ORIGIN`, the WebView used
`begonia-client://app` with persistent storage, and login used
`localStorage['token']`. It did not read the user's token. These are delegated
package findings, not a claim that this cloud task inspected that old binary.

Library metadata located the exact 1.0.6 private handoff archive, but the
official list-result materialization helper stopped at hosted-app tool discovery
with HTTP 401, before downloading any bytes. That action was not retried through
another route. The separately available A11 is actually 1.0.11-dev, and the
restored original source archives declare 1.0.2; neither substitutes for 1.0.6.

There is no old runtime endpoint key to wait for or invent. A proposed minimal
build input is the verified old package identity and
`SHA-256(UTF-8(normalizeServiceUrl(oldApiBase)))`, supplied privately by the task
holding that package. The existing build would inject only a matching verifier,
not an endpoint default. Only an exact match could authorize the legacy branch;
other addresses could not receive the old credential. The raw address/token
must not enter Git or public artifacts. This deployment verifier is **not yet
implemented or provisioned** in this checkpoint.

A digest cannot recover or prefill the original address. This proposal preserves
login after a matching first entry; it does not claim automatic address recovery.
If automatic prefill is required, an independently authorized private local
configuration delivery is needed. No new delivery protocol is introduced here.

## Verification

- [135 checks](unit-result.json): real TS/ETS with synthetic storage/native IO
  adapters. Covers all four states, read-only classification, same-base legacy
  retention, different-base refusal, quota failure retaining original data,
  configured service switches, and the existing native bridge boundaries.
- [15 browser scenarios](browser-result.json): current Harmony entry and actual
  shared UI on the existing Vite at `http://127.0.0.1:4181`, Chromium 390x844,
  Chinese locale, disposable persistent profile, intercepted synthetic HTTP.
  Covers unknown-state preservation, fresh setup, process restart/resource reload,
  download-only settings, deliberate destination changes, expiry/revocation,
  logout and invalid schema. Zero final page errors, no live server or device.
- [Screenshot](unbound-preserved.png): actual unknown-state page preserves old
  data and makes no premature backup claim; agent-inspected, not human QA.
- [Web typecheck](typecheck.txt), targeted ESLint and source inventory passed.
- The first Vite run rejected a cache-busting script query in its test assertion;
  the next stalled while closing a restored HMR page. The fixture now checks the
  script pathname and navigates that restored page through `about:blank` after
  routing is installed. A subsequent blank-page locale initialization error was
  corrected in the fixture. These intermediate attempts are not reported as
  passing runs; the final result has no page errors.

The actual-old-resources test `harmony_106_migration_ui.py` now requires session
retention at the verified original base, instead of calling forced re-login
success. It has **not run**: the real old resources and a provisioned candidate
are still required. Synthetic binding tests cannot replace that acceptance.

```sh
node mobile/client/tests/stage2/harmony_service_config.cjs
HARMONY_TEST_BASE=http://127.0.0.1:4181 \
  python mobile/client/tests/stage2/harmony_service_config_ui.py
python mobile/check_source_subset.py
```

After deployment verification and the actual migration test, increment the
existing application version to the next patch and build once with the retained
SDK. Use the already authorized repository-file distribution, original Windows
signing task and IPv6 delivery. Do not relabel or rebuild the old 775b artifact.
