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
# Scratch locations (reviews/, research/) are exempt — work logs and survey
# snapshots live there untracked by design. hidden_files/ is NOT exempt: it
# is banned (AGENTS.md §5) and must never reappear — its presence fails the
# guard outright (see the banned-directory check below). The exemption was
# written when hidden_files/ was a legitimate scratch location; the torture
# scripts recreated it on every run (review R-P1-8) while this guard
# explicitly ignored it.
set -u
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

# Banned-directory guard: hidden_files/ must never reappear (AGENTS.md §5).
# Fail loudly if it exists, instead of silently exempting its contents.
if [ -d hidden_files ]; then
    echo "PRECOMMIT FAIL: banned directory hidden_files/ exists in the tree (AGENTS.md §5)."
    echo "Fix: remove it — run outputs belong under a gitignored out/ or .tmp/ dir,"
    echo "and committed corpus programs under tools/torture/corpus/ — never hidden_files/."
    exit 1
fi

untracked="$(git status --porcelain --untracked-files=all 2>/dev/null | grep '^??' | grep -E '\.(java|go)$' || true)"
# Exempt scratch dirs (hidden_files/ deliberately NOT exempt — banned, see above).
untracked="$(echo "$untracked" | grep -v -E 'reviews/|research/' || true)"

if [ -n "$untracked" ]; then
    echo "PRECOMMIT FAIL: untracked source files would be silently omitted from the commit:"
    echo "$untracked"
    echo "Fix: 'git add' them (or move scratch out of the source tree)."
    exit 1
fi
echo "precommit-check: clean (no untracked *.java/*.go outside scratch dirs; no banned hidden_files/)"
