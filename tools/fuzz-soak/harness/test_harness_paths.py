#!/usr/bin/env python3
"""Regression pins for NEW-P1-7: fuzz soak dead on deleted phases/ paths,
and for REVIEW-2026-11-06-10 R-P1-13 residuals: the GHA driver + image
still on phases/, and the overnight loop swallowing batch failures.

The harness scripts (run_batch.py, diff_one.sh, triage.py,
run_fuzz_native.sh, run_soak.sh) used to resolve the repo root through
the deleted phases/phase3/fuzzing layout. They now resolve it from
their own location; these tests pin that resolution with GR unset, so
a future move that breaks the walk-up fails here instead of silently
killing the overnight soak.

Run: python3 tools/fuzz-soak/harness/test_harness_paths.py
No JVM, no build, no network: every test is cheap. The diff_one test
only exercises the fail-fast / gc-build-fail paths, never the graalgo
run (the program deliberately fails `go build`). The run_soak tests
run a copied harness tree with a stubbed run_batch.py, never the real
build or JVM.
"""

import importlib.util
import json
import os
import shutil
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
    # R-P1-13 residuals: the GHA driver + image bake the same layout.
    FUZZ_DIR / "gha" / "driver" / "fuzz_driver.py",
    FUZZ_DIR / "gha" / "Dockerfile",
    FUZZ_DIR / "gha" / "README.md",
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


class TestGhaDriver(unittest.TestCase):
    def _load_driver(self, graalgo_home):
        """Import fuzz_driver.py with GRAALGO_HOME set/unset as given."""
        saved = os.environ.pop("GRAALGO_HOME", None)
        try:
            if graalgo_home is not None:
                os.environ["GRAALGO_HOME"] = graalgo_home
            spec = importlib.util.spec_from_file_location(
                "fuzz_driver_pin",
                str(FUZZ_DIR / "gha" / "driver" / "fuzz_driver.py"))
            mod = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(mod)
            return mod
        finally:
            os.environ.pop("GRAALGO_HOME", None)
            if saved is not None:
                os.environ["GRAALGO_HOME"] = saved

    def test_driver_fuzz_resolves_to_tools_fuzz_soak(self):
        # Image default: /opt/graalgo is the repo root in the image.
        mod = self._load_driver(None)
        self.assertEqual(Path(mod.FUZZ),
                         Path("/opt/graalgo/tools/fuzz-soak"))
        # Against this repo the harness files it needs must exist.
        mod = self._load_driver(str(REPO_ROOT))
        self.assertEqual(Path(mod.FUZZ), FUZZ_DIR)
        for rel in ("gen/generator.py", "harness/diff_one.sh"):
            self.assertTrue((FUZZ_DIR / rel).is_file(), rel)

    def test_driver_fail_fasts_on_stale_image_layout(self):
        # A tarball built from the old layout must fail at shard start,
        # not fail every seed identically with a hidden cause.
        src = (FUZZ_DIR / "gha" / "driver" / "fuzz_driver.py").read_text()
        self.assertIn("FUZZ layout mismatch", src)
        self.assertIn("sys.exit(2)", src)

    def test_gha_image_prebuild_uses_root_make_build(self):
        # The image Go is /usr/local/go/bin/go; the Makefiles default
        # GO to ~/tools/go/bin/go, which does not exist in the image —
        # the pre-build must pass GO explicitly, or `make build` would
        # fail to build gofrontend and every shard would HARNESS_ERROR.
        src = (FUZZ_DIR / "gha" / "Dockerfile").read_text()
        self.assertIn("make build GO=/usr/local/go/bin/go", src)
        self.assertNotIn("phases/phase3", src)


class TestRunBatchExitCodes(unittest.TestCase):
    def test_exit_code_contract(self):
        # 0 clean, 1 divergences found, 2 the batch could not run
        # (build/generator failure, HARNESS_ERROR). The overnight loop
        # in run_soak.sh distinguishes 1 from 2; a single exit code
        # for both would conflate "found bugs" with "harness broken".
        src = (HARNESS_DIR / "run_batch.py").read_text()
        self.assertIn("Exit codes", src)
        self.assertIn("sys.exit(2)", src)
        self.assertIn("return 2", src)
        self.assertIn("HARNESS_ERROR", src)
        # No bare sys.exit(1) left for infra failures: build and
        # generator failures must exit 2.
        self.assertNotIn("sys.exit(1)", src)

    def test_zero_programs_exits_two(self):
        # --seeds 5:5 (empty) and 8:3 (reversed) select zero programs:
        # the batch must fail loudly (exit 2, infrastructure failure),
        # not run zero differentials and report a clean exit 0. The
        # guard sits before the backend build, so a stub `make` on PATH
        # must never be invoked — that is what makes this test
        # falsifying on unpatched code: there, `make` IS invoked and
        # the run ends 0 after comparing nothing. GR points at an
        # empty temp dir so the test stays cheap either way (no build,
        # no JVM, no generator).
        td = tempfile.mkdtemp(prefix="zero-programs-pin-")
        try:
            shim_dir = Path(td) / "shims"
            shim_dir.mkdir()
            make_log = Path(td) / "make-invoked.log"
            (shim_dir / "make").write_text(
                "#!/bin/bash\n"
                f"echo \"make invoked: $*\" >> \"{make_log}\"\n"
                "exit 0\n")
            os.chmod(shim_dir / "make", 0o755)
            env = {
                "PATH": f"{shim_dir}:{os.environ['PATH']}",
                "HOME": td,
                "GR": td,
            }
            for seeds in ("5:5", "8:3"):
                r = subprocess.run(
                    [sys.executable, str(HARNESS_DIR / "run_batch.py"),
                     "--seeds", seeds, "--out", str(Path(td) / "out")],
                    capture_output=True, text=True, env=env, timeout=60)
                self.assertEqual(r.returncode, 2, seeds)
                self.assertIn("empty seed range", r.stdout + r.stderr)
            self.assertFalse(make_log.exists(),
                             "run_batch.py invoked `make` for a zero-"
                             "program batch — the guard must fire first")
        finally:
            shutil.rmtree(td, ignore_errors=True)


# Behavioural pins for the run_soak.sh failure semantics (R-P1-13):
# the single-batch slot must propagate the batch verdict after
# minimizing, and the overnight loop must record failures and exit
# nonzero instead of printing "failed; continuing" and exiting 0.
# The tests run a COPIED harness tree with run_batch.py replaced by
# a stub (no build, no JVM, no generator), so they are cheap.
STUB_RUN_BATCH = """#!/usr/bin/env python3
import os, sys
print(f"stub run_batch: exiting {os.environ.get('STUB_RC', '0')}",
      flush=True)
sys.exit(int(os.environ.get("STUB_RC", "0")))
"""

# Fake `date +%s` on PATH: the first two calls return T0, later calls
# T0+3601. run_soak.sh calls date once for START_TS and once per loop
# check, so with --hours 1 the loop runs exactly one batch
# (T0 < T0+3600) and then stops.
FAKE_DATE = """#!/bin/bash
count_file="$FAKE_DATE_COUNT"
c=$(cat "$count_file" 2>/dev/null || echo 0)
c=$((c + 1))
printf '%s' "$c" > "$count_file"
if [ "$c" -le 2 ]; then printf '1700000000\\n'; else printf '1700003601\\n'; fi
"""


class TestRunSoakFailureSemantics(unittest.TestCase):
    def _run_soak_copy(self, stub_rc, args):
        """Run run_soak.sh from a copied tree with a stubbed
        run_batch.py. Returns (returncode, stdout, outdir, tempdir);
        the caller must rmtree the temp dir (see the tests below)."""
        td = tempfile.mkdtemp(prefix="soak-pin-")
        fuzz_copy = Path(td) / "tools" / "fuzz-soak"
        shutil.copytree(FUZZ_DIR, fuzz_copy,
                        ignore=shutil.ignore_patterns(
                            "findings", "__pycache__"))
        (fuzz_copy / "harness" / "run_batch.py").write_text(STUB_RUN_BATCH)
        shim_dir = Path(td) / "shims"
        shim_dir.mkdir()
        (shim_dir / "date").write_text(FAKE_DATE)
        os.chmod(shim_dir / "date", 0o755)
        outdir = Path(td) / "out"
        env = {
            "PATH": f"{shim_dir}:{os.environ['PATH']}",
            "HOME": td,  # GOCACHE/TMPDIR defaults land in the temp dir
            "STUB_RC": str(stub_rc),
            "FAKE_DATE_COUNT": str(Path(td) / "date-count"),
        }
        r = subprocess.run(
            ["bash", str(fuzz_copy / "run_soak.sh")] + args
            + ["--out", str(outdir)],
            capture_output=True, text=True, env=env, timeout=60)
        return r, outdir, td

    def _assert_soak_report(self, outdir, failed, divergent):
        rep = json.loads((outdir / "soak-report.json").read_text())
        self.assertEqual(rep["failed_batches"], failed)
        self.assertEqual(rep["divergent_batches"], divergent)

    def test_single_batch_propagates_infra_failure(self):
        r, outdir, td = self._run_soak_copy(2, ["--seeds", "0:1"])
        try:
            self.assertEqual(r.returncode, 2)
            self.assertIn("batch verdict rc=2", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_single_batch_propagates_divergence(self):
        r, outdir, td = self._run_soak_copy(1, ["--seeds", "0:1"])
        try:
            self.assertEqual(r.returncode, 1)
            self.assertIn("batch verdict rc=1", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_single_batch_clean_exits_zero(self):
        r, outdir, td = self._run_soak_copy(0, ["--seeds", "0:1"])
        try:
            self.assertEqual(r.returncode, 0)
            self.assertIn("batch done:", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_overnight_loop_fails_loudly_on_infra_failure(self):
        r, outdir, td = self._run_soak_copy(2, ["--hours", "1"])
        try:
            self.assertEqual(r.returncode, 2)
            self.assertIn("LOUD: batch 0 FAILED to run", r.stdout)
            self.assertIn("SOAK FAILED", r.stdout)
            self._assert_soak_report(outdir, [0], [])
            marker = (outdir / "SOAK_FAILED").read_text()
            self.assertIn("infrastructure failure", marker)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_overnight_loop_fails_loudly_on_divergence(self):
        r, outdir, td = self._run_soak_copy(1, ["--hours", "1"])
        try:
            self.assertEqual(r.returncode, 1)
            self.assertIn("LOUD: batch 0 found divergences", r.stdout)
            self.assertIn("SOAK FAILED", r.stdout)
            self._assert_soak_report(outdir, [], [0])
            marker = (outdir / "SOAK_FAILED").read_text()
            self.assertIn("divergences found", marker)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_overnight_loop_clean_exits_zero(self):
        r, outdir, td = self._run_soak_copy(0, ["--hours", "1"])
        try:
            self.assertEqual(r.returncode, 0)
            self.assertIn("SOAK CLEAN", r.stdout)
            self._assert_soak_report(outdir, [], [])
            self.assertFalse((outdir / "SOAK_FAILED").exists())
        finally:
            shutil.rmtree(td, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
