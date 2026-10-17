#!/bin/bash
# tools/precommit-check.sh — mechanical pre-commit guard (M6 P1-4/P1-5 remediation).
#
# The 4b24b89 escape: a commit added references to TypeparamDifferentialTest.java
# and internal/typeparam/typeparam.go while both files were still untracked, so
# a clean checkout of that commit could not build (javac/go build failures).
# This script fails if any *.java/*.go source file that should be committed is
# still untracked. Run from the repo root before every commit:
#
#   bash tools/precommit-check.sh && git commit ...
#
# The guard FAILS CLOSED: if `git status` itself errors (not a repo, git
# broken), the script exits non-zero — a pre-commit guard that reports
# "clean" when it cannot run is worse than none. git's stderr is never
# swallowed.
#
# Scratch locations (reviews/, research/) are exempt — work logs and survey
# snapshots live there untracked by design. hidden_files/ is deliberately
# NOT exempt: AGENTS.md §5 forbids it outright, so untracked sources there
# must fail the guard.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)" || exit 2
cd "$ROOT" || exit 2

status_out="$(git status --porcelain --untracked-files=all)" || {
    echo "PRECOMMIT FAIL: 'git status' failed (not a git repo, or git is broken); refusing to report clean." >&2
    exit 2
}
untracked="$(printf '%s\n' "$status_out" | grep '^??' | grep -E '\.(java|go)$' || true)"
# Exempt scratch dirs (NOT hidden_files/ — see header).
untracked="$(printf '%s\n' "$untracked" | grep -v -E 'reviews/|research/' || true)"

if [ -n "$untracked" ]; then
    echo "PRECOMMIT FAIL: untracked source files would be silently omitted from the commit:"
    echo "$untracked"
    echo "Fix: 'git add' them (or move scratch out of the source tree)."
    exit 1
fi
echo "precommit-check: clean (no untracked *.java/*.go outside scratch dirs)"
