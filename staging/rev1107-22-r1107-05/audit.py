#!/usr/bin/env python3
"""R-1107-05 re-test audit (read-only): cross-reference WS-D-family fail-fast
panic stubs in internal/frontend/synth_*.go against the Appendix-M desugar
tables in internal/frontend/mrdesugar_m5.go.

Usage: python3 audit.py <graalgo-root>
Output: audit report on stdout; machine-readable JSON to audit.json (same dir).
"""
import json, os, re, sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/workspace/graalgo")
FE = os.path.join(ROOT, "internal/frontend")

# --- 1. Parse desugar tables from mrdesugar_m5.go -------------------------
TABLE_VARS = ["m7MultiResultFuncs", "m8MultiResultFuncs", "osMultiResultFuncs",
              "m9MultiResultFuncs", "ioMultiResultFuncs",
              "m8IfaceMultiResultMethods", "m5StaticMREntries"]
ENTRY_RE = re.compile(r'\{\s*"([^"]+)",\s*"([^"]+)",\s*(?:"([^"]*)")?')

def parse_tables():
    path = os.path.join(FE, "mrdesugar_m5.go")
    text = open(path).read()
    tables = {}
    for var in TABLE_VARS:
        m = re.search(r"var %s = \[\]\w+?\{(.*?)\n\}" % re.escape(var), text, re.S)
        if not m:
            tables[var] = []
            continue
        entries = []
        for em in ENTRY_RE.finditer(m.group(1)):
            pkg, name, recv = em.group(1), em.group(2), em.group(3) or ""
            entries.append((pkg, name, recv))
        tables[var] = entries
    return tables

# --- 2. Collect WS-D-family stub sites ------------------------------------
FAMILY_RES = [
    re.compile(r"multi-result calls? need WS D"),
    re.compile(r"need the M7 desugar"),
    re.compile(r"needs 2-result interface dispatch"),
    re.compile(r"multi-result interface dispatch needs WS D"),
    re.compile(r"multi-result call needs the mrM8 desugar"),
    re.compile(r"re-test: WS D"),
    re.compile(r"re-test when WS D lands"),
]
PANIC_RE = re.compile(r"\bpanic\(")
FUNC_RE = re.compile(r"^func\s+(?:\(\s*\*?(\w+)\s*\)\s*)?(\w+)\s*\(")
COMMENT_RE = re.compile(r"^\s*//")

# file stem -> stdlib package path (only where naive _ -> / is wrong)
PKG_OVERRIDE = {
    "synth_base32": "encoding/base32", "synth_base64": "encoding/base64",
    "synth_binary": "encoding/binary", "synth_csv": "encoding/csv",
    "synth_json": "encoding/json", "synth_xml": "encoding/xml",
    "synth_gob": "encoding/gob", "synth_asn1": "encoding/asn1",
    "synth_pem": "encoding/pem", "synth_hex": "encoding/hex",
    "synth_ascii85": "encoding/ascii85",
    "synth_archive_tar": "archive/tar", "synth_archive_zip": "archive/zip",
    "synth_crypto_aes": "crypto/aes",
    "synth_math": "math", "synth_sort": "sort",
    "synth_testing": "testing", "synth_iotest": "testing/iotest",
    "synth_context": "context", "synth_io": "io",
    "synth_strings": "strings", "synth_bytes": "bytes",
    "synth_time": "time", "synth_errors": "errors",
    "synth_mime": "mime", "synth_mime_multipart": "mime/multipart",
    "synth_mime_quotedprintable": "mime/quotedprintable",
    "synth_net": "net", "synth_net_http": "net/http",
    "synth_net_url": "net/url", "synth_net_netip": "net/netip",
    "synth_crypto": "crypto", "synth_os": "os",
    "synth_path": "path", "synth_path_filepath": "path/filepath",
    "synth_sync": "sync", "synth_runtime": "runtime",
    "synth_reflect": "reflect", "synth_slices": "slices",
    "synth_maps": "maps", "synth_strconv": "strconv",
    "synth_unicode": "unicode", "synth_unicode_utf8": "unicode/utf8",
    "synth_bufio": "bufio", "synth_fmt": "fmt",
    "synth_hash": "hash", "synth_crypto_cipher": "crypto/cipher",
}

def file_pkg(path):
    stem = os.path.basename(path)[:-3]
    if stem in PKG_OVERRIDE:
        return PKG_OVERRIDE[stem]
    rest = stem[len("synth_"):] if stem.startswith("synth_") else stem
    return rest.replace("_", "/")

def enclosing_func(lines, idx):
    for j in range(idx, max(-1, idx - 120), -1):
        m = FUNC_RE.match(lines[j])
        if m:
            return (m.group(1) or "", m.group(2), j + 1)
    return ("", None, None)

def collect_stubs():
    stubs = []  # dicts
    for dirpath, _, files in os.walk(FE):
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, ROOT)
            lines = open(path, errors="replace").read().split("\n")
            for i, line in enumerate(lines):
                if not any(r.search(line) for r in FAMILY_RES):
                    continue
                kind = "comment" if COMMENT_RE.match(line) else None
                is_panic = bool(PANIC_RE.search(line))
                if not is_panic and kind is None:
                    # continuation line of a multi-line panic? look back
                    prev = "\n".join(lines[max(0, i - 2):i])
                    is_panic = "panic(" in prev and "panic(" in lines[i - 1] if i > 0 else False
                    kind = "continuation?" if is_panic else "other"
                if kind == "comment":
                    stubs.append({"file": rel, "line": i + 1, "kind": "comment",
                                  "text": line.strip()[:160]})
                    continue
                if not is_panic:
                    stubs.append({"file": rel, "line": i + 1, "kind": kind or "other",
                                  "text": line.strip()[:160]})
                    continue
                recv, name, decl_line = enclosing_func(lines, i)
                stubs.append({"file": rel, "line": i + 1, "kind": "stub",
                              "pkg_file": file_pkg(path), "recv": recv or "",
                              "name": name, "decl_line": decl_line,
                              "text": line.strip()[:200]})
    return stubs

# --- 3. Cross-reference ----------------------------------------------------
def main():
    tables = parse_tables()
    covered = set()  # (pkg, name, recv); recv "" matches any recv for iface tables
    for var, entries in tables.items():
        for (pkg, name, recv) in entries:
            covered.add((pkg, name, recv))
    table_counts = {v: len(e) for v, e in tables.items()}
    stubs = collect_stubs()
    panic_stubs = [s for s in stubs if s["kind"] == "stub"]
    comments = [s for s in stubs if s["kind"] == "comment"]
    others = [s for s in stubs if s["kind"] not in ("stub", "comment")]

    # match: exact (pkg,name,recv) or (pkg,name,"") or iface (pkg,name, any)
    def match(s):
        pkg, name, recv = s["pkg_file"], s["name"], s["recv"]
        if name is None:
            return None
        for key in [(pkg, name, recv), (pkg, name, ""),
                    (pkg, name, "*iface*")]:
            if key in covered:
                return key
        # also try message-derived pkg: parse "pkg: Name" / "pkg.Name:" prefix
        m = re.match(r'"?([\w/]+)[.:]\s*([\w.]+)', s["text"].replace("panic(\"", ""))
        return None

    rows = []
    for s in panic_stubs:
        key = match(s)
        rows.append({**s, "table_key": list(key) if key else None,
                     "covered": key is not None})
    n_covered = sum(1 for r in rows if r["covered"])
    n_uncovered = len(rows) - n_covered

    # helper-existence heuristic for uncovered: any __func in same file whose
    # name contains the stub name (case-insensitive)
    helper_cache = {}
    def has_helper(r):
        p = os.path.join(ROOT, r["file"])
        if p not in helper_cache:
            helper_cache[p] = re.findall(r"func\s+(__\w+)\s*\(", open(p, errors="replace").read())
        if not r["name"]:
            return None
        needle = r["name"].lower()
        hits = [h for h in helper_cache[p] if needle in h.lower()]
        return hits

    for r in rows:
        if not r["covered"]:
            r["helper_hits"] = has_helper(r)

    print("== table sizes ==")
    for v, c in table_counts.items():
        print(f"  {v}: {c}")
    print(f"  TOTAL table rows: {sum(table_counts.values())}")
    print("== stub counts ==")
    print(f"  WS-D-family panic stubs (non-test .go under internal/frontend/): {len(panic_stubs)}")
    print(f"  comment mentions: {len(comments)}")
    print(f"  other/continuation: {len(others)}")
    print(f"  covered by a table row: {n_covered}")
    print(f"  NOT covered by any table row: {n_uncovered}")
    unc = [r for r in rows if not r["covered"]]
    unc_helper = [r for r in unc if r.get("helper_hits")]
    unc_nohelper = [r for r in unc if not r.get("helper_hits")]
    print(f"  uncovered WITH a like-named __helper in file (heuristic): {len(unc_helper)}")
    print(f"  uncovered with NO like-named __helper: {len(unc_nohelper)}")
    print("== uncovered by package ==")
    bypkg = {}
    for r in unc:
        bypkg.setdefault(r["pkg_file"], []).append(r)
    for pkg in sorted(bypkg):
        print(f"  {pkg}: {len(bypkg[pkg])} uncovered "
              f"({sum(1 for r in bypkg[pkg] if r.get('helper_hits'))} w/ helper)")
    print("== covered by package (top) ==")
    bypkg2 = {}
    for r in rows:
        if r["covered"]:
            bypkg2.setdefault(r["pkg_file"], []).append(r)
    for pkg in sorted(bypkg2):
        print(f"  {pkg}: {len(bypkg2[pkg])}")
    print("== files with stubs outside top-level synth_*.go ==")
    for r in rows:
        if "/synth_" not in r["file"] or "/p3frontend/" in r["file"]:
            print(f"  {r['file']}:{r['line']} {r.get('name')} recv={r.get('recv')}")
    json.dump({"tables": table_counts, "rows": rows,
               "counts": {"panic_stubs": len(panic_stubs), "comments": len(comments),
                          "other": len(others), "covered": n_covered,
                          "uncovered": n_uncovered,
                          "uncovered_with_helper": len(unc_helper),
                          "uncovered_no_helper": len(unc_nohelper)}},
              open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "audit.json"), "w"),
              indent=1)
    print("audit.json written")

if __name__ == "__main__":
    main()
