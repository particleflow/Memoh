#!/usr/bin/env python3
"""Offline validation of research inputs; does not test a product implementation."""

import datetime
import hashlib
import json
import pathlib
import re
import subprocess


HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
BASE = "cefdf0cbb3207d2a07db17ef365e1b8d2d43ca3f"


def git(*args):
    return subprocess.check_output(["git", "-C", str(ROOT), *args], text=True).strip()


def main():
    data = json.loads((HERE / "acceptance-cases.json").read_text())
    assert data["source_commit"] == BASE
    assert data["kind"] == "design_acceptance_inputs_not_product_results"
    assert git("rev-parse", BASE) == BASE
    assert len(data["invariants"]) == len(set(data["invariants"]))
    cases = data["cases"]
    ids = [case["id"] for case in cases]
    assert len(ids) == len(set(ids))
    for case in cases:
        assert set(case) == {"id", "category", "facts", "events", "expected"}
        assert all(isinstance(case[key], list) and case[key]
                   for key in ("facts", "events", "expected"))
        assert all(isinstance(value, str) and value.strip()
                   for key in ("facts", "events", "expected")
                   for value in case[key])

    local_links = []
    for link in re.findall(r"\]\(([^)]+)\)", (HERE / "README.md").read_text()):
        if link.startswith(("https://", "http://", "#")):
            continue
        target = (HERE / link.split("#", 1)[0]).resolve()
        assert target.is_relative_to(ROOT), link
        if target.name != "STATIC-CHECK.json":
            assert target.is_file(), link
        local_links.append(link)

    changed = set(filter(None, git("diff", "--name-only", BASE).splitlines()))
    changed.update(filter(None, git("ls-files", "--others", "--exclude-standard").splitlines()))
    for name in changed:
        assert (name.startswith("docs/testing/conversation-presence-20261030/")
                or name == "docs/testing/compatibility-incident-register.md"), name

    whitespace_checks = []
    for name in sorted(changed):
        result = subprocess.run(
            ["git", "diff", "--no-index", "--check", "/dev/null", str(ROOT / name)],
            capture_output=True, text=True, check=False,
        )
        # --no-index may use 1 for differing content; whitespace errors have
        # diagnostic output (and normally status 2) and must still fail.
        assert result.returncode in (0, 1) and not result.stdout and not result.stderr, result.stdout + result.stderr
        whitespace_checks.append(name)

    evidence_paths = [HERE / "README.md", HERE / "acceptance-cases.json",
                      HERE / "check-design.py", ROOT / "docs/testing/compatibility-incident-register.md"]
    result = {
        "checked_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source_commit": BASE,
        "kind": "offline_design_input_validation_only",
        "status": "passed",
        "case_count": len(cases),
        "categories": sorted({case["category"] for case in cases}),
        "local_link_count": len(local_links),
        "changed_files_are_research_docs_only": True,
        "whitespace_checked": whitespace_checks,
        "sha256": {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                   for path in evidence_paths},
        "go_test_attempt": {
            "command": "go test ./internal/reasoning -run 'TestResolveConfigAgreesWithOffReachability|TestResolveConfigHonorsProjectedOffDeclarations|TestAlwaysOnModelIsSupportedWithoutAControl' -count=1 -v",
            "exit_code": 1,
            "error": "Go: Unknown option: test",
            "observed_cause": "/usr/bin/go --help identifies a local two-player board-game application",
            "tests_ran": False,
            "denied": False,
            "status": "blocked_pending_identified_go_compiler",
        },
        "scope_declaration": {
            "real_model_requests": 0,
            "tts_requests": 0,
            "production_changes": 0,
            "product_implementation": False,
            "ui_or_live_latency_verified": False,
            "note": "These scope declarations describe this research turn; static validation does not measure them or prove product behavior.",
        },
    }
    (HERE / "STATIC-CHECK.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"status": result["status"], "case_count": len(cases),
                      "local_link_count": len(local_links), "product_tests_passed": False}, ensure_ascii=False))


if __name__ == "__main__":
    main()
