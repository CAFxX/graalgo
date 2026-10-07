// P3-M7 (wave 1): synthetic `encoding/json` package.
package main

// syntheticJsonSource is the P3-M7 synthetic `encoding/json` package.
const syntheticJsonSource = `package json

import "bytes"

// Float formatting and parsing for the synthetic encoding/json.
//
// All algorithms are clean-room and use only integer arithmetic (no
// math, no strconv, no unsafe), so they run identically under gc and
// under graalgo's frontend/v1. Byte-identity with gc is verified
// differentially (see float_test.go).
//
// Formatting (marshal): decompose the float64 into exact (mantissa,
// exponent) via power-of-2 scaling, expand to the exact decimal,
// select the shortest decimal that round-trips (verified with the
// exact parser), then render in gc's json 'f'/'e' style.
// float32 uses the same machinery at 32-bit precision (jsonShortest32:
// shortest decimal that round-trips through float32, with 32-bit ulps
// and significand tie-breaking), as gc does (bitSize 32).
//
// Parsing (unmarshal): parse the decimal literal exactly, compute a
// float64 approximation, then fix up via exact decimal comparison
// (provably correctly rounded).

// jsonDecompose splits a finite nonzero float64 f into (m, e) with
// |f| = m * 2^e exactly, m in [2^52, 2^53). Pure arithmetic: scaling
// by powers of two is exact for finite floats.
// jsonDecomposePair is the single-result form of jsonDecompose (WS D:
// v1 cannot emit multi-result functions).
type jsonDecomposePair struct {
	m uint64
	e int
}

func jsonDecompose(f float64) jsonDecomposePair {
	if f < 0 {
		f = -f
	}
	// f > 0 here.
	e2 := 0
	for f >= 2 {
		f /= 2
		e2++
	}
	for f < 1 {
		f *= 2
		e2--
	}
	// 1 <= f < 2; f * 2^52 is an exact integer in [2^52, 2^53).
	m := uint64(f * 4503599627370496.0)
	return jsonDecomposePair{m, e2 - 52}
}

// jsonIsNegZero reports whether f is negative zero.
func jsonIsNegZero(f float64) bool {
	return f == 0 && 1/f < 0
}

// jsonIsInf reports whether f is infinite (either sign).
func jsonIsInf(f float64) bool {
	return f > 1.7976931348623157e308 || f < -1.7976931348623157e308
}

// jsonIsNaN reports whether f is NaN.
func jsonIsNaN(f float64) bool {
	return f != f
}

// jsonDecimal is an exact decimal value: neg * 0.digits * 10^exp.
// digits holds the significant decimal digits (no leading zeros;
// no trailing zeros unless the value is exactly "0").
type jsonDecimal struct {
	neg    bool
	digits []byte // each 0-9
	exp    int
}

// jsonDecimalZero is the exact decimal zero.
func jsonDecimalZero(neg bool) jsonDecimal {
	return jsonDecimal{neg: neg, digits: []byte{0}, exp: 0}
}

// jsonStripZeros removes leading and trailing zeros from a digit slice,
// returning the stripped digits and the exponent adjustment.
func jsonStripZeros(d []byte) []byte {
	i := 0
	for i < len(d) && d[i] == 0 {
		i++
	}
	j := len(d)
	for j > i && d[j-1] == 0 {
		j--
	}
	if i >= j {
		return []byte{0}
	}
	return d[i:j]
}

// jsonDecimalFromParts builds the exact decimal for m * 2^e (m > 0).

// jsonDecimalCompare compares exact decimals: -1, 0, +1.
// Signs first, then magnitude by exponent, then lexicographically.
func jsonDecimalCompare(a, b jsonDecimal) int {
	if a.neg != b.neg {
		// Zero is never negative here (jsonDecimalZero keeps the sign
		// but -0 == +0 for comparison).
		aZero := len(a.digits) == 1 && a.digits[0] == 0
		bZero := len(b.digits) == 1 && b.digits[0] == 0
		if aZero && bZero {
			return 0
		}
		if a.neg {
			return -1
		}
		return 1
	}
	c := jsonDecimalCompareMag(a, b)
	if a.neg {
		return -c
	}
	return c
}

func jsonDecimalCompareMag(a, b jsonDecimal) int {
	aZero := len(a.digits) == 1 && a.digits[0] == 0
	bZero := len(b.digits) == 1 && b.digits[0] == 0
	if aZero && bZero {
		return 0
	}
	if aZero {
		return -1
	}
	if bZero {
		return 1
	}
	// Compare by decimal exponent (position of most significant digit),
	// then lexicographically after aligning.
	if a.exp != b.exp {
		if a.exp < b.exp {
			return -1
		}
		return 1
	}
	// Same exponent: pad the shorter with trailing zeros and compare.
	la, lb := len(a.digits), len(b.digits)
	n := la
	if lb > n {
		n = lb
	}
	for i := 0; i < n; i++ {
		var da, db byte
		if i < la {
			da = a.digits[i]
		}
		if i < lb {
			db = b.digits[i]
		}
		if da != db {
			if da < db {
				return -1
			}
			return 1
		}
	}
	return 0
}

// jsonDecimalRound rounds d to k significant digits (k >= 1). Ties on
// exact halfway round to even, matching gc strconv's shouldRoundUp: the
// shortest-repr candidate at each length is the correctly rounded one.
// d's digits carry no trailing zeros, so a '5' in the rounding position
// with no digits after it is an exact halfway case. Used for
// shortest-repr candidate generation.
func jsonDecimalRound(d jsonDecimal, k int) jsonDecimal {
	if len(d.digits) <= k {
		return d
	}
	// Look at digit k (0-based) to decide rounding.
	roundUp := d.digits[k] > 5
	if d.digits[k] == 5 {
		if len(d.digits) > k+1 {
			// '5' followed by more digits: above halfway (no trailing
			// zeros, so a following digit is nonzero).
			roundUp = true
		} else {
			// Exact halfway: round to even.
			roundUp = d.digits[k-1]%2 == 1
		}
	}
	// Take first k digits.
	nd := make([]byte, k)
	copy(nd, d.digits[:k])
	if roundUp {
		// Add one, propagating carry.
		for i := k - 1; i >= 0; i-- {
			nd[i]++
			if nd[i] < 10 {
				break
			}
			nd[i] = 0
			if i == 0 {
				// Overflow: 999 -> 1000.
				nd = append([]byte{1}, nd...)
				// exp increases by 1 (one more digit before the point).
				return jsonDecimal{neg: d.neg, digits: jsonStripZeros(nd), exp: d.exp + 1}
			}
		}
	}
	return jsonDecimal{neg: d.neg, digits: jsonStripZeros(nd), exp: d.exp}
}

// ============ Decimal -> float64 (correctly rounded) ============

// jsonParseDecimal parses a JSON number literal (already validated) into
// (neg, digits, exp10) with value = [-]0.digits * 10^exp10. digits has no
// leading zeros and no trailing zeros (unless the value is zero).
// jsonDecimalPair is the single-result form of jsonParseDecimal (WS D:
// v1 cannot emit multi-result functions).
type jsonDecimalPair struct {
	neg    bool
	digits []byte
	exp10  int
}

func jsonParseDecimal(s string) jsonDecimalPair {
	var neg bool
	var digits []byte
	var exp10 int
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	} else if s[0] == '+' {
		i = 1
	}
	// Integer part.
	intStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intDigits := s[intStart:i]
	// Fraction part.
	var fracDigits string
	if i < len(s) && s[i] == '.' {
		i++
		fracStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		fracDigits = s[fracStart:i]
	}
	// Exponent part.
	expVal := 0
	expNeg := false
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && s[i] == '+' {
			i++
		} else if i < len(s) && s[i] == '-' {
			expNeg = true
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			expVal = expVal*10 + int(s[i]-'0')
			i++
		}
	}
	if expNeg {
		expVal = -expVal
	}
	// Combine: digits = intDigits + fracDigits, strip leading/trailing zeros.
	all := intDigits + fracDigits
	// Strip leading zeros.
	j := 0
	for j < len(all) && all[j] == '0' {
		j++
	}
	all = all[j:]
	// Strip trailing zeros (count them for the exponent).
	tz := 0
	for len(all) > 0 && all[len(all)-1] == '0' {
		all = all[:len(all)-1]
		tz++
	}
	if all == "" {
		return jsonDecimalPair{neg, []byte{0}, 0}
	}
	digits = make([]byte, len(all))
	for idx := range all {
		digits[idx] = all[idx] - '0'
	}
	// value = 0.digits * 10^exp10.
	// orig value = orig_int * 10^(expVal - len(fracDigits)),
	// stripped_int = orig_int / 10^tz,
	// 0.digits = stripped_int * 10^(-len(digits)).
	// So exp10 = expVal - len(fracDigits) + tz + len(digits).
	exp10 = expVal - len(fracDigits) + tz + len(digits)
	return jsonDecimalPair{neg, digits, exp10}
}

// jsonDecimalFromFloat returns the exact decimal of a float64.
func jsonDecimalFromFloat(f float64) jsonDecimal {
	if f == 0 {
		return jsonDecimalZero(jsonIsNegZero(f))
	}
	neg := f < 0 || jsonIsNegZero(f)
	if neg {
		f = -f
	}
	jdecomp := jsonDecompose(f)
	m, e := jdecomp.m, jdecomp.e
	return jsonDecimalFromParts(m, e, neg)
}

// jsonPow2 returns 2^n exactly (n in [-1074, 1023]).
func jsonPow2(n int) float64 {
	p := 1.0
	if n >= 0 {
		for i := 0; i < n; i++ {
			p *= 2
		}
	} else {
		for i := 0; i < -n; i++ {
			p /= 2
		}
	}
	return p
}

// jsonNextUp returns the next float64 above f (f finite; f may be 0).
func jsonNextUp(f float64) float64 {
	if f == 0 {
		return 5e-324 // min subnormal
	}
	// f > 0 here (callers use magnitudes).
	jdecomp := jsonDecompose(f)
	e := jdecomp.e
	ue := e
	if ue < -1074 {
		ue = -1074
	}
	return f + jsonPow2(ue)
}

// jsonNextDown returns the previous float64 below f (f > 0, finite).
func jsonNextDown(f float64) float64 {
	if f == 5e-324 {
		return 0
	}
	jdecomp := jsonDecompose(f)
	m, e := jdecomp.m, jdecomp.e
	if m == 1<<52 && e >= -1074 {
		// f is a power of 2 (normal): ulp below is halved.
		return f - jsonPow2(e-1)
	}
	ue := e
	if ue < -1074 {
		ue = -1074
	}
	return f - jsonPow2(ue)
}

// jsonIsEvenFloat reports whether f's significand LSB is 0
// (for round-half-even tie-breaking).
func jsonIsEvenFloat(f float64) bool {
	jdecomp := jsonDecompose(f)
	m, e := jdecomp.m, jdecomp.e
	if e >= -1074 {
		return m%2 == 0
	}
	// Subnormal: f = M * 2^-1074, M = m >> (-e-1074).
	shift := uint(-e - 1074)
	return (m>>shift)%2 == 0
}

// jsonPickNearer picks the nearer of two adjacent floats f_lo < f_hi to
// the exact decimal trueDec; ties go to the even float.
func jsonPickNearer(f_lo, f_hi float64, trueDec jsonDecimal) float64 {
	jdecompLo := jsonDecompose(f_lo)
	m1, e1 := jdecompLo.m, jdecompLo.e
	jdecompHi := jsonDecompose(f_hi)
	m2, e2 := jdecompHi.m, jdecompHi.e
	// sum = f_lo + f_hi, exactly, as (sumM, sumE).
	var sumM uint64
	var sumE int
	if e1 <= e2 {
		sumM = m1 + (m2 << uint(e2-e1))
		sumE = e1
	} else {
		sumM = m2 + (m1 << uint(e1-e2))
		sumE = e2
	}
	// midpoint = sum/2, exactly.
	midDec := jsonDecimalFromParts(sumM, sumE-1, false)
	cmp := jsonDecimalCompare(trueDec, midDec)
	if cmp < 0 {
		return f_lo
	}
	if cmp > 0 {
		return f_hi
	}
	if jsonIsEvenFloat(f_lo) {
		return f_lo
	}
	return f_hi
}

// jsonMaxFloat is the largest finite float64.
const jsonMaxFloat = 1.7976931348623157e308

// jsonMinSubnormal is the smallest positive subnormal float64.
const jsonMinSubnormal = 5e-324

// jsonOverThresh is 2^1024 - 2^970: magnitudes >= it round to Inf.
// jsonUnderThresh is 2^-1075: magnitudes <= it round to 0.
// Computed once at package init (big-int decimal work, not per-parse).
var jsonOverThresh = jsonDecimalFromParts((1<<54)-1, 970, false)
var jsonUnderThresh = jsonDecimalFromParts(1, -1075, false)

// jsonParseFloat parses a JSON number literal to the nearest float64
// (correctly rounded, matching strconv.ParseFloat).
func jsonParseFloat(s string) float64 {
	jdec := jsonParseDecimal(s)
	neg, digits, exp10 := jdec.neg, jdec.digits, jdec.exp10
	if len(digits) == 1 && digits[0] == 0 {
		if neg {
			z := 0.0
			return -z
		}
		return 0
	}
	trueDec := jsonDecimal{neg: false, digits: digits, exp: exp10}
	// Overflow: |true| >= 2^1024 - 2^970 rounds to Inf.
	zero := 0.0
	if jsonDecimalCompareMag(trueDec, jsonOverThresh) >= 0 {
		if neg {
			return -(1.0 / zero) // -Inf
		}
		return 1.0 / zero // +Inf
	}
	// Underflow: |true| <= 2^-1075 rounds to 0.
	if jsonDecimalCompareMag(trueDec, jsonUnderThresh) <= 0 {
		if neg {
			z := 0.0
			return -z
		}
		return 0
	}
	// Naive approximation: first 16 digits, then scale by 10^e.
	var d uint64
	n := len(digits)
	if n > 16 {
		n = 16
	}
	for i := 0; i < n; i++ {
		d = d*10 + uint64(digits[i])
	}
	approx := float64(d)
	e := exp10 - n
	for e > 0 {
		approx *= 10
		e--
	}
	for e < 0 {
		approx /= 10
		e++
	}
	// Clamp spurious over/underflow (true value is within ~200 ulps).
	if jsonIsInf(approx) {
		approx = jsonMaxFloat
	} else if approx == 0 {
		approx = jsonMinSubnormal
	}
	// Fixup: walk by ulps until the true value is bracketed.
	for i := 0; i < 100000; i++ {
		decApprox := jsonDecimalFromFloat(approx)
		// Compare magnitudes (both positive).
		decApprox.neg = false
		cmp := jsonDecimalCompare(decApprox, trueDec)
		if cmp == 0 {
			break
		}
		if cmp < 0 {
			next := jsonNextUp(approx)
			decNext := jsonDecimalFromFloat(next)
			decNext.neg = false
			if jsonDecimalCompare(decNext, trueDec) >= 0 {
				approx = jsonPickNearer(approx, next, trueDec)
				break
			}
			approx = next
		} else {
			next := jsonNextDown(approx)
			decNext := jsonDecimalFromFloat(next)
			decNext.neg = false
			if jsonDecimalCompare(decNext, trueDec) <= 0 {
				approx = jsonPickNearer(next, approx, trueDec)
				break
			}
			approx = next
		}
	}
	if neg {
		approx = -approx
	}
	return approx
}

// ============ float64 -> shortest decimal (for marshal) ============

// jsonShortest returns the shortest significant-digit string and decimal
// exponent for f: value = 0.digits * 10^exp, digits has no trailing zeros,
// and parsing the digits back yields exactly f.
// jsonShortestPair is the single-result form of jsonShortest (WS D:
// v1 cannot emit multi-result functions).
type jsonShortestPair struct {
	digits []byte
	exp    int
}

func jsonShortest(f float64) jsonShortestPair {
	// f is finite and nonzero (callers handle 0/Inf/NaN).
	neg := f < 0
	if neg {
		f = -f
	}
	exact := jsonDecimalFromFloat(f)
	// The exact decimal always round-trips; try fewer digits.
	for k := 1; k <= 17; k++ {
		cand := jsonDecimalRound(exact, k)
		// Parse the candidate back (as a decimal string) and check.
		candStr := jsonDecimalToString(cand)
		if jsonParseFloat(candStr) == f {
			if neg {
				// digits are magnitude-only; sign handled by caller.
			}
			return jsonShortestPair{cand.digits, cand.exp}
		}
	}
	// 17 digits always suffice; unreachable in practice.
	cand := jsonDecimalRound(exact, 17)
	return jsonShortestPair{cand.digits, cand.exp}
}

// jsonDecimalToString renders a jsonDecimal as a plain decimal string
// (for re-parsing; not for output).
func jsonDecimalToString(d jsonDecimal) string {
	var s []byte
	if d.neg {
		s = append(s, '-')
	}
	// value = 0.digits * 10^exp.
	// Render as digits with decimal point.
	nd := len(d.digits)
	if d.exp <= 0 {
		s = append(s, '0', '.')
		for i := 0; i < -d.exp; i++ {
			s = append(s, '0')
		}
		for _, c := range d.digits {
			s = append(s, '0'+c)
		}
	} else if d.exp >= nd {
		for _, c := range d.digits {
			s = append(s, '0'+c)
		}
		for i := 0; i < d.exp-nd; i++ {
			s = append(s, '0')
		}
	} else {
		for i, c := range d.digits {
			if i == d.exp {
				s = append(s, '.')
			}
			s = append(s, '0'+c)
		}
	}
	return string(s)
}

// jsonFormatFloat formats f as gc's encoding/json does:
// 'f' format normally, 'e' format for abs < 1e-6 or abs >= 1e21,
// with the e-0X -> e-X cleanup. Returns the string (no sign).
// ok=false for Inf/NaN (caller raises UnsupportedValueError).
// jsonFormatFloatPair is the single-result form of jsonFormatFloat (WS D:
// v1 cannot emit multi-result functions).
type jsonFormatFloatPair struct {
	s  string
	ok bool
}

func jsonFormatFloat(f float64) jsonFormatFloatPair {
	if jsonIsNaN(f) || jsonIsInf(f) {
		return jsonFormatFloatPair{"", false}
	}
	neg := f < 0 || jsonIsNegZero(f)
	if neg {
		f = -f
	}
	var out []byte
	if neg {
		out = append(out, '-')
	}
	if f == 0 {
		// -0 handled by caller sign; 0 formats as "0".
		out = append(out, '0')
		return jsonFormatFloatPair{string(out), true}
	}
	jshort := jsonShortest(f)
	return jsonFormatFloatRender(out, jshort.digits, jshort.exp, f < 1e-6 || f >= 1e21)
}

// jsonFormatFloat32 formats a float32 exactly as gc's encoding/json does:
// the shortest decimal that round-trips through float32 (bitSize 32,
// like strconv.FormatFloat(f, 'g'/'f', -1, 32) but in gc's json 'f'/'e'
// style), then rendered like jsonFormatFloat. ok=false for Inf/NaN
// (caller raises UnsupportedValueError).
func jsonFormatFloat32(f float32) jsonFormatFloatPair {
	if jsonIsNaN(float64(f)) || jsonIsInf(float64(f)) {
		return jsonFormatFloatPair{"", false}
	}
	neg := f < 0 || jsonIsNegZero(float64(f))
	if neg {
		f = -f
	}
	var out []byte
	if neg {
		out = append(out, '-')
	}
	if f == 0 {
		out = append(out, '0')
		return jsonFormatFloatPair{string(out), true}
	}
	jshort := jsonShortest32(f)
	// gc decides 'e' vs 'f' with float32-precision comparisons for float32
	// (the 1e-6/1e21 constants convert to float32 in the comparison).
	return jsonFormatFloatRender(out, jshort.digits, jshort.exp, f < float32(1e-6) || f >= float32(1e21))
}

// jsonFormatFloatRender renders the shortest digits/exp of a finite nonzero
// magnitude (sign already emitted into out) in gc's 'f'/'e' style:
// 'e' when useE, else 'f', with the e-0X -> e-X cleanup.
func jsonFormatFloatRender(out []byte, digits []byte, exp int, useE bool) jsonFormatFloatPair {
	if useE {
		// 'e' format: d1[.d2...]e±X, X = exp-1, no leading zeros.
		out = append(out, '0'+digits[0])
		if len(digits) > 1 {
			out = append(out, '.')
			out = append(out, jsonDigitsToBytes(digits[1:])...)
		}
		out = append(out, 'e')
		e10 := exp - 1
		if e10 < 0 {
			out = append(out, '-')
			e10 = -e10
		} else {
			out = append(out, '+')
		}
		out = append(out, jsonItoa(e10)...)
	} else {
		// 'f' format.
		nd := len(digits)
		if exp <= 0 {
			out = append(out, '0', '.')
			for i := 0; i < -exp; i++ {
				out = append(out, '0')
			}
			out = append(out, jsonDigitsToBytes(digits)...)
		} else if exp >= nd {
			out = append(out, jsonDigitsToBytes(digits)...)
			for i := 0; i < exp-nd; i++ {
				out = append(out, '0')
			}
		} else {
			out = append(out, jsonDigitsToBytes(digits[:exp])...)
			out = append(out, '.')
			out = append(out, jsonDigitsToBytes(digits[exp:])...)
		}
	}
	return jsonFormatFloatPair{string(out), true}
}

func jsonDigitsToBytes(d []byte) []byte {
	b := make([]byte, len(d))
	for i, c := range d {
		b[i] = '0' + c
	}
	return b
}

// ============ float32 shortest decimal (for marshal) ============

// jsonDecompose32 splits a finite nonzero float32 f into (m, e) with
// |f| = m * 2^e exactly, m in [2^23, 2^24) for normals. For float32
// subnormals m still lands in [2^23, 2^24) with e <= -150 (scaling by
// powers of two is exact in every case). Mirrors jsonDecompose (which is
// float64-only: float64 decomposition of a subnormal float32 loses the
// 32-bit ulp structure).
func jsonDecompose32(f float32) jsonDecomposePair {
	if f < 0 {
		f = -f
	}
	// f > 0 here.
	e2 := 0
	for f >= 2 {
		f /= 2
		e2++
	}
	for f < 1 {
		f *= 2
		e2--
	}
	// 1 <= f < 2; f * 2^23 is an exact integer in [2^23, 2^24).
	m := uint64(float64(f) * 8388608.0)
	return jsonDecomposePair{m, e2 - 23}
}

// jsonUlp32Pair is the single-result form of jsonUlp32 (WS D:
// v1 cannot emit multi-result functions).
type jsonUlp32Pair struct {
	lo float64
	hi float64
}

// jsonUlp32 returns the float32 ulps adjacent to the finite nonzero
// float32 f: (ulp below f, ulp above f), as exact float64 values.
// Powers of two have a halved ulp below (except 2^-126, the smallest
// normal, whose lower neighbor is subnormal-spaced); subnormals are
// spaced 2^-149 on both sides.
func jsonUlp32(f float32) jsonUlp32Pair {
	jdecomp := jsonDecompose32(f)
	m, e := jdecomp.m, jdecomp.e
	if e <= -150 {
		// Subnormal float32: spacing 2^-149 on both sides. (Covers
		// subnormal powers of two like 2^-149, whose lower neighbor
		// is 0, one ulp below.)
		u := jsonPow2(-149)
		return jsonUlp32Pair{u, u}
	}
	if m == 1<<23 {
		// Normal power of two (2^-126..2^128): ulp below is halved.
		return jsonUlp32Pair{jsonPow2(e - 1), jsonPow2(e)}
	}
	return jsonUlp32Pair{jsonPow2(e), jsonPow2(e)}
}

// jsonIsEvenFloat32 reports whether f's float32 significand LSB is 0
// (for round-half-even tie-breaking at 32-bit precision). Cannot reuse
// jsonIsEvenFloat: the float64 decomposition of a subnormal float32
// always yields an even 52-bit mantissa, hiding the 32-bit LSB.
func jsonIsEvenFloat32(f float32) bool {
	jdecomp := jsonDecompose32(f)
	m, e := jdecomp.m, jdecomp.e
	if e <= -150 {
		// Subnormal float32: integer significand M = m >> (-(e+149)).
		shift := uint(-(e + 149))
		return (m>>shift)%2 == 0
	}
	// Normal: m is the 24-bit significand (hidden bit included).
	return m%2 == 0
}

// jsonShortest32 returns the shortest significant-digit string and decimal
// exponent for a finite nonzero float32 f: value = 0.digits * 10^exp,
// digits has no trailing zeros, and parsing the digits back at 32-bit
// precision (correctly rounded, ties to even) yields exactly f.
// This is what gc's encoding/json uses for float32 (bitSize 32).
func jsonShortest32(f float32) jsonShortestPair {
	// f is finite and nonzero (callers handle 0/Inf/NaN).
	neg := f < 0
	if neg {
		f = -f
	}
	f64 := float64(f)
	exact := jsonDecimalFromFloat(f64)
	// Round-trip interval: the decimals that parse (correctly rounded) to
	// f are those strictly between the midpoints to f's float32 neighbors;
	// an exact midpoint also qualifies when f's significand is even.
	julp := jsonUlp32(f)
	ulo, uhi := julp.lo, julp.hi
	midLo := jsonDecimalFromFloat(f64 - ulo/2)
	midHi := jsonDecimalFromFloat(f64 + uhi/2)
	fEven := jsonIsEvenFloat32(f)
	for k := 1; k <= 9; k++ {
		cand := jsonDecimalRound(exact, k)
		cmpLo := jsonDecimalCompare(cand, midLo)
		cmpHi := jsonDecimalCompare(cand, midHi)
		if cmpLo > 0 && cmpHi < 0 {
			return jsonShortestPair{cand.digits, cand.exp}
		}
		if (cmpLo == 0 || cmpHi == 0) && fEven {
			return jsonShortestPair{cand.digits, cand.exp}
		}
	}
	// 9 digits always suffice for float32; unreachable in practice.
	cand := jsonDecimalRound(exact, 9)
	return jsonShortestPair{cand.digits, cand.exp}
}

func jsonItoa(v int) []byte {
	if v == 0 {
		return []byte{'0'}
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return b
}

// ============ Precomputed powers (for fast exact decimals) ============

// jsonPow5str[k] is 5^k as a decimal string (k = 0..1199).
// jsonPow2str[k] is 2^k as a decimal string (k = 0..971).
// Built once at package init; used by jsonDecimalFromParts to avoid
// O(k^2) repeated multiplication.
var jsonPow5str = jsonBuildPow5()
var jsonPow2str = jsonBuildPow2()

func jsonBuildPow5() []string {
	p := make([]string, 1200)
	p[0] = "1"
	for k := 1; k <= 1199; k++ {
		p[k] = jsonDecimalMulSmall(p[k-1], 5)
	}
	return p
}

func jsonBuildPow2() []string {
	p := make([]string, 972)
	p[0] = "1"
	for k := 1; k <= 971; k++ {
		p[k] = jsonDecimalMulSmall(p[k-1], 2)
	}
	return p
}

// jsonDecimalMulSmall multiplies a decimal string by a small int (<= 10).
func jsonDecimalMulSmall(s string, m int) string {
	b := []byte(s)
	carry := 0
	for i := len(b) - 1; i >= 0; i-- {
		v := int(b[i]-'0')*m + carry
		b[i] = byte(v%10) + '0'
		carry = v / 10
	}
	for carry > 0 {
		b = append([]byte{byte(carry%10) + '0'}, b...)
		carry /= 10
	}
	return string(b)
}

// jsonDecimalMul64 multiplies a decimal string by a uint64.
func jsonDecimalMul64(s string, m uint64) string {
	if m == 0 {
		return "0"
	}
	// Little-endian limbs for the multiply.
	b := []byte(s)
	le := make([]byte, len(b))
	for i := range b {
		le[len(b)-1-i] = b[i] - '0'
	}
	var carry uint64
	for i := range le {
		v := uint64(le[i])*m + carry
		le[i] = byte(v % 10)
		carry = v / 10
	}
	for carry > 0 {
		le = append(le, byte(carry%10))
		carry /= 10
	}
	// Back to string.
	out := make([]byte, len(le))
	for i, c := range le {
		out[len(le)-1-i] = c + '0'
	}
	return string(out)
}

// jsonDecimalFromPartsFast builds the exact decimal for m * 2^e using
// precomputed powers (much faster than repeated multiplication).
func jsonDecimalFromParts(m uint64, e int, neg bool) jsonDecimal {
	if m == 0 {
		return jsonDecimalZero(neg)
	}
	var dstr string
	if e >= 0 {
		// m * 2^e.
		dstr = jsonDecimalMul64(jsonPow2str[e], m)
	} else {
		// m * 5^(-e) / 10^(-e).
		dstr = jsonDecimalMul64(jsonPow5str[-e], m)
	}
	// dstr is the integer D. value = D / 10^k where k = -e (if e<0), else D.
	nd := len(dstr)
	// Strip trailing zeros (keep count for exponent).
	tz := 0
	for nd-tz > 1 && dstr[nd-1-tz] == '0' {
		tz++
	}
	digits := make([]byte, nd-tz)
	for i := 0; i < nd-tz; i++ {
		digits[i] = dstr[i] - '0'
	}
	// Strip leading zeros (shouldn't be any, but be safe).
	j := 0
	for j < len(digits) && digits[j] == 0 {
		j++
	}
	digits = digits[j:]
	if len(digits) == 0 {
		return jsonDecimalZero(neg)
	}
	var exp int
	if e >= 0 {
		// value = D (integer with nd digits). 0.digits * 10^nd.
		exp = nd
	} else {
		// value = D / 10^(-e). 0.digits * 10^(nd - (-e)).
		exp = nd + e // e is negative
	}
	// Adjust for stripped trailing zeros: value unchanged, but our
	// digits are shorter. Actually the exp above uses nd (unstripped).
	// After stripping tz trailing zeros, digits has nd-tz digits, but
	// the value is still 0.digits_stripped * 10^exp (trailing zeros
	// don't affect the 0.digits value? No wait...
	// 0.12300 * 10^exp vs 0.123 * 10^exp: these are DIFFERENT
	// (0.12300 = 0.123, so actually they're the SAME value!).
	// 0.digits as a fraction: trailing zeros don't change it.
	// So exp stays nd (or nd+e), and digits are stripped. Correct.
	_ = tz
	return jsonDecimal{neg: neg, digits: digits, exp: exp}
}

// UTF-8 decode/encode helpers (clean-room; unicode/utf8 is not in
// wave-1 scope). Only the subset needed by the JSON string codec.

const (
	jsonRuneError = 0xFFFD
	jsonRuneSelf  = 0x80
	jsonMaxRune   = 0x10FFFF
	jsonSurrogateMin = 0xD800
	jsonSurrogateMax = 0xDFFF
)

// jsonDecodeRune decodes the UTF-8 sequence starting at s[i], returning
// the rune value and its size in bytes. Invalid sequences yield
// (jsonRuneError, 1), exactly like utf8.DecodeRuneInString.
// jsonDecodeRunePair is the single-result form of jsonDecodeRune (WS D:
// v1 cannot emit multi-result functions).
type jsonDecodeRunePair struct {
	r    int
	size int
}

func jsonDecodeRune(s string, i int) jsonDecodeRunePair {
	b0 := s[i]
	if b0 < jsonRuneSelf {
		return jsonDecodeRunePair{int(b0), 1}
	}
	// Multi-byte.
	if b0 < 0xC2 {
		// 0x80-0xC1: continuation or overlong.
		return jsonDecodeRunePair{jsonRuneError, 1}
	}
	if b0 < 0xE0 {
		// 2-byte: C2-DF.
		if i+1 >= len(s) {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		b1 := s[i+1]
		if b1 < 0x80 || b1 >= 0xC0 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		return jsonDecodeRunePair{int(b0&0x1F)<<6 | int(b1&0x3F), 2}
	}
	if b0 < 0xF0 {
		// 3-byte: E0-EF.
		if i+2 >= len(s) {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		b1, b2 := s[i+1], s[i+2]
		if b1 < 0x80 || b1 >= 0xC0 || b2 < 0x80 || b2 >= 0xC0 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		// Overlong / surrogate checks.
		if b0 == 0xE0 && b1 < 0xA0 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		if b0 == 0xED && b1 >= 0xA0 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		return jsonDecodeRunePair{int(b0&0x0F)<<12 | int(b1&0x3F)<<6 | int(b2&0x3F), 3}
	}
	if b0 < 0xF5 {
		// 4-byte: F0-F4.
		if i+3 >= len(s) {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		b1, b2, b3 := s[i+1], s[i+2], s[i+3]
		if b1 < 0x80 || b1 >= 0xC0 || b2 < 0x80 || b2 >= 0xC0 || b3 < 0x80 || b3 >= 0xC0 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		if b0 == 0xF0 && b1 < 0x90 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		if b0 == 0xF4 && b1 >= 0x90 {
			return jsonDecodeRunePair{jsonRuneError, 1}
		}
		return jsonDecodeRunePair{int(b0&0x07)<<18 | int(b1&0x3F)<<12 | int(b2&0x3F)<<6 | int(b3&0x3F), 4}
	}
	return jsonDecodeRunePair{jsonRuneError, 1}
}

// jsonEncodeRune appends the UTF-8 encoding of r to b.
func jsonEncodeRune(b []byte, r int) []byte {
	if r < jsonRuneSelf {
		return append(b, byte(r))
	}
	if r < 0x800 {
		return append(b, byte(0xC0|r>>6), byte(0x80|r&0x3F))
	}
	if r < 0x10000 {
		return append(b, byte(0xE0|r>>12), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	}
	return append(b, byte(0xF0|r>>18), byte(0x80|r>>12&0x3F), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
}

// jsonHexDigits for \u00XX escapes.
const jsonHexDigits = "0123456789abcdef"

// jsonSafeByte reports whether b can appear unescaped in a JSON string
// when escapeHTML is false (gc's safeSet).
func jsonSafeByte(b byte) bool {
	return b >= 0x20 && b != '"' && b != '\\'
}

// jsonHTMLSafeByte reports whether b can appear unescaped when
// escapeHTML is true (gc's htmlSafeSet).
func jsonHTMLSafeByte(b byte) bool {
	return jsonSafeByte(b) && b != '<' && b != '>' && b != '&'
}

// jsonAppendString appends s as a JSON string literal (with quotes) to
// dst, exactly like gc's appendString (escapeHTML controls <, >, &).
func jsonAppendString(dst []byte, s string, escapeHTML bool) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < jsonRuneSelf {
			var safe bool
			if escapeHTML {
				safe = jsonHTMLSafeByte(b)
			} else {
				safe = jsonSafeByte(b)
			}
			if safe {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				// Control chars and (if escapeHTML) <, >, &.
				dst = append(dst, '\\', 'u', '0', '0', jsonHexDigits[b>>4], jsonHexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		// Multi-byte: decode (up to 4 bytes).
		jdr := jsonDecodeRune(s, i)
		r, size := jdr.r, jdr.size
		if r == jsonRuneError && size == 1 {
			// Invalid UTF-8: emit the literal U+FFFD (EF BF BD), matching
			// gc 1.27.1's empirical behavior (the vendored source says
			// "\ufffd" but the toolchain emits the literal rune).
			dst = append(dst, s[start:i]...)
			dst = append(dst, 0xEF, 0xBF, 0xBD)
			i++
			start = i
			continue
		}
		// U+2028/U+2029 always escaped (even if !escapeHTML).
		if r == 0x2028 || r == 0x2029 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', jsonHexDigits[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	dst = append(dst, '"')
	return dst
}

// Public API types (gc-identical signatures; wave-1 dynamic-model subset).

// SyntaxError describes a JSON syntax error.
type SyntaxError struct {
	msg    string // description of error
	Offset int64  // error occurred after reading Offset bytes
}

func (e *SyntaxError) Error() string { return e.msg }

// UnmarshalTypeError describes a JSON value that was not appropriate for
// a Go value. (Defined for API completeness; the dynamic model rarely
// produces it.)
type UnmarshalTypeError struct {
	Value  string // description of JSON value ("number", "boolean", etc.)
	Type   string // type name in the dynamic model ("any")
	Offset int64  // error occurred after reading Offset bytes
	Struct string
	Field  string
}

func (e *UnmarshalTypeError) Error() string {
	return "json: cannot unmarshal " + e.Value + " into Go value of type " + e.Type
}

// Marshaler is the interface implemented by types that can marshal
// themselves into valid JSON. Honored for the dynamic model via
// RawMessage; named types fail fast (need reflect, P3-M8).
type Marshaler interface {
	MarshalJSON() ([]byte, error)
}

// Unmarshaler is the interface implemented by types that can unmarshal
// a JSON description of themselves. Honored for RawMessage; named types
// fail fast (need reflect, P3-M8).
type Unmarshaler interface {
	UnmarshalJSON([]byte) error
}

// RawMessage is a raw encoded JSON value. It implements Marshaler and
// Unmarshaler and can be used to delay JSON decoding or precompute a
// JSON encoding.
type RawMessage []byte

// MarshalJSON returns m as the JSON encoding of m.
//
// (WS D: desugars to __rawMessageMarshalJSON.)
func (m RawMessage) MarshalJSON() ([]byte, error) {
	panic("json.RawMessage.MarshalJSON: unreachable (call sites desugar to __rawMessageMarshalJSON)")
}

// rawMessageMarshalJSONPair is the single-result form of
// RawMessage.MarshalJSON (WS D).
type rawMessageMarshalJSONPair struct {
	data []byte
	err  error
}

// __rawMessageMarshalJSON is the single-result RawMessage.MarshalJSON helper.
func __rawMessageMarshalJSON(m RawMessage) rawMessageMarshalJSONPair {
	// gc: empty RawMessage.MarshalJSON returns empty bytes (not "null")
	// with nil error. (json.Marshal then fails compacting it, but the
	// method itself succeeds.)
	if len(m) == 0 {
		return rawMessageMarshalJSONPair{[]byte{}, nil}
	}
	return rawMessageMarshalJSONPair{m, nil}
}

// UnmarshalJSON sets *m to a copy of data.
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. Writing through
// m needs store-through-pointer (*m = ...), which frontend/v1 cannot
// emit (WS D). Re-test when WS D lands.
func (m *RawMessage) UnmarshalJSON(data []byte) error {
	panic("json.RawMessage.UnmarshalJSON: not in M7 scope (needs store-through-pointer; re-test when WS D lands)")
}

// Compact is omitted in wave 1 (needs *bytes.Buffer, inexpressible);
// re-test when synthetic bytes lands.

// InvalidUnmarshalError describes an invalid argument passed to Unmarshal.
type InvalidUnmarshalError struct {
	Type string
}

func (e *InvalidUnmarshalError) Error() string {
	return "json: Unmarshal(nil " + e.Type + ")"
}

// Number represents a JSON number literal.
type Number string

// String returns the literal text of the number.
func (n Number) String() string { return string(n) }

// Float64 returns the number as a float64.
//
// (WS D: desugars to __numberFloat64.)
func (n Number) Float64() (float64, error) {
	panic("json.Number.Float64: unreachable (call sites desugar to __numberFloat64)")
}

// numberFloat64Pair is the single-result form of Number.Float64 (WS D).
type numberFloat64Pair struct {
	f   float64
	err error
}

// __numberFloat64 is the single-result Number.Float64 helper.
func __numberFloat64(n Number) numberFloat64Pair {
	s := string(n)
	// gc: invalid number literal -> syntax error (not a zero value).
	// The error is a *strconv.NumError via ParseFloat; here we return a
	// *SyntaxError (the program only checks err != nil).
	if !jsonValidNumber(s) {
		return numberFloat64Pair{0, &SyntaxError{msg: "invalid number literal " + s}}
	}
	return numberFloat64Pair{jsonParseFloat(s), nil}
}

// Int64 returns the number as an int64.
//
// (WS D: desugars to __numberInt64.)
func (n Number) Int64() (int64, error) {
	panic("json.Number.Int64: unreachable (call sites desugar to __numberInt64)")
}

// numberInt64Pair is the single-result form of Number.Int64 (WS D).
type numberInt64Pair struct {
	i   int64
	err error
}

// __numberInt64 is the single-result Number.Int64 helper.
func __numberInt64(n Number) numberInt64Pair {
	// Parse as integer; on failure or overflow, return error like gc.
	s := string(n)
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	} else if len(s) > 0 && s[0] == '+' {
		s = s[1:]
	}
	// Must be all digits (no fraction/exponent) for Int64.
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return numberInt64Pair{0, &jsonNumError{s: string(n)}}
		}
	}
	// Strip leading zeros.
	j := 0
	for j < len(s) && s[j] == '0' {
		j++
	}
	s = s[j:]
	if s == "" {
		return numberInt64Pair{0, nil}
	}
	// Accumulate with overflow check.
	var v uint64
	for i := 0; i < len(s); i++ {
		d := uint64(s[i] - '0')
		if v > (1<<64-1-d)/10 {
			return numberInt64Pair{0, &jsonNumError{s: string(n)}}
		}
		v = v*10 + d
	}
	if neg {
		if v > 1<<63 {
			return numberInt64Pair{0, &jsonNumError{s: string(n)}}
		}
		if v == 1<<63 {
			return numberInt64Pair{-1 << 63, nil}
		}
		return numberInt64Pair{-int64(v), nil}
	}
	if v > 1<<63-1 {
		return numberInt64Pair{0, &jsonNumError{s: string(n)}}
	}
	return numberInt64Pair{int64(v), nil}
}

type jsonNumError struct {
	s string
}

func (e *jsonNumError) Error() string {
	return "json: invalid number literal \"" + e.s + "\" trying to unmarshal into Number"
}

// Delim is a JSON array or object delimiter.
type Delim rune

func (d Delim) String() string { return string(d) }

// Token holds a value of one of these types: Delim, bool, float64,
// Number, string. (nil is not a valid Token; use nil interface.)
type Token any

// jsonReader is the structural reader interface for NewDecoder.
// (Documented divergence: gc takes io.Reader; the corpus defines local
// types satisfying both this and io.Reader.)
type jsonReader interface {
	Read(p []byte) (n int, err error)
}

// jsonWriter is the structural writer interface for NewEncoder.
type jsonWriter interface {
	Write(p []byte) (n int, err error)
}

// Recursive-descent JSON parser with gc-identical SyntaxError messages.
// Validates and builds the any value tree in one pass.

// jsonMaxNestingDepth matches gc's maxNestingDepth.
const jsonMaxNestingDepth = 10000

// jsonParser parses JSON from data.
type jsonParser struct {
	data      []byte
	off       int
	depth     int
	useNumber bool
}

// jsonSyntaxError creates a SyntaxError with the gc message format.
func jsonSyntaxError(data []byte, off int, c byte, context string) *SyntaxError {
	return &SyntaxError{
		msg:    "invalid character " + jsonQuoteChar(c) + " " + context,
		Offset: int64(off + 1),
	}
}

// jsonQuoteChar formats a byte as gc's quoteChar does.
func jsonQuoteChar(c byte) string {
	if c == '\'' {
		return "'\\''"
	}
	if c == '"' {
		return "'\"'"
	}
	var inner string
	switch c {
	case '\a':
		inner = "\\a"
	case '\b':
		inner = "\\b"
	case '\f':
		inner = "\\f"
	case '\n':
		inner = "\\n"
	case '\r':
		inner = "\\r"
	case '\t':
		inner = "\\t"
	case '\v':
		inner = "\\v"
	case '\\':
		inner = "\\\\"
	default:
		if c >= 0x20 && c < 0x7f {
			inner = string([]byte{c})
		} else {
			inner = "\\x" + string([]byte{jsonHexDigits[c>>4], jsonHexDigits[c&0xF]})
		}
	}
	return "'" + inner + "'"
}

func jsonIsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func (p *jsonParser) skipSpace() {
	for p.off < len(p.data) && jsonIsSpace(p.data[p.off]) {
		p.off++
	}
}

// parseValue parses a JSON value at the current offset.
// jsonValuePair is the single-result form of the jsonParser value
// methods (WS D: v1 cannot emit multi-result functions). All of
// parseValue/parseObject/parseArray/parseNumber/parseTrue/parseFalse/
// parseNull/parseLiteralRest return it, so "return p.parseX()"
// delegation sites are unchanged.
type jsonValuePair struct {
	v   any
	err error
}

// jsonStringPair is the single-result form of parseString (WS D).
type jsonStringPair struct {
	s   string
	err error
}

func (p *jsonParser) parseValue() jsonValuePair {
	p.skipSpace()
	if p.off >= len(p.data) {
		return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
	}
	c := p.data[p.off]
	switch c {
	case '{':
		return p.parseObject()
	case '[':
		return p.parseArray()
	case '"':
		sp := p.parseString()
		return jsonValuePair{sp.s, sp.err}
	case 't':
		return p.parseTrue()
	case 'f':
		return p.parseFalse()
	case 'n':
		return p.parseNull()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return p.parseNumber()
	}
	return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, c, "looking for beginning of value")}
}

func (p *jsonParser) parseObject() jsonValuePair {
	// Current char is '{'.
	p.depth++
	if p.depth > jsonMaxNestingDepth {
		p.depth--
		return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, '{', "exceeded max depth")}
	}
	defer func() { p.depth-- }()
	p.off++ // consume '{'
	obj := make(map[string]any)
	p.skipSpace()
	if p.off < len(p.data) && p.data[p.off] == '}' {
		p.off++
		return jsonValuePair{obj, nil}
	}
	for {
		p.skipSpace()
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		if p.data[p.off] != '"' {
			return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, p.data[p.off], "looking for beginning of object key string")}
		}
		ksp := p.parseString()
		key, err := ksp.s, ksp.err
		if err != nil {
			return jsonValuePair{nil, err}
		}
		p.skipSpace()
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		if p.data[p.off] != ':' {
			return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, p.data[p.off], "after object key")}
		}
		p.off++ // consume ':'
		vp := p.parseValue()
		val, err := vp.v, vp.err
		if err != nil {
			return jsonValuePair{nil, err}
		}
		obj[key] = val
		p.skipSpace()
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		c := p.data[p.off]
		if c == ',' {
			p.off++
			continue
		}
		if c == '}' {
			p.off++
			return jsonValuePair{obj, nil}
		}
		return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, c, "after object key:value pair")}
	}
}

func (p *jsonParser) parseArray() jsonValuePair {
	// Current char is '['.
	p.depth++
	if p.depth > jsonMaxNestingDepth {
		p.depth--
		return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, '[', "exceeded max depth")}
	}
	defer func() { p.depth-- }()
	p.off++ // consume '['
	var arr []any
	p.skipSpace()
	if p.off < len(p.data) && p.data[p.off] == ']' {
		p.off++
		return jsonValuePair{arr, nil}
	}
	for {
		vp := p.parseValue()
		val, err := vp.v, vp.err
		if err != nil {
			return jsonValuePair{nil, err}
		}
		arr = append(arr, val)
		p.skipSpace()
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		c := p.data[p.off]
		if c == ',' {
			p.off++
			continue
		}
		if c == ']' {
			p.off++
			return jsonValuePair{arr, nil}
		}
		return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, c, "after array element")}
	}
}

// parseString parses a JSON string literal (current char is '"'),
// returning the decoded Go string.
func (p *jsonParser) parseString() jsonStringPair {
	// Current char is '"'.
	p.off++ // consume opening quote
	var out []byte
	for {
		if p.off >= len(p.data) {
			return jsonStringPair{"", &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		c := p.data[p.off]
		if c == '"' {
			p.off++
			return jsonStringPair{string(out), nil}
		}
		if c == '\\' {
			// Escape sequence. Use v2-style errors.
			ep := p.parseEscape(out)
			out = ep.out
			if ep.err != nil {
				return jsonStringPair{"", ep.err}
			}
			continue
		}
		if c < 0x20 {
			// v2: "invalid character '\x01' in string" (not "in string literal").
			return jsonStringPair{"", &SyntaxError{
				msg:    "invalid character " + jsonQuoteChar(c) + " in string",
				Offset: int64(p.off + 1),
			}}
		}
		// Copy the byte (for multi-byte, copy all bytes of the sequence).
		if c < 0x80 {
			out = append(out, c)
			p.off++
		} else {
			jdr := jsonDecodeRune(string(p.data), p.off)
			r, size := jdr.r, jdr.size
			if r == jsonRuneError && size == 1 {
				// Invalid: replace with U+FFFD.
				out = append(out, 0xEF, 0xBF, 0xBD)
				p.off++
			} else {
				for i := 0; i < size; i++ {
					out = append(out, p.data[p.off+i])
				}
				p.off += size
			}
		}
	}
}

// parseEscape parses a JSON escape sequence (current char is '\').
// Uses v2-style "invalid escape sequence" errors.
// jsonEscapePair is the single-result form of parseEscape /
// parseUnicodeEscapeV2 (WS D: v1 cannot emit multi-result functions,
// and cannot assign through a *[]byte).
type jsonEscapePair struct {
	out []byte
	err error
}

func (p *jsonParser) parseEscape(out []byte) jsonEscapePair {
	// p.off points at '\'.
	start := p.off
	p.off++ // consume '\'
	if p.off >= len(p.data) {
		// Truncated: "\ at end.
		what := string(p.data[start:])
		return jsonEscapePair{out, &SyntaxError{
			msg:    "invalid escape sequence " + jsonBacktick(what) + " in string",
			Offset: int64(start + 1),
		}}
	}
	esc := p.data[p.off]
	p.off++
	switch esc {
	case '"', '\\', '/':
		out = append(out, esc)
		return jsonEscapePair{out, nil}
	case 'b':
		out = append(out, '\b')
		return jsonEscapePair{out, nil}
	case 'f':
		out = append(out, '\f')
		return jsonEscapePair{out, nil}
	case 'n':
		out = append(out, '\n')
		return jsonEscapePair{out, nil}
	case 'r':
		out = append(out, '\r')
		return jsonEscapePair{out, nil}
	case 't':
		out = append(out, '\t')
		return jsonEscapePair{out, nil}
	case 'u':
		return p.parseUnicodeEscapeV2(out, start)
	default:
		// Invalid escape: what is backslash + char (2 bytes).
		what := string(p.data[start:p.off])
		return jsonEscapePair{out, &SyntaxError{
			msg:    "invalid escape sequence " + jsonBacktick(what) + " in string",
			Offset: int64(start + 1),
		}}
	}
}

// parseUnicodeEscapeV2 parses \uXXXX with v2-style errors.
func (p *jsonParser) parseUnicodeEscapeV2(out []byte, start int) jsonEscapePair {
	// p.off is after the 'u'. Need 4 hex digits.
	if p.off+4 > len(p.data) {
		// Truncated: what is from '\' to end of input.
		what := string(p.data[start:])
		return jsonEscapePair{out, &SyntaxError{
			msg:    "invalid escape sequence " + jsonBacktick(what) + " in string",
			Offset: int64(start + 1),
		}}
	}
	var r int
	for i := 0; i < 4; i++ {
		c := p.data[p.off+i]
		if !jsonIsHex(c) {
			// Invalid hex: what is 6 bytes (backslash-u + 4 chars).
			what := string(p.data[start : start+6])
			return jsonEscapePair{out, &SyntaxError{
				msg:    "invalid escape sequence " + jsonBacktick(what) + " in string",
				Offset: int64(start + 1),
			}}
		}
		r = r*16 + jsonHexVal(c)
	}
	p.off += 4
	// Surrogate pair?
	if r >= 0xD800 && r <= 0xDBFF {
		// High surrogate: expect \uDC00-\uDFFF.
		if p.off+6 > len(p.data) || p.data[p.off] != '\\' || p.data[p.off+1] != 'u' {
			// Lone high surrogate: if truncated, what is from start to end.
			// Otherwise, it's replaced with U+FFFD (not an error).
			// Check if it's a truncated surrogate pair.
			if p.off < len(p.data) && p.data[p.off] == '\\' {
				// There's a backslash but not a full \uXXXX.
				// v2: what is from start to end (surrogate pair error if len>6).
				what := string(p.data[start:])
				if len(what) > 6 {
					return jsonEscapePair{out, &SyntaxError{
						msg:    "invalid surrogate pair " + jsonBacktick(what) + " in string",
						Offset: int64(start + 1),
					}}
				}
			}
			// Lone surrogate: replace with U+FFFD.
			out = jsonEncodeRune(out, jsonRuneError)
			return jsonEscapePair{out, nil}
		}
		var r2 int
		for i := 0; i < 4; i++ {
			c := p.data[p.off+2+i]
			if !jsonIsHex(c) {
				what := string(p.data[start : p.off+6])
				return jsonEscapePair{out, &SyntaxError{
					msg:    "invalid surrogate pair " + jsonBacktick(what) + " in string",
					Offset: int64(start + 1),
				}}
			}
			r2 = r2*16 + jsonHexVal(c)
		}
		if r2 < 0xDC00 || r2 > 0xDFFF {
			what := string(p.data[start : p.off+6])
			return jsonEscapePair{out, &SyntaxError{
				msg:    "invalid surrogate pair " + jsonBacktick(what) + " in string",
				Offset: int64(start + 1),
			}}
		}
		p.off += 6
		r = 0x10000 + (r-0xD800)*0x400 + (r2 - 0xDC00)
		out = jsonEncodeRune(out, r)
		return jsonEscapePair{out, nil}
	}
	if r >= 0xDC00 && r <= 0xDFFF {
		// Lone low surrogate: replace with U+FFFD.
		out = jsonEncodeRune(out, jsonRuneError)
		return jsonEscapePair{out, nil}
	}
	out = jsonEncodeRune(out, r)
	return jsonEscapePair{out, nil}
}

// jsonBacktick formats s with backticks (v2 style), escaping if needed.
func jsonBacktick(s string) string {
	// Check if it needs escaping (contains backtick, etc.).
	for i := 0; i < len(s); i++ {
		if s[i] == 0x60 || s[i] < 0x20 || s[i] >= 0x7f {
			// Use quoted form (simplified).
			return jsonQuoteString(s)
		}
	}
	return "\x60" + s + "\x60"
}

// jsonQuoteString quotes a string (minimal, for error messages).
func jsonQuoteString(s string) string {
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			b = append(b, '\\', c)
		case '\n':
			b = append(b, '\\', 'n')
		case '\r':
			b = append(b, '\\', 'r')
		case '\t':
			b = append(b, '\\', 't')
		default:
			if c < 0x20 || c >= 0x7f {
				b = append(b, '\\', 'x', jsonHexDigits[c>>4], jsonHexDigits[c&0xF])
			} else {
				b = append(b, c)
			}
		}
	}
	b = append(b, '"')
	return string(b)
}

func jsonIsHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func jsonHexVal(c byte) int {
	if '0' <= c && c <= '9' {
		return int(c - '0')
	}
	if 'a' <= c && c <= 'f' {
		return int(c-'a') + 10
	}
	return int(c-'A') + 10
}

// parseNumber parses a JSON number literal (current char is '-' or digit),
// returning the literal text.
func (p *jsonParser) parseNumber() jsonValuePair {
	start := p.off
	c := p.data[p.off]
	if c == '-' {
		p.off++
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		c = p.data[p.off]
		if c < '0' || c > '9' {
			return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, c, "in numeric literal")}
		}
	}
	// Integer part.
	if p.data[p.off] == '0' {
		p.off++
	} else {
		// '1'-'9' (already validated it's a digit).
		for p.off < len(p.data) && p.data[p.off] >= '0' && p.data[p.off] <= '9' {
			p.off++
		}
	}
	// Fraction part.
	if p.off < len(p.data) && p.data[p.off] == '.' {
		p.off++
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		if p.data[p.off] < '0' || p.data[p.off] > '9' {
			return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, p.data[p.off], "in numeric literal")}
		}
		for p.off < len(p.data) && p.data[p.off] >= '0' && p.data[p.off] <= '9' {
			p.off++
		}
	}
	// Exponent part.
	if p.off < len(p.data) && (p.data[p.off] == 'e' || p.data[p.off] == 'E') {
		p.off++
		if p.off < len(p.data) && (p.data[p.off] == '+' || p.data[p.off] == '-') {
			p.off++
		}
		if p.off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		if p.data[p.off] < '0' || p.data[p.off] > '9' {
			return jsonValuePair{nil, jsonSyntaxError(p.data, p.off, p.data[p.off], "in numeric literal")}
		}
		for p.off < len(p.data) && p.data[p.off] >= '0' && p.data[p.off] <= '9' {
			p.off++
		}
	}
	lit := string(p.data[start:p.off])
	if p.useNumber {
		return jsonValuePair{Number(lit), nil}
	}
	return jsonValuePair{jsonParseFloat(lit), nil}
}

func (p *jsonParser) parseTrue() jsonValuePair {
	// Current char is 't'. Expect "rue".
	return p.parseLiteralRest("true", "rue")
}

func (p *jsonParser) parseFalse() jsonValuePair {
	// Current char is 'f'. Expect "alse".
	lrest := p.parseLiteralRest("false", "alse")
	v, err := lrest.v, lrest.err
	if err != nil {
		return jsonValuePair{nil, err}
	}
	return jsonValuePair{v, nil}
}

func (p *jsonParser) parseNull() jsonValuePair {
	// Current char is 'n'. Expect "ull".
	lrest := p.parseLiteralRest("null", "ull")
	v, err := lrest.v, lrest.err
	if err != nil {
		return jsonValuePair{nil, err}
	}
	return jsonValuePair{v, nil}
}

// parseLiteralRest parses the rest of a literal (after the first char).
// full is the full literal ("true"), rest is the expected suffix ("rue").
// Returns the value (true/false/nil) or a SyntaxError with the gc message.
func (p *jsonParser) parseLiteralRest(full, rest string) jsonValuePair {
	// p.off points at the first char. We'll check rest[i] at p.off+1+i.
	for i := 0; i < len(rest); i++ {
		off := p.off + 1 + i
		if off >= len(p.data) {
			return jsonValuePair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: int64(len(p.data))}}
		}
		c := p.data[off]
		if c != rest[i] {
			// gc message: "in literal true (expecting 'r')" etc.
			// The "expecting" char is rest[i].
			return jsonValuePair{nil, &SyntaxError{
				msg:    "invalid character " + jsonQuoteChar(c) + " in literal " + full + " (expecting '" + string([]byte{rest[i]}) + "')",
				Offset: int64(off + 1),
			}}
		}
	}
	p.off += 1 + len(rest)
	switch full {
	case "true":
		return jsonValuePair{true, nil}
	case "false":
		return jsonValuePair{false, nil}
	}
	return jsonValuePair{nil, nil}
}

// Unmarshal parses JSON-encoded data and stores the result in the value
// pointed to by v. v must be a non-nil pointer to an any (wave-1
// dynamic-model subset); other types panic (need reflect, P3-M8).
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. Writing the
// parsed value through v needs store-through-pointer (*p = val), which
// frontend/v1 cannot emit (WS D); moreover v1 user code cannot produce
// a readable *any (no & on variables, no pointer dereference), so
// Unmarshal has no reachable use in M7 scope. Re-test when WS D lands
// (store-through-pointer + pointer dereference).
func Unmarshal(data []byte, v any) error {
	panic("json.Unmarshal: not in M7 scope (needs store-through-pointer; re-test when WS D lands)")
}

// Marshal returns the JSON encoding of v. v must be nil, bool, string,
// an int/uint type, float64, []any, map[string]any, RawMessage, Number,
// or a nested combination. Named struct types and other concrete types
// panic (need reflect, P3-M8).
//
// Output is byte-identical to gc: map keys are sorted, floats use the
// shortest repr, HTML escaping is on by default.
//
// (WS D: multi-result functions desugar at call sites to the __marshal
// helper; this declaration keeps the exact gc signature for type-checking.)
func Marshal(v any) ([]byte, error) {
	panic("json.Marshal: unreachable (call sites desugar to __marshal)")
}

// marshalPair is the single-result form of Marshal (WS D).
type marshalPair struct {
	data []byte
	err  error
}

// __marshal is the single-result Marshal helper (real implementation).
func __marshal(v any) marshalPair {
	e := &jsonEncoderState{escapeHTML: true}
	if err := e.marshalValue(v); err != nil {
		return marshalPair{nil, err}
	}
	return marshalPair{e.buf, nil}
}

// MarshalIndent is like Marshal but applies Indent to format the output.
//
// (WS D: desugars to __marshalIndent.)
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	panic("json.MarshalIndent: unreachable (call sites desugar to __marshalIndent)")
}

// marshalIndentPair is the single-result form of MarshalIndent (WS D).
type marshalIndentPair struct {
	data []byte
	err  error
}

// __marshalIndent is the single-result MarshalIndent helper.
func __marshalIndent(v any, prefix, indent string) marshalIndentPair {
	e := &jsonEncoderState{escapeHTML: true}
	if err := e.marshalValue(v); err != nil {
		return marshalIndentPair{nil, err}
	}
	return marshalIndentPair{jsonIndent(e.buf, prefix, indent), nil}
}

// jsonEncoderState holds encoder options and the output buffer.
type jsonEncoderState struct {
	buf        []byte
	escapeHTML bool
}

func (e *jsonEncoderState) marshalValue(v any) error {
	switch v := v.(type) {
	case nil:
		e.buf = append(e.buf, "null"...)
	case bool:
		if v {
			e.buf = append(e.buf, "true"...)
		} else {
			e.buf = append(e.buf, "false"...)
		}
	case string:
		e.buf = jsonAppendString(e.buf, v, e.escapeHTML)
	case float64:
		jff := jsonFormatFloat(v)
		s, ok := jff.s, jff.ok
		if !ok {
			return &jsonUnsupportedValueError{v: v, str: "cannot encode non-finite float64"}
		}
		e.buf = append(e.buf, s...)
	case float32:
		// gc formats float32 with the shortest repr that round-trips
		// through float32 (bitSize 32), not the widened float64 value.
		jff := jsonFormatFloat32(v)
		s, ok := jff.s, jff.ok
		if !ok {
			return &jsonUnsupportedValueError{v: v, str: "cannot encode non-finite float32"}
		}
		e.buf = append(e.buf, s...)
	case int:
		e.buf = jsonAppendInt(e.buf, int64(v))
	case int8:
		e.buf = jsonAppendInt(e.buf, int64(v))
	case int16:
		e.buf = jsonAppendInt(e.buf, int64(v))
	case int32:
		e.buf = jsonAppendInt(e.buf, int64(v))
	case int64:
		e.buf = jsonAppendInt(e.buf, v)
	case uint:
		e.buf = jsonAppendUint(e.buf, uint64(v))
	case uint8:
		e.buf = jsonAppendUint(e.buf, uint64(v))
	case uint16:
		e.buf = jsonAppendUint(e.buf, uint64(v))
	case uint32:
		e.buf = jsonAppendUint(e.buf, uint64(v))
	case uint64:
		e.buf = jsonAppendUint(e.buf, v)
	case uintptr:
		e.buf = jsonAppendUint(e.buf, uint64(v))
	case []byte:
		// gc base64-encodes []byte as a JSON string (no reflect
		// involved); a nil slice encodes as null.
		if v == nil {
			e.buf = append(e.buf, "null"...)
		} else {
			e.buf = append(e.buf, '"')
			e.buf = jsonAppendBase64(e.buf, v)
			e.buf = append(e.buf, '"')
		}
	case []any:
		e.buf = append(e.buf, '[')
		for i, elem := range v {
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			if err := e.marshalValue(elem); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, ']')
	case map[string]any:
		e.buf = append(e.buf, '{')
		keys := jsonSortedKeys(v)
		// P3-M7 WORKAROUND: range-over-slice drops the last element in
		// synthetic packages (guest bug; re-test when fixed). Use manual
		// indexing instead.
		for i := 0; i < len(keys); i++ {
			k := keys[i]
			if i > 0 {
				e.buf = append(e.buf, ',')
			}
			e.buf = jsonAppendString(e.buf, k, e.escapeHTML)
			e.buf = append(e.buf, ':')
			if err := e.marshalValue(v[k]); err != nil {
				return err
			}
		}
		e.buf = append(e.buf, '}')
	case RawMessage:
		// RawMessage: compact and append (validates).
		if len(v) == 0 {
			e.buf = append(e.buf, "null"...)
		} else {
			// Validate by parsing.
			parser := &jsonParser{data: []byte(v)}
			jvp := parser.parseValue()
			if _, err := jvp.v, jvp.err; err != nil {
				return err
			}
			parser.skipSpace()
			if parser.off < len(v) {
				return jsonSyntaxError([]byte(v), parser.off, v[parser.off], "after top-level value")
			}
			e.buf = append(e.buf, v...)
		}
	case Number:
		s := string(v)
		if !jsonValidNumber(s) {
			return &jsonUnsupportedValueError{v: v, str: "invalid Number literal " + jsonQuoteChar(0)}
		}
		e.buf = append(e.buf, s...)
	case Marshaler:
		// Marshaler (other than RawMessage): unreachable in M7 scope —
		// implementing Marshaler needs a 2-result MarshalJSON method,
		// which v1 cannot emit (WS D). RawMessage is handled in its
		// own case above.
		panic("json.Marshaler: not in M7 scope (needs 2-result methods; re-test when WS D lands)")
	case complex64:
		return &UnsupportedTypeError{typ: "complex64"}
	case complex128:
		return &UnsupportedTypeError{typ: "complex128"}
	case func():
		return &UnsupportedTypeError{typ: "func()"}
	case chan int:
		return &UnsupportedTypeError{typ: "chan int"}
	default:
		panic("not in M7 scope (needs reflect, P3-M8)")
	}
	return nil
}

// jsonUnsupportedValueError is returned for values that cannot be encoded.
type jsonUnsupportedValueError struct {
	v   any
	str string
}

func (e *jsonUnsupportedValueError) Error() string {
	return "json: error calling MarshalJSON for type " + e.str
}

// Actually gc's UnsupportedValueError:
type UnsupportedValueError struct {
	Str string
}

func (e *UnsupportedValueError) Error() string { return "json: unsupported value: " + e.Str }

// UnsupportedTypeError is gc's error for values of types that cannot be
// encoded (complex, func, chan, ...). gc carries a reflect.Type; without
// reflect (P3-M8) we carry the Go type name string directly.
type UnsupportedTypeError struct {
	typ string
}

func (e *UnsupportedTypeError) Error() string { return "json: unsupported type: " + e.typ }

// jsonAppendInt appends a signed integer.
func jsonAppendInt(b []byte, v int64) []byte {
	if v < 0 {
		b = append(b, '-')
		// Careful with MinInt64.
		if v == -1<<63 {
			return append(b, "9223372036854775808"...)
		}
		v = -v
	}
	return jsonAppendUint(b, uint64(v))
}

// jsonAppendUint appends an unsigned integer.
func jsonAppendUint(b []byte, v uint64) []byte {
	if v == 0 {
		return append(b, '0')
	}
	var tmp [20]byte
	i := len(tmp)
	for v > 0 {
		i--
		tmp[i] = byte(v%10) + '0'
		v /= 10
	}
	return append(b, tmp[i:]...)
}

// jsonBase64Alphabet is the RFC 4648 standard base64 alphabet.
const jsonBase64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// jsonAppendBase64 appends the standard base64 encoding of src to dst
// (with '=' padding), byte-identical to encoding/base64.StdEncoding.
// Clean-room: the synthetic package is self-contained and cannot import
// the synthetic base64 package.
func jsonAppendBase64(dst, src []byte) []byte {
	for i := 0; i < len(src); i += 3 {
		var b [3]byte
		n := copy(b[:], src[i:])
		v := uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2])
		dst = append(dst, jsonBase64Alphabet[(v>>18)&0x3f], jsonBase64Alphabet[(v>>12)&0x3f])
		if n > 1 {
			dst = append(dst, jsonBase64Alphabet[(v>>6)&0x3f])
		} else {
			dst = append(dst, '=')
		}
		if n > 2 {
			dst = append(dst, jsonBase64Alphabet[v&0x3f])
		} else {
			dst = append(dst, '=')
		}
	}
	return dst
}

// jsonSortedKeys returns the sorted keys of m.
func jsonSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Insertion sort (maps are usually small; avoids importing sort).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// jsonValidNumber reports whether s is a valid JSON number literal.
func jsonValidNumber(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[i] == '-' {
		i++
		if i >= len(s) {
			return false
		}
	}
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return false
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return i == len(s)
}

// jsonIndent formats b with prefix/indent (like gc's Indent).
func jsonIndent(b []byte, prefix, indent string) []byte {
	// Simplified: use the parser to rebuild with indentation.
	// For now, a basic implementation that handles the dynamic model.
	// Actually, let me implement it properly via re-encoding.
	var out []byte
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			out = append(out, c)
		case '{', '[':
			out = append(out, c)
			// Check if next non-space is the closer.
			j := i + 1
			for j < len(b) && jsonIsSpace(b[j]) {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				// Empty: don't add newline.
			} else {
				depth++
				out = append(out, '\n')
				out = append(out, prefix...)
				for d := 0; d < depth; d++ {
					out = append(out, indent...)
				}
			}
		case '}', ']':
			// Check if previous was opener (empty).
			// We need to track; simplify: look back.
			// Find previous non-space char.
			j := len(out) - 1
			for j >= 0 && (out[j] == ' ' || out[j] == '\n') {
				j--
			}
			if j >= 0 && (out[j] == '{' || out[j] == '[') {
				// Empty object/array: just append closer (depth was
				// not incremented for empty, so don't decrement).
				out = append(out, c)
			} else {
				depth--
				out = append(out, '\n')
				out = append(out, prefix...)
				for d := 0; d < depth; d++ {
					out = append(out, indent...)
				}
				out = append(out, c)
			}
		case ',':
			out = append(out, c)
			out = append(out, '\n')
			out = append(out, prefix...)
			for d := 0; d < depth; d++ {
				out = append(out, indent...)
			}
		case ':':
			out = append(out, ':', ' ')
		case ' ', '\t', '\n', '\r':
			// Skip whitespace outside strings.
		default:
			out = append(out, c)
		}
	}
	return out
}

// Decoder reads and decodes JSON values from an input stream.
type Decoder struct {
	r         jsonReader
	buf       []byte
	off       int
	useNumber bool
	// For Token(): parsed tree and walk state.
	tree      any
	treeInit  bool
	stack     []jsonTokenFrame
	tokOffset int64
}

type jsonTokenFrame struct {
	container any      // []any or map[string]any
	index     int      // for []any: next index; for map: next key index
	keys      []string // for map[string]any: sorted keys
	phase     int      // for map: 0=emit key, 1=emit value
}

// NewDecoder returns a new decoder that reads from r.
func NewDecoder(r jsonReader) *Decoder {
	return &Decoder{r: r}
}

// UseNumber causes the Decoder to unmarshal a number into Number
// instead of float64.
func (dec *Decoder) UseNumber() {
	dec.useNumber = true
}

// Decode reads the next JSON-encoded value from its input and stores
// it in the value pointed to by v (must be *any in wave-1).
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. Writing the
// parsed value through v needs store-through-pointer (*p = val), which
// frontend/v1 cannot emit (WS D); moreover a *Decoder value is not
// constructible from v1 user code (NewDecoder needs a user-defined
// 2-result Read method), so Decode has no reachable use in M7 scope.
// Re-test when WS D lands.
func (dec *Decoder) Decode(v any) error {
	panic("json.Decoder.Decode: not in M7 scope (needs store-through-pointer; re-test when WS D lands)")
}

// Token returns the next JSON token in the input stream.
// Token returns the next JSON token in the input stream.
//
// (WS D: desugars to __decoderToken.)
func (dec *Decoder) Token() (Token, error) {
	panic("json.Decoder.Token: unreachable (call sites desugar to __decoderToken)")
}

// decoderTokenPair is the single-result form of Decoder.Token (WS D).
type decoderTokenPair struct {
	tok Token
	err error
}

// __decoderToken is the single-result Decoder.Token helper.
func __decoderToken(dec *Decoder) decoderTokenPair {
	if !dec.treeInit {
		// Read all and parse.
		jra := jsonReadAll(dec.r)
		data, err := jra.data, jra.err
		if err != nil {
			return decoderTokenPair{nil, err}
		}
		off := 0
		for off < len(data) && jsonIsSpace(data[off]) {
			off++
		}
		if off >= len(data) {
			return decoderTokenPair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: 0}}
		}
		parser := &jsonParser{data: data[off:], useNumber: dec.useNumber}
		jvp := parser.parseValue()
		val, err := jvp.v, jvp.err
		if err != nil {
			return decoderTokenPair{nil, err}
		}
		dec.tree = val
		dec.treeInit = true
		// Initialize stack with the root.
		dec.stack = []jsonTokenFrame{{container: val, index: -1}}
	}
	// Walk the tree.
	for len(dec.stack) > 0 {
		si := len(dec.stack) - 1
		switch c := dec.stack[si].container.(type) {
		case map[string]any:
			if dec.stack[si].index == -1 {
				// First time: emit '{', init keys.
				dec.stack[si].keys = jsonSortedKeys(c)
				dec.stack[si].index = 0
				dec.stack[si].phase = 0
				return decoderTokenPair{Delim('{'), nil}
			}
			if dec.stack[si].index < len(dec.stack[si].keys) {
				key := dec.stack[si].keys[dec.stack[si].index]
				if dec.stack[si].phase == 0 {
					// Emit key.
					dec.stack[si].phase = 1
					return decoderTokenPair{key, nil}
				}
				// Emit value.
				dec.stack[si].phase = 0
				dec.stack[si].index++
				elem := c[key]
				if _, ok := elem.(map[string]any); ok {
					dec.stack = append(dec.stack, jsonTokenFrame{container: elem, index: -1})
					continue
				}
				if _, ok := elem.([]any); ok {
					dec.stack = append(dec.stack, jsonTokenFrame{container: elem, index: -1})
					continue
				}
				return decoderTokenPair{jsonTokenValue(elem), nil}
			}
			// Done: pop and emit '}'.
			dec.stack = dec.stack[:len(dec.stack)-1]
			return decoderTokenPair{Delim('}'), nil}
		case []any:
			if dec.stack[si].index == -1 {
				dec.stack[si].index = 0
				return decoderTokenPair{Delim('['), nil}
			}
			if dec.stack[si].index < len(c) {
				elem := c[dec.stack[si].index]
				dec.stack[si].index++
				// If elem is a container, push and emit its delim.
				if _, ok := elem.(map[string]any); ok {
					dec.stack = append(dec.stack, jsonTokenFrame{container: elem, index: -1})
					continue
				}
				if _, ok := elem.([]any); ok {
					dec.stack = append(dec.stack, jsonTokenFrame{container: elem, index: -1})
					continue
				}
				return decoderTokenPair{jsonTokenValue(elem), nil}
			}
			dec.stack = dec.stack[:len(dec.stack)-1]
			return decoderTokenPair{Delim(']'), nil}
		default:
			// Scalar root: emit once, then done.
			dec.stack = dec.stack[:len(dec.stack)-1]
			return decoderTokenPair{jsonTokenValue(c), nil}
		}
	}
	return decoderTokenPair{nil, &SyntaxError{msg: "unexpected end of JSON input", Offset: 0}}
}

// jsonTokenValue converts a scalar to a Token.
func jsonTokenValue(v any) Token {
	switch v := v.(type) {
	case nil:
		return nil
	case bool, string, float64, Number:
		return v
	}
	return nil
}

// More reports whether there is another element in the current array
// or object being parsed.
func (dec *Decoder) More() bool {
	if !dec.treeInit || len(dec.stack) == 0 {
		return false
	}
	top := dec.stack[len(dec.stack)-1]
	switch c := top.container.(type) {
	case []any:
		return top.index >= 0 && top.index < len(c)
	case map[string]any:
		if top.index < 0 {
			return false
		}
		// If we're in phase 1 (need to emit value), there's more.
		// If in phase 0, check if there are more keys.
		if top.phase == 1 {
			return true
		}
		return top.index < len(top.keys)
	}
	return false
}

// InputOffset returns the input stream byte offset of the current
// decoder position.
func (dec *Decoder) InputOffset() int64 {
	return dec.tokOffset
}

// Buffered returns a reader of the data remaining in the Decoder's buffer.
//
// M7 scope: fail-fast body. gc returns the bytes already read from the
// underlying reader but not yet consumed by the decoder. The synthetic
// decoder has no unconsumed-buffer state to expose: __decoderToken reads
// the whole input up front via jsonReadAll and walks a parsed tree, so
// "data remaining in the buffer" is not a meaningful quantity in this
// design (the Decoder.buf/off fields are never populated). Implementing
// Buffered faithfully needs the streaming decode path — incremental
// reads through the 2-result Read method plus a consumption cursor —
// which v1 cannot emit (WS D). Re-test when WS D lands.
func (dec *Decoder) Buffered() jsonReader {
	panic("json.Decoder.Buffered: not in M7 scope (needs streaming decode; re-test when WS D lands)")
}

// jsonReadAllPair is the single-result form of jsonReadAll (WS D:
// v1 cannot emit multi-result functions).
type jsonReadAllPair struct {
	data []byte
	err  error
}

// jsonReadAll reads all bytes from r.
//
// M7 scope: fail-fast body. Reading through the jsonReader interface
// needs the 2-result Read method, which v1 cannot emit or dispatch
// (WS D); moreover no jsonReader implementation is constructible from
// v1 user code. Re-test when WS D lands.
func jsonReadAll(r jsonReader) jsonReadAllPair {
	panic("json.jsonReadAll: not in M7 scope (needs 2-result interface methods; re-test when WS D lands)")
}

// Encoder writes JSON values to an output stream.
type Encoder struct {
	w          jsonWriter
	escapeHTML bool
	indent     string
	prefix     string
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w jsonWriter) *Encoder {
	return &Encoder{w: w, escapeHTML: true}
}

// SetEscapeHTML specifies whether problematic HTML characters should be
// escaped inside JSON quoted strings.
func (enc *Encoder) SetEscapeHTML(on bool) {
	enc.escapeHTML = on
}

// SetIndent instructs the encoder to format each subsequent encoded
// value as if indented by the package-level Indent function.
func (enc *Encoder) SetIndent(prefix, indent string) {
	enc.prefix = prefix
	enc.indent = indent
}

// Encode writes the JSON encoding of v to the stream, followed by a
// newline character.
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. Writing through
// the jsonWriter interface needs the 2-result Write method, which v1
// cannot emit or dispatch (WS D); moreover no jsonWriter implementation
// is constructible from v1 user code (NewEncoder needs a user-defined
// 2-result Write method). Re-test when WS D lands.
func (enc *Encoder) Encode(v any) error {
	panic("json.Encoder.Encode: not in M7 scope (needs 2-result interface methods; re-test when WS D lands)")
}

// Valid reports whether data is a valid JSON encoding.
func Valid(data []byte) bool {
	parser := &jsonParser{data: data}
	jvp := parser.parseValue()
	_, err := jvp.v, jvp.err
	if err != nil {
		return false
	}
	parser.skipSpace()
	return parser.off >= len(parser.data)
}

// Indent appends to dst an indented form of the JSON-encoded src.
// Indent appends to dst an indented form of the JSON-encoded src.
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. Calling Indent
// needs &buf (address of a bytes.Buffer variable), which v1 cannot emit
// (WS D), so Indent has no reachable use in M7 scope. Re-test when WS D
// lands.
func Indent(dst *bytes.Buffer, src []byte, prefix, indent string) error {
	panic("json.Indent: not in M7 scope (needs address-of variable; re-test when WS D lands)")
}

// HTMLEscape is omitted in wave 1 (needs *bytes.Buffer, inexpressible);
// re-test when synthetic bytes lands with type Buffer.
`
