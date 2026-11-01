#!/bin/bash
# Periodic master merge for the frontend-rewrite lane.
# Research track since 2026-11-01 (lane D, review P1-4): merge when the
# track is active, not hourly — the hourly order died with the move out
# of the product tree.
#
# Usage: ./merge-master.sh
# Run from the worktree root: research/frontend-v2/merge-master.sh

set -e
cd "$(dirname "$0")/../../.."

echo "=== Fetching master from main tree ==="
nice -n 10 git fetch ~/workspace/graalgo master:refs/remotes/main-tree/master

echo "=== Merging ==="
if nice -n 10 git merge main-tree/master --no-edit; then
    echo "=== Merge clean ==="
else
    echo "=== CONFLICTS — resolve manually ==="
    git status --short | head -20
    exit 1
fi

echo "=== Verifying v2 still builds ==="
export PATH=~/tools/go/bin:$PATH
cd research/frontend-v2
nice -n 10 go test ./... 2>&1 | tail -5

echo "=== Done. Log the merge in PROGRESS.md ==="
