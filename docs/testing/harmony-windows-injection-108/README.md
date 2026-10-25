# Harmony 1.0.8 Windows configuration handoff

Built once from `ee00ff76a07ac27e08f4b4f58fd1354f0ecbeef3`, using the retained
official API26 / SDK 26.0.0.105 toolchain and the existing verified LibRaw inputs.
The Web/SDK pin remains `68be06092a1ea85d961115aac404082982bdecf4`.

- [Native compile and integrity](native-build-summary.json): both exit codes zero,
  rawfile digests verified, native ABC and both ELF architectures checked.
- [Public package](package-verification.json): 1.0.8 / 1000008, unchanged bundle
  and permissions, valid CRCs, empty 8192-byte configuration slot. All decompressed
  entries checked for listed credential patterns; no readable URL added compared
  with the previously reviewed 775b package. This is a scoped scan, not proof of
  detecting every possible encoding or secret.
- [Actual HAP injection](real-hap-injection-test.json): synthetic configuration
  modifies exactly one reserved resource, preserving every other payload and
  all offsets. This test derivative remains private and is not the public HAP.
- [Compiled UI](compiled-browser-result.json): actual output copied into an isolated
  local HTTP fixture, intercepted synthetic API, Chromium 390x844. Automatically
  retained login/drafts/pins with verified binding, refused mismatches and failed
  writes without requests/data loss, preserved persisted config across restarts,
  and opened fresh-install login without manual address entry. Zero page errors.
  Screenshots: [retained session](compiled-login-retained.png),
  [mismatch retained](compiled-mismatch-retained.png), inspected by the agent.

[Windows procedure](../harmony-service-migration/README.md) and
[generic unsigned artifact](../../../artifacts/harmony/1.0.8-ee00ff76a/README.md).
The exact old-package binding is held by the original Windows task, not cloud.
These checks are not Windows execution, official signing, native WebView/phone
runtime, actual 1.0.6/1.0.7 overwrite-install, live NAS authentication or human QA.
Keep the same app identity and original persistent storage during device acceptance.
