# Pi on Windows: private DPAPI credential command

Preparation checkpoint, not an installed integration. No Memoh protocol, image,
server or Pi source change is needed. This uses Windows PowerShell 5.1 and its
built-in DPAPI commands; there is no new service, module or package dependency.
The operator must already have approval to create/use the dedicated gateway key.
This directory contains no real key, endpoint, user path or machine identifier.

## Private deployment layout

Use one dedicated **absolute** `PI_CODING_AGENT_DIR`, outside business workspaces
and public checkouts. Copy these fixed artifacts there; do not edit source on the
Windows machine. The files within that root use relative references:

```text
<private-agent-root>/
  read-dpapi.ps1
  models.json          # models.json.example; replace only private deployment values
  settings.json        # settings.json.example
  auth.json            # {} in this dedicated directory; no stored Magpie key
  private/
    magpie.dpapi       # encrypted text; never a plaintext key
```

The existing approved provisioning flow must create `private/magpie.dpapi` using
Windows `ConvertFrom-SecureString` **without `-Key` or `-SecureKey`**, as the same
Windows user that will run Pi. Store its encrypted text in UTF-8/ASCII. This is
CurrentUser DPAPI, not CLIXML, AES-with-a-shared-key, or a raw Base64 DPAPI blob.
The reader does not create, migrate, discover, refresh or recover credentials.
Keep the root and ciphertext private with the existing approved Windows ACLs.
DPAPI does not isolate agents running under that same Windows account.

Replace the `.invalid` endpoint in `models.json` with the approved private
Magpie OpenAI-compatible base URL. `baseUrl` is a literal deployment setting,
not an assumed environment interpolation. Do not put the key into that file,
`auth.json`, an environment variable, an argument, a prompt or a transcript.
No extra compatibility flags or model limits are invented here.

Use Herdr `tab.create.env` to supply the non-secret absolute
`PI_CODING_AGENT_DIR`. Start Pi in that tab using the existing
`agent.start(kind=pi)` and explicit arguments:

```text
--provider magpie --model codex/gpt-6.1-sol
```

The settings file supplies the same defaults. Do not pass `--api-key`, reuse a
root with an existing Magpie credential, or load an extension that overrides
this provider. Stored/runtime credentials have higher precedence than the
configured command. This template creates no Bot grants and does not implement
Herdr agent scheduling or monitoring.

## Exact command contract

The leading `!` in `models.json` selects Pi's native **command** mode. It launches
`powershell.exe -NoLogo -NoProfile -NonInteractive -File .../read-dpapi.ps1`.
`${PI_CODING_AGENT_DIR}` inside that command is expanded by the command shell;
it contains only the private configuration root, never the key. A value such as
`"$SOME_VARIABLE"` would instead be Pi's environment-reference mode: placing a
`!command` string in that variable does **not** turn it into command execution.

Pi 1.2.0's Windows credential resolver calls `getShellConfig()` without a
`settings.shellPath` argument: it checks standard Git Bash locations, then PATH.
Confirm that this resolves the already verified **Windows Git Bash**, not a WSL
launcher. If necessary, prepend that verified Git Bash directory in this tab's
process-local PATH, retaining the system PowerShell path. Do not change global
PATH or install a shell. `settings.shellPath` alone does not configure credential
command discovery. This Bash-form command does not promise a cmd/WSL fallback.
Respect the existing PowerShell execution policy; the template does not use
`-ExecutionPolicy Bypass` or change policy. A blocked script is a deployment
failure to report, not a reason to reinterpret credentials.

The fixed reader resolves `private/magpie.dpapi` from its own script root,
independently of the business cwd. It prints exactly one nonempty printable
ASCII bearer token to stdout on success, with no newline. Invalid platform,
missing/corrupt/undecryptable file or invalid token returns nonzero, empty stdout
and a constant stderr message. It never prints exceptions, paths or ciphertext.
The BSTR is cleared; no claim is made that every managed-memory copy is erased.
Pi necessarily holds the key in process memory and sends it to the configured
gateway. Do not run the reader directly in an observed terminal with a real key.

`models.json` commands run at request time. Pi resolves the result or throws;
`authHeader: true` additionally requires a resolved key. There is no alternate
provider in this template; keep provider/model explicit. This differs from an
`auth.json` command, whose output/failure is cached for the process lifetime.

## Verification

Cloud checks used the unmodified published `@earendil-works/pi-coding-agent`
**1.2.0** CLI (npm integrity verified), a loopback HTTP fixture and a synthetic
`powershell.exe` subprocess. No production gateway or credential was contacted.
The real template command, a configuration path containing spaces and a business
cwd outside that root were exercised. Valid stdout authenticated only the
explicit Magpie model. Nonzero exit (even with stdout) and empty stdout produced
zero model requests and no fallback to a configured unrelated provider. Key and
subprocess diagnostic sentinels did not appear in Pi stdout/stderr.

```sh
python contrib/pi-windows/test-contract.py /path/to/pi/dist/bundle/cli.js
```

**Windows DPAPI and PowerShell 5.1 execution have not been run in this Linux
cloud environment.** The authorized runtime side can run the frozen synthetic
`test-dpapi.ps1` under Windows PowerShell 5.1. It encrypts only a public synthetic
value in an owned temporary directory, tests success/missing/corrupt/empty data and a whitespace token,
captures child stdout privately, reports assertions only and cleans that test
directory. It does not touch the deployed ciphertext. Cross-user decryption and
actual Pi → Magpie authentication remain separate runtime acceptance checks.
Do not create another Windows account just for this test.

## Verified upstream contracts

- [Pi 1.2.0 provider documentation](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/docs/providers.md)
- [Pi 1.2.0 models and credential precedence](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/docs/models.md)
- [Command resolution, timeout and failure](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/src/core/resolve-config-value.ts)
- [Provider authentication composition](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/src/core/provider-composer.ts)
- [Windows credential shell discovery](https://github.com/earendil-works/pi/blob/v1.2.0/packages/coding-agent/src/utils/shell.ts)
- [Microsoft ConvertFrom-SecureString](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.security/convertfrom-securestring)
- [Microsoft ConvertTo-SecureString implementation contract](https://learn.microsoft.com/en-us/dotnet/api/microsoft.powershell.commands.converttosecurestringcommand)
