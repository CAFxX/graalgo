#!/usr/bin/env python3
"""
Check that the generated archive/mime desugar table is a subset of the
committed m7MultiResultFuncs table (A-P1-11, REVIEW-2026-11-05-15).

The generator (internal/frontend/gen_synth_archive_mime.py) emits a
table-entries file for every multi-result function/method it extracts
from gc 1.27.1. Those rows were historically copied into
internal/frontend/mrdesugar_m5.go by hand — and five were dropped
(mime/multipart ReadForm + FileHeader.Open; archive/zip fileListEntry.Info
+ openDir.Stat + openDir.Read), leaving working __-helpers unreachable
behind "not in M7 scope" panic stubs. This check regenerates the table
into a temp dir (never touching the repo) and fails unless every
generated row is present in the committed table with the identical
helper, pair type, and pair fields.

Comparison is on full rows, not just keys: the committed table once
carried nine wrong pair-field names (e.g. tar computeChecksum
{"f0", "signed"} for a struct whose fields are {unsigned, signed}),
which synthPairField would have rejected loudly at the first desugar.
All nine were corrected with the A-P1-11 rows; this check pins them.

Deliberate exclusions (generated rows with no committed row, each
verified unreachable from user code — see REVIEW-2026-11-05-15
"checked-clear"); adding an exclusion needs a fresh review note here:
- ("archive/zip", "Info", "headerFileInfo"): headerFileInfo is only
  obtainable as fs.FileInfo, which has no Info method.
- ("archive/zip", "findBodyOffset", "File"): unexported; internal calls
  are rewritten in-source to __zipFilefindBodyOffset by the generator.
- ("mime/multipart", "readForm", "Reader"): unexported; called only
  from __mpReaderReadForm.

The check NEVER passes vacuously: zero generated rows, zero committed
archive/mime rows, or an unparseable row all fail loudly. The generator
needs the go1.27.1 sources (GO env var or `go` on PATH, as in
run_gen_synth.py); if they cannot be located the check fails instead
of skipping.
"""

import os
import re
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
GENERATOR = os.path.join(REPO, "internal", "frontend",
                         "gen_synth_archive_mime.py")
TABLE = os.path.join(REPO, "internal", "frontend", "mrdesugar_m5.go")

# (pkgPath, name, recvType) generated rows with no committed row, by design.
EXCLUSIONS = {
    ("archive/zip", "Info", "headerFileInfo"),
    ("archive/zip", "findBodyOffset", "File"),
    ("mime/multipart", "readForm", "Reader"),
}

GEN_ROW_RE = re.compile(
    r'^\s*\{"([^"]+)", "([^"]*)", "([^"]*)", "([^"]+)", "([^"]+)", '
    r'\[\]string\{([^}]*)\}\},$')
COMMITTED_ROW_RE = re.compile(
    r'^\s*\{"([^"]+)", "([^"]*)", "([^"]*)", "([^"]+)", "([^"]+)", '
    r'\[\]string\{([^}]*)\}, (true|false)\},(\s*//.*)?$')


def parse_fields(raw):
    return [f.strip().strip('"') for f in raw.split(",") if f.strip()]


def parse_generated(path):
    rows = {}
    with open(path) as f:
        lines = f.readlines()
    if not lines:
        raise SystemExit("check_mr_table_subset: generated entries file "
                         "is empty: %s" % path)
    for line in lines:
        m = GEN_ROW_RE.match(line.rstrip("\n"))
        if not m:
            raise SystemExit("check_mr_table_subset: unparseable generated "
                             "row: %r" % line.rstrip("\n"))
        pkg, name, recv, helper, pair, fields = m.groups()
        key = (pkg, name, recv)
        if key in rows:
            raise SystemExit("check_mr_table_subset: duplicate generated "
                             "row for %r" % (key,))
        rows[key] = (helper, pair, tuple(parse_fields(fields)))
    return rows


def parse_committed_m7(path):
    """Parse the m7MultiResultFuncs literal only (other tables in the
    same file have different struct shapes)."""
    with open(path) as f:
        lines = f.readlines()
    start = None
    for i, line in enumerate(lines):
        if line.startswith("var m7MultiResultFuncs = "):
            start = i
            break
    if start is None:
        raise SystemExit("check_mr_table_subset: m7MultiResultFuncs "
                         "literal not found in %s" % path)
    rows = {}
    for line in lines[start + 1:]:
        if line.rstrip("\n") == "}":
            break
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        m = COMMITTED_ROW_RE.match(line.rstrip("\n"))
        if not m:
            raise SystemExit("check_mr_table_subset: unparseable committed "
                             "m7 row: %r" % line.rstrip("\n"))
        pkg, name, recv, helper, pair, fields, _fallback, _comment = m.groups()
        key = (pkg, name, recv)
        if key in rows:
            raise SystemExit("check_mr_table_subset: duplicate committed "
                             "m7 row for %r" % (key,))
        rows[key] = (helper, pair, tuple(parse_fields(fields)))
    else:
        raise SystemExit("check_mr_table_subset: unterminated "
                         "m7MultiResultFuncs literal in %s" % path)
    return rows


def main():
    go = os.environ.get("GO", "go")
    try:
        goroot = subprocess.run(
            [go, "env", "GOROOT"], capture_output=True, text=True,
            check=True, timeout=60).stdout.strip()
    except Exception as e:
        raise SystemExit("check_mr_table_subset: cannot determine GOROOT "
                         "via %r (set GO to the go1.27.1 binary): %s"
                         % (go, e))
    if not goroot:
        raise SystemExit("check_mr_table_subset: empty GOROOT from %r" % go)
    if "go1.27.1" not in subprocess.run(
            [go, "version"], capture_output=True, text=True,
            timeout=60).stdout:
        raise SystemExit("check_mr_table_subset: generator requires "
                         "go1.27.1 sources; %r reports otherwise" % go)

    with tempfile.TemporaryDirectory(prefix="mr-table-subset-") as tmp:
        outdir = os.path.join(tmp, "out")
        os.mkdir(outdir)
        # The generator shells out to the extract_decls helper, which
        # run_gen_synth.py builds into its stage dir (the in-repo
        # default path holds only main.go, no binary). Build it here
        # into the temp dir, reusing the ambient Go build cache exactly
        # like run_gen_synth.py does (a cold cache makes this build
        # minutes slow; by the time `make check` runs, `generate` has
        # already warmed the cache for the go/* packages).
        helper_src = os.path.join(REPO, "internal", "frontend",
                                  "gen_synth_helper", "extract_decls")
        extract_decls = os.path.join(tmp, "extract_decls")
        build_env = dict(os.environ)
        build_env["GOTMPDIR"] = tmp
        try:
            r = subprocess.run(
                [go, "build", "-o", extract_decls, "."],
                cwd=helper_src, env=build_env, capture_output=True,
                text=True, timeout=900)
        except subprocess.TimeoutExpired:
            raise SystemExit("check_mr_table_subset: extract_decls build "
                             "timed out")
        if r.returncode != 0:
            raise SystemExit("check_mr_table_subset: extract_decls build "
                             "failed:\n%s%s" % (r.stdout, r.stderr))
        env = dict(os.environ)
        env["GO"] = go
        env["GRAALGO_GOROOT"] = goroot
        env["GEN_SYNTH_OUTDIR"] = outdir
        env["GEN_SYNTH_TMPDIR"] = tmp
        env["GEN_SYNTH_EXTRACT_DECLS"] = extract_decls
        try:
            r = subprocess.run(
                [sys.executable, GENERATOR], env=env, capture_output=True,
                text=True, timeout=600)
        except subprocess.TimeoutExpired:
            raise SystemExit("check_mr_table_subset: generator timed out")
        if r.returncode != 0:
            raise SystemExit("check_mr_table_subset: generator failed:\n%s%s"
                             % (r.stdout, r.stderr))
        entries = os.path.join(tmp, "archive_mime_table_entries.txt")
        generated = parse_generated(entries)
    committed = parse_committed_m7(TABLE)
    committed_am = {k: v for k, v in committed.items()
                    if k[0].startswith("archive/") or k[0].startswith("mime")}
    if not committed_am:
        raise SystemExit("check_mr_table_subset: zero committed "
                         "archive/mime m7 rows parsed (parser drift?)")

    problems = []
    for key in sorted(generated):
        want = generated[key]
        if key in EXCLUSIONS:
            if key in committed:
                problems.append(
                    "%r is an exclusion but now has a committed row; "
                    "remove it from EXCLUSIONS (or the row)" % (key,))
            continue
        if key not in committed:
            problems.append(
                "%r: generated row missing from m7MultiResultFuncs "
                "(helper %s, pair %s, fields %s)"
                % (key, want[0], want[1], list(want[2])))
            continue
        got = committed[key]
        if got != want:
            problems.append(
                "%r: committed row %s != generated row %s"
                % (key, got, want))
    if problems:
        sys.stderr.write(
            "check_mr_table_subset: FAIL (%d problem(s)):\n" % len(problems))
        for p in problems:
            sys.stderr.write("  - %s\n" % p)
        return 1
    print("check_mr_table_subset: OK (%d generated rows covered by %d "
          "committed archive/mime rows, %d exclusions)"
          % (len(generated), len(committed_am), len(EXCLUSIONS)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
