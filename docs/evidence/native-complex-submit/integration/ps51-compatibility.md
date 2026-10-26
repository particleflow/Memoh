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
