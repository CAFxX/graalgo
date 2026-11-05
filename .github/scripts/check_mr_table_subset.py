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

Deliberate exclusions (generated rows with no committed row). Each
exclusion is a claim that the method is unreachable from user code —
and the claim is CHECKED, not trusted: verify_exclusions() below
re-derives it mechanically on every run (unexported name, no remaining
call site in any synthetic source, not a member of any interface
declared in the synthetic sources). A wrong claim used to pass
silently: headerFileInfo.Info was excluded as "only obtainable as
fs.FileInfo, which has no Info method", but headerFileInfo implements
fs.DirEntry, so it IS reachable (fs.ReadDir file entries are
headerFileInfo values; FileHeader.FileInfo().(fs.DirEntry).Info()).
That exclusion was removed and the row added (NEEDS-FIX re-review of
acab9a30, 2026-11-05); adding an exclusion needs a fresh review note
here AND must pass the mechanical checks.
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

import glob
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
SYNTH_DIR = os.path.join(REPO, "internal", "frontend")

# (pkgPath, name, recvType) generated rows with no committed row, by design.
EXCLUSIONS = {
    ("archive/zip", "findBodyOffset", "File"),
    ("mime/multipart", "readForm", "Reader"),
}


def synth_sources():
    """Synthetic stdlib sources (the generated Go lives inside raw-string
    consts in synth_*.go; scanning the wrapper files scans the source)."""
    return sorted(glob.glob(os.path.join(SYNTH_DIR, "synth_*.go")))


def remaining_call_sites(name):
    """`.name(` occurrences in any synthetic source (the stub definition
    `func (r T) name(` and the `__helper(` rewrites have no dot, so any
    hit is a real call site — including an interface-dispatched one,
    which can reach the concrete method through the itab)."""
    pat = re.compile(r"\." + re.escape(name) + r"\(")
    hits = []
    for path in synth_sources():
        with open(path) as f:
            for i, line in enumerate(f, 1):
                if pat.search(line):
                    if line.strip().startswith("//"):
                        continue
                    hits.append("%s:%d: %s"
                                % (os.path.basename(path), i, line.strip()))
    return hits


def interface_members():
    """Bare interface name -> member method names, transitive through
    embedding, over all `type X interface { ... }` declarations in the
    synthetic sources. A method that is a member of any interface can be
    reached by interface dispatch on any implementer."""
    ifaces = {}
    decl_re = re.compile(r"type\s+(\w+)(?:\[[^\]]*\])?\s+interface\s*\{")
    for path in synth_sources():
        with open(path) as f:
            text = f.read()
        for m in decl_re.finditer(text):
            name = m.group(1)
            start = m.end()
            depth = 1
            i = start
            while i < len(text) and depth > 0:
                c = text[i]
                if c == "{":
                    depth += 1
                elif c == "}":
                    depth -= 1
                i += 1
            if depth != 0:
                raise SystemExit(
                    "check_mr_table_subset: unterminated interface %s in %s"
                    % (name, path))
            body = text[start:i - 1]
            members, embeds = set(), set()
            for raw in body.split("\n"):
                line = raw.strip()
                if not line or line.startswith("//"):
                    continue
                if "//" in line:
                    line = line[:line.index("//")].strip()
                mm = re.match(r"(\w+)\s*\(", line)
                if mm:
                    members.add(mm.group(1))
                    continue
                em = re.match(r"^([\w./]+)(?:\[[^\]]*\])?$", line)
                if em:
                    embeds.add(em.group(1).split(".")[-1])
            e = ifaces.setdefault(name, {"members": set(), "embeds": set()})
            e["members"] |= members
            e["embeds"] |= embeds
    resolved = {}

    def all_members(n, seen):
        if n in resolved:
            return resolved[n]
        if n in seen:
            return set()
        e = ifaces.get(n)
        if e is None:
            return set()
        out = set(e["members"])
        for emb in e["embeds"]:
            out |= all_members(emb, seen | {n})
        resolved[n] = out
        return out

    return {n: all_members(n, set()) for n in ifaces}


def verify_exclusions():
    """Re-derive every EXCLUSIONS claim mechanically. Returns a list of
    problems (empty when every exclusion holds). This is the guard's
    root-cause fix: an exclusion used to be a trusted human claim, and
    a wrong one (headerFileInfo.Info) passed the check while the table
    missed a reachable row. Each exclusion must now satisfy all three:
    the method is unexported; no `.name(` call site remains in any
    synthetic source; the name is a member of no declared interface."""
    problems = []
    members_by_iface = interface_members()
    if not members_by_iface:
        raise SystemExit("check_mr_table_subset: zero interfaces parsed "
                         "from synthetic sources (parser drift?)")
    for key in sorted(EXCLUSIONS):
        _pkg, name, _recv = key
        if not name or not name[0].islower():
            problems.append(
                "%r: exclusion requires an unexported method name" % (key,))
        hits = remaining_call_sites(name)
        if hits:
            problems.append(
                "%r: excluded as unreachable but call sites remain: %s"
                % (key, "; ".join(hits)))
        hit_ifaces = sorted(
            iface for iface, members in members_by_iface.items()
            if name in members)
        if hit_ifaces:
            problems.append(
                "%r: excluded as unreachable but %s is a member of "
                "interface(s) %s (interface dispatch can reach it)"
                % (key, name, ", ".join(hit_ifaces)))
    return problems

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
    # The exclusions are claims, not facts: re-derive each one
    # mechanically (unexported, no call site, no interface member).
    # A wrong exclusion used to pass while the table missed a reachable
    # row (headerFileInfo.Info, NEEDS-FIX re-review of acab9a30).
    problems.extend(verify_exclusions())
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
    # Reverse direction: a committed archive/mime row with no generated
    # row is an orphan (e.g. a single-result method that never had a
    # helper) — it can never classify, so delete it instead of carrying it.
    for key in sorted(committed_am):
        if key not in generated and key not in EXCLUSIONS:
            problems.append(
                "%r: committed archive/mime row has no generated row "
                "(orphan; delete it)" % (key,))
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
