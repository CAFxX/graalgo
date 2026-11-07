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
ROW_RE = re.compile(r'(?m)^\s*\{\s*"([^"]+)",\s*"([^"]+)",\s*"([^"]*)"(?:,\s*"([^"]*)")?(?:,\s*"([^"]*)")?(?:,\s*"([^"]*)")?')

def parse_tables():
    text = open(os.path.join(FE, "mrdesugar_m5.go")).read()
    # drop full-line comments: table blocks quote rows in comments, which
    # would otherwise fake coverage
    text = "\n".join(l for l in text.split("\n")
                     if not l.strip().startswith("//"))
    tables, raw_counts = {}, {}
    for var in TABLE_VARS:
        m = re.search(r"var %s = \[\]\w+?\{(.*?)\n\}" % re.escape(var), text, re.S)
        if not m:
            tables[var] = []
            continue
        block = m.group(1)
        raw_counts[var] = len(re.findall(r"(?m)^\s*\{\"", block))
        entries = []
        for em in ROW_RE.finditer(block):
            g = em.groups()
            pkg, name, recv = g[0], g[1], g[2]
            if var == "m8IfaceMultiResultMethods":
                helper, pair = "", g[2]  # 3rd field is the pair type
                recv = ""
            elif var in ("m8MultiResultFuncs", "osMultiResultFuncs", "m9MultiResultFuncs"):
                helper, pair = g[3] or "", g[5] or ""
            elif var == "m5StaticMREntries":
                helper, pair = "", ""
            else:  # m7 / io tables: pkg,name,recv,helper,pair,fields
                helper, pair = g[3] or "", g[4] or ""
            entries.append({"pkg": pkg, "name": name, "recv": recv,
                            "helper": helper, "pair": pair, "table": var})
        tables[var] = entries
    return tables, raw_counts

FUNC_RE = re.compile(r"func\s+(?:\(\s*\w+\s+\*?([\w]+)(?:\[[^\n]*?\])?\s*\)\s*)?(\w+)\s*(?:\[[^\n]*?\])?\s*\(")
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
            "netip": "net/netip", "neturl": "net/url",
            "fs": "io/fs", "url": "net/url", "flate": "compress/flate",
            "gzip": "compress/gzip", "zlib": "compress/zlib",
            "lzw": "compress/lzw", "bzip2": "compress/bzip2", "hash": "hash",
            "filepath": "path/filepath"}

def msg_pkg(text):
    """Parse the stdlib pkg path from the panic message prefix.

    Handles plain Go lines (panic("archive/tar.Next: ...")) and embedded
    Go-string lines ('"\\tpanic(\\"math/big: X: ...\\")\\n" +').
    """
    m = re.search(r'panic\(\s*\\*"?\s*\{?pkg_path\}?', text)
    if m:
        return None  # template placeholder, no real pkg
    m = re.search(r'panic\(\s*\\*"?\s*(golang\.org/[\w/]+)', text)
    if m:
        return m.group(1)
    m = re.search(r'panic\(\s*\\*"?\s*([\w/]+?)[.:]', text)
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
    if "multi-result desugar" in text:
        return "H"
    if "WS D" in text:
        return "G"
    if "multi-result" in text or "desugar" in text:
        return "I"
    return None

def _func_line(line):
    # synth sources embedded as raw strings / quoted string literals carry
    # the decl indented or behind a leading quote + \t escapes; normalise
    s = line.strip()
    s = re.sub(r'^"\s*', "", s)
    s = s.replace("\\t", " ").replace('\\"', '"')
    return s

def enclosing_func(lines, idx):
    for j in range(idx, max(-1, idx - 150), -1):
        m = FUNC_RE.search(_func_line(lines[j]))
        if m:
            return (m.group(1) or "", m.group(2), j + 1)
    return ("", None, None)

def collect():
    stubs, comments, templates = [], [], []
    for dirpath, _, files in os.walk(FE):
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, ROOT)
            lines = open(path, errors="replace").read().split("\n")
            for i, line in enumerate(lines):
                if "WS D" not in line and "multi-result" not in line \
                   and "desugar" not in line:
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
                if "{pkg_path}" in line:
                    templates.append({"file": rel, "line": i + 1, "text": line.strip()[:160]})
                    continue
                cat = category(line)
                if cat is None:
                    continue
                recv, name, decl_line = enclosing_func(lines, i)
                stubs.append({"file": rel, "line": i + 1, "cat": cat,
                              "pkg_msg": msg_pkg(line), "recv": recv or "",
                              "name": name, "decl_line": decl_line,
                              "text": line.strip()[:200]})
    return stubs, comments, templates

def main():
    tables, raw_counts = parse_tables()
    covered = {}  # (pkg, name, recv) -> entry dict
    for var, entries in tables.items():
        for e in entries:
            covered[(e["pkg"], e["name"], e["recv"])] = e
    stubs, comments, templates = collect()
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
        entry = None
        if pkg and name:
            for k in [(pkg, name, recv), (pkg, name, "")]:
                if k in covered:
                    entry = covered[k]
                    break
        s["covered"] = entry is not None
        s["table_key"] = [entry["pkg"], entry["name"], entry["recv"]] if entry else None
        s["helper"] = entry["helper"] if entry else ""
        s["pair"] = entry["pair"] if entry else ""
        s["table"] = entry["table"] if entry else ""
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
    for c in "ABCDEFGHI":
        ss = cats.get(c, [])
        cov = sum(1 for s in ss if s["covered"])
        print(f"  {c}: total={len(ss)} covered={cov} uncovered={len(ss)-cov}")
    print(f"  TOTAL panic stubs: {len(stubs)}  covered={sum(1 for s in stubs if s['covered'])}"
          f" uncovered={sum(1 for s in stubs if not s['covered'])}")
    print(f"  comment lines mentioning WS D/multi-result/desugar: {len(comments)}")
    print(f"  template-placeholder sites (excluded): {len(templates)}")
    for t in templates:
        print(f"    {t['file']}:{t['line']} {t['text']}")
    unc = [s for s in stubs if not s["covered"]]
    print("== uncovered by category ==")
    for c in "ABCDEFGHI":
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
               "comments": comments, "templates": templates}, open(OUT, "w"), indent=1)
    print("audit2.json written")

if __name__ == "__main__":
    main()
