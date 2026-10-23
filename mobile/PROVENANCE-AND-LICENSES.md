# Source and license review

The per-file register is `SOURCE-PROVENANCE.json`; every listed path is relative
to this repository. Its hashes describe the imported source, not a release or
signing record. Cloud additions were checked against development history and
their module contents. Absence from the inherited source set alone is not an
authorship or license determination. Import statements reference dependencies;
they do not copy those dependencies' implementations.

| Category | Treatment |
| --- | --- |
| Client additions developed during cloud alignment | Import reviewed modules and synthetic test harnesses under the repository's existing contribution context; do not assign a new license. |
| Empty configuration and public pins | Import metadata only. Runtime endpoints remain blank; dependency archives are excluded. |
| Third-party notice | Preserve `client/mobile/memoh/licenses/noble-hashes.txt` verbatim: @noble/hashes 2.2.0, MIT, Paul Miller. No hashing implementation is vendored. |
| Inherited application shell, helpers and contracts | Hold until individual source attribution and redistribution basis are established. |
| Inherited graphics and assets | Hold because original versus third-party derivation is not established. |
| Web overlay builder | Hold: it contains upstream replacement context and derived implementation; do not call it exclusively original. |

The pinned Memoh repository has an AGPL version 3 root `LICENSE`, while its root
`package.json` says `ISC`. This is an unresolved upstream metadata inconsistency,
not a conclusion about which statement controls. Neither is changed here. No
new license header overrides those terms. The source subset preserves this ambiguity; it does not resolve dependency
licensing for a full public source or binary distribution.

The pinned `felinics/ui` revision is
`f69402a55dd87a59c10722fe11507ad8fd7048f5`; the inspected package did not provide
a package license field or a root license file. Its implementation is excluded.
The source-preparation helper records how the existing public source pins were
combined; it is not a redistribution authorization and is not run by this import.

Other original-project dependencies (including wavesurfer, Lucide/Feather and
LibRaw) and their implementations/assets are not imported. Their notices remain
with the original project. Huawei SDK/CLT content and licenses are not copied.

The review excludes infrastructure addresses, private text, real account
identifiers, credentials, signing files, build products, caches and private
handoff records. Synthetic tests intentionally contain reserved test domains,
loopback fixture listeners, fixed dummy identities and invalid-token examples;
these are not production connection settings. Public author links in retained
license notices are attribution and must not be stripped.
