#!/usr/bin/env bash
# ci_status.sh [BRANCH] — the latest run of every workflow, per trigger
# event (push, schedule, workflow_dispatch, ...), on BRANCH (default
# master). Read-only (`gh run list`). Exit 1 when any latest completed run
# is red (not success/skipped) and not superseded: a red SCHEDULED
# workflow (weekly full-suite, nightly fuzz-soak, ...) is as much a red
# master as a red push.
#
# Superseded (shown, never red): a red schedule or workflow_dispatch row
# for which a successful schedule/workflow_dispatch run of the SAME
# workflow, on a commit at or after the red one's (git ancestry in this
# checkout; a commit not present locally never supersedes), came after it
# — the manual verify-dispatch pattern. "After" means: started after a
# failure had finished (a success running alongside a failure is a flake,
# not a fix), or finished after a cancelled run (a cancel carries no
# verdict). Push rows are never superseded here: the next push is the
# latest push row.
#
# Retired (shown, never red): a schedule row of a workflow whose file (in
# this checkout) no longer has an active `cron:` line (stdlib-gate's
# schedule was removed under BUG-0033).
#
# The coordinator runs it after every merge wave and at least daily
# (AGENTS.md §4). Columns: workflow, event, status/conclusion, age, run id,
# head sha. Test: test_ci_status.py feeds fixtures through
# CI_STATUS_RUNS_JSON (the `gh run list` JSON) and CI_STATUS_WORKFLOWS_JSON
# (the `gh workflow list` JSON) instead of calling gh.
set -euo pipefail
branch="${1:-master}"
root="$(git rev-parse --show-toplevel)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
runs="${CI_STATUS_RUNS_JSON:-}"
if [ -z "$runs" ]; then
  runs="$tmp/runs.json"
  gh run list --branch "$branch" --limit 200 \
    --json workflowName,event,status,conclusion,createdAt,updatedAt,databaseId,headSha >"$runs"
fi
wfs="${CI_STATUS_WORKFLOWS_JSON:-}"
if [ -z "$wfs" ]; then
  wfs="$tmp/workflows.json"
  gh workflow list --all --json name,path >"$wfs"
fi

python3 - "$runs" "$wfs" "$root" "$branch" <<'PY'
import json, re, subprocess, sys

runs_path, wfs_path, root, branch = sys.argv[1:5]
runs = json.load(open(runs_path))
paths = {w["name"]: w["path"] for w in json.load(open(wfs_path))}
PENDING = {"in_progress", "queued", "waiting", "pending", "requested"}
REPEAT = {"schedule", "workflow_dispatch"}
CRON = re.compile(r"^\s*-\s*cron:", re.M)


def state(r):
    return r["conclusion"] if r["status"] == "completed" else r["status"]


def ancestor(a, b):
    """a is b or an ancestor of b (False when either is not local)."""
    return a == b or subprocess.run(["git", "-C", root, "merge-base", "--is-ancestor", a, b],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def active_cron(wf):
    p = paths.get(wf)
    try:
        return bool(p) and bool(CRON.search(open("%s/%s" % (root, p)).read()))
    except OSError:
        return False


def superseder(red):
    """The run that supersedes this red one, or None (header comment)."""
    if red["event"] not in REPEAT:
        return None
    cancelled = state(red) == "cancelled"
    for s in sorted(runs, key=lambda r: r["createdAt"], reverse=True):  # newest first
        if (s["workflowName"] != red["workflowName"] or s["event"] not in REPEAT
                or s["status"] != "completed" or s["conclusion"] != "success"):
            continue
        later = (s["updatedAt"] > red["updatedAt"]) if cancelled else (s["createdAt"] > red["updatedAt"])
        if later and ancestor(red["headSha"], s["headSha"]):
            return s
    return None


latest = {}
for r in runs:
    k = (r["workflowName"], r["event"])
    if k not in latest or r["createdAt"] > latest[k]["createdAt"]:
        latest[k] = r

rows, red = [], []
for k in sorted(latest):
    r = latest[k]
    st = state(r)
    if r["event"] == "schedule" and not active_cron(r["workflowName"]):
        st = "retired(%s)" % st
    elif st not in ("success", "skipped") and st not in PENDING:
        s = superseder(r)
        if s:
            st = "superseded(%s)<-%s" % (st, s["databaseId"])
        else:
            red.append((r, st))
    rows.append([r["workflowName"], r["event"], st, r["createdAt"], str(r["databaseId"]), r["headSha"][:8]])

widths = [max(len(row[i]) for row in rows) for i in range(6)] if rows else []
for row in rows:
    print("  ".join(c.ljust(w) for c, w in zip(row, widths)).rstrip())
if red:
    print("\nRED (latest run per workflow/event on %s, not superseded):" % branch)
    for r, st in red:
        print("  %s [%s] %s: gh run view %s" % (r["workflowName"], r["event"], st, r["databaseId"]))
    sys.exit(1)
PY
