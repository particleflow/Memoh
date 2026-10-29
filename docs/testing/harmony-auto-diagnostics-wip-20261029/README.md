# Automatic diagnostics source checkpoint — not admitted

This isolated checkpoint preserves in-progress implementation from the22910-byte owner handoff SHA2562c32a5b72f554964cb1bb1a9a02df47fba7987947a40460ec41d2f90bc673df1. It is deliberately excluded from the next P1 client package. No new HAP, signing, deployment or field data capture is claimed.

The actual native two-file journal and frame/ready tests pass with real UTF8 files, tornPUT/ACK, unknown fsync, original rotation and scoped ACK. The first native SDK26 compile passed; a subsequent canonical-body validation addition has not been recompiled. Actual SDK tests initially passed four identity/dispatch cases but two upload/resume cases failed on mixed Node/DOM multipart platforms. Test platform adaptation and full combination verification are unfinished. Initial generated typecheck had two optional-response errors, now changed in source but not yet reverified. Keep this source as WIP; do not treat it as a release-ready diagnostic uploader.

A fixed-reference API-client snapshot extends the existing source composition; it retains the existing401/generation handling. The native permission string is unchanged, no raw journal/freeform details/addresses/token fields are exported, and no anonymous public-page upload bridge is provided. Existing public-page phone timing remains uncollected.

User-facing delivery priority is the separately admitted b5e7aff six-file P1 correction; preserve this WIP on its own branch before switching back to the original delivery branch.
