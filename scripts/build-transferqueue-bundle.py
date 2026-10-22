#!/usr/bin/env python3
"""Build a review/test bundle, never install or publish it. Run in repository root."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument("--output", required=True)
parser.add_argument("--arch", choices=["amd64", "arm64"], default="amd64")
args = parser.parse_args()
if subprocess.check_output(["git", "status", "--porcelain"], text=True).strip():
    raise SystemExit("Refusing to label an uncommitted tree as a reviewed source SHA")
out = Path(args.output).resolve()
# Do not overwrite an existing package or write into a Herdr installation.
out.mkdir(parents=True, exist_ok=False)
env = dict(os.environ, GOOS="windows", GOARCH=args.arch, CGO_ENABLED="0")
subprocess.run(["go", "build", "-trimpath", "-o", str(out / "transferqueue.exe"), "./cmd/transferqueue"], env=env, check=True)
subprocess.run(["go", "test", "-trimpath", "-c", "-o", str(out / "transferqueue.test.exe"), "./internal/transferclient"], env=env, check=True)
for name in ("herdr-plugin.toml", "verify-fixtures.ps1", "README.md"):
    shutil.copyfile(Path("contrib/herdr/memoh-delivery") / name, out / name)
sha = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
files = [{"name": p.name, "sha256": hashlib.sha256(p.read_bytes()).hexdigest()} for p in sorted(out.iterdir())]
(out / "SHA256SUMS.json").write_text(json.dumps({"source_commit": sha, "arch": args.arch, "files": files}, indent=2) + "\n", encoding="utf-8")
print(json.dumps({"bundle": str(out), "source_commit": sha, "arch": args.arch}))
