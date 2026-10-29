# Harmony v12 source recovery checkpoint

This checkpoint preserves the recovered, tested source composition before the original executor became unavailable. It pairs shared `959efb3aab3f7165b1dd0e9e7f00dac27526688e` (tree `369e6da77391a139f33ad557f5616a30e4409fc0`) with the existing Harmony branch. It is not a new HAP or a deployed client.

The source map covers 127 shared files: 113 exact upstream files and 14 native adaptations. Two unchanged shared journal dependencies and two dependency metadata files close the generated context. The canonical SDK retains 379 operations and adds three failure-report operations. Four retired phone-only Agenvo aliases are absent from the canonical SDK; the server tests explicitly return 404 for those endpoints. The obsolete generated connection component is removed without modifying stored operations or credentials.

The fixed paired snapshot runs after the existing guarded phone adaptations and checks both old and new bytes. It retains the existing 47 adapter records. Native first-error reporting composes the typed API26-compatible envelope with the journal publication fix and the correct dual photo mode. Original selection preserves bytes; non-original selection retains the existing compression policy and cleans only handed-off temporary output.

Previously completed validation is preserved here without rerunning it: full generated Web typecheck, the focused metadata outbox suite, two actual generated-SDK/old-server seams, two workspace recovery seams, and the source inventory. Initial type and harness failures remain alongside the final successful logs. Skipped tests are not counted as passes. The exact native hashes match the previously successful official SDK26.0.0.105/API26 compatibility receipt at `5f674c8a3e72681bfc6bfd34574b53cd21410446`; no new native compilation or HAP build is claimed.

These are controlled source/component tests, not a live Server, device process restart, phone URI, TLS, performance or physical-phone acceptance. No schema180 deployment, model call, signing, version bump or publication occurred. Streaming and history-menu cleanup were not included in this v12 checkpoint; the admitted v13 increment must be composed separately.

The original executor resumed on 2026-10-29 with all fourteen pending source files readable. This commit saves them before further v13 work. Evidence digests are in `evidence-files.json`; absolute paths in original logs identify this cloud executor only. The existing HAP1.0.28 identity remains unchanged.
