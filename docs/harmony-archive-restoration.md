# Verified archive source restoration

Starting from `b568d81a0287945ebb39bfa031761c1e87ecf6a2`, both user-selected
source ZIPs were materialized into the same Memoh cloud workspace through the
supported Library flow. The previous transfer blocker is resolved for these
two files. Original ZIPs, extracted copies, hashes and prior checkpoints remain
private and preserved; neither archive nor its private handoff reports are
published here.

| Archive | Bytes | SHA-256 | Verified contents |
| --- | ---: | --- | --- |
| Original sanitized source | 168253 | `0247ad142675598684bb2734de6376a1e793982da6cc2e160d983f6cbaa9d0ac` | 80 files; all CRCs and 79 SHA entries pass |
| 5900 alignment checkpoint | 317555 | `152a8c433e21cdec499c23195cc090015a7cc8612fa619c8a940491f14bf85b2` | 90 files including 79 source files; all CRCs, 89 SHA entries and 13 before/after change records pass |

Both ZIPs contain all 72 paths previously excluded from the public source.
The checkpoint source content is restored with the repository's LF line endings
(and one archived trailing empty line removed); original archive bytes remain
preserved and separately hashed. Of the 72 paths, 66 match the original
ZIP and six differ: the Web builder, packager, `harmony-files.ts`, voice text
test, API26 build wrapper and toolchain entry. The five overlapping public
inputs retain their current `b568d81` versions, including configuration locks
and newer URL validation. No older ZIP overwrites those public changes.

Two additional archive tests, `memoh_upload_preparation_checks.mjs` and
`native_voice_result_checks.cjs`, were absent from the old omission register and
are also restored. All 79 checkpoint source paths are now accounted for: 74
restored files and five retained public inputs.

The repository now contains 116 reviewed source entries and 125 files under
`mobile`, with all 72 historical excluded paths present. This is a verified
combination of available inputs, not a claim to reproduce the inaccessible
127-file snapshot or its Git SHA. The reported 19 later cloud-modified files
still lack a recoverable per-file mapping/content comparison. Their identity
and changes remain unverified; no guessed reconstruction is recorded as source
recovery. Full per-file hashes and overlap decisions are in
[coverage.json](testing/harmony-archive-restoration/coverage.json).

The clean project source is published under the user's explicit authorization;
existing project, Lucide and WaveSurfer license notices are preserved. No private
configuration, endpoint, credential, signing material, official SDK/CLT binary,
generated raw source snapshot or old HAP is included.

## Build state at this source checkpoint

The existing overlay builder, native shell, native file helper, packaging and
toolchain scripts are now readable. They are older 5900 implementations; mere
path recovery does not establish compatibility with the retained newer modules.
For example, the old native router has no current `download.fetch` or runtime
`client.configure` dispatch, and the old packager does not generate the newer
`ServiceUrl.ets` dependency. These require explicit integration against actual
source, not an assumption that the lost later edits have returned.

The current unified target is
`40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`. Build alignment continues from this
durable restoration checkpoint. No full Web or HAP compilation, current UI
acceptance or phone/NAS transfer is claimed by the archive recovery itself.
