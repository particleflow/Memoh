# Recovered Harmony client

Source follow-up: [task observation recovery](../docs/harmony-observation-recovery.md)
now carries the current shared Web fix for transient downstream reads. Its
typecheck and focused/browser checks passed; it is not yet packaged into the
previously delivered 1.0.7 HAP. Login persistence code remains unchanged.

The existing client source has been restored and aligned with unified Memoh
`40f0a69fd4b7e34dfcf36ec8a44a9805403e7be8`. The Web build, official API26 native
compilation and HAP integrity qualification have passed. The new HAP is unsigned;
installation, physical-device runtime, NAS transfers and human acceptance remain
unverified. The current unsigned package is `1.0.7 / 1000007`, above the reported
installed 1.0.6, with unchanged bundle, permissions and persistence code. See
the [upgrade preparation evidence](../docs/harmony-upgrade-107.md) and
[first native build checkpoint](../docs/harmony-native-api26.md).

`SOURCE-PROVENANCE.json` records 116 reviewed source entries, their archive
origins, current hashes and subsequent alignment changes. The two source ZIPs
restored all 72 formerly excluded paths plus two tests. The historical 19 later
cloud edits remain unverified; this is not a claim to recover the unavailable
5a7 Git tree. Original archives and private backups remain preserved.

The existing native shell, Web overlay builder, packaging helpers and upstream
license notices are retained. No SDK/CLT binaries, dependency caches, generated
Web resources, HAP, signing material, private endpoint or account credentials
belong in this source tree. Runtime service configuration and exact delegation
attachment authorization are preserved; there is no new transfer service or
automatic LAN/IPv6 route/token migration.

Follow [BUILD-REPRODUCTION.md](BUILD-REPRODUCTION.md) for the exercised build
sequence and pinned external inputs. Source inventory verification remains:

```sh
python3 mobile/check_source_subset.py
```

That command only verifies source hashes and syntax; its `fullClientBuild: false`
field describes that check, not the separately recorded official compiler run.
Previous partial-import and blocked-stage evidence remains in Git history and
linked checkpoint documents; it must not be used as the current build status.
