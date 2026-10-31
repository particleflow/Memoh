# Native complex-task launch checkpoint

Base: `eb1a9634c37097455f0eb055d791bc16598ef56d`. The relevant submit, producer-envelope and native CLI files are also unchanged from deployed `391adba82a2347123f02f29fdbd289cde84d26cb`. This is isolated source/transport verification, not a deployment or a replay of a field operation.

## Reproduction and branch

The synthetic request uses `kind=herdr_pi_task`, `work_dir="."`, no resume ID, a 3,435-byte task and one valid artifact (`source_ref` 23 bytes, MIME `text/plain` 10 bytes). It contains no field prompt, device path, credential or field operation identity. A second test keeps the artifact/child-workdir rejection and verifies zero operation rows and zero native calls.

At the fixed base the request fails before the operation claim. The fixture explicitly validates the generated batch identity, producer plan and native producer locators. The generated old launch is 31,901 bytes even with the fixture's short native references; preflight also reserves space for longer native references. It reaches the producer envelope's 30,000-character guard. ASCII and UTF-8 variants reproduce the rejection. This proves an actual source defect for the reported shape; it is not production branch telemetry, and unknown field producer validity must not be relabeled as verified.

The other pre-claim branches remain intact:

- Invalid task/artifact metadata, incompatible child workdir: `ErrInvalid`.
- Target resolution/ownership and current policy: denied, approval or preflight-unavailable errors.
- Invalid batch/layout or producer plan/root binding: unsupported.
- Missing/invalid native producer locator: unsupported (invalid registered nonempty profiles are rejected at target resolution).
- Producer launch above 30,000, or general launch above 48,000: unsupported.

The native CLI's 24,000 UTF-16 argv guard is later than the claim and cannot explain this reported pre-claim failure. Its original encoder is exercised by the new service fixture and its limit is unchanged. The old error's Agenvo wording alone does not establish legacy routing.

## Minimal change

The existing native batch launcher now gzip-packs its generated script. The existing PowerShell/.NET process fully decodes it, checks the expected SHA256, then writes its exact bytes to the configured Bash process's stdin. Bash argv contains only the existing fixed flags. Neither the inflated script nor task text becomes a second process command line. Legacy relay launch bytes are unchanged.

All original limits, target fingerprints, account/Bot/session authorization, admission, exclusive attempt directories, producer identities, receipts and unknown/no-replay behavior remain. No Agent/SDK core, HTTP endpoint, database schema, provider/model configuration, Windows bundle, queue or service is added. Packing is an internal transport representation, not a new user configuration step. Oversized encoded requests can still be rejected; this is not an unlimited-input claim.

PowerShell's error handling is terminating within the launcher. Invalid gzip, partial decoded data and a wrong expected hash are rejected before Bash starts. The controlled peer verifies original UTF-8/CRLF script bytes and stdout/stderr. A generated envelope also closes declared files with their original hashes and invokes the existing finite producer exactly once.

## Checks

Official Go 1.25.7 and repository-pinned golangci-lint 2.10.1 were used. Linux PowerShell 7.4.6 was downloaded from the official release and verified against its official SHA256 listing. Commands, with the isolated PowerShell path supplied through the existing test environment convention:

```sh
MEMOH_TEST_PWSH=<isolated-pwsh> go test ./internal/agenvo ./internal/computertask ./internal/nativeherdr -count=1 -timeout=5m
go vet ./internal/agenvo ./internal/computertask ./internal/nativeherdr
golangci-lint run ./internal/agenvo/... ./internal/computertask/... ./internal/nativeherdr/...
git diff --check
```

The affected default Go packages pass, including the production collector's existing isolated unknown-create/no-repeat test. New tests cover the reported byte/metadata shape, varied input, real native CLI argument encoding, unchanged child-workdir rejection, original producer authority, lost acknowledgement, serialized-ledger service reconstruction, same-operation result recovery and pending NAS delivery without forged references. Legacy compatibility and corrupt-payload rejection are independent assertions. NAS committed delivery is not inferred from a process exit or fixture collector acknowledgement.

The existing Windows native workflow now also runs `TestNativeBatchLauncherWindowsPowerShell` against OS PowerShell 5.1 and installed Git for Windows Bash. It is explicitly skipped on Linux. Linux PowerShell verification does not prove Windows 5.1 execution; that Windows check must pass on the integration head before release.

Evidence: [baseline regression patch](baseline-regression.patch), [expected baseline failure](baseline-red.txt), [affected package results](affected-tests.txt), [commands and exits](checks.json), [product hashes](product-files.sha256).

## Remaining acceptance

No production task was submitted, resumed, canceled or rewritten; no real model or device was contacted. The reported task's precise production branch still needs the field owner's existing non-secret validation. Real Windows execution, NAS committed artifact delivery, original-session attachment return, deployed application UI and human QA are not claimed by this checkpoint. No real UI screenshot was captured; this isolated source checkpoint is not a substitute for the repository's runtime/UI acceptance requirement or ready-to-deploy approval. The original unknown operations remain unchanged.

Interface evidence: [official Herdr v0.9.5 pane CLI](https://github.com/herdrdev/herdr/blob/2ecf19a92fc3a7fff75b6ce3655ef04f4625ec01/src/cli/pane.rs#L1039) accepts the command as argv; no invented stdin flag is used. [Microsoft CreateProcessW](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw) specifies the process command-line bound. This patch retains Memoh's stricter native guard.
