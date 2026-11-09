#!/usr/bin/env python3
"""Regression pins for NEW-P1-7: fuzz soak dead on deleted phases/ paths,
and for REVIEW-2026-11-06-10 R-P1-13 residuals: the GHA driver + image
still on phases/, and the overnight loop swallowing batch failures,
and for REVIEW-2026-11-09-03 R03-T1: the shard/soak exit contract
ignoring divergences (driver exiting 0 on findings, soak report
without programs_run/batches_failed).

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

    def _fake_driver_home(self, td):
        """Fake GRAALGO_HOME with a stub generator + stub diff_one.sh.

        The generator writes a trivial .go (and fails main-batch seeds
        when GEN_FAIL_MAIN=1); diff_one.sh PASSes the canary and
        returns $DIFF_RESULT for every other program. No build, no JVM.
        """
        fuzz = Path(td) / "fake-home" / "tools" / "fuzz-soak"
        (fuzz / "gen").mkdir(parents=True, exist_ok=True)
        (fuzz / "harness").mkdir(parents=True, exist_ok=True)
        (fuzz / "gen" / "generator.py").write_text(
            "#!/usr/bin/env python3\n"
            "import os, sys\n"
            "args = sys.argv[1:]\n"
            "seed = args[args.index('--seed') + 1]\n"
            "out = args[args.index('--out') + 1]\n"
            "if seed != '999999' and os.environ.get('GEN_FAIL_MAIN') "
            "== '1':\n"
            "    print('stub generator: failing main-batch seed',\n"
            "          file=sys.stderr)\n"
            "    sys.exit(1)\n"
            "open(out, 'w').write('package main\\n\\n"
            "func main() {}\\n')\n")
        (fuzz / "harness" / "diff_one.sh").write_text(
            "#!/bin/bash\n"
            "# $1=gofile; canary always PASSes, other seeds use "
            "$DIFF_RESULT.\n"
            "base=\"$(basename \"$1\" .go)\"\n"
            "if [ \"$base\" = \"canary\" ]; then res=\"PASS\"; "
            "else res=\"${DIFF_RESULT:-PASS}\"; fi\n"
            "printf '{\"file\": \"%s\", \"result\": \"%s\", "
            "\"detail\": \"stub\"}\\n' \"$base\" \"$res\"\n")
        return Path(td) / "fake-home"

    def _run_driver(self, td, fake_home, diff_result="PASS",
                    seeds_per_shard="2", gen_fail_main=False, tag="case"):
        outdir = Path(td) / f"out-{tag}"
        env = dict(os.environ)
        env.update({
            "GRAALGO_HOME": str(fake_home),
            "OUT_DIR": str(outdir),
            "SHARD_IDX": "0",
            "SHARD_COUNT": "1",
            "SEEDS_PER_SHARD": seeds_per_shard,
            "DIFF_RESULT": diff_result,
        })
        if gen_fail_main:
            env["GEN_FAIL_MAIN"] = "1"
        r = subprocess.run(
            [sys.executable,
             str(FUZZ_DIR / "gha" / "driver" / "fuzz_driver.py")],
            capture_output=True, text=True, env=env, timeout=60)
        return r, outdir

    def test_driver_exit_contract(self):
        # Behavioural pin for R03-T1: the shard driver must fail the
        # step when divergences are found (exit 1), fail loudly when
        # the shard could not run (exit 2: generator failure,
        # HARNESS_ERROR, zero programs), and exit 0 only on a clean
        # shard that actually ran programs — with shard-summary.json
        # recording programs_run so "0 divergences" is distinguishable
        # from "0 programs ran". Falsifying: on the pre-fix driver
        # every case below exits 0 after "shard done" (the zero-seed
        # case never even ran a differential).
        td = tempfile.mkdtemp(prefix="driver-exit-pin-")
        try:
            fake_home = self._fake_driver_home(td)
            cases = [
                # (diff_result, seeds, gen_fail, rc, programs_run, tag)
                ("PASS", "2", False, 0, 2, "clean"),
                ("WRONG_OUTPUT", "2", False, 1, 2, "wrong-output"),
                ("CRASH", "2", False, 1, 2, "crash"),
                ("HARNESS_ERROR", "2", False, 2, 2, "harness-error"),
                ("PASS", "2", True, 2, 0, "gen-fail"),
            ]
            for diff_result, seeds, gen_fail, rc, n, tag in cases:
                r, outdir = self._run_driver(
                    td, fake_home, diff_result, seeds, gen_fail, tag)
                self.assertEqual(r.returncode, rc, tag)
                if tag == "gen-fail":
                    # Generator died before results.jsonl existed.
                    self.assertFalse(
                        (outdir / "results.jsonl").exists(), tag)
                    continue
                summary = json.loads(
                    (outdir / "shard-summary.json").read_text())
                self.assertEqual(summary["programs_run"], n, tag)
                self.assertEqual(summary["counts"].get(diff_result), n,
                                 tag)
                if rc == 0:
                    self.assertIn("shard done", r.stdout, tag)
                elif rc == 1:
                    self.assertIn("SHARD DIVERGENT", r.stdout, tag)
                    self.assertEqual(summary["diverged"], n, tag)
                else:
                    self.assertIn("SHARD FAILED", r.stdout, tag)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_driver_zero_seeds_exits_two(self):
        # SEEDS_PER_SHARD=0 selects zero programs: the shard must fail
        # loudly (exit 2) instead of running the canary only and
        # reporting a clean exit 0 — the shard-level form of the
        # run_batch.py empty-range guard.
        td = tempfile.mkdtemp(prefix="driver-zero-seeds-pin-")
        try:
            fake_home = self._fake_driver_home(td)
            r, outdir = self._run_driver(td, fake_home, "PASS", "0",
                                         False, "zero-seeds")
            self.assertEqual(r.returncode, 2)
            self.assertIn("no programs to run", r.stdout + r.stderr)
            self.assertFalse((outdir / "results.jsonl").exists())
        finally:
            shutil.rmtree(td, ignore_errors=True)


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

    def test_diverged_batch_exits_one(self):
        # Behavioural pin for the R03-T1 exit contract: a batch that
        # ran and found divergences must exit 1, a clean batch 0, and
        # a HARNESS_ERROR batch 2. test_exit_code_contract above pins
        # the source text; this pins the behaviour with a stubbed
        # backend (stub `make`, stub generator, stub diff_one.sh) —
        # no build, no JVM, no generator. Falsifying: on the pre-fix
        # run_batch.py (return 0 unless GC_BUILD_FAIL), the
        # WRONG_OUTPUT case exits 0.
        td = tempfile.mkdtemp(prefix="diverged-batch-pin-")
        try:
            fake_gr = Path(td) / "fake-gr"
            fuzz = fake_gr / "tools" / "fuzz-soak"
            (fuzz / "gen").mkdir(parents=True)
            (fuzz / "harness").mkdir(parents=True)
            (fuzz / "gen" / "generator.py").write_text(
                "#!/usr/bin/env python3\n"
                "import sys\n"
                "out = sys.argv[sys.argv.index('--out') + 1]\n"
                "open(out, 'w').write('package main\\n\\n"
                "func main() {}\\n')\n")
            (fuzz / "harness" / "diff_one.sh").write_text(
                "#!/bin/bash\n"
                "# $1=gofile $2=workdir $3=gmp; result from $DIFF_RESULT.\n"
                "printf '{\"file\": \"%s\", \"result\": \"%s\", "
                "\"detail\": \"stub\"}\\n' "
                "\"$(basename \"$1\" .go)\" \"${DIFF_RESULT:-PASS}\"\n")
            shim_dir = Path(td) / "shims"
            shim_dir.mkdir()
            (shim_dir / "make").write_text("#!/bin/bash\nexit 0\n")
            os.chmod(shim_dir / "make", 0o755)
            for result, expected_rc in (
                    ("PASS", 0), ("WRONG_OUTPUT", 1), ("CRASH", 1),
                    ("HANG", 1), ("EXIT_MISMATCH", 1),
                    ("GC_BUILD_FAIL", 1), ("HARNESS_ERROR", 2)):
                env = {
                    "PATH": f"{shim_dir}:{os.environ['PATH']}",
                    "HOME": td,
                    "GR": str(fake_gr),
                    "DIFF_RESULT": result,
                }
                outdir = Path(td) / f"out-{result}"
                r = subprocess.run(
                    [sys.executable, str(HARNESS_DIR / "run_batch.py"),
                     "--seeds", "0:2", "--out", str(outdir), "--jobs", "1"],
                    capture_output=True, text=True, env=env, timeout=60)
                self.assertEqual(r.returncode, expected_rc, result)
                recs = [json.loads(l) for l in
                        (outdir / "results.jsonl").read_text().splitlines()]
                self.assertEqual(len(recs), 2, result)
        finally:
            shutil.rmtree(td, ignore_errors=True)


# Behavioural pins for the run_soak.sh failure semantics (R-P1-13):
# the single-batch slot must propagate the batch verdict after
# minimizing, and the overnight loop must record failures and exit
# nonzero instead of printing "failed; continuing" and exiting 0.
# The tests run a COPIED harness tree with run_batch.py replaced by
# a stub (no build, no JVM, no generator), so they are cheap.
STUB_RUN_BATCH = """#!/usr/bin/env python3
import json, os, sys
out = sys.argv[sys.argv.index("--out") + 1]
n = int(os.environ.get("STUB_RESULTS", "0"))
if n:
    os.makedirs(out, exist_ok=True)
    with open(os.path.join(out, "results.jsonl"), "w") as f:
        for i in range(n):
            f.write(json.dumps({"file": f"fuzz_{i:05d}",
                                "result": "PASS", "detail": ""}) + "\\n")
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
    def _run_soak_copy(self, stub_rc, args, stub_results=None):
        """Run run_soak.sh from a copied tree with a stubbed
        run_batch.py. Returns (returncode, stdout, outdir, tempdir);
        the caller must rmtree the temp dir (see the tests below).
        stub_results: when set, the stub writes that many PASS records
        to each batch's results.jsonl (R03-T1: programs_run pin)."""
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
        if stub_results is not None:
            env["STUB_RESULTS"] = str(stub_results)
        r = subprocess.run(
            ["bash", str(fuzz_copy / "run_soak.sh")] + args
            + ["--out", str(outdir)],
            capture_output=True, text=True, env=env, timeout=60)
        return r, outdir, td

    def _assert_soak_report(self, outdir, failed, divergent,
                            programs_run, batches_failed):
        rep = json.loads((outdir / "soak-report.json").read_text())
        self.assertEqual(rep["failed_batches"], failed)
        self.assertEqual(rep["divergent_batches"], divergent)
        # R03-T1: the report must record how many programs actually
        # ran and how many batches failed to run, so "0 divergences"
        # is distinguishable from "0 programs ran".
        self.assertEqual(rep["programs_run"], programs_run)
        self.assertEqual(rep["batches_failed"], batches_failed)

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
            # No results.jsonl: the stub ran nothing, so programs_run
            # is 0 and the batch counts as failed, not divergent.
            self._assert_soak_report(outdir, [0], [], 0, 1)
            marker = (outdir / "SOAK_FAILED").read_text()
            self.assertIn("infrastructure failure", marker)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_overnight_loop_fails_loudly_on_divergence(self):
        r, outdir, td = self._run_soak_copy(1, ["--hours", "1"],
                                            stub_results=2)
        try:
            self.assertEqual(r.returncode, 1)
            self.assertIn("LOUD: batch 0 found divergences", r.stdout)
            self.assertIn("SOAK FAILED", r.stdout)
            self._assert_soak_report(outdir, [], [0], 2, 0)
            marker = (outdir / "SOAK_FAILED").read_text()
            self.assertIn("divergences found", marker)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_overnight_loop_clean_exits_zero(self):
        r, outdir, td = self._run_soak_copy(0, ["--hours", "1"],
                                            stub_results=2)
        try:
            self.assertEqual(r.returncode, 0)
            self.assertIn("SOAK CLEAN", r.stdout)
            # Clean means 2 programs ran and 0 diverged — not 0 ran.
            self._assert_soak_report(outdir, [], [], 2, 0)
            self.assertFalse((outdir / "SOAK_FAILED").exists())
        finally:
            shutil.rmtree(td, ignore_errors=True)


class TestAggregateExitCode(unittest.TestCase):
    def _write_shard(self, root, shard, records, canary_fail=False):
        d = Path(root) / shard
        d.mkdir(parents=True)
        with open(d / "results.jsonl", "w") as f:
            for rec in records:
                f.write(json.dumps(rec) + "\n")
        if canary_fail:
            (d / "CANARY_FAIL").write_text("{}")

    def _run_aggregate(self, root):
        return subprocess.run(
            [sys.executable, str(FUZZ_DIR / "gha" / "aggregate.py"),
             str(root)],
            capture_output=True, text=True, timeout=60)

    def test_clean_exits_zero(self):
        # All PASS: the aggregation is a clean verdict.
        td = tempfile.mkdtemp(prefix="aggregate-pin-")
        try:
            self._write_shard(td, "shard-0", [
                {"file": "fuzz_1", "result": "PASS", "detail": "",
                 "seed": 1, "shard": 0},
                {"file": "fuzz_2", "result": "SCOPE", "detail": "",
                 "seed": 2, "shard": 0},
            ])
            r = self._run_aggregate(td)
            self.assertEqual(r.returncode, 0)
            self.assertIn("total: 2", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_diverged_exits_one(self):
        # Failure groups present: exit 1, not a clean 0. Falsifying:
        # the pre-fix aggregate.py printed the groups and exited 0.
        td = tempfile.mkdtemp(prefix="aggregate-pin-")
        try:
            self._write_shard(td, "shard-0", [
                {"file": "fuzz_1", "result": "WRONG_OUTPUT",
                 "detail": "stdout differs", "seed": 1, "shard": 0},
            ])
            r = self._run_aggregate(td)
            self.assertEqual(r.returncode, 1)
            self.assertIn("AGGREGATE DIVERGENT", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_empty_root_exits_two(self):
        # No shard results at all: exit 2, not a vacuous "total: 0".
        td = tempfile.mkdtemp(prefix="aggregate-pin-")
        try:
            r = self._run_aggregate(td)
            self.assertEqual(r.returncode, 2)
            self.assertIn("no shard results", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)

    def test_quarantined_only_exits_two(self):
        # Every shard quarantined (canary fail): its results are
        # skipped, so nothing aggregates — exit 2, not clean.
        td = tempfile.mkdtemp(prefix="aggregate-pin-")
        try:
            self._write_shard(td, "shard-0", [
                {"file": "fuzz_1", "result": "PASS", "detail": "",
                 "seed": 1, "shard": 0},
            ], canary_fail=True)
            r = self._run_aggregate(td)
            self.assertEqual(r.returncode, 2)
            self.assertIn("QUARANTINED shard-0", r.stdout)
            self.assertIn("no shard results", r.stdout)
        finally:
            shutil.rmtree(td, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
