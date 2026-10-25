#!/usr/bin/env python3
"""Synthetic cross-end acceptance bytes only; no network, credentials or services.

These fixed samples are test data, not product size/count limits. Create refuses
an existing destination. Verification never writes, renames or deletes a file.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil


BLOCK = 65536


def samples():
    yield "chunked.bin", 17 * 1024 * 1024 + 123, "chunked"
    for index in range(1, 33):
        yield f"batch/{index:02d}.bin", BLOCK + index, f"batch-{index}"
    yield "same-content.bin", BLOCK + 1, "batch-1"
    yield "collision-a/result.bin", 4097, "collision-a"
    yield "collision-b/result.bin", 4097, "collision-b"


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("expected a regular test file")
    size, checksum = 0, hashlib.sha256()
    with path.open("rb") as source:
        while block := source.read(BLOCK):
            size += len(block)
            checksum.update(block)
    return size, checksum.hexdigest()


def create(root):
    total = sum(size for _, size, _ in samples())
    # A small fixture reserve, never a transfer admission or total-queue policy.
    if shutil.disk_usage(root.parent).free < total + 64 * 1024 * 1024:
        raise ValueError("insufficient free space for this test dataset")
    root.mkdir(mode=0o700, exist_ok=False)
    records = []
    for name, size, seed in samples():
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        checksum = hashlib.sha256()
        with target.open("xb") as output:
            for offset in range(0, size, BLOCK):
                block = hashlib.shake_256(
                    f"memoh-transfer-acceptance-v1:{seed}:{offset}".encode()
                ).digest(min(BLOCK, size - offset))
                output.write(block)
                checksum.update(block)
        records.append({"path": name, "size_bytes": size, "sha256": checksum.hexdigest()})
    manifest = {"fixture": "memoh-transfer-acceptance-v1", "files": records,
                "total_bytes": total}
    with (root / "manifest.json").open("x", encoding="utf-8") as output:
        json.dump(manifest, output, indent=2)
        output.write("\n")
    print(json.dumps({"created_files": len(records), "written_bytes": total}))


def verify(root, logical=None, downloaded=None):
    manifest = json.loads((root / "manifest.json").read_text(encoding="utf-8"))
    expected = [(name, size) for name, size, _ in samples()]
    actual = [(item["path"], item["size_bytes"]) for item in manifest["files"]]
    if (manifest["fixture"] != "memoh-transfer-acceptance-v1" or actual != expected
            or manifest["total_bytes"] != sum(size for _, size in expected)):
        raise ValueError("unexpected fixture manifest")
    records = [item for item in manifest["files"] if logical is None or item["path"] == logical]
    if not records:
        raise ValueError("unknown fixture file")
    for item in records:
        path = downloaded if downloaded is not None else root / item["path"]
        size, checksum = digest(path)
        if size != item["size_bytes"] or checksum != item["sha256"]:
            raise ValueError("size/hash mismatch: " + item["path"])
    print(json.dumps({"verified_files": len(records),
                      "verified_bytes": sum(item["size_bytes"] for item in records)}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest="action", required=True)
    for action in ("create", "verify", "check-file"):
        command = actions.add_parser(action)
        command.add_argument("root", type=Path)
        if action == "check-file":
            command.add_argument("logical_name")
            command.add_argument("downloaded_file", type=Path)
    args = parser.parse_args()
    try:
        if args.action == "create":
            create(args.root)
        elif args.action == "verify":
            verify(args.root)
        else:
            verify(args.root, args.logical_name, args.downloaded_file)
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f"test data check failed: {error}\n")


if __name__ == "__main__":
    main()
