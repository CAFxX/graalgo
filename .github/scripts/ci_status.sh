#!/usr/bin/env bash
# ci_status.sh [BRANCH] — the latest run of every workflow, per trigger
# event (push, schedule, workflow_dispatch, ...), on BRANCH (default
# master). Read-only (`gh run list`). Exit 1 when any latest completed run
# is red (not success/skipped) and not superseded: a red SCHEDULED
# workflow (weekly full-suite, nightly fuzz-soak, ...) is as much a red
# master as a red push.
#
# Superseded (shown, never red): a red schedule or workflow_dispatch row
# for which a successful FULL-SCOPE schedule/workflow_dispatch run of the
# SAME workflow, on a commit at or after the red one's (git ancestry in this
# checkout; a commit not present locally never supersedes), came after it
# — the manual verify-dispatch pattern. "After" means: started after a
# failure had finished (a success running alongside a failure is a flake,
# not a fix), or finished after a cancelled run (a cancel carries no
# verdict). Push rows are never superseded here: the next push is the
# latest push row. Full scope: a schedule run always; a dispatch only when
# its inputs equal the workflow's `workflow_dispatch` input defaults (in
# this checkout). GitHub does not report a run's inputs, so an
# input-bearing workflow puts them in its dispatch title (`run-name:
# "... dispatch inputs: k=v ..."`); a dispatch whose title does not show
# every input at its default (fuzz-soak hours=0.1, fuzz-newpath n=1, or no
# inputs in the title at all) supersedes nothing.
#
# Retired (shown, never red): a schedule row of a workflow whose file (in
# this checkout) no longer has an active `cron:` line (stdlib-gate's
# schedule was removed under BUG-0033).
#
# The coordinator runs it after every merge wave and at least daily
# (AGENTS.md §4). Columns: workflow, event, status/conclusion, age, run id,
# head sha. Runs are listed PER WORKFLOW (`gh run list --workflow`), plus
# one listing per repeat event (schedule, workflow_dispatch) the workflow
# declares, so a weekly scheduled red is never pushed out of the window by
# a busy push workflow. Test: test_ci_status.py feeds fixtures through
# CI_STATUS_RUNS_JSON (the merged `gh run list` JSON) and
# CI_STATUS_WORKFLOWS_JSON (the `gh workflow list` JSON) instead of
# calling gh, and drives the per-workflow listing through a fake `gh`.
set -euo pipefail
branch="${1:-master}"
root="$(git rev-parse --show-toplevel)"

wfs="${CI_STATUS_WORKFLOWS_JSON:-}"
if [ -z "$wfs" ]; then
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  wfs="$tmp/workflows.json"
  gh workflow list --all --json name,path >"$wfs"
fi

python3 - "${CI_STATUS_RUNS_JSON:-}" "$wfs" "$root" "$branch" <<'PY'
import json, re, subprocess, sys
from concurrent.futures import ThreadPoolExecutor

runs_path, wfs_path, root, branch = sys.argv[1:5]
workflows = json.load(open(wfs_path))
paths = {w["name"]: w["path"] for w in workflows}
PENDING = {"in_progress", "queued", "waiting", "pending", "requested"}
REPEAT = {"schedule", "workflow_dispatch"}
CRON = re.compile(r"^\s*-\s*cron:", re.M)
DISPATCH = re.compile(r"^\s*workflow_dispatch:", re.M)
FIELDS = "workflowName,event,status,conclusion,createdAt,updatedAt,databaseId,headSha,displayTitle"
TITLE_INPUTS = re.compile(r"dispatch inputs:((?:\s+[\w-]+=\S*)*)\s*$")


def workflow_text(wf):
    p = paths.get(wf)
    try:
        return open("%s/%s" % (root, p)).read() if p else ""
    except OSError:
        return ""


def gh_runs(path, *extra):
    out = subprocess.run(["gh", "run", "list", "--workflow", path.rsplit("/", 1)[-1],
                          "--branch", branch, "--json", FIELDS, *extra],
                         check=True, capture_output=True, text=True).stdout
    return json.loads(out or "[]")


def fetch_runs():
    """Every workflow's recent runs, listed per workflow (plus per declared
    repeat event), deduplicated by run id; the listings run concurrently."""
    calls = []
    for w in workflows:
        calls.append((w["path"], "--limit", "100"))
        if active_cron(w["name"]):
            calls.append((w["path"], "--event", "schedule", "--limit", "20"))
        if DISPATCH.search(workflow_text(w["name"])):
            calls.append((w["path"], "--event", "workflow_dispatch", "--limit", "20"))
    seen = {}
    with ThreadPoolExecutor(max_workers=8) as pool:
        for listing in pool.map(lambda c: gh_runs(*c), calls):
            for r in listing:
                seen[r["databaseId"]] = r
    return list(seen.values())


def dispatch_defaults(wf):
    """{input: default} of the workflow's workflow_dispatch inputs (a
    minimal block-YAML read of the trigger: no PyYAML dependency)."""
    lines = workflow_text(wf).splitlines()
    out, base, inputs_ind, cur = {}, None, None, None
    for ln in lines:
        stripped = ln.strip()
        if not stripped or stripped.startswith("#"):
            continue
        ind = len(ln) - len(ln.lstrip())
        if base is None:
            if stripped.startswith("workflow_dispatch:"):
                base = ind
            continue
        if ind <= base:
            break
        if inputs_ind is None:
            if stripped == "inputs:":
                inputs_ind = ind
            continue
        if ind <= inputs_ind:
            break
        key, _, val = stripped.partition(":")
        if cur is None or ind <= cur[1]:
            cur = (key.strip(), ind)
            out[cur[0]] = None  # no default: never equal to a value shown
        elif key.strip() == "default":
            out[cur[0]] = val.strip().strip("'\"")
    return out


def same_value(a, b):
    if a == b:
        return True
    try:
        return float(a) == float(b)
    except (TypeError, ValueError):
        return False


def full_scope(run):
    """A schedule run, or a dispatch whose title shows every input at its
    default (header comment)."""
    if run["event"] != "workflow_dispatch":
        return True
    defaults = dispatch_defaults(run["workflowName"])
    if not defaults:
        return True
    m = TITLE_INPUTS.search(run.get("displayTitle") or "")
    if not m:
        return False
    shown = dict(kv.split("=", 1) for kv in m.group(1).split())
    return all(k in shown and same_value(shown[k], v) for k, v in defaults.items())


def state(r):
    return r["conclusion"] if r["status"] == "completed" else r["status"]


def ancestor(a, b):
    """a is b or an ancestor of b (False when either is not local)."""
    return a == b or subprocess.run(["git", "-C", root, "merge-base", "--is-ancestor", a, b],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def active_cron(wf):
    return bool(CRON.search(workflow_text(wf)))


def superseder(red):
    """The run that supersedes this red one, or None (header comment)."""
    if red["event"] not in REPEAT:
        return None
    cancelled = state(red) == "cancelled"
    for s in sorted(runs, key=lambda r: r["createdAt"], reverse=True):  # newest first
        if (s["workflowName"] != red["workflowName"] or s["event"] not in REPEAT
                or s["status"] != "completed" or s["conclusion"] != "success"
                or not full_scope(s)):
            continue
        later = (s["updatedAt"] > red["updatedAt"]) if cancelled else (s["createdAt"] > red["updatedAt"])
        if later and ancestor(red["headSha"], s["headSha"]):
            return s
    return None


runs = json.load(open(runs_path)) if runs_path else fetch_runs()
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
