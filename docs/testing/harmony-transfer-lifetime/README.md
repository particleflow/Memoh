# Harmony transfer ownership: stage 1

Base: `a158c56488ed193ec3e9f2d57480ad4f16a58068`.
Branch: `fix/harmony-transfer-lifetime`.
This independent source increment is for later coordinated combination. It does
not modify another working branch, publish a HAP or change a deployed product.

Closing/rebinding SharedChat used to cancel its page-owned native engines and
clear their configured origin. Component unmount and every document bootstrap
also sent cancellation. Accepted uploads/downloads now belong to one process-local
owner of the existing engines. Page instances own observers, previews and voice.
The owner joins the same active upload/download promise, retains the existing
single-engine admission rules and rejects late page/account replies. Account
revocation stops I/O; a stale credential-scoped cleanup cannot stop a newer login.

The actual aboutToDisappear, renderExited and attachNative method bodies are
exercised by the small native lifetime test. It executes the production bridge,
upload/download engines, filesystem/crypto/network code with Node Kit adapters,
real temporary files and controlled loopback HTTP. Its logical fixture authority
is nas.example; the test-only RCP adapter supplies the loopback transport. This
is not real Harmony RCP routing or TLS acceptance.

Only a complete, staged original accepted by upload.run can outlive the view.
An incomplete bridge File import is stopped and cannot publish a successful
journal. A finished transfer does not submit a new chat turn. A reattached
original still uses its existing receipt/HEAD checks; download saving rechecks
the original scoped endpoint and requires a current view before its picker.
Ordinary upload TTL and task lease behavior are untouched.

The composer fix is applied AFTER the pinned paired shared snapshot. Its exact
input SHA and replacement counts remain checked. Applying the fix earlier would
be rejected by the paired guard or overwritten. The current complete context has
47 reviewed host patches; the final composer SHA is recorded in result.json.

## Executed checks

`result.json` lists the exact commands, exit codes, source hashes and raw logs.
Two regressions fail against the exact base source: native view teardown cancels
an accepted PATCH, and normal bootstrap sends destructive client.configuration.
The same tests pass with this increment. Ten focused Node checks pass, including
1 MiB existing upload-scope/ready-HEAD and download regressions; no large-file
matrix is represented as newly executed.

The complete locked public Memoh/UI source context was reconstructed with its
existing archive hashes. The generated Web passed vue-tsc, a production Vite
build and scoped ESLint (0 errors, 53 warnings in unchanged templates). Reused
local dependencies were used first; a missing pinned wavesurfer.js 8.0.2 archive
was retrieved by frozen-lockfile pnpm. No Harmony SDK was downloaded.

Example focused reproduction from repository root, using an existing compiler:

```sh
export HARMONY_TEST_TYPESCRIPT=/path/to/existing/node_modules/typescript
node mobile/client/tests/stage2/harmony_native_lifetime.cjs
node mobile/client/tests/stage2/harmony_entry_lifetime.cjs
HARMONY_LIFETIME_SOURCE_REF=a158c56488ed193ec3e9f2d57480ad4f16a58068 node mobile/client/tests/stage2/harmony_native_lifetime.cjs
```

The last command is expected to fail. The bootstrap test accepts the same source
ref for its independent red check. Existing router tests now load the real owner
with their existing engine doubles. Their frame, input, account and endpoint
assertions remain; the old close-clears-origin assertion is replaced with the
new required detach-preserves-origin assertion.

## Remaining gates

Official API26 compilation was not executed. This environment has no existing
26.0.0.105 SDK/hvigor toolchain. After the coordinated source is packaged through
the existing Web/materials pipeline, reuse the installed toolchain via:

```sh
cd mobile/client
python3 -B mobile/harmony/tools/build_api26.py --entry hvigor --toolchain-root /path/to/existing/toolchain
```

Do not download another SDK to treat this gap as closed. Node transpilation and
Web type checking are not the official ArkTS check. No device/ArkWeb lifecycle,
background/lockscreen, rendered application screenshots, human QA, new HAP,
installation or deployment passed here. These remain unverified.

DATA_TRANSFER, permissions, notifications and Windows host changes are outside
this source increment. The existing ordinary receipt expiry and unknown
Create/Complete protections remain. Process death still requires reauthentication
and existing journal recovery; this change does not promise execution while the
process is absent. It does not start subsequent unaccepted items after a view's
submission observer is canceled.
