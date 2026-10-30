#!/bin/bash
# tools/check-no-phase-paths.sh — P1-13 remediation gate.
#
# Fails on any live reference to the removed phases/phaseN tree in the
# developer-tool dirs this sweep owns (gate1, fuzz, mutation,
# godebug-diff, stdlib-excl, stdlib-vendor, stdlib-linkdiff, stdlib-fork,
# fuzz-soak) plus test/e2e/run_module.sh. Both spellings are caught: the
# literal path (phases/phaseN/...) and the Go segment-joined form
# ("phases", "phaseN").
#
# Allowed: citations of the historical archive under docs/arch/phases/
# (e.g. docs/arch/phases/phase2/GODEBUG.md) — those are the archive, not
# live paths, and are filtered out below.
#
# Run from the repo root, or via `make check` (root Makefile).
set -u
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

DIRS="tools/gate1 tools/fuzz tools/mutation tools/godebug-diff
  tools/stdlib-excl tools/stdlib-vendor tools/stdlib-linkdiff
  tools/stdlib-fork tools/fuzz-soak"
FILES="test/e2e/run_module.sh"

# shellcheck disable=SC2086 (DIRS is intentionally word-split)
hits="$(grep -rIn -E 'phases/phase[0-9]|"phases", *"phase[0-9]"' $DIRS $FILES 2>/dev/null \
  | grep -v 'docs/arch/phases/' || true)"

if [ -n "$hits" ]; then
    echo "NO-PHASE-PATHS GATE FAILED: live references to the removed phases/ tree:"
    echo "$hits"
    echo "Fix: repoint to the docs/REPO_STRUCTURE.md layout (internal/frontend/,"
    echo "internal/backend/{truffle,runtime,interop}/, tools/stdlib-fork/)."
    exit 1
fi
echo "no-phase-paths gate: clean"
