#!/usr/bin/env python3
"""Regression pins for NEW-P1-7: fuzz soak dead on deleted phases/ paths.

The harness scripts (run_batch.py, diff_one.sh, triage.py,
run_fuzz_native.sh, run_soak.sh) used to resolve the repo root through
the deleted phases/phase3/fuzzing layout. They now resolve it from
their own location; these tests pin that resolution with GR unset, so
a future move that breaks the walk-up fails here instead of silently
killing the overnight soak.

Run: python3 tools/fuzz-soak/harness/test_harness_paths.py
No JVM, no build, no network: every test is cheap. The diff_one test
only exercises the fail-fast / gc-build-fail paths, never the graalgo
run (the program deliberately fails `go build`).
"""

import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

# Importing run_batch/triage as modules must not litter the harness dir
# with __pycache__ (untracked debris per the repo no-debris rule).
sys.dont_write_bytecode = True

HARNESS_DIR = Path(__file__).resolve().parent
# parents of the harness dir: [0]=fuzz-soak, [1]=tools, [2]=repo root.
REPO_ROOT = HARNESS_DIR.parents[2]
FUZZ_DIR = REPO_ROOT / "tools" / "fuzz-soak"

# Files the soak entry points touch; none may reference the deleted layout.
HARNESS_FILES = [
    FUZZ_DIR / "harness" / "run_batch.py",
    FUZZ_DIR / "harness" / "diff_one.sh",
    FUZZ_DIR / "harness" / "triage.py",
    FUZZ_DIR / "run_soak.sh",
    REPO_ROOT / "test" / "differential" / "fuzz" / "run_fuzz_native.sh",
]

STALE_MARKERS = ("phases/phase", "phases\\phase")


def assert_no_deleted_phases_paths(text, where):
    """The tripwire: any reference to the deleted phases/ layout fails."""
    for marker in STALE_MARKERS:
        assert marker not in text, (
            f"{where}: stale deleted-path reference {marker!r}")


def load_module_unset_gr(path, name):
    """Import a harness .py with GR removed from the environment."""
    saved = os.environ.pop("GR", None)
    try:
        spec = importlib.util.spec_from_file_location(name, str(path))
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
        return mod
    finally:
        if saved is not None:
            os.environ["GR"] = saved


def eval_shell_assignment(script, var, here_dir, extra_lines=()):
    """Execute one assignment line taken verbatim from `script` with
    HERE bound to the script's real directory, and return $var.

    This pins the file's own walk-up expression (not a copy of it): if
    someone edits the `../..` depth, the evaluated value moves and the
    test fails.
    """
    lines = list(extra_lines)
    for raw in Path(script).read_text().splitlines():
        if raw.startswith(var + "="):
            lines.append(raw)
            break
    else:
        raise AssertionError(f"{script}: no {var}= assignment found")
    src = f'HERE="{here_dir}"\n' + "\n".join(lines) + f'\nprintf "%s" "${var}"\n'
    r = subprocess.run(["bash", "-c", src], capture_output=True, text=True,
                       timeout=30)
    assert r.returncode == 0, f"{script}: walk-up eval failed: {r.stderr}"
    return r.stdout


class TestGrResolution(unittest.TestCase):
    def test_run_batch_gr_resolves_to_repo_root_with_gr_unset(self):
        mod = load_module_unset_gr(HARNESS_DIR / "run_batch.py",
                                   "run_batch_pin")
        self.assertEqual(Path(mod.GR), REPO_ROOT)
        self.assertEqual(Path(mod.FUZZ), FUZZ_DIR)
        for rel in ("harness/run_batch.py", "harness/diff_one.sh",
                    "harness/triage.py", "gen/generator.py"):
            self.assertTrue((FUZZ_DIR / rel).is_file(), rel)

    def test_triage_gr_resolves_to_repo_root_with_gr_unset(self):
        mod = load_module_unset_gr(HARNESS_DIR / "triage.py", "triage_pin")
        self.assertEqual(Path(mod.GR), REPO_ROOT)
        self.assertEqual(Path(mod.FUZZ), FUZZ_DIR)

    def test_no_deleted_phases_paths_in_harness(self):
        for path in HARNESS_FILES:
            assert_no_deleted_phases_paths(path.read_text(), str(path))

    def test_no_deleted_phases_positive_control(self):
        # The tripwire must still fire on the old layout (a gate that
        # cannot fail is a rubber stamp, not a regression pin).
        with self.assertRaises(AssertionError):
            assert_no_deleted_phases_paths(
                "make -C phases/phase3 main", "synthetic stale reference")

    def test_run_batch_build_step_builds_gofrontend(self):
        # diff_one.sh fail-fasts with HARNESS_ERROR when gofrontend is
        # missing, so the batch build must produce it. Root `make build`
        # does: the truffle module's `main` target depends on
        # $(GOFRONTEND) (built via internal/frontend/build.sh). Pin both
        # ends — a future "simplification" of either side that drops the
        # gofrontend build must fail here, not silently HARNESS_ERROR
        # every program in the overnight soak.
        src = (HARNESS_DIR / "run_batch.py").read_text()
        self.assertIn("make -C {GR} build", src)
        self.assertNotIn("phases/phase3", src)
        truffle_mk = (REPO_ROOT / "internal" / "backend" / "truffle"
                      / "Makefile").read_text()
        self.assertIn("main: ", truffle_mk)
        main_line = next(l for l in truffle_mk.splitlines()
                         if l.startswith("main:"))
        self.assertIn("$(GOFRONTEND)", main_line)
        self.assertIn("$(GOFRONTEND):", truffle_mk)
        self.assertIn("./build.sh build", truffle_mk)

    def test_shell_repo_root_walkups(self):
        # run_soak.sh: HERE=tools/fuzz-soak, REPO_ROOT is two levels up.
        self.assertEqual(
            eval_shell_assignment(FUZZ_DIR / "run_soak.sh", "REPO_ROOT",
                                  FUZZ_DIR),
            str(REPO_ROOT))
        # run_fuzz_native.sh: DH_ROOT is one level up from the fuzz dir,
        # REPO_ROOT two more levels up from DH_ROOT.
        fuzz_dir = REPO_ROOT / "test" / "differential" / "fuzz"
        dh_line = next(
            l for l in (fuzz_dir / "run_fuzz_native.sh").read_text()
            .splitlines() if l.startswith("DH_ROOT="))
        self.assertEqual(
            eval_shell_assignment(fuzz_dir / "run_fuzz_native.sh",
                                  "REPO_ROOT", fuzz_dir,
                                  extra_lines=(dh_line,)),
            str(REPO_ROOT))
        # diff_one.sh: GR is three levels up from the harness dir.
        self.assertEqual(
            eval_shell_assignment(HARNESS_DIR / "diff_one.sh", "GR",
                                  HARNESS_DIR),
            str(REPO_ROOT))

    def test_diff_one_gr_unset_emits_repo_root_paths(self):
        # Behavioural pin on diff_one.sh's own GR walk-up, with GR unset.
        # The program deliberately fails `go build`, so the script never
        # reaches the JVM: on an unbuilt tree it fail-fasts with
        # HARNESS_ERROR naming the repo-root launcher; on a built tree
        # the fail-fast passes and the record is GC_BUILD_FAIL.
        with tempfile.TemporaryDirectory() as td:
            prog = os.path.join(td, "trivial.go")
            Path(prog).write_text(
                "package main\n\nfunc main() { nosuchfunc() }\n")
            workdir = os.path.join(td, "work")
            env = {k: v for k, v in os.environ.items()
                   if k not in ("GR", "JAVA_BIN", "GRAALVM_HOME")}
            r = subprocess.run(
                ["bash", str(HARNESS_DIR / "diff_one.sh"), prog, workdir,
                 "1"],
                capture_output=True, text=True, env=env, timeout=60)
            rec = json.loads(r.stdout.strip().split("\n")[-1])
            if rec["result"] == "HARNESS_ERROR":
                # The missing pieces are named with repo-root paths:
                # that proves GR resolved to the repo root (a walk-up
                # that undercounts by one would name tools/... instead).
                # (The launcher itself is NOT named: it exists, so it
                # is not among the missing pieces.)
                self.assertIn(
                    str(REPO_ROOT / "internal" / "backend" / "interop"
                        / "out" / "classes"),
                    rec["detail"])
                self.assertNotIn("tools/fuzz-soak/internal", rec["detail"])
            else:
                self.assertEqual(rec["result"], "GC_BUILD_FAIL")


if __name__ == "__main__":
    unittest.main()
