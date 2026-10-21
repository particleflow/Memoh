# Additional native stderr probe (unexecuted)

[windows-stderr-vector.json](windows-stderr-vector.json) is an independent fifth
vector generated from the same actual `powershellCommand` function as the first
four. Its source is commit `a671be3ba4797323bc1ab6fa57b1b5e1d3061ce9`; `exec.go`
is unchanged from the review snapshot `cc29415989cf6f6be3f0a89cd878ab490c0f04ad`.
No production behavior was changed to generate this artifact.

The body requested for this probe is:

```powershell
& $env:ComSpec /d /c 'echo MEMOH_STDERR 1>&2 & exit /b 0'; Write-Output 'MEMOH_AFTER'
```

Use the JSON's complete **`pane_run_command`**, not this unwrapped body. Submit
it once through the existing authorized connection in the same continuously
observed acceptance pane. Do not repeat any prior vector. Follow the
[original read/parse and uncertain-lifecycle rules](windows-acceptance.md).
If a pane's lifecycle became uncertain, stop and report that state. Do not
close or terminate anything.

Expected: decoded merged output contains `MEMOH_STDERR` and then `MEMOH_AFTER`,
`exit_code: 0`, `truncated: false`. Preserve raw output: `cmd.exe echo` can leave
trailing whitespace, so do not require byte equality with the illustrative
synthetic frame. Never inject that example frame into the terminal.

The **suspected, not yet real-Windows-confirmed** defect is that Windows
PowerShell 5.1 can represent redirected native stderr as `NativeCommandError`.
The current wrapper's `$ErrorActionPreference = 'Stop'`, combined with the
scriptblock's `2>&1`, could turn that diagnostic into a terminating error. Its
`catch` would then overwrite the command's actual zero exit code with 1 and
skip `MEMOH_AFTER`. Capture the real decoded frame even if it demonstrates
that failure; do not rerun the command or switch to PowerShell 7 to obtain a
passing result.

The cloud generator verified source/command hashes, echo-only non-completion,
and parsing of the **synthetic expected frame**. It did not execute Windows or
confirm the suspected defect. Return the actual exit code, both marker-presence
checks, the decoded diagnostic type/text if present, and submission count.
