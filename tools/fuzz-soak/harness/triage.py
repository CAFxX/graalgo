#!/usr/bin/env python3
"""Delta-debugging minimizer for fuzzing findings.

Given a failing program and its failure signature, repeatedly removes
statements/functions while the failure persists, producing a minimal
reproducer.

Usage: triage.py <failing.go> --out <minimized-dir> [--workdir DIR]
   (also accepts --prog <failing.go> as a long-form alias)

The "interesting" test re-runs diff_one.sh and checks the result matches
the original failure category.
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys

# Repo root: tools/fuzz-soak/harness/triage.py -> tools/fuzz-soak -> tools -> repo.
_GR = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
GR = os.environ.get("GR", _GR)
FUZZ = os.path.join(GR, "tools", "fuzz-soak")


def diff_result(gofile, workdir):
    r = subprocess.run(
        ["bash", f"{FUZZ}/harness/diff_one.sh", gofile, workdir, "1"],
        capture_output=True, text=True, timeout=300)
    try:
        return json.loads(r.stdout.strip().split("\n")[-1])
    except Exception:
        return {"result": "HARNESS_ERROR", "detail": ""}


def split_statements(src):
    """Naive statement splitter: top-level lines of func main body."""
    # Find func main body.
    m = re.search(r'func main\(\) \{(.*)\n\}\n?$', src, re.DOTALL)
    if not m:
        return None, None
    body = m.group(1)
    # Split into logical lines (respecting braces crudely).
    lines = body.split("\n")
    return m, lines


def minimize(prog_path, out_path, workdir):
    with open(prog_path) as f:
        orig_src = f.read()

    base_rec = diff_result(prog_path, os.path.join(workdir, "base"))
    want_result = base_rec["result"]
    want_sig = base_rec["detail"][:60]
    print(f"original: {want_result} [{want_sig}]")
    if want_result in ("PASS", "SCOPE", "GC_BUILD_FAIL", "HARNESS_ERROR"):
        print("nothing to minimize")
        return False

    def interesting(src):
        tmp = os.path.join(workdir, "candidate.go")
        with open(tmp, "w") as f:
            f.write(src)
        # Must still build under gc (else not a valid reduction).
        env = dict(os.environ)
        env["PATH"] = os.path.expanduser("~/tools/go/bin") + ":" + env.get("PATH", "")
        r = subprocess.run(["go", "build", "-o", os.devnull, tmp],
                           capture_output=True, env=env)
        if r.returncode != 0:
            return False
        rec = diff_result(tmp, os.path.join(workdir, "cand"))
        return (rec["result"] == want_result
                and rec["detail"][:60] == want_sig)

    src = orig_src
    # ddmin on main-body lines.
    m, lines = split_statements(src)
    if m is None:
        print("cannot parse main body")
        return False

    # Only try removing non-essential lines (keep func main braces).
    changed = True
    while changed:
        changed = False
        n = len(lines)
        # Try chunks of decreasing size.
        chunk = max(1, n // 2)
        while chunk >= 1:
            i = 0
            while i < len(lines):
                trial = lines[:i] + lines[i + chunk:]
                if not trial:
                    i += chunk
                    continue
                body = "\n".join(trial)
                trial_src = src[:m.start(1)] + body + src[m.end(1):]
                if interesting(trial_src):
                    lines = trial
                    changed = True
                    # restart with new line count
                    break
                i += chunk
            else:
                chunk //= 2
                continue
            break

    body = "\n".join(lines)
    minimal = src[:m.start(1)] + body + src[m.end(1):]
    with open(out_path, "w") as f:
        f.write(minimal)
    print(f"minimized: {len(orig_src)} -> {len(minimal)} bytes")
    return True


def main():
    ap = argparse.ArgumentParser()
    # run_soak.sh invokes triage.py positionally (triage.py <prog> --out <dir>).
    ap.add_argument("prog_pos", nargs="?",
                    help="failing program (positional, as run_soak.sh invokes it)")
    ap.add_argument("--prog", default=None,
                    help="failing program (long-form alias for the positional)")
    ap.add_argument("--out", required=True)
    ap.add_argument("--workdir",
                    default=os.path.join(FUZZ, ".tmp", "triage"),
                    help="scratch dir (default: tools/fuzz-soak/.tmp/triage)")
    args = ap.parse_args()
    prog = args.prog or args.prog_pos
    if not prog:
        ap.error("a failing program is required (positional or --prog)")
    os.makedirs(args.workdir, exist_ok=True)
    ok = minimize(prog, args.out, args.workdir)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
