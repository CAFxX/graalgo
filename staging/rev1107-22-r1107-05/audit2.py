#!/usr/bin/env python3
"""R-1107-05 re-test audit v2 (read-only): WS-D-family fail-fast panic stubs
in internal/frontend/*.go (non-test) vs Appendix-M desugar tables.

Categories (by panic-message shape; a site matches the first that fits):
  A canonical  : 'multi-result calls need WS D'
  B m7-desugar : 'need the M7 desugar'
  C 2-result-iface: 'needs 2-result interface dispatch'
  D mr-iface   : 'multi-result interface dispatch needs WS D'
  E mrM8       : 'mrM8 desugar'
  F retired    : 'desugar did not fire' OR 'use __<helper> helper (WS D)'
  G other WS D : panic line contains 'WS D' but none of the above
  H m7-3target : 'only 2-target' / 'only the ... 2-target form desugars'
     (overlaps A; counted inside A, flagged separately)
Usage: python3 audit2.py <graalgo-root>  -> stdout report + audit2.json
"""
import json, os, re, sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/workspace/graalgo")
FE = os.path.join(ROOT, "internal/frontend")
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "audit2.json")

TABLE_VARS = ["m7MultiResultFuncs", "m8MultiResultFuncs", "osMultiResultFuncs",
              "m9MultiResultFuncs", "ioMultiResultFuncs",
              "m8IfaceMultiResultMethods", "m5StaticMREntries"]
ENTRY3_RE = re.compile(r'\{\s*"([^"]+)",\s*"([^"]+)",\s*"([^"]*)"')
ENTRY2_RE = re.compile(r'\{\s*"([^"]+)",\s*"([^"]+)",')

def parse_tables():
    text = open(os.path.join(FE, "mrdesugar_m5.go")).read()
    tables, raw_counts = {}, {}
    for var in TABLE_VARS:
        m = re.search(r"var %s = \[\]\w+?\{(.*?)\n\}" % re.escape(var), text, re.S)
        if not m:
            tables[var] = []
            continue
        block = m.group(1)
        raw_counts[var] = block.count('{"')
        entries = []
        if var == "m8IfaceMultiResultMethods":
            for em in ENTRY2_RE.finditer(block):
                entries.append((em.group(1), em.group(2), ""))
        else:
            for em in ENTRY3_RE.finditer(block):
                entries.append((em.group(1), em.group(2), em.group(3)))
        tables[var] = entries
    return tables, raw_counts

FUNC_RE = re.compile(r"^func\s+(?:\(\s*\w+\s+\*?([\w\[\]]+)\s*\)\s*)?(\w+)\s*\(")
COMMENT_RE = re.compile(r"^\s*//")
PANIC_RE = re.compile(r"\bpanic\(")
# message prefix -> pkg path normalisation for bare single-word prefixes
BARE_PKG = {"csv": "encoding/csv", "xml": "encoding/xml", "json": "encoding/json",
            "gob": "encoding/gob", "asn1": "encoding/asn1", "pem": "encoding/pem",
            "hex": "encoding/hex", "base32": "encoding/base32",
            "base64": "encoding/base64", "binary": "encoding/binary",
            "ascii85": "encoding/ascii85", "mime": "mime",
            "multipart": "mime/multipart", "quotedprintable": "mime/quotedprintable",
            "tar": "archive/tar", "zip": "archive/zip", "iotest": "testing/iotest",
            "netip": "net/netip", "neturl": "net/url"}

def msg_pkg(text):
    """Parse the stdlib pkg path from the panic message prefix."""
    m = re.match(r'\s*panic\("\s*\{?pkg_path\}?', text)
    if m:
        return None  # template placeholder, no real pkg
    m = re.match(r'\s*panic\("\s*([\w/]+?)[.:]', text)
    if not m:
        return None
    p = m.group(1)
    if "/" in p:
        return p
    return BARE_PKG.get(p, p)

def category(text):
    if "desugar did not fire" in text or re.search(r"use __\w+ helper \(WS D\)", text):
        return "F"
    if "multi-result interface dispatch needs WS D" in text:
        return "D"
    if "needs 2-result interface dispatch" in text:
        return "C"
    if "need the M7 desugar" in text:
        return "B"
    if "mrM8 desugar" in text:
        return "E"
    if "multi-result calls need WS D" in text:
        return "A"
    if "WS D" in text:
        return "G"
    return None

def enclosing_func(lines, idx):
    for j in range(idx, max(-1, idx - 150), -1):
        m = FUNC_RE.match(lines[j])
        if m:
            return (m.group(1) or "", m.group(2), j + 1)
    return ("", None, None)

def collect():
    stubs, comments = [], []
    for dirpath, _, files in os.walk(FE):
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, ROOT)
            lines = open(path, errors="replace").read().split("\n")
            for i, line in enumerate(lines):
                if "WS D" not in line and "M7 desugar" not in line \
                   and "2-result interface dispatch" not in line \
                   and "mrM8 desugar" not in line:
                    continue
                if COMMENT_RE.match(line):
                    comments.append({"file": rel, "line": i + 1, "text": line.strip()[:160]})
                    continue
                is_panic = bool(PANIC_RE.search(line))
                if not is_panic:
                    prev = lines[i - 1] if i > 0 else ""
                    is_panic = "panic(" in prev and not prev.rstrip().endswith('")')
                if not is_panic:
                    continue
                cat = category(line)
                if cat is None:
                    continue
                recv, name, decl_line = enclosing_func(lines, i)
                stubs.append({"file": rel, "line": i + 1, "cat": cat,
                              "pkg_msg": msg_pkg(line), "recv": recv or "",
                              "name": name, "decl_line": decl_line,
                              "text": line.strip()[:200]})
    return stubs, comments

def main():
    tables, raw_counts = parse_tables()
    covered = set()
    for var, entries in tables.items():
        for (pkg, name, recv) in entries:
            covered.add((pkg, name, recv))
    stubs, comments = collect()
    helpers_cache = {}
    def helper_hits(r):
        p = os.path.join(ROOT, r["file"])
        if p not in helpers_cache:
            helpers_cache[p] = re.findall(r"func\s+(__\w+)\s*\(", open(p, errors="replace").read())
        if not r["name"]:
            return []
        needle = r["name"].lower()
        return [h for h in helpers_cache[p] if needle in h.lower()]
    for s in stubs:
        pkg, name, recv = s["pkg_msg"], s["name"], s["recv"]
        key = None
        if pkg and name:
            for k in [(pkg, name, recv), (pkg, name, "")]:
                if k in covered:
                    key = k
                    break
        s["covered"] = key is not None
        s["table_key"] = list(key) if key else None
        if not s["covered"]:
            s["helper_hits"] = helper_hits(s)
    cats = {}
    for s in stubs:
        cats.setdefault(s["cat"], []).append(s)
    print("== table parse sanity (parsed rows vs raw '{\" count) ==")
    for v in TABLE_VARS:
        print(f"  {v}: parsed={len(tables[v])} raw={raw_counts.get(v)}")
    print(f"  TOTAL rows: {sum(len(e) for e in tables.values())}")
    print("== stub sites by category ==")
    for c in "ABCDEFG":
        ss = cats.get(c, [])
        cov = sum(1 for s in ss if s["covered"])
        print(f"  {c}: total={len(ss)} covered={cov} uncovered={len(ss)-cov}")
    print(f"  TOTAL panic stubs: {len(stubs)}  covered={sum(1 for s in stubs if s['covered'])}"
          f" uncovered={sum(1 for s in stubs if not s['covered'])}")
    print(f"  comment mentions: {len(comments)}")
    unc = [s for s in stubs if not s["covered"]]
    print("== uncovered by category ==")
    for c in "ABCDEFG":
        ss = [s for s in unc if s["cat"] == c]
        if not ss:
            continue
        wh = sum(1 for s in ss if s.get("helper_hits"))
        print(f"  {c}: {len(ss)} uncovered ({wh} w/ like-named __helper in file)")
    print("== uncovered by package (msg-derived pkg) ==")
    bypkg = {}
    for s in unc:
        bypkg.setdefault(s["pkg_msg"] or "?", []).append(s)
    for pkg in sorted(bypkg):
        ss = bypkg[pkg]
        wh = sum(1 for s in ss if s.get("helper_hits"))
        print(f"  {pkg}: {len(ss)} ({wh} w/ helper)")
    print("== uncovered with NO helper, by file (first 60) ==")
    n = 0
    for s in unc:
        if not s.get("helper_hits"):
            print(f"  {s['file']}:{s['line']} [{s['cat']}] {s['pkg_msg']}.{s['recv'] + '.' if s['recv'] else ''}{s['name']}")
            n += 1
            if n >= 60:
                break
    print("== retired-form (F) sites: still carrying 're-test when WS D lands'? ==")
    f = cats.get("F", [])
    print(f"  F total={len(f)}; with 're-test when WS D lands' in text: "
          f"{sum(1 for s in f if 're-test when WS D lands' in s['text'])}")
    print(f"  F covered by table: {sum(1 for s in f if s['covered'])}")
    json.dump({"tables": {v: len(e) for v, e in tables.items()},
               "raw_counts": raw_counts, "stubs": stubs,
               "comments": comments}, open(OUT, "w"), indent=1)
    print("audit2.json written")

if __name__ == "__main__":
    main()
