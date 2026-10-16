package main

// ar19_complex_edges: differential evidence for the PKG-026 complex-division
// port (commit bd6bf298, BinaryOpNode.complex128div — Smith-1962 robust
// division + C99 G.5.1 edge corrections).
//
// R3-P1-2 / R3-P2-1: bd6bf298's message claims the division port "was
// validated bit-exact vs stock gc on 30 edge cases" but committed no
// artifact — the only differential test (ar18) covers 17 finite values and
// never touches the C99 correction paths that make the port non-trivial.
// This file reconstructs that claimed coverage as a checked-in artifact:
// every variable below is compared against stock gc at test time by
// ArithmeticCorpusTest.testAr19Complex, encoded as hex(re):hex(im) of the
// component bits — bit-identical except for NaN payloads (see the NaN note
// below).
//
// The original 30 input pairs were never recorded, so an exact 1:1 mapping
// is impossible; the 43 observed values below cover every category the
// commit message names — overflow, underflow, subnormals, +-Inf, NaN
// signs/payloads, signed zeros, div-by-zero — exercising:
//   - C99 correction 1 (finite/zero): dz1..dz4 — incl. copySign(Inf,-0.0)
//     via a -0.0 denominator (dz2) and NaN numerators (dz4)
//   - C99 correction 2 (infinite numerator, finite denominator): di1..di4 —
//     incl. -Inf (di2), (Inf,Inf) (di3), NaN-sign propagation (di4)
//   - C99 correction 3 (finite numerator, infinite denominator): iz1..iz3 —
//     signed-zero results incl. (-0,+0) (iz3)
//   - Smith-1962 robustness where the naive formula diverges: robDiv —
//     (1+1i)/(1e308+1e308i) is (+0,+0); the naive formula gives (NaN,NaN)
//   - NaN in the real and imaginary component of each of + - * /:
//     nadd1 nadd2 nsub1 nsub2 nmul1 nmul2 ndiv1 ndiv2 (plus infsub, where
//     the NaN comes from Inf-Inf)
//   - signed-zero propagation: szA szB szAdd szMul szNeg
//   - overflow: ovfMul ((1e308,1e308)^2 -> (NaN,+Inf)), ovfDiv
//   - underflow: unfMul; subnormals: subAdd subMul subDiv subDiv2
//   - complex64 div-by-zero narrowing: c64dz1 c64dz2; complex64 overflow
//     (c64ovf), infinite-numerator narrowing (c64inf), NaN (c64nan),
//     subnormal (c64sub)
//   - ++ saturation near float32/float64 max: c64inc c128inc
//   - untyped-complex constant division: ucd — gc folds these at ~512-bit
//     precision while the frontend evaluates in float64 (R3-P2-4 caveat);
//     pinned here, bit-exact on this input.
//
// NaN note (measured 2026-10-16, x86-64): NaN *presence* is asserted, NaN
// *payloads* are not — and cannot be, on the JVM. Go leaves NaN payloads
// unspecified; gc emits the hardware indefinite NaN (0xfff8…: 0.0/0.0 and
// Inf*0.0 both yield 0xfff8000000000000), while HotSpot canonicalizes every
// NaN result to 0x7ff8… (the same operations yield 0x7ff8000000000000 —
// even the *sign* differs: fff8… is negative, 7ff8… positive). Payload
// equality would pin an implementation accident, not a semantic (it varies
// by CPU and JVM), so the harness compares NaN payloads by NaN-ness
// (ArithmeticCorpusTest.valuesEqual); every non-NaN bit stays bit-exact,
// and signed zeros still distinguish +0 from -0. Precedent:
// ComplexBinaryOpTest asserts isNaN only for NaN components.
//
// NaN/Inf are produced at runtime (no imports allowed in corpus files).

func main() {
	z := 0.0
	nz := -z // -0.0
	big := 1e308
	inf := big * 10 // +Inf
	ninf := -inf
	nan := z / z   // quiet NaN
	tiny := 5e-324 // smallest float64 subnormal
	t2 := tiny * 2 // subnormal, exact

	// C99 correction 1: finite numerator, zero denominator.
	dz1 := complex(1, 0) / complex(z, z)
	dz2 := complex(1, 2) / complex(nz, z)
	dz3 := complex(0, 0) / complex(z, z)
	dz4 := complex(nan, 0) / complex(z, z)

	// C99 correction 2: infinite numerator, finite denominator.
	di1 := complex(inf, 1) / complex(2, 3)
	di2 := complex(1, ninf) / complex(2, 3)
	di3 := complex(inf, inf) / complex(2, 3)
	di4 := complex(inf, nan) / complex(2, 3)

	// C99 correction 3: finite numerator, infinite denominator.
	iz1 := complex(1, 1) / complex(inf, inf)
	iz2 := complex(1, 1) / complex(inf, 0)
	iz3 := complex(1, 1) / complex(0, ninf)

	// Smith-1962 robustness: naive formula overflows the denominator.
	robDiv := complex(1, 1) / complex(big, big)

	// NaN in real/imaginary components of + - * /.
	nadd1 := complex(nan, 1) + complex(2, 3)
	nadd2 := complex(1, nan) + complex(2, 3)
	nsub1 := complex(nan, 1) - complex(2, 3)
	nsub2 := complex(1, nan) - complex(2, 3)
	nmul1 := complex(nan, 1) * complex(2, 3)
	nmul2 := complex(1, 2) * complex(3, nan)
	ndiv1 := complex(nan, 0) / complex(1, 1)
	ndiv2 := complex(1, 1) / complex(0, nan)
	infsub := complex(inf, 1) - complex(inf, 2)

	// Signed zeros.
	szA := complex(nz, z)
	szB := complex(z, nz)
	szAdd := complex(nz, nz) + complex(z, z)
	szMul := complex(nz, z) * complex(2, 0)
	szNeg := -complex(nz, z)
	cneginf := -complex(inf, ninf)

	// Overflow / underflow.
	ovfMul := complex(big, big) * complex(big, big)
	ovfDiv := complex(big, big) / complex(1e-308, 0)
	unfMul := complex(1e-308, 1e-308) * complex(1e-308, 1e-308)

	// Subnormals.
	subAdd := complex(tiny, tiny) + complex(tiny, tiny)
	subMul := complex(tiny, 0) * complex(2, 0)
	subDiv := complex(t2, 0) / complex(2, 0)
	subDiv2 := complex(tiny, tiny) / complex(tiny, tiny)

	// complex64: div-by-zero narrowing, overflow, Inf narrowing, NaN, subnormal.
	fz := float32(0)
	fnz := -fz
	fbig := float32(3e38)
	finf := fbig * 10
	c64dz1 := complex(float32(1), float32(0)) / complex(fz, fz)
	c64dz2 := complex(float32(1), float32(2)) / complex(fnz, fz)
	c64ovf := complex(fbig, fbig) * complex(float32(2), float32(0))
	c64inf := complex(finf, float32(1)) / complex(float32(2), float32(3))
	c64nan := complex(float32(nan), fz) / complex(float32(2), float32(3))
	c64sub := complex(float32(1e-45), float32(0)) * complex(float32(2), float32(0))

	// ++ saturation near float32/float64 max.
	c64inc := complex(fbig, float32(1))
	c64inc++
	c128inc := complex(1.7976931348623157e308, 1)
	c128inc++

	// Untyped-complex constant division (R3-P2-4 caveat pin).
	ucd := (1 + 2i) / (3 + 4i)

	dz1 = dz1
	dz2 = dz2
	dz3 = dz3
	dz4 = dz4
	di1 = di1
	di2 = di2
	di3 = di3
	di4 = di4
	iz1 = iz1
	iz2 = iz2
	iz3 = iz3
	robDiv = robDiv
	nadd1 = nadd1
	nadd2 = nadd2
	nsub1 = nsub1
	nsub2 = nsub2
	nmul1 = nmul1
	nmul2 = nmul2
	ndiv1 = ndiv1
	ndiv2 = ndiv2
	infsub = infsub
	szA = szA
	szB = szB
	szAdd = szAdd
	szMul = szMul
	szNeg = szNeg
	cneginf = cneginf
	ovfMul = ovfMul
	ovfDiv = ovfDiv
	unfMul = unfMul
	subAdd = subAdd
	subMul = subMul
	subDiv = subDiv
	subDiv2 = subDiv2
	c64dz1 = c64dz1
	c64dz2 = c64dz2
	c64ovf = c64ovf
	c64inf = c64inf
	c64nan = c64nan
	c64sub = c64sub
	c64inc = c64inc
	c128inc = c128inc
	ucd = ucd
}

// observe: dz1 dz2 dz3 dz4 di1 di2 di3 di4 iz1 iz2 iz3 robDiv nadd1 nadd2 nsub1 nsub2 nmul1 nmul2 ndiv1 ndiv2 infsub szA szB szAdd szMul szNeg cneginf ovfMul ovfDiv unfMul subAdd subMul subDiv subDiv2 c64dz1 c64dz2 c64ovf c64inf c64nan c64sub c64inc c128inc ucd
