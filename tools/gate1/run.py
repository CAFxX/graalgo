#!/usr/bin/env python3
"""Gate 1 differential runner: run a synthesized test driver via graalgo vs gc.

For a package:
  1. Discover tests (discover.py)
  2. Synthesize driver (synthesize.py)
  3. Build & run driver via graalgo (graalgo-run)
  4. Build & run driver via gc (go run)
  5. Compare outputs; report per-test verdicts.

Usage: run.py <package> [--goroot PATH] [--graalgo-run PATH] [--out DIR]
"""
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))

# Minimum synthesised-test coverage for a PASS verdict: if fewer than
# this fraction of the discovered tests survive synthesis (the rest are
# skipped as internal files or unsupported imports), the package has not
# really been validated and the verdict is PARTIAL, not PASS. This is a
# pilot floor, not a quality bar: the skip list in verdict.txt names
# exactly what was not run.
MIN_COVERAGE = 0.50

def run_cmd(cmd, env=None, timeout=300):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=env)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return -1, "", "TIMEOUT"

def decide_verdict(rc_gc, rc_gr, gc_res, gr_res):
    """Pure verdict decision from driver exit codes and parsed results.

    Returns (verdict, divergences). The two rules that keep this gate
    honest:
    - both drivers failing is HARNESS_ERROR, never PASS: nothing was
      measured, so nothing may be certified (P0-1);
    - a clean comparison with zero parsed results on both sides is also
      HARNESS_ERROR: an empty comparison is not agreement.
    """
    if rc_gc != 0 and rc_gr != 0:
        return "HARNESS_ERROR", []
    if rc_gc != 0 and rc_gr == 0:
        return "SKIP", []
    if rc_gr != 0 and rc_gc == 0:
        return "DIVERGE", []
    divergences = []
    for name in sorted(set(gc_res) | set(gr_res)):
        g, r = gc_res.get(name, "MISSING"), gr_res.get(name, "MISSING")
        if g != r:
            divergences.append((name, g, r))
    if divergences:
        return "DIVERGE", divergences
    if not gc_res and not gr_res:
        return "HARNESS_ERROR", []
    return "PASS", []

def main():
    if len(sys.argv) < 2:
        sys.exit("usage: run.py <package> [--goroot PATH] [--graalgo-run PATH] [--out DIR]")
    pkg = sys.argv[1]
    goroot = os.path.expanduser("~/tools/go")
    graalgo_run = None
    outdir = None
    args = sys.argv[2:]
    i = 0
    while i < len(args):
        if args[i] == "--goroot":
            goroot = args[i+1]; i += 2
        elif args[i] == "--graalgo-run":
            graalgo_run = args[i+1]; i += 2
        elif args[i] == "--out":
            outdir = args[i+1]; i += 2
        else:
            i += 1

    if graalgo_run is None:
        # Default: phase1 graalgo-run in this repo (HERE=tools/gate1)
        repo = os.path.abspath(os.path.join(HERE, "..", ".."))
        graalgo_run = os.path.join(repo, "phases", "phase1", "graalgo-run")

    workdir = outdir or tempfile.mkdtemp(prefix=f"gate1-{pkg}-")
    os.makedirs(workdir, exist_ok=True)

    # 1. Discover
    disc_path = os.path.join(workdir, "discover.json")
    rc, out, err = run_cmd([sys.executable, os.path.join(HERE, "discover.py"),
                            pkg, "--goroot", goroot])
    if rc != 0:
        sys.exit(f"discover failed: {err}")
    with open(disc_path, "w") as f:
        f.write(out)

    # 2. Synthesize. Never trust a pre-existing driver: a failed synthesis
    # in a reused --out dir must not silently run the previous run's
    # driver (P1-6). Delete stale outputs first, then fail closed if
    # synthesis itself fails — existence of driver.go proves nothing
    # about which run produced it.
    driver_path = os.path.join(workdir, "driver.go")
    sidecar_path = driver_path + ".synth.json"
    for stale in (driver_path, sidecar_path, os.path.join(workdir, "verdict.txt")):
        if os.path.isfile(stale):
            os.remove(stale)
    rc, out, err = run_cmd([sys.executable, os.path.join(HERE, "synthesize.py"),
                            disc_path, "--out", driver_path,
                            "--shim", os.path.join(HERE, "shim", "shim.go")])
    print(out, file=sys.stderr)
    if err:
        print(err, file=sys.stderr)
    if rc != 0:
        sys.exit(f"synthesize failed (rc={rc}): {err}")
    if not os.path.exists(driver_path):
        sys.exit("synthesize produced no driver")
    if not os.path.isfile(sidecar_path):
        sys.exit("synthesize produced no coverage sidecar")

    # Coverage bookkeeping (P1-5): what fraction of the discovered tests
    # actually made it into the driver, and which files were skipped.
    with open(disc_path) as f:
        total_tests = json.load(f).get("total_tests", 0)
    with open(sidecar_path) as f:
        sidecar = json.load(f)
    synthesized_tests = sidecar.get("synthesized_tests", 0)
    skipped_files = sidecar.get("skipped", [])
    skip_ratio = (1.0 - synthesized_tests / total_tests) if total_tests > 0 else 0.0

    # 3. Run via gc (ground truth for the harness itself)
    go_bin = os.path.join(goroot, "bin", "go")
    env = dict(os.environ)
    # The driver imports the package under test; ensure GOPATH/GOROOT resolve.
    # We run from the workdir; the driver is self-contained (shim inlined).
    rc_gc, out_gc, err_gc = run_cmd([go_bin, "run", driver_path], timeout=300)
    with open(os.path.join(workdir, "gc.stdout"), "w") as f:
        f.write(out_gc)
    with open(os.path.join(workdir, "gc.stderr"), "w") as f:
        f.write(err_gc)

    # 4. Run via graalgo
    rc_gr, out_gr, err_gr = run_cmd([graalgo_run, driver_path], timeout=600)
    with open(os.path.join(workdir, "graalgo.stdout"), "w") as f:
        f.write(out_gr)
    with open(os.path.join(workdir, "graalgo.stderr"), "w") as f:
        f.write(err_gr)

    # 5. Compare
    def parse_results(text):
        results = {}
        for line in text.split("\n"):
            for prefix in ("PASS ", "FAIL ", "SKIP "):
                if line.startswith(prefix):
                    name = line[len(prefix):].strip()
                    results[name] = prefix.strip()
        summary = {}
        for line in text.split("\n"):
            if line.startswith("SUMMARY "):
                for part in line[len("SUMMARY "):].split():
                    k, v = part.split("=")
                    summary[k] = int(v)
        return results, summary

    gc_res, gc_sum = parse_results(out_gc)
    gr_res, gr_sum = parse_results(out_gr)

    print(f"=== {pkg} ===")
    print(f"gc:     rc={rc_gc} {gc_sum}")
    print(f"graalgo: rc={rc_gr} {gr_sum}")

    verdict, divergences = decide_verdict(rc_gc, rc_gr, gc_res, gr_res)
    if verdict == "HARNESS_ERROR" and rc_gc != 0 and rc_gr != 0:
        # Both drivers failed (e.g. harness/build failure) — nothing was
        # measured, so this must never read as PASS.
        print(f"HARNESS_ERROR: both drivers failed (gc rc={rc_gc}, graalgo rc={rc_gr}); no test was measured")
        print(f"gc stderr tail: {err_gc[-500:]}")
        print(f"graalgo stderr tail: {err_gr[-800:]}")
    elif verdict == "SKIP":
        # gc itself failed (e.g. unsupported test helper) — harness issue, not graalgo
        print(f"SKIP: gc driver failed (rc={rc_gc}); harness cannot validate this package")
        print(f"gc stderr tail: {err_gc[-500:]}")
    elif verdict == "DIVERGE" and rc_gr != 0 and rc_gc == 0:
        print(f"DIVERGE: graalgo driver failed (rc={rc_gr}) while gc passed")
        print(f"graalgo stderr tail: {err_gr[-800:]}")
    elif verdict == "DIVERGE":
        for name, g, r in divergences:
            print(f"  DIVERGE {name}: gc={g} graalgo={r}")
    elif verdict == "HARNESS_ERROR":
        print("HARNESS_ERROR: neither driver reported a test result; nothing was measured")
    else:
        print(f"  {len(gc_res)} tests agree")

    # Coverage downgrade (P1-5): a passing subset is not a passing
    # package. Never mask a worse verdict (HARNESS_ERROR/DIVERGE/SKIP)
    # with PARTIAL — only downgrade a would-be PASS.
    coverage = (synthesized_tests / total_tests) if total_tests > 0 else 0.0
    if verdict == "PASS" and coverage < MIN_COVERAGE:
        verdict = "PARTIAL"
        print(f"PARTIAL: only {synthesized_tests}/{total_tests} discovered tests "
              f"synthesised (coverage {coverage:.2f} < {MIN_COVERAGE:.2f}); "
              f"see skip list in verdict.txt")
    print(f"COVERAGE synthesised={synthesized_tests} total={total_tests} "
          f"skip_ratio={skip_ratio:.3f}")

    print(f"VERDICT {pkg}: {verdict}")
    with open(os.path.join(workdir, "verdict.txt"), "w") as f:
        f.write(f"{verdict}\n")
        f.write(f"COVERAGE synthesised={synthesized_tests} total={total_tests} "
                f"skip_ratio={skip_ratio:.3f}\n")
        for entry in skipped_files:
            f.write(f"SKIP {entry.get('file', '?')}: {entry.get('reason', '?')}\n")
    return 0 if verdict == "PASS" else 1

if __name__ == "__main__":
    sys.exit(main())
