#!/usr/bin/env python3
"""R-1107-05 audit4 (read-only): classify the 110 TABLE-UNCOVERED stubs from
audit2.json. For each, find like-named __-helpers in the same file and
classify each helper body as real / PANIC-ONLY / absent, and note whether
any frontend test pins the callee as fail-fast (must NOT be table-added).

Reads audit2.json, writes audit4.json + stdout report.
"""
import json, os, re, sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/workspace/graalgo")
FE = os.path.join(ROOT, "internal/frontend")
D = "/tmp/rev1107-22-r1107-05"
OUT = os.path.join(D, "audit4.json")

def norm_stmt(line):
    s = line.strip()
    s = re.sub(r'^"\s*', "", s)
    s = s.replace("\\t", " ").replace('\\"', '"')
    s = re.sub(r'\\n"\s*\+?\s*$', "", s)
    s = re.sub(r'"\s*\+?\s*$', "", s)
    return s.strip()

def first_stmt_is_panic(lines, decl_idx):
    for j in range(decl_idx + 1, min(len(lines), decl_idx + 12)):
        s = norm_stmt(lines[j])
        if not s or s.startswith("//"):
            continue
        return s.startswith("panic(")
    return None

def main():
    d = json.load(open(os.path.join(D, "audit2.json")))
    stubs = [s for s in d["stubs"] if not s["covered"]]
    print(f"uncovered stubs: {len(stubs)}")
    # index helper defs per file
    helpers = {}  # file -> {name: (decl_line_idx, lines)}
    for dirpath, _, files in os.walk(FE):
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, ROOT)
            lines = open(path, errors="replace").read().split("\n")
            for i, line in enumerate(lines):
                m = re.search(r"func\s+(__\w+)\s*(?:\[[^\n]*?\])?\s*\(", line)
                if m:
                    helpers.setdefault(rel, {})[m.group(1)] = (i, lines)
    # test pins: search test files for the callee name + fail-fast markers
    test_text = {}
    for dirpath, _, files in os.walk(FE):
        for fn in files:
            if fn.endswith("_test.go"):
                p = os.path.join(dirpath, fn)
                test_text[os.path.relpath(p, ROOT)] = open(p, errors="replace").read()
    for s in stubs:
        name = s.get("name") or ""
        needle = name.lower()
        cands = []
        for hname, (di, lines) in helpers.get(s["file"], {}).items():
            if needle and needle in hname.lower():
                is_panic = first_stmt_is_panic(lines, di)
                body = ("PANIC-ONLY" if is_panic is True else
                        "real" if is_panic is False else "UNDETERMINED")
                cands.append({"helper": hname, "def_line": di + 1, "body": body})
        s["helper_cands"] = cands
        # test pins mentioning this pkg.name
        callee = (s.get("pkg_msg") or "") + "." + (s.get("recv") + "." if s.get("recv") else "") + name
        pins = [f for f, t in test_text.items()
                if name and name in t and ("WS D" in t or "FailsFast" in t or "fail fast" in t)]
        s["test_pin_files"] = pins
    n_real = sum(1 for s in stubs if any(c["body"] == "real" for c in s["helper_cands"]))
    n_paniconly = sum(1 for s in stubs if s["helper_cands"] and all(c["body"] == "PANIC-ONLY" for c in s["helper_cands"]))
    n_none = sum(1 for s in stubs if not s["helper_cands"])
    n_mixed = len(stubs) - n_real - n_paniconly - n_none
    print(f"  with >=1 REAL like-named helper: {n_real}")
    print(f"  helpers all PANIC-ONLY: {n_paniconly}")
    print(f"  no like-named helper: {n_none}")
    print(f"  mixed/undetermined: {n_mixed}")
    print("== REAL-helper candidates (table-row-add fixable?) ==")
    for s in stubs:
        if any(c["body"] == "real" for c in s["helper_cands"]):
            real = [c["helper"] for c in s["helper_cands"] if c["body"] == "real"]
            print(f"  {s['file']}:{s['line']} [{s['cat']}] {s.get('pkg_msg')}.{s.get('recv')+'.' if s.get('recv') else ''}{s.get('name')} helpers={real} pins={s['test_pin_files']}")
    print("== PANIC-ONLY-helper stubs (live gap, helper itself a stub) ==")
    for s in stubs:
        if s["helper_cands"] and all(c["body"] == "PANIC-ONLY" for c in s["helper_cands"]):
            print(f"  {s['file']}:{s['line']} [{s['cat']}] {s.get('pkg_msg')}.{s.get('recv')+'.' if s.get('recv') else ''}{s.get('name')} helpers={[c['helper'] for c in s['helper_cands']]}")
    print("== NO-helper stubs (no implementation anywhere in file) ==")
    for s in stubs:
        if not s["helper_cands"]:
            print(f"  {s['file']}:{s['line']} [{s['cat']}] {s.get('pkg_msg')}.{s.get('recv')+'.' if s.get('recv') else ''}{s.get('name')}")
    json.dump({"stubs": stubs}, open(OUT, "w"), indent=1)
    print("audit4.json written")

if __name__ == "__main__":
    main()
