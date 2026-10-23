# Existing frozen-checkpoint evidence

No full build or behavioral suite was rerun for this source-only publication.
Previously recorded evidence belongs to the full private source checkpoint:

- Final source `a01a3ec207326987d13b857f5b11c97f3511c6d4`: clean exported Web
  typecheck/build/source checks exit 0; shared component/reader/delivery tests
  77 passed; configuration checks 46 passed; official SDK API26 compilation and
  resource qualification exit 0.
- Browser suites ran on `f0a37b442eb2ed08c14163f354b9e2ea411c87b4` before a final
  template-formatting change. All 798 compiled resources were byte-identical to
  a01a3ec2, resource-map SHA256
  `a1e0ecfacd2adf7b16db4aa51e9b0e161b1825a6bfb30b00ec38c870cd246156`.
  Do not describe them as rerun on a01a3ec2.
- Adapted Web ESLint: 0 errors, 37 inherited warnings, no new warning instances;
  UI contract: 3 existing warnings. Native build log contains 324 warning lines,
  not 324 distinct defects. Neither build is claimed warning-free.

All ten browser suites below use compiled Web in real Chromium at 390x844,
synthetic HTTP and, where applicable, a synthetic native bridge. Their recorded
exit codes are 0 with no page errors. They do not demonstrate a Harmony emulator,
phone installation, microphone/ASR, production API or server authorization.

| Harness under `client/tests/stage2/` | Actual scope |
| --- | --- |
| `harmony_106_migration_ui.py` | Old unbound endpoint needs configuration/relogin; old token is never sent after binding; no account re-registration. |
| `harmony_r1_upgrade_ui.py` | Same browser origin/profile survives resource upgrade and process restart; revoke/expiry invalidate login. Not signed HAP upgrade. |
| `harmony_service_config_ui.py` | Empty/invalid/valid settings, service switch isolation, distinct download address, logout/revoke; no refresh renewal claim. |
| `harmony_task_lifecycle_ui.py` | Duplicate open, actual timeout, late response, network recovery, return/pagehide, focus/keyboard. |
| `harmony_task_results_ui.py` | Safe plain final text, truncation/unknown, denied/error clearing, late scope/identity replies and unsaved computer choice. |
| `harmony_bot_navigation.py` | Bot creation/settings/back restored; cancel has no POST, double submit one POST, deliberate denial respected. Not successful production creation. |
| `harmony_combined_ui.py` | Pending synthetic upload and task view coexist; task denial does not clear attachment; discard/auth close use original scope. Not real large upload or ready-to-model proof. |
| `harmony_voice_style_ui.py` | Recording gestures, spinner and synthetic bridge/ASR completion/cancel. No device recording. |
| `harmony_delegations_ui.py` | Shared read-only body in HTML dialog, independent pagination, safe text, unsupported/scoped errors, scope cleanup. |
| `harmony_delivery_ui.py` | Read-only host has no action, no mutation on open, duplicate click one delivery POST, accepted is not processed, error/revocation and late reply isolation. |

Old-server unsupported behavior is a fixture, not a production request. Scoped
attachments and absent cancel/continue/followup APIs are not represented by fake
buttons. Background recovery, gallery synchronization and true device large-file
memory behavior remain bounded by separate implementation/OS evidence.

Full development-runtime and production UI acceptance, uploaded screenshots,
signing, real-device upgrade and human QA remain incomplete. Existing browser
screenshots/evidence are private and are not part of this source publication.
