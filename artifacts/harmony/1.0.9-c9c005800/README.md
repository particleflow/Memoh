# Historical Harmony 1.0.9 artifact record

The obsolete unsigned HAP has been removed from the current working tree.
Git history has not been deleted or rewritten. It is not a current release or
rollback target; the [current 1.0.19 handoff](../../../docs/testing/harmony-unified-1.0.19/README.md)
and the separate 1.0.18 field/rollback baseline remain unchanged.

| Field | Original artifact identity |
| --- | --- |
| Version | 1.0.9 / 1000009 |
| Source | `c9c00580052d09d5dce273e958ec384bef2ebed3` |
| Shared Web/SDK | `8a2422935916f56b5bacfac2f991824d1cb9bd9f` |
| Bundle | `cloud.begonia.workassistant` |
| Historical filename | `memoh-harmony-1.0.9-1000009-c9c005800-api26-unsigned.hap` |
| Bytes | 37053849 |
| SHA256 | `ebe3b6168ec013d1e78a2a523a155c07e2122000c98e99316ff88997ca948ae4` |
| Git blob | `d909bd204a7ad5993f3b50a931a90f7ee7c8387f` |
| Original distribution commit | `1bd851eca27bfb86adad690d01907b56f4b3f82f` |

The original bytes remain available at the
[immutable historical artifact](https://github.com/particleflow/Memoh/blob/1bd851eca27bfb86adad690d01907b56f4b3f82f/artifacts/harmony/1.0.9-c9c005800/memoh-harmony-1.0.9-1000009-c9c005800-api26-unsigned.hap).
Normal Git recovery from the repository root is:

```sh
git restore --source=1bd851eca27bfb86adad690d01907b56f4b3f82f --worktree -- artifacts/harmony/1.0.9-c9c005800/memoh-harmony-1.0.9-1000009-c9c005800-api26-unsigned.hap
```

Use that only for explicit historical reproduction. After restoring the old
payload, run `sha256sum -c SHA256SUMS` from this record's directory and compare
the byte length above. The checksum file identifies restored historical bytes;
it does not promise that a HAP remains beside it in the current tree.

The [original qualification record](../../../docs/testing/harmony-unified-8a24229/README.md)
and [build-identity.json](build-identity.json)
preserve the exact source/package and configuration-slot audit. They do not
certify a current Windows signing operation, device install or downgrade.
No tracked build, runtime or test reader for this obsolete binary was found.
The historical record is retained so old source/hash and injection reports can
be interpreted and independently recovered; no binary copy was moved elsewhere.

The 1.0.7 HAP is separately retained for its actual no-injection-slot rejection
fixture. Private configuration, signing inputs and user data are outside this
cleanup; no such material is added, accessed or deleted here.
