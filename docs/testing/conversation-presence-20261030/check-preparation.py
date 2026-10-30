#!/usr/bin/env python3
"""Offline preparation checks. This is not Go parsing, compilation or a test run."""

import hashlib
import json
import pathlib
import re
import subprocess
from datetime import datetime, timezone

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[2]
RESEARCH_COMMIT = "1e0950077a4e2ec025cbda68a5388bae15a75d6f"
OWNED_GO = {
    "internal/conversationpresence/policy.go",
    "internal/conversationpresence/policy_test.go",
    "internal/conversationpresence/coordinator.go",
    "internal/conversationpresence/coordinator_test.go",
    "internal/agent/application/conversation_presence_sdk.go",
    "internal/agent/application/conversation_presence_sdk_test.go",
}


def git(*args):
    return subprocess.check_output(["git", "-C", str(ROOT), *args], text=True).strip()


def main():
    changed = set(filter(None, git("diff", "--name-only", RESEARCH_COMMIT).splitlines()))
    changed.update(filter(None, git("ls-files", "--others", "--exclude-standard").splitlines()))
    for name in changed:
        assert name in OWNED_GO or name.startswith("docs/testing/conversation-presence-20261030/"), name
    tests = []
    hashes = {}
    for name in sorted(OWNED_GO):
        path = ROOT / name
        text = path.read_text()
        assert text.startswith("package ") or text.startswith("// Package "), name
        assert not re.search(r"[ \t]+$", text, re.MULTILINE), name
        for imported in re.findall(r'"github.com/felinics/memoh/([^\"]+)"', text):
            assert (ROOT / imported).is_dir(), (name, imported)
        if name.endswith("_test.go"):
            tests += re.findall(r"^func (Test\w+)\(", text, re.MULTILINE)
        else:
            assert not re.search(r"glm-|gpt-|claude-", text, re.IGNORECASE), name
        hashes[name] = hashlib.sha256(path.read_bytes()).hexdigest()
    assert len(tests) == len(set(tests))
    assert "TestPresenceSDKUsesSameModelOffThenLowWithoutPreferenceWrites" in tests
    assert "TestStartDoesNotWaitForAuxiliaryStoreOrGeneration" in tests
    assert "TestFormalFirstAndCancelRejectProviderThatIgnoresCancellation" in tests
    adapter = (ROOT / "internal/agent/application/conversation_presence_sdk.go").read_text()
    for forbidden in ("ResolveModelCredentials(", "writeBackSessionModelPreference(", "modelretry.Do(", "http.Post("):
        assert forbidden not in adapter, forbidden
    acceptance = json.loads((HERE / "acceptance-cases.json").read_text())
    assert len(acceptance["cases"]) == 37
    ids = [item["id"] for item in acceptance["cases"]]
    assert len(ids) == len(set(ids))
    result = {
        "checked_at_utc": datetime.now(timezone.utc).isoformat(),
        "base_research_commit": RESEARCH_COMMIT,
        "status": "offline_structure_checks_passed_only",
        "changed_files_within_declared_ownership": True,
        "shared_runtime_handler_projection_speech_files_changed": False,
        "prepared_go_test_functions": tests,
        "go_tests_executed": False,
        "gofmt_executed": False,
        "go_type_or_syntax_check_executed": False,
        "go_blocker": "Previously identified PATH go is a board-game executable; no compiler installed or new Go test attempted in this preparation turn.",
        "durable_claims_and_main_pipeline_wired": False,
        "ui_and_live_latency_verified": False,
        "real_model_or_tts_requests": 0,
        "deployments": 0,
        "remote_push_or_external_submissions": 0,
        "request_count_fields_are_scope_declarations_not_measured_by_this_script": True,
        "source_sha256": hashes,
    }
    (HERE / "PREPARATION-CHECK.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"status": result["status"], "prepared_go_tests": len(tests),
                      "go_tests_executed": False, "integrated": False}))


if __name__ == "__main__":
    main()
