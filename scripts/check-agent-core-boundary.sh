#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
baseline=$(tr -d '\r\n' < scripts/agent-core-baseline.txt)
if [[ ! "$baseline" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Invalid official Agent baseline SHA' >&2
  exit 1
fi
git cat-file -e "${baseline}^{commit}"
paths=(internal/agent/runtime internal/contextview)
if (($# > 1)); then
  echo 'Usage: check-agent-core-boundary.sh [revision]' >&2
  exit 2
fi
if (($# == 1)); then
  git diff --exit-code "$baseline" "$1" -- "${paths[@]}"
else
  # Include tracked worktree edits and staged changes. New files cannot evade
  # the boundary merely by remaining untracked during local verification.
  git diff --exit-code "$baseline" -- "${paths[@]}"
  untracked=$(git ls-files --others --exclude-standard -- "${paths[@]}")
  if [[ -n "$untracked" ]]; then
    echo "Unexpected new Agent core files: $untracked" >&2
    exit 1
  fi
fi
echo "Agent runtime and contextview match official baseline $baseline"
