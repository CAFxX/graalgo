#!/bin/bash
# periodic-merge-rework.sh — periodically merge rework/loopcap-gc-model into master.
# Safety rules (all load-bearing):
#   1. NEVER merge over uncommitted edits in the main tree (M6 may be writing).
#      If any file the merge would touch is dirty in the main worktree, skip quietly.
#   2. NEVER auto-resolve conflicts: on conflict, abort and record; a human/agent resolves.
#   3. NEVER land a red merge: the merge is built and tested in the integration
#      worktree first; only a green merge reaches master.
#   4. Idempotent: no new rework commits -> no-op. Safe to re-run.
# State + logs: ~/workspace/graalgo/phases/phase1/hidden_files/merge-log/
set -u
MAIN="$HOME/workspace/graalgo"
INT="$HOME/workspace/graalgo-merge-int"
LOGDIR="$MAIN/phases/phase1/hidden_files/merge-log"
REWORK="rework/loopcap-gc-model"
mkdir -p "$LOGDIR"
STAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LOG="$LOGDIR/merge-$STAMP.log"
exec >"$LOG" 2>&1
echo "=== periodic merge run $STAMP ==="

cd "$MAIN" || exit 2
git rev-parse --verify "$REWORK" >/dev/null 2>&1 || { echo "no rework branch; exit"; exit 0; }

# New commits on the rework branch since master?
NEW="$(git rev-list --count "master..$REWORK")"
echo "new rework commits since master: $NEW"
if [ "$NEW" -eq 0 ]; then echo "nothing to merge; exit"; exit 0; fi

# Files the merge would touch (union of both sides' diffs vs merge-base).
BASE="$(git merge-base master "$REWORK")"
TOUCHED="$(git diff --name-only "$BASE" master; git diff --name-only "$BASE" "$REWORK")"
TOUCHED="$(echo "$TOUCHED" | sort -u)"
echo "touched files:"; echo "$TOUCHED" | head -20

# Main worktree clean for exactly those files? (M6 may be mid-write.)
# shellcheck disable=SC2086
DIRTY="$(git status --porcelain -- $TOUCHED 2>/dev/null | head -20)"
if [ -n "$DIRTY" ]; then
  echo "SKIP: main-tree files touched by merge have uncommitted edits (M6 writing):"
  echo "$DIRTY"
  exit 0
fi

# Merge + test in the integration worktree (never in the live main tree).
cd "$INT" || exit 2
git checkout -q -B merge-int master || exit 2
if ! git merge --no-ff -m "Periodic merge: $REWORK into master ($STAMP)" "$REWORK"; then
  git merge --abort 2>/dev/null
  echo "CONFLICT: merge aborted, no state changed. Needs manual resolution."
  echo "$STAMP CONFLICT" >> "$LOGDIR/status.txt"
  exit 1
fi
MERGED_SHA="$(git rev-parse HEAD)"
echo "merged sha: $MERGED_SHA"

# Build + test the merge. Timeout guards a hung suite.
# NOTE: the Phase-1 Makefile lives under phases/phase1, not the repo root.
if ! timeout 1800 make -C phases/phase1 test; then
  echo "RED: merge $MERGED_SHA failed make test; NOT landing on master."
  echo "$STAMP RED $MERGED_SHA" >> "$LOGDIR/status.txt"
  exit 1
fi
echo "GREEN: merge $MERGED_SHA passed make test."

# Main tree still clean for touched files? (M6 may have started writing meanwhile.)
cd "$MAIN" || exit 2
# shellcheck disable=SC2086
DIRTY2="$(git status --porcelain -- $TOUCHED 2>/dev/null | head -20)"
if [ -n "$DIRTY2" ]; then
  echo "SKIP-LATE: main tree dirtied during test; merge stays on merge-int branch for next round."
  exit 0
fi
if git merge --ff-only "$MERGED_SHA"; then
  echo "LANDED: $MERGED_SHA on master (fast-forward)."
else
  # Master moved during the test run: do NOT land an untested merge.
  # Reset the integration branch to the tested merge and try again next round.
  echo "SKIP-STALE: master moved during test; merge stays on merge-int branch for next round."
  exit 0
fi
echo "$STAMP LANDED $MERGED_SHA ($NEW commits)" >> "$LOGDIR/status.txt"
# Keep the integration branch pointer at the landed merge for the record.
cd "$INT" && git checkout -q -B merge-int "$MERGED_SHA"
echo "=== done ==="
