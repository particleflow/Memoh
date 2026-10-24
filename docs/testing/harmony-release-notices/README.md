# Harmony release notice preparation

This follow-up changes notice resources and their build-input binding only.
It does not change client behavior, add a dependency, rebuild/sign a HAP, publish
a binary, create a tag/release, or use the Library handoff. The last compiled
product source remains `f6403deaded6e76c0788aa66cffcaac5fef0badd` until a later
authorized build records its own source commit and output hash.

## Exact UI provenance and remaining question

- The user's repository is `particleflow/Memoh`, a fork of `felinics/Memoh`.
  `.gitmodules` points `packages/ui` to `https://github.com/felinics/ui.git`.
- The pinned UI revision is `f69402a55dd87a59c10722fe11507ad8fd7048f5`.
  The original repository is owned by `felinics` and is not reported as a fork.
  It is not established as user-authored code merely by its presence in this fork.
- The [pinned README](https://github.com/felinics/ui/blob/f69402a55dd87a59c10722fe11507ad8fd7048f5/README.md)
  expressly describes consumption as a host submodule. The
  [pinned package](https://github.com/felinics/ui/blob/f69402a55dd87a59c10722fe11507ad8fd7048f5/package.json)
  sets public publishing but has no license field; its root tree has no LICENSE.
- The [upstream Memoh release workflow](https://github.com/felinics/Memoh/blob/main/.github/workflows/release.yml)
  explicitly includes the selected UI sources in the unified source archives
  and publishes `@felinic/ui` with the Memoh version. The exact workflow blob
  inspected is `c81ef3a76c3240b182ab50029f01fafcfdacd995`, also present in the
  fixed shared source. The upstream root AGPL-3.0 license is blob
  `0ad25db4bd1d86c452db3f9602ccdbe172438f52` and is already bundled verbatim.

This is positive evidence of intended integrated distribution, not a finding
that the component is prohibited. It is also not an explicit statement that
the parent AGPL grant covers every contribution in that precise UI revision.
The remaining question is narrowly whether the upstream copyright holders
license this UI pin under the parent Memoh AGPL terms, or provide another
applicable grant. No license has been invented, replaced, or applied to the UI
by this change. No maintainer message or permission request was sent.

## Notice changes

Both existing Web build paths now use one explicit notice allowlist and copy
the texts verbatim into `client-web/licenses/`. The same files are included
in the source-input hashes and the ordinary packaged-resource manifest. Missing
notice input fails the existing source validation; no integrity check is bypassed.

- Existing Memoh AGPL, Lucide and WaveSurfer 8.0.2 notices remain unchanged.
- The existing @noble/hashes 2.2.0 MIT notice is retained in the allowlist.
  Correction to the preliminary audit: the current phone digest path uses
  `crypto.subtle.digest`; it does not directly import noble. Its absence from
  the prior package is not, by itself, proof of a runtime notice violation.
- Inter 5.2.8 and Instrument Sans 5.3.0 notices are copied from the exact
  installed/pinned Fontsource packages (SIL OFL 1.1); KaTeX 0.18.1 retains its
  package's original MIT notice.
- Seti's packaged font matches the original VS Code Git blob
  `6a8a3da7cf05b57901a2d75bfc7acf7067438f07`. The complete official theme-seti
  third-party notice is preserved, blob
  `5faf4bb5a8fe00f76db29c92d5f58b8e2a81b0ba`. The theme JSON's older Seti UI
  commit alone did not identify the packaged font, so the actual byte match
  was checked before selecting the notice.
- MiSans is explicitly attributed. The `misans-vf@1.0.0` package's Apache-2.0
  text is preserved separately from the original Xiaomi font agreement. The
  wrapper's package label is not treated as relicensing the font. The complete
  [official original agreement](https://hyperos.mi.com/font-download/MiSans%E5%AD%97%E4%BD%93%E7%9F%A5%E8%AF%86%E4%BA%A7%E6%9D%83%E8%AE%B8%E5%8F%AF%E5%8D%8F%E8%AE%AE.pdf)
  is included as text extracted with `pdftotext -layout`, without rewriting
  its clauses. No font bytes or font-selection behavior were changed.
- The complete libc++ and libc++abi component sections are copied verbatim
  from the retained official SDK's LLVM NOTICE, including Apache-2.0 with
  LLVM exceptions and legacy terms. Extraction boundaries and original hash
  are recorded; no SDK/CLT executables or libraries are added to Git.
- The existing LibRaw 0.22.2 generator still verifies all 244 pinned source
  files and emits COPYRIGHT plus both original license texts. It was not
  replaced or relicensed. Source and rebuild instructions remain available.

`notice-origins.json` records exact byte hashes, package versions, official
source references, and LLVM section boundaries. `sources.txt` travels with the
application notices and points to the build/source records. These additions
are scoped attribution work, not a claim of exhaustive legal clearance for
every transitive dependency or a new permission grant.

## Validation and retained boundaries

Validation runs only the notice copier into an isolated directory, compares
every result with the original bytes, checks resource MIME support and source
input binding, and verifies that LibRaw notice generation is unchanged. The
source manifest is updated from 116 to 125 entries for nine new text files;
none are described as restored historical source. The 19 unverified later
historical modifications remain unverified.

No Web/native compilation, new HAP, release/tag, Library write, signing operation
or SDK download occurs in this preparation. Existing compiled artifacts retain
their old source/hash and do not contain these newly prepared notices.
