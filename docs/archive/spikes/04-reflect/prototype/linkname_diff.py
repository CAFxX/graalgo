#!/usr/bin/env python3
"""Per-release linkname-diff scoping rule (spike 04-reflect, step 5).

Usage: linkname_diff.py <old_go_tree> <new_go_tree>

Extracts the reflect<->runtime linkname edge set from each tree and classifies
the delta:
  REMOVED          edge in old tree, gone in new -> manual adaptation or drop (SKIP-LIST)
  ADDED            edge new in new tree -> new intrinsic-surface requirement (WATCH-LIST)
  SIG-CHANGED      same edge name, different Go signature -> adapter needed (SKIP-LIST)
  UNCHANGED        present in both with identical signature

The skip-list is the set needing manual work. Kill trigger (study section 8.4):
skip-list growth > 15 in two consecutive releases defeats the diff-scoping
design.

Edge extraction rules (go1.27.1 layout):
  - runtime->reflect: `//go:linkname <impl> reflect.<name>` in runtime/, internal/
    (covers `//go:linknamestd` too); the <name> is the edge.
  - reflect->runtime: `//go:linkname <local> runtime.<name>` in reflect/;
    the <name> is the edge.
  - push markers: `//go:linkname <local>` (single arg) in reflect/; tracked
    separately as the ecosystem-compat set (no in-tree consumer).
Test files and badlinkname.go are excluded: the badlinkname_* hooks are
explicitly unstable by design and must never count toward the skip-list.
"""
import os, re, sys

def go_files(tree, sub):
    out = []
    root = os.path.join(tree, sub)
    for dp, dn, fn in os.walk(root):
        dn[:] = [d for d in dn if d != "testdata"]
        for f in fn:
            if f.endswith(".go") and not f.endswith("_test.go"):
                out.append(os.path.join(dp, f))
    return out

def signature_of(path, lineno, decl_re):
    """Grab the func signature following a linkname directive (best effort)."""
    with open(path) as f:
        lines = f.readlines()
    # look ahead up to 6 lines for the func declaration
    for j in range(lineno, min(lineno + 6, len(lines))):
        m = re.match(r"\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z0-9_]+)\s*(\(.*\))\s*(.*)\{?\s*$",
                     lines[j])
        if m:
            sig = m.group(2)
            ret = m.group(3).strip().rstrip("{").strip()
            return f"{m.group(1)}{sig} {ret}".strip()
    return "?"

def extract(tree):
    rt_to_ref = {}   # edge reflect.<name> -> (impl_symbol, signature, file)
    ref_to_rt = {}   # edge runtime.<name> -> (local, signature, file)
    pushes = set()   # ecosystem push markers
    # runtime -> reflect (src/arena holds the goexperiment.arenas-gated edge)
    for path in (go_files(tree, "src/runtime") + go_files(tree, "src/internal")
                 + go_files(tree, "src/arena")):
        with open(path) as f:
            lines = f.readlines()
        for i, line in enumerate(lines):
            m = re.search(r"//go:linkname(?:std)?\s+(\S+)\s+(\S+)", line)
            if not m:
                continue
            impl, target = m.group(1), m.group(2)
            if target.startswith("reflect."):
                edge = target
                rt_to_ref[edge] = (impl, signature_of(path, i + 1, None), path)
    # reflect -> runtime + pushes
    for path in go_files(tree, "src/reflect"):
        if os.path.basename(path) == "badlinkname.go":
            continue
        with open(path) as f:
            lines = f.readlines()
        for i, line in enumerate(lines):
            m = re.search(r"//go:linkname\s+(\S+)(?:\s+(\S+))?\s*$", line)
            if not m:
                continue
            local, target = m.group(1), m.group(2)
            if target is None:
                pushes.add("reflect." + local)
            elif target.startswith("runtime."):
                ref_to_rt[target] = (local, signature_of(path, i + 1, None), path)
    return rt_to_ref, ref_to_rt, pushes

def main():
    old, new = sys.argv[1], sys.argv[2]
    o_rt, o_fr, o_pu = extract(old)
    n_rt, n_fr, n_pu = extract(new)
    print(f"== baseline {old}: runtime->reflect={len(o_rt)} reflect->runtime={len(o_fr)} pushes={len(o_pu)}")
    print(f"== new      {new}: runtime->reflect={len(n_rt)} reflect->runtime={len(n_fr)} pushes={len(o_pu)}")
    skip, watch = [], []
    print("\n-- runtime->reflect --")
    for edge in sorted(set(o_rt) | set(n_rt)):
        if edge not in n_rt:
            print(f"REMOVED     {edge}  (was {o_rt[edge][0]} {o_rt[edge][1]})"); skip.append(edge)
        elif edge not in o_rt:
            print(f"ADDED       {edge}  (now {n_rt[edge][0]} {n_rt[edge][1]})"); watch.append(edge)
        elif o_rt[edge][1] != n_rt[edge][1]:
            print(f"SIG-CHANGED {edge}\n  old: {o_rt[edge][1]}\n  new: {n_rt[edge][1]}"); skip.append(edge)
    print("\n-- reflect->runtime --")
    for edge in sorted(set(o_fr) | set(n_fr)):
        if edge not in n_fr:
            print(f"REMOVED     {edge}"); skip.append(edge)
        elif edge not in o_fr:
            print(f"ADDED       {edge}"); watch.append(edge)
        elif o_fr[edge][1] != n_fr[edge][1]:
            print(f"SIG-CHANGED {edge}"); skip.append(edge)
    print("\n-- push markers (ecosystem-compat, not skip-list) --")
    for p in sorted(set(o_pu) | set(n_pu)):
        mark = " " if p in o_pu and p in n_pu else ("+" if p in n_pu else "-")
        print(f"{mark} {p}")
    print(f"\nSKIP-LIST size: {len(skip)}")
    print(f"WATCH-LIST size: {len(watch)}")
    print("KILL-TRIGGER check: skip-list growth > 15 twice in a row ->",
          "FIRED" if len(skip) > 15 else "not fired (needs two consecutive)")

if __name__ == "__main__":
    main()
