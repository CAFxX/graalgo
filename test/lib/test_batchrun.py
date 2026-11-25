#!/usr/bin/env python3
"""Self-test of test/lib/batchrun.sh's driver logic (run by `make check`;
no JVM: a fake runner plays BundleBatchMain). Every queued program gets a
status; a program the batch did not END (the JVM died) and every later one
run through the caller's per-process leg, in order; GRAALGO_BATCH=off never
starts the runner; a field with a tab is refused; batch_isolated picks the
process-global mutators. The real runner's planted failures are
test/m1r/batch.sh (make test-goroutine-modes)."""
import os
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

# Ends programs 1..$DIE_AT-1 with status 10+id, STARTs $DIE_AT and exits
# 3 (a dead JVM); DIE_AT=0 finishes them all; STARTED marks a launch.
FAKE_RUNNER = """#!/bin/bash
[ "$1" = --batch ] || exit 99
echo started >> "$STARTED"
while IFS=$'\\t' read -r id cwd argv0 in out err to rest; do
    echo "START $id" >> "$3"
    [ "$id" = "${DIE_AT:-0}" ] && exit 3
    echo "batch $id $rest" > "$out"
    echo "END $id $((10 + id))" >> "$3"
done < "$2"
"""

DRIVER = """
set -uo pipefail
. "$LIB/infra.sh"
. "$LIB/batchrun.sh"
proc() { echo "proc $1" >> "$TMP/proc.log"; echo "proc $1" > "$TMP/$1.out"; return $((20 + $1)); }
batch_init
for id in 1 2 3 4; do batch_add "$id" "$TMP" ./p /dev/null "$TMP/$id.out" "$TMP/$id.err" 5 x "y z"; done
batch_run "$RUNNER" proc
for id in 1 2 3 4; do echo "$id ${BATCH_STATUS[$id]} $(cat "$TMP/$id.out")"; done
"""


class BatchRunTest(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.runner = os.path.join(self.d, "runner")
        with open(self.runner, "w") as f:
            f.write(FAKE_RUNNER)
        os.chmod(self.runner, 0o755)
        self.tmp = os.path.join(self.d, "scratch")
        os.mkdir(self.tmp)
        self.started = os.path.join(self.d, "started")

    def tearDown(self):
        subprocess.call(["rm", "-rf", self.d])

    def run_driver(self, driver=DRIVER, **env):
        e = dict(os.environ, LIB=HERE, TMP=self.tmp, RUNNER=self.runner, STARTED=self.started)
        e.pop("GRAALGO_BATCH", None)
        e.update(env)
        return subprocess.run(["bash", "-c", driver], env=e, capture_output=True, text=True)

    def proc_log(self):
        p = os.path.join(self.tmp, "proc.log")
        if not os.path.exists(p):
            return []
        with open(p) as f:
            return f.read().split("\n")[:-1]

    def test_all_in_the_batch(self):
        out = self.run_driver()
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertEqual(out.stdout.splitlines(),
                         ["1 11 batch 1 x\ty z", "2 12 batch 2 x\ty z", "3 13 batch 3 x\ty z",
                          "4 14 batch 4 x\ty z"])
        self.assertEqual(self.proc_log(), [])
        self.assertIn("batch: 4 programs in one JVM", out.stderr)

    def test_dead_jvm_reruns_the_rest_per_process(self):
        out = self.run_driver(DIE_AT="3")
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertEqual(out.stdout.splitlines(),
                         ["1 11 batch 1 x\ty z", "2 12 batch 2 x\ty z", "3 23 proc 3", "4 24 proc 4"])
        self.assertEqual(self.proc_log(), ["proc 3", "proc 4"])
        self.assertIn("after 2 of 4 programs; ran the other 2 in their own processes", out.stderr)

    def test_runner_that_never_starts(self):
        out = self.run_driver(RUNNER="/nonexistent/runner")
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertEqual([l.split()[1] for l in out.stdout.splitlines()], ["21", "22", "23", "24"])

    def test_off_never_starts_the_runner(self):
        out = self.run_driver(GRAALGO_BATCH="off")
        self.assertEqual(out.returncode, 0, out.stderr)
        self.assertEqual(self.proc_log(), ["proc 1", "proc 2", "proc 3", "proc 4"])
        self.assertFalse(os.path.exists(self.started))
        self.assertNotIn("batch:", out.stderr)

    def test_save(self):
        save = os.path.join(self.d, "save")
        out = self.run_driver(BATCH_SAVE=save)
        self.assertEqual(out.returncode, 0, out.stderr)
        names = sorted(os.listdir(save))
        self.assertIn("scratch-1.4.status", names)
        with open(os.path.join(save, "scratch-1.2.status")) as f:
            self.assertEqual(f.read(), "12\n")

    def test_tab_in_a_field_is_refused(self):
        bad = DRIVER.replace('5 x "y z"', '5 $\'a\\tb\'')
        out = self.run_driver(bad)
        self.assertEqual(out.returncode, 3, out.stdout + out.stderr)
        self.assertIn("a tab or newline", out.stdout + out.stderr)

    def test_isolated(self):
        cases = {
            "package main\nfunc main() { println(1) }\n": False,
            "syscall.Setrlimit(syscall.RLIMIT_NOFILE, &l)\n": True,
            "signal.Notify(c, os.Interrupt)\n": True,
            "syscall.Kill(syscall.Getpid(), syscall.SIGTERM)\n": True,
            "syscall.Umask(022)\n": True,
            "//go:linkname exit syscall.Exit\n": True,
            "syscall.Exited()\n": False,
        }
        for i, (src, want) in enumerate(cases.items()):
            p = os.path.join(self.tmp, "p%d.go" % i)
            with open(p, "w") as f:
                f.write(src)
            out = self.run_driver('. "$LIB/infra.sh"; . "$LIB/batchrun.sh"; batch_isolated "%s"' % p)
            self.assertEqual(out.returncode == 0, want, src)


if __name__ == "__main__":
    unittest.main()
