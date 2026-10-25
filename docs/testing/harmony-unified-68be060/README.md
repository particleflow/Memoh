# Harmony shared 68be060 source and package alignment

Shared commit: `68be06092a1ea85d961115aac404082982bdecf4`.
Shared tree: `9697857688118f86bce672cc40977686e483b536`.
Recovery base: `eea13736690b31c84e2e29c8e757ae52c039dfdd` on the existing
`recovery/harmony-source-20261023` branch.

## Source verification

All 5,926 files and modes in the public main archive match the fetched Git tree.
The existing generator reproduced the combined source archive from verified
public cached inputs. UI revision `f69402a55dd87a59c10722fe11507ad8fd7048f5`,
package resolutions and workspace configuration remain unchanged.
[Source verification](source-verification.json).

Exactly ten Web/SDK paths differ from 1502199: three translations, the shared
root-run component and history composable, two existing task components, their
two test files, and the SDK type file. The SDK adds optional native run identity,
state and timestamp fields; no alternate endpoint or phone page is introduced.
The full source pin supplies them directly. The 43 existing phone adaptations,
native login storage, attachments, app identity and permissions remain unchanged.
[Generated context comparison](context-comparison.json).

The expert root run is separate from child Agent/computer tasks. Its state is a
persisted native snapshot, not a live queue or delivery acknowledgment. History
navigation re-reads the scoped delegation and validates the target bot, root
session and RunID before opening the existing chat panel. Permission failures,
changed relations, logout and late responses cannot authorize navigation.
An older server omitting optional run fields has an explicit unavailable state.
The prior parent reconnect and login response-ownership fixes remain upstream.

## Executed source checks

- 140 tests in seven files passed: root-run/history permissions, task/delegation
  readers, attachment display, API ownership, parent reconnect and native
  continuation. [Log](targeted-tests.txt).
- Complete phone Web typecheck and targeted six-file ESLint passed.
  [Typecheck](typecheck.txt), [ESLint](eslint.txt).
- The source manifest validates 125 entries and 134 public files. The historical
  19 later cloud edits remain unidentified; no unavailable 5a7 source is claimed.

## Running phone UI

The existing Vite application at `http://127.0.0.1:4181` was reused. All 1,456
Web/package non-test input files match the current generated phone context.
The first listener probe lacked loopback network permission; the supported
network-enabled check confirmed the original server remained active. A strict
port startup attempt exited without replacing it.
[Runtime source comparison](runtime-source-equivalence.json).

At 390x844, the real phone-adapted Vue UI shows the expert root RunID/time/state
separately from a running child and pending delivery; opens the authorized
expert history; and retains login across navigation and reload. Committed
attachment bytes are hash-verified under the actual expert Bot, image preview
returns to the task, and saving uses the retained `download.fetch/save/abort`
bridge. Its OS response is simulated, not a native file-picker acceptance.
There are zero observation/continuation mutations and zero application page
errors. A transient old-session read during bot switching receives 404 and no
content, before the correct target history is displayed.
[Browser result](specialist-browser-result.json).

Inspected screenshots: [root run](expert-root-run-390.png),
[child task](child-run-distinct-390.png), [attachment preview](attachment-preview-390.png),
[expert history](expert-history-390.png).
The HTTP and WebSocket fixtures are synthetic and isolated from production.
Initial harness failures and final cleanup warnings are retained with hashes
and explanations in [the correction record](browser-harness-corrections.json).
No product implementation was changed to make those fixture assumptions pass.

## Completed package and boundary

The new unsigned package was subsequently built from source
`775b494db1b888f63140afba5030a9177eb2400f` using the retained official SDK.
[Completed compile, integrity checks, compiled UI and private readback](../harmony-775b494d-hap/README.md).
The old `f2488597` / shared 5e55 HAP is preserved unchanged.
No public binary upload, signing, production change, real model execution or
Windows operation is authorized or performed by this source update.
Device runtime, phone/NAS transfers and human acceptance remain unverified.
