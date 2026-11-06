# Host binding integration checkpoint

Base `3d5f145c452dda5e17861d401de049b7ddded2e6`; source `695269a7b2a5f271dd0c61f3bc169831fcd369fa` from the existing fix/native-file-frame-snapshots branch; clean cherry-pick with origin retained: `2702147a2378282193d110ffb0734bcbebd6f7dc`.

The six changed product files were byte-identical between source prerequisite2782ee06 and the integration base before applying this incremental commit. No earlier source history was merged wholesale. The integrated delta has no Web/Harmony/UI, API/SDK/Swagger, workflow or schema migration changes. It preserves the existing literal request-field correction and official Agent runtime/contextview boundary. The concurrent queue-send/system-theme UI work is not included or overwritten.

## What this changes

Decision answers/approvals re-enter the original workspace preparation using their resolved identity, session, run and selected target. Explicit accepted-answer recovery provides its actual admission RunID before preparation; preparation failure is persisted as a failed outcome, not completed. Federation tools retain the host-owned RunContext and actual tool call ID through callbacks. Existing toolcontext.Bind keeps both callback and owning-run cancellation effective. Computer binding guards still reject public headers, missing authority, revoked context and foreign sessions; model arguments cannot create authority.

Binding diagnostics contain only correlation IDs and boolean presence/match facts, not command/input/path/target/user values, credentials or terminal contents. No task is replayed or new workspace created by this integration validation.

## Differential validation

A directed race selection passed 33 test/subtest events (5 top-level) across application, federation tool, computer-task and toolcontext packages; zero failures and skips. See [race-summary.json](race-summary.json). These tests include message/answer/approval tool assembly, unsafe identity/permission/target/session rejection, callback-without-values, callback/run cancellation and sanitized binding diagnostics. The federation test stops at a nil task service after the guard and cannot dispatch a business task. Application provider probes use a synthetic SDK provider and assert zero network calls.

```sh
go test -race -json -p=2   ./internal/agent/application ./internal/agent/tool ./internal/computertask ./internal/toolcontext   -run '^(TestNativeDecisionComputerBinding|TestNativeDecisionComputerBindingRejectsUnsafeContext|TestFederationRetainsOwningComputerContext|TestComputerBindingDiagnosticsDoNotInventConnectivityOrLeakInput|TestBindUsesRunContextAsValueAuthority|TestResolveComputerBinding)'   -count=1 -timeout=180s
```

The command records actual events, not an assumed count from its regex. The TestResolveComputerBinding alternative matches no tests in this tree; the existing authority test that ran is TestBindUsesRunContextAsValueAuthority. No zero-match family is claimed as additional coverage. Official core boundary check passed. No UI build, HAP build or old matrix was repeated because their source files did not change.

Reuse, rather than re-label, source-owner evidence: 47 directed race events including a real synthetic PostgreSQL recovery admission; lint/application/computer-task checks passed. The source affected-package aggregate did NOT fully pass: seven existing image-download tool tests failed at configured proxy/SSRF validation. They were not repeated in this differential selection, no proxy/guard/test was changed, and their origin is not proven here. This checkpoint does not claim a fully green aggregate; its exact remote full CI is still required.

## Field evidence and remaining acceptance

The field owner has read-only evidence that session/Bot/creator and saved user/channel identities are present and mutually consistent; native target and absent workdir are legitimate. Do not misdiagnose missing user configuration. No historical callback-context presence/match facts were captured, so database identity consistency cannot prove that the observed field failure came from federation callback context loss. The controlled callback reproduction yields the same missing-binding sentinel, but is not a confirmed production root cause or a complete repair claim.

Future field validation must use the same original conversation and existing authorization, preserving unknown operations without resubmitting them. No real model/image/device operation, deployment or data migration was performed. The existing task panel's actual read failure still lacks an HTTP status and is not labeled503. Field browser/control recovery and memory corrections were not performed by this task and are not claimed complete. Windows whole-package/data layout remains independent and unfinished.

No source-owner private path was accessed or assumed to exist in this workspace. Review used the formal Git commit and its committed evidence; private handoff logs remain with the source owner.
