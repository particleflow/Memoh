# Harmony source recovery checkpoint

This recovery branch preserves the 51 public client files from
`8184928c3746175b56b398be76f054c00d5e4b40` byte-for-byte. It adds evidence only;
it does not reconstruct the unavailable private Git history or introduce a new
client. The source inventory and per-path gaps are in
`harmony-recovery-20261023.json`. Existing license notices are unchanged.

## Input coverage

| Input | Verified in this recovery | Coverage and conflicts |
| --- | --- | --- |
| Original user source ZIP | Library filename and declared size: 168253 bytes | Bytes unavailable; archive readability, hashes, membership and conflicts unknown. |
| Earlier alignment checkpoint ZIP | Library filename and declared size: 317555 bytes | Bytes unavailable; cannot assume it includes subsequent cloud modifications. |
| Public PR67 fixed commit | Git objects fetched and all 51 mobile files read; 42 original manifest hashes verified | The complete public subset is preserved; 72 inherited paths listed by its manifest are absent. No byte-level ZIP comparison was possible. |
| Candidate server/Web commit `dfc7cf5104c382ecc622061ce43c3dcd733b7998` | Git objects fetched and six relevant Web modules compared | Reference input only; no server/Web changes are imported by this checkpoint. |

The current official Library download helper stopped before downloading either
archive: `hosted apps tools/list request failed with HTTP status 401`. No
alternative private URL, transfer route, or source reconstruction was used.
Restore the consumer environment's Library helper authentication before resuming
materialization. Metadata sizes above are not locally verified byte counts.

Earlier records describe 127 tracked files and 114 provenance entries in an
unavailable later source checkpoint. They also describe 53 of the 72 inherited
paths matching the original archive and 19 subsequently modified in the cloud.
Those counts are historical evidence only. The exact 19-path mapping and their
later contents have not been recovered, so every missing path in the new
inventory remains explicitly unresolved. The 72 entries describe omissions from
the public subset; they are not a reconstruction of the later 114-entry manifest.
Changes unique to unavailable commits cannot be enumerated or recovered from
the available Git objects. No old uncertain Git write was retried.

## Checks run on this recovery

- `python3 mobile/check_source_subset.py`: passed; 42 manifest hashes, 51 files,
  16 Python syntax checks and 9 CJS syntax checks.
- `HARMONY_TEST_TYPESCRIPT=<existing-typescript-module> node
  mobile/client/tests/unit/service-config.test.cjs`: 46 checks passed.
- TypeScript 6.0.3 strict no-emit check of `harmony-service-url.ts` and
  `harmony-service-config.ts`: passed, with ES2022/DOM libraries, ESNext modules
  and bundler module resolution. This covers only these two pure TS modules.
- The existing Node 24.19.0 / Python 3.12.14 environment was reused. No SDK,
  dependency bundle, signing material, generated Web or HAP was installed or
  added to this recovery commit. This is not reproduction of the old Node 22
  build environment.
- A pre-publication text scan and review of the 51 client files plus these two
  recovery records found no credential material or private service settings.
  Matches were synthetic test tokens, rejected URL fixtures, public source and
  attribution links, and repository-relative paths. Root `LICENSE` and the
  retained dependency notice remain byte-identical to the fixed public commit.

These checks are source/configuration evidence only. They do not validate a
complete Vue application, ArkTS, device behavior or an installable HAP. Old A11
build/test records are not current acceptance results.

## Build and runtime blockers

The public input lacks both `mobile/client/mobile/build_memoh_web.py` and
`mobile/client/mobile/harmony/tools/build_api26.py`; invoking each returned
`[Errno 2] No such file or directory`. Native app/project configuration, shell,
packaging helpers and assets are also among the 72 missing paths. Toolchain
requirements remain documented in `mobile/BUILD-REPRODUCTION.md`.

The documented development command `mise run dev` returned
`mise: command not found`. Docker is present, but starting the unrelated server
cannot supply the absent native client and overlay builder. No new complete
client runtime, real UI verification, screenshots, signed build or human QA is
claimed. The build/acceptance work remains incomplete until the source inputs
and appropriate runtime are available; this branch is a recovery checkpoint.

## Alignment boundary

The four shared task/delegation modules listed in the JSON inventory are
byte-identical between `be6862497a58a01ef8c6f4168f02d5624cd44061` and `dfc7cf5`.
The two attachment modules are also identical between those two commits, but
differ from the client's pinned Web base `6ddf61dcd8199758e4110398645c556a3d6d7375`:

- `useMediaGallery.ts` validates the delegation-scoped path and matching content
  hash; malformed delegated references resolve to an empty URL. They must never
  fall back to the broader Bot/hash route.
- `attachment-block.vue` carries `delegationUrl` into the preview identity and
  request context.

After the two archives can be read, compare all files before selecting a source:
preserve their original bytes separately, identify conflicts against PR67, and
keep unknown later edits explicitly unresolved. Then restore the proven build
inputs, retain applicable notices, scan the proposed public diff, and commit it
before extending alignment. Do not infer source equivalence from an old HAP,
compiled-resource fingerprint or an unavailable commit identifier.
