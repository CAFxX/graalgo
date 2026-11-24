#!/usr/bin/env python3
"""Tests for ci_status.sh: fixture `gh run list` / `gh workflow list` JSON
(CI_STATUS_RUNS_JSON / CI_STATUS_WORKFLOWS_JSON) against a throwaway git
repo with commits A -> B -> C. A red schedule/dispatch run superseded by a
later green run of the same workflow on a commit at or after it is not
red; every planted unsuperseded red exits 1.

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
       {"name": "old", "path": ".github/workflows/old.yml"}]
_ids = iter(range(1000, 2000))


def run(wf, event, concl, commit, created, updated, status="completed"):
    """A `gh run list` row; times are hours of one day."""
    return {"workflowName": wf, "event": event, "status": status,
            "conclusion": concl if status == "completed" else "",
            "createdAt": "2026-11-24T%02d:00:00Z" % created,
            "updatedAt": "2026-11-24T%02d:30:00Z" % updated,
            "databaseId": next(_ids), "headSha": SHA.get(commit, commit)}


def status(runs):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    (d / "runs.json").write_text(json.dumps(runs))
    (d / "wfs.json").write_text(json.dumps(WFS))
    env = dict(os.environ, CI_STATUS_RUNS_JSON=str(d / "runs.json"),
               CI_STATUS_WORKFLOWS_JSON=str(d / "wfs.json"))
    p = subprocess.run(["bash", str(SCRIPT)], cwd=REPO, env=env, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


class CiStatus(unittest.TestCase):
    def green(self, runs):
        rc, out = status(runs)
        self.assertEqual(rc, 0, out)
        self.assertNotIn("RED", out)
        return out

    def red(self, runs, row):
        rc, out = status(runs)
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


if __name__ == "__main__":
    unittest.main()
