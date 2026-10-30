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

def run_cmd(cmd, env=None, timeout=300):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout,
                           env=env)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return -1, "", "TIMEOUT"

def decide_verdict(rc_gc, rc_gr, gc_res, gr_res):
    """Pure verdict decision for one package.

    Returns (verdict, divergences). PASS must be earned: both legs exit
    0 AND at least one parsed test result AND no per-test divergence.
    Anything else is SKIP (gc alone failed: harness issue, not graalgo),
    DIVERGE (graalgo alone failed, or per-test disagreement), or ERROR
    (both legs failed, or both legs exited 0 with no parsed results —
    i.e. nothing was actually compared). Previously the verdict
    defaulted to PASS, so two crashed legs with two empty result maps
    reported "0 tests agree" / PASS with exit 0 (review 2026-10-30,
    P1-9). ERROR is never PASS and always exits non-zero.
    """
    if rc_gc != 0 and rc_gr == 0:
        return "SKIP", []
    if rc_gr != 0 and rc_gc == 0:
        return "DIVERGE", []
    if rc_gc != 0 and rc_gr != 0:
        # Both legs failed: no evidence about graalgo at all. Not a
        # divergence (gc did not pass) and certainly not a pass.
        return "ERROR", []
    if not gc_res and not gr_res:
        # Both legs exited 0 but produced no parseable test results:
        # nothing was compared, so nothing passed.
        return "ERROR", []
    divergences = [
        (name, gc_res.get(name, "MISSING"), gr_res.get(name, "MISSING"))
        for name in sorted(set(gc_res) | set(gr_res))
        if gc_res.get(name, "MISSING") != gr_res.get(name, "MISSING")
    ]
    if divergences:
        return "DIVERGE", divergences
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

    # 2. Synthesize
    driver_path = os.path.join(workdir, "driver.go")
    rc, out, err = run_cmd([sys.executable, os.path.join(HERE, "synthesize.py"),
                            disc_path, "--out", driver_path,
                            "--shim", os.path.join(HERE, "shim", "shim.go")])
    print(out, file=sys.stderr)
    if err:
        print(err, file=sys.stderr)
    if not os.path.exists(driver_path):
        sys.exit("synthesize produced no driver")

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
    if verdict == "SKIP":
        # gc itself failed (e.g. unsupported test helper) — harness issue, not graalgo
        print(f"SKIP: gc driver failed (rc={rc_gc}); harness cannot validate this package")
        print(f"gc stderr tail: {err_gc[-500:]}")
    elif verdict == "DIVERGE" and rc_gr != 0 and rc_gc == 0:
        print(f"DIVERGE: graalgo driver failed (rc={rc_gr}) while gc passed")
        print(f"graalgo stderr tail: {err_gr[-800:]}")
    elif verdict == "ERROR" and rc_gc != 0 and rc_gr != 0:
        print(f"ERROR: both drivers failed (gc rc={rc_gc}, graalgo rc={rc_gr}); no comparison possible")
        print(f"gc stderr tail: {err_gc[-500:]}")
        print(f"graalgo stderr tail: {err_gr[-800:]}")
    elif verdict == "ERROR":
        print("ERROR: both drivers exited 0 but produced no parsed test results; no comparison possible")
    elif verdict == "DIVERGE":
        for name, g, r in divergences:
            print(f"  DIVERGE {name}: gc={g} graalgo={r}")
    else:
        print(f"  {len(gc_res)} tests agree")

    print(f"VERDICT {pkg}: {verdict}")
    with open(os.path.join(workdir, "verdict.txt"), "w") as f:
        f.write(f"{verdict}\n")
    return 0 if verdict == "PASS" else 1

if __name__ == "__main__":
    sys.exit(main())
