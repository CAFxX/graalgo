#!/usr/bin/env python3
"""R2-P1-3: build the final shape-coverage matrix from gen_matrix.go output.
Reads matrix.tsv + mrdesugar_m5.go; recomputes joins with fallbacks,
applies the helper-trap refinement, computes iface coverage by
(name, result-arity), and emits matrix_final.tsv + summary.
"""
import re, sys, csv
from collections import Counter, defaultdict

REPO = "/home/hatch/workspace/graalgo"
import os
W = os.environ.get("R2P13_STAGE", "/var/tmp/review-1111-14-r2p1-3")

# ---- parse tables from mrdesugar_m5.go ----
text = open(f"{REPO}/internal/frontend/mrdesugar_m5.go").read()
TABLES = ["m5StaticMREntries","m7MultiResultFuncs","m8MultiResultFuncs",
          "osMultiResultFuncs","m9MultiResultFuncs","ioMultiResultFuncs",
          "m8IfaceMultiResultMethods"]
# split file into table segments
idx = [(m.start(), m.group(1)) for m in re.finditer(
    r"^var (m5StaticMREntries|m7MultiResultFuncs|m8MultiResultFuncs|osMultiResultFuncs|m9MultiResultFuncs|ioMultiResultFuncs|m8IfaceMultiResultMethods) = \[\]", text, re.M)]
rows = []  # (table, pkg, name, recv, helper, pairfields)
for i,(start,name) in enumerate(idx):
    end = idx[i+1][0] if i+1 < len(idx) else len(text)
    seg = text[start:end]
    # row: {"a", "b", "c", "d", "e", []string{"x", "y"}, ...} — capture strings + []string list
    for m in re.finditer(r'\{\s*((?:"(?:[^"\\]|\\.)*"\s*,?\s*)+)(?:\[\]string\{([^}]*)\})?', seg):
        strs = re.findall(r'"((?:[^"\\]|\\.)*)"', m.group(1))
        fields = re.findall(r'"((?:[^"\\]|\\.)*)"', m.group(2) or "")
        if len(strs) < 2:
            continue
        pkg, nm = strs[0], strs[1]
        recv = strs[2] if len(strs) > 2 else ""
        helper = strs[3] if len(strs) > 3 else ""
        # iface rows have schema {pkg, name, pair, [fields]}: 3rd string is the pair name
        if name == "m8IfaceMultiResultMethods":
            helper, recv = "", ""
            pair = strs[2] if len(strs) > 2 else ""
        else:
            pair = strs[4] if len(strs) > 4 else ""
        rows.append((name, pkg, nm, recv, helper, pair, tuple(fields)))

by_key = defaultdict(list)
iface_by_name = defaultdict(list)
for r in rows:
    t, pkg, nm, recv, helper, pair, fields = r
    if t == "m8IfaceMultiResultMethods":
        iface_by_name[nm].append(r)
    else:
        by_key[(pkg, nm, recv)].append(r)
        if recv == "":
            pass
print(f"table rows parsed: {len(rows)}", file=sys.stderr)
for t in TABLES:
    print(f"  {t}: {sum(1 for r in rows if r[0]==t)}", file=sys.stderr)

# ---- read generator output ----
recs = []
with open(f"{W}/matrix.tsv") as f:
    rd = csv.DictReader(f, delimiter="\t")
    for row in rd:
        recs.append(row)
print(f"sites: {len(recs)}", file=sys.stderr)

# helper panic sites by funcName
helper_panics = {}  # funcName -> (file, line)
for r in recs:
    if r["funcName"].startswith("__"):
        helper_panics.setdefault(r["funcName"], []).append((r["file"], int(r["line"])))

def join(pkg, name, recv):
    """Return (table, helper) for a concrete-table match, else (None, None)."""
    recv = recv.lstrip("*")
    if "[" in recv:
        recv = recv[:recv.index("[")]
    for key in [(pkg, name, recv), (pkg, name, "")]:
        if key in by_key:
            r = by_key[key][0]
            return r[0], r[4]
    # dotted-pkg fallback: "reflect.Type" -> "reflect"
    if "." in pkg:
        short = pkg.split(".")[0]
        for key in [(short, name, recv), (short, name, "")]:
            if key in by_key:
                r = by_key[key][0]
                return r[0], r[4]
    return None, None

def iface_match(name, nres):
    for r in iface_by_name.get(name, []):
        if len(r[6]) == nres:
            return r
    return None

out = []
for r in recs:
    pkg, name, recv = r["msgPkg"], r["msgName"], r["recv"]
    nres = int(r["nResults"])
    table, helper = join(pkg, name, recv)
    im = iface_match(name, nres)
    # kind
    fn = r["funcName"]
    if fn.startswith("__"):
        kind = "__-helper"
    elif fn[:1].islower():
        kind = "unexported-helper"
    elif recv == "":
        kind = "pkg-func"
    else:
        kind = "method"
    unreachable_claim = "unreachable: desugared" in r["msg"]
    # direct
    helper_trap = ""
    if table:
        if helper and helper in helper_panics:
            # helper panics in the same file? check file match
            same = [x for x in helper_panics[helper] if x[0] == r["file"]]
            if same:
                helper_trap = f"{helper}@{r['file']}:{same[0][1]}"
        if helper_trap:
            direct = "PANICS-via-helper"
        else:
            direct = "COVERED"
    elif (im and recv.lstrip("*")[:1].islower() and recv != ""
          and unreachable_claim):
        # Only callable through an interface type (unexported concrete recv
        # + the body itself claims desugar-unreachability): there is no
        # concrete direct call form in user code.
        direct = "N/A-interface-only"
    else:
        direct = "PANICS"
    # iface dispatch: name+arity structural match. Same-package rows and
    # the canonical io/net interface-method families are COVERED; a
    # cross-package name collision (e.g. asn1.Unmarshal vs the elliptic
    # Unmarshal row) is PROBE-OWED: the classifier is structural, so only
    # the probe can confirm the pair types align.
    if im:
        row_pkg, row_name = im[1], im[2]
        pkg_base = pkg.split(".")[0]  # "reflect.Type" -> "reflect"
        canon = row_pkg in ("io", "net") and name in (
            "Read", "Write", "ReadByte", "WriteTo", "ReadFrom", "WriteString")
        if row_pkg == pkg_base or canon:
            iface = "COVERED"
        else:
            iface = "PROBE-OWED-cross-pkg"
        iface_via = f"{im[0]}:{row_pkg}.{row_name}"
    else:
        iface = "PROBE-OWED"
        iface_via = ""
    out.append({**r, "kind": kind, "table": table or "",
                "helper": helper or "", "helperTrap": helper_trap,
                "direct2": direct, "iface2": iface, "ifaceVia": iface_via,
                "unreachableClaim": "yes" if unreachable_claim else "no"})

with open(f"{W}/matrix_final.tsv", "w") as f:
    w = csv.writer(f, delimiter="\t", lineterminator="\n")
    w.writerow(["file","line","scope","pkg","method","recv","funcName","kind",
                "nResults","table","helper","helperTrap","direct","methodValue",
                "ifaceDispatch","ifaceVia","reflect","unreachableClaim","msg"])
    for r in out:
        w.writerow([r["file"], r["line"], r["scope"], r["msgPkg"], r["msgName"],
                    r["recv"], r["funcName"], r["kind"], r["nResults"],
                    r["table"], r["helper"], r["helperTrap"], r["direct2"],
                    "PANICS", r["iface2"], r["ifaceVia"], "PROBE-OWED",
                    r["unreachableClaim"], r["msg"]])

c = Counter(r["direct2"] for r in out)
print("direct:", dict(c))
print("iface:", dict(Counter(r["iface2"] for r in out)))
print("iface cross-pkg suspect:", sorted({f"{r['msgPkg']}.{r['msgName']} via {r['ifaceVia']}" for r in out if r["iface2"]=="PROBE-OWED-cross-pkg"}))
print("kind:", dict(Counter(r["kind"] for r in out)))
print("scope:", dict(Counter(r["scope"] for r in out)))
print("table:", dict(Counter(r["table"] for r in out if r["table"])))
print("helperTrap rows:", sum(1 for r in out if r["helperTrap"]))
for r in out:
    if r["helperTrap"]:
        print("  TRAP:", r["file"], r["line"], r["msgPkg"]+"."+r["msgName"], "->", r["helperTrap"])
print("unreachableClaim rows:", sum(1 for r in out if r["unreachableClaim"]=="yes"))
print("N/A-interface-only:", [f"{r['msgPkg']}.{r['msgName']}" for r in out if r["direct2"]=="N/A-interface-only"])
print("iface COVERED methods:", sorted({f"{r['msgPkg']}.{r['msgName']}" for r in out if r["iface2"]=="COVERED"}))
print("distinct methods:", len({(r["msgPkg"], r["msgName"], r["recv"]) for r in out}))
