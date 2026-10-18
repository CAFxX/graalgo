#!/usr/bin/env python3
# P3-M9 (tier1c): generate synth_mathbits.go from gc 1.27.1's math/bits.
#
# Transformations (all documented in the emitted file header):
#  1. bits_errors.go's //go:linkname VARs (runtime.overflowError /
#     runtime.divideError) become local errors.New sentinels with the
#     exact gc messages ("runtime error: integer overflow" /
#     "runtime error: integer divide by zero"); frontend/v1 has no
#     linkname-VAR support. The synthetic source therefore imports
#     "errors".
#  2. The 12 multi-result functions (Add/Add32/Add64, Sub/Sub32/Sub64,
#     Mul/Mul32/Mul64, Div/Div32/Div64) follow the Appendix M pattern:
#     exact gc signature + fail-fast body, real implementation in a
#     single-result __bits<Name> helper returning a bits<Name>Pair
#     struct (field names match the gc result names, in order).
#     Internal multi-result calls (Add->Add32/64, Sub->Sub32/64,
#     Mul->Mul32/64, Div->Div32/64, Rem64->Div64) are rewritten to the
#     __-helpers with field extraction.
#  3. bits_tables.go is carried verbatim. Everything else (all
#     single-result functions) is carried verbatim.
#
# Re-run: python3 gen_synth_mathbits.py > /dev/null (writes
# phases/phase1/frontend/synth_mathbits.go in place). The script lives
# next to the generated file; it is NOT shipped in corpus runs.
import os
import re, sys
import subprocess

# --- P1-R1-2 portability prologue: GOROOT and OUTDIR are environment-driven
# so the generator is CI-runnable and never writes outside the repo.
#   GRAALGO_GOROOT   Go installation whose gc 1.27.1 sources are read
#                    (default: `go env GOROOT`).
#   GEN_SYNTH_OUTDIR directory for the generated file(s) (default: this
#                    script's directory, i.e. in-place regeneration).
_HERE = os.path.dirname(os.path.abspath(__file__))

def _graalgo_goroot():
    g = os.environ.get("GRAALGO_GOROOT")
    if g:
        return g
    try:
        return subprocess.run(["go", "env", "GOROOT"], capture_output=True,
                              text=True, check=True).stdout.strip()
    except Exception as e:
        raise SystemExit(
            "gen_synth: cannot determine GOROOT; set GRAALGO_GOROOT (%s)" % e)

OUTDIR = os.environ.get("GEN_SYNTH_OUTDIR", _HERE)

GOROOT = os.path.join(_graalgo_goroot(), "src", "math", "bits")


with open(f"{GOROOT}/bits.go") as f:
    bits_go = f.read()
with open(f"{GOROOT}/bits_tables.go") as f:
    tables_go = f.read()

HEADER = '''// SYNTHETIC (Principle 4) — math/bits: real go1.27.1 source uses constructs the graalgo frontend cannot compile; ported with the documented adaptations below.
// RETIRES WHEN: frontend gains the underlying capability and the real GOROOT source compiles unchanged — then delete this file.
// RE-TEST TRIGGER: the capability named in this file's adaptation notes lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (tier1c): synthetic `math/bits` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (same mechanism as the P3-M7 synthetic math package,
// ADR-0007 D5). The resulting *types.Package is what
// overlayImporter.Import("math/bits") returns; the exporter emits the
// synthetic declarations' bodies into the wire document via
// exportSyntheticPackages.
//
// Value model:
//   - Pure-Go functions are gc 1.27.1's implementations ported verbatim
//     (bit-exact by construction). math/bits has no assembly on any
//     platform, so no transliteration was needed.
//   - Multi-result functions (Add, Sub, Mul, Div and the 32/64 variants)
//     keep exact gc signatures with fail-fast bodies; the real
//     implementations live in single-result __bits<Name> helpers
//     returning bits<Name>Pair structs (Appendix M pattern). 2-target
//     call sites desugar via the m7MultiResultFuncs table entries added
//     in mrdesugar_m5.go. Inside this source, multi-result calls are
//     only ever made to __-helpers, never to the public multi-result
//     names (a synthetic-internal multi-result call would fail export).
//   - DEVIATION (documented): bits_errors.go's //go:linkname VARs
//     (runtime.overflowError / runtime.divideError) have no frontend/v1
//     support (linkname is func-only, P3-M4). They are reconstructed as
//     errors.New with the exact gc messages, verified against gc
//     1.27.1: panic(divideError) prints
//     "panic: runtime error: integer divide by zero" and
//     panic(overflowError) prints "panic: runtime error: integer
//     overflow". The synthetic source therefore imports "errors".
//   - DEVIATION (documented): ReverseBytes32 is rewritten with explicit
//     byte operations via locals instead of gc's mask-based form. The
//     frontend mis-evaluates gc's form when the function is called more
//     than once in a program (2026-10-09: single call passes, two calls
//     fail; ReverseBytes64 is unaffected). Frontend bug filed; retest
//     trigger: frontend handles the mask form correctly.
//
// Out-of-scope members: none — the full math/bits API is covered.
//
// Generated by gen_synth_mathbits.py from gc 1.27.1; DO NOT EDIT BY
// HAND (edit the generator and re-run).
package main

// syntheticMathBitsSource is the P3-M9 synthetic `math/bits` package.
const syntheticMathBitsSource = `package bits

// ---- Injected: error sentinels (see header deviation note) ----
// P3-M9: panic with the message STRING, not an error value. The
// frontend prints non-string panic values as "panic: (*errors.errorString)
// 0xADDR" (it does not call Error()); a string panic prints the message
// verbatim, matching gc's stderr byte-for-byte.
var overflowError = "runtime error: integer overflow"
var divideError = "runtime error: integer divide by zero"

'''

# (name, pair struct fields in result order, helper body)
# Bodies are gc 1.27.1's, restructured to single-result. Internal
# multi-result calls rewritten to __-helpers.
MULTI = [
("Add", "uint", "x, y, carry uint", "sum, carryOut uint",
"""	if UintSize == 32 {
		p := __bitsAdd32(uint32(x), uint32(y), uint32(carry))
		return bitsAddPair{sum: uint(p.sum), carryOut: uint(p.carryOut)}
	}
	p := __bitsAdd64(uint64(x), uint64(y), uint64(carry))
	return bitsAddPair{sum: uint(p.sum), carryOut: uint(p.carryOut)}"""),
("Add32", "uint32", "x, y, carry uint32", "sum, carryOut uint32",
"""	sum64 := uint64(x) + uint64(y) + uint64(carry)
	return bitsAdd32Pair{sum: uint32(sum64), carryOut: uint32(sum64 >> 32)}"""),
("Add64", "uint64", "x, y, carry uint64", "sum, carryOut uint64",
"""	sum := x + y + carry
	// The sum will overflow if both top bits are set (x & y) or if one of them
	// is (x | y), and a carry from the lower place happened. If such a carry
	// happens, the top bit will be 1 + 0 + 1 = 0 (&^ sum).
	carryOut := ((x & y) | ((x | y) &^ sum)) >> 63
	return bitsAdd64Pair{sum: sum, carryOut: carryOut}"""),
("Sub", "uint", "x, y, borrow uint", "diff, borrowOut uint",
"""	if UintSize == 32 {
		p := __bitsSub32(uint32(x), uint32(y), uint32(borrow))
		return bitsSubPair{diff: uint(p.diff), borrowOut: uint(p.borrowOut)}
	}
	p := __bitsSub64(uint64(x), uint64(y), uint64(borrow))
	return bitsSubPair{diff: uint(p.diff), borrowOut: uint(p.borrowOut)}"""),
("Sub32", "uint32", "x, y, borrow uint32", "diff, borrowOut uint32",
"""	diff := x - y - borrow
	// The difference will underflow if the top bit of x is not set and the top
	// bit of y is set (^x & y) or if they are the same (^(x ^ y)) and a borrow
	// from the lower place happens. If that borrow happens, the result will be
	// 1 - 1 - 1 = 0 - 0 - 1 = 1 (& diff).
	borrowOut := ((^x & y) | (^(x ^ y) & diff)) >> 31
	return bitsSub32Pair{diff: diff, borrowOut: borrowOut}"""),
("Sub64", "uint64", "x, y, borrow uint64", "diff, borrowOut uint64",
"""	diff := x - y - borrow
	// See Sub32 for the bit logic.
	borrowOut := ((^x & y) | (^(x ^ y) & diff)) >> 63
	return bitsSub64Pair{diff: diff, borrowOut: borrowOut}"""),
("Mul", "uint", "x, y uint", "hi, lo uint",
"""	if UintSize == 32 {
		p := __bitsMul32(uint32(x), uint32(y))
		return bitsMulPair{hi: uint(p.hi), lo: uint(p.lo)}
	}
	p := __bitsMul64(uint64(x), uint64(y))
	return bitsMulPair{hi: uint(p.hi), lo: uint(p.lo)}"""),
("Mul32", "uint32", "x, y uint32", "hi, lo uint32",
"""	tmp := uint64(x) * uint64(y)
	return bitsMul32Pair{hi: uint32(tmp >> 32), lo: uint32(tmp)}"""),
("Mul64", "uint64", "x, y uint64", "hi, lo uint64",
"""	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + w0>>32
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	return bitsMul64Pair{hi: x1*y1 + w2 + w1>>32, lo: x * y}"""),
("Div", "uint", "hi, lo, y uint", "quo, rem uint",
"""	if UintSize == 32 {
		p := __bitsDiv32(uint32(hi), uint32(lo), uint32(y))
		return bitsDivPair{quo: uint(p.quo), rem: uint(p.rem)}
	}
	p := __bitsDiv64(uint64(hi), uint64(lo), uint64(y))
	return bitsDivPair{quo: uint(p.quo), rem: uint(p.rem)}"""),
("Div32", "uint32", "hi, lo, y uint32", "quo, rem uint32",
"""	if y != 0 && y <= hi {
		panic(overflowError)
	}
	z := uint64(hi)<<32 | uint64(lo)
	return bitsDiv32Pair{quo: uint32(z / uint64(y)), rem: uint32(z % uint64(y))}"""),
("Div64", "uint64", "hi, lo, y uint64", "quo, rem uint64",
"""	if y == 0 {
		panic(divideError)
	}
	if y <= hi {
		panic(overflowError)
	}

	// If high part is zero, we can directly return the results.
	if hi == 0 {
		return bitsDiv64Pair{quo: lo / y, rem: lo % y}
	}

	s := uint(LeadingZeros64(y))
	y <<= s

	const (
		two32  = 1 << 32
		mask32 = two32 - 1
	)
	yn1 := y >> 32
	yn0 := y & mask32
	un32 := hi<<s | lo>>(64-s)
	un10 := lo << s
	un1 := un10 >> 32
	un0 := un10 & mask32
	q1 := un32 / yn1
	rhat := un32 - q1*yn1

	for q1 >= two32 || q1*yn0 > two32*rhat+un1 {
		q1--
		rhat += yn1
		if rhat >= two32 {
			break
		}
	}

	un21 := un32*two32 + un1 - q1*y
	q0 := un21 / yn1
	rhat = un21 - q0*yn1

	for q0 >= two32 || q0*yn0 > two32*rhat+un0 {
		q0--
		rhat += yn1
		if rhat >= two32 {
			break
		}
	}

	return bitsDiv64Pair{quo: q1*two32 + q0, rem: (un21*two32 + un0 - q0*y) >> s}"""),
]

# Result field names per function (must match gc result names, in order).
FIELDS = {
    "Add": ("sum", "carryOut"), "Add32": ("sum", "carryOut"), "Add64": ("sum", "carryOut"),
    "Sub": ("diff", "borrowOut"), "Sub32": ("diff", "borrowOut"), "Sub64": ("diff", "borrowOut"),
    "Mul": ("hi", "lo"), "Mul32": ("hi", "lo"), "Mul64": ("hi", "lo"),
    "Div": ("quo", "rem"), "Div32": ("quo", "rem"), "Div64": ("quo", "rem"),
}

def strip_multiresult_funcs(src):
    """Remove the 12 multi-result function declarations (doc comment +
    signature + body) from bits.go; they are re-emitted in Appendix M
    form by emit_multiresult()."""
    names = "|".join(n for n, _, _, _, _ in MULTI)
    # Match: doc comment lines (//...) immediately followed by
    # "func <Name>(" ... through the closing brace at column 0.
    pat = re.compile(
        r"(?:^//.*\n)+^func (" + names + r")\(.*?\n\}(?:\n|$)",
        re.M,
    )
    # The above is greedy-sensitive; do it per-name for safety.
    for name, _, _, _, _ in MULTI:
        pat = re.compile(r"(?:^//[^\n]*\n)+^func " + name + r"\(.*?^}\n", re.M | re.S)
        src, n = pat.subn("", src, count=1)
        assert n == 1, f"failed to strip {name}"
    return src

def emit_multiresult():
    out = []
    out.append("// ---- Appendix M: multi-result functions (fail-fast public +\n")
    out.append("// ---- single-result __-helpers; see header) ----\n\n")
    for name, wt, params, results, body in MULTI:
        f1, f2 = FIELDS[name]
        pair = f"bits{name}Pair"
        helper = f"__bits{name}"
        out.append(f"// {pair} is the Appendix M single-result carrier for {name}.\n")
        out.append(f"type {pair} struct {{\n\t{f1} {wt}\n\t{f2} {wt}\n}}\n\n")
        out.append(f"// {helper} is the real {name} implementation (gc 1.27.1's\n")
        out.append(f"// {name} body, restructured to single-result).\n")
        out.append(f"func {helper}({params}) {pair} {{\n{body}\n}}\n\n")
        out.append(f"// {name} keeps the exact gc 1.27.1 multi-result signature\n")
        out.append(f"// with a fail-fast body; 2-target call sites desugar to\n")
        out.append(f"// {helper} via the m7MultiResultFuncs table.\n")
        out.append(f"func {name}({params}) ({results}) {{\n")
        out.append(f'\tpanic("math/bits: {name}: multi-result call needs the Appendix M desugar (re-test: WS D multi-result call support)")\n}}\n\n')
    return "".join(out)

def main():
    body = strip_multiresult_funcs(bits_go)
    # Drop the package clause and the generate directive from bits.go's
    # head; the const block stays. bits.go head is:
    #   // Copyright...
    #   //go:generate ...
    #   // Package bits implements...
    #   package bits
    #   const uintSize = ...
    m = re.search(r"^package bits\n", body, re.M)
    assert m
    body = body[m.end():]
    # The doc comment above "package bits" described the package; keep a
    # one-line attribution instead (the full doc is in the header).
    tables = tables_go
    m2 = re.search(r"^package bits\n", tables, re.M)
    assert m2
    tables = tables[m2.end():]

    # Rem64 is single-result but calls the multi-result Div64 internally;
    # rewrite to the __-helper (synthetic-internal multi-result calls
    # fail export).
    body = body.replace(
        "\t_, rem := Div64(hi%y, lo, y)\n\treturn rem\n",
        "\t// P3-M9: Div64 -> __bitsDiv64 (see header: no multi-result\n\t// calls inside synthetic sources).\n\treturn __bitsDiv64(hi%y, lo, y).rem\n",
    )
    assert "__bitsDiv64(hi%y, lo, y).rem" in body

    # P3-M9: ReverseBytes32 misbehaves in the frontend when called more
    # than once in a program (2026-10-09: single call passes, two calls
    # fail; ReverseBytes64 is unaffected). Rewrite with fully explicit
    # byte operations via locals (no shared constants, no compound
    # expression).
    body = body.replace(
        "func ReverseBytes32(x uint32) uint32 {\n\tconst m = 1<<32 - 1\n\tx = x>>8&(m3&m) | x&(m3&m)<<8\n\treturn x>>16 | x<<16\n}",
        "func ReverseBytes32(x uint32) uint32 {\n"
        "\t// P3-M9: explicit bytes via locals (see header deviation).\n"
        "\tb0 := x >> 24\n"
        "\tb1 := x >> 8 & 0xFF00\n"
        "\tb2 := x << 8 & 0xFF0000\n"
        "\tb3 := x << 24\n"
        "\treturn b0 | b1 | b2 | b3\n}",
    )
    assert "b0 := x >> 24" in body

    src = HEADER + body.rstrip() + "\n\n" + emit_multiresult() + tables.rstrip() + "\n`\n"
    with open(os.path.join(OUTDIR, "synth_mathbits.go"), "w") as f:
        f.write(src)
    print("wrote synth_mathbits.go", len(src), "bytes")

main()
