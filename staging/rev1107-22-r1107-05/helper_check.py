#!/usr/bin/env python3
"""R-1107-05 helper-body check (read-only): for every table-covered stub,
is the table's __-helper a REAL implementation or itself a panic-only stub?
Reads audit2.json (audit3.py output), writes helper_check.json + summary.
"""
import json, os, re, sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser("~/workspace/graalgo")
FE = os.path.join(ROOT, "internal/frontend")
AUDIT = "/tmp/rev1107-22-r1107-05/audit2.json"
OUT = "/tmp/rev1107-22-r1107-05/helper_check.json"

def norm_stmt(line):
    s = line.strip()
    s = re.sub(r'^"\s*', "", s)          # embedded-string open quote
    s = s.replace("\\t", " ").replace('\\"', '"')
    s = re.sub(r'\\n"\s*\+?\s*$', "", s)  # embedded-string close
    s = re.sub(r'"\s*\+?\s*$', "", s)
    return s.strip()

def first_stmt_is_panic(lines, decl_idx):
    for j in range(decl_idx + 1, min(len(lines), decl_idx + 12)):
        s = norm_stmt(lines[j])
        if not s or s.startswith("//"):
            continue
        return s.startswith("panic(")
    return None  # no statement found (should not happen)

def main():
    d = json.load(open(AUDIT))
    stubs = d["stubs"]
    # index helper definitions: name -> list of (file, decl_line_idx, lines)
    helper_defs = {}
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
                    helper_defs.setdefault(m.group(1), []).append((rel, i, lines))
    covered = [s for s in stubs if s["covered"]]
    print(f"covered stubs: {len(covered)}")
    n_real, n_stub, n_noddef, n_nohelper = 0, 0, 0, 0
    stub_helpers = []
    no_helper = []
    for s in covered:
        h = s.get("helper") or ""
        if not h:
            n_nohelper += 1
            no_helper.append(s)
            s["helper_body"] = "no-helper (iface/static row)"
            continue
        defs = helper_defs.get(h, [])
        if not defs:
            n_noddef += 1
            s["helper_body"] = "NO DEF FOUND"
            stub_helpers.append(s)
            continue
        # prefer same-file definition
        same = [x for x in defs if x[0] == s["file"]]
        use = same[0] if same else defs[0]
        is_panic = first_stmt_is_panic(use[2], use[1])
        if is_panic is True:
            n_stub += 1
            s["helper_body"] = "PANIC-ONLY"
            s["helper_def"] = f"{use[0]}:{use[1]+1}"
            stub_helpers.append(s)
        elif is_panic is False:
            n_real += 1
            s["helper_body"] = "real"
        else:
            n_noddef += 1
            s["helper_body"] = "UNDETERMINED"
            stub_helpers.append(s)
    print(f"  helper real: {n_real}")
    print(f"  helper PANIC-ONLY (row exists, helper is a stub): {n_stub}")
    print(f"  helper def not found / undetermined: {n_noddef}")
    print(f"  no helper in row (iface/static): {n_nohelper}")
    print("== covered stubs whose helper is PANIC-ONLY (live failures) ==")
    for s in stub_helpers:
        print(f"  {s['file']}:{s['line']} [{s['cat']}] {s['pkg_msg']}.{s['recv']}.{s['name']} "
              f"helper={s.get('helper')} def={s.get('helper_def','?')} body={s['helper_body']}")
    print("== covered stubs with NO helper in row ==")
    for s in no_helper:
        print(f"  {s['file']}:{s['line']} [{s['cat']}] {s['pkg_msg']}.{s['recv']}.{s['name']} "
              f"table={s.get('table')} pair={s.get('pair')}")
    json.dump({"stubs": stubs}, open(OUT, "w"), indent=1)
    print("helper_check.json written")

if __name__ == "__main__":
    main()
