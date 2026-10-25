# Full production pin and shared auth handoff

The original recovery branch remains `recovery/harmony-source-20261023`.
The verified delayed-401 repair `e26d56b8b544231bb9e9d7990656307274b41337`
was pushed through normal Git. The follow-up handoff commit
`787780fbe3c1a91262d99a5a3b8820d531406c20` was also pushed; both remote heads
were checked with `git ls-remote` before this alignment.

The full production source `5e55c45c85659f04b339b4df42aeff72966542a8` was
successfully fetched through the existing Git remote. Its tree is
`4ebeb57041ba5bb58b7400f372cb4c8ac1f28a61`. The earlier short-ID lookup failure
is resolved; no claim of equality is based on the short label.

## Exact comparison and minimal changes

Normal Git comparison from the prior `51b19cb` pin found ten changed paths:
Provider `models-panel.vue`, its test, and eight integration evidence files.
The only product change is the three-line Provider mount refresh at
`6c3de5e9ea1b2fcba07cc82fcf8879045a764f67`, already byte-identical in the phone
adapter. The source lock and notice source reference now identify full `5e55`;
the duplicate Provider adapter is removed (45 to 44 host patches).

All 5,851 main archive file objects and modes were compared to the fetched Git
tree. The UI revision and frozen dependencies are unchanged. The existing
source generator reproduced the combined archive using verified cached public
inputs, including the retained UI archive; no SDK was downloaded. The ordinary
builder's guarded context generation and dependency hash check passed.
See [archive verification](5e55-archive-verification.json).

The new 6,304-file context has 6,295 unchanged normalized files. Only one test
and eight evidence files differ. All 1,454 Web/SDK non-test files (including
configuration and adjacent documentation) equal the already-verified e26 phone
context and the existing Vite source checkout. Nineteen pre-existing long-path
PAX entries outside Web/SDK retain the archive prefix; only that prefix was
normalized for comparison, not their content. The builder is not rewritten.
See [complete comparison](5e55-generated-equivalence.json).

The unchanged production bytes retain login persistence, Bot creation/settings,
model selection, downstream observation/continuation and original attachment
save/open adapters. No additional confirmed functional difference from full
5e55 was found. This establishes shared source alignment with the documented
phone adaptations; it does not establish recovery of the missing historical
Git snapshot or deployment to a phone.

## Checks executed this follow-up

- Updated upstream Provider panel suite: **14 tests passed**, including fresh
  sanitized-cache mount with success, failed GET and missing-key outcomes.
  [Log, workspace path and trailing blank line normalized](5e55-provider-panel-tests.txt).
- Existing source-manifest check: 125 entries / 134 public files, zero held
  source paths, historical later edits still 19 unverified.
  [Result](5e55-source-integrity.json).
- Reused the current Vite at `http://127.0.0.1:4181` without a second server.
  Real 390x844 Chinese UI/store with intercepted synthetic HTTP: anonymous
  late 401 preserves a new login; a prior-session 401 with identical token
  bytes is ignored; reload preserves login; a current-session 401 returns to
  login. One login HTTP request and no page errors.
  [Result](5e55-result.json), [retained login](5e55-login-retained.png),
  [current 401 returns to login](5e55-current-401-login.png).
  Both fresh screenshots were inspected. Same-token login uses the real
  store method, not a second server authentication.
- The earlier e26 146-test/typecheck results remain earlier results. They were
  not rerun or relabeled for this pin; production/dependency byte equality is
  the evidence that no additional type/build inputs changed.

This is source and cloud-browser validation, not live API/business acceptance,
Human QA, an on-device upgrade, signed HAP or a phone/NAS transfer test. No
Web production build, native compile, signing, release, SDK download or HAP
upload was performed. The retained unsigned HAP remains the original f6403de
artifact and contains none of the subsequent source changes.

## Shared Web fix transfer

[Handoff](upstream-handoff.md) includes the two-file
[patch against full 5e55](upstream-5e55-auth-session.patch). It has been applied
cleanly to the exact upstream files and compared with the verified phone
implementation. Shared Web must include the auth-session generation helper,
which predates e26 on the phone; copying only the API hunk misses that dependency.
The parent/shared task owns its separate integration; no shared branch was
modified from this task.

## Additional historical-source evidence

The retained checkpoint's `changes.json` and `client-alignment.patch` identify
13 earlier changes. Every recorded before hash equals the original ZIP file
(or correctly records an added file); every after hash equals the checkpoint.
The manifest targets `5900e4c1fd5b2bf43c470eb2b29ad090a67e7dfa`, with original
local head `37b0eb469423f158344203faad6fdb1eab763841`. The 13 path/hash pairs are
in [the verified record](5e55-old-checkpoint-change-records.json); no private
configuration values are reproduced.

These are the older 5900 alignment edits, not a mapping of the subsequent
historical 19. Local recovery records, source-manifest Git history and relevant
filename searches in the existing workspace/tmp did not find
`INHERITED-SOURCES.md`, the interrupted upload payload, or another provenance
manifest. Local Git still has no `5a7d28ff7624a5eb1f59804f7fb432369b8e951a`
object. Personal Context was unavailable on the preceding audit and was not
blindly retried. No unknown historical write was retried.

Thus the 72 formerly excluded paths remain physically restored, with the
29 exact / 37 text-equivalent / 6 recovery-modified breakdown at the documented
bf44 audit base. The additional 13-record verification does not reduce the
**19 unidentified later edits**. They remain unaccepted individually, and no
code has been guessed to manufacture historical equality.
