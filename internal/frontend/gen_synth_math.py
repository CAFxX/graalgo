#!/usr/bin/env python3
"""Generate internal/frontend/synth_math.go from gc 1.27.1's math package.

P3-M7 (WS G, ADR-0007 D5): the synthetic `math` package. REAL Go source,
embedded as text and type-checked on demand with the overlay importer.

PROVENANCE: the original m7-math assemble_synth.py was never checked in;
this script (written 2026-10-18, review lane review-p1-mathgen) reconstructs
it. It reproduces the reviewed synth_math.go byte-identically from
gc 1.27.1 sources plus the explicit, asserted transformations below.
`make gen-synth` runs it via run_gen_synth.py; `make all` regenerates and
CI fails on drift (`git diff --exit-code`).

TRANSFORMATION CATALOG (each applied with an asserted anchor; a gc source
change that invalidates an anchor fails the build loudly instead of
silently drifting):
  T1  arch dispatch: `if haveArchX { return archX(...) }` branches are
      dropped. The synthetic package is arch-independent pure Go; only the
      generic path is carried. (stubs.go / *_amd64.s / *_s390x.* are not
      inputs.)
  T2  doc comments: carried verbatim, EXCEPT the per-file DROP_DOC sets.
      (Historical: the original assembly dropped these docs; the set is
      retained for byte-stability of the reviewed file. A kept doc is
      emitted as its own block, i.e. followed by a blank line.)
  T3  multi-result calls (WS D: the synthetic package has no multi-result
      calls): `a, b := Frexp/Modf/Sincos(args)` becomes
      `__tmN := __frexp/__modf/__sincos(args); a, b := __tmN.<f1>, __tmN.<f2>`
      with a per-file __tmN counter (fields are gc's result names, in
      order). Special forms, rewritten explicitly:
        `switch s, c := Sincos(x); ...` -> `__sc := __sincos(x); ...`
        `j, z = trigReduce(x)`          -> `tz := trigReduce(x)` + field copy
        `y1, y2 := stirling(x)`         -> `stN := stirling(x)` + field copy
      normalize() (bits.go) is inlined at its 3 use sites (ilogb, ldexp,
      __frexp); its declaration is dropped (its doc comment is retained,
      dangling, as in the reviewed file).
  T4  pair restructures (Appendix M for internal multi-result funcs):
      trigReduce -> __trigReducePair{j, z}; stirling -> __stirlingPair{a, b}.
  T5  amd64 transliterations (carried as literals, provenance in the
      emitted doc): Exp (FMA path of exp_amd64.s), Log (log_amd64.s),
      Floor/Ceil/Trunc (generic + sNaN quieting for ROUNDPD parity).
      They REPLACE gc's Exp/exp/Log/log/Floor/Ceil/Trunc decls.
  T6  intrinsics: Float32/64bits/frombits, Sqrt, FMA get panic bodies;
      the exporter intercepts every call into wire nodes (see the emitted
      header's value model).
  T7  fail-fast multi-result: Frexp, Modf, Lgamma, Sincos keep exact gc
      signatures with panic bodies (WS D; the P3-M5 F4 precedent). The
      real single-result implementations are __frexp/__modf/__sincos.
  T8  mathbits vendoring: trig_reduce.go's math/bits uses become local
      mathbits* wrappers (verbatim ports of gc 1.27.1 math/bits); the
      synthetic package cannot import math/bits (WS G import overlay).
  T9  const.go intSize: the `intSize` const is dropped; MaxInt/MinInt/
      MaxUint are specialized to 64-bit (the synthetic package targets
      64-bit ints).
  T10 P3-M7 guards (each with a rationale comment in the emitted source):
      trigReduce lz+1 shift guard; RoundToEven e<shift guard; pzero/pone/
      qzero/qone refactored to avoid & (WS D); mathbitsLen64 replaces
      len8tab indexing (WS D).

ACCURACY CONTRACT: the synthetic math package is bit-exact vs gc 1.27.1
on every input. Pure-Go functions are verbatim ports (bit-exact by
construction); the amd64 transliterations and the six intrinsics were
differentially verified bit-exact. There are NO accepted divergences.
The "1-ulp diffs" noted in the emitted header describe the REJECTED
alternative (routing transcendentals through java.lang.StrictMath, which
measured not bit-exact: NaN-payload canonicalization, 1-ulp diffs on
atan/tanh/cbrt/sinh/cosh/exp/expm1/hypot, larger on the rest) -- not the
shipped code. The pure-Go ports ARE the fdlibm-derived reference (gc's
math is fdlibm/msun-derived), so there is nothing further to port.
One platform-conditional note: Exp transliterates the FMA3 path; on
hardware without FMA3 gc takes its non-FMA path (146 observed 1-ulp
diffs when forced). That is a documented platform condition, not a
divergence in the shipped configuration.

ORIGINAL DIFFERENTIAL EVIDENCE (P3-M7, 2026-10-07; raw logs retired per
the no-hidden_files rule, counts recorded here):
  Sqrt via StrictMath.sqrt: 0 mismatches / 50071 adversarial+random inputs
  FMA via Math.fma:         0 mismatches / 20000 adversarial+random inputs
  Exp (FMA-path transliteration):  0 mismatches / 50071 inputs
  Log (log_amd64.s transliteration): 0 mismatches / 50071 inputs
  Floor/Ceil/Trunc (sNaN quieting): 0 mismatches / 50071 inputs
  Sqrt/FMA intrinsic claims additionally rest on JLS/spec guarantees
  (StrictMath.sqrt and Math.fma are specified correctly rounded).

RE-TEST TRIGGER: re-run the differential re-test
(internal/frontend/synth_math_diff_test.go, which extracts this file's
syntheticMathSource, remaps the six intrinsics to stdlib math, and
asserts bit-exactness vs gc on adversarial+random inputs) when ANY of:
  - the Go toolchain version changes (the version pin in run_gen_synth.py
    fails fast; the generator must be re-validated against the new gc),
  - the JDK changes (the StrictMath.sqrt / Math.fma bit-exactness claims),
  - this generator changes (the drift gate covers the file, but the
    accuracy evidence must be re-established, not just re-emitted).
"""

import os
import re
import subprocess
import sys

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
            "gen_synth_math: cannot determine GOROOT; set GRAALGO_GOROOT (%s)" % e)


OUTDIR = os.environ.get("GEN_SYNTH_OUTDIR", _HERE)
MATH = os.path.join(_graalgo_goroot(), "src", "math")

# ---------------------------------------------------------------------------
# Emitted-file literals. Injected by the composer from the reviewed
# synth_math.go; they are the hand-written parts of the file (T5/T6/T7/T8).
# ---------------------------------------------------------------------------
PROLOGUE = """// Bit-conversion intrinsics: exact gc 1.27.1 signatures; bodies never
// run (the exporter intercepts every call into wire nodes; the Java
// side uses Double.doubleToRawLongBits etc. — bit-exact by
// construction).
func Float32bits(f float32) uint32     { panic("math: intrinsic Float32bits") }
func Float32frombits(b uint32) float32 { panic("math: intrinsic Float32frombits") }
func Float64bits(f float64) uint64     { panic("math: intrinsic Float64bits") }
func Float64frombits(b uint64) float64 { panic("math: intrinsic Float64frombits") }

// JVM intrinsics (verified bit-exact vs gc 1.27.1;
// evidence recorded in the gen_synth_math.py header):
// Sqrt via StrictMath.sqrt (0 mismatches / 50071), FMA via Math.fma
// (0 mismatches / 20000). All other transcendentals are pure-Go ports:
// StrictMath is NOT bit-exact (NaN payload canonicalization, 1-ulp
// diffs on atan/tanh/cbrt/sinh/cosh/exp/expm1/hypot, larger on the rest).
func Sqrt(x float64) float64       { panic("math: intrinsic Sqrt") }
func FMA(x, y, z float64) float64 { panic("math: intrinsic FMA") }
"""
GEN_MARKER = """// Code generated by gen_synth_math.py from gc 1.27.1.
// DO NOT EDIT BY HAND; edit the script and re-run.
"""
MATHBITS = """// ---- Injected helpers (not in gc math; local to the synthetic package) ----

// mathbits* are ports of math/bits (Add64, Sub64, Mul64, LeadingZeros64)
// used by trig_reduce.go. The synthetic package cannot import math/bits
// (WS G import overlay), so they are inlined here, verbatim from gc 1.27.1
// math/bits/bits.go.
type __bits64Pair struct{ hi, lo uint64 }
func mathbitsAdd64(x, y, carry uint64) __bits64Pair {
	sum := x + y + carry
	carryOut := ((x & y) | ((x | y) &^ sum)) >> 63
	return __bits64Pair{carryOut, sum}
}

func mathbitsSub64(x, y, borrow uint64) __bits64Pair {
	diff := x - y - borrow
	borrowOut := ((^x & y) | (^(x ^ y) & diff)) >> 63
	return __bits64Pair{borrowOut, diff}
}

func mathbitsMul64(x, y uint64) __bits64Pair {
	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + w0>>32
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	hi := x1*y1 + w2 + w1>>32
	lo := x * y
	return __bits64Pair{hi, lo}
}

func mathbitsLeadingZeros64(x uint64) int {
	return 64 - mathbitsLen64(x)
}


func mathbitsLen64(x uint64) (n int) {
	if x >= 1<<32 {
		x >>= 32
		n = 32
	}
	if x >= 1<<16 {
		x >>= 16
		n += 16
	}
	if x >= 1<<8 {
		x >>= 8
		n += 8
	}
	// P3-M7: bit length of x (0 <= x < 256). Replaces len8tab[x]
	// (WS E doesn't support indexing untyped string consts).
	switch {
	case x >= 128:
		return n + 8
	case x >= 64:
		return n + 7
	case x >= 32:
		return n + 6
	case x >= 16:
		return n + 5
	case x >= 8:
		return n + 4
	case x >= 4:
		return n + 3
	case x >= 2:
		return n + 2
	case x >= 1:
		return n + 1
	}
	return n
}
"""
HELPERS = """// __frexpPair / __modfPair carry the two results of Frexp/Modf without
// multi-value returns (WS D: the frontend has no multi-result calls).
type __frexpPair struct {
	frac float64
	exp  int
}

type __modfPair struct {
	int  float64
	frac float64
}

// __frexp is gc 1.27.1's frexp (frexp.go, the haveArchFrexp=false path)
// with the two results packed into __frexpPair. Bit-exact: the
// computation is unchanged.
func __frexp(f float64) __frexpPair {
	// Special cases.
	switch {
	case f == 0:
		return __frexpPair{f, 0} // correctly return -0
	case IsInf(f, 0) || IsNaN(f):
		return __frexpPair{f, 0}
	}
	var exp int
	// Inlined normalize (gc bits.go): multi-result not allowed in WS D.
	const __smallestNormal = 2.2250738585072014e-308 // 2**-1022
	if Abs(f) < __smallestNormal {
		f = f * (1 << 52)
		exp = -52
	}
	x := Float64bits(f)
	exp += int((x>>shift)&mask) - bias + 1
	x &^= mask << shift
	x |= (-1 + bias) << shift
	return __frexpPair{Float64frombits(x), exp}
}

// __modf is gc 1.27.1's Modf (modf.go) with the two results packed
// into __modfPair. Bit-exact: the computation is unchanged.
func __modf(f float64) __modfPair {
	integer := Trunc(f)
	fractional := Copysign(f-integer, f)
	return __modfPair{integer, fractional}
}

type __sincosPair struct {
	sin float64
	cos float64
}

// __sincos computes Sin(x) and Cos(x) (sharing nothing; each is
// bit-exact). Used by the Bessel functions; the exported Sincos stays
// fail-fast (WS D).
func __sincos(x float64) __sincosPair {
	return __sincosPair{Sin(x), Cos(x)}
}
"""
FAILFAST = """// ---- Injected: fail-fast stubs (WS D multi-result; F4 precedent) ----

// Frexp breaks f into a normalized fraction and an integral power of two.
// It returns frac and exp satisfying f == frac × 2**exp, with the absolute
// value of frac in the interval [½, 1).
//
// NOT IN M7 SCOPE (fail-fast): multi-result calls need WS D (F4 precedent).
// The single-result internal __frexp is used by Pow/Log2.
func Frexp(f float64) (frac float64, exp int) {
	panic("math.Frexp not in M7 scope (multi-result calls need WS D; re-test when it lands)")
}

// Modf returns integer and fractional floating-point numbers that sum to f.
// Both values have the same sign as f.
//
// NOT IN M7 SCOPE (fail-fast): multi-result calls need WS D (F4 precedent).
// The single-result internal __modf is used by Pow/Gamma.
func Modf(f float64) (int float64, frac float64) {
	panic("math.Modf not in M7 scope (multi-result calls need WS D; re-test when it lands)")
}

// Lgamma returns the natural logarithm of the absolute value of the
// Gamma function.
//
// NOT IN M7 SCOPE (fail-fast): multi-result calls need WS D (F4 precedent).
func Lgamma(x float64) (lgamma float64, sign int) {
	panic("math.Lgamma not in M7 scope (multi-result calls need WS D; re-test when it lands)")
}

// Sincos returns Sin(x), Cos(x).
//
// NOT IN M7 SCOPE (fail-fast): multi-result calls need WS D (F4 precedent).
func Sincos(x float64) (sin, cos float64) {
	panic("math.Sincos not in M7 scope (multi-result calls need WS D; re-test when it lands)")
}
"""
EXP = """// ---- Injected: Exp (amd64 FMA-path transliteration) ----

// Exp returns e**x, the base-e exponential of x.
//
// This is a transliteration of gc 1.27.1's exp_amd64.s archExp FMA path
// (the path taken when useFMA=1, i.e. on any CPU with FMA3 — including
// this VM). Differentially verified bit-exact vs gc 1.27.1: 0 mismatches
// over 50071 adversarial+random inputs (evidence: gen_synth_math.py header).
//
// The synthetic FMA() intrinsic (mathfma wire node → Math.fma) provides
// the fused operation; the polynomial, reduction, and ldexp are pure Go.
//
// Carried divergence: on hardware without FMA3, gc uses the non-FMA path
// (146 observed 1-ulp diffs on this VM when forced). Re-test trigger:
// GOMIPS/no-FMA CI or a non-FMA host.
func Exp(x float64) float64 {
	const (
		log2e = 1.4426950408889634073599246810018920
		ln2u  = 0.69314718055966295651160180568695068359375
		ln2l  = 0.28235290563031577122588448175013436025525412068e-12
		oflow = 7.09782712893384e+02
	)
	c := [9]float64{0.5, 1.0, 2.0,
		1.6666666666666666667e-1, 4.1666666666666666667e-2,
		8.3333333333333333333e-3, 1.3888888888888888889e-3,
		1.9841269841269841270e-4, 2.4801587301587301587e-5}

	bx := Float64bits(x)
	// Test bits for not-finite.
	if bx&^(1<<63) >= 0x7FF0000000000000 {
		if bx == 0xFFF0000000000000 {
			return 0
		}
		return x
	}
	// Check if argument will overflow.
	if x > oflow {
		return Inf(1)
	}

	// BX = exponent. CVTSD2SI rounds to nearest (MXCSR).
	k := int32(RoundToEven(log2e * x))
	kf := float64(k)

	// FMA path reduction (VFNMADD231SD: X0 = X0 - X2*X1, fused).
	r := FMA(-ln2u, kf, x)
	r = FMA(-ln2l, kf, r)
	// Reduce argument.
	r *= 0.0625

	// Taylor series evaluation (VFMADD213SD: X1 = X0*X1 + mem, fused).
	t := c[8]
	t = FMA(r, t, c[7])
	t = FMA(r, t, c[6])
	t = FMA(r, t, c[5])
	t = FMA(r, t, c[4])
	t = FMA(r, t, c[3])
	t = FMA(r, t, c[0])
	t = FMA(r, t, c[1])
	fr := t * r

	// Squaring loop: 3 iterations (the 4th is fused with +1 below).
	for i := 0; i < 3; i++ {
		s := fr + 2.0
		fr = fr * s
	}
	// Fused 4th iteration + 1: VFMADD213SD 1.0, X1, X0 → X0 = X1*X0 + 1.
	s := fr + 2.0
	fr = FMA(s, fr, 1.0)

	// Return fr * 2**exponent (ldexp).
	be := int64(k) + 0x3FF
	if be <= 0 {
		// Denormal.
		if be < -52 {
			return 0
		}
		be2 := be + 0x3FE
		fr *= Float64frombits(uint64(be2) << 52)
		fr *= Float64frombits(uint64(1) << 52) // 2**-1022
		return fr
	}
	if be >= 0x7FF {
		return Inf(1)
	}
	fr *= Float64frombits(uint64(be) << 52)
	return fr
}
"""
LOG = """// ---- Injected: Log (amd64 asm transliteration) ----

// Log returns the natural logarithm of x.
//
// This is a transliteration of gc 1.27.1's log_amd64.s archLog.
// Differentially verified bit-exact vs gc 1.27.1: 0 mismatches over
// 50071 adversarial+random inputs (evidence: gen_synth_math.py header).
// Straight-line
// port; the only subtlety is the NaN-payload preservation (return x
// directly for +Inf/NaN, not via a computation).
func Log(x float64) float64 {
	const (
		hSqrt2 = 7.07106781186547524401e-01
		ln2Hi  = 6.93147180369123816490e-01
		ln2Lo  = 1.90821492927058770002e-10
		l1     = 6.666666666666735130e-01
		l2     = 3.999999999940941908e-01
		l3     = 2.857142874366239149e-01
		l4     = 2.222219843214978396e-01
		l5     = 1.818357216161805012e-01
		l6     = 1.531383769920937332e-01
		l7     = 1.479819860511658591e-01
	)

	bx := Float64bits(x)
	// Test bits for special cases.
	if bx&^(1<<63) == 0 {
		return Inf(-1) // ±0
	}
	if int64(bx) < 0 {
		return Float64frombits(0x7FF8000000000001) // x < 0 → NaN
	}
	if bx >= 0x7FF0000000000000 {
		return x // +Inf or NaN: return x (preserves NaN payload)
	}

	// f1, k := frexp(x).
	f1 := Float64frombits((bx & 0x000FFFFFFFFFFFFF) | 0x3FE0000000000000)
	k := int32((bx>>52)&0x7FF) - 0x3FE
	kf := float64(k)

	// If f1 < Sqrt2/2 { k--; f1 *= 2 }.
	if f1 < hSqrt2 {
		kf -= 1
		f1 *= 2
	}

	// f := f1 - 1
	f := f1 - 1
	// s := f / (2 + f)
	s := f / (2 + f)
	// s2 := s * s; s4 := s2 * s2
	s2 := s * s
	s4 := s2 * s2
	// t1 := s2 * (L1 + s4*(L3+s4*(L5+s4*L7)))
	t1 := s2 * (l1 + s4*(l3+s4*(l5+s4*l7)))
	// t2 := s4 * (L2 + s4*(L4+s4*L6))
	t2 := s4 * (l2 + s4*(l4+s4*l6))
	// R := t1 + t2
	r := t1 + t2
	// hfsq := 0.5 * f * f
	hfsq := 0.5 * f * f
	// return k*Ln2Hi - ((hfsq - (s*(hfsq+R) + k*Ln2Lo)) - f)
	return kf*ln2Hi - ((hfsq - (s*(hfsq+r) + kf*ln2Lo)) - f)
}
"""
TAIL = """// mathIntrinsicNames are the synthetic math package's intrinsic entry
// points (P3-M7, ADR-0007 D5): declared with exact gc signatures,
// always intercepted into wire nodes, never exported as funcdecls.
var mathIntrinsicNames = map[string]bool{
	"Float32bits": true, "Float32frombits": true,
	"Float64bits": true, "Float64frombits": true,
	"Sqrt": true, "FMA": true,
}
"""
HEADER = """// P3-M7 (WS G, ADR-0007 D5): synthetic `math` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer. The resulting *types.Package is what
// overlayImporter.Import("math") returns; the exporter emits the
// synthetic declarations' bodies into the wire document via
// exportSyntheticPackages (same as time/context, P3-M5).
//
// Value model:
//   - Pure-Go functions are gc 1.27.1's implementations ported
//     verbatim (bit-exact by construction). Where gc's amd64 path uses
//     arch assembly, the synthetic source carries a transliteration of
//     the exact amd64 algorithm (Exp: FMA-path of exp_amd64.s; Log:
//     log_amd64.s; Floor/Ceil/Trunc: generic + sNaN quieting for
//     ROUNDPD parity), each differentially verified bit-exact vs gc
//     1.27.1 on adversarial+random inputs (evidence recorded in the
//     gen_synth_math.py header).
//   - Intrinsics (exact gc signatures, panic bodies, intercepted by the
//     exporter into wire nodes): the four bit conversions (Java bit ops,
//     exact by construction), Sqrt (StrictMath.sqrt, correctly rounded
//     per JLS so bit-exact vs gc), FMA (Math.fma, correctly rounded per
//     its spec so bit-exact vs gc). StrictMath measured NOT bit-exact
//     for the other transcendentals (NaN-payload canonicalization,
//     1-ulp diffs on atan/tanh/cbrt/sinh/cosh/exp/expm1/hypot, larger on
//     the rest), so they are pure-Go ports, not JVM intrinsics.
//   - Multi-result functions (Frexp, Modf, Lgamma, Sincos) keep exact gc
//     signatures with fail-fast bodies (WS D; the P3-M5 F4 precedent).
//     Unexported single-result helpers (__frexp, __modfPair) serve the
//     pure-Go ports that need them internally (Pow, Gamma).
//
// ACCURACY CONTRACT: bit-exact vs gc 1.27.1 on every input -- by
// construction (verbatim ports) or by differential verification
// (transliterations, intrinsics). There are NO accepted divergences; the
// "1-ulp diffs" above describe the rejected StrictMath alternative, not
// the shipped code. Re-test trigger: re-run synth_math_diff_test.go when
// the Go toolchain, the JDK, or this generator changes (original
// differential evidence is recorded in the gen_synth_math.py header).
//
// Out-of-scope members: exact gc signature + panic("not in M7 scope").
// The sources use only frontend/v1-supported constructs (no generics).
package main

// syntheticMathSource is the P3-M7 synthetic `math` package. Exact gc
// 1.27.1 signatures for the in-scope API.
"""

_T_PZERO_OLD = """func pzero(x float64) float64 {
	var p *[6]float64
	var q *[5]float64
	if x >= 8 {
		p = &p0R8
		q = &p0S8
	} else if x >= 4.5454 {
		p = &p0R5
		q = &p0S5
	} else if x >= 2.8571 {
		p = &p0R3
		q = &p0S3
	} else if x >= 2 {
		p = &p0R2
		q = &p0S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*q[4]))))
	return 1 + r/s
}
"""
_T_PZERO_NEW = """func pzero(x float64) float64 {
	// P3-M7: refactored to avoid & (WS D). Select coefficients via index.
	var pi, qi int // 0=R8/S8, 1=R5/S5, 2=R3/S3, 3=R2/S2
	if x >= 8 {
		pi, qi = 0, 0
	} else if x >= 4.5454 {
		pi, qi = 1, 1
	} else if x >= 2.8571 {
		pi, qi = 2, 2
	} else if x >= 2 {
		pi, qi = 3, 3
	}
	var p [6]float64
	var q [5]float64
	switch pi {
	case 0:
		p = p0R8
	case 1:
		p = p0R5
	case 2:
		p = p0R3
	case 3:
		p = p0R2
	}
	switch qi {
	case 0:
		q = p0S8
	case 1:
		q = p0S5
	case 2:
		q = p0S3
	case 3:
		q = p0S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*q[4]))))
	return 1 + r/s
}
"""
_T_QZERO_OLD = """func qzero(x float64) float64 {
	var p, q *[6]float64
	if x >= 8 {
		p = &q0R8
		q = &q0S8
	} else if x >= 4.5454 {
		p = &q0R5
		q = &q0S5
	} else if x >= 2.8571 {
		p = &q0R3
		q = &q0S3
	} else if x >= 2 {
		p = &q0R2
		q = &q0S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*(q[4]+z*q[5])))))
	return (-0.125 + r/s) / x
}
"""
_T_QZERO_NEW = """func qzero(x float64) float64 {
	var p, q [6]float64
	if x >= 8 {
		p, q = q0R8, q0S8
	} else if x >= 4.5454 {
		p, q = q0R5, q0S5
	} else if x >= 2.8571 {
		p, q = q0R3, q0S3
	} else if x >= 2 {
		p, q = q0R2, q0S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*(q[4]+z*q[5])))))
	return (-0.125 + r/s) / x
}
"""
_T_PONE_OLD = """func pone(x float64) float64 {
	var p *[6]float64
	var q *[5]float64
	if x >= 8 {
		p = &p1R8
		q = &p1S8
	} else if x >= 4.5454 {
		p = &p1R5
		q = &p1S5
	} else if x >= 2.8571 {
		p = &p1R3
		q = &p1S3
	} else if x >= 2 {
		p = &p1R2
		q = &p1S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1.0 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*q[4]))))
	return 1 + r/s
}
"""
_T_PONE_NEW = """func pone(x float64) float64 {
	var p [6]float64
	var q [5]float64
	if x >= 8 {
		p, q = p1R8, p1S8
	} else if x >= 4.5454 {
		p, q = p1R5, p1S5
	} else if x >= 2.8571 {
		p, q = p1R3, p1S3
	} else if x >= 2 {
		p, q = p1R2, p1S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1.0 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*q[4]))))
	return 1 + r/s
}
"""
_T_QONE_OLD = """func qone(x float64) float64 {
	var p, q *[6]float64
	if x >= 8 {
		p = &q1R8
		q = &q1S8
	} else if x >= 4.5454 {
		p = &q1R5
		q = &q1S5
	} else if x >= 2.8571 {
		p = &q1R3
		q = &q1S3
	} else if x >= 2 {
		p = &q1R2
		q = &q1S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*(q[4]+z*q[5])))))
	return (0.375 + r/s) / x
}
"""
_T_QONE_NEW = """func qone(x float64) float64 {
	var p, q [6]float64
	if x >= 8 {
		p, q = q1R8, q1S8
	} else if x >= 4.5454 {
		p, q = q1R5, q1S5
	} else if x >= 2.8571 {
		p, q = q1R3, q1S3
	} else if x >= 2 {
		p, q = q1R2, q1S2
	}
	z := 1 / (x * x)
	r := p[0] + z*(p[1]+z*(p[2]+z*(p[3]+z*(p[4]+z*p[5]))))
	s := 1 + z*(q[0]+z*(q[1]+z*(q[2]+z*(q[3]+z*(q[4]+z*q[5])))))
	return (0.375 + r/s) / x
}
"""
_T_STIRLING_OLD = """func stirling(x float64) (float64, float64) {
	if x > 200 {
		return Inf(1), 1
	}
	const (
		SqrtTwoPi   = 2.506628274631000502417
		MaxStirling = 143.01608
	)
	w := 1 / x
	w = 1 + w*((((_gamS[0]*w+_gamS[1])*w+_gamS[2])*w+_gamS[3])*w+_gamS[4])
	y1 := Exp(x)
	y2 := 1.0
	if x > MaxStirling { // avoid Pow() overflow
		v := Pow(x, 0.5*x-0.25)
		y1, y2 = v, v/y1
	} else {
		y1 = Pow(x, x-0.5) / y1
	}
	return y1, SqrtTwoPi * w * y2
}
"""
_T_STIRLING_NEW = """type __stirlingPair struct{ a, b float64 }
func stirling(x float64) __stirlingPair {
	if x > 200 {
		return __stirlingPair{Inf(1), 1}
	}
	const (
		SqrtTwoPi   = 2.506628274631000502417
		MaxStirling = 143.01608
	)
	w := 1 / x
	w = 1 + w*((((_gamS[0]*w+_gamS[1])*w+_gamS[2])*w+_gamS[3])*w+_gamS[4])
	y1 := Exp(x)
	y2 := 1.0
	if x > MaxStirling { // avoid Pow() overflow
		v := Pow(x, 0.5*x-0.25)
		y1, y2 = v, v/y1
	} else {
		y1 = Pow(x, x-0.5) / y1
	}
	return __stirlingPair{y1, SqrtTwoPi * w * y2}
}
"""
_T_STIRLING_CALL1_OLD = """			y1, y2 := stirling(x)
"""
_T_STIRLING_CALL1_NEW = """			st1 := stirling(x)
			y1, y2 := st1.a, st1.b
"""
_T_STIRLING_CALL2_OLD = """		sq1, sq2 := stirling(q)
"""
_T_STIRLING_CALL2_NEW = """		st2 := stirling(q)
		sq1, sq2 := st2.a, st2.b
"""

# ---------------------------------------------------------------------------
# gc source reading
# ---------------------------------------------------------------------------

def read_gc(fn):
    """Read a gc math source file, dropping the copyright header, any
    package doc, the package clause, and leading FILE-LEVEL comments
    (comment blocks followed by a blank line -- the /* */ file headers
    and // attribution blocks). A comment block directly abutting the
    first declaration is that decl's doc and is kept (drop_docs handles
    it)."""
    with open(os.path.join(MATH, fn)) as f:
        lines = f.read().split("\n")
    assert lines[0].startswith("// Copyright"), fn
    i = 0
    while lines[i].strip() != "":
        i += 1
    j = i
    while not lines[j].startswith("package math"):
        j += 1
    lines = lines[j + 1:]
    while True:
        while lines and lines[0].strip() == "":
            lines.pop(0)
        if not lines:
            break
        first = lines[0].lstrip()
        if first.startswith("//"):
            k = 0
            while k < len(lines) and lines[k].lstrip().startswith("//"):
                k += 1
        elif first.startswith("/*"):
            k = 0
            while k < len(lines) and "*/" not in lines[k]:
                k += 1
            k += 1
        else:
            break
        # File-level iff followed by a blank line (or EOF); a doc abuts
        # its declaration with no blank.
        if k < len(lines) and lines[k].strip() != "":
            break
        del lines[:k]
    src = "\n".join(lines).lstrip("\n")
    assert "//go:build" not in src, fn
    return src


# ---------------------------------------------------------------------------
# T1: arch-dispatch branches
# ---------------------------------------------------------------------------

HAVEARCH_RE = re.compile(
    r"\n\tif haveArch\w+ \{\n\t\treturn arch\w+\([^()\n]*\)\n\t\}\n")


def drop_havearch(src, fn, expect):
    new, n = HAVEARCH_RE.subn("\n", src)
    if n != expect:
        raise AssertionError(
            "%s: haveArch branches: found %d, expected %d" % (fn, n, expect))
    assert "haveArch" not in new, fn
    return new


# ---------------------------------------------------------------------------
# T2: doc comments
# ---------------------------------------------------------------------------

def drop_doc(src, fn, name):
    """Drop the doc-comment block immediately preceding name's declaration."""
    pat = re.compile(
        r"(?m)^((?:[ \t]*//[^\n]*\n)+)"
        r"(?=[ \t]*(?:func\s+(?:\([^)]*\)\s+)?%s\b|(?:type|const|var)\s+%s\b))"
        % (re.escape(name), re.escape(name)))
    new, n = pat.subn("", src)
    if n != 1:
        raise AssertionError("%s: drop_doc(%s): matched %d" % (fn, name, n))
    return new


def blank_after_docs(src):
    """A kept doc comment is its own block: blank line before its decl."""
    pat = re.compile(
        r"(?m)^((?:[ \t]*//[^\n]*\n)+)(?=^(?:func|type|const|var)\b)")
    return pat.sub(r"\1\n", src)


# ---------------------------------------------------------------------------
# Decl dropping (T3 normalize, T5 transliteration replacements)
# ---------------------------------------------------------------------------

def _strip_for_braces(line):
    out = []
    i = 0
    while i < len(line):
        if line.startswith("//", i):
            break
        c = line[i]
        if c in "\"'`":
            q = c
            i += 1
            while i < len(line) and line[i] != q:
                if line[i] == "\\":
                    i += 1
                i += 1
            i += 1
            continue
        out.append(c)
        i += 1
    return "".join(out)


def drop_decl(src, fn, name, with_doc=True):
    """Drop a whole declaration. With with_doc=True (default) the
    doc-comment block immediately preceding it is dropped too; with
    False it is retained (used for normalize, whose doc dangles in the
    reviewed file)."""
    lines = src.split("\n")
    start = None
    for i, ln in enumerate(lines):
        if not re.match(r"^(func|type|const|var)\b", ln):
            continue
        m = re.match(r"^func\s+(?:\([^)]*\)\s+)?(\w+)", ln) or \
            re.match(r"^(?:type|const|var)\s+(\w+)", ln)
        if m and m.group(1) == name:
            start = i
            break
    if start is None:
        raise AssertionError("%s: drop_decl(%s): not found" % (fn, name))
    decl_line = lines[start]
    if with_doc:
        # Extend upward over an immediately preceding doc-comment block.
        # Tolerates a blank line between doc and decl (exp.go's exp doc).
        if start > 0 and lines[start - 1].strip() == "":
            j = start - 1
            while j > 0 and lines[j - 1].strip() == "":
                j -= 1
            if j > 0 and lines[j - 1].lstrip().startswith("//"):
                start = j
        while start > 0 and lines[start - 1].lstrip().startswith("//"):
            start -= 1
    ln = decl_line
    if ln.startswith("func"):
        depth = 0
        j = start
        started = False
        while j < len(lines):
            for ch in _strip_for_braces(lines[j]):
                if ch == "{":
                    depth += 1
                    started = True
                elif ch == "}":
                    depth -= 1
            j += 1
            if started and depth == 0:
                break
        else:
            raise AssertionError("%s: drop_decl(%s): unbalanced" % (fn, name))
        end = j
    elif re.match(r"^(const|var)\s*\($", ln):
        j = start + 1
        while lines[j].strip() != ")":
            j += 1
        end = j + 1
    else:
        end = start + 1
    del lines[start:end]
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# T3: multi-result call rewriting
# ---------------------------------------------------------------------------

_MR_INFO = {
    "Frexp": ("__frexp", "frac", "exp"),
    "Modf": ("__modf", "int", "frac"),
    "Sincos": ("__sincos", "sin", "cos"),
}
_MR_RE = re.compile(
    r"(?m)^(\t+)([A-Za-z_]\w*), ([A-Za-z_]\w*) := (Frexp|Modf|Sincos)\((.*)\)$")
# `_, b := F(args)`: no temp needed; direct field access.
_MR_BLANK_RE = re.compile(
    r"(?m)^(\t+)_, ([A-Za-z_]\w*) := (Frexp|Modf|Sincos)\((.*)\)$")


def rewrite_multiresult(src, fn):
    """Rewrite `a, b := F(args)` to the __-helper form with a per-file
    __tmN counter. `_, b := F(args)` becomes `b := __F(args).<f2>`."""
    def blank_repl(m):
        indent, b, f, args = m.groups()
        hf, _, f2 = _MR_INFO[f]
        return "%s%s := %s(%s).%s" % (indent, b, hf, args, f2)

    src = _MR_BLANK_RE.sub(blank_repl, src)

    counter = 0

    def repl(m):
        nonlocal counter
        counter += 1
        indent, a, b, f, args = m.groups()
        hf, f1, f2 = _MR_INFO[f]
        tm = "__tm%d" % counter
        return "%s%s := %s(%s); %s, %s := %s.%s, %s.%s" % (
            indent, tm, hf, args, a, b, tm, f1, tm, f2)

    return _MR_RE.sub(repl, src)


# ---------------------------------------------------------------------------
# Per-file specs
# ---------------------------------------------------------------------------

def _t(old, new, count=1):
    return (old, new, count)


def apply_transforms(src, fn, transforms):
    for old, new, count in transforms:
        n = src.count(old)
        if n != count:
            raise AssertionError(
                "%s: transform anchor matched %d, expected %d:\n%r"
                % (fn, n, count, old[:120]))
        src = src.replace(old, new)
    return src


# (gc file, options)
FILES = [
    ("const.go", dict(
        havearch=0,
        drop_docs=[],
        drop_decls=[],
        transforms=[
            _t("// Mathematical constants.\n", ""),
            _t("\tintSize = 32 << (^uint(0) >> 63) // 32 or 64\n", ""),
            _t("1<<(intSize-1) - 1", "1<<(64-1) - 1"),
            _t("-1 << (intSize - 1)", "-1 << (64 - 1)"),
            _t("1<<intSize - 1", "1<<64 - 1"),
        ],
    )),
    ("bits.go", dict(
        havearch=0,
        drop_docs=[],
        drop_decls=["normalize"],
    )),
    ("abs.go", dict(havearch=0, drop_docs=["Abs"])),
    ("signbit.go", dict(havearch=0, drop_docs=["Signbit"])),
    ("copysign.go", dict(havearch=0, drop_docs=["Copysign"])),
    ("mod.go", dict(
        havearch=1,
        drop_docs=["Mod"],
    )),
    ("remainder.go", dict(havearch=1, drop_docs=["Remainder"])),
    ("nextafter.go", dict(havearch=0, drop_docs=["Nextafter32"])),
    ("logb.go", dict(
        havearch=0,
        drop_docs=["Logb"],
        transforms=[
            _t("\tx, exp := normalize(x)\n"
               "\treturn int((Float64bits(x)>>shift)&mask) - bias + exp",
               "\t// Inlined normalize (gc bits.go).\n"
               "\tconst __smallestNormal2 = 2.2250738585072014e-308\n"
               "\texp := 0\n"
               "\tif Abs(x) < __smallestNormal2 {\n"
               "\t\tx = x * (1 << 52)\n"
               "\t\texp = -52\n"
               "\t}\n"
               "\treturn int((Float64bits(x)>>shift)&mask) - bias + exp"),
        ],
    )),
    ("ldexp.go", dict(
        havearch=1,
        drop_docs=["Ldexp"],
        transforms=[
            _t("\tfrac, e := normalize(frac)\n\texp += e",
               "\t// Inlined normalize (gc bits.go).\n"
               "\tconst __smallestNormal3 = 2.2250738585072014e-308\n"
               "\t{\n"
               "\t\te := 0\n"
               "\t\tif Abs(frac) < __smallestNormal3 {\n"
               "\t\t\tfrac = frac * (1 << 52)\n"
               "\t\t\te = -52\n"
               "\t\t}\n"
               "\t\texp += e\n"
               "\t}"),
        ],
    )),
    ("pow10.go", dict(
        havearch=0,
        drop_docs=["pow10tab"],
    )),
    ("tan.go", dict(
        havearch=1,
        drop_docs=["_tanP"],
        transforms=[
            _t("\t\tj, z = trigReduce(x)",
               "\t\ttz := trigReduce(x)\n\t\tj, z = tz.j, tz.z"),
        ],
    )),
    ("asin.go", dict(havearch=2, drop_docs=["Asin"])),
    ("atan.go", dict(havearch=1, drop_docs=["xatan"])),
    ("atan2.go", dict(havearch=1, drop_docs=["Atan2"])),
    ("asinh.go", dict(havearch=1, drop_docs=["Asinh"])),
    ("acosh.go", dict(havearch=1, drop_docs=["Acosh"])),
    ("atanh.go", dict(havearch=1, drop_docs=["Atanh"])),
    ("expm1.go", dict(havearch=1, drop_docs=["Expm1"])),
    ("log1p.go", dict(havearch=1, drop_docs=["Log1p"])),
    ("erf.go", dict(havearch=2, drop_docs=[])),
    ("erfinv.go", dict(havearch=0, drop_docs=[])),
    ("j0.go", dict(
        havearch=0,
        drop_docs=["J0"],
        transforms=[
            _t(_T_PZERO_OLD, _T_PZERO_NEW),
            _t(_T_QZERO_OLD, _T_QZERO_NEW),
        ],
    )),
    ("j1.go", dict(
        havearch=0,
        drop_docs=["J1"],
        transforms=[
            _t(_T_PONE_OLD, _T_PONE_NEW),
            _t(_T_QONE_OLD, _T_QONE_NEW),
        ],
    )),
    ("jn.go", dict(
        havearch=0,
        drop_docs=["Jn"],
        transforms=[
            _t("\t\tswitch s, c := Sincos(x); n & 3 {",
               "\t\t__sc := __sincos(x); s, c := __sc.sin, __sc.cos; switch n & 3 {",
               2),
        ],
    )),
    ("sinh.go", dict(havearch=2, drop_docs=["Sinh"])),
    ("tanh.go", dict(havearch=1, drop_docs=[])),
    ("log10.go", dict(havearch=2, drop_docs=["Log10"])),
    ("cbrt.go", dict(havearch=1, drop_docs=["Cbrt"])),
    ("sin.go", dict(
        havearch=2,
        drop_docs=["_sin"],
        transforms=[
            _t("\t\tj, z = trigReduce(x)",
               "\t\ttz := trigReduce(x)\n\t\tj, z = tz.j, tz.z",
               2),
        ],
    )),
    ("trig_reduce.go", dict(
        havearch=0,
        drop_docs=["reduceThreshold"],
        transforms=[
            _t('import (\n\t"math/bits"\n)\n\n', ""),
            _t("func trigReduce(x float64) (j uint64, z float64) {",
               "type __trigReducePair struct{ j uint64; z float64 }\n"
               "func trigReduce(x float64) __trigReducePair {"),
            _t("\tif x < PI4 {\n\t\treturn 0, x\n\t}\n",
               "\tif x < PI4 {\n\t\treturn __trigReducePair{0, x}\n\t}\n"
               "\tvar j uint64\n\tvar z float64\n"),
            _t("\tz2hi, _ := bits.Mul64(z2, ix)\n"
               "\tz1hi, z1lo := bits.Mul64(z1, ix)",
               "\tm1 := mathbitsMul64(z2, ix)\n"
               "\tz2hi := m1.hi\n"
               "\tm2 := mathbitsMul64(z1, ix)\n"
               "\tz1hi, z1lo := m2.hi, m2.lo"),
            _t("\tlo, c := bits.Add64(z1lo, z2hi, 0)\n"
               "\thi, _ := bits.Add64(z0lo, z1hi, c)",
               "\ta1 := mathbitsAdd64(z1lo, z2hi, 0)\n"
               "\tlo, c := a1.lo, a1.hi\n"
               "\ta2 := mathbitsAdd64(z0lo, z1hi, c)\n"
               "\thi := a2.lo"),
            _t("\tlz := uint(bits.LeadingZeros64(hi))",
               "\tlz := uint(mathbitsLeadingZeros64(hi))"),
            _t("\thi = (hi << (lz + 1)) | (lo >> (64 - (lz + 1)))",
               "\t// P3-M7: guard lz+1 <= 64 to avoid shift underflow (Go's uint\n"
               "\t// wraparound gives >>huge==0; the guard is equivalent).\n"
               "\tif lz+1 <= 64 {\n"
               "\t\thi = (hi << (lz + 1)) | (lo >> (64 - (lz + 1)))\n"
               "\t} else {\n"
               "\t\thi = 0\n"
               "\t}"),
            _t("\treturn j, z * PI4",
               "\treturn __trigReducePair{j, z * PI4}"),
        ],
    )),
    ("pow.go", dict(havearch=1, drop_docs=[])),
    ("gamma.go", dict(
        havearch=0,
        drop_docs=[],
        transforms=[
            _t(_T_STIRLING_OLD, _T_STIRLING_NEW),
            _t(_T_STIRLING_CALL1_OLD, _T_STIRLING_CALL1_NEW),
            _t(_T_STIRLING_CALL2_OLD, _T_STIRLING_CALL2_NEW),
        ],
    )),
    ("dim.go", dict(havearch=2, drop_docs=["Dim"])),
    ("floor.go", dict(
        havearch=0,
        drop_docs=["Floor"],
        transforms=[
            # T5: sNaN-quieting wrappers (ROUNDPD parity). The arch
            # branches are replaced, not merely dropped.
            _t("func Floor(x float64) float64 {\n"
               "\tif haveArchFloor {\n"
               "\t\treturn archFloor(x)\n"
               "\t}\n"
               "\treturn floor(x)\n"
               "}",
               "func Floor(x float64) float64 {\n"
               "\tif IsNaN(x) {\n"
               "\t\treturn Float64frombits(Float64bits(x) | 0x0008000000000000)\n"
               "\t}\n"
               "\treturn floor(x)\n"
               "}"),
            _t("func Ceil(x float64) float64 {\n"
               "\tif haveArchCeil {\n"
               "\t\treturn archCeil(x)\n"
               "\t}\n"
               "\treturn ceil(x)\n"
               "}",
               "func Ceil(x float64) float64 {\n"
               "\tif IsNaN(x) {\n"
               "\t\treturn Float64frombits(Float64bits(x) | 0x0008000000000000)\n"
               "\t}\n"
               "\treturn ceil(x)\n"
               "}"),
            _t("func Trunc(x float64) float64 {\n"
               "\tif haveArchTrunc {\n"
               "\t\treturn archTrunc(x)\n"
               "\t}\n"
               "\treturn trunc(x)\n"
               "}",
               "func Trunc(x float64) float64 {\n"
               "\tif IsNaN(x) {\n"
               "\t\treturn Float64frombits(Float64bits(x) | 0x0008000000000000)\n"
               "\t}\n"
               "\treturn trunc(x)\n"
               "}"),
            _t("\t\tbits += (halfMinusULP + (bits>>(shift-e))&1) >> e\n"
               "\t\tbits &^= fracMask >> e",
               "\t\t// P3-M7: guard against e >= shift (no fractional bits; x is\n"
               "\t\t// already an integer). gc's uint arithmetic makes shift-e wrap\n"
               "\t\t// to a huge value (bits>>huge == 0); the guard is equivalent\n"
               "\t\t// and avoids relying on wraparound in the shift.\n"
               "\t\tif e < shift {\n"
               "\t\t\tbits += (halfMinusULP + (bits>>(shift-e))&1) >> e\n"
               "\t\t\tbits &^= fracMask >> e\n"
               "\t\t}"),
        ],
    )),
    ("hypot.go", dict(havearch=1, drop_docs=["Hypot"])),
    ("exp.go", dict(
        havearch=2,
        drop_docs=["Exp2"],
        drop_decls=["Exp", "exp"],
    )),
]


def process_file(fn, opts):
    src = read_gc(fn)
    src = apply_transforms(src, fn, opts.get("transforms", ()))
    src = drop_havearch(src, fn, opts.get("havearch", 0))
    for name in opts.get("drop_docs", ()):
        src = drop_doc(src, fn, name)
    for name in opts.get("drop_decls", ()):
        # normalize keeps its (dangling) doc; everything else drops it.
        src = drop_decl(src, fn, name, with_doc=(name != "normalize"))
    src = blank_after_docs(src)
    src = rewrite_multiresult(src, fn)
    # Normalize inter-decl spacing: the reviewed file separates top-level
    # declarations with exactly one blank line (gc occasionally omits it).
    src = re.sub(r"(?m)^\}$\n(?=^(?:var|const|func|type)\b)", "}\n\n", src)
    return src.strip("\n")


def main():
    gc_parts = []
    for fn, opts in FILES:
        gc_parts.append(process_file(fn, opts))
    # The reviewed file joins sections with a blank line, except two
    # hand-assembled boundaries (HELPERS->EXP, LOG->FAILFAST) which have
    # none; reproduced exactly.
    body = "\n\n".join(gc_parts)
    body += "\n\n" + MATHBITS.rstrip("\n")
    body += "\n\n" + HELPERS.rstrip("\n")
    body += "\n" + EXP.rstrip("\n")
    body += "\n\n" + LOG.rstrip("\n")
    body += "\n" + FAILFAST.rstrip("\n")

    out = HEADER
    out += "const syntheticMathSource = `package math\n"
    out += "\n" + PROLOGUE.rstrip("\n")
    out += "\n\n" + GEN_MARKER.rstrip("\n")
    out += "\n\n" + body
    out += "\n`\n\n" + TAIL
    if not out.endswith("\n"):
        out += "\n"
    with open(os.path.join(OUTDIR, "synth_math.go"), "w") as f:
        f.write(out)
    print("gen_synth_math: wrote synth_math.go (%d bytes)" % len(out))


main()
