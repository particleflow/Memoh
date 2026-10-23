"""Analyze synthetic code only; retain every diagnostic and never run the Go code."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time

parser = argparse.ArgumentParser()
parser.add_argument("binary", type=Path)
args = parser.parse_args()
source = Path(__file__).resolve().parent
config = source.parents[3] / ".golangci.yml"
binary = str(args.binary.resolve())
version = subprocess.check_output([binary, "version"], text=True)
assert "version 2.11.4 " in version, version
print(version, end="")
started = time.monotonic()
with tempfile.TemporaryDirectory(prefix="memoh-ownership-probe-") as directory:
    probe = Path(directory)
    (probe / "go.mod").write_text("module example.invalid/cancelownership\n\ngo 1.25.7\n")
    for name in ("probe.go", "bounds.go", "indirect.go"):
        (probe / name).write_bytes((source / (name + ".txt")).read_bytes())
    result = subprocess.run(
        [binary, "run", "--concurrency", "2", "--config", str(config), "./..."],
        cwd=probe, capture_output=True, text=True, timeout=90,
    )
    output = result.stdout + result.stderr
    print(output, end="")
    diagnostics = []
    for line in output.splitlines():
        match = re.match(r".*[/\\]([^/\\]+\.go):(\d+):(\d+): (.*) \(([^()]+)\)$", line)
        if match:
            name, row, column, message, linter = match.groups()
            diagnostics.append((name, int(row), message.split(":", 1)[0], linter))
    expected = [
        ("bounds.go", 37, "G115", "gosec"),
        ("bounds.go", 45, "G115", "gosec"),
        ("bounds.go", 51, "G602", "gosec"),
        ("bounds.go", 55, "G115", "gosec"),
        ("indirect.go", 11, "G118", "gosec"),
        ("indirect.go", 32, "G118", "gosec"),
        ("probe.go", 53, "G118", "gosec"),
        ("probe.go", 69, "G118", "gosec"),
    ]
    assert result.returncode == 1, result.returncode
    assert sorted(diagnostics) == expected, diagnostics
print(json.dumps({"harness_exit": 0, "lint_exit": result.returncode,
                  "diagnostics": len(diagnostics), "full_repository_rules": True,
                  "go_functions_executed": 0, "business_network_requests": 0,
                  "elapsed_seconds": round(time.monotonic() - started, 3)}, indent=2))
