#!/usr/bin/env python3
"""Tests for ci_status.sh: fixture `gh run list` / `gh workflow list` JSON
(CI_STATUS_RUNS_JSON / CI_STATUS_WORKFLOWS_JSON) against a throwaway git
repo with commits A -> B -> C. A red schedule/dispatch run superseded by a
later green run of the same workflow on a commit at or after it is not
red; every planted unsuperseded red exits 1. A dispatch of an input-bearing
workflow supersedes only when its title shows every input at its default
(a reduced-scope dispatch proves nothing about the full run), and the live
listing is per workflow (a fake `gh` on PATH), so a weekly scheduled red
outside a busy workflow's newest runs is still seen.

Run: python3 .github/scripts/test_ci_status.py
"""

import atexit
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "ci_status.sh"


def git(repo, *args):
    return subprocess.run(["git", "-C", str(repo), *args], check=True, capture_output=True,
                          text=True).stdout.strip()


def make_repo():
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    git(d, "init", "-q")
    wf = d / ".github" / "workflows"
    wf.mkdir(parents=True)
    (wf / "soak.yml").write_text("on:\n  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n")
    (wf / "other.yml").write_text("on:\n  schedule:\n    - cron: '0 2 * * *'\n")
    (wf / "old.yml").write_text("on:\n  workflow_dispatch:\n  # - cron: '0 3 * * *'\n")
    (wf / "fuzz.yml").write_text(
        "on:\n  schedule:\n    - cron: '0 1 * * *'\n  workflow_dispatch:\n    inputs:\n"
        "      hours:\n        # a comment: default: '9'\n        description: 'Soak hours: max 4'\n"
        "        required: false\n        default: '4'\n      n:\n        default: 500\n"
        "  push:\n    paths:\n      - 'x/**'\n")
    shas = []
    for name in "ABC":
        (d / "f").write_text(name)
        git(d, "add", "-A")
        git(d, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", name)
        shas.append(git(d, "rev-parse", "HEAD"))
    return d, dict(zip("ABC", shas))


REPO, SHA = make_repo()
WFS = [{"name": "soak", "path": ".github/workflows/soak.yml"},
       {"name": "other", "path": ".github/workflows/other.yml"},
       {"name": "old", "path": ".github/workflows/old.yml"},
       {"name": "fuzz", "path": ".github/workflows/fuzz.yml"}]
_ids = iter(range(1000, 2000))


def run(wf, event, concl, commit, created, updated, status="completed", title=""):
    """A `gh run list` row; times are hours of one day (minutes optional:
    created may be a (hour, minute) pair)."""
    ch, cm = created if isinstance(created, tuple) else (created, 0)
    return {"workflowName": wf, "event": event, "status": status,
            "conclusion": concl if status == "completed" else "",
            "createdAt": "2026-11-24T%02d:%02d:00Z" % (ch, cm),
            "updatedAt": "2026-11-24T%02d:30:00Z" % updated,
            "databaseId": next(_ids), "headSha": SHA.get(commit, commit),
            "displayTitle": title}


FAKE_GH = """#!/usr/bin/env python3
import json, os, sys
a = sys.argv[1:]
open(os.environ["FAKE_GH_LOG"], "a").write(" ".join(a) + "\\n")
assert a[:2] == ["run", "list"], a
opt = {a[i]: a[i + 1] for i in range(2, len(a) - 1, 2)}
wfs = {w["path"].rsplit("/", 1)[-1]: w["name"] for w in json.load(open(os.environ["FAKE_GH_WFS"]))}
rows = [r for r in json.load(open(os.environ["FAKE_GH_RUNS"]))
        if r["workflowName"] == wfs[opt["--workflow"]]
        and r["event"] == opt.get("--event", r["event"])]
rows.sort(key=lambda r: r["createdAt"], reverse=True)
print(json.dumps(rows[:int(opt["--limit"])]))
"""


def status(runs, live=False):
    """Run ci_status.sh on fixture runs: as the merged listing
    (CI_STATUS_RUNS_JSON), or live=True through a fake `gh` that answers
    `gh run list --workflow/--event/--limit` from them."""
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    (d / "runs.json").write_text(json.dumps(runs))
    (d / "wfs.json").write_text(json.dumps(WFS))
    env = dict(os.environ, CI_STATUS_WORKFLOWS_JSON=str(d / "wfs.json"))
    if live:
        (d / "bin").mkdir()
        (d / "bin" / "gh").write_text(FAKE_GH)
        (d / "bin" / "gh").chmod(0o755)
        env.update(PATH="%s:%s" % (d / "bin", env["PATH"]), FAKE_GH_LOG=str(d / "gh.log"),
                   FAKE_GH_WFS=str(d / "wfs.json"), FAKE_GH_RUNS=str(d / "runs.json"))
        env.pop("CI_STATUS_RUNS_JSON", None)
    else:
        env["CI_STATUS_RUNS_JSON"] = str(d / "runs.json")
    p = subprocess.run(["bash", str(SCRIPT)], cwd=REPO, env=env, capture_output=True, text=True)
    status.gh_log = (d / "gh.log").read_text() if live and (d / "gh.log").exists() else ""
    return p.returncode, p.stdout + p.stderr


class CiStatus(unittest.TestCase):
    def green(self, runs, live=False):
        rc, out = status(runs, live)
        self.assertEqual(rc, 0, out)
        self.assertNotIn("RED", out)
        return out

    def red(self, runs, row, live=False):
        rc, out = status(runs, live)
        self.assertEqual(rc, 1, out)
        self.assertIn("RED", out)
        self.assertIn(row, out.split("RED", 1)[1])
        return out

    def test_all_green(self):
        self.green([run("soak", "schedule", "success", "A", 1, 2),
                    run("soak", "push", "success", "B", 3, 4)])

    def test_failed_schedule_superseded_by_later_dispatch(self):
        sched = run("soak", "schedule", "failure", "A", 1, 2)
        fix = run("soak", "workflow_dispatch", "success", "B", 3, 4)
        out = self.green([sched, fix])
        self.assertIn("superseded(failure)<-%d" % fix["databaseId"], out)

    def test_failed_schedule_superseded_same_commit(self):
        self.green([run("soak", "schedule", "failure", "B", 1, 2),
                    run("soak", "workflow_dispatch", "success", "B", 3, 4)])

    def test_failed_dispatch_superseded_by_later_schedule(self):
        self.green([run("soak", "workflow_dispatch", "failure", "A", 1, 2),
                    run("soak", "schedule", "success", "C", 3, 4)])

    def test_cancelled_dispatch_superseded_by_success_finishing_later(self):
        # The fuzz-soak pattern: a duplicate dispatch cancelled at once
        # while the earlier one on the same commit ran on to success.
        ok = run("soak", "workflow_dispatch", "success", "B", 1, 5)
        dup = run("soak", "workflow_dispatch", "cancelled", "B", 2, 2)
        out = self.green([ok, dup])
        self.assertIn("superseded(cancelled)<-%d" % ok["databaseId"], out)

    def test_retired_schedule_not_red(self):
        self.assertIn("retired(failure)",
                      self.green([run("old", "schedule", "failure", "A", 1, 2)]))

    def test_pending_not_red(self):
        self.green([run("soak", "schedule", "", "A", 1, 2, status="in_progress")])

    # Planted unsuperseded reds: each must exit 1.

    def test_unsuperseded_schedule_failure(self):
        self.red([run("soak", "schedule", "failure", "A", 1, 2)], "soak [schedule] failure")

    def test_success_on_older_commit_does_not_supersede(self):
        self.red([run("soak", "schedule", "failure", "B", 1, 2),
                  run("soak", "workflow_dispatch", "success", "A", 3, 4)],
                 "soak [schedule] failure")

    def test_concurrent_success_does_not_supersede_failure(self):
        # Started before the failure finished: a flake, not a fix.
        self.red([run("soak", "workflow_dispatch", "success", "B", 1, 4),
                  run("soak", "schedule", "failure", "B", 2, 3)],
                 "soak [schedule] failure")

    def test_cancel_after_success_finished_stays_red(self):
        self.red([run("soak", "workflow_dispatch", "success", "B", 1, 1),
                  run("soak", "schedule", "cancelled", "B", 2, 3)],
                 "soak [schedule] cancelled")

    def test_other_workflow_does_not_supersede(self):
        self.red([run("soak", "schedule", "failure", "A", 1, 2),
                  run("other", "workflow_dispatch", "success", "C", 3, 4)],
                 "soak [schedule] failure")

    def test_push_success_does_not_supersede_schedule(self):
        self.red([run("soak", "schedule", "failure", "A", 1, 2),
                  run("soak", "push", "success", "C", 3, 4)],
                 "soak [schedule] failure")

    def test_red_push_never_superseded(self):
        self.red([run("soak", "push", "failure", "A", 1, 2),
                  run("soak", "workflow_dispatch", "success", "C", 3, 4)],
                 "soak [push] failure")

    def test_unknown_commit_does_not_supersede(self):
        self.red([run("soak", "schedule", "failure", "A", 1, 2),
                  run("soak", "workflow_dispatch", "success", "f" * 40, 3, 4)],
                 "soak [schedule] failure")

    def test_one_red_among_superseded_still_fails(self):
        out = self.red([run("soak", "schedule", "failure", "A", 1, 2),
                        run("soak", "workflow_dispatch", "success", "B", 3, 4),
                        run("other", "schedule", "failure", "C", 5, 6)],
                       "other [schedule] failure")
        self.assertNotIn("soak [schedule]", out.split("RED", 1)[1])

    # Dispatch scope: only a dispatch at the workflow's input defaults
    # supersedes (its title carries the inputs: `run-name:`).

    def test_default_input_dispatch_supersedes(self):
        sched = run("fuzz", "schedule", "failure", "A", 1, 2)
        fix = run("fuzz", "workflow_dispatch", "success", "B", 3, 4,
                  title="fuzz dispatch inputs: hours=4.0 n=500")
        self.assertIn("superseded(failure)<-%d" % fix["databaseId"], self.green([sched, fix]))

    def test_reduced_scope_dispatch_does_not_supersede(self):
        self.red([run("fuzz", "schedule", "failure", "A", 1, 2),
                  run("fuzz", "workflow_dispatch", "success", "B", 3, 4,
                      title="fuzz dispatch inputs: hours=0.1 n=500")],
                 "fuzz [schedule] failure")

    def test_dispatch_missing_an_input_does_not_supersede(self):
        self.red([run("fuzz", "schedule", "failure", "A", 1, 2),
                  run("fuzz", "workflow_dispatch", "success", "B", 3, 4,
                      title="fuzz dispatch inputs: hours=4")],
                 "fuzz [schedule] failure")

    def test_dispatch_without_inputs_in_title_does_not_supersede(self):
        self.red([run("fuzz", "schedule", "failure", "A", 1, 2),
                  run("fuzz", "workflow_dispatch", "success", "B", 3, 4, title="fuzz")],
                 "fuzz [schedule] failure")

    def test_later_schedule_supersedes_reduced_dispatch_red(self):
        self.green([run("fuzz", "workflow_dispatch", "failure", "A", 1, 2,
                        title="fuzz dispatch inputs: hours=0.1 n=1"),
                    run("fuzz", "schedule", "success", "C", 3, 4)])

    # Live listing: per workflow, plus per declared repeat event.

    def test_weekly_red_outside_busy_window_is_seen(self):
        # 150 newer green pushes of the same workflow: a single newest-N
        # listing (the old global `--limit 200`, or per workflow --limit
        # 100) would never see the scheduled red.
        runs = [run("soak", "schedule", "failure", "A", 1, 2)]
        runs += [run("soak", "push", "success", "C", (3 + i // 60, i % 60), 23) for i in range(150)]
        runs += [run("other", "schedule", "success", "C", 4, 5)]
        self.red(runs, "soak [schedule] failure", live=True)
        log = status.gh_log
        for wf in ("soak.yml", "other.yml", "old.yml", "fuzz.yml"):
            self.assertIn("--workflow %s" % wf, log)
        self.assertIn("--workflow soak.yml --branch master --json", log)
        self.assertRegex(log, r"--workflow soak.yml .*--event schedule")

    def test_live_listing_green(self):
        self.green([run("soak", "schedule", "success", "A", 1, 2),
                    run("fuzz", "schedule", "success", "B", 3, 4)], live=True)


if __name__ == "__main__":
    unittest.main()
