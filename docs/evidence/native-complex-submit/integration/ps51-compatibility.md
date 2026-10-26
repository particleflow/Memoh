# Windows PowerShell5.1 compatibility correction

The first unified head9dbf1bc75601d50dfce74e4e08184f967ca0db5d actually ran the
new named Windows test. [Native run40335985321](https://github.com/particleflow/Memoh/actions/runs/40335985321),
job121617564671, failed at14:05:20UTC on2026-10-26 with `StandardInputEncoding`
property assignment unavailable. The prior native CLI tests passed, including
large stderr and nonzero exit. The new batch failed before Bash started, with
stdout0 versus3465 expected bytes. Later transfer/PG/bundle steps did not run;
this head is not releaseable. The adjacent excerpt preserves this failure.

Only the unnecessary StandardInputEncoding assignment is removed. The launcher
already writes the verified raw byte array directly to StandardInput.BaseStream;
it now closes that same raw stream, rather than flushing a text StreamWriter.
No text encoding property is needed, and no encoding conversion, BOM/newline,
relaxed hash check or changed payload/permission/length bound is introduced.
[Microsoft's existing StandardInput API](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.process.standardinput?view=netframework-4.8.1)
documents the original redirected input/UseShellExecute boundary retained here.

The same Windows test and all original assertions remain. Local Linux complex
preflight, exact packing and same-operation unknown reconstruction passed after
the correction; Windows-only/optional local PowerShell tests still skip locally.
This is not claimed as PS5.1 success before the new exact-head Windows CI result.
No Windows host, production device, deployment or paid model is involved.

## Second observed failure and minimal BOM correction

Head14958cbef9224068cfcf89235ee66a74399e5832 actually executed the script and
returned exactly3465 expected stdout bytes, but [Native40337021505](https://github.com/particleflow/Memoh/actions/runs/40337021505),
job121621225185, correctly failed the unchanged stderr assertion at14:11:23UTC.
The first Bash line had an injected UTF-8 BOM before its comment; no assertion
was removed and this head is also not releaseable.

[Microsoft's .NET Framework source](https://github.com/microsoft/referencesource/blob/main/System/services/monitoring/system/diagnosticts/Process.cs#L2009)
initializes the redirected input writer from Console.InputEncoding and enables
AutoFlush during Process.Start. That can emit a preamble before any raw write.
The correction therefore uses the same UTF-8 codepage with a BOM-free encoder
only around Start, and restores the original encoding in finally immediately
afterward. Other codepages retain their existing behavior. It does not strip or
alter script bytes, change the codepage, add another shell/helper/transport,
disable validation, or persist console configuration.

The Windows test retains exact stdout and empty-stderr assertions and now also
checks that the original console codepage and preamble are restored. Windows
PS5.1 remains the decisive required test; Linux source/preflight passes alone
are not called Windows success. Both original Windows failures are preserved.
