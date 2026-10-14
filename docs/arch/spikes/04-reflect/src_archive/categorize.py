#!/usr/bin/env python3
"""Spike 04-reflect: exhaustive categorization of every unsafe.* occurrence
in go1.27.1 src/reflect (non-test files).

Method: textual occurrences of `unsafe.<ident>` (grep -o), excluding lines
whose match is inside a // comment. Each occurrence is assigned one category:

  TD    type-descriptor reinterpret cast: (*abi.XType)(unsafe.Pointer(t))
  TD-R  module offset resolution: resolveNameOff/TypeOff/TextOff, addReflectOff
  PA    pointer arithmetic: add(), unsafe.Add, uintptr(addr)%align
  IF    iface/eface word construction or reading (incl. prototype extraction)
  RW    raw memory read/write: *(*T)(p), unsafe.Slice((*T)(p),n), copy()
  SH    slice/string header reinterpret: (*unsafeheader.Slice/String)(...)
  TP    type-position: signatures, struct fields, var decls, pure ptr conversions
  AL    layout query: unsafe.Sizeof/Alignof/Offsetof
  CF    call-frame machinery: regArgs slot addresses, frame word R/W
  FP    func code-pointer extraction / PC<->pointer conversion
  ZV    address of runtime zeroVal global
  AD    address-of a Go local/global (non-frame): unsafe.Pointer(&x)
  RC    race-detector call argument
  NONOP comment-only or string-literal matches (excluded from operational count)

Assignments were determined by reading the surrounding code of every line.
"""
import re, collections, sys

INV = "/home/hatch/workspace/graalgo/spikes/04-reflect/logs/inventory_raw.txt"

# (file, lineno) -> category. Built by hand-reading every matched line.
CAT = {
 # ---- type.go ----
 ("type.go",105):"NONOP", ("type.go",455):"RW", ("type.go",456):"TD-R",
 ("type.go",526):"NONOP", ("type.go",534):"TP", ("type.go",541):"TP",
 ("type.go",548):"TP", ("type.go",564):"TP", ("type.go",569):"TD-R",
 ("type.go",575):"TD-R", ("type.go",581):"TP", ("type.go",586):"TD-R",
 ("type.go",590):"TD-R", ("type.go",593):"TP", ("type.go",594):"TD-R", ("type.go",597):"TP",
 ("type.go",638):"TD", ("type.go",646):"TD", ("type.go",658):"TD",
 ("type.go",671):"AD", ("type.go",680):"TD", ("type.go",752):"TD",
 ("type.go",757):"TD", ("type.go",776):"TD", ("type.go",784):"TD",
 ("type.go",792):"TD", ("type.go",800):"TD", ("type.go",808):"TD",
 ("type.go",816):"TD", ("type.go",824):"TD", ("type.go",832):"TD",
 ("type.go",840):"TD", ("type.go",848):"TD", ("type.go",856):"TD",
 ("type.go",1011):"TP", ("type.go",1012):"PA", ("type.go",1187):"AD",
 ("type.go",1188):"AL", ("type.go",1189):"PA", ("type.go",1191):"RW",
 ("type.go",1318):"TD", ("type.go",1416):"TD", ("type.go",1425):"NONOP",
 ("type.go",1426):"IF", ("type.go",1427):"IF", ("type.go",1492):"TD",
 ("type.go",1510):"TD", ("type.go",1646):"TD", ("type.go",1647):"TD",
 ("type.go",1664):"TD", ("type.go",1665):"TD", ("type.go",1680):"TD",
 ("type.go",1681):"TD", ("type.go",1735):"TP", ("type.go",1859):"TD",
 ("type.go",1867):"IF", ("type.go",1868):"IF", ("type.go",1922):"IF",
 ("type.go",1930):"TD", ("type.go",1931):"RW", ("type.go",2015):"TD",
 ("type.go",2048):"TD", ("type.go",2051):"TD", ("type.go",2075):"TD",
 ("type.go",2078):"TD", ("type.go",2097):"TD", ("type.go",2100):"TD",
 ("type.go",2143):"TD", ("type.go",2151):"IF", ("type.go",2152):"IF",
 ("type.go",2309):"TD", ("type.go",2316):"FP", ("type.go",2325):"TD",
 ("type.go",2472):"AL", ("type.go",2490):"IF", ("type.go",2564):"AD",
 ("type.go",2572):"TP", ("type.go",2630):"TD", ("type.go",2672):"TD",
 ("type.go",2680):"IF", ("type.go",2681):"IF", ("type.go",2733):"AD",
 ("type.go",2744):"TP", ("type.go",2943):"TD", ("type.go",2950):"TD",
 # ---- value.go ----
 ("value.go",47):"TP", ("value.go",106):"TD", ("value.go",112):"TP",
 ("value.go",117):"RW", ("value.go",124):"IF", ("value.go",132):"TP",
 ("value.go",150):"RW", ("value.go",159):"IF", ("value.go",209):"TP",
 ("value.go",322):"TD", ("value.go",323):"RW", ("value.go",392):"TD",
 ("value.go",394):"TP", ("value.go",402):"RW", ("value.go",476):"TP",
 ("value.go",479):"TP", ("value.go",505):"CF", ("value.go",508):"CF",
 ("value.go",510):"CF", ("value.go",534):"CF", ("value.go",546):"CF",
 ("value.go",660):"CF", ("value.go",697):"TP", ("value.go",733):"CF",
 ("value.go",748):"CF", ("value.go",877):"TP", ("value.go",880):"TD",
 ("value.go",893):"FP", ("value.go",894):"TD", ("value.go",906):"AD",
 ("value.go",907):"TD", ("value.go",916):"TP", ("value.go",921):"CF",
 ("value.go",923):"CF", ("value.go",925):"CF", ("value.go",954):"TP",
 ("value.go",972):"TP", ("value.go",983):"CF", ("value.go",986):"CF",
 ("value.go",988):"CF", ("value.go",1034):"CF", ("value.go",1053):"CF",
 ("value.go",1145):"FP", ("value.go",1185):"TD", ("value.go",1247):"RW",
 ("value.go",1253):"TD", ("value.go",1268):"TD", ("value.go",1294):"ZV",
 ("value.go",1403):"TD", ("value.go",1426):"TD", ("value.go",1577):"IF",
 ("value.go",1582):"IF", ("value.go",1596):"RW", ("value.go",1652):"RW",
 ("value.go",1658):"RW", ("value.go",1690):"ZV", ("value.go",1693):"TD",
 ("value.go",1699):"ZV", ("value.go",1704):"RW", ("value.go",1721):"ZV",
 ("value.go",1724):"TD", ("value.go",1728):"ZV", ("value.go",1733):"RW",
 ("value.go",1758):"PA", ("value.go",1776):"RW", ("value.go",1843):"RW",
 ("value.go",1872):"TD", ("value.go",1892):"TP", ("value.go",1900):"RW",
 ("value.go",1968):"TD", ("value.go",2033):"NONOP", ("value.go",2087):"RW",
 ("value.go",2112):"TD", ("value.go",2118):"TP", ("value.go",2124):"AD",
 ("value.go",2145):"TD", ("value.go",2151):"TP", ("value.go",2155):"AD",
 ("value.go",2167):"TP", ("value.go",2173):"ZV", ("value.go",2179):"RW",
 ("value.go",2317):"NONOP", ("value.go",2320):"TP", ("value.go",2323):"RW",
 ("value.go",2341):"TP", ("value.go",2351):"TD", ("value.go",2353):"TD",
 ("value.go",2357):"TD", ("value.go",2371):"AD", ("value.go",2379):"TP",
 ("value.go",2382):"SH", ("value.go",2393):"AD", ("value.go",2403):"TP",
 ("value.go",2413):"TD", ("value.go",2415):"TD", ("value.go",2419):"TD",
 ("value.go",2431):"TP", ("value.go",2434):"SH", ("value.go",2445):"AD",
 ("value.go",2495):"TD", ("value.go",2527):"TD", ("value.go",2577):"NONOP",
 ("value.go",2596):"NONOP", ("value.go",2610):"TP", ("value.go",2620):"RW",
 ("value.go",2634):"FP", ("value.go",2640):"RW", ("value.go",2701):"NONOP",
 ("value.go",2714):"NONOP", ("value.go",2734):"TP", ("value.go",2777):"AD",
 ("value.go",2792):"TD", ("value.go",2889):"TP", ("value.go",2890):"TP",
 ("value.go",3015):"TD", ("value.go",3030):"AD", ("value.go",3046):"TD",
 ("value.go",3058):"TD", ("value.go",3065):"RW", ("value.go",3078):"TP",
 ("value.go",3081):"TP", ("value.go",3101):"AD", ("value.go",3107):"NONOP",
 ("value.go",3108):"TP", ("value.go",3111):"AD", ("value.go",3179):"TP",
 ("value.go",3181):"ZV", ("value.go",3223):"TP",
 ("value.go",3212):"TP",
 ("value.go",3738):"TP", ("value.go",3741):"TP", ("value.go",3744):"TP",
 ("value.go",3754):"TP", ("value.go",3757):"TP", ("value.go",3759):"TP",
 ("value.go",3764):"TP", ("value.go",3765):"TP", ("value.go",3768):"TP",
 ("value.go",3771):"TP", ("value.go",3774):"TP", ("value.go",3786):"TP",
 ("value.go",3793):"TP", ("value.go",3795):"TP", ("value.go",3796):"SH",
 ("value.go",3802):"TP", ("value.go",3805):"TP", ("value.go",3808):"TP",
 ("value.go",3810):"TP", ("value.go",3838):"TP", ("value.go",3840):"TP",
 ("value.go",3845):"TP", ("value.go",3850):"TP", ("value.go",3855):"TP",
 ("value.go",3861):"TP", ("value.go",3873):"TP", ("value.go",3876):"TP",
 ("value.go",3884):"TP", ("value.go",3904):"TP",
 # ---- map.go ----
 ("map.go",20):"TD", ("map.go",49):"TD", ("map.go",61):"IF",
 ("map.go",62):"IF", ("map.go",69):"TP", ("map.go",184):"TD",
 ("map.go",194):"TP", ("map.go",200):"TP", ("map.go",204):"AD",
 ("map.go",223):"RC", ("map.go",236):"RC", ("map.go",248):"TD",
 ("map.go",297):"TD", ("map.go",317):"TP", ("map.go",322):"TD",
 ("map.go",341):"TD", ("map.go",361):"TP", ("map.go",366):"TD",
 ("map.go",383):"TD", ("map.go",443):"TD", ("map.go",453):"TP",
 ("map.go",457):"AD", ("map.go",464):"TP", ("map.go",468):"AD",
 ("map.go",476):"TP", ("map.go",480):"AD",
 # ---- makefunc.go ----
 ("makefunc.go",54):"TD", ("makefunc.go",72):"TP", ("makefunc.go",109):"TD",
 ("makefunc.go",131):"TP", ("makefunc.go",172):"NONOP", ("makefunc.go",173):"CF",
 # ---- abi.go ----
 ("abi.go",224):"TD", ("abi.go",237):"TD", ("abi.go",473):"TP",
 ("abi.go",480):"TP", ("abi.go",487):"TP", ("abi.go",492):"RW",
 ("abi.go",501):"TP",
 # ---- deepequal.go ----
 ("deepequal.go",19):"TP", ("deepequal.go",20):"TP", ("deepequal.go",63):"TP",
 # ---- swapper.go ----
 ("swapper.go",42):"RW",
 # ---- badlinkname.go ----
 ("badlinkname.go",130):"TP",
 # ---- float32reg_generic.go ----
 ("float32reg_generic.go",18):"RW", ("float32reg_generic.go",22):"RW",
}

def main():
    # inventory_raw.txt covers type.go value.go map.go makefunc.go abi.go
    # deepequal.go swapper.go (286 lines). Add the 3 extra lines manually.
    extra = [
        ("badlinkname.go",130,"func badlinkname_Value_pointer(Value) unsafe.Pointer"),
        ("float32reg_generic.go",18,"\treturn *(*float32)(unsafe.Pointer(&i))"),
        ("float32reg_generic.go",22,"\treturn uint64(*(*uint32)(unsafe.Pointer(&val)))"),
    ]
    rows = []
    with open(INV) as f:
        for line in f:
            line=line.rstrip("\n")
            assert line.startswith("INV|"), line
            body=line[4:]
            file_, rest = body.split(":",1)
            lineno_s, code = rest.split(":",1)
            rows.append((file_, int(lineno_s), code))
    rows += extra
    # count occurrences per line (a line may hold >1 match)
    per_line = collections.Counter()
    for (file_, lineno, code) in rows:
        n = len(re.findall(r'unsafe\.[A-Za-z0-9_]*', code))
        # strip comment-only portion: if the only matches are after '//', drop them
        cpos = code.find("//")
        if cpos != -1:
            n_code = len(re.findall(r'unsafe\.[A-Za-z0-9_]*', code[:cpos]))
            n_com = n - n_code
        else:
            n_code, n_com = n, 0
        per_line[(file_,lineno)] = (n_code, n_com, code)
    missing = [k for k in per_line if k not in CAT]
    if missing:
        print("MISSING assignments:", missing); sys.exit(1)
    # also check CAT has no extras vs per_line
    extrak = [k for k in CAT if k not in per_line]
    if extrak:
        print("EXTRA assignments (no inventory line):", extrak); sys.exit(1)
    counts = collections.Counter()
    op_total = 0
    nonop = 0
    out_lines = []
    for (file_, lineno) in sorted(per_line):
        n_code, n_com, code = per_line[(file_,lineno)]
        cat = CAT[(file_,lineno)]
        if cat == "NONOP":
            nonop += n_code + n_com
            continue
        counts[cat]+= n_code
        op_total += n_code
        if n_com:
            nonop += n_com
        out_lines.append(f"{cat:5s} | {file_}:{lineno}: {code.strip()}")
    with open("/home/hatch/workspace/graalgo/spikes/04-reflect/logs/inventory_categorized.txt","w") as f:
        f.write("\n".join(out_lines)+"\n")
    print(f"operational occurrences: {op_total}")
    print(f"non-operational (comment/literal): {nonop}")
    print(f"total: {op_total+nonop}")
    for c,n in sorted(counts.items()):
        print(f"  {c:5s} {n:3d}")

if __name__ == "__main__":
    main()
