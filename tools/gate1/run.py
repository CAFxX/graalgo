#!/usr/bin/env python3
"""Gate 1 differential runner: compare `go test` (gc) against a
synthesized test driver run via graalgo.

For a package:
  1. Synthesize the test driver with the graalgo binary itself
     (`graalgo test -emit-driver` — the single test-driver synthesis,
     owned by cmd/graalgo/testsynth.go; this harness never synthesizes
     drivers itself).
  2. Run the package's tests via gc: real `go test -v` (reference).
  3. Run the driver via graalgo (graalgo-sync-run, the launcher
     `graalgo test` uses), with --test-package-dir so the frontend
     grants the driver's package internal visibility.
  4. Compare per-test go-test-format verdicts; report the verdict.

The verdict is evidence-driven, over the reported per-test results —
never over exit codes alone (a dual failure used to certify PASS):

  PASS    both sides reported the same non-empty per-test verdicts.
          (A test failing identically under both is agreement — the
          gate compares graalgo against gc, it does not require the
          tests themselves to pass.)
  DIVERGE per-test verdicts differ, or graalgo reported nothing while
          gc reported results (graalgo-side crash).
  SKIP    gc reported nothing (build/test failure on the reference
          side); the harness cannot validate this package.
  ERROR   neither side reported any test result, or a side exited 0
          while reporting nothing. No evidence is never a PASS.

Usage: run.py <package> [--goroot PATH] [--graalgo PATH]
            [--graalgo-run PATH] [--out DIR]
"""
import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))

# Spawn failure: the command could not even start (missing binary,
# not executable). Distinct from a child that ran and failed (-1 is
# the timeout sentinel); callers treat any negative rc as "no
# evidence from this side".
SPAWN_FAILED_RC = -2


def run_cmd(cmd, env=None, timeout=300):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=env)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return -1, "", "TIMEOUT"
    except OSError as e:
        # Missing/unrunnable binary: surface it as a failed side with
        # the reason attached, never as a traceback.
        return SPAWN_FAILED_RC, "", f"SPAWN FAILED: {e}"


def parse_go_test_results(text):
    """Per-test verdicts from `go test -v` / driver output.

    Parses `--- PASS/FAIL/SKIP: Name` lines (both the reference side
    and the synthesized driver print go-test format). Returns
    {test_name: "PASS"|"FAIL"|"SKIP"}.

    Only TestXxx names are kept: the driver synthesizes TestXxx
    functions only, so `go test`'s Example* results have no graalgo
    counterpart and would diverge spuriously on every package.
    """
    results = {}
    for line in text.split("\n"):
        m = re.match(r"^--- (PASS|FAIL|SKIP): (\S+)", line)
        if m and m.group(2).startswith("Test"):
            results[m.group(2)] = m.group(1)
    return results


def decide_verdict(rc_gc, rc_gr, gc_res, gr_res, err_gc, err_gr):
    """Decide the package verdict from the two sides' evidence.

    Returns (verdict, report_lines). The verdict is computed from the
    reported per-test results only; exit codes are reported, and are
    consulted only to classify an empty side (a side that failed is
    SKIP/DIVERGE; a side that exited 0 while reporting nothing is a
    harness anomaly — ERROR, never PASS).
    """
    lines = []
    if not gc_res and not gr_res:
        lines.append(
            f"ERROR: no test results from either side "
            f"(gc rc={rc_gc}, graalgo rc={rc_gr})"
        )
        if err_gc:
            lines.append(f"gc stderr tail: {err_gc[-500:]}")
        if err_gr:
            lines.append(f"graalgo stderr tail: {err_gr[-500:]}")
        return "ERROR", lines
    if not gc_res:
        if rc_gc == 0:
            lines.append(
                "ERROR: gc exited 0 but reported no test results; "
                "harness cannot validate this package"
            )
            return "ERROR", lines
        lines.append(
            f"SKIP: gc reported no test results (rc={rc_gc}); "
            "harness cannot validate this package"
        )
        lines.append(f"gc stderr tail: {err_gc[-500:]}")
        return "SKIP", lines
    if not gr_res:
        if rc_gr == 0:
            lines.append(
                "ERROR: graalgo exited 0 but reported no test results; "
                "harness cannot validate this package"
            )
            return "ERROR", lines
        lines.append(
            f"DIVERGE: graalgo reported no test results (rc={rc_gr}) "
            "while gc reported results"
        )
        lines.append(f"graalgo stderr tail: {err_gr[-800:]}")
        return "DIVERGE", lines
    divergences = []
    for name in sorted(set(gc_res) | set(gr_res)):
        g, r = gc_res.get(name, "MISSING"), gr_res.get(name, "MISSING")
        if g != r:
            divergences.append((name, g, r))
    if divergences:
        for name, g, r in divergences:
            lines.append(f"  DIVERGE {name}: gc={g} graalgo={r}")
        return "DIVERGE", lines
    lines.append(f"  {len(gc_res)} tests agree")
    return "PASS", lines


def _default_graalgo_run():
    # The launcher `graalgo test` itself uses (graalgo-sync-run); fall
    # back to the plain launcher if it is absent (e.g. older build).
    sync_run = os.path.join(REPO, "internal", "backend", "graalgo-sync-run")
    if os.path.isfile(sync_run):
        return sync_run
    return os.path.join(REPO, "internal", "backend", "graalgo-run")


def _check_executable(path, what, hint):
    if not os.path.isfile(path):
        sys.exit(f"{what} not found: {path} ({hint})")
    if not os.access(path, os.X_OK):
        sys.exit(f"{what} not executable: {path} ({hint})")


def main():
    if len(sys.argv) < 2:
        sys.exit("usage: run.py <package> [--goroot PATH] [--graalgo PATH] "
                 "[--graalgo-run PATH] [--out DIR]")
    pkg = sys.argv[1]
    goroot = os.path.expanduser("~/tools/go")
    graalgo = os.path.join(REPO, "graalgo")
    graalgo_run = None
    outdir = None
    args = sys.argv[2:]
    i = 0
    while i < len(args):
        if args[i] == "--goroot":
            goroot = args[i + 1]; i += 2
        elif args[i] == "--graalgo":
            graalgo = args[i + 1]; i += 2
        elif args[i] == "--graalgo-run":
            graalgo_run = args[i + 1]; i += 2
        elif args[i] == "--out":
            outdir = args[i + 1]; i += 2
        else:
            i += 1

    if graalgo_run is None:
        graalgo_run = _default_graalgo_run()
    _check_executable(graalgo, "graalgo binary", "build it with `make graalgo`")
    _check_executable(graalgo_run, "graalgo launcher",
                      "build the backend with `make build`")

    pkg_dir = os.path.join(goroot, "src", pkg)
    if not os.path.isdir(pkg_dir):
        sys.exit(f"no such package dir: {pkg_dir}")

    workdir = outdir or tempfile.mkdtemp(prefix=f"gate1-{pkg}-")
    os.makedirs(workdir, exist_ok=True)

    # 1. Synthesize the driver with the graalgo binary (single
    # implementation — cmd/graalgo/testsynth.go). The driver is always
    # verbose so every test reports a `--- PASS/FAIL/SKIP` line.
    driver_path = os.path.join(workdir, "driver.go")
    rc, out, err = run_cmd([graalgo, "test", "-emit-driver", driver_path,
                            pkg_dir], timeout=300)
    if rc != 0:
        sys.exit(f"synthesize failed (rc={rc}): {err[-500:]}")
    if not os.path.exists(driver_path):
        sys.exit("synthesize produced no driver")

    # 2. Run via gc: real `go test -v` on the package (reference).
    go_bin = os.path.join(goroot, "bin", "go")
    rc_gc, out_gc, err_gc = run_cmd([go_bin, "test", "-v", "-count=1",
                                     pkg_dir], timeout=300)
    with open(os.path.join(workdir, "gc.stdout"), "w") as f:
        f.write(out_gc)
    with open(os.path.join(workdir, "gc.stderr"), "w") as f:
        f.write(err_gc)

    # 3. Run the driver via graalgo. --test-package-dir is the same
    # flag `graalgo test` passes: the frontend applies Go's
    # internal-visibility rule with the package's own privileges.
    rc_gr, out_gr, err_gr = run_cmd(
        [graalgo_run, "--test-package-dir=" + pkg_dir, driver_path],
        timeout=600)
    with open(os.path.join(workdir, "graalgo.stdout"), "w") as f:
        f.write(out_gr)
    with open(os.path.join(workdir, "graalgo.stderr"), "w") as f:
        f.write(err_gr)

    # 4. Compare per-test verdicts. Parse both streams: the
    # synthesized driver prints via builtin println, which writes to
    # stderr, so its `--- PASS/FAIL/SKIP` lines arrive there; `go
    # test -v` writes its results to stdout (parsing its stderr too
    # is harmless — build failures print no `--- ` lines).
    gc_res = parse_go_test_results(out_gc + "\n" + err_gc)
    gr_res = parse_go_test_results(out_gr + "\n" + err_gr)

    print(f"=== {pkg} ===")
    print(f"gc:      rc={rc_gc} {len(gc_res)} tests")
    print(f"graalgo: rc={rc_gr} {len(gr_res)} tests")
    verdict, lines = decide_verdict(rc_gc, rc_gr, gc_res, gr_res,
                                    err_gc, err_gr)
    for line in lines:
        print(line)

    print(f"VERDICT {pkg}: {verdict}")
    with open(os.path.join(workdir, "verdict.txt"), "w") as f:
        f.write(f"{verdict}\n")
    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
