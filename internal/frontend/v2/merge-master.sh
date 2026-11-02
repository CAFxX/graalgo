#!/bin/bash
# Periodic master merge for the frontend-rewrite lane.
# Standing order (2026-10-15): merge master at least hourly to prevent drift.
#
# Usage: ./merge-master.sh (run from anywhere inside a graalgo checkout).
#
# Repo-relative (rewritten 2026-11-02, lane H): the old script fetched from
# a hardcoded ~/workspace/graalgo path into a main-tree remote that does not
# exist in a normal clone. This version resolves the checkout root via git,
# fetches master from the upstream remote (GRAALGO_UPSTREAM_REMOTE, defaulting
# to `github` when present, else `origin`), and verifies v2 with the pinned
# Go toolchain (GO env, else ~/tools/go/bin/go, else PATH go).
set -e

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

REMOTE="${GRAALGO_UPSTREAM_REMOTE:-}"
if [ -z "$REMOTE" ]; then
  if git remote | grep -qx github; then
    REMOTE=github
  elif git remote | grep -qx origin; then
    REMOTE=origin
  else
    echo "merge-master.sh: no github/origin remote (set GRAALGO_UPSTREAM_REMOTE)" >&2
    exit 1
  fi
fi

echo "=== Fetching master from $REMOTE ==="
nice -n 10 git fetch "$REMOTE" "master:refs/remotes/$REMOTE/master"

echo "=== Merging ==="
if nice -n 10 git merge "$REMOTE/master" --no-edit; then
    echo "=== Merge clean ==="
else
    echo "=== CONFLICTS — resolve manually ==="
    git status --short | head -20
    exit 1
fi

echo "=== Verifying v2 still builds ==="
GO_BIN="${GO:-$HOME/tools/go/bin/go}"
[ -x "$GO_BIN" ] || GO_BIN=go
cd internal/frontend/v2
nice -n 10 "$GO_BIN" test ./... 2>&1 | tail -5

echo "=== Done. Log the merge in PROGRESS.md ==="
