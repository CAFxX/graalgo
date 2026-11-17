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
# Scratch locations (reviews/, research/, and gitignored .tmp/ dirs) are
# exempt. The banned research-era scratch dir (hidden_files) is NOT exempt:
# per the no-debris rule it must never reappear, so its presence fails.
set -u
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

if find . -path ./.git -prune -o -type d -name 'hidden_files' -print | grep -q .; then  # dead-path-ok (detector)
    echo "PRECOMMIT FAIL: a hidden_files/ directory exists (banned scratch; move evidence to commit messages):"  # dead-path-ok
    find . -path ./.git -prune -o -type d -name 'hidden_files' -print  # dead-path-ok
    exit 1
fi
untracked="$(git status --porcelain --untracked-files=all 2>/dev/null | grep '^??' | grep -E '\.(java|go)$' || true)"
# Exempt scratch dirs.
untracked="$(echo "$untracked" | grep -v -E 'reviews/|research/' || true)"

if [ -n "$untracked" ]; then
    echo "PRECOMMIT FAIL: untracked source files would be silently omitted from the commit:"
    echo "$untracked"
    echo "Fix: 'git add' them (or move scratch out of the source tree)."
    exit 1
fi
echo "precommit-check: clean (no untracked *.java/*.go outside scratch dirs)"
