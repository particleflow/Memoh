"""Static-only G704 differential check. Does not execute either Go function."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("old_binary", type=Path)
parser.add_argument("new_binary", type=Path)
parser.add_argument("--new-version", default="2.11.0", choices=["2.11.0", "2.11.4"])
args = parser.parse_args()
source = Path(__file__).resolve().parent
config = source.parents[2] / ".golangci.yml"
rows = []
with tempfile.TemporaryDirectory(prefix="g704-probe-") as directory:
    probe = Path(directory)
    (probe / "go.mod").write_text("module example.invalid/g704probe\n\ngo 1.25.7\n")
    (probe / "probe.go").write_bytes((source / "probe.go.txt").read_bytes())
    for version, binary in [("2.10.1", args.old_binary), (args.new_version, args.new_binary)]:
        result = subprocess.run(
            [str(binary.resolve()), "run", "--config", str(config), "--enable-only", "gosec", "./..."],
            cwd=probe, capture_output=True, text=True, timeout=90,
        )
        output = result.stdout + result.stderr
        rows.append({
            "version": version,
            "exit_code": result.returncode,
            "constant_destination_flagged": ":17:18: G704:" in output,
            "untrusted_destination_flagged": ":22:17: G704:" in output,
            "g704_count": output.count("G704:"),
        })
        print(output, end="")
assert rows == [
    {"version": "2.10.1", "exit_code": 1, "constant_destination_flagged": True,
     "untrusted_destination_flagged": True, "g704_count": 2},
    {"version": args.new_version, "exit_code": 1, "constant_destination_flagged": False,
     "untrusted_destination_flagged": True, "g704_count": 1},
], rows
print(json.dumps({"analysis_only": True, "network_requests": 0, "runs": rows}, indent=2))
