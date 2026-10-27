# Historical Harmony 1.0.8 artifact record

The obsolete unsigned HAP has been removed from the current working tree.
Git history has not been deleted or rewritten. It is not a current release or
rollback target; the [current 1.0.19 handoff](../../../docs/testing/harmony-unified-1.0.19/README.md)
and the separate 1.0.18 field/rollback baseline remain unchanged.

| Field | Original artifact identity |
| --- | --- |
| Version | 1.0.8 / 1000008 |
| Source | `ee00ff76a07ac27e08f4b4f58fd1354f0ecbeef3` |
| Shared Web/SDK | `68be06092a1ea85d961115aac404082982bdecf4` |
| Bundle | `cloud.begonia.workassistant` |
| Historical filename | `memoh-harmony-1.0.8-1000008-ee00ff76a-api26-unsigned.hap` |
| Bytes | 37029849 |
| SHA256 | `83109c5c889e6f314dc0916b9ea4027ca35fa79055c4cff48c66669f7c5a44cd` |
| Git blob | `246cc7c219de2c080a5314bad9bf4f8fa9158117` |
| Original distribution commit | `cdedf9239f0c93c41a0e679b570778ac999eadbe` |

The original bytes remain available at the
[immutable historical artifact](https://github.com/particleflow/Memoh/blob/cdedf9239f0c93c41a0e679b570778ac999eadbe/artifacts/harmony/1.0.8-ee00ff76a/memoh-harmony-1.0.8-1000008-ee00ff76a-api26-unsigned.hap).
Normal Git recovery from the repository root is:

```sh
git restore --source=cdedf9239f0c93c41a0e679b570778ac999eadbe --worktree -- artifacts/harmony/1.0.8-ee00ff76a/memoh-harmony-1.0.8-1000008-ee00ff76a-api26-unsigned.hap
```

Use that only for explicit historical reproduction. After restoring the old
payload, run `sha256sum -c SHA256SUMS` from this record's directory and compare
the byte length above. The checksum file identifies restored historical bytes;
it does not promise that a HAP remains beside it in the current tree.

The [original qualification record](../../../docs/testing/harmony-windows-injection-108/README.md)
preserve the exact source/package and configuration-slot audit. They do not
certify a current Windows signing operation, device install or downgrade.
No tracked build, runtime or test reader for this obsolete binary was found.
The historical record is retained so old source/hash and injection reports can
be interpreted and independently recovered; no binary copy was moved elsewhere.

The 1.0.7 HAP is separately retained for its actual no-injection-slot rejection
fixture. Private configuration, signing inputs and user data are outside this
cleanup; no such material is added, accessed or deleted here.
