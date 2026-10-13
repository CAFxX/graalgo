// P3-M7 (WS G): synthetic `strconv` package.
//
// `syntheticStrconvSource` below is REAL Go source, embedded as text and
// type-checked on demand with the overlay importer (like the P3-M5
// time/context packages in synth_time_context.go and the P3-M7 strings
// package in synth_strings.go). The resulting *types.Package is what
// overlayImporter.Import("strconv") returns, so the user's type-check and
// the synthetic source's own type-check share identical *types.Func /
// *types.TypeName objects; the exporter then emits the synthetic
// declarations' bodies into the same wire document (shared type/object
// tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (verified against
// ~/workspace/graalgo-stdlib/go1.27.1/strconv/*.go and
// ~/workspace/graalgo-stdlib/go1.27.1/internal/strconv/*.go).
// Implementations are ports of gc's algorithms to self-contained pure Go
// with one import: "errors" (already a synthetic package). In particular:
//   - unsafe.Pointer bit casts (internal/strconv/deps.go) are replaced by
//     exact float64 arithmetic (every multiplication is by a power of two,
//     so float64frombits/float32frombits/float64bits/float32bits are
//     bit-exact; verified by the differential corpus).
//   - math/bits (Len64/Mul64/TrailingZeros) is inlined (len64u/mul64u/
//     trailingZerosUint); unicode/utf8 is inlined (decodeRuneInString et
//     al., accept rules identical to unicode/utf8).
//   - The vendored strconv/number.go wrappers delegate to internal/
//     strconv; here both layers are merged into one package. gc's
//     internal/strconv.Error type is renamed to parseError so the merged
//     package does not export strconv.Error (not in gc's public API).
//
// The source uses only frontend/v1-supported constructs (no generics, no
// goto, no fallthrough, no labeled breaks, no range-over-int, no builtin
// min/max, no address-of): it must keep passing `go vet`-level type-check
// AND the exporter's scope rules.
//
// Multi-result functions (WS D): v1 cannot emit or call multi-result
// functions, so the public ParseBool/ParseInt/ParseUint/ParseFloat/
// ParseComplex/Atoi/Unquote/UnquoteChar/QuotedPrefix keep exact gc
// signatures with unreachable panic bodies, and the real logic lives in
// unexported single-result __-helpers returning pair structs. Two/four-
// target call sites desugar via mrdesugar_m5.go (the Appendix M
// __parseDuration precedent); every other position fails fast via
// isMultiResultSynthetic. ALL internal multi-result plumbing is likewise
// converted to pair structs, so the emitted package is single-result
// throughout.
//
// Carried divergence (documented): synthetic nanF64() is 0x7ff8000000000000;
// gc's is 0x7ff8000000000001. Unobservable through strconv's API
// (comparisons, formatting, float32 conversion are payload-insensitive);
// re-test if a bit-exact NaN payload source becomes available.
// The build-tags coupling (M6: bytealg.go/bytealg_bootstrap.go carry
// //go:build !compiler_bootstrap / compiler_bootstrap) is resolved by the
// GG-0023 "carry the linux files" rule: indexByte uses the bootstrap loop
// (bytealg_bootstrap.go), which is the correct non-asm implementation.
package main

// syntheticStrconvSource is the P3-M7 synthetic `strconv` package.
// Exact gc 1.27.1 signatures for the full public API; pure-Go
// implementations with one import ("errors"); fail-fast bodies for the
// multi-result declarations (see above).
const syntheticStrconvSource = `package strconv

import "errors"

// ---------------------------------------------------------------------------
// Bit-cast layer (replaces internal/strconv/deps.go's unsafe.Pointer).
//
// float64frombits/float32frombits assemble the value from sign, exponent
// and mantissa using exact arithmetic: float64(m) is exact for m < 2^53,
// and multiplication by an exact power of two only shifts the exponent,
// so the result is bit-exact. Subnormal products are exact multiples of
// 2^-1074 below 2^-1022, hence exactly representable.
// float64bits/float32bits decompose by exact power-of-two scaling.
// Inf/NaN come from IEEE division (verified bit-identical vs gc on the
// graalgo backend: 1/0=+Inf, 0/0=NaN, 1/-0.0=-Inf).
// ---------------------------------------------------------------------------

// pow2f64 returns 2**e as a float64, exact for -1074 <= e <= 1023.
func pow2f64(e int) float64 {
	p := 1.0
	if e >= 0 {
		for i := 0; i < e; i++ {
			p *= 2
		}
		return p
	}
	for i := 0; i < -e; i++ {
		p /= 2
	}
	return p
}

// pow2f32 returns 2**e as a float32, exact for -149 <= e <= 127.
func pow2f32(e int) float32 {
	p := float32(1)
	if e >= 0 {
		for i := 0; i < e; i++ {
			p *= 2
		}
		return p
	}
	for i := 0; i < -e; i++ {
		p /= 2
	}
	return p
}

func float64frombits(b uint64) float64 {
	s := 1.0
	if b&(1<<63) != 0 {
		s = -1
	}
	exp := int(b>>52) & 0x7ff
	mant := b & (uint64(1)<<52 - 1)
	switch {
	case exp == 0x7ff:
		if mant == 0 {
			return s * infF64()
		}
		return nanF64()
	case exp == 0:
		// Subnormal (or zero): mant * 2^-1074, exact.
		return s * float64(mant) * pow2f64(-1074)
	default:
		// Normal: (2^52 | mant) * 2^(exp-1075), exact.
		return s * float64((uint64(1)<<52)|mant) * pow2f64(exp-1075)
	}
}

func float32frombits(b uint32) float32 {
	s := float32(1)
	if b&(1<<31) != 0 {
		s = -1
	}
	exp := int(b>>23) & 0xff
	mant := b & (uint32(1)<<23 - 1)
	switch {
	case exp == 0xff:
		if mant == 0 {
			return s * float32(infF64())
		}
		return float32(nanF64())
	case exp == 0:
		// Subnormal (or zero): mant * 2^-149, exact.
		return s * float32(mant) * pow2f32(-149)
	default:
		// Normal: (2^23 | mant) * 2^(exp-150), exact.
		return s * float32((uint32(1)<<23)|mant) * pow2f32(exp-150)
	}
}

func float64bits(f float64) uint64 {
	if f != f {
		// NaN: canonical payload. Downstream only tests exp==max and
		// mant!=0 (ftoa's NaN check), so any nonzero payload is fine.
		return 0x7ff8000000000001
	}
	neg := false
	if f < 0 {
		neg = true
	} else if f == 0 {
		// Distinguish +0.0 from -0.0: 1/-0.0 = -Inf.
		var z float64
		_ = z
		if 1/f < 0 {
			return 1 << 63
		}
		return 0
	}
	a := f
	if neg {
		a = -a
	}
	if a > 1.7976931348623157e308 {
		// +Inf (NaN excluded above; nothing finite exceeds maxFloat64).
		if neg {
			return 0xfff0000000000000
		}
		return 0x7ff0000000000000
	}
	if a == 0 {
		if neg {
			return 1 << 63
		}
		return 0
	}
	// Scale a into [1, 2) by exact powers of two.
	e := 0
	for a >= 2 {
		a /= 2
		e++
	}
	for a < 1 {
		a *= 2
		e--
	}
	// Now f = a * 2^e with a in [1,2).
	var b uint64
	if e >= -1022 {
		// Normal: (a-1)*2^52 is the exact integer mantissa.
		mant := uint64((a - 1) * 4503599627370496) // 2^52
		b = uint64(e+1023)<<52 | mant
	} else {
		// Subnormal: a * 2^(e+1074) is the exact integer mantissa.
		mant := uint64(a * pow2f64(e+1074))
		b = mant
	}
	if neg {
		b |= 1 << 63
	}
	return b
}

func float32bits(f float32) uint32 {
	if f != f {
		return 0x7fc00001
	}
	neg := false
	if f < 0 {
		neg = true
	} else if f == 0 {
		var z float32
		_ = z
		if 1/f < 0 {
			return 1 << 31
		}
		return 0
	}
	a := f
	if neg {
		a = -a
	}
	if a > 3.4028234663852886e38 {
		if neg {
			return 0xff800000
		}
		return 0x7f800000
	}
	if a == 0 {
		if neg {
			return 1 << 31
		}
		return 0
	}
	e := 0
	for a >= 2 {
		a /= 2
		e++
	}
	for a < 1 {
		a *= 2
		e--
	}
	var b uint32
	if e >= -126 {
		mant := uint32((a - 1) * 8388608) // 2^23
		b = uint32(e+127)<<23 | mant
	} else {
		mant := uint32(a * pow2f32(e+149))
		b = mant
	}
	if neg {
		b |= 1 << 31
	}
	return b
}

func infF64() float64 {
	var z float64
	return 1 / z
}

func infF(sign int) float64 {
	var z float64
	if sign >= 0 {
		return 1 / z
	}
	return -1 / z
}

func isNaNF64(f float64) bool { return f != f }

func nanF64() float64 {
	var z float64
	// Negating flips the sign bit even of a NaN (pure bit flip), giving a
	// deterministic positive quiet NaN (0x7ff8000000000000). gc's nan() is
	// 0x7ff8000000000001; the 1-bit payload difference is unobservable
	// through strconv's API (comparisons, formatting, float32 conversion
	// are all payload-insensitive) -- documented carried divergence.
	return -(z / z)
}

// ---------------------------------------------------------------------------
// Integer parsing (port of internal/strconv/atoi.go).
//
// The internal error type is renamed from Error to parseError: merging it
// under its gc name would export strconv.Error, which is not in gc's
// public API. Public ParseInt/ParseUint/Atoi keep exact gc signatures with
// unreachable panic bodies; the real implementations are the single-result
// __parseInt/__parseUint/__atoi helpers returning pair structs (WS D;
// the Appendix M __parseDuration precedent).
// ---------------------------------------------------------------------------

// lower(c) is a lower-case letter if and only if
// c is either that lower-case letter or the equivalent upper-case letter.
// Instead of writing c == 'x' || c == 'X' one can write lower(c) == 'x'.
// Note that lower of non-letters can produce other non-letters.
func lower(c byte) byte {
	return c | ('x' - 'X')
}

// parseError is the internal parse failure code (gc's internal/strconv.Error,
// renamed so the merged package does not export strconv.Error).
type parseError int

const (
	_ parseError = iota
	errRange
	errSyntax
	errBase
	errBitSize
)

// errRangeError is errRange as an error interface, for comparisons against
// error-typed values (v1 cannot build interface conversions in binary ops).
var errRangeError error = errRange

func (e parseError) Error() string {
	switch e {
	case errRange:
		return "value out of range"
	case errSyntax:
		return "invalid syntax"
	case errBase:
		return "invalid base"
	case errBitSize:
		return "invalid bit size"
	}
	return "unknown error"
}

const intSize = 32 << (^uint(0) >> 63)

// IntSize is the size in bits of an int or uint value.
const IntSize = intSize

// parseUintPair is the single-result form of ParseUint (WS D).
type parseUintPair struct {
	v   uint64
	err error
}

// __parseUint is the real ParseUint implementation (single-result).
func __parseUint(s string, base int, bitSize int) parseUintPair {
	if s == "" {
		return parseUintPair{0, errSyntax}
	}

	base0 := base == 0

	s0 := s
	switch {
	case 2 <= base && base <= 36:
		// valid base; nothing to do

	case base == 0:
		// Look for octal, hex prefix.
		base = 10
		if s[0] == '0' {
			switch {
			case len(s) >= 3 && lower(s[1]) == 'b':
				base = 2
				s = s[2:]
			case len(s) >= 3 && lower(s[1]) == 'o':
				base = 8
				s = s[2:]
			case len(s) >= 3 && lower(s[1]) == 'x':
				base = 16
				s = s[2:]
			default:
				base = 8
				s = s[1:]
			}
		}

	default:
		return parseUintPair{0, errBase}
	}

	if bitSize == 0 {
		bitSize = IntSize
	} else if bitSize < 0 || bitSize > 64 {
		return parseUintPair{0, errBitSize}
	}

	// Cutoff is the smallest number such that cutoff*base > maxUint64.
	// Use precomputed values for common cases (v1 has no const
	// declarations inside function bodies).
	var cutoff uint64
	switch base {
	case 10:
		cutoff = 1844674407370955161 + 1
	case 16:
		cutoff = 1152921504606846975 + 1
	default:
		cutoff = 18446744073709551615/uint64(base) + 1
	}

	maxVal := uint64(1)<<uint(bitSize) - 1

	underscores := false
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d byte
		switch {
		case c == '_' && base0:
			underscores = true
			continue
		case '0' <= c && c <= '9':
			d = c - '0'
		case 'a' <= lower(c) && lower(c) <= 'z':
			d = lower(c) - 'a' + 10
		default:
			return parseUintPair{0, errSyntax}
		}

		if d >= byte(base) {
			return parseUintPair{0, errSyntax}
		}

		if n >= cutoff {
			// n*base overflows
			return parseUintPair{maxVal, errRange}
		}
		n *= uint64(base)

		n1 := n + uint64(d)
		if n1 < n || n1 > maxVal {
			// n+d overflows
			return parseUintPair{maxVal, errRange}
		}
		n = n1
	}

	if underscores && !underscoreOK(s0) {
		return parseUintPair{0, errSyntax}
	}

	return parseUintPair{n, nil}
}

// parseIntPair is the single-result form of ParseInt (WS D).
type parseIntPair struct {
	v   int64
	err error
}

// __parseInt is the real ParseInt implementation (single-result).
func __parseInt(s string, base int, bitSize int) parseIntPair {
	if s == "" {
		return parseIntPair{0, errSyntax}
	}

	// Pick off leading sign.
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		s = s[1:]
		neg = true
	}

	// Convert unsigned and check range.
	pu := __parseUint(s, base, bitSize)
	un, err := pu.v, pu.err
	if err != nil && err != errRangeError {
		return parseIntPair{0, err}
	}

	if bitSize == 0 {
		bitSize = IntSize
	}

	cutoff := uint64(1 << uint(bitSize-1))
	if !neg && un >= cutoff {
		return parseIntPair{int64(cutoff - 1), errRange}
	}
	if neg && un > cutoff {
		return parseIntPair{-int64(cutoff), errRange}
	}
	n := int64(un)
	if neg {
		n = -n
	}
	return parseIntPair{n, nil}
}

// atoiPair is the single-result form of Atoi (WS D).
type atoiPair struct {
	v   int
	err error
}

// __atoi is the real Atoi implementation (single-result).
func __atoi(s string) atoiPair {
	sLen := len(s)
	if intSize == 32 && (0 < sLen && sLen < 10) ||
		intSize == 64 && (0 < sLen && sLen < 19) {
		// Fast path for small integers that fit int type.
		s0 := s
		if s[0] == '-' || s[0] == '+' {
			s = s[1:]
			if len(s) < 1 {
				return atoiPair{0, errSyntax}
			}
		}

		n := 0
		for i := 0; i < len(s); i++ {
			ch := s[i] - '0'
			if ch > 9 {
				return atoiPair{0, errSyntax}
			}
			n = n*10 + int(ch)
		}
		if s0[0] == '-' {
			n = -n
		}
		return atoiPair{n, nil}
	}

	// Slow path for invalid, big, or underscored integers.
	pi := __parseInt(s, 10, 0)
	return atoiPair{int(pi.v), pi.err}
}

// underscoreOK reports whether the underscores in s are allowed.
// Checking them in this one function lets all the parsers skip over them simply.
// Underscore must appear only between digits or between a base prefix and a digit.
func underscoreOK(s string) bool {
	// saw tracks the last character (class) we saw:
	// ^ for beginning of number,
	// 0 for a digit or base prefix,
	// _ for an underscore,
	// ! for none of the above.
	saw := '^'
	i := 0

	// Optional sign.
	if len(s) >= 1 && (s[0] == '-' || s[0] == '+') {
		s = s[1:]
	}

	// Optional base prefix.
	hex := false
	if len(s) >= 2 && s[0] == '0' && (lower(s[1]) == 'b' || lower(s[1]) == 'o' || lower(s[1]) == 'x') {
		i = 2
		saw = '0' // base prefix counts as a digit for "underscore as digit separator"
		hex = lower(s[1]) == 'x'
	}

	// Number proper.
	for ; i < len(s); i++ {
		// Digits are always okay.
		if '0' <= s[i] && s[i] <= '9' || hex && 'a' <= lower(s[i]) && lower(s[i]) <= 'f' {
			saw = '0'
			continue
		}
		// Underscore must follow digit.
		if s[i] == '_' {
			if saw != '0' {
				return false
			}
			saw = '_'
			continue
		}
		// Underscore must also be followed by digit.
		if saw == '_' {
			return false
		}
		// Saw non-digit, non-underscore.
		saw = '!'
	}
	return saw != '_'
}

// ---------------------------------------------------------------------------
// Integer formatting (port of internal/strconv/itoa.go).
//
// bits.TrailingZeros -> trailingZerosUint. formatBits keeps gc's shape but
// returns a pair struct (WS D). RuntimeFormatBase10 is dropped (runtime-only,
// unused in this package).
// ---------------------------------------------------------------------------

// FormatUint returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func FormatUint(i uint64, base int) string {
	if base == 10 {
		if i < nSmalls {
			return small(int(i))
		}
		var a [24]byte
		nd := numDigits(i)
		formatBase10(a[:nd], i)
		return string(a[:nd])
	}
	r := formatBits(i, base, false)
	return r.s
}

// FormatInt returns the string representation of i in the given base,
// for 2 <= base <= 36. The result uses the lower-case letters 'a' to 'z'
// for digit values >= 10.
func FormatInt(i int64, base int) string {
	if base == 10 {
		if 0 <= i && i < nSmalls {
			return small(int(i))
		}
		var a [24]byte
		u := uint64(i)
		if i < 0 {
			u = -u
		}
		nd := numDigits(u)
		formatBase10(a[1:1+nd], u)
		if i < 0 {
			a[0] = '-'
			return string(a[:1+nd])
		}
		return string(a[1 : 1+nd])
	}
	r := formatBits(uint64(i), base, i < 0)
	return r.s
}

// Itoa is equivalent to [FormatInt](int64(i), 10).
func Itoa(i int) string {
	return FormatInt(int64(i), 10)
}

// AppendInt appends the string form of the integer i,
// as generated by [FormatInt], to dst and returns the extended buffer.
func AppendInt(dst []byte, i int64, base int) []byte {
	u := uint64(i)
	if i < 0 {
		dst = append(dst, '-')
		u = -u
	}
	return AppendUint(dst, u, base)
}

// AppendUint appends the string form of the unsigned integer i,
// as generated by [FormatUint], to dst and returns the extended buffer.
func AppendUint(dst []byte, i uint64, base int) []byte {
	if base == 10 {
		if i < nSmalls {
			return append(dst, small(int(i))...)
		}
		var a [24]byte
		nd := numDigits(i)
		formatBase10(a[:nd], i)
		return append(dst, a[:nd]...)
	}
	r := formatBitsAppend(dst, i, base, false)
	return r
}

const digits = "0123456789abcdefghijklmnopqrstuvwxyz"

// formatBitsResult is the single-result form of formatBits (WS D).
type formatBitsResult struct {
	s string
}

// formatBits computes the string representation of u in the given base.
// If neg is set, u is treated as negative int64 value.
// (gc's append_ variant is split into formatBitsAppend below.)
func formatBits(u uint64, base int, neg bool) formatBitsResult {
	if base < 2 || base == 10 || base > len(digits) {
		panic("strconv: illegal AppendInt/FormatInt base")
	}
	// 2 <= base && base <= len(digits)

	var a [64 + 1]byte // +1 for sign of 64bit value in base 2
	i := len(a)
	if neg {
		u = -u
	}

	// convert bits
	// We use uint values where we can because those will
	// fit into a single register even on a 32bit machine.
	if isPowerOfTwo(base) {
		// Use shifts and masks instead of / and %.
		shift := trailingZerosUint(uint(base))
		b := uint64(base)
		m := uint(base) - 1 // == 1<<shift - 1
		for u >= b {
			i--
			a[i] = digits[uint(u)&m]
			u >>= shift
		}
		// u < base
		i--
		a[i] = digits[uint(u)]
	} else {
		// general case
		b := uint64(base)
		for u >= b {
			i--
			// Avoid using r = a%b in addition to q = a/b
			// since 64bit division and modulo operations
			// are calculated by runtime functions on 32bit machines.
			q := u / b
			a[i] = digits[uint(u-q*b)]
			u = q
		}
		// u < base
		i--
		a[i] = digits[uint(u)]
	}

	// add sign, if any
	if neg {
		i--
		a[i] = '-'
	}

	return formatBitsResult{string(a[i:])}
}

// formatBitsAppend is gc's formatBits with append_ = true.
func formatBitsAppend(dst []byte, u uint64, base int, neg bool) []byte {
	if base < 2 || base == 10 || base > len(digits) {
		panic("strconv: illegal AppendInt/FormatInt base")
	}

	var a [64 + 1]byte // +1 for sign of 64bit value in base 2
	i := len(a)
	if neg {
		u = -u
	}

	if isPowerOfTwo(base) {
		shift := trailingZerosUint(uint(base))
		b := uint64(base)
		m := uint(base) - 1
		for u >= b {
			i--
			a[i] = digits[uint(u)&m]
			u >>= shift
		}
		i--
		a[i] = digits[uint(u)]
	} else {
		b := uint64(base)
		for u >= b {
			i--
			q := u / b
			a[i] = digits[uint(u-q*b)]
			u = q
		}
		i--
		a[i] = digits[uint(u)]
	}

	if neg {
		i--
		a[i] = '-'
	}

	return append(dst, a[i:]...)
}

func isPowerOfTwo(x int) bool {
	return x&(x-1) == 0
}

// trailingZerosUint is math/bits.TrailingZeros for uint (inlined:
// math/bits is not an available import).
func trailingZerosUint(x uint) int {
	n := 0
	for x&1 == 0 {
		n++
		x >>= 1
	}
	return n
}

const nSmalls = 100

// smalls is the formatting of 00..99 concatenated.
const smalls = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// small returns the string for an i with 0 <= i < nSmalls.
func small(i int) string {
	if i < 10 {
		return digits[i : i+1]
	}
	return smalls[i*2 : i*2+2]
}

// formatBase10 formats the decimal representation of u into a.
// The caller is responsible for ensuring that a is big enough to hold u.
// If a is too big, leading zeros will be filled in as needed.
func formatBase10(a []byte, u uint64) {
	nd := len(a)
	for nd >= 8 {
		// Format last 8 digits (4 pairs).
		x3210 := uint32(u % 1e8)
		u /= 1e8
		x32, x10 := x3210/1e4, x3210%1e4
		x1, x0 := (x10/100)*2, (x10%100)*2
		x3, x2 := (x32/100)*2, (x32%100)*2
		a[nd-1], a[nd-2] = smalls[x0+1], smalls[x0]
		a[nd-3], a[nd-4] = smalls[x1+1], smalls[x1]
		a[nd-5], a[nd-6] = smalls[x2+1], smalls[x2]
		a[nd-7], a[nd-8] = smalls[x3+1], smalls[x3]
		nd -= 8
	}

	x := uint32(u)
	if nd >= 4 {
		// Format last 4 digits (2 pairs).
		x10 := x % 1e4
		x /= 1e4
		x1, x0 := (x10/100)*2, (x10%100)*2
		a[nd-1], a[nd-2] = smalls[x0+1], smalls[x0]
		a[nd-3], a[nd-4] = smalls[x1+1], smalls[x1]
		nd -= 4
	}
	if nd >= 2 {
		// Format last 2 digits.
		x0 := (x % 1e2) * 2
		x /= 1e2
		a[nd-1], a[nd-2] = smalls[x0+1], smalls[x0]
		nd -= 2
	}
	if nd > 0 {
		// Format final digit.
		a[0] = byte('0' + x)
	}
}

// ---------------------------------------------------------------------------
// Floating point binary<->decimal conversion by fast unrounded scaling
// (port of internal/strconv/uscale.go).
//
// Adaptations: math/bits.Len64 -> len64u, math/bits.Mul64 -> mul64u,
// bool2[T] -> monomorphized bool2int/bool2uint32/bool2uint64/bool2unrounded,
// shortFloat[F] -> shortFloat32/shortFloat64 via shortFloatGen,
// builtin min -> explicit ifs.
// ---------------------------------------------------------------------------

// bool2int converts b to an int: 1 for true, 0 for false.
func bool2int(b bool) int {
	if b {
		return 1
	}
	return 0
}

// bool2uint32 converts b to a uint32: 1 for true, 0 for false.
func bool2uint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// bool2uint64 converts b to a uint64: 1 for true, 0 for false.
func bool2uint64(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// bool2unrounded converts b to an unrounded: 1 for true, 0 for false.
func bool2unrounded(b bool) unrounded {
	if b {
		return 1
	}
	return 0
}

// len64u is math/bits.Len64 (inlined: math/bits is not an available import).
func len64u(x uint64) int {
	n := 0
	for x != 0 {
		n++
		x >>= 1
	}
	return n
}

// mul64u is math/bits.Mul64 (inlined): the 128-bit product of x and y.
type mul64uPair struct {
	hi uint64
	lo uint64
}

func mul64u(x, y uint64) mul64uPair {
	x0 := x & 0xffffffff
	x1 := x >> 32
	y0 := y & 0xffffffff
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + (w0 >> 32)
	w1 := (t & 0xffffffff) + x0*y1
	w2 := t >> 32
	hi := x1*y1 + w2 + (w1 >> 32)
	_ = w0
	return mul64uPair{hi, (w1 << 32) | (w0 & 0xffffffff)}
}

// pack64 takes m, e and returns f = m * 2**e.
// It assumes the caller has provided a 53-bit mantissa m
// and an exponent that is in range for the mantissa.
type pack64Pair struct {
	f   float64
	err error
}

func pack64(m uint64, e int) pack64Pair {
	if m&(1<<52) == 0 {
		return pack64Pair{float64frombits(m), nil}
	}
	if e >= 0x7FF-1075 {
		return pack64Pair{float64frombits(m&(1<<63) | 0x7ff<<52), errRange}
	}
	return pack64Pair{float64frombits(m&^(1<<52) | uint64(1075+e)<<52), nil}
}

// pack32 takes m, e and returns f = m * 2**e.
// It assumes the caller has provided a 24-bit mantissa m
// and an exponent that is in range for the mantissa.
type pack32Pair struct {
	f   float32
	err error
}

func pack32(m uint32, e int) pack32Pair {
	if m&(1<<23) == 0 {
		return pack32Pair{float32frombits(m), nil}
	}
	if e >= 0xFF-150 {
		return pack32Pair{float32frombits(m&(1<<31) | 0xff<<23), errRange}
	}
	return pack32Pair{float32frombits(m&^(1<<23) | uint32(150+e)<<23), nil}
}

// An unrounded represents an unrounded value.
type unrounded uint64

func (u unrounded) floor() uint64         { return uint64((u + 0) >> 2) }
func (u unrounded) roundHalfDown() uint64 { return uint64((u + 1) >> 2) }
func (u unrounded) round() uint64         { return uint64((u + 1 + (u>>2)&1) >> 2) }
func (u unrounded) roundHalfUp() uint64   { return uint64((u + 2) >> 2) }
func (u unrounded) ceil() uint64          { return uint64((u + 3) >> 2) }
func (u unrounded) nudge(d int) unrounded { return u + unrounded(d) }

func (u unrounded) div(d uint64) unrounded {
	x := uint64(u)
	return unrounded(x/d) | u&1 | bool2unrounded(x%d != 0)
}

func (u unrounded) rsh(s int) unrounded {
	return u>>s | u&1 | bool2unrounded(u&((1<<s)-1) != 0)
}

// log10Pow2(x) returns floor(log10 2**x) = floor(x * log10 2).
func log10Pow2(x int) int {
	// log10 2 ~= 0.30102999566 ~= 78913 / 2^18
	return (x * 78913) >> 18
}

// log2Pow10(x) returns floor(log2 10**x) = floor(x * log2 10).
func log2Pow10(x int) int {
	// log2 10 ~= 3.32192809489 ~= 108853 / 2^15
	return (x * 108853) >> 15
}

// uint64pow10[x] is 10**x.
var uint64pow10 = [...]uint64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// dpResult is the single-result form of (d uint64, p int) (WS D).
type dpResult struct {
	d uint64
	p int
}

// fixedWidthFloat returns the n-digit decimal form of f = m * 2**e as d * 10**p.
// n can be at most 18.
// If fmt == 'f' then n is a conservative estimate of the number of digits,
// and digits are discarded to match prec.
func fixedWidthFloat(m uint64, e, n, prec int, fmt byte) dpResult {
	p := n - 1 - log10Pow2(e+63)
	pre := prescale(e, p, log2Pow10(p))
	u := uscale(m, pre)
	if u >= unmin(uint64pow10[n]) {
		u = u.div(10)
		p--
	}
	if fmt == 'f' {
		for p > prec {
			u = u.div(10)
			p--
		}
	}
	return dpResult{u.round(), -p}
}

// floatErrResult is the single-result form of (float64, error) (WS D).
type floatErrResult struct {
	f   float64
	err error
}

// float32ErrResult is the single-result form of (float32, error) (WS D).
type float32ErrResult struct {
	f   float32
	err error
}

// parseFloat64 rounds d * 10**p to the nearest float64 f.
// d can have at most 19 digits.
// It returns errRange if the result rounds to infinity.
func parseFloat64(d uint64, p int, sign uint64) floatErrResult {
	b := len64u(d)
	lp := log2Pow10(p)
	e := 53 - b - lp
	if e > 1074 {
		e = 1074
	}
	pre := prescale(e-(64-b), p, lp)
	if pre.s >= 64 {
		return floatErrResult{float64frombits(sign | 0), nil}
	}
	u := uscale(d<<(64-b), pre)

	// This block is branch-free code for:
	//
	//	if u.round() >= 1<<53 {
	//		u = u.rsh(1)
	//		e = e - 1
	//	}
	s := bool2int(u >= unmin(1<<53))
	u = u>>s | u&1
	e = e - s

	pr64 := pack64(sign|u.round(), -e)
	f, err := pr64.f, pr64.err
	return floatErrResult{f, err}
}

// parseFloat32 rounds d * 10**p to the nearest float32 f.
// d can have at most 19 digits.
// It returns errRange if the result rounds to infinity.
func parseFloat32(d uint64, p int, sign uint32) float32ErrResult {
	b := len64u(d)
	lp := log2Pow10(p)
	e := 24 - b - lp
	if e > 149 {
		e = 149
	}
	pre := prescale(e-(64-b), p, lp)
	if pre.s >= 64 {
		return float32ErrResult{float32frombits(sign | 0), nil}
	}
	u := uscale(d<<(64-b), pre)

	// This block is branch-free code for:
	//
	//	if u.round() >= 1<<24 {
	//		u = u.rsh(1)
	//		e = e - 1
	//	}
	s := bool2int(u >= unmin(1<<24))
	u = u>>s | u&1
	e = e - s

	pr32 := pack32(sign|uint32(u.round()), -e)
	f, err := pr32.f, pr32.err
	return float32ErrResult{f, err}
}

// unmin returns the minimum unrounded that rounds to x.
func unmin(x uint64) unrounded {
	return unrounded(x<<2 - 2)
}

// shortFloat32 computes the shortest formatting of a float32 f = m * 2**e,
// using as few digits as possible that will still round trip
// back to the original float. (Monomorphized from shortFloat[F].)
func shortFloat32(m uint64, e int) dpResult {
	return shortFloatGen(m, e, float32MantBits, float32MinExp)
}

// shortFloat64 computes the shortest formatting of a float64 f = m * 2**e,
// using as few digits as possible that will still round trip
// back to the original float. (Monomorphized from shortFloat[F].)
func shortFloat64(m uint64, e int) dpResult {
	return shortFloatGen(m, e, float64MantBits, float64MinExp)
}

// shortFloatGen is the shared body of shortFloat32/shortFloat64.
func shortFloatGen(m uint64, e, mantBits, minExp int) dpResult {
	var d uint64
	var p int
	// Note: these cases could be factored a little more,
	// but in the first two branches, z is a constant,
	// allowing the compiler to greatly simplify the code.
	var min, max uint64
	var odd int
	z := 63 - mantBits
	if m == 1<<63 && e > minExp {
		p = -skewed(e + z)
		min = m - 1<<(z-2) // min = m - 1/4 * 2**(e+z)
		max = m + 1<<(z-1) // max = m + 1/2 * 2**(e+z)
		odd = int(m>>z) & 1
	} else if e >= minExp {
		p = -log10Pow2(e + z)
		min = m - 1<<(z-1) // min = m - 1/2 * 2**(e+z)
		max = m + 1<<(z-1) // max = m + 1/2 * 2**(e+z)
		odd = int(m>>z) & 1
	} else {
		z = z + (minExp - e)
		p = -log10Pow2(e + z)
		min = m - 1<<(z-1) // min = m - 1/2 * 2**(e+z)
		max = m + 1<<(z-1) // max = m + 1/2 * 2**(e+z)
		odd = int(m>>z) & 1
	}

	pre := prescale(e, p, log2Pow10(p))
	dmin := uscale(min, pre).nudge(+odd).ceil()
	dmax := uscale(max, pre).nudge(-odd).floor()

	if d = dmax / 10; d*10 >= dmin {
		return dpResult{d, -(p - 1)}
	}
	if d = dmin; d < dmax {
		d = uscale(m, pre).round()
	}
	return dpResult{d, -p}
}

// skewed computes the skewed footprint of m * 2**e,
// which is floor(log10 3/4 * 2**e) = floor(e*(log10 2)-(log10 4/3)).
func skewed(e int) int {
	return (e*631305 - 261663) >> 21
}

// A pmHiLo represents hi<<64 - lo.
type pmHiLo struct {
	hi uint64
	lo uint64
}

// A scaler holds derived scaling constants for a given e, p pair.
type scaler struct {
	// Note: using pm pmHiLo here nudges uscale just over the inlining boundary. Don't.
	pmHi uint64
	pmLo uint64
	s    int
}

// prescale returns the scaling constants for e, p.
// lp must be log2Pow10(p).
// The caller is responsible for either avoiding e, p pairs
// that cause pre.s < 0 or pre.s >= 64, or else handling
// those cases before passing the result to uscale.
// In practice, pre.s < 0 would indicate a buggy caller
// and pre.s >= 64 can only happen for parsing and is
// picked off at those call sites.
func prescale(e, p, lp int) scaler {
	var pre scaler
	pre.pmHi = pow10Tab[p-pow10Min].hi
	pre.pmLo = pow10Tab[p-pow10Min].lo
	pre.s = -(e + lp + 3)
	return pre
}

// uscale returns unround(x * 2**e * 10**p).
// The caller should pass pre := prescale(e, p, log2Pow10(p))
// and should have left-justified x so its high bit is set.
// The caller is also responsible for checking that c.s < 64.
// For formatting, that's always true.
// For parsing, the caller needs to pick it off early and return a signed 0.
func uscale(x uint64, c scaler) unrounded {
	m64 := mul64u(x, c.pmHi)
	hi, mid := m64.hi, m64.lo
	s := c.s & 63 // make shifts cheaper
	if hi>>s<<s != hi {
		return unrounded(hi>>s | 1)
	}
	m64b := mul64u(x, c.pmLo)
	mid2 := m64b.hi
	hi -= bool2uint64(mid < mid2)
	return unrounded(hi>>s | bool2uint64(mid-mid2 > 1))
}

// digitsResult is the single-result form of setDigits' (dp, nd int) (WS D).
type digitsResult struct {
	dp int
	nd int
}

// setDigits sets digs to the nd digits described by d, p.
func setDigits(s []byte, d uint64, p, nd int) digitsResult {
	// Note: nd <= len(s) is guaranteed by caller,
	// but writing it explicitly here lets the compiler know,
	// so that it can remove the bounds check in the loop.
	// (The slice s[:nd] not panicking only establishes nd <= cap(s).)
	dp := 0
	nzd := nd
	if nd <= len(s) {
		formatBase10(s[:nd], d)
		dp = nd + p
		for nzd > 0 && s[nzd-1] == '0' {
			nzd--
		}
	}
	return digitsResult{dp, nzd}
}

// numDigits returns the number of decimal digits in d.
// It requires d >= 1.
func numDigits(d uint64) int {
	nd := log10Pow2(len64u(d))
	return nd + bool2int(d >= uint64pow10[nd])
}
// ---------------------------------------------------------------------------
// pow10Tab: 128-bit mantissas of powers of 10 (verbatim from
// internal/strconv/pow10tab.go; generated by pow10gen.go).
// ---------------------------------------------------------------------------

const (
	pow10Min = -348
	pow10Max = 347
)

// pow10Tab holds 128-bit mantissas of powers of 10.
// The values are scaled so the high bit is always set.
var pow10Tab = [...]pmHiLo{
	{0xfa8fd5a0081c0289, 0xe8cd3796329f1bac}, // 1e-348 * 2**1284
	{0x9c99e58405118196, 0xf18042bddfa3714b}, // 1e-347 * 2**1280
	{0xc3c05ee50655e1fb, 0xade0536d578c4d9e}, // 1e-346 * 2**1277
	{0xf4b0769e47eb5a79, 0x19586848ad6f6106}, // 1e-345 * 2**1274
	{0x98ee4a22ecf3188c, 0x6fd7412d6c659ca3}, // 1e-344 * 2**1270
	{0xbf29dcaba82fdeaf, 0x8bcd1178c77f03cc}, // 1e-343 * 2**1267
	{0xeef453d6923bd65b, 0xeec055d6f95ec4c0}, // 1e-342 * 2**1264
	{0x9558b4661b6565f9, 0xb53835a65bdb3af8}, // 1e-341 * 2**1260
	{0xbaaee17fa23ebf77, 0xa286430ff2d209b6}, // 1e-340 * 2**1257
	{0xe95a99df8ace6f54, 0x0b27d3d3ef868c23}, // 1e-339 * 2**1254
	{0x91d8a02bb6c10595, 0x86f8e46475b41796}, // 1e-338 * 2**1250
	{0xb64ec836a47146fa, 0x68b71d7d93211d7b}, // 1e-337 * 2**1247
	{0xe3e27a444d8d98b8, 0x02e4e4dcf7e964da}, // 1e-336 * 2**1244
	{0x8e6d8c6ab0787f73, 0x01cf0f0a1af1df08}, // 1e-335 * 2**1240
	{0xb208ef855c969f50, 0x4242d2cca1ae56ca}, // 1e-334 * 2**1237
	{0xde8b2b66b3bc4724, 0x52d3877fca19ec7d}, // 1e-333 * 2**1234
	{0x8b16fb203055ac77, 0xb3c434afde5033ce}, // 1e-332 * 2**1230
	{0xaddcb9e83c6b1794, 0x20b541dbd5e440c2}, // 1e-331 * 2**1227
	{0xd953e8624b85dd79, 0x28e29252cb5d50f2}, // 1e-330 * 2**1224
	{0x87d4713d6f33aa6c, 0x798d9b73bf1a5297}, // 1e-329 * 2**1220
	{0xa9c98d8ccb009507, 0x97f10250aee0e73d}, // 1e-328 * 2**1217
	{0xd43bf0effdc0ba49, 0xfded42e4da99210d}, // 1e-327 * 2**1214
	{0x84a57695fe98746e, 0xfeb449cf089fb4a8}, // 1e-326 * 2**1210
	{0xa5ced43b7e3e9189, 0xbe615c42cac7a1d2}, // 1e-325 * 2**1207
	{0xcf42894a5dce35eb, 0xadf9b3537d798a46}, // 1e-324 * 2**1204
	{0x818995ce7aa0e1b3, 0x8cbc10142e6bf66c}, // 1e-323 * 2**1200
	{0xa1ebfb4219491a20, 0xefeb14193a06f407}, // 1e-322 * 2**1197
	{0xca66fa129f9b60a7, 0x2be5d91f8888b109}, // 1e-321 * 2**1194
	{0xfd00b897478238d1, 0x76df4f676aaadd4b}, // 1e-320 * 2**1191
	{0x9e20735e8cb16383, 0xaa4b91a0a2aaca4f}, // 1e-319 * 2**1187
	{0xc5a890362fddbc63, 0x14de7608cb557ce2}, // 1e-318 * 2**1184
	{0xf712b443bbd52b7c, 0x5a16138afe2adc1b}, // 1e-317 * 2**1181
	{0x9a6bb0aa55653b2e, 0xb84dcc36dedac991}, // 1e-316 * 2**1177
	{0xc1069cd4eabe89f9, 0x66613f4496917bf5}, // 1e-315 * 2**1174
	{0xf148440a256e2c77, 0x3ff98f15bc35daf2}, // 1e-314 * 2**1171
	{0x96cd2a865764dbcb, 0xc7fbf96d95a1a8d7}, // 1e-313 * 2**1167
	{0xbc807527ed3e12bd, 0x39faf7c8fb0a130d}, // 1e-312 * 2**1164
	{0xeba09271e88d976c, 0x0879b5bb39cc97d1}, // 1e-311 * 2**1161
	{0x93445b8731587ea4, 0x854c1195041fdee2}, // 1e-310 * 2**1157
	{0xb8157268fdae9e4d, 0xa69f15fa4527d69b}, // 1e-309 * 2**1154
	{0xe61acf033d1a45e0, 0x9046db78d671cc42}, // 1e-308 * 2**1151
	{0x8fd0c16206306bac, 0x5a2c492b86071fa9}, // 1e-307 * 2**1147
	{0xb3c4f1ba87bc8697, 0x70b75b766788e793}, // 1e-306 * 2**1144
	{0xe0b62e2929aba83d, 0xcce53254016b2178}, // 1e-305 * 2**1141
	{0x8c71dcd9ba0b4926, 0x600f3f7480e2f4eb}, // 1e-304 * 2**1137
	{0xaf8e5410288e1b70, 0xf8130f51a11bb226}, // 1e-303 * 2**1134
	{0xdb71e91432b1a24b, 0x3617d32609629eaf}, // 1e-302 * 2**1131
	{0x892731ac9faf056f, 0x41cee3f7c5dda32d}, // 1e-301 * 2**1127
	{0xab70fe17c79ac6cb, 0x92429cf5b7550bf9}, // 1e-300 * 2**1124
	{0xd64d3d9db981787e, 0xf6d34433252a4ef7}, // 1e-299 * 2**1121
	{0x85f0468293f0eb4f, 0xda440a9ff73a715a}, // 1e-298 * 2**1117
	{0xa76c582338ed2622, 0x50d50d47f5090db1}, // 1e-297 * 2**1114
	{0xd1476e2c07286fab, 0xe50a5099f24b511e}, // 1e-296 * 2**1111
	{0x82cca4db847945cb, 0xaf267260376f12b2}, // 1e-295 * 2**1107
	{0xa37fce126597973d, 0x1af00ef8454ad75f}, // 1e-294 * 2**1104
	{0xcc5fc196fefd7d0d, 0xe1ac12b6569d8d37}, // 1e-293 * 2**1101
	{0xff77b1fcbebcdc50, 0xda171763ec44f085}, // 1e-292 * 2**1098
	{0x9faacf3df73609b2, 0x884e6e9e73ab1653}, // 1e-291 * 2**1094
	{0xc795830d75038c1e, 0x2a620a461095dbe8}, // 1e-290 * 2**1091
	{0xf97ae3d0d2446f26, 0xb4fa8cd794bb52e2}, // 1e-289 * 2**1088
	{0x9becce62836ac578, 0xb11c9806bcf513cd}, // 1e-288 * 2**1084
	{0xc2e801fb244576d6, 0xdd63be086c3258c0}, // 1e-287 * 2**1081
	{0xf3a20279ed56d48b, 0x94bcad8a873eeef0}, // 1e-286 * 2**1078
	{0x9845418c345644d7, 0x7cf5ec7694875556}, // 1e-285 * 2**1074
	{0xbe5691ef416bd60d, 0xdc33679439a92aac}, // 1e-284 * 2**1071
	{0xedec366b11c6cb90, 0xd340417948137557}, // 1e-283 * 2**1068
	{0x94b3a202eb1c3f3a, 0x840828ebcd0c2956}, // 1e-282 * 2**1064
	{0xb9e08a83a5e34f08, 0x250a3326c04f33ac}, // 1e-281 * 2**1061
	{0xe858ad248f5c22ca, 0x2e4cbff070630097}, // 1e-280 * 2**1058
	{0x91376c36d99995bf, 0xdceff7f6463de05e}, // 1e-279 * 2**1054
	{0xb58547448ffffb2e, 0x542bf5f3d7cd5875}, // 1e-278 * 2**1051
	{0xe2e69915b3fff9fa, 0xe936f370cdc0ae93}, // 1e-277 * 2**1048
	{0x8dd01fad907ffc3c, 0x51c2582680986d1c}, // 1e-276 * 2**1044
	{0xb1442798f49ffb4b, 0x6632ee3020be8863}, // 1e-275 * 2**1041
	{0xdd95317f31c7fa1e, 0xbfbfa9bc28ee2a7c}, // 1e-274 * 2**1038
	{0x8a7d3eef7f1cfc53, 0xb7d7ca159994da8d}, // 1e-273 * 2**1034
	{0xad1c8eab5ee43b67, 0x25cdbc9afffa1130}, // 1e-272 * 2**1031
	{0xd863b256369d4a41, 0x6f412bc1bff8957d}, // 1e-271 * 2**1028
	{0x873e4f75e2224e69, 0xa588bb5917fb5d6e}, // 1e-270 * 2**1024
	{0xa90de3535aaae203, 0x8eeaea2f5dfa34c9}, // 1e-269 * 2**1021
	{0xd3515c2831559a84, 0xf2a5a4bb3578c1fc}, // 1e-268 * 2**1018
	{0x8412d9991ed58092, 0x17a786f5016b793d}, // 1e-267 * 2**1014
	{0xa5178fff668ae0b7, 0x9d9168b241c6578d}, // 1e-266 * 2**1011
	{0xce5d73ff402d98e4, 0x04f5c2ded237ed70}, // 1e-265 * 2**1008
	{0x80fa687f881c7f8f, 0x831999cb4362f466}, // 1e-264 * 2**1004
	{0xa139029f6a239f73, 0xe3e0003e143bb17f}, // 1e-263 * 2**1001
	{0xc987434744ac874f, 0x5cd8004d994a9ddf}, // 1e-262 * 2**998
	{0xfbe9141915d7a923, 0xb40e0060ff9d4557}, // 1e-261 * 2**995
	{0x9d71ac8fada6c9b6, 0x9088c03c9fc24b56}, // 1e-260 * 2**991
	{0xc4ce17b399107c23, 0x34aaf04bc7b2de2c}, // 1e-259 * 2**988
	{0xf6019da07f549b2c, 0x81d5ac5eb99f95b7}, // 1e-258 * 2**985
	{0x99c102844f94e0fc, 0xd1258bbb3403bd92}, // 1e-257 * 2**981
	{0xc0314325637a193a, 0x056eeeaa0104acf7}, // 1e-256 * 2**978
	{0xf03d93eebc589f89, 0x86caaa548145d835}, // 1e-255 * 2**975
	{0x96267c7535b763b6, 0xb43eaa74d0cba721}, // 1e-254 * 2**971
	{0xbbb01b9283253ca3, 0x614e551204fe90e9}, // 1e-253 * 2**968
	{0xea9c227723ee8bcc, 0xb9a1ea56863e3523}, // 1e-252 * 2**965
	{0x92a1958a76751760, 0xf405327613e6e136}, // 1e-251 * 2**961
	{0xb749faed14125d37, 0x31067f1398e09984}, // 1e-250 * 2**958
	{0xe51c79a85916f485, 0x7d481ed87f18bfe5}, // 1e-249 * 2**955
	{0x8f31cc0937ae58d3, 0x2e4d13474f6f77ef}, // 1e-248 * 2**951
	{0xb2fe3f0b8599ef08, 0x79e05819234b55ea}, // 1e-247 * 2**948
	{0xdfbdcece67006aca, 0x98586e1f6c1e2b65}, // 1e-246 * 2**945
	{0x8bd6a141006042be, 0x1f3744d3a392db1f}, // 1e-245 * 2**941
	{0xaecc49914078536e, 0xa70516088c7791e7}, // 1e-244 * 2**938
	{0xda7f5bf590966849, 0x50c65b8aaf957661}, // 1e-243 * 2**935
	{0x888f99797a5e012e, 0x927bf936adbd69fc}, // 1e-242 * 2**931
	{0xaab37fd7d8f58179, 0x371af784592cc47c}, // 1e-241 * 2**928
	{0xd5605fcdcf32e1d7, 0x04e1b5656f77f59b}, // 1e-240 * 2**925
	{0x855c3be0a17fcd27, 0xa30d115f65aaf980}, // 1e-239 * 2**921
	{0xa6b34ad8c9dfc070, 0x0bd055b73f15b7e1}, // 1e-238 * 2**918
	{0xd0601d8efc57b08c, 0x0ec46b250edb25d9}, // 1e-237 * 2**915
	{0x823c12795db6ce58, 0x893ac2f72948f7a7}, // 1e-236 * 2**911
	{0xa2cb1717b52481ee, 0xab8973b4f39b3591}, // 1e-235 * 2**908
	{0xcb7ddcdda26da269, 0x566bd0a2308202f6}, // 1e-234 * 2**905
	{0xfe5d54150b090b03, 0x2c06c4cabca283b3}, // 1e-233 * 2**902
	{0x9efa548d26e5a6e2, 0x3b843afeb5e59250}, // 1e-232 * 2**898
	{0xc6b8e9b0709f109b, 0xca6549be635ef6e4}, // 1e-231 * 2**895
	{0xf867241c8cc6d4c1, 0x3cfe9c2dfc36b49d}, // 1e-230 * 2**892
	{0x9b407691d7fc44f9, 0x861f219cbda230e2}, // 1e-229 * 2**888
	{0xc21094364dfb5637, 0x67a6ea03ed0abd1b}, // 1e-228 * 2**885
	{0xf294b943e17a2bc5, 0xc190a484e84d6c62}, // 1e-227 * 2**882
	{0x979cf3ca6cec5b5b, 0x58fa66d3113063bd}, // 1e-226 * 2**878
	{0xbd8430bd08277232, 0xaf390087d57c7cac}, // 1e-225 * 2**875
	{0xece53cec4a314ebe, 0x5b0740a9cadb9bd7}, // 1e-224 * 2**872
	{0x940f4613ae5ed137, 0x78e4886a1ec94166}, // 1e-223 * 2**868
	{0xb913179899f68585, 0xd71daa84a67b91c0}, // 1e-222 * 2**865
	{0xe757dd7ec07426e6, 0xcce51525d01a7630}, // 1e-221 * 2**862
	{0x9096ea6f38489850, 0xc00f2d37a21089de}, // 1e-220 * 2**858
	{0xb4bca50b065abe64, 0xf012f8858a94ac56}, // 1e-219 * 2**855
	{0xe1ebce4dc7f16dfc, 0x2c17b6a6ed39d76b}, // 1e-218 * 2**852
	{0x8d3360f09cf6e4be, 0x9b8ed228544426a3}, // 1e-217 * 2**848
	{0xb080392cc4349ded, 0x427286b26955304c}, // 1e-216 * 2**845
	{0xdca04777f541c568, 0x130f285f03aa7c5f}, // 1e-215 * 2**842
	{0x89e42caaf9491b61, 0x0be9793b624a8dbb}, // 1e-214 * 2**838
	{0xac5d37d5b79b623a, 0xcee3d78a3add312a}, // 1e-213 * 2**835
	{0xd77485cb25823ac8, 0x829ccd6cc9947d74}, // 1e-212 * 2**832
	{0x86a8d39ef77164bd, 0x51a20063fdfcce68}, // 1e-211 * 2**828
	{0xa8530886b54dbdec, 0x260a807cfd7c0203}, // 1e-210 * 2**825
	{0xd267caa862a12d67, 0x2f8d209c3cdb0284}, // 1e-209 * 2**822
	{0x8380dea93da4bc61, 0xbdb83461a608e192}, // 1e-208 * 2**818
	{0xa46116538d0deb79, 0xad26417a0f8b19f7}, // 1e-207 * 2**815
	{0xcd795be870516657, 0x986fd1d8936de074}, // 1e-206 * 2**812
	{0x806bd9714632dff7, 0xff45e3275c24ac49}, // 1e-205 * 2**808
	{0xa086cfcd97bf97f4, 0x7f175bf1332dd75b}, // 1e-204 * 2**805
	{0xc8a883c0fdaf7df1, 0x9edd32ed7ff94d32}, // 1e-203 * 2**802
	{0xfad2a4b13d1b5d6d, 0x86947fa8dff7a07e}, // 1e-202 * 2**799
	{0x9cc3a6eec6311a64, 0x341ccfc98bfac44f}, // 1e-201 * 2**795
	{0xc3f490aa77bd60fd, 0x412403bbeef97563}, // 1e-200 * 2**792
	{0xf4f1b4d515acb93c, 0x116d04aaeab7d2bb}, // 1e-199 * 2**789
	{0x991711052d8bf3c6, 0x8ae422ead2b2e3b5}, // 1e-198 * 2**785
	{0xbf5cd54678eef0b7, 0x2d9d2ba5875f9ca2}, // 1e-197 * 2**782
	{0xef340a98172aace5, 0x7904768ee93783cb}, // 1e-196 * 2**779
	{0x9580869f0e7aac0f, 0x2ba2ca1951c2b25f}, // 1e-195 * 2**775
	{0xbae0a846d2195713, 0x768b7c9fa6335ef6}, // 1e-194 * 2**772
	{0xe998d258869facd8, 0xd42e5bc78fc036b4}, // 1e-193 * 2**769
	{0x91ff83775423cc07, 0x849cf95cb9d82230}, // 1e-192 * 2**765
	{0xb67f6455292cbf09, 0xe5c437b3e84e2abd}, // 1e-191 * 2**762
	{0xe41f3d6a7377eecb, 0xdf3545a0e261b56c}, // 1e-190 * 2**759
	{0x8e938662882af53f, 0xab814b848d7d1163}, // 1e-189 * 2**755
	{0xb23867fb2a35b28e, 0x16619e65b0dc55bc}, // 1e-188 * 2**752
	{0xdec681f9f4c31f32, 0x9bfa05ff1d136b2b}, // 1e-187 * 2**749
	{0x8b3c113c38f9f37f, 0x217c43bf722c22fb}, // 1e-186 * 2**745
	{0xae0b158b4738705f, 0x69db54af4eb72bba}, // 1e-185 * 2**742
	{0xd98ddaee19068c77, 0xc45229db2264f6a8}, // 1e-184 * 2**739
	{0x87f8a8d4cfa417ca, 0x1ab35a28f57f1a29}, // 1e-183 * 2**735
	{0xa9f6d30a038d1dbd, 0xa16030b332dee0b3}, // 1e-182 * 2**732
	{0xd47487cc8470652c, 0x89b83cdfff9698e0}, // 1e-181 * 2**729
	{0x84c8d4dfd2c63f3c, 0xd613260bffbe1f8c}, // 1e-180 * 2**725
	{0xa5fb0a17c777cf0a, 0x0b97ef8effada76f}, // 1e-179 * 2**722
	{0xcf79cc9db955c2cd, 0x8e7deb72bf99114b}, // 1e-178 * 2**719
	{0x81ac1fe293d599c0, 0x390eb327b7bfaacf}, // 1e-177 * 2**715
	{0xa21727db38cb0030, 0x47525ff1a5af9583}, // 1e-176 * 2**712
	{0xca9cf1d206fdc03c, 0x5926f7ee0f1b7ae3}, // 1e-175 * 2**709
	{0xfd442e4688bd304b, 0x6f70b5e992e2599c}, // 1e-174 * 2**706
	{0x9e4a9cec15763e2f, 0x65a671b1fbcd7801}, // 1e-173 * 2**702
	{0xc5dd44271ad3cdbb, 0xbf100e1e7ac0d602}, // 1e-172 * 2**699
	{0xf7549530e188c129, 0x2ed411a619710b83}, // 1e-171 * 2**696
	{0x9a94dd3e8cf578ba, 0x7d448b07cfe6a731}, // 1e-170 * 2**692
	{0xc13a148e3032d6e8, 0x1c95adc9c3e050fe}, // 1e-169 * 2**689
	{0xf18899b1bc3f8ca2, 0x23bb193c34d8653e}, // 1e-168 * 2**686
	{0x96f5600f15a7b7e6, 0xd654efc5a1073f46}, // 1e-167 * 2**682
	{0xbcb2b812db11a5df, 0x8bea2bb709490f18}, // 1e-166 * 2**679
	{0xebdf661791d60f57, 0xeee4b6a4cb9b52de}, // 1e-165 * 2**676
	{0x936b9fcebb25c996, 0x354ef226ff4113cb}, // 1e-164 * 2**672
	{0xb84687c269ef3bfc, 0xc2a2aeb0bf1158bd}, // 1e-163 * 2**669
	{0xe65829b3046b0afb, 0xf34b5a5ceed5aeed}, // 1e-162 * 2**666
	{0x8ff71a0fe2c2e6dd, 0xb80f187a15458d54}, // 1e-161 * 2**662
	{0xb3f4e093db73a094, 0xa612de989a96f0a9}, // 1e-160 * 2**659
	{0xe0f218b8d25088b9, 0xcf97963ec13cacd3}, // 1e-159 * 2**656
	{0x8c974f7383725574, 0xe1bebde738c5ec04}, // 1e-158 * 2**652
	{0xafbd2350644eead0, 0x1a2e6d6106f76705}, // 1e-157 * 2**649
	{0xdbac6c247d62a584, 0x20ba08b948b540c6}, // 1e-156 * 2**646
	{0x894bc396ce5da773, 0x94744573cd71487c}, // 1e-155 * 2**642
	{0xab9eb47c81f51150, 0xf99156d0c0cd9a9b}, // 1e-154 * 2**639
	{0xd686619ba27255a3, 0x37f5ac84f1010142}, // 1e-153 * 2**636
	{0x8613fd0145877586, 0x42f98bd316a0a0c9}, // 1e-152 * 2**632
	{0xa798fc4196e952e8, 0xd3b7eec7dc48c8fb}, // 1e-151 * 2**629
	{0xd17f3b51fca3a7a1, 0x08a5ea79d35afb3a}, // 1e-150 * 2**626
	{0x82ef85133de648c5, 0x6567b28c2418dd04}, // 1e-149 * 2**622
	{0xa3ab66580d5fdaf6, 0x3ec19f2f2d1f1445}, // 1e-148 * 2**619
	{0xcc963fee10b7d1b4, 0xce7206faf866d957}, // 1e-147 * 2**616
	{0xffbbcfe994e5c620, 0x020e88b9b6808fad}, // 1e-146 * 2**613
	{0x9fd561f1fd0f9bd4, 0x01491574121059cc}, // 1e-145 * 2**609
	{0xc7caba6e7c5382c9, 0x019b5ad11694703f}, // 1e-144 * 2**606
	{0xf9bd690a1b68637c, 0xc20231855c398c4f}, // 1e-143 * 2**603
	{0x9c1661a651213e2e, 0xf9415ef359a3f7b1}, // 1e-142 * 2**599
	{0xc31bfa0fe5698db9, 0xb791b6b0300cf59d}, // 1e-141 * 2**596
	{0xf3e2f893dec3f127, 0xa576245c3c103305}, // 1e-140 * 2**593
	{0x986ddb5c6b3a76b8, 0x0769d6b9a58a1fe3}, // 1e-139 * 2**589
	{0xbe89523386091466, 0x09444c680eeca7dc}, // 1e-138 * 2**586
	{0xee2ba6c0678b5980, 0x8b955f8212a7d1d3}, // 1e-137 * 2**583
	{0x94db483840b717f0, 0x573d5bb14ba8e323}, // 1e-136 * 2**579
	{0xba121a4650e4ddec, 0x6d0cb29d9e931bec}, // 1e-135 * 2**576
	{0xe896a0d7e51e1567, 0x884fdf450637e2e8}, // 1e-134 * 2**573
	{0x915e2486ef32cd61, 0xf531eb8b23e2edd1}, // 1e-133 * 2**569
	{0xb5b5ada8aaff80b9, 0xf27e666decdba945}, // 1e-132 * 2**566
	{0xe3231912d5bf60e7, 0xef1e000968129396}, // 1e-131 * 2**563
	{0x8df5efabc5979c90, 0x3572c005e10b9c3e}, // 1e-130 * 2**559
	{0xb1736b96b6fd83b4, 0x42cf7007594e834d}, // 1e-129 * 2**556
	{0xddd0467c64bce4a1, 0x53834c092fa22421}, // 1e-128 * 2**553
	{0x8aa22c0dbef60ee5, 0x94320f85bdc55694}, // 1e-127 * 2**549
	{0xad4ab7112eb3929e, 0x793e93672d36ac39}, // 1e-126 * 2**546
	{0xd89d64d57a607745, 0x178e3840f8845748}, // 1e-125 * 2**543
	{0x87625f056c7c4a8c, 0xeeb8e3289b52b68d}, // 1e-124 * 2**539
	{0xa93af6c6c79b5d2e, 0x2a671bf2c2276430}, // 1e-123 * 2**536
	{0xd389b4787982347a, 0xb500e2ef72b13d3c}, // 1e-122 * 2**533
	{0x843610cb4bf160cc, 0x31208dd5a7aec645}, // 1e-121 * 2**529
	{0xa54394fe1eedb8ff, 0x3d68b14b119a77d7}, // 1e-120 * 2**526
	{0xce947a3da6a9273f, 0x8cc2dd9dd60115cd}, // 1e-119 * 2**523
	{0x811ccc668829b888, 0xf7f9ca82a5c0ada0}, // 1e-118 * 2**519
	{0xa163ff802a3426a9, 0x35f83d234f30d908}, // 1e-117 * 2**516
	{0xc9bcff6034c13053, 0x03764c6c22fd0f4a}, // 1e-116 * 2**513
	{0xfc2c3f3841f17c68, 0x4453df872bbc531d}, // 1e-115 * 2**510
	{0x9d9ba7832936edc1, 0x2ab46bb47b55b3f2}, // 1e-114 * 2**506
	{0xc5029163f384a932, 0xf56186a19a2b20ee}, // 1e-113 * 2**503
	{0xf64335bcf065d37e, 0xb2b9e84a00b5e92a}, // 1e-112 * 2**500
	{0x99ea0196163fa42f, 0xafb4312e4071b1ba}, // 1e-111 * 2**496
	{0xc06481fb9bcf8d3a, 0x1ba13d79d08e1e29}, // 1e-110 * 2**493
	{0xf07da27a82c37089, 0xa2898cd844b1a5b3}, // 1e-109 * 2**490
	{0x964e858c91ba2656, 0xc595f8072aef0790}, // 1e-108 * 2**486
	{0xbbe226efb628afeb, 0x76fb7608f5aac974}, // 1e-107 * 2**483
	{0xeadab0aba3b2dbe6, 0xd4ba538b33157bd1}, // 1e-106 * 2**480
	{0x92c8ae6b464fc970, 0xc4f47436ffed6d62}, // 1e-105 * 2**476
	{0xb77ada0617e3bbcc, 0xf6319144bfe8c8bb}, // 1e-104 * 2**473
	{0xe55990879ddcaabe, 0x33bdf595efe2faea}, // 1e-103 * 2**470
	{0x8f57fa54c2a9eab7, 0x6056b97db5eddcd2}, // 1e-102 * 2**466
	{0xb32df8e9f3546565, 0xb86c67dd23695406}, // 1e-101 * 2**463
	{0xdff9772470297ebe, 0xa68781d46c43a908}, // 1e-100 * 2**460
	{0x8bfbea76c619ef37, 0xa814b124c3aa49a5}, // 1e-99 * 2**456
	{0xaefae51477a06b04, 0x1219dd6df494dc0e}, // 1e-98 * 2**453
	{0xdab99e59958885c5, 0x16a054c971ba1312}, // 1e-97 * 2**450
	{0x88b402f7fd75539c, 0xee2434fde7144beb}, // 1e-96 * 2**446
	{0xaae103b5fcd2a882, 0x29ad423d60d95ee6}, // 1e-95 * 2**443
	{0xd59944a37c0752a3, 0xb41892ccb90fb6a0}, // 1e-94 * 2**440
	{0x857fcae62d8493a6, 0x908f5bbff3a9d224}, // 1e-93 * 2**436
	{0xa6dfbd9fb8e5b88f, 0x34b332aff09446ad}, // 1e-92 * 2**433
	{0xd097ad07a71f26b3, 0x81dfff5becb95858}, // 1e-91 * 2**430
	{0x825ecc24c8737830, 0x712bff9973f3d737}, // 1e-90 * 2**426
	{0xa2f67f2dfa90563c, 0x8d76ff7fd0f0cd05}, // 1e-89 * 2**423
	{0xcbb41ef979346bcb, 0xb0d4bf5fc52d0046}, // 1e-88 * 2**420
	{0xfea126b7d78186bd, 0x1d09ef37b6784057}, // 1e-87 * 2**417
	{0x9f24b832e6b0f437, 0xf2263582d20b2836}, // 1e-86 * 2**413
	{0xc6ede63fa05d3144, 0x6eafc2e3868df244}, // 1e-85 * 2**410
	{0xf8a95fcf88747d95, 0x8a5bb39c68316ed5}, // 1e-84 * 2**407
	{0x9b69dbe1b548ce7d, 0x36795041c11ee545}, // 1e-83 * 2**403
	{0xc24452da229b021c, 0x0417a45231669e97}, // 1e-82 * 2**400
	{0xf2d56790ab41c2a3, 0x051d8d66bdc0463c}, // 1e-81 * 2**397
	{0x97c560ba6b0919a6, 0x2332786036982be5}, // 1e-80 * 2**393
	{0xbdb6b8e905cb6010, 0xabff1678443e36df}, // 1e-79 * 2**390
	{0xed246723473e3814, 0xd6fedc16554dc497}, // 1e-78 * 2**387
	{0x9436c0760c86e30c, 0x065f498df5509ade}, // 1e-77 * 2**383
	{0xb94470938fa89bcf, 0x07f71bf172a4c196}, // 1e-76 * 2**380
	{0xe7958cb87392c2c3, 0x49f4e2edcf4df1fb}, // 1e-75 * 2**377
	{0x90bd77f3483bb9ba, 0x4e390dd4a190b73d}, // 1e-74 * 2**373
	{0xb4ecd5f01a4aa829, 0xe1c75149c9f4e50c}, // 1e-73 * 2**370
	{0xe2280b6c20dd5233, 0xda39259c3c721e4f}, // 1e-72 * 2**367
	{0x8d590723948a5360, 0xa863b781a5c752f1}, // 1e-71 * 2**363
	{0xb0af48ec79ace838, 0xd27ca5620f3927ae}, // 1e-70 * 2**360
	{0xdcdb1b2798182245, 0x071bceba9307719a}, // 1e-69 * 2**357
	{0x8a08f0f8bf0f156c, 0xe47161349be4a700}, // 1e-68 * 2**353
	{0xac8b2d36eed2dac6, 0x1d8db981c2ddd0c0}, // 1e-67 * 2**350
	{0xd7adf884aa879178, 0xa4f127e2339544f0}, // 1e-66 * 2**347
	{0x86ccbb52ea94baeb, 0x6716b8ed603d4b16}, // 1e-65 * 2**343
	{0xa87fea27a539e9a6, 0xc0dc6728b84c9ddb}, // 1e-64 * 2**340
	{0xd29fe4b18e88640f, 0x711380f2e65fc552}, // 1e-63 * 2**337
	{0x83a3eeeef9153e8a, 0xe6ac3097cffbdb53}, // 1e-62 * 2**333
	{0xa48ceaaab75a8e2c, 0xa0573cbdc3fad228}, // 1e-61 * 2**330
	{0xcdb02555653131b7, 0xc86d0bed34f986b2}, // 1e-60 * 2**327
	{0x808e17555f3ebf12, 0x1d442774411bf42f}, // 1e-59 * 2**323
	{0xa0b19d2ab70e6ed7, 0xa49531515162f13b}, // 1e-58 * 2**320
	{0xc8de047564d20a8c, 0x0dba7da5a5bbad8a}, // 1e-57 * 2**317
	{0xfb158592be068d2f, 0x11291d0f0f2a98ed}, // 1e-56 * 2**314
	{0x9ced737bb6c4183e, 0xaab9b229697a9f94}, // 1e-55 * 2**310
	{0xc428d05aa4751e4d, 0x55681eb3c3d94779}, // 1e-54 * 2**307
	{0xf53304714d9265e0, 0x2ac22660b4cf9957}, // 1e-53 * 2**304
	{0x993fe2c6d07b7fac, 0x1ab957fc7101bfd6}, // 1e-52 * 2**300
	{0xbf8fdb78849a5f97, 0x2167adfb8d422fcc}, // 1e-51 * 2**297
	{0xef73d256a5c0f77d, 0x69c1997a7092bbbf}, // 1e-50 * 2**294
	{0x95a8637627989aae, 0x2218ffec865bb557}, // 1e-49 * 2**290
	{0xbb127c53b17ec15a, 0xaa9f3fe7a7f2a2ad}, // 1e-48 * 2**287
	{0xe9d71b689dde71b0, 0x55470fe191ef4b59}, // 1e-47 * 2**284
	{0x9226712162ab070e, 0x354c69ecfb358f17}, // 1e-46 * 2**280
	{0xb6b00d69bb55c8d2, 0xc29f84683a02f2dd}, // 1e-45 * 2**277
	{0xe45c10c42a2b3b06, 0x734765824883af95}, // 1e-44 * 2**274
	{0x8eb98a7a9a5b04e4, 0x880c9f716d524dbd}, // 1e-43 * 2**270
	{0xb267ed1940f1c61d, 0xaa0fc74dc8a6e12c}, // 1e-42 * 2**267
	{0xdf01e85f912e37a4, 0x9493b9213ad09977}, // 1e-41 * 2**264
	{0x8b61313bbabce2c7, 0xdcdc53b4c4c25fea}, // 1e-40 * 2**260
	{0xae397d8aa96c1b78, 0x541368a1f5f2f7e5}, // 1e-39 * 2**257
	{0xd9c7dced53c72256, 0x691842ca736fb5de}, // 1e-38 * 2**254
	{0x881cea14545c7576, 0x81af29be8825d1ab}, // 1e-37 * 2**250
	{0xaa242499697392d3, 0x221af42e2a2f4616}, // 1e-36 * 2**247
	{0xd4ad2dbfc3d07788, 0x6aa1b139b4bb179b}, // 1e-35 * 2**244
	{0x84ec3c97da624ab5, 0x42a50ec410f4eec1}, // 1e-34 * 2**240
	{0xa6274bbdd0fadd62, 0x134e527515322a71}, // 1e-33 * 2**237
	{0xcfb11ead453994bb, 0x9821e7125a7eb50d}, // 1e-32 * 2**234
	{0x81ceb32c4b43fcf5, 0x7f15306b788f3128}, // 1e-31 * 2**230
	{0xa2425ff75e14fc32, 0x5eda7c8656b2fd72}, // 1e-30 * 2**227
	{0xcad2f7f5359a3b3f, 0xf6911ba7ec5fbccf}, // 1e-29 * 2**224
	{0xfd87b5f28300ca0e, 0x74356291e777ac03}, // 1e-28 * 2**221
	{0x9e74d1b791e07e49, 0x88a15d9b30aacb82}, // 1e-27 * 2**217
	{0xc612062576589ddb, 0x6ac9b501fcd57e62}, // 1e-26 * 2**214
	{0xf79687aed3eec552, 0xc57c22427c0addfb}, // 1e-25 * 2**211
	{0x9abe14cd44753b53, 0x3b6d95698d86cabd}, // 1e-24 * 2**207
	{0xc16d9a0095928a28, 0x8a48fac3f0e87d6c}, // 1e-23 * 2**204
	{0xf1c90080baf72cb2, 0xacdb3974ed229cc7}, // 1e-22 * 2**201
	{0x971da05074da7bef, 0x2c0903e91435a1fc}, // 1e-21 * 2**197
	{0xbce5086492111aeb, 0x770b44e359430a7b}, // 1e-20 * 2**194
	{0xec1e4a7db69561a6, 0xd4ce161c2f93cd1a}, // 1e-19 * 2**191
	{0x9392ee8e921d5d08, 0xc500cdd19dbc6030}, // 1e-18 * 2**187
	{0xb877aa3236a4b44a, 0xf6410146052b783d}, // 1e-17 * 2**184
	{0xe69594bec44de15c, 0xb3d141978676564c}, // 1e-16 * 2**181
	{0x901d7cf73ab0acda, 0xf062c8feb409f5ef}, // 1e-15 * 2**177
	{0xb424dc35095cd810, 0xac7b7b3e610c736b}, // 1e-14 * 2**174
	{0xe12e13424bb40e14, 0xd79a5a0df94f9046}, // 1e-13 * 2**171
	{0x8cbccc096f5088cc, 0x06c07848bbd1ba2c}, // 1e-12 * 2**167
	{0xafebff0bcb24aaff, 0x0870965aeac628b7}, // 1e-11 * 2**164
	{0xdbe6fecebdedd5bf, 0x4a8cbbf1a577b2e4}, // 1e-10 * 2**161
	{0x89705f4136b4a598, 0xce97f577076acfcf}, // 1e-9 * 2**157
	{0xabcc77118461cefd, 0x023df2d4c94583c2}, // 1e-8 * 2**154
	{0xd6bf94d5e57a42bd, 0xc2cd6f89fb96e4b3}, // 1e-7 * 2**151
	{0x8637bd05af6c69b6, 0x59c065b63d3e4ef0}, // 1e-6 * 2**147
	{0xa7c5ac471b478424, 0xf0307f23cc8de2ac}, // 1e-5 * 2**144
	{0xd1b71758e219652c, 0x2c3c9eecbfb15b57}, // 1e-4 * 2**141
	{0x83126e978d4fdf3c, 0x9ba5e353f7ced916}, // 1e-3 * 2**137
	{0xa3d70a3d70a3d70b, 0xc28f5c28f5c28f5c}, // 1e-2 * 2**134
	{0xcccccccccccccccd, 0x3333333333333333}, // 1e-1 * 2**131
	{0x8000000000000000, 0x0000000000000000}, // 1e0 * 2**127
	{0xa000000000000000, 0x0000000000000000}, // 1e1 * 2**124
	{0xc800000000000000, 0x0000000000000000}, // 1e2 * 2**121
	{0xfa00000000000000, 0x0000000000000000}, // 1e3 * 2**118
	{0x9c40000000000000, 0x0000000000000000}, // 1e4 * 2**114
	{0xc350000000000000, 0x0000000000000000}, // 1e5 * 2**111
	{0xf424000000000000, 0x0000000000000000}, // 1e6 * 2**108
	{0x9896800000000000, 0x0000000000000000}, // 1e7 * 2**104
	{0xbebc200000000000, 0x0000000000000000}, // 1e8 * 2**101
	{0xee6b280000000000, 0x0000000000000000}, // 1e9 * 2**98
	{0x9502f90000000000, 0x0000000000000000}, // 1e10 * 2**94
	{0xba43b74000000000, 0x0000000000000000}, // 1e11 * 2**91
	{0xe8d4a51000000000, 0x0000000000000000}, // 1e12 * 2**88
	{0x9184e72a00000000, 0x0000000000000000}, // 1e13 * 2**84
	{0xb5e620f480000000, 0x0000000000000000}, // 1e14 * 2**81
	{0xe35fa931a0000000, 0x0000000000000000}, // 1e15 * 2**78
	{0x8e1bc9bf04000000, 0x0000000000000000}, // 1e16 * 2**74
	{0xb1a2bc2ec5000000, 0x0000000000000000}, // 1e17 * 2**71
	{0xde0b6b3a76400000, 0x0000000000000000}, // 1e18 * 2**68
	{0x8ac7230489e80000, 0x0000000000000000}, // 1e19 * 2**64
	{0xad78ebc5ac620000, 0x0000000000000000}, // 1e20 * 2**61
	{0xd8d726b7177a8000, 0x0000000000000000}, // 1e21 * 2**58
	{0x878678326eac9000, 0x0000000000000000}, // 1e22 * 2**54
	{0xa968163f0a57b400, 0x0000000000000000}, // 1e23 * 2**51
	{0xd3c21bcecceda100, 0x0000000000000000}, // 1e24 * 2**48
	{0x84595161401484a0, 0x0000000000000000}, // 1e25 * 2**44
	{0xa56fa5b99019a5c8, 0x0000000000000000}, // 1e26 * 2**41
	{0xcecb8f27f4200f3a, 0x0000000000000000}, // 1e27 * 2**38
	{0x813f3978f8940985, 0xc000000000000000}, // 1e28 * 2**34
	{0xa18f07d736b90be6, 0xb000000000000000}, // 1e29 * 2**31
	{0xc9f2c9cd04674edf, 0x5c00000000000000}, // 1e30 * 2**28
	{0xfc6f7c4045812297, 0xb300000000000000}, // 1e31 * 2**25
	{0x9dc5ada82b70b59e, 0x0fe0000000000000}, // 1e32 * 2**21
	{0xc5371912364ce306, 0x93d8000000000000}, // 1e33 * 2**18
	{0xf684df56c3e01bc7, 0x38ce000000000000}, // 1e34 * 2**15
	{0x9a130b963a6c115d, 0xc380c00000000000}, // 1e35 * 2**11
	{0xc097ce7bc90715b4, 0xb460f00000000000}, // 1e36 * 2**8
	{0xf0bdc21abb48db21, 0xe1792c0000000000}, // 1e37 * 2**5
	{0x96769950b50d88f5, 0xecebbb8000000000}, // 1e38 * 2**1
	{0xbc143fa4e250eb32, 0xe826aa6000000000}, // 1e39 * 2**-2
	{0xeb194f8e1ae525fe, 0xa23054f800000000}, // 1e40 * 2**-5
	{0x92efd1b8d0cf37bf, 0xa55e351b00000000}, // 1e41 * 2**-9
	{0xb7abc627050305ae, 0x0eb5c261c0000000}, // 1e42 * 2**-12
	{0xe596b7b0c643c71a, 0x926332fa30000000}, // 1e43 * 2**-15
	{0x8f7e32ce7bea5c70, 0x1b7dffdc5e000000}, // 1e44 * 2**-19
	{0xb35dbf821ae4f38c, 0x225d7fd375800000}, // 1e45 * 2**-22
	{0xe0352f62a19e306f, 0x2af4dfc852e00000}, // 1e46 * 2**-25
	{0x8c213d9da502de46, 0xbad90bdd33cc0000}, // 1e47 * 2**-29
	{0xaf298d050e4395d7, 0x698f4ed480bf0000}, // 1e48 * 2**-32
	{0xdaf3f04651d47b4d, 0xc3f32289a0eec000}, // 1e49 * 2**-35
	{0x88d8762bf324cd10, 0x5a77f59604953800}, // 1e50 * 2**-39
	{0xab0e93b6efee0054, 0x7115f2fb85ba8600}, // 1e51 * 2**-42
	{0xd5d238a4abe98069, 0x8d5b6fba67292780}, // 1e52 * 2**-45
	{0x85a36366eb71f042, 0xb85925d48079b8b0}, // 1e53 * 2**-49
	{0xa70c3c40a64e6c52, 0x666f6f49a09826dc}, // 1e54 * 2**-52
	{0xd0cf4b50cfe20766, 0x000b4b1c08be3093}, // 1e55 * 2**-55
	{0x82818f1281ed44a0, 0x40070ef18576de5b}, // 1e56 * 2**-59
	{0xa321f2d7226895c8, 0x5008d2ade6d495f2}, // 1e57 * 2**-62
	{0xcbea6f8ceb02bb3a, 0x640b07596089bb6f}, // 1e58 * 2**-65
	{0xfee50b7025c36a09, 0xfd0dc92fb8ac2a4b}, // 1e59 * 2**-68
	{0x9f4f2726179a2246, 0xfe289dbdd36b9a6f}, // 1e60 * 2**-72
	{0xc722f0ef9d80aad7, 0xbdb2c52d4846810a}, // 1e61 * 2**-75
	{0xf8ebad2b84e0d58c, 0x2d1f76789a58214d}, // 1e62 * 2**-78
	{0x9b934c3b330c8578, 0x9c33aa0b607714d0}, // 1e63 * 2**-82
	{0xc2781f49ffcfa6d6, 0xc340948e3894da04}, // 1e64 * 2**-85
	{0xf316271c7fc3908b, 0x7410b9b1c6ba1085}, // 1e65 * 2**-88
	{0x97edd871cfda3a57, 0x688a740f1c344a53}, // 1e66 * 2**-92
	{0xbde94e8e43d0c8ed, 0xc2ad1112e3415ce8}, // 1e67 * 2**-95
	{0xed63a231d4c4fb28, 0xb35855579c11b422}, // 1e68 * 2**-98
	{0x945e455f24fb1cf9, 0x70173556c18b1095}, // 1e69 * 2**-102
	{0xb975d6b6ee39e437, 0x4c1d02ac71edd4bb}, // 1e70 * 2**-105
	{0xe7d34c64a9c85d45, 0x9f2443578e6949e9}, // 1e71 * 2**-108
	{0x90e40fbeea1d3a4b, 0x4376aa16b901ce32}, // 1e72 * 2**-112
	{0xb51d13aea4a488de, 0x9454549c674241be}, // 1e73 * 2**-115
	{0xe264589a4dcdab15, 0x396969c38112d22e}, // 1e74 * 2**-118
	{0x8d7eb76070a08aed, 0x03e1e21a30abc35d}, // 1e75 * 2**-122
	{0xb0de65388cc8ada9, 0xc4da5aa0bcd6b434}, // 1e76 * 2**-125
	{0xdd15fe86affad913, 0xb610f148ec0c6141}, // 1e77 * 2**-128
	{0x8a2dbf142dfcc7ac, 0x91ca96cd9387bcc8}, // 1e78 * 2**-132
	{0xacb92ed9397bf997, 0xb63d3c80f869abfb}, // 1e79 * 2**-135
	{0xd7e77a8f87daf7fc, 0x23cc8ba1368416f9}, // 1e80 * 2**-138
	{0x86f0ac99b4e8dafe, 0x965fd744c2128e5c}, // 1e81 * 2**-142
	{0xa8acd7c0222311bd, 0x3bf7cd15f29731f3}, // 1e82 * 2**-145
	{0xd2d80db02aabd62c, 0x0af5c05b6f3cfe6f}, // 1e83 * 2**-148
	{0x83c7088e1aab65dc, 0x86d9983925861f05}, // 1e84 * 2**-152
	{0xa4b8cab1a1563f53, 0xa88ffe476ee7a6c7}, // 1e85 * 2**-155
	{0xcde6fd5e09abcf27, 0x12b3fdd94aa19079}, // 1e86 * 2**-158
	{0x80b05e5ac60b6179, 0xabb07ea7cea4fa4b}, // 1e87 * 2**-162
	{0xa0dc75f1778e39d7, 0x969c9e51c24e38de}, // 1e88 * 2**-165
	{0xc913936dd571c84d, 0xfc43c5e632e1c716}, // 1e89 * 2**-168
	{0xfb5878494ace3a60, 0xfb54b75fbf9a38dc}, // 1e90 * 2**-171
	{0x9d174b2dcec0e47c, 0x9d14f29bd7c06389}, // 1e91 * 2**-175
	{0xc45d1df942711d9b, 0xc45a2f42cdb07c6b}, // 1e92 * 2**-178
	{0xf5746577930d6501, 0x3570bb13811c9b86}, // 1e93 * 2**-181
	{0x9968bf6abbe85f21, 0x816674ec30b1e134}, // 1e94 * 2**-185
	{0xbfc2ef456ae276e9, 0x61c012273cde5981}, // 1e95 * 2**-188
	{0xefb3ab16c59b14a3, 0x3a3016b10c15efe1}, // 1e96 * 2**-191
	{0x95d04aee3b80ece6, 0x445e0e2ea78db5ed}, // 1e97 * 2**-195
	{0xbb445da9ca612820, 0xd57591ba51712368}, // 1e98 * 2**-198
	{0xea1575143cf97227, 0x0ad2f628e5cd6c42}, // 1e99 * 2**-201
	{0x924d692ca61be759, 0xa6c3d9d98fa063a9}, // 1e100 * 2**-205
	{0xb6e0c377cfa2e12f, 0x9074d04ff3887c93}, // 1e101 * 2**-208
	{0xe498f455c38b997b, 0xf4920463f06a9bb8}, // 1e102 * 2**-211
	{0x8edf98b59a373fed, 0xb8db42be7642a153}, // 1e103 * 2**-215
	{0xb2977ee300c50fe8, 0xa712136e13d349a8}, // 1e104 * 2**-218
	{0xdf3d5e9bc0f653e2, 0xd0d6984998c81c12}, // 1e105 * 2**-221
	{0x8b865b215899f46d, 0x42861f2dff7d118b}, // 1e106 * 2**-225
	{0xae67f1e9aec07188, 0x1327a6f97f5c55ee}, // 1e107 * 2**-228
	{0xda01ee641a708dea, 0x17f190b7df336b6a}, // 1e108 * 2**-231
	{0x884134fe908658b3, 0xcef6fa72eb802322}, // 1e109 * 2**-235
	{0xaa51823e34a7eedf, 0x42b4b90fa6602bea}, // 1e110 * 2**-238
	{0xd4e5e2cdc1d1ea97, 0x9361e7538ff836e5}, // 1e111 * 2**-241
	{0x850fadc09923329f, 0xfc1d309439fb224f}, // 1e112 * 2**-245
	{0xa6539930bf6bff46, 0x7b247cb94879eae3}, // 1e113 * 2**-248
	{0xcfe87f7cef46ff17, 0x19ed9be79a98659c}, // 1e114 * 2**-251
	{0x81f14fae158c5f6f, 0xb0348170c09f3f81}, // 1e115 * 2**-255
	{0xa26da3999aef774a, 0x1c41a1ccf0c70f62}, // 1e116 * 2**-258
	{0xcb090c8001ab551d, 0xa3520a402cf8d33a}, // 1e117 * 2**-261
	{0xfdcb4fa002162a64, 0x8c268cd038370809}, // 1e118 * 2**-264
	{0x9e9f11c4014dda7f, 0xd798180223226505}, // 1e119 * 2**-268
	{0xc646d63501a1511e, 0x4d7e1e02abeafe47}, // 1e120 * 2**-271
	{0xf7d88bc24209a566, 0xe0dda58356e5bdd9}, // 1e121 * 2**-274
	{0x9ae7575969460760, 0xcc8a8772164f96a7}, // 1e122 * 2**-278
	{0xc1a12d2fc3978938, 0xffad294e9be37c51}, // 1e123 * 2**-281
	{0xf209787bb47d6b85, 0x3f9873a242dc5b65}, // 1e124 * 2**-284
	{0x9745eb4d50ce6333, 0x07bf484569c9b91f}, // 1e125 * 2**-288
	{0xbd176620a501fc00, 0x49af1a56c43c2767}, // 1e126 * 2**-291
	{0xec5d3fa8ce427b00, 0x5c1ae0ec754b3141}, // 1e127 * 2**-294
	{0x93ba47c980e98ce0, 0x3990cc93c94efec8}, // 1e128 * 2**-298
	{0xb8a8d9bbe123f018, 0x47f4ffb8bba2be7b}, // 1e129 * 2**-301
	{0xe6d3102ad96cec1e, 0x59f23fa6ea8b6e1a}, // 1e130 * 2**-304
	{0x9043ea1ac7e41393, 0x783767c8529724d0}, // 1e131 * 2**-308
	{0xb454e4a179dd1878, 0xd64541ba673cee04}, // 1e132 * 2**-311
	{0xe16a1dc9d8545e95, 0x0bd69229010c2985}, // 1e133 * 2**-314
	{0x8ce2529e2734bb1e, 0xe7661b59a0a799f3}, // 1e134 * 2**-318
	{0xb01ae745b101e9e5, 0xa13fa23008d18070}, // 1e135 * 2**-321
	{0xdc21a1171d42645e, 0x898f8abc0b05e08c}, // 1e136 * 2**-324
	{0x899504ae72497ebb, 0x95f9b6b586e3ac57}, // 1e137 * 2**-328
	{0xabfa45da0edbde6a, 0xfb782462e89c976d}, // 1e138 * 2**-331
	{0xd6f8d7509292d604, 0xba562d7ba2c3bd49}, // 1e139 * 2**-334
	{0x865b86925b9bc5c3, 0xf475dc6d45ba564d}, // 1e140 * 2**-338
	{0xa7f26836f282b733, 0x719353889728ebe1}, // 1e141 * 2**-341
	{0xd1ef0244af236500, 0xcdf8286abcf326d9}, // 1e142 * 2**-344
	{0x8335616aed761f20, 0x80bb1942b617f847}, // 1e143 * 2**-348
	{0xa402b9c5a8d3a6e8, 0xa0e9df93639df659}, // 1e144 * 2**-351
	{0xcd036837130890a2, 0xc92457783c8573f0}, // 1e145 * 2**-354
	{0x802221226be55a65, 0x3db6b6ab25d36876}, // 1e146 * 2**-358
	{0xa02aa96b06deb0fe, 0x0d246455ef484293}, // 1e147 * 2**-361
	{0xc83553c5c8965d3e, 0x906d7d6b6b1a5338}, // 1e148 * 2**-364
	{0xfa42a8b73abbf48d, 0x3488dcc645e0e806}, // 1e149 * 2**-367
	{0x9c69a97284b578d8, 0x00d589fbebac9104}, // 1e150 * 2**-371
	{0xc38413cf25e2d70e, 0x010aec7ae697b545}, // 1e151 * 2**-374
	{0xf46518c2ef5b8cd2, 0x814da799a03da296}, // 1e152 * 2**-377
	{0x98bf2f79d5993803, 0x10d088c00426859e}, // 1e153 * 2**-381
	{0xbeeefb584aff8604, 0x5504aaf005302705}, // 1e154 * 2**-384
	{0xeeaaba2e5dbf6785, 0x6a45d5ac067c30c7}, // 1e155 * 2**-387
	{0x952ab45cfa97a0b3, 0x226ba58b840d9e7c}, // 1e156 * 2**-391
	{0xba756174393d88e0, 0x6b068eee6511061b}, // 1e157 * 2**-394
	{0xe912b9d1478ceb18, 0x85c832a9fe5547a2}, // 1e158 * 2**-397
	{0x91abb422ccb812ef, 0x539d1faa3ef54cc5}, // 1e159 * 2**-401
	{0xb616a12b7fe617ab, 0xa8846794ceb29ff6}, // 1e160 * 2**-404
	{0xe39c49765fdf9d95, 0x12a5817a025f47f4}, // 1e161 * 2**-407
	{0x8e41ade9fbebc27e, 0xeba770ec417b8cf8}, // 1e162 * 2**-411
	{0xb1d219647ae6b31d, 0xa6914d2751da7037}, // 1e163 * 2**-414
	{0xde469fbd99a05fe4, 0x9035a07126510c44}, // 1e164 * 2**-417
	{0x8aec23d680043bef, 0xda218446b7f2a7ab}, // 1e165 * 2**-421
	{0xada72ccc20054aea, 0x50a9e55865ef5195}, // 1e166 * 2**-424
	{0xd910f7ff28069da5, 0xe4d45eae7f6b25fb}, // 1e167 * 2**-427
	{0x87aa9aff79042287, 0x6f04bb2d0fa2f7bd}, // 1e168 * 2**-431
	{0xa99541bf57452b29, 0xcac5e9f8538bb5ac}, // 1e169 * 2**-434
	{0xd3fa922f2d1675f3, 0xbd776476686ea317}, // 1e170 * 2**-437
	{0x847c9b5d7c2e09b8, 0x966a9eca014525ee}, // 1e171 * 2**-441
	{0xa59bc234db398c26, 0xbc05467c81966f6a}, // 1e172 * 2**-444
	{0xcf02b2c21207ef2f, 0x6b06981ba1fc0b44}, // 1e173 * 2**-447
	{0x8161afb94b44f57e, 0xe2e41f11453d870a}, // 1e174 * 2**-451
	{0xa1ba1ba79e1632dd, 0x9b9d26d5968ce8cd}, // 1e175 * 2**-454
	{0xca28a291859bbf94, 0x8284708afc302301}, // 1e176 * 2**-457
	{0xfcb2cb35e702af79, 0xa3258cadbb3c2bc1}, // 1e177 * 2**-460
	{0x9defbf01b061adac, 0xc5f777ec95059b58}, // 1e178 * 2**-464
	{0xc56baec21c7a1917, 0xf77555e7ba47022f}, // 1e179 * 2**-467
	{0xf6c69a72a3989f5c, 0x7552ab61a8d8c2ba}, // 1e180 * 2**-470
	{0x9a3c2087a63f639a, 0xc953ab1d098779b4}, // 1e181 * 2**-474
	{0xc0cb28a98fcf3c80, 0x7ba895e44be95822}, // 1e182 * 2**-477
	{0xf0fdf2d3f3c30ba0, 0x9a92bb5d5ee3ae2a}, // 1e183 * 2**-480
	{0x969eb7c47859e744, 0x609bb51a5b4e4cda}, // 1e184 * 2**-484
	{0xbc4665b596706115, 0x78c2a260f221e011}, // 1e185 * 2**-487
	{0xeb57ff22fc0c795a, 0x56f34af92eaa5815}, // 1e186 * 2**-490
	{0x9316ff75dd87cbd9, 0xf6580edbbd2a770d}, // 1e187 * 2**-494
	{0xb7dcbf5354e9becf, 0xf3ee1292ac7514d0}, // 1e188 * 2**-497
	{0xe5d3ef282a242e82, 0x70e9973757925a05}, // 1e189 * 2**-500
	{0x8fa475791a569d11, 0x0691fe8296bb7843}, // 1e190 * 2**-504
	{0xb38d92d760ec4456, 0xc8367e233c6a5653}, // 1e191 * 2**-507
	{0xe070f78d3927556b, 0x7a441dac0b84ebe8}, // 1e192 * 2**-510
	{0x8c469ab843b89563, 0x6c6a928b87331371}, // 1e193 * 2**-514
	{0xaf58416654a6babc, 0xc785372e68ffd84d}, // 1e194 * 2**-517
	{0xdb2e51bfe9d0696b, 0xf96684fa033fce61}, // 1e195 * 2**-520
	{0x88fcf317f22241e3, 0xbbe0131c4207e0fc}, // 1e196 * 2**-524
	{0xab3c2fddeeaad25b, 0x2ad817e35289d93c}, // 1e197 * 2**-527
	{0xd60b3bd56a5586f2, 0x758e1ddc272c4f8b}, // 1e198 * 2**-530
	{0x85c7056562757457, 0x0978d2a9987bb1b6}, // 1e199 * 2**-534
	{0xa738c6bebb12d16d, 0x4bd70753fe9a9e24}, // 1e200 * 2**-537
	{0xd106f86e69d785c8, 0x1eccc928fe4145ad}, // 1e201 * 2**-540
	{0x82a45b450226b39d, 0x133ffdb99ee8cb8c}, // 1e202 * 2**-544
	{0xa34d721642b06085, 0xd80ffd2806a2fe6f}, // 1e203 * 2**-547
	{0xcc20ce9bd35c78a6, 0xce13fc72084bbe0b}, // 1e204 * 2**-550
	{0xff290242c83396cf, 0x8198fb8e8a5ead8e}, // 1e205 * 2**-553
	{0x9f79a169bd203e42, 0xf0ff9d39167b2c79}, // 1e206 * 2**-557
	{0xc75809c42c684dd2, 0xad3f84875c19f797}, // 1e207 * 2**-560
	{0xf92e0c3537826146, 0x588f65a93320757d}, // 1e208 * 2**-563
	{0x9bbcc7a142b17ccc, 0x77599f89bff4496e}, // 1e209 * 2**-567
	{0xc2abf989935ddbff, 0x9530076c2ff15bca}, // 1e210 * 2**-570
	{0xf356f7ebf83552ff, 0xfa7c09473bedb2bc}, // 1e211 * 2**-573
	{0x98165af37b2153df, 0x3c8d85cc85748fb5}, // 1e212 * 2**-577
	{0xbe1bf1b059e9a8d7, 0x8bb0e73fa6d1b3a3}, // 1e213 * 2**-580
	{0xeda2ee1c7064130d, 0xee9d210f9086208c}, // 1e214 * 2**-583
	{0x9485d4d1c63e8be8, 0x752234a9ba53d457}, // 1e215 * 2**-587
	{0xb9a74a0637ce2ee2, 0x926ac1d428e8c96d}, // 1e216 * 2**-590
	{0xe8111c87c5c1ba9a, 0x370572493322fbc8}, // 1e217 * 2**-593
	{0x910ab1d4db9914a1, 0xe263676dbff5dd5d}, // 1e218 * 2**-597
	{0xb54d5e4a127f59c9, 0xdafc41492ff354b4}, // 1e219 * 2**-600
	{0xe2a0b5dc971f303b, 0xd1bb519b7bf029e2}, // 1e220 * 2**-603
	{0x8da471a9de737e25, 0xa31513012d761a2d}, // 1e221 * 2**-607
	{0xb10d8e1456105dae, 0x8bda57c178d3a0b8}, // 1e222 * 2**-610
	{0xdd50f1996b947519, 0x2ed0edb1d70888e6}, // 1e223 * 2**-613
	{0x8a5296ffe33cc930, 0x7d42948f26655590}, // 1e224 * 2**-617
	{0xace73cbfdc0bfb7c, 0x9c9339b2effeaaf4}, // 1e225 * 2**-620
	{0xd8210befd30efa5b, 0xc3b8081fabfe55b1}, // 1e226 * 2**-623
	{0x8714a775e3e95c79, 0x9a530513cb7ef58e}, // 1e227 * 2**-627
	{0xa8d9d1535ce3b397, 0x80e7c658be5eb2f2}, // 1e228 * 2**-630
	{0xd31045a8341ca07d, 0xe121b7eeedf65faf}, // 1e229 * 2**-633
	{0x83ea2b892091e44e, 0x6cb512f554b9fbcd}, // 1e230 * 2**-637
	{0xa4e4b66b68b65d61, 0x07e257b2a9e87ac0}, // 1e231 * 2**-640
	{0xce1de40642e3f4ba, 0xc9daed9f54629971}, // 1e232 * 2**-643
	{0x80d2ae83e9ce78f4, 0x3e28d48394bd9fe6}, // 1e233 * 2**-647
	{0xa1075a24e4421731, 0x4db309a479ed07e0}, // 1e234 * 2**-650
	{0xc94930ae1d529cfd, 0x211fcc0d986849d8}, // 1e235 * 2**-653
	{0xfb9b7cd9a4a7443d, 0xe967bf10fe825c4e}, // 1e236 * 2**-656
	{0x9d412e0806e88aa6, 0x71e0d76a9f1179b1}, // 1e237 * 2**-660
	{0xc491798a08a2ad4f, 0x0e590d4546d5d81d}, // 1e238 * 2**-663
	{0xf5b5d7ec8acb58a3, 0x51ef5096988b4e24}, // 1e239 * 2**-666
	{0x9991a6f3d6bf1766, 0x5335925e1f5710d6}, // 1e240 * 2**-670
	{0xbff610b0cc6edd40, 0xe802f6f5a72cd50c}, // 1e241 * 2**-673
	{0xeff394dcff8a948f, 0x2203b4b310f80a4f}, // 1e242 * 2**-676
	{0x95f83d0a1fb69cda, 0xb54250efea9b0671}, // 1e243 * 2**-680
	{0xbb764c4ca7a44410, 0x6292e52be541c80e}, // 1e244 * 2**-683
	{0xea53df5fd18d5514, 0x7b379e76de923a12}, // 1e245 * 2**-686
	{0x92746b9be2f8552d, 0xcd02c30a4b1b644b}, // 1e246 * 2**-690
	{0xb7118682dbb66a78, 0xc04373ccdde23d5e}, // 1e247 * 2**-693
	{0xe4d5e82392a40516, 0xf05450c0155accb5}, // 1e248 * 2**-696
	{0x8f05b1163ba6832e, 0xd634b2780d58bff1}, // 1e249 * 2**-700
	{0xb2c71d5bca9023f9, 0x8bc1df1610aeefed}, // 1e250 * 2**-703
	{0xdf78e4b2bd342cf7, 0x6eb256db94daabe9}, // 1e251 * 2**-706
	{0x8bab8eefb6409c1b, 0xe52f76493d08ab71}, // 1e252 * 2**-710
	{0xae9672aba3d0c321, 0x5e7b53db8c4ad64e}, // 1e253 * 2**-713
	{0xda3c0f568cc4f3e9, 0x361a28d26f5d8be1}, // 1e254 * 2**-716
	{0x8865899617fb1872, 0x81d05983859a776d}, // 1e255 * 2**-720
	{0xaa7eebfb9df9de8e, 0x22446fe467011548}, // 1e256 * 2**-723
	{0xd51ea6fa85785632, 0xaad58bdd80c15a9a}, // 1e257 * 2**-726
	{0x8533285c936b35df, 0x2ac5776a7078d8a0}, // 1e258 * 2**-730
	{0xa67ff273b8460357, 0x7576d5450c970ec8}, // 1e259 * 2**-733
	{0xd01fef10a657842d, 0xd2d48a964fbcd27a}, // 1e260 * 2**-736
	{0x8213f56a67f6b29c, 0x63c4d69df1d6038c}, // 1e261 * 2**-740
	{0xa298f2c501f45f43, 0x7cb60c456e4b8470}, // 1e262 * 2**-743
	{0xcb3f2f7642717714, 0xdbe38f56c9de658c}, // 1e263 * 2**-746
	{0xfe0efb53d30dd4d8, 0x12dc732c7c55feef}, // 1e264 * 2**-749
	{0x9ec95d1463e8a507, 0x0bc9c7fbcdb5bf55}, // 1e265 * 2**-753
	{0xc67bb4597ce2ce49, 0x4ebc39fac1232f2a}, // 1e266 * 2**-756
	{0xf81aa16fdc1b81db, 0x226b4879716bfaf5}, // 1e267 * 2**-759
	{0x9b10a4e5e9913129, 0x35830d4be6e37cd9}, // 1e268 * 2**-763
	{0xc1d4ce1f63f57d73, 0x02e3d09ee09c5c0f}, // 1e269 * 2**-766
	{0xf24a01a73cf2dcd0, 0x439cc4c698c37313}, // 1e270 * 2**-769
	{0x976e41088617ca02, 0x2a41fafc1f7a27ec}, // 1e271 * 2**-773
	{0xbd49d14aa79dbc83, 0xb4d279bb2758b1e7}, // 1e272 * 2**-776
	{0xec9c459d51852ba3, 0x22071829f12ede61}, // 1e273 * 2**-779
	{0x93e1ab8252f33b46, 0x35446f1a36bd4afc}, // 1e274 * 2**-783
	{0xb8da1662e7b00a18, 0xc2958ae0c46c9dbc}, // 1e275 * 2**-786
	{0xe7109bfba19c0c9e, 0xf33aed98f587c52b}, // 1e276 * 2**-789
	{0x906a617d450187e3, 0xd804d47f9974db3a}, // 1e277 * 2**-793
	{0xb484f9dc9641e9db, 0x4e06099f7fd21209}, // 1e278 * 2**-796
	{0xe1a63853bbd26452, 0xa1878c075fc6968c}, // 1e279 * 2**-799
	{0x8d07e33455637eb3, 0x24f4b7849bdc1e17}, // 1e280 * 2**-803
	{0xb049dc016abc5e60, 0x6e31e565c2d3259d}, // 1e281 * 2**-806
	{0xdc5c5301c56b75f8, 0x89be5ebf3387ef04}, // 1e282 * 2**-809
	{0x89b9b3e11b6329bb, 0x5616fb378034f562}, // 1e283 * 2**-813
	{0xac2820d9623bf42a, 0xab9cba05604232bb}, // 1e284 * 2**-816
	{0xd732290fbacaf134, 0x5683e886b852bf6a}, // 1e285 * 2**-819
	{0x867f59a9d4bed6c1, 0xb61271543333b7a2}, // 1e286 * 2**-823
	{0xa81f301449ee8c71, 0xa3970da94000a58b}, // 1e287 * 2**-826
	{0xd226fc195c6a2f8d, 0x8c7cd1139000ceee}, // 1e288 * 2**-829
	{0x83585d8fd9c25db8, 0x37ce02ac3a008154}, // 1e289 * 2**-833
	{0xa42e74f3d032f526, 0x45c183574880a1aa}, // 1e290 * 2**-836
	{0xcd3a1230c43fb270, 0xd731e42d1aa0ca14}, // 1e291 * 2**-839
	{0x80444b5e7aa7cf86, 0x867f2e9c30a47e4c}, // 1e292 * 2**-843
	{0xa0555e361951c367, 0x281efa433ccd9de0}, // 1e293 * 2**-846
	{0xc86ab5c39fa63441, 0x7226b8d40c010558}, // 1e294 * 2**-849
	{0xfa856334878fc151, 0x4eb067090f0146ae}, // 1e295 * 2**-852
	{0x9c935e00d4b9d8d3, 0x912e4065a960cc2c}, // 1e296 * 2**-856
	{0xc3b8358109e84f08, 0xf579d07f13b8ff37}, // 1e297 * 2**-859
	{0xf4a642e14c6262c9, 0x32d8449ed8a73f05}, // 1e298 * 2**-862
	{0x98e7e9cccfbd7dbe, 0x7fc72ae347688763}, // 1e299 * 2**-866
	{0xbf21e44003acdd2d, 0x1fb8f59c1942a93c}, // 1e300 * 2**-869
	{0xeeea5d5004981479, 0xe7a733031f93538b}, // 1e301 * 2**-872
	{0x95527a5202df0ccc, 0xf0c87fe1f3bc1437}, // 1e302 * 2**-876
	{0xbaa718e68396cffe, 0x2cfa9fda70ab1945}, // 1e303 * 2**-879
	{0xe950df20247c83fe, 0xb83947d10cd5df96}, // 1e304 * 2**-882
	{0x91d28b7416cdd27f, 0xb323cce2a805abbe}, // 1e305 * 2**-886
	{0xb6472e511c81471e, 0x1fecc01b520716ad}, // 1e306 * 2**-889
	{0xe3d8f9e563a198e6, 0xa7e7f0222688dc59}, // 1e307 * 2**-892
	{0x8e679c2f5e44ff90, 0xa8f0f615581589b7}, // 1e308 * 2**-896
	{0xb201833b35d63f74, 0xd32d339aae1aec25}, // 1e309 * 2**-899
	{0xde81e40a034bcf50, 0x07f8808159a1a72e}, // 1e310 * 2**-902
	{0x8b112e86420f6192, 0x04fb5050d805087d}, // 1e311 * 2**-906
	{0xadd57a27d29339f7, 0x863a24650e064a9c}, // 1e312 * 2**-909
	{0xd94ad8b1c7380875, 0xe7c8ad7e5187dd43}, // 1e313 * 2**-912
	{0x87cec76f1c830549, 0x70dd6c6ef2f4ea4a}, // 1e314 * 2**-916
	{0xa9c2794ae3a3c69b, 0x4d14c78aafb224dd}, // 1e315 * 2**-919
	{0xd433179d9c8cb842, 0xa059f96d5b9eae14}, // 1e316 * 2**-922
	{0x849feec281d7f329, 0x24383be459432ccc}, // 1e317 * 2**-926
	{0xa5c7ea73224deff4, 0xed464add6f93f7ff}, // 1e318 * 2**-929
	{0xcf39e50feae16bf0, 0x2897dd94cb78f5ff}, // 1e319 * 2**-932
	{0x81842f29f2cce376, 0x195eea7cff2b99bf}, // 1e320 * 2**-936
	{0xa1e53af46f801c54, 0x9fb6a51c3ef6802f}, // 1e321 * 2**-939
	{0xca5e89b18b602369, 0xc7a44e634eb4203b}, // 1e322 * 2**-942
	{0xfcf62c1dee382c43, 0xb98d61fc2261284a}, // 1e323 * 2**-945
	{0x9e19db92b4e31baa, 0x93f85d3d957cb92e}, // 1e324 * 2**-949
	{0xc5a05277621be294, 0x38f6748cfadbe77a}, // 1e325 * 2**-952
	{0xf70867153aa2db39, 0x473411b03992e158}, // 1e326 * 2**-955
	{0x9a65406d44a5c904, 0x8c808b0e23fbccd7}, // 1e327 * 2**-959
	{0xc0fe908895cf3b45, 0xafa0add1acfac00d}, // 1e328 * 2**-962
	{0xf13e34aabb430a16, 0x9b88d94618397010}, // 1e329 * 2**-965
	{0x96c6e0eab509e64e, 0xa13587cbcf23e60a}, // 1e330 * 2**-969
	{0xbc789925624c5fe1, 0x4982e9bec2ecdf8d}, // 1e331 * 2**-972
	{0xeb96bf6ebadf77d9, 0x1be3a42e73a81770}, // 1e332 * 2**-975
	{0x933e37a534cbaae8, 0x716e469d08490ea6}, // 1e333 * 2**-979
	{0xb80dc58e81fe95a2, 0x8dc9d8444a5b524f}, // 1e334 * 2**-982
	{0xe61136f2227e3b0a, 0x313c4e555cf226e3}, // 1e335 * 2**-985
	{0x8fcac257558ee4e7, 0xdec5b0f55a17584e}, // 1e336 * 2**-989
	{0xb3bd72ed2af29e20, 0x56771d32b09d2e62}, // 1e337 * 2**-992
	{0xe0accfa875af45a8, 0x6c14e47f5cc479fa}, // 1e338 * 2**-995
	{0x8c6c01c9498d8b89, 0x438d0ecf99facc3c}, // 1e339 * 2**-999
	{0xaf87023b9bf0ee6b, 0x1470528380797f4b}, // 1e340 * 2**-1002
	{0xdb68c2ca82ed2a06, 0x598c67246097df1e}, // 1e341 * 2**-1005
	{0x892179be91d43a44, 0x77f7c076bc5eeb73}, // 1e342 * 2**-1009
	{0xab69d82e364948d5, 0x95f5b0946b76a64f}, // 1e343 * 2**-1012
	{0xd6444e39c3db9b0a, 0x7b731cb986544fe3}, // 1e344 * 2**-1015
	{0x85eab0e41a6940e6, 0x0d27f1f3f3f4b1ee}, // 1e345 * 2**-1019
	{0xa7655d1d21039120, 0x9071ee70f0f1de6a}, // 1e346 * 2**-1022
	{0xd13eb46469447568, 0xb48e6a0d2d2e5604}, // 1e347 * 2**-1025
}

// ---------------------------------------------------------------------------
// Multiprecision decimal numbers (port of internal/strconv/decimal.go).
// For floating-point formatting only; not general purpose.
// Adaptations: goto -> structured if/else in floatBits; for i := range dst
// -> explicit loop; uintSize/maxShift as plain consts (64-bit target).
// ---------------------------------------------------------------------------

type decimal struct {
	d     [800]byte // digits, big-endian representation
	nd    int       // number of digits used
	dp    int       // decimal point
	neg   bool      // negative flag
	trunc bool      // discarded nonzero digits beyond d[:nd]
}

func (a decimal) String() string {
	n := 10 + a.nd
	if a.dp > 0 {
		n += a.dp
	}
	if a.dp < 0 {
		n += -a.dp
	}

	buf := make([]byte, n)
	w := 0
	switch {
	case a.nd == 0:
		return "0"

	case a.dp <= 0:
		// zeros fill space between decimal point and digits
		buf[w] = '0'
		w++
		buf[w] = '.'
		w++
		w += digitZero(buf[w : w+-a.dp])
		w += copy(buf[w:], a.d[0:a.nd])

	case a.dp < a.nd:
		// decimal point in middle of digits
		w += copy(buf[w:], a.d[0:a.dp])
		buf[w] = '.'
		w++
		w += copy(buf[w:], a.d[a.dp:a.nd])

	default:
		// zeros fill space between digits and decimal point
		w += copy(buf[w:], a.d[0:a.nd])
		w += digitZero(buf[w : w+a.dp-a.nd])
	}
	return string(buf[0:w])
}

func digitZero(dst []byte) int {
	for i := 0; i < len(dst); i++ {
		dst[i] = '0'
	}
	return len(dst)
}

// trim trailing zeros from number.
// (They are meaningless; the decimal point is tracked
// independent of the number of digits.)
func trim(a decimal) decimal {
	for a.nd > 0 && a.d[a.nd-1] == '0' {
		a.nd--
	}
	if a.nd == 0 {
		a.dp = 0
	}
	return a
}

// Assign v to a.
func newDecimal(v uint64) decimal {
	var a decimal
	var buf [24]byte

	// Write reversed decimal in buf.
	n := 0
	for v > 0 {
		v1 := v / 10
		v -= 10 * v1
		buf[n] = byte(v + '0')
		n++
		v = v1
	}

	// Reverse again to produce forward decimal in a.d.
	a.nd = 0
	for n--; n >= 0; n-- {
		a.d[a.nd] = buf[n]
		a.nd++
	}
	a.dp = a.nd
	return trim(a)
}

// Maximum shift that we can do in one pass without overflow.
// The synthetic package targets 64-bit only.
const uintSize = 64
const maxShift = uintSize - 4

// Binary shift right (/ 2) by k bits.  k <= maxShift to avoid overflow.
func rightShift(a decimal, k uint) decimal {
	r := 0 // read pointer
	w := 0 // write pointer

	// Pick up enough leading digits to cover first shift.
	var n uint
	for ; n>>k == 0; r++ {
		if r >= a.nd {
			if n == 0 {
				// a == 0; shouldn't get here, but handle anyway.
				a.nd = 0
				return a
			}
			for n>>k == 0 {
				n = n * 10
				r++
			}
			break
		}
		c := uint(a.d[r])
		n = n*10 + c - '0'
	}
	a.dp -= r - 1

	var mask uint = (1 << k) - 1

	// Pick up a digit, put down a digit.
	for ; r < a.nd; r++ {
		c := uint(a.d[r])
		dig := n >> k
		n &= mask
		a.d[w] = byte(dig + '0')
		w++
		n = n*10 + c - '0'
	}

	// Put down extra digits.
	for n > 0 {
		dig := n >> k
		n &= mask
		if w < len(a.d) {
			a.d[w] = byte(dig + '0')
			w++
		} else if dig > 0 {
			a.trunc = true
		}
		n = n * 10
	}

	a.nd = w
	return trim(a)
}

// Cheat sheet for left shift: table indexed by shift count giving
// number of new digits that will be introduced by that shift.
//
// For example, leftcheats[4] = {2, "625"}.  That means that
// if we are shifting by 4 (multiplying by 16), it will add 2 digits
// when the string prefix is "625" through "999", and one fewer digit
// if the string prefix is "000" through "624".
//
// Credit for this trick goes to Ken.

type leftCheat struct {
	delta  int    // number of new digits
	cutoff string // minus one digit if original < a.
}

var leftcheats = []leftCheat{
	// Leading digits of 1/2^i = 5^i.
	{0, ""},
	{1, "5"},             // * 2
	{1, "25"},            // * 4
	{1, "125"},           // * 8
	{2, "625"},           // * 16
	{2, "3125"},          // * 32
	{2, "15625"},         // * 64
	{3, "78125"},         // * 128
	{3, "390625"},        // * 256
	{3, "1953125"},       // * 512
	{4, "9765625"},       // * 1024
	{4, "48828125"},      // * 2048
	{4, "244140625"},     // * 4096
	{4, "1220703125"},    // * 8192
	{5, "6103515625"},    // * 16384
	{5, "30517578125"},   // * 32768
	{5, "152587890625"},  // * 65536
	{6, "762939453125"},  // * 131072
	{6, "3814697265625"}, // * 262144
	{6, "19073486328125"}, // * 524288
	{7, "95367431640625"},        // * 1048576
	{7, "476837158203125"},       // * 2097152
	{7, "2384185791015625"},      // * 4194304
	{7, "11920928955078125"},     // * 8388608
	{8, "59604644775390625"},     // * 16777216
	{8, "298023223876953125"},    // * 33554432
	{8, "1490116119384765625"},   // * 67108864
	{9, "7450580596923828125"},   // * 134217728
	{9, "37252902984619140625"},  // * 268435456
	{9, "186264514923095703125"}, // * 536870912
	{10, "931322574615478515625"},    // * 1073741824
	{10, "4656612873077392578125"},   // * 2147483648
	{10, "23283064365386962890625"},  // * 4294967296
	{10, "116415321826934814453125"}, // * 8589934592
	{11, "582076609134674072265625"},    // * 17179869184
	{11, "2910383045673370361328125"},   // * 34359738368
	{11, "14551915228366851806640625"},  // * 68719476736
	{12, "72759576141834259033203125"},  // * 137438953472
	{12, "363797880709171295166015625"}, // * 274877906944
	{12, "1818989403545856475830078125"}, // * 549755813888
	{13, "9094947017729282379150390625"},    // * 1099511627776
	{13, "45474735088646411895751953125"},   // * 2199023255552
	{13, "227373675443232059478759765625"},  // * 4398046511104
	{13, "1136868377216160297393798828125"}, // * 8796093022208
	{14, "5684341886080801486968994140625"},    // * 17592186044416
	{14, "28421709430404007434844970703125"},   // * 35184372088832
	{14, "142108547152020037174224853515625"},  // * 70368744177664
	{15, "710542735760100185871124267578125"},  // * 140737488355328
	{15, "3552713678800500929355621337890625"}, // * 281474976710656
	{15, "17763568394002504646778106689453125"}, // * 562949953421312
	{16, "88817841970012523233890533447265625"},    // * 1125899906842624
	{16, "444089209850062616169452667236328125"},   // * 2251799813685248
	{16, "2220446049250313080847263336181640625"},  // * 4503599627370496
	{16, "11102230246251565404236316680908203125"}, // * 9007199254740992
	{17, "55511151231257827021181583404541015625"},    // * 18014398509481984
	{17, "277555756156289135105907917022705078125"},   // * 36028797018963968
	{17, "1387778780781445675529539585113525390625"},  // * 72057594037927936
	{18, "6938893903907228377647697925567626953125"},   // * 144115188075855872
	{18, "34694469519536141888238489627838134765625"},  // * 288230376151711744
	{18, "173472347597680709441192448139190673828125"}, // * 576460752303423488
	{19, "867361737988403547205962240695953369140625"}, // * 1152921504606846976
}

// Is the leading prefix of b lexicographically less than s?
func prefixIsLessThan(b []byte, s string) bool {
	for i := 0; i < len(s); i++ {
		if i >= len(b) {
			return true
		}
		if b[i] != s[i] {
			return b[i] < s[i]
		}
	}
	return false
}

// Binary shift left (* 2) by k bits.  k <= maxShift to avoid overflow.
func leftShift(a decimal, k uint) decimal {
	delta := leftcheats[k].delta
	if prefixIsLessThan(a.d[0:a.nd], leftcheats[k].cutoff) {
		delta--
	}

	r := a.nd         // read index
	w := a.nd + delta // write index

	// Pick up a digit, put down a digit.
	var n uint
	for r--; r >= 0; r-- {
		n += (uint(a.d[r]) - '0') << k
		quo := n / 10
		rem := n - 10*quo
		w--
		if w < len(a.d) {
			a.d[w] = byte(rem + '0')
		} else if rem != 0 {
			a.trunc = true
		}
		n = quo
	}

	// Put down extra digits.
	for n > 0 {
		quo := n / 10
		rem := n - 10*quo
		w--
		if w < len(a.d) {
			a.d[w] = byte(rem + '0')
		} else if rem != 0 {
			a.trunc = true
		}
		n = quo
	}

	a.nd += delta
	if a.nd >= len(a.d) {
		a.nd = len(a.d)
	}
	a.dp += delta
	return trim(a)
}

// Binary shift left (k > 0) or right (k < 0).
func (a decimal) Shift(k int) decimal {
	switch {
	case a.nd == 0:
		// nothing to do: a == 0
	case k > 0:
		for k > maxShift {
			a = leftShift(a, maxShift)
			k -= maxShift
		}
		a = leftShift(a, uint(k))
	case k < 0:
		for k < -maxShift {
			a = rightShift(a, maxShift)
			k += maxShift
		}
		a = rightShift(a, uint(-k))
	}
	return a
}

// If we chop a at nd digits, should we round up?
func shouldRoundUp(a decimal, nd int) bool {
	if nd < 0 || nd >= a.nd {
		return false
	}
	if a.d[nd] == '5' && nd+1 == a.nd { // exactly halfway - round to even
		// if we truncated, a little higher than what's recorded - always round up
		if a.trunc {
			return true
		}
		return nd > 0 && (a.d[nd-1]-'0')%2 != 0
	}
	// not halfway - digit tells all
	return a.d[nd] >= '5'
}

// Round a to nd digits (or fewer).
// If nd is zero, it means we're rounding
// just to the left of the digits, as in
// 0.09 -> 0.1.
func (a decimal) Round(nd int) decimal {
	if nd < 0 || nd >= a.nd {
		return a
	}
	if shouldRoundUp(a, nd) {
		return a.RoundUp(nd)
	}
	return a.RoundDown(nd)
}

// Round a down to nd digits (or fewer).
func (a decimal) RoundDown(nd int) decimal {
	if nd < 0 || nd >= a.nd {
		return a
	}
	a.nd = nd
	return trim(a)
}

// Round a up to nd digits (or fewer).
func (a decimal) RoundUp(nd int) decimal {
	if nd < 0 || nd >= a.nd {
		return a
	}

	// round up
	for i := nd - 1; i >= 0; i-- {
		c := a.d[i]
		if c < '9' { // can stop after this digit
			a.d[i]++
			a.nd = i + 1
			return a
		}
	}

	// Number is all 9s.
	// Change to single 1 with adjusted decimal point.
	a.d[0] = '1'
	a.nd = 1
	a.dp++
	return a
}

// Extract integer part, rounded appropriately.
// No guarantees about overflow.
func (a decimal) RoundedInteger() uint64 {
	if a.dp > 20 {
		return 0xFFFFFFFFFFFFFFFF
	}
	var i int
	n := uint64(0)
	for i = 0; i < a.dp && i < a.nd; i++ {
		n = n*10 + uint64(a.d[i]-'0')
	}
	for ; i < a.dp; i++ {
		n *= 10
	}
	if shouldRoundUp(a, a.dp) {
		n++
	}
	return n
}

// ---------------------------------------------------------------------------
// Binary to decimal floating point conversion (port of internal/strconv/ftoa.go).
//
// Adaptations: no unsafe (ftoa32/ftoa64 are already specialized, so the
// Sizeof switch is dropped); math/bits.Len64 -> len64u; builtin min/max ->
// explicit ifs; for range n -> classic loops; lowerhex/upperhex declared
// once here (quote.go port shares them).
// ---------------------------------------------------------------------------

const (
	lowerhex = "0123456789abcdef"
	upperhex = "0123456789ABCDEF"
)

const (
	float32MantBits = 23
	float32ExpBits  = 8
	float32Bias     = -127
	float32MinExp   = -189

	float64MantBits = 52
	float64ExpBits  = 11
	float64Bias     = -1023
	float64MinExp   = -1085
)

// FormatFloat converts the floating-point number f to a string,
// according to the format fmt and precision prec. It rounds the
// result assuming that the original was obtained from a floating-point
// value of bitSize bits (32 for float32, 64 for float64).
//
// The format fmt is one of
//   - 'b' (-ddddp±ddd, a binary exponent),
//   - 'e' (-d.dddde±dd, a decimal exponent),
//   - 'E' (-d.ddddE±dd, a decimal exponent),
//   - 'f' (-ddd.dddd, no exponent),
//   - 'g' ('e' for large exponents, 'f' otherwise),
//   - 'G' ('E' for large exponents, 'f' otherwise),
//   - 'x' (-0xd.ddddp±ddd, a hexadecimal fraction and binary exponent), or
//   - 'X' (-0Xd.ddddP±ddd, a hexadecimal fraction and binary exponent).
//
// The precision prec controls the number of digits (excluding the exponent)
// printed by the 'e', 'E', 'f', 'x', and 'X' formats.
// For 'e', 'E', 'f', 'x', and 'X', it is the number of digits after the decimal point.
// For 'g' and 'G' it is the maximum number of significant digits (trailing
// zeros are removed).
// The special precision -1 uses the smallest number of digits
// necessary such that ParseFloat will return f exactly.
// The exponent is written as a decimal integer;
// for all formats other than 'b', it will be at least two digits.
func FormatFloat(f float64, fmt byte, prec, bitSize int) string {
	if bitSize == 32 {
		n := prec + 4
		if n < 24 {
			n = 24
		}
		return string(ftoa32(make([]byte, 0, n), float32(f), fmt, prec))
	}
	if bitSize == 64 {
		n := prec + 4
		if n < 24 {
			n = 24
		}
		return string(ftoa64(make([]byte, 0, n), f, fmt, prec))
	}
	panic("strconv: illegal FormatFloat bitSize")
}

// AppendFloat appends the string form of the floating-point number f,
// as generated by [FormatFloat], to dst and returns the extended buffer.
func AppendFloat(dst []byte, f float64, fmt byte, prec, bitSize int) []byte {
	if bitSize == 32 {
		return ftoa32(dst, float32(f), fmt, prec)
	}
	if bitSize == 64 {
		return ftoa64(dst, f, fmt, prec)
	}
	panic("strconv: illegal AppendFloat bitSize")
}

func ftoa32(dst []byte, val float32, fmt byte, prec int) []byte {
	b := uint64(float32bits(val))
	expBits := float32ExpBits
	mantBits := float32MantBits
	bias := float32Bias

	neg := b>>(expBits+mantBits) != 0
	exp := int(b>>mantBits) & (1<<expBits - 1)
	mant := b & (1<<mantBits - 1)
	if exp == 1<<expBits-1 {
		if mant != 0 {
			return append(dst, "NaN"...)
		}
		if neg {
			return append(dst, "-Inf"...)
		}
		return append(dst, "+Inf"...)
	}
	if exp == 0 {
		exp++
	} else {
		mant |= 1 << mantBits
	}
	exp += bias

	// Pick off easy binary, hex formats.
	if fmt == 'b' {
		return fmtB(dst, neg, mant, exp-mantBits)
	}
	if fmt == 'x' || fmt == 'X' {
		return fmtX(dst, prec, fmt, neg, mant, exp, mantBits)
	}

	// Pick off zero.
	if mant == 0 {
		return fmtEFG(dst, neg, nil, 0, 0, prec, fmt, prec < 0)
	}

	// Negative precision means "only as much as needed to be exact."
	if prec < 0 {
		// Use fast unrounded scaling.
		var buf [32]byte
		s := 64 - len64u(mant)
		m := mant << s
		e := exp - s
		r := shortFloat32(m, e-mantBits)
		d, p := r.d, r.p
		sd := setDigits(buf[:], d, p, numDigits(d))
		dp, nd := sd.dp, sd.nd
		// Precision for shortest representation mode.
		switch fmt {
		case 'e', 'E':
			prec = nd - 1
			if prec < 0 {
				prec = 0
			}
		case 'f':
			prec = nd - dp
			if prec < 0 {
				prec = 0
			}
		case 'g', 'G':
			prec = nd
		}
		return fmtEFG(dst, neg, buf[:], dp, nd, prec, fmt, true)
	}

	if optimize {
		// Fixed number of digits.
		digits := prec
		switch fmt {
		case 'f':
			// %f precision specifies digits after the decimal point.
			// Estimate an upper bound on the total number of digits needed.
			// ftoaFixed will shorten as needed according to prec.
			if exp >= 0 {
				digits = 1 + log10Pow2(1+exp) + prec
			} else {
				digits = 1 + prec - log10Pow2(-exp)
			}
		case 'e', 'E':
			digits++
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			digits = prec
		default:
			// Invalid mode.
			digits = 1
		}
		if digits <= 18 {
			// digits <= 0 happens for %f on very small numbers
			// and means that we're guaranteed to print all zeros.
			var buf [24]byte
			var dp, nd int
			if digits > 0 {
				s := 64 - len64u(mant)
				m := mant << s
				e := exp - s
				r := fixedWidthFloat(m, e-mantBits, digits, prec, fmt)
				d, p := r.d, r.p
				if d != 0 {
					sd := setDigits(buf[:], d, p, numDigits(d))
					dp, nd = sd.dp, sd.nd
				}
			}
			return fmtEFG(dst, neg, buf[:], dp, nd, prec, fmt, false)
		}
	}

	// Slow bignum case. Only for non-shortest results.
	d := newDecimal(mant)
	d = d.Shift(exp - mantBits)
	switch fmt {
	case 'e', 'E':
		d = d.Round(prec + 1)
	case 'f':
		d = d.Round(d.dp + prec)
	case 'g', 'G':
		if prec == 0 {
			prec = 1
		}
		d = d.Round(prec)
	}
	return fmtEFG(dst, neg, d.d[:], d.dp, d.nd, prec, fmt, false)
}

func ftoa64(dst []byte, val float64, fmt byte, prec int) []byte {
	b := float64bits(val)
	expBits := float64ExpBits
	mantBits := float64MantBits
	bias := float64Bias

	neg := b>>(expBits+mantBits) != 0
	exp := int(b>>mantBits) & (1<<expBits - 1)
	mant := b & (1<<mantBits - 1)
	if exp == 1<<expBits-1 {
		if mant != 0 {
			return append(dst, "NaN"...)
		}
		if neg {
			return append(dst, "-Inf"...)
		}
		return append(dst, "+Inf"...)
	}
	if exp == 0 {
		exp++
	} else {
		mant |= 1 << mantBits
	}
	exp += bias

	// Pick off easy binary, hex formats.
	if fmt == 'b' {
		return fmtB(dst, neg, mant, exp-mantBits)
	}
	if fmt == 'x' || fmt == 'X' {
		return fmtX(dst, prec, fmt, neg, mant, exp, mantBits)
	}

	// Pick off zero.
	if mant == 0 {
		return fmtEFG(dst, neg, nil, 0, 0, prec, fmt, prec < 0)
	}

	// Negative precision means "only as much as needed to be exact."
	if prec < 0 {
		// Use fast unrounded scaling.
		var buf [32]byte
		s := 64 - len64u(mant)
		m := mant << s
		e := exp - s
		r := shortFloat64(m, e-mantBits)
		d, p := r.d, r.p
		sd := setDigits(buf[:], d, p, numDigits(d))
		dp, nd := sd.dp, sd.nd
		// Precision for shortest representation mode.
		switch fmt {
		case 'e', 'E':
			prec = nd - 1
			if prec < 0 {
				prec = 0
			}
		case 'f':
			prec = nd - dp
			if prec < 0 {
				prec = 0
			}
		case 'g', 'G':
			prec = nd
		}
		return fmtEFG(dst, neg, buf[:], dp, nd, prec, fmt, true)
	}

	if optimize {
		// Fixed number of digits.
		digits := prec
		switch fmt {
		case 'f':
			// %f precision specifies digits after the decimal point.
			// Estimate an upper bound on the total number of digits needed.
			// ftoaFixed will shorten as needed according to prec.
			if exp >= 0 {
				digits = 1 + log10Pow2(1+exp) + prec
			} else {
				digits = 1 + prec - log10Pow2(-exp)
			}
		case 'e', 'E':
			digits++
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			digits = prec
		default:
			// Invalid mode.
			digits = 1
		}
		if digits <= 18 {
			// digits <= 0 happens for %f on very small numbers
			// and means that we're guaranteed to print all zeros.
			var buf [24]byte
			var dp, nd int
			if digits > 0 {
				s := 64 - len64u(mant)
				m := mant << s
				e := exp - s
				r := fixedWidthFloat(m, e-mantBits, digits, prec, fmt)
				d, p := r.d, r.p
				if d != 0 {
					sd := setDigits(buf[:], d, p, numDigits(d))
					dp, nd = sd.dp, sd.nd
				}
			}
			return fmtEFG(dst, neg, buf[:], dp, nd, prec, fmt, false)
		}
	}

	// Slow bignum case. Only for non-shortest results.
	d := newDecimal(mant)
	d = d.Shift(exp - mantBits)
	switch fmt {
	case 'e', 'E':
		d = d.Round(prec + 1)
	case 'f':
		d = d.Round(d.dp + prec)
	case 'g', 'G':
		if prec == 0 {
			prec = 1
		}
		d = d.Round(prec)
	}
	return fmtEFG(dst, neg, d.d[:], d.dp, d.nd, prec, fmt, false)
}

func fmtEFG(dst []byte, neg bool, s []byte, dp, nd, prec int, fmt byte, shortest bool) []byte {
	if fmt == 'g' || fmt == 'G' {
		// trailing fractional zeros in 'e' form will be trimmed.
		eprec := prec
		if eprec > nd && nd >= dp {
			eprec = nd
		}
		// %e is used if the exponent from the conversion
		// is less than -4 or greater than or equal to the precision.
		// if precision was the shortest possible, use precision 6 for this decision.
		if shortest {
			eprec = 6
		}
		exp := dp - 1
		if exp < -4 || exp >= eprec {
			if prec > nd {
				prec = nd
			}
			prec--
			fmt = fmt + 'e' - 'g'
		} else {
			if prec > dp {
				prec = nd
			}
			prec = prec - dp
			if prec < 0 {
				prec = 0
			}
			fmt = 'f'
		}
	}

	switch fmt {
	case 'e', 'E': // %e: -d.ddddde±dd
		// sign
		if neg {
			dst = append(dst, '-')
		}

		// first digit
		ch := byte('0')
		if nd != 0 {
			ch = s[0]
		}
		dst = append(dst, ch)

		// .moredigits
		if prec > 0 {
			dst = append(dst, '.')
			i := 1
			m := nd
			if m > prec+1 {
				m = prec + 1
			}
			if i < m {
				dst = append(dst, s[i:m]...)
				i = m
			}
			for j := 0; j < prec+1-i; j++ {
				dst = append(dst, '0')
			}
		}

		// e±
		dst = append(dst, fmt)
		exp := dp - 1
		if nd == 0 { // special case: 0 has exponent 0
			exp = 0
		}
		if exp < 0 {
			ch = '-'
			exp = -exp
		} else {
			ch = '+'
		}
		dst = append(dst, ch)

		// dd or ddd
		switch {
		case exp < 10:
			dst = append(dst, '0', byte(exp)+'0')
		case exp < 100:
			dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
		default:
			dst = append(dst, byte(exp/100)+'0', byte(exp/10)%10+'0', byte(exp%10)+'0')
		}
		return dst

	case 'f': // %f: -ddddddd.ddddd
		// sign
		if neg {
			dst = append(dst, '-')
		}

		// integer, padded with zeros as needed.
		if dp > 0 {
			m := nd
			if m > dp {
				m = dp
			}
			for _, c := range s[:m] {
				dst = append(dst, c)
			}
			for j := 0; j < dp-m; j++ {
				dst = append(dst, '0')
			}
		} else {
			dst = append(dst, '0')
		}

		// fraction
		if prec > 0 {
			dst = append(dst, '.')
			negdp := 0
			if -dp > 0 {
				negdp = -dp
			}
			lz := prec // leading zeros
			if lz > negdp {
				lz = negdp
			}
			off := dp + lz
			m := prec - lz // middle digits
			nom := nd - off
			if nom < 0 {
				nom = 0
			}
			if m > nom {
				m = nom
			}
			tz := prec - lz - m // trailing zeros
			if tz < 0 {
				tz = 0
			}
			for j := 0; j < lz; j++ {
				dst = append(dst, '0')
			}
			for i := 0; i < m; i++ {
				dst = append(dst, s[off+i])
			}
			for j := 0; j < tz; j++ {
				dst = append(dst, '0')
			}
		}
		return dst
	}

	// unknown format
	return append(dst, '%', fmt)
}

// %b: -ddddddddp±ddd
func fmtB(dst []byte, neg bool, mant uint64, exp int) []byte {
	if neg {
		dst = append(dst, '-')
	}
	dst = AppendUint(dst, mant, 10)
	dst = append(dst, 'p')
	if exp >= 0 {
		dst = append(dst, '+')
	}
	dst = AppendInt(dst, int64(exp), 10)
	return dst
}

// %x: -0x1.yyyyyyyyp±ddd or -0x0p+0. (y is hex digit, d is decimal digit)
func fmtX(dst []byte, prec int, fmt byte, neg bool, mant uint64, exp, mantBits int) []byte {
	if mant == 0 {
		exp = 0
	}

	// Shift digits so leading 1 (if any) is at bit 1<<60.
	// TODO: Is this the right way to handle subnormals?
	mant <<= 60 - mantBits
	for mant != 0 && mant&(1<<60) == 0 {
		mant <<= 1
		exp--
	}

	// Round if requested.
	if prec >= 0 && prec < 15 {
		shift := uint(prec * 4)
		extra := (mant << shift) & (1<<60 - 1)
		mant >>= 60 - shift
		if extra|(mant&1) > 1<<59 {
			mant++
		}
		mant <<= 60 - shift
		if mant&(1<<61) != 0 {
			// Wrapped around.
			mant >>= 1
			exp++
		}
	}

	hex := lowerhex
	if fmt == 'X' {
		hex = upperhex
	}

	// sign, 0x, leading digit
	if neg {
		dst = append(dst, '-')
	}
	dst = append(dst, '0', fmt, '0'+byte((mant>>60)&1))

	// .fraction
	mant <<= 4 // remove leading 0 or 1
	if prec < 0 && mant != 0 {
		dst = append(dst, '.')
		for mant != 0 {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	} else if prec > 0 {
		dst = append(dst, '.')
		for i := 0; i < prec; i++ {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	}

	// p±
	ch := byte('P')
	if fmt == lower(fmt) {
		ch = 'p'
	}
	dst = append(dst, ch)
	if exp < 0 {
		ch = '-'
		exp = -exp
	} else {
		ch = '+'
	}
	dst = append(dst, ch)

	// dd or ddd or dddd
	switch {
	case exp < 100:
		dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
	case exp < 1000:
		dst = append(dst, byte(exp/100)+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	default:
		dst = append(dst, byte(exp/1000)+'0', byte(exp/100)%10+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	}

	return dst
}

// ---------------------------------------------------------------------------
// Decimal to binary floating point conversion (port of internal/strconv/atof.go).
//
// Adaptations: builtin min -> explicit if; goto in floatBits -> structured;
// labeled break in readFloat -> trailing-break restructure; fallthrough in
// special -> restructured (preserves gc's "+nan"-is-syntax-error quirk);
// bool2[uint32]/bool2[uint64] -> monomorphized helpers; multi-result
// functions -> pair structs (WS D).
// ---------------------------------------------------------------------------

type floatInfo struct {
	mantbits uint
	expbits  uint
	bias     int
}

var (
	float32info = floatInfo{float32MantBits, float32ExpBits, float32Bias}
	float64info = floatInfo{float64MantBits, float64ExpBits, float64Bias}
)

var optimize = true // set to false to force slow-path conversions for testing

// commonPrefixLenIgnoreCase returns the length of the common
// prefix of s and prefix, with the character case of s ignored.
// The prefix argument must be all lower-case.
func commonPrefixLenIgnoreCase(s, prefix string) int {
	n := len(prefix)
	if n > len(s) {
		n = len(s)
	}
	for i := 0; i < n; i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return i
		}
	}
	return n
}

// specialResult is the single-result form of special (WS D).
type specialResult struct {
	f  float64
	n  int
	ok bool
}

// special returns the floating-point value for the special,
// possibly signed floating-point representations inf, infinity,
// and NaN. The result is ok if a prefix of s contains one
// of these representations and n is the length of that prefix.
// The character case is ignored.
//
// NOTE: gc's switch uses fallthrough from the sign case into the 'i'
// case, so after an explicit sign only inf/infinity is reachable:
// "+nan" is a syntax error. This restructure preserves that quirk
// exactly (verified against gc).
func special(s string) specialResult {
	if len(s) == 0 {
		return specialResult{}
	}
	sign := 1
	nsign := 0
	signed := false
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			sign = -1
		}
		nsign = 1
		s = s[1:]
		signed = true
	}
	if len(s) > 0 && (s[0] == 'i' || s[0] == 'I') {
		n := commonPrefixLenIgnoreCase(s, "infinity")
		// Anything longer than "inf" is ok, but if we
		// don't have "infinity", only consume "inf".
		if 3 < n && n < 8 {
			n = 3
		}
		if n == 3 || n == 8 {
			return specialResult{infF(sign), nsign + n, true}
		}
		return specialResult{}
	}
	if !signed && len(s) > 0 && (s[0] == 'n' || s[0] == 'N') {
		if commonPrefixLenIgnoreCase(s, "nan") == 3 {
			return specialResult{nanF64(), 3, true}
		}
	}
	return specialResult{}
}

type decimalSetPair struct {
	d  decimal
	ok bool
}

func decimalSet(s string) decimalSetPair {
	var dec decimal
	i := 0
	dec.neg = false
	dec.trunc = false

	// optional sign
	if i >= len(s) {
		return decimalSetPair{dec, false}
	}
	switch s[i] {
	case '+':
		i++
	case '-':
		i++
		dec.neg = true
	}

	// digits
	sawdot := false
	sawdigits := false
	for ; i < len(s); i++ {
		switch {
		case s[i] == '_':
			// readFloat already checked underscores
			continue
		case s[i] == '.':
			if sawdot {
				return decimalSetPair{dec, false}
			}
			sawdot = true
			dec.dp = dec.nd
			continue

		case '0' <= s[i] && s[i] <= '9':
			sawdigits = true
			if s[i] == '0' && dec.nd == 0 { // ignore leading zeros
				dec.dp--
				continue
			}
			if dec.nd < len(dec.d) {
				dec.d[dec.nd] = s[i]
				dec.nd++
			} else if s[i] != '0' {
				dec.trunc = true
			}
			continue
		}
		break
	}
	if !sawdigits {
		return decimalSetPair{dec, false}
	}
	if !sawdot {
		dec.dp = dec.nd
	}

	// optional exponent moves decimal point.
	// if we read a very large, very long number,
	// just be sure to move the decimal point by
	// a lot (say, 100000).  it doesn't matter if it's
	// not the exact number.
	if i < len(s) && lower(s[i]) == 'e' {
		i++
		if i >= len(s) {
			return decimalSetPair{dec, false}
		}
		esign := 1
		switch s[i] {
		case '+':
			i++
		case '-':
			i++
			esign = -1
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return decimalSetPair{dec, false}
		}
		e := 0
		for ; i < len(s) && ('0' <= s[i] && s[i] <= '9' || s[i] == '_'); i++ {
			if s[i] == '_' {
				// readFloat already checked underscores
				continue
			}
			if e < 10000 {
				e = e*10 + int(s[i]) - '0'
			}
		}
		dec.dp += e * esign
	}

	if i != len(s) {
		return decimalSetPair{dec, false}
	}

	return decimalSetPair{dec, true}
}

// readFloatResult is the single-result form of readFloat (WS D).
type readFloatResult struct {
	mantissa uint64
	exp      int
	neg      bool
	trunc    bool
	hex      bool
	i        int
	ok       bool
}

// readFloat reads a decimal or hexadecimal mantissa and exponent from a float
// string representation in s; the number may be followed by other characters.
// readFloat reports the number of bytes consumed (i), and whether the number
// is valid (ok).
func readFloat(s string) readFloatResult {
	var r readFloatResult
	underscores := false
	i := 0

	// optional sign
	if i >= len(s) {
		return r
	}
	switch s[i] {
	case '+':
		i++
	case '-':
		i++
		r.neg = true
	}

	// digits
	base := uint64(10)
	maxMantDigits := 19 // 10^19 fits in uint64
	expChar := byte('e')
	if i+2 < len(s) && s[i] == '0' && lower(s[i+1]) == 'x' {
		base = 16
		maxMantDigits = 16 // 16^16 fits in uint64
		i += 2
		expChar = 'p'
		r.hex = true
	}
	sawdot := false
	sawdigits := false
	nd := 0
	ndMant := 0
	dp := 0
	for ; i < len(s); i++ {
		// NOTE: gc uses a labeled loop: + break loop here to exit the
		// for loop from inside the switch. The trailing break below the
		// switch exits the loop identically (a break inside a switch case
		// only breaks the switch).
		switch c := s[i]; true {
		case c == '_':
			underscores = true
			continue

		case c == '.':
			if sawdot {
				break
			}
			sawdot = true
			dp = nd
			continue

		case '0' <= c && c <= '9':
			sawdigits = true
			if c == '0' && nd == 0 { // ignore leading zeros
				dp--
				continue
			}
			nd++
			if ndMant < maxMantDigits {
				r.mantissa *= base
				r.mantissa += uint64(c - '0')
				ndMant++
			} else if c != '0' {
				r.trunc = true
			}
			continue

		case base == 16 && 'a' <= lower(c) && lower(c) <= 'f':
			sawdigits = true
			nd++
			if ndMant < maxMantDigits {
				r.mantissa *= 16
				r.mantissa += uint64(lower(c) - 'a' + 10)
				ndMant++
			} else {
				r.trunc = true
			}
			continue
		}
		break
	}
	if !sawdigits {
		return r
	}
	if !sawdot {
		dp = nd
	}

	if base == 16 {
		dp *= 4
		ndMant *= 4
	}

	// optional exponent moves decimal point.
	// if we read a very large, very long number,
	// just be sure to move the decimal point by
	// a lot (say, 100000).  it doesn't matter if it's
	// not the exact number.
	if i < len(s) && lower(s[i]) == expChar {
		i++
		if i >= len(s) {
			return r
		}
		esign := 1
		switch s[i] {
		case '+':
			i++
		case '-':
			i++
			esign = -1
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return r
		}
		e := 0
		for ; i < len(s) && ('0' <= s[i] && s[i] <= '9' || s[i] == '_'); i++ {
			if s[i] == '_' {
				underscores = true
				continue
			}
			if e < 10000 {
				e = e*10 + int(s[i]) - '0'
			}
		}
		dp += e * esign
	} else if base == 16 {
		// Must have exponent.
		return r
	}

	if r.mantissa != 0 {
		r.exp = dp - ndMant
	}

	if underscores && !underscoreOK(s[:i]) {
		return r
	}

	r.i = i
	r.ok = true
	return r
}

// decimal power of ten to binary power of two.
var powtab = []int{1, 3, 6, 9, 13, 16, 19, 23, 26}

// floatBitsResult is the single-result form of decimal.floatBits (WS D).
type floatBitsResult struct {
	b        uint64
	overflow bool
}

func (d decimal) floatBits(flt floatInfo) floatBitsResult {
	var exp int
	var mant uint64
	overflow := false

	// NOTE: gc uses goto out/overflow here; restructured into if/else
	// with identical control flow (verified by code review: each goto
	// target's assignments are reproduced in the corresponding branch).

	// Zero is always a special case.
	if d.nd == 0 {
		mant = 0
		exp = flt.bias
	} else if d.dp > 310 {
		// Obvious overflow. These bounds are for 64-bit floats.
		// ±Inf
		mant = 0
		exp = 1<<flt.expbits - 1 + flt.bias
		overflow = true
	} else if d.dp < -330 {
		// Obvious underflow: zero.
		mant = 0
		exp = flt.bias
	} else {
		// Scale by powers of two until in range [0.5, 1.0)
		exp = 0
		for d.dp > 0 {
			var n int
			if d.dp >= len(powtab) {
				n = 27
			} else {
				n = powtab[d.dp]
			}
			d = d.Shift(-n)
			exp += n
		}
		for d.dp < 0 || d.dp == 0 && d.d[0] < '5' {
			var n int
			if -d.dp >= len(powtab) {
				n = 27
			} else {
				n = powtab[-d.dp]
			}
			d = d.Shift(n)
			exp -= n
		}

		// Our range is [0.5,1) but floating point range is [1,2).
		exp--

		// Minimum representable exponent is flt.bias+1.
		// If the exponent is smaller, move it up and
		// adjust d accordingly.
		if exp < flt.bias+1 {
			n := flt.bias + 1 - exp
			d = d.Shift(-n)
			exp += n
		}

		if exp-flt.bias >= 1<<flt.expbits-1 {
			// ±Inf
			mant = 0
			exp = 1<<flt.expbits - 1 + flt.bias
			overflow = true
		} else {
			// Extract 1+flt.mantbits bits.
			d = d.Shift(int(1 + flt.mantbits))
			mant = d.RoundedInteger()

			// Rounding might have added a bit; shift down.
			if mant == 2<<flt.mantbits {
				mant >>= 1
				exp++
				if exp-flt.bias >= 1<<flt.expbits-1 {
					// ±Inf
					mant = 0
					exp = 1<<flt.expbits - 1 + flt.bias
					overflow = true
				}
			}

			// Denormalized?
			if !overflow && mant&(1<<flt.mantbits) == 0 {
				exp = flt.bias
			}
		}
	}

	// Assemble bits.
	bits := mant & (uint64(1)<<flt.mantbits - 1)
	bits |= uint64((exp-flt.bias)&(1<<flt.expbits-1)) << flt.mantbits
	if d.neg {
		bits |= 1 << flt.mantbits << flt.expbits
	}
	return floatBitsResult{bits, overflow}
}

// Exact powers of 10.
var float64pow10 = []float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
	1e20, 1e21, 1e22,
}
var float32pow10 = []float32{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10}

// exactResult is the single-result form of (float64, bool) (WS D).
type exactResult struct {
	f  float64
	ok bool
}

// exact32Result is the single-result form of (float32, bool) (WS D).
type exact32Result struct {
	f  float32
	ok bool
}

// If possible to convert decimal representation to 64-bit float f exactly,
// entirely in floating-point math, do so, avoiding the expense of decimalToFloatBits.
// Three common cases:
//
//	value is exact integer
//	value is exact integer * exact power of ten
//	value is exact integer / exact power of ten
//
// These all produce potentially inexact but correctly rounded answers.
func atof64exact(mantissa uint64, exp int, neg bool) exactResult {
	if mantissa>>float64info.mantbits != 0 {
		return exactResult{}
	}
	f := float64(mantissa)
	if neg {
		f = -f
	}
	switch {
	case exp == 0:
		// an integer.
		return exactResult{f, true}
	// Exact integers are <= 10^15.
	// Exact powers of ten are <= 10^22.
	case exp > 0 && exp <= 15+22: // int * 10^k
		// If exponent is big but number of digits is not,
		// can move a few zeros into the integer part.
		if exp > 22 {
			f *= float64pow10[exp-22]
			exp = 22
		}
		if f > 1e15 || f < -1e15 {
			// the exponent was really too large.
			return exactResult{}
		}
		return exactResult{f * float64pow10[exp], true}
	case exp < 0 && exp >= -22: // int / 10^k
		return exactResult{f / float64pow10[-exp], true}
	}
	return exactResult{}
}

// If possible to compute mantissa*10^exp to 32-bit float f exactly,
// entirely in floating-point math, do so, avoiding the machinery above.
func atof32exact(mantissa uint64, exp int, neg bool) exact32Result {
	if mantissa>>float32MantBits != 0 {
		return exact32Result{}
	}
	f := float32(mantissa)
	if neg {
		f = -f
	}
	switch {
	case exp == 0:
		return exact32Result{f, true}
	// Exact integers are <= 10^7.
	// Exact powers of ten are <= 10^10.
	case exp > 0 && exp <= 7+10: // int * 10^k
		// If exponent is big but number of digits is not,
		// can move a few zeros into the integer part.
		if exp > 10 {
			f *= float32pow10[exp-10]
			exp = 10
		}
		if f > 1e7 || f < -1e7 {
			// the exponent was really too large.
			return exact32Result{}
		}
		return exact32Result{f * float32pow10[exp], true}
	case exp < 0 && exp >= -10: // int / 10^k
		return exact32Result{f / float32pow10[-exp], true}
	}
	return exact32Result{}
}

// atofHex converts the hex floating-point string s
// to a rounded float32 or float64 value (depending on flt==&float32info or flt==&float64info)
// and returns it as a float64.
// The string s has already been parsed into a mantissa, exponent, and sign (neg==true for negative).
// If trunc is true, trailing non-zero bits have been omitted from the mantissa.
func atofHex(s string, flt floatInfo, mantissa uint64, exp int, neg, trunc bool) floatErrResult {
	maxExp := 1<<flt.expbits + flt.bias - 2
	minExp := flt.bias + 1
	exp += int(flt.mantbits) // mantissa now implicitly divided by 2^mantbits.

	// Shift mantissa and exponent to bring representation into float range.
	// Eventually we want a mantissa with a leading 1-bit followed by mantbits other bits.
	// For rounding, we need two more, where the bottom bit represents
	// whether that bit or any later bit was non-zero.
	// (If the mantissa has already lost non-zero bits, trunc is true,
	// and we OR in a 1 below after shifting left appropriately.)
	for mantissa != 0 && mantissa>>(flt.mantbits+2) == 0 {
		mantissa <<= 1
		exp--
	}
	if trunc {
		mantissa |= 1
	}
	for mantissa>>(1+flt.mantbits+2) != 0 {
		mantissa = mantissa>>1 | mantissa&1
		exp++
	}

	// If exponent is too negative,
	// denormalize in hopes of making it representable.
	// (The -2 is for the rounding bits.)
	for mantissa > 1 && exp < minExp-2 {
		mantissa = mantissa>>1 | mantissa&1
		exp++
	}

	// Round using two bottom bits.
	round := mantissa & 3
	mantissa >>= 2
	round |= mantissa & 1 // round to even (round up if mantissa is odd)
	exp += 2
	if round == 3 {
		mantissa++
		if mantissa == 1<<(1+flt.mantbits) {
			mantissa >>= 1
			exp++
		}
	}

	if mantissa>>flt.mantbits == 0 { // Denormal or zero.
		exp = flt.bias
	}
	var err error
	if exp > maxExp { // infinity and range error
		mantissa = 1 << flt.mantbits
		exp = maxExp + 1
		err = errRange
	}

	bits := mantissa & (1<<flt.mantbits - 1)
	bits |= uint64((exp-flt.bias)&(1<<flt.expbits-1)) << flt.mantbits
	if neg {
		bits |= 1 << flt.mantbits << flt.expbits
	}
	if flt.mantbits == 23 {
		return floatErrResult{float64(float32frombits(uint32(bits))), err}
	}
	return floatErrResult{float64frombits(bits), err}
}

// atofResult is the single-result form of (float64, int, error) (WS D).
type atofResult struct {
	f   float64
	n   int
	err error
}

// atof32Result is the single-result form of (float32, int, error) (WS D).
type atof32Result struct {
	f   float32
	n   int
	err error
}

func atof32(s string) atof32Result {
	if sp := special(s); sp.ok {
		return atof32Result{float32(sp.f), sp.n, nil}
	}

	rf := readFloat(s)
	d, p, neg, trunc, hex, n, ok := rf.mantissa, rf.exp, rf.neg, rf.trunc, rf.hex, rf.i, rf.ok
	if !ok {
		return atof32Result{0, n, errSyntax}
	}

	if hex {
		fr := atofHex(s[:n], float32info, d, p, neg, trunc)
		return atof32Result{float32(fr.f), n, fr.err}
	}

	if optimize {
		sign := bool2uint32(neg) << 31
		if d == 0 {
			return atof32Result{float32frombits(sign | 0), n, nil}
		}
		if p > 40 { // overflow to ±Inf
			return atof32Result{float32frombits(sign | 0xff<<23), n, errRange}
		}
		if p < -70 { // underflow to ±0
			return atof32Result{float32frombits(sign | 0), n, nil}
		}
		if !trunc {
			// Exact rounding with single multiplication or division.
			if er := atof32exact(d, p, neg); er.ok {
				return atof32Result{er.f, n, nil}
			}
		}
		// Use fast unrounded scaling.
		// The only possible err is errRange, when the result overflows to ±Inf.
		fr := parseFloat32(d, p, sign)
		f, err := fr.f, fr.err
		if !trunc {
			return atof32Result{f, n, err}
		}
		// If additional digits were truncated from d
		// but d+1 converts to the same value,
		// then the additional digits don't matter.
		fr1 := parseFloat32(d+1, p, sign)
		f1 := fr1.f
		if f == f1 {
			return atof32Result{f, n, err}
		}
	}

	// Slow fallback.
	dsr := decimalSet(s[:n])
	dec, ok := dsr.d, dsr.ok
	if !ok {
		return atof32Result{0, n, errSyntax}
	}
	fb := dec.floatBits(float32info)
	f := float32frombits(uint32(fb.b))
	err := error(nil)
	if fb.overflow {
		err = errRange
	}
	return atof32Result{f, n, err}
}

func atof64(s string) atofResult {
	if sp := special(s); sp.ok {
		return atofResult{sp.f, sp.n, nil}
	}

	rf := readFloat(s)
	d, p, neg, trunc, hex, n, ok := rf.mantissa, rf.exp, rf.neg, rf.trunc, rf.hex, rf.i, rf.ok
	if !ok {
		return atofResult{0, n, errSyntax}
	}
	if hex {
		fr := atofHex(s[:n], float64info, d, p, neg, trunc)
		return atofResult{fr.f, n, fr.err}
	}
	if optimize {
		sign := bool2uint64(neg) << 63
		if d == 0 {
			return atofResult{float64frombits(sign | 0), n, nil}
		}
		if p > 310 { // overflow to ±Inf
			return atofResult{float64frombits(sign | 0x7ff<<52), n, errRange}
		}
		if p < -345 { // underflow to ±0
			return atofResult{float64frombits(sign | 0), n, nil}
		}
		if !trunc {
			// Exact rounding with single multiplication or division.
			if er := atof64exact(d, p, neg); er.ok {
				return atofResult{er.f, n, nil}
			}
		}
		// Use fast unrounded scaling.
		// The only possible err is errRange, when the result overflows to ±Inf.
		fr := parseFloat64(d, p, sign)
		f, err := fr.f, fr.err
		if !trunc {
			return atofResult{f, n, err}
		}
		// If additional digits were truncated from d
		// but d+1 converts to the same value,
		// then the additional digits don't matter.
		fr1 := parseFloat64(d+1, p, sign)
		f1 := fr1.f
		if f == f1 {
			return atofResult{f, n, err}
		}
	}

	// Slow fallback.
	dsr := decimalSet(s[:n])
	dec, ok := dsr.d, dsr.ok
	if !ok {
		return atofResult{0, n, errSyntax}
	}
	fb := dec.floatBits(float64info)
	f := float64frombits(fb.b)
	err := error(nil)
	if fb.overflow {
		err = errRange
	}
	return atofResult{f, n, err}
}

// parseFloatPrefix parses the float prefix of s (WS D: single result).
func parseFloatPrefix(s string, bitSize int) atofResult {
	if bitSize == 32 {
		r := atof32(s)
		return atofResult{float64(r.f), r.n, r.err}
	}
	return atof64(s)
}

// ---------------------------------------------------------------------------
// Bool/complex conversions (ports of internal/strconv/atob.go, atoc.go,
// ctoa.go). Public multi-result functions keep exact gc signatures with
// unreachable panic bodies; real logic is in the __-helpers (WS D).
// ---------------------------------------------------------------------------

// parseBoolPair is the single-result form of ParseBool (WS D).
type parseBoolPair struct {
	v   bool
	err error
}

// __parseBool is the real ParseBool implementation (single-result).
func __parseBool(str string) parseBoolPair {
	switch str {
	case "1", "t", "T", "true", "TRUE", "True":
		return parseBoolPair{true, nil}
	case "0", "f", "F", "false", "FALSE", "False":
		return parseBoolPair{false, nil}
	}
	return parseBoolPair{false, errSyntax}
}

// __parseBoolPub is the public-shape single-result ParseBool (error mapping).
func __parseBoolPub(str string) parseBoolPair {
	pr := __parseBool(str)
	if pr.err != nil {
		return parseBoolPair{pr.v, toError("ParseBool", str, 0, 0, pr.err)}
	}
	return pr
}

// parseComplexPair is the single-result form of ParseComplex (WS D).
type parseComplexPair struct {
	v   complex128
	err error
}

// __parseComplexPub is the public-shape single-result ParseComplex (error mapping).
func __parseComplexPub(s string, bitSize int) parseComplexPair {
	pr := __parseComplex(s, bitSize)
	if pr.err != nil {
		return parseComplexPair{pr.v, toError("ParseComplex", s, 0, bitSize, pr.err)}
	}
	return pr
}

// __parseComplex is the real ParseComplex implementation (single-result).
func __parseComplex(s string, bitSize int) parseComplexPair {
	size := 64
	if bitSize == 64 {
		size = 32 // complex64 uses float32 parts
	}

	// Remove parentheses, if any.
	if len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		s = s[1 : len(s)-1]
	}

	var pending error // pending range error, or nil

	// Read real part (possibly imaginary part if followed by 'i').
	pr := parseFloatPrefix(s, size)
	re, n, err := pr.f, pr.n, pr.err
	if err != nil {
		if err != errRangeError {
			return parseComplexPair{0, err}
		}
		pending = err
	}
	s = s[n:]

	// If we have nothing left, we're done.
	if len(s) == 0 {
		return parseComplexPair{complex(re, 0), pending}
	}

	// Otherwise, look at the next character.
	switch s[0] {
	case '+':
		// Consume the '+' to avoid an error if we have "+NaNi", but
		// do this only if we don't have a "++" (don't hide that error).
		if len(s) > 1 && s[1] != '+' {
			s = s[1:]
		}
	case '-':
		// ok
	case 'i':
		// If 'i' is the last character, we only have an imaginary part.
		// Otherwise it is a syntax error (gc's fallthrough into default).
		if len(s) == 1 {
			return parseComplexPair{complex(0, re), pending}
		}
		return parseComplexPair{0, errSyntax}
	default:
		return parseComplexPair{0, errSyntax}
	}

	// Read imaginary part.
	pi := parseFloatPrefix(s, size)
	im, n, err := pi.f, pi.n, pi.err
	if err != nil {
		if err != errRangeError {
			return parseComplexPair{0, err}
		}
		pending = err
	}
	s = s[n:]
	if s != "i" {
		return parseComplexPair{0, errSyntax}
	}
	return parseComplexPair{complex(re, im), pending}
}

// appendComplex appends the result of FormatComplex to dst.
// (gc's internal AppendComplex, unexported here: there is no public
// strconv.AppendComplex.)
func appendComplex(dst []byte, c complex128, fmt byte, prec, bitSize int) []byte {
	if bitSize != 64 && bitSize != 128 {
		panic("invalid bitSize")
	}
	bitSize >>= 1 // complex64 uses float32 internally

	dst = append(dst, '(')
	dst = AppendFloat(dst, real(c), fmt, prec, bitSize)
	i := len(dst)
	dst = AppendFloat(dst, imag(c), fmt, prec, bitSize)
	// Check if imaginary part has a sign. If not, add one.
	if dst[i] != '+' && dst[i] != '-' {
		dst = append(dst, 0)
		copy(dst[i+1:], dst[i:])
		dst[i] = '+'
	}
	dst = append(dst, "i)"...)
	return dst
}

// ---------------------------------------------------------------------------
// Public API: errors, NumError, and the exact-gc-signature wrappers
// (port of strconv/number.go).
//
// Multi-result public functions (ParseBool, ParseInt, ParseUint, ParseFloat,
// ParseComplex, Atoi) have unreachable panic bodies: v1 cannot emit or call
// multi-result functions (WS D), so call sites desugar to the __-helpers
// (mrdesugar_m5.go; the Appendix M __parseDuration precedent).
// ---------------------------------------------------------------------------

// ErrRange indicates that a value is out of range for the target type.
var ErrRange = errors.New("value out of range")

// ErrSyntax indicates that a value does not have the right syntax for the target type.
var ErrSyntax = errors.New("invalid syntax")

// A NumError records a failed conversion.
type NumError struct {
	Func string // the failing function (ParseBool, ParseInt, ParseUint, ParseFloat, ParseComplex)
	Num  string // the input
	Err  error  // the reason the conversion failed (e.g. ErrRange, ErrSyntax, etc.)
}

func (e *NumError) Error() string {
	return "strconv." + e.Func + ": " + "parsing " + Quote(e.Num) + ": " + e.Err.Error()
}

func (e *NumError) Unwrap() error { return e.Err }

// toError converts from parseError to the error guaranteed by this package's APIs.
func toError(fn, s string, base, bitSize int, err error) error {
	switch err {
	case errSyntax:
		return syntaxError(fn, s)
	case errRange:
		return rangeError(fn, s)
	case errBase:
		return baseError(fn, s, base)
	case errBitSize:
		return bitSizeError(fn, s, bitSize)
	}
	return err
}

func syntaxError(fn, str string) *NumError {
	return &NumError{fn, str, ErrSyntax}
}

func rangeError(fn, str string) *NumError {
	return &NumError{fn, str, ErrRange}
}

func baseError(fn, str string, base int) *NumError {
	return &NumError{fn, str, errors.New("invalid base " + Itoa(base))}
}

func bitSizeError(fn, str string, bitSize int) *NumError {
	return &NumError{fn, str, errors.New("invalid bit size " + Itoa(bitSize))}
}

// ParseBool returns the boolean value represented by the string.
// It accepts 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False.
// Any other value returns an error.
func ParseBool(str string) (bool, error) {
	panic("strconv.ParseBool: unreachable (call sites desugar to __parseBoolPub)")
}

// FormatBool returns "true" or "false" according to the value of b.
func FormatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// AppendBool appends "true" or "false", according to the value of b,
// to dst and returns the extended buffer.
func AppendBool(dst []byte, b bool) []byte {
	if b {
		return append(dst, "true"...)
	}
	return append(dst, "false"...)
}

// ParseComplex converts the string s to a complex number
// with the precision specified by bitSize: 64 for complex64, or 128 for complex128.
// When bitSize=64, the result still has type complex128, but it will be
// convertible to complex64 without changing its value.
//
// The number represented by s must be of the form N, Ni, or N±Ni, where N stands
// for a floating-point number as recognized by [ParseFloat], and i is the imaginary
// component. If the second N is unsigned, a + sign is required between the two components
// as indicated by the ±. If the second N is NaN, only a + sign is accepted.
// The form may be parenthesized and cannot contain any spaces.
// The resulting complex number consists of the two components converted by ParseFloat.
//
// The errors that ParseComplex returns have concrete type [*NumError]
// and include err.Num = s.
//
// If s is not syntactically well-formed, ParseComplex returns err.Err = ErrSyntax.
//
// If s is syntactically well-formed but either component is more than 1/2 ULP
// away from the largest floating point number of the given component's size,
// ParseComplex returns err.Err = ErrRange and c = ±Inf for the respective component.
func ParseComplex(s string, bitSize int) (complex128, error) {
	panic("strconv.ParseComplex: unreachable (call sites desugar to __parseComplexPub)")
}

// ParseFloat converts the string s to a floating-point number
// with the precision specified by bitSize: 32 for float32, or 64 for float64.
// When bitSize=32, the result still has type float64, but it will be
// convertible to float32 without changing its value.
//
// ParseFloat accepts decimal and hexadecimal floating-point numbers
// as defined by the Go syntax for [floating-point literals].
// If s is well-formed and near a valid floating-point number,
// ParseFloat returns the nearest floating-point number rounded
// using IEEE754 unbiased rounding.
// (Parsing a hexadecimal floating-point value only rounds when
// there are more bits in the hexadecimal representation than
// will fit in the mantissa.)
//
// The errors that ParseFloat returns have concrete type *NumError
// and include err.Num = s.
//
// If s is not syntactically well-formed, ParseFloat returns err.Err = ErrSyntax.
//
// If s is syntactically well-formed but is more than 1/2 ULP
// away from the largest floating point number of the given size,
// ParseFloat returns f = ±Inf, err.Err = ErrRange.
//
// ParseFloat recognizes the string "NaN", and the (possibly signed) strings "Inf" and "Infinity"
// as their respective special floating point values. It ignores case when matching.
//
// [floating-point literals]: https://go.dev/ref/spec#Floating-point_literals
func ParseFloat(s string, bitSize int) (float64, error) {
	panic("strconv.ParseFloat: unreachable (call sites desugar to __parseFloat)")
}

// parseFloatPair is the single-result form of ParseFloat (WS D).
type parseFloatPair struct {
	v   float64
	err error
}

// __parseFloat is the real ParseFloat implementation (single-result).
func __parseFloat(s string, bitSize int) parseFloatPair {
	pr := parseFloatPrefix(s, bitSize)
	f, n, err := pr.f, pr.n, pr.err
	if n != len(s) {
		return parseFloatPair{0, toError("ParseFloat", s, 0, bitSize, errSyntax)}
	}
	if err != nil {
		return parseFloatPair{f, toError("ParseFloat", s, 0, bitSize, err)}
	}
	return parseFloatPair{f, nil}
}

// ParseUint is like [ParseInt] but for unsigned numbers.
//
// A sign prefix is not permitted.
func ParseUint(s string, base int, bitSize int) (uint64, error) {
	panic("strconv.ParseUint: unreachable (call sites desugar to __parseUintPub)")
}

// __parseUintPub is the public-shape single-result ParseUint (error mapping).
func __parseUintPub(s string, base int, bitSize int) parseUintPair {
	pr := __parseUint(s, base, bitSize)
	if pr.err != nil {
		return parseUintPair{pr.v, toError("ParseUint", s, base, bitSize, pr.err)}
	}
	return pr
}

// ParseInt interprets a string s in the given base (0, 2 to 36) and
// bit size (0 to 64) and returns the corresponding value i.
//
// The string may begin with a leading sign: "+" or "-".
//
// If the base argument is 0, the true base is implied by the string's
// prefix following the sign (if present): 2 for "0b", 8 for "0" or "0o",
// 16 for "0x", and 10 otherwise. Also, for argument base 0 only,
// underscore characters are permitted as defined by the Go syntax for
// [integer literals].
//
// The bitSize argument specifies the integer type
// that the result must fit into. Bit sizes 0, 8, 16, 32, and 64
// correspond to int, int8, int16, int32, and int64.
// If bitSize is below 0 or above 64, an error is returned.
//
// The errors that ParseInt returns have concrete type [*NumError]
// and include err.Num = s. If s is empty or contains invalid
// digits, err.Err = [ErrSyntax] and the returned value is 0;
// if the value corresponding to s cannot be represented by a
// signed integer of the given size, err.Err = [ErrRange] and the
// returned value is the maximum magnitude integer of the
// appropriate bitSize and sign.
//
// [integer literals]: https://go.dev/ref/spec#Integer_literals
func ParseInt(s string, base int, bitSize int) (int64, error) {
	panic("strconv.ParseInt: unreachable (call sites desugar to __parseIntPub)")
}

// __parseIntPub is the public-shape single-result ParseInt (error mapping).
func __parseIntPub(s string, base int, bitSize int) parseIntPair {
	pr := __parseInt(s, base, bitSize)
	if pr.err != nil {
		return parseIntPair{pr.v, toError("ParseInt", s, base, bitSize, pr.err)}
	}
	return pr
}

// Atoi is equivalent to ParseInt(s, 10, 0), converted to type int.
func Atoi(s string) (int, error) {
	panic("strconv.Atoi: unreachable (call sites desugar to __atoiPub)")
}

// __atoiPub is the public-shape single-result Atoi (error mapping).
func __atoiPub(s string) atoiPair {
	pr := __atoi(s)
	if pr.err != nil {
		return atoiPair{pr.v, toError("Atoi", s, 10, 0, pr.err)}
	}
	return pr
}

// FormatComplex converts the complex number c to a string of the
// form (a+bi) where a and b are the real and imaginary parts,
// formatted according to the format fmt and precision prec.
//
// The format fmt and precision prec have the same meaning as in [FormatFloat].
// It rounds the result assuming that the original was obtained from a complex
// value of bitSize bits, which must be 64 for complex64 and 128 for complex128.
func FormatComplex(c complex128, fmt byte, prec, bitSize int) string {
	var buf [64]byte
	return string(appendComplex(buf[:0], c, fmt, prec, bitSize))
}

// ---------------------------------------------------------------------------
// Quoting (port of strconv/quote.go).
//
// Adaptations: unicode/utf8 is not an available import, so DecodeRuneInString,
// ValidRune, AppendRune, ValidString and the RuneError/RuneSelf constants are
// inlined below (accept-range rules identical to unicode/utf8; the direct
// range-check form follows the proven synth_strings.go port). bytealg's
// index -> indexByte loop. bsearch[S ~[]E, ...] (generic) -> bsearch16/
// bsearch32. Public multi-result Unquote/UnquoteChar/QuotedPrefix keep exact
// gc signatures with unreachable panic bodies; real logic in __-helpers
// (WS D).
// ---------------------------------------------------------------------------

const (
	quoteRuneError = '\uFFFD' // == utf8.RuneError
	quoteRuneSelf  = 0x80     // == utf8.RuneSelf
	quoteMaxRune   = '\U0010FFFF'
)

// decodeRuneInString decodes the first UTF-8 encoding in s.
// Behaviorally identical to utf8.DecodeRuneInString.
type runePair struct {
	r    rune
	size int
}

func decodeRuneInString(s string) runePair {
	n := len(s)
	if n == 0 {
		return runePair{quoteRuneError, 0}
	}
	c0 := s[0]
	if c0 < quoteRuneSelf {
		return runePair{rune(c0), 1}
	}
	if c0 < 0xE0 {
		// 2-byte sequence: lead 0xC2-0xDF (0xC0-0xC1 are overlong).
		if c0 < 0xC2 || n < 2 {
			return runePair{quoteRuneError, 1}
		}
		c1 := s[1]
		if c1 < 0x80 || c1 >= 0xC0 {
			return runePair{quoteRuneError, 1}
		}
		return runePair{rune(c0&0x1F)<<6 | rune(c1&0x3F), 2}
	}
	if c0 < 0xF0 {
		// 3-byte sequence: lead 0xE0-0xEF.
		if n < 3 {
			return runePair{quoteRuneError, 1}
		}
		c1, c2 := s[1], s[2]
		if c1 < 0x80 || c1 >= 0xC0 || c2 < 0x80 || c2 >= 0xC0 {
			return runePair{quoteRuneError, 1}
		}
		if c0 == 0xE0 && c1 < 0xA0 {
			return runePair{quoteRuneError, 1} // overlong
		}
		if c0 == 0xED && c1 >= 0xA0 {
			return runePair{quoteRuneError, 1} // surrogate
		}
		return runePair{rune(c0&0x0F)<<12 | rune(c1&0x3F)<<6 | rune(c2&0x3F), 3}
	}
	if c0 < 0xF5 {
		// 4-byte sequence: lead 0xF0-0xF4.
		if n < 4 {
			return runePair{quoteRuneError, 1}
		}
		c1, c2, c3 := s[1], s[2], s[3]
		if c1 < 0x80 || c1 >= 0xC0 || c2 < 0x80 || c2 >= 0xC0 || c3 < 0x80 || c3 >= 0xC0 {
			return runePair{quoteRuneError, 1}
		}
		if c0 == 0xF0 && c1 < 0x90 {
			return runePair{quoteRuneError, 1} // overlong
		}
		if c0 == 0xF4 && c1 >= 0x90 {
			return runePair{quoteRuneError, 1} // > MaxRune
		}
		return runePair{rune(c0&0x07)<<18 | rune(c1&0x3F)<<12 | rune(c2&0x3F)<<6 | rune(c3&0x3F), 4}
	}
	return runePair{quoteRuneError, 1}
}

// validRune reports whether r is a valid Unicode code point.
// Behaviorally identical to utf8.ValidRune.
func validRune(r rune) bool {
	if r < 0 || r > quoteMaxRune {
		return false
	}
	// Surrogates are invalid.
	if 0xD800 <= r && r <= 0xDFFF {
		return false
	}
	return true
}

// appendRune appends the UTF-8 encoding of r to buf.
// Behaviorally identical to utf8.AppendRune.
func appendRune(buf []byte, r rune) []byte {
	if !validRune(r) {
		r = quoteRuneError
	}
	switch {
	case r < 0x80:
		return append(buf, byte(r))
	case r < 0x800:
		return append(buf, byte(0xC0|r>>6), byte(0x80|r&0x3F))
	case r < 0x10000:
		return append(buf, byte(0xE0|r>>12), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	default:
		return append(buf, byte(0xF0|r>>18), byte(0x80|r>>12&0x3F), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
	}
}

// validString reports whether s is a valid UTF-8 string.
// Behaviorally identical to utf8.ValidString.
func validString(s string) bool {
	for i := 0; i < len(s); {
		rp := decodeRuneInString(s[i:])
		r, w := rp.r, rp.size
		if r == quoteRuneError && w == 1 {
			return false
		}
		i += w
	}
	return true
}

// indexByte returns the index of the first instance of c in s, or -1 if missing.
// (gc's bytealg index; the bootstrap loop version.)
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// contains reports whether the string contains the byte c.
func contains(s string, c byte) bool {
	return indexByte(s, c) != -1
}

func quoteWith(s string, quote byte, ASCIIonly, graphicOnly bool) string {
	return string(appendQuotedWith(make([]byte, 0, 3*len(s)/2), s, quote, ASCIIonly, graphicOnly))
}

func quoteRuneWith(r rune, quote byte, ASCIIonly, graphicOnly bool) string {
	return string(appendQuotedRuneWith(nil, r, quote, ASCIIonly, graphicOnly))
}

func appendQuotedWith(buf []byte, s string, quote byte, ASCIIonly, graphicOnly bool) []byte {
	// Often called with big strings, so preallocate. If there's quoting,
	// this is conservative but still helps a lot.
	if cap(buf)-len(buf) < len(s) {
		nBuf := make([]byte, len(buf), len(buf)+1+len(s)+1)
		copy(nBuf, buf)
		buf = nBuf
	}
	buf = append(buf, quote)
	for r, width := rune(0), 0; len(s) > 0; s = s[width:] {
		rwp := decodeRuneInString(s)
		r, width = rwp.r, rwp.size
		if width == 1 && r == quoteRuneError {
			buf = append(buf, "\\x"...)
			buf = append(buf, lowerhex[s[0]>>4])
			buf = append(buf, lowerhex[s[0]&0xF])
			continue
		}
		buf = appendEscapedRune(buf, r, quote, ASCIIonly, graphicOnly)
	}
	buf = append(buf, quote)
	return buf
}

func appendQuotedRuneWith(buf []byte, r rune, quote byte, ASCIIonly, graphicOnly bool) []byte {
	buf = append(buf, quote)
	if !validRune(r) {
		r = quoteRuneError
	}
	buf = appendEscapedRune(buf, r, quote, ASCIIonly, graphicOnly)
	buf = append(buf, quote)
	return buf
}

func appendEscapedRune(buf []byte, r rune, quote byte, ASCIIonly, graphicOnly bool) []byte {
	if r == rune(quote) || r == '\\' { // always backslashed
		buf = append(buf, '\\')
		buf = append(buf, byte(r))
		return buf
	}
	if ASCIIonly {
		if r < quoteRuneSelf && IsPrint(r) {
			buf = append(buf, byte(r))
			return buf
		}
	} else if IsPrint(r) || graphicOnly && isInGraphicList(r) {
		return appendRune(buf, r)
	}
	switch r {
	case '\a':
		buf = append(buf, "\\a"...)
	case '\b':
		buf = append(buf, "\\b"...)
	case '\f':
		buf = append(buf, "\\f"...)
	case '\n':
		buf = append(buf, "\\n"...)
	case '\r':
		buf = append(buf, "\\r"...)
	case '\t':
		buf = append(buf, "\\t"...)
	case '\v':
		buf = append(buf, "\\v"...)
	default:
		switch {
		case r < ' ' || r == 0x7f:
			buf = append(buf, "\\x"...)
			buf = append(buf, lowerhex[byte(r)>>4])
			buf = append(buf, lowerhex[byte(r)&0xF])
		default:
			// (gc uses fallthrough from !validRune into the \u branch;
			// spelled out: 0xFFFD < 0x10000.)
			if !validRune(r) {
				r = 0xFFFD
			}
			if r < 0x10000 {
				buf = append(buf, "\\u"...)
				for s := 12; s >= 0; s -= 4 {
					buf = append(buf, lowerhex[r>>uint(s)&0xF])
				}
			} else {
				buf = append(buf, "\\U"...)
				for s := 28; s >= 0; s -= 4 {
					buf = append(buf, lowerhex[r>>uint(s)&0xF])
				}
			}
		}
	}
	return buf
}

// Quote returns a double-quoted Go string literal representing s. The
// returned string uses Go escape sequences (\t, \n, \xFF, \u0100) for
// control characters and non-printable characters as defined by
// [IsPrint].
func Quote(s string) string {
	return quoteWith(s, '"', false, false)
}

// AppendQuote appends a double-quoted Go string literal representing s,
// as generated by [Quote], to dst and returns the extended buffer.
func AppendQuote(dst []byte, s string) []byte {
	return appendQuotedWith(dst, s, '"', false, false)
}

// QuoteToASCII returns a double-quoted Go string literal representing s.
// The returned string uses Go escape sequences (\t, \n, \xFF, \u0100) for
// non-ASCII characters and non-printable characters as defined by [IsPrint].
func QuoteToASCII(s string) string {
	return quoteWith(s, '"', true, false)
}

// AppendQuoteToASCII appends a double-quoted Go string literal representing s,
// as generated by [QuoteToASCII], to dst and returns the extended buffer.
func AppendQuoteToASCII(dst []byte, s string) []byte {
	return appendQuotedWith(dst, s, '"', true, false)
}

// QuoteToGraphic returns a double-quoted Go string literal representing s.
// The returned string leaves Unicode graphic characters, as defined by
// [IsGraphic], unchanged and uses Go escape sequences (\t, \n, \xFF, \u0100)
// for non-graphic characters.
func QuoteToGraphic(s string) string {
	return quoteWith(s, '"', false, true)
}

// AppendQuoteToGraphic appends a double-quoted Go string literal representing s,
// as generated by [QuoteToGraphic], to dst and returns the extended buffer.
func AppendQuoteToGraphic(dst []byte, s string) []byte {
	return appendQuotedWith(dst, s, '"', false, true)
}

// QuoteRune returns a single-quoted Go character literal representing the
// rune. The returned string uses Go escape sequences (\t, \n, \xFF, \u0100)
// for control characters and non-printable characters as defined by [IsPrint].
// If r is not a valid Unicode code point, it is interpreted as the Unicode
// replacement character U+FFFD.
func QuoteRune(r rune) string {
	return quoteRuneWith(r, '\'', false, false)
}

// AppendQuoteRune appends a single-quoted Go character literal representing the rune,
// as generated by [QuoteRune], to dst and returns the extended buffer.
func AppendQuoteRune(dst []byte, r rune) []byte {
	return appendQuotedRuneWith(dst, r, '\'', false, false)
}

// QuoteRuneToASCII returns a single-quoted Go character literal representing
// the rune. The returned string uses Go escape sequences (\t, \n, \xFF,
// \u0100) for non-ASCII characters and non-printable characters as defined
// by [IsPrint].
// If r is not a valid Unicode code point, it is interpreted as the Unicode
// replacement character U+FFFD.
func QuoteRuneToASCII(r rune) string {
	return quoteRuneWith(r, '\'', true, false)
}

// AppendQuoteRuneToASCII appends a single-quoted Go character literal representing the rune,
// as generated by [QuoteRuneToASCII], to dst and returns the extended buffer.
func AppendQuoteRuneToASCII(dst []byte, r rune) []byte {
	return appendQuotedRuneWith(dst, r, '\'', true, false)
}

// QuoteRuneToGraphic returns a single-quoted Go character literal representing
// the rune. If the rune is not a Unicode graphic character,
// as defined by [IsGraphic], the returned string will use a Go escape sequence
// (\t, \n, \xFF, \u0100).
// If r is not a valid Unicode code point, it is interpreted as the Unicode
// replacement character U+FFFD.
func QuoteRuneToGraphic(r rune) string {
	return quoteRuneWith(r, '\'', false, true)
}

// AppendQuoteRuneToGraphic appends a single-quoted Go character literal representing the rune,
// as generated by [QuoteRuneToGraphic], to dst and returns the extended buffer.
func AppendQuoteRuneToGraphic(dst []byte, r rune) []byte {
	return appendQuotedRuneWith(dst, r, '\'', false, true)
}

// CanBackquote reports whether the string s can be represented
// unchanged as a single-line backquoted string without control
// characters other than tab.
func CanBackquote(s string) bool {
	for len(s) > 0 {
		rp := decodeRuneInString(s)
		r, wid := rp.r, rp.size
		s = s[wid:]
		if wid > 1 {
			if r == '\ufeff' {
				return false // BOMs are invisible and should not be quoted.
			}
			continue // All other multibyte runes are correctly encoded and assumed printable.
		}
		if r == quoteRuneError {
			return false
		}
		if (r < ' ' && r != '\t') || r == 0x60 || r == '\u007F' {
			return false
		}
	}
	return true
}

type unhexPair struct {
	v  rune
	ok bool
}

func unhex(b byte) unhexPair {
	c := rune(b)
	switch {
	case '0' <= c && c <= '9':
		return unhexPair{c - '0', true}
	case 'a' <= c && c <= 'f':
		return unhexPair{c - 'a' + 10, true}
	case 'A' <= c && c <= 'F':
		return unhexPair{c - 'A' + 10, true}
	}
	return unhexPair{0, false}
}

// unquoteCharPair is the single-result form of UnquoteChar (WS D).
type unquoteCharPair struct {
	value     rune
	multibyte bool
	tail      string
	err       error
}

// UnquoteChar decodes the first character or byte in the escaped string
// or character literal represented by the string s.
// It returns four values:
//
//  1. value, the decoded Unicode code point or byte value;
//  2. multibyte, a boolean indicating whether the decoded character requires a multibyte UTF-8 representation;
//  3. tail, the remainder of the string after the character; and
//  4. an error that will be nil if the character is syntactically valid.
//
// The second argument, quote, specifies the type of literal being parsed
// and therefore which escaped quote character is permitted.
// If set to a single quote, it permits the sequence \' and disallows unescaped '.
// If set to a double quote, it permits \" and disallows unescaped ".
// If set to zero, it does not permit either escape and allows both quote characters to appear unescaped.
func UnquoteChar(s string, quote byte) (value rune, multibyte bool, tail string, err error) {
	panic("strconv.UnquoteChar: unreachable (call sites desugar to __unquoteChar)")
}

// __unquoteChar is the real UnquoteChar implementation (single-result).
func __unquoteChar(s string, quote byte) unquoteCharPair {
	// easy cases
	if len(s) == 0 {
		return unquoteCharPair{0, false, "", ErrSyntax}
	}
	switch c := s[0]; {
	case c == quote && (quote == '\'' || quote == '"'):
		return unquoteCharPair{0, false, "", ErrSyntax}
	case c >= quoteRuneSelf:
		rp := decodeRuneInString(s)
		r, size := rp.r, rp.size
		return unquoteCharPair{r, true, s[size:], nil}
	case c != '\\':
		return unquoteCharPair{rune(s[0]), false, s[1:], nil}
	}

	// hard case: c is backslash
	if len(s) <= 1 {
		return unquoteCharPair{0, false, "", ErrSyntax}
	}
	c := s[1]
	s = s[2:]

	switch c {
	case 'a':
		return unquoteCharPair{'\a', false, s, nil}
	case 'b':
		return unquoteCharPair{'\b', false, s, nil}
	case 'f':
		return unquoteCharPair{'\f', false, s, nil}
	case 'n':
		return unquoteCharPair{'\n', false, s, nil}
	case 'r':
		return unquoteCharPair{'\r', false, s, nil}
	case 't':
		return unquoteCharPair{'\t', false, s, nil}
	case 'v':
		return unquoteCharPair{'\v', false, s, nil}
	case 'x', 'u', 'U':
		n := 0
		switch c {
		case 'x':
			n = 2
		case 'u':
			n = 4
		case 'U':
			n = 8
		}
		var v rune
		if len(s) < n {
			return unquoteCharPair{0, false, "", ErrSyntax}
		}
		for j := 0; j < n; j++ {
			uhr := unhex(s[j])
			x, ok := uhr.v, uhr.ok
			if !ok {
				return unquoteCharPair{0, false, "", ErrSyntax}
			}
			v = v<<4 | x
		}
		s = s[n:]
		if c == 'x' {
			// single-byte string, possibly not UTF-8
			return unquoteCharPair{v, false, s, nil}
		}
		if !validRune(v) {
			return unquoteCharPair{0, false, "", ErrSyntax}
		}
		return unquoteCharPair{v, true, s, nil}
	case '0', '1', '2', '3', '4', '5', '6', '7':
		v := rune(c) - '0'
		if len(s) < 2 {
			return unquoteCharPair{0, false, "", ErrSyntax}
		}
		for j := 0; j < 2; j++ { // one digit already; two more
			x := rune(s[j]) - '0'
			if x < 0 || x > 7 {
				return unquoteCharPair{0, false, "", ErrSyntax}
			}
			v = (v << 3) | x
		}
		s = s[2:]
		if v > 255 {
			return unquoteCharPair{0, false, "", ErrSyntax}
		}
		return unquoteCharPair{v, false, s, nil}
	case '\\':
		return unquoteCharPair{'\\', false, s, nil}
	case '\'', '"':
		if c != quote {
			return unquoteCharPair{0, false, "", ErrSyntax}
		}
		return unquoteCharPair{rune(c), false, s, nil}
	default:
		return unquoteCharPair{0, false, "", ErrSyntax}
	}
}

// QuotedPrefix returns the quoted string (as understood by [Unquote]) at the prefix of s.
// If s does not start with a valid quoted string, QuotedPrefix returns an error.
func QuotedPrefix(s string) (string, error) {
	panic("strconv.QuotedPrefix: unreachable (call sites desugar to __quotedPrefix)")
}

// quotedPrefixPair is the single-result form of QuotedPrefix (WS D).
type quotedPrefixPair struct {
	v   string
	err error
}

// __quotedPrefix is the real QuotedPrefix implementation (single-result).
func __quotedPrefix(s string) quotedPrefixPair {
	r := __unquote(s, false)
	return quotedPrefixPair{r.v, r.err}
}

// Unquote interprets s as a single-quoted, double-quoted,
// or backquoted Go string literal, returning the string value
// that s quotes.  (If s is single-quoted, it would be a Go
// character literal; Unquote returns the corresponding
// one-character string. For an empty character literal
// Unquote returns the empty string.)
func Unquote(s string) (string, error) {
	panic("strconv.Unquote: unreachable (call sites desugar to __unquotePub)")
}

// unquotePair is the single-result form of Unquote (WS D).
type unquotePair struct {
	v   string
	err error
}

// __unquotePub is the public-shape single-result Unquote.
func __unquotePub(s string) unquotePair {
	r := __unquote(s, true)
	if len(r.tail) > 0 {
		return unquotePair{"", ErrSyntax}
	}
	return unquotePair{r.v, r.err}
}

// unquoteResult is the single-result form of unquote (WS D).
type unquoteResult struct {
	v    string
	tail string
	err  error
}

// unquote parses a quoted string at the start of the input,
// returning the parsed prefix, the remaining suffix, and any parse errors.
// If unescape is true, the parsed prefix is unescaped,
// otherwise the input prefix is provided verbatim.
func __unquote(in string, unescape bool) unquoteResult {
	// Determine the quote form and optimistically find the terminating quote.
	if len(in) < 2 {
		return unquoteResult{"", in, ErrSyntax}
	}
	quote := in[0]
	end := indexByte(in[1:], quote)
	if end < 0 {
		return unquoteResult{"", in, ErrSyntax}
	}
	end += 2 // position after terminating quote; may be wrong if escape sequences are present

	switch quote {
	case 0x60:
		switch {
		case !unescape:
			return unquoteResult{in[:end], in[end:], nil} // include quotes
		case !contains(in[:end], '\r'):
			return unquoteResult{in[1 : end-1], in[end:], nil} // exclude quotes
		default:
			// Carriage return characters ('\r') inside raw string literals
			// are discarded from the raw string value.
			buf := make([]byte, 0, end-1-len("\\r")-1)
			for i := 1; i < end-1; i++ {
				if in[i] != '\r' {
					buf = append(buf, in[i])
				}
			}
			return unquoteResult{string(buf), in[end:], nil}
		}
		// NOTE: Prior implementations did not verify that raw strings consist
		// of valid UTF-8 characters and we continue to not verify it as such.
		// The Go specification does not explicitly require valid UTF-8,
		// but only mention that it is implicitly valid for Go source code
		// (which must be valid UTF-8).
	case '"', '\'':
		// Handle quoted strings without any escape sequences.
		if !contains(in[:end], '\\') && !contains(in[:end], '\n') {
			var valid bool
			switch quote {
			case '"':
				valid = validString(in[1 : end-1])
			case '\'':
				rp := decodeRuneInString(in[len("'") : end-len("'")])
				r, n := rp.r, rp.size
				valid = len("'")+n+len("'") == end && (r != quoteRuneError || n != 1)
			}
			if valid {
				out := in[:end]
				if unescape {
					out = out[1 : end-1] // exclude quotes
				}
				return unquoteResult{out, in[end:], nil}
			}
		}

		// Handle quoted strings with escape sequences.
		var buf []byte
		in0 := in
		in = in[1:] // skip starting quote
		if unescape {
			buf = make([]byte, 0, 3*end/2) // try to avoid more allocations
		}
		for len(in) > 0 && in[0] != quote {
			// Process the next character,
			// rejecting any unescaped newline characters which are invalid.
			qc := __unquoteChar(in, quote)
			r, multibyte, rem, err := qc.value, qc.multibyte, qc.tail, qc.err
			if in[0] == '\n' || err != nil {
				return unquoteResult{"", in0, ErrSyntax}
			}
			in = rem

			// Append the character if unescaping the input.
			if unescape {
				if r < quoteRuneSelf || !multibyte {
					buf = append(buf, byte(r))
				} else {
					buf = appendRune(buf, r)
				}
			}

			// Single quoted strings must be a single character.
			if quote == '\'' {
				break
			}
		}

		// Verify that the string ends with a terminating quote.
		if !(len(in) > 0 && in[0] == quote) {
			return unquoteResult{"", in0, ErrSyntax}
		}
		in = in[1:] // skip terminating quote

		if unescape {
			return unquoteResult{string(buf), in, nil}
		}
		return unquoteResult{in0[:len(in0)-len(in)], in, nil}
	default:
		return unquoteResult{"", in, ErrSyntax}
	}
}

// bsearch16 is bsearch specialized to []uint16 (gc's bsearch[S ~[]E, ...]
// is generic; monomorphized here since v1 has no generics).
type bsearchPair struct {
	i     int
	found bool
}

func bsearch16(s []uint16, v uint16) bsearchPair {
	n := len(s)
	i, j := 0, n
	for i < j {
		h := i + (j-i)>>1
		if s[h] < v {
			i = h + 1
		} else {
			j = h
		}
	}
	return bsearchPair{i, i < n && s[i] == v}
}

// bsearch32 is bsearch specialized to []uint32.
func bsearch32(s []uint32, v uint32) bsearchPair {
	n := len(s)
	i, j := 0, n
	for i < j {
		h := i + (j-i)>>1
		if s[h] < v {
			i = h + 1
		} else {
			j = h
		}
	}
	return bsearchPair{i, i < n && s[i] == v}
}

// IsPrint reports whether the rune is defined as printable by Go, with
// the same definition as [unicode.IsPrint]: letters, numbers, punctuation,
// symbols and ASCII space.
func IsPrint(r rune) bool {
	// Fast check for Latin-1
	if r <= 0xFF {
		if 0x20 <= r && r <= 0x7E {
			// All the ASCII is printable from space through DEL-1.
			return true
		}
		if 0xA1 <= r && r <= 0xFF {
			// Similarly for ¡ through ÿ...
			return r != 0xAD // ...except for the bizarre soft hyphen.
		}
		return false
	}

	// Same algorithm, either on uint16 or uint32 value.
	// First, find first i such that isPrint[i] >= x.
	// This is the index of either the start or end of a pair that might span x.
	// The start is even (isPrint[i&^1]) and the end is odd (isPrint[i|1]).
	// If we find x in a range, make sure x is not in isNotPrint list.

	if 0 <= r && r < 1<<16 {
		rr := uint16(r)
		bsr := bsearch16(isPrint16, rr)
		i := bsr.i
		if i >= len(isPrint16) || rr < isPrint16[i&^1] || isPrint16[i|1] < rr {
			return false
		}
		bsr = bsearch16(isNotPrint16, rr)
		found := bsr.found
		return !found
	}

	rr := uint32(r)
	bsr := bsearch32(isPrint32, rr)
		i := bsr.i
	if i >= len(isPrint32) || rr < isPrint32[i&^1] || isPrint32[i|1] < rr {
		return false
	}
	if r >= 0x20000 {
		return true
	}
	r -= 0x10000
	bsr = bsearch16(isNotPrint32, uint16(r))
	found := bsr.found
	return !found
}

// IsGraphic reports whether the rune is defined as a Graphic by Unicode. Such
// characters include letters, marks, numbers, punctuation, symbols, and
// spaces, from categories L, M, N, P, S, and Zs.
func IsGraphic(r rune) bool {
	if IsPrint(r) {
		return true
	}
	return isInGraphicList(r)
}

// isInGraphicList reports whether the rune is in the isGraphic list. This separation
// from IsGraphic allows quoteWith to avoid two calls to IsPrint.
// Should be called only if IsPrint fails.
func isInGraphicList(r rune) bool {
	// We know r must fit in 16 bits - see makeisprint.go.
	if r > 0xFFFF {
		return false
	}
	bsr := bsearch16(isGraphic, uint16(r))
	found := bsr.found
	return found
}

// ---------------------------------------------------------------------------
// isPrint/isGraphic tables (verbatim from strconv/isprint.go; generated by
// makeisprint.go).
// ---------------------------------------------------------------------------


// (412+132+120)*2 + (566)*4 = 3592 bytes

var isPrint16 = []uint16{
	0x0020, 0x007e,
	0x00a1, 0x0377,
	0x037a, 0x037f,
	0x0384, 0x0556,
	0x0559, 0x058a,
	0x058d, 0x05c7,
	0x05d0, 0x05ea,
	0x05ef, 0x05f4,
	0x0606, 0x070d,
	0x0710, 0x074a,
	0x074d, 0x07b1,
	0x07c0, 0x07fa,
	0x07fd, 0x082d,
	0x0830, 0x085b,
	0x085e, 0x086a,
	0x0870, 0x088f,
	0x0897, 0x098c,
	0x098f, 0x0990,
	0x0993, 0x09b2,
	0x09b6, 0x09b9,
	0x09bc, 0x09c4,
	0x09c7, 0x09c8,
	0x09cb, 0x09ce,
	0x09d7, 0x09d7,
	0x09dc, 0x09e3,
	0x09e6, 0x09fe,
	0x0a01, 0x0a0a,
	0x0a0f, 0x0a10,
	0x0a13, 0x0a39,
	0x0a3c, 0x0a42,
	0x0a47, 0x0a48,
	0x0a4b, 0x0a4d,
	0x0a51, 0x0a51,
	0x0a59, 0x0a5e,
	0x0a66, 0x0a76,
	0x0a81, 0x0ab9,
	0x0abc, 0x0acd,
	0x0ad0, 0x0ad0,
	0x0ae0, 0x0ae3,
	0x0ae6, 0x0af1,
	0x0af9, 0x0b0c,
	0x0b0f, 0x0b10,
	0x0b13, 0x0b39,
	0x0b3c, 0x0b44,
	0x0b47, 0x0b48,
	0x0b4b, 0x0b4d,
	0x0b55, 0x0b57,
	0x0b5c, 0x0b63,
	0x0b66, 0x0b77,
	0x0b82, 0x0b8a,
	0x0b8e, 0x0b95,
	0x0b99, 0x0b9f,
	0x0ba3, 0x0ba4,
	0x0ba8, 0x0baa,
	0x0bae, 0x0bb9,
	0x0bbe, 0x0bc2,
	0x0bc6, 0x0bcd,
	0x0bd0, 0x0bd0,
	0x0bd7, 0x0bd7,
	0x0be6, 0x0bfa,
	0x0c00, 0x0c39,
	0x0c3c, 0x0c4d,
	0x0c55, 0x0c5d,
	0x0c60, 0x0c63,
	0x0c66, 0x0c6f,
	0x0c77, 0x0cb9,
	0x0cbc, 0x0ccd,
	0x0cd5, 0x0cd6,
	0x0cdc, 0x0ce3,
	0x0ce6, 0x0cf3,
	0x0d00, 0x0d4f,
	0x0d54, 0x0d63,
	0x0d66, 0x0d96,
	0x0d9a, 0x0dbd,
	0x0dc0, 0x0dc6,
	0x0dca, 0x0dca,
	0x0dcf, 0x0ddf,
	0x0de6, 0x0def,
	0x0df2, 0x0df4,
	0x0e01, 0x0e3a,
	0x0e3f, 0x0e5b,
	0x0e81, 0x0ebd,
	0x0ec0, 0x0ed9,
	0x0edc, 0x0edf,
	0x0f00, 0x0f6c,
	0x0f71, 0x0fda,
	0x1000, 0x10c7,
	0x10cd, 0x10cd,
	0x10d0, 0x124d,
	0x1250, 0x125d,
	0x1260, 0x128d,
	0x1290, 0x12b5,
	0x12b8, 0x12c5,
	0x12c8, 0x1315,
	0x1318, 0x135a,
	0x135d, 0x137c,
	0x1380, 0x1399,
	0x13a0, 0x13f5,
	0x13f8, 0x13fd,
	0x1400, 0x169c,
	0x16a0, 0x16f8,
	0x1700, 0x1715,
	0x171f, 0x1736,
	0x1740, 0x1753,
	0x1760, 0x1773,
	0x1780, 0x17dd,
	0x17e0, 0x17e9,
	0x17f0, 0x17f9,
	0x1800, 0x1819,
	0x1820, 0x1878,
	0x1880, 0x18aa,
	0x18b0, 0x18f5,
	0x1900, 0x192b,
	0x1930, 0x193b,
	0x1940, 0x1940,
	0x1944, 0x196d,
	0x1970, 0x1974,
	0x1980, 0x19ab,
	0x19b0, 0x19c9,
	0x19d0, 0x19da,
	0x19de, 0x1a1b,
	0x1a1e, 0x1a7c,
	0x1a7f, 0x1a89,
	0x1a90, 0x1a99,
	0x1aa0, 0x1aad,
	0x1ab0, 0x1add,
	0x1ae0, 0x1aeb,
	0x1b00, 0x1bf3,
	0x1bfc, 0x1c37,
	0x1c3b, 0x1c49,
	0x1c4d, 0x1c8a,
	0x1c90, 0x1cba,
	0x1cbd, 0x1cc7,
	0x1cd0, 0x1cfa,
	0x1d00, 0x1f15,
	0x1f18, 0x1f1d,
	0x1f20, 0x1f45,
	0x1f48, 0x1f4d,
	0x1f50, 0x1f7d,
	0x1f80, 0x1fd3,
	0x1fd6, 0x1fef,
	0x1ff2, 0x1ffe,
	0x2010, 0x2027,
	0x2030, 0x205e,
	0x2070, 0x2071,
	0x2074, 0x209c,
	0x20a0, 0x20c1,
	0x20d0, 0x20f0,
	0x2100, 0x218b,
	0x2190, 0x2429,
	0x2440, 0x244a,
	0x2460, 0x2b73,
	0x2b76, 0x2cf3,
	0x2cf9, 0x2d27,
	0x2d2d, 0x2d2d,
	0x2d30, 0x2d67,
	0x2d6f, 0x2d70,
	0x2d7f, 0x2d96,
	0x2da0, 0x2e5d,
	0x2e80, 0x2ef3,
	0x2f00, 0x2fd5,
	0x2ff0, 0x3096,
	0x3099, 0x30ff,
	0x3105, 0x31e5,
	0x31ef, 0xa48c,
	0xa490, 0xa4c6,
	0xa4d0, 0xa62b,
	0xa640, 0xa6f7,
	0xa700, 0xa7dc,
	0xa7f1, 0xa82c,
	0xa830, 0xa839,
	0xa840, 0xa877,
	0xa880, 0xa8c5,
	0xa8ce, 0xa8d9,
	0xa8e0, 0xa953,
	0xa95f, 0xa97c,
	0xa980, 0xa9d9,
	0xa9de, 0xaa36,
	0xaa40, 0xaa4d,
	0xaa50, 0xaa59,
	0xaa5c, 0xaac2,
	0xaadb, 0xaaf6,
	0xab01, 0xab06,
	0xab09, 0xab0e,
	0xab11, 0xab16,
	0xab20, 0xab6b,
	0xab70, 0xabed,
	0xabf0, 0xabf9,
	0xac00, 0xd7a3,
	0xd7b0, 0xd7c6,
	0xd7cb, 0xd7fb,
	0xf900, 0xfa6d,
	0xfa70, 0xfad9,
	0xfb00, 0xfb06,
	0xfb13, 0xfb17,
	0xfb1d, 0xfdcf,
	0xfdf0, 0xfe19,
	0xfe20, 0xfe6b,
	0xfe70, 0xfefc,
	0xff01, 0xffbe,
	0xffc2, 0xffc7,
	0xffca, 0xffcf,
	0xffd2, 0xffd7,
	0xffda, 0xffdc,
	0xffe0, 0xffee,
	0xfffc, 0xfffd,
}

var isNotPrint16 = []uint16{
	0x00ad,
	0x038b,
	0x038d,
	0x03a2,
	0x0530,
	0x0590,
	0x061c,
	0x06dd,
	0x083f,
	0x085f,
	0x08e2,
	0x0984,
	0x09a9,
	0x09b1,
	0x09de,
	0x0a04,
	0x0a29,
	0x0a31,
	0x0a34,
	0x0a37,
	0x0a3d,
	0x0a5d,
	0x0a84,
	0x0a8e,
	0x0a92,
	0x0aa9,
	0x0ab1,
	0x0ab4,
	0x0ac6,
	0x0aca,
	0x0b00,
	0x0b04,
	0x0b29,
	0x0b31,
	0x0b34,
	0x0b5e,
	0x0b84,
	0x0b91,
	0x0b9b,
	0x0b9d,
	0x0bc9,
	0x0c0d,
	0x0c11,
	0x0c29,
	0x0c45,
	0x0c49,
	0x0c57,
	0x0c5b,
	0x0c8d,
	0x0c91,
	0x0ca9,
	0x0cb4,
	0x0cc5,
	0x0cc9,
	0x0cdf,
	0x0cf0,
	0x0d0d,
	0x0d11,
	0x0d45,
	0x0d49,
	0x0d80,
	0x0d84,
	0x0db2,
	0x0dbc,
	0x0dd5,
	0x0dd7,
	0x0e83,
	0x0e85,
	0x0e8b,
	0x0ea4,
	0x0ea6,
	0x0ec5,
	0x0ec7,
	0x0ecf,
	0x0f48,
	0x0f98,
	0x0fbd,
	0x0fcd,
	0x10c6,
	0x1249,
	0x1257,
	0x1259,
	0x1289,
	0x12b1,
	0x12bf,
	0x12c1,
	0x12d7,
	0x1311,
	0x1680,
	0x176d,
	0x1771,
	0x180e,
	0x191f,
	0x1a5f,
	0x1b4d,
	0x1f58,
	0x1f5a,
	0x1f5c,
	0x1f5e,
	0x1fb5,
	0x1fc5,
	0x1fdc,
	0x1ff5,
	0x208f,
	0x2d26,
	0x2da7,
	0x2daf,
	0x2db7,
	0x2dbf,
	0x2dc7,
	0x2dcf,
	0x2dd7,
	0x2ddf,
	0x2e9a,
	0x3000,
	0x3040,
	0x3130,
	0x318f,
	0x321f,
	0xa9ce,
	0xa9ff,
	0xab27,
	0xab2f,
	0xfb37,
	0xfb3d,
	0xfb3f,
	0xfb42,
	0xfb45,
	0xfe53,
	0xfe67,
	0xfe75,
	0xffe7,
}

var isPrint32 = []uint32{
	0x010000, 0x01004d,
	0x010050, 0x01005d,
	0x010080, 0x0100fa,
	0x010100, 0x010102,
	0x010107, 0x010133,
	0x010137, 0x01019c,
	0x0101a0, 0x0101a0,
	0x0101d0, 0x0101fd,
	0x010280, 0x01029c,
	0x0102a0, 0x0102d0,
	0x0102e0, 0x0102fb,
	0x010300, 0x010323,
	0x01032d, 0x01034a,
	0x010350, 0x01037a,
	0x010380, 0x0103c3,
	0x0103c8, 0x0103d5,
	0x010400, 0x01049d,
	0x0104a0, 0x0104a9,
	0x0104b0, 0x0104d3,
	0x0104d8, 0x0104fb,
	0x010500, 0x010527,
	0x010530, 0x010563,
	0x01056f, 0x0105bc,
	0x0105c0, 0x0105f3,
	0x010600, 0x010736,
	0x010740, 0x010755,
	0x010760, 0x010767,
	0x010780, 0x0107ba,
	0x010800, 0x010805,
	0x010808, 0x010838,
	0x01083c, 0x01083c,
	0x01083f, 0x01089e,
	0x0108a7, 0x0108af,
	0x0108e0, 0x0108f5,
	0x0108fb, 0x01091b,
	0x01091f, 0x010939,
	0x01093f, 0x010959,
	0x010980, 0x0109b7,
	0x0109bc, 0x0109cf,
	0x0109d2, 0x010a06,
	0x010a0c, 0x010a35,
	0x010a38, 0x010a3a,
	0x010a3f, 0x010a48,
	0x010a50, 0x010a58,
	0x010a60, 0x010a9f,
	0x010ac0, 0x010ae6,
	0x010aeb, 0x010af6,
	0x010b00, 0x010b35,
	0x010b39, 0x010b55,
	0x010b58, 0x010b72,
	0x010b78, 0x010b91,
	0x010b99, 0x010b9c,
	0x010ba9, 0x010baf,
	0x010c00, 0x010c48,
	0x010c80, 0x010cb2,
	0x010cc0, 0x010cf2,
	0x010cfa, 0x010d27,
	0x010d30, 0x010d39,
	0x010d40, 0x010d65,
	0x010d69, 0x010d85,
	0x010d8e, 0x010d8f,
	0x010e60, 0x010ead,
	0x010eb0, 0x010eb1,
	0x010ec2, 0x010ec7,
	0x010ed0, 0x010ed8,
	0x010efa, 0x010f27,
	0x010f30, 0x010f59,
	0x010f70, 0x010f89,
	0x010fb0, 0x010fcb,
	0x010fe0, 0x010ff6,
	0x011000, 0x01104d,
	0x011052, 0x011075,
	0x01107f, 0x0110c2,
	0x0110d0, 0x0110e8,
	0x0110f0, 0x0110f9,
	0x011100, 0x011147,
	0x011150, 0x011176,
	0x011180, 0x0111f4,
	0x011200, 0x011241,
	0x011280, 0x0112a9,
	0x0112b0, 0x0112ea,
	0x0112f0, 0x0112f9,
	0x011300, 0x01130c,
	0x01130f, 0x011310,
	0x011313, 0x011344,
	0x011347, 0x011348,
	0x01134b, 0x01134d,
	0x011350, 0x011350,
	0x011357, 0x011357,
	0x01135d, 0x011363,
	0x011366, 0x01136c,
	0x011370, 0x011374,
	0x011380, 0x01138b,
	0x01138e, 0x0113c2,
	0x0113c5, 0x0113d8,
	0x0113e1, 0x0113e2,
	0x011400, 0x011461,
	0x011480, 0x0114c7,
	0x0114d0, 0x0114d9,
	0x011580, 0x0115b5,
	0x0115b8, 0x0115dd,
	0x011600, 0x011644,
	0x011650, 0x011659,
	0x011660, 0x01166c,
	0x011680, 0x0116b9,
	0x0116c0, 0x0116c9,
	0x0116d0, 0x0116e3,
	0x011700, 0x01171a,
	0x01171d, 0x01172b,
	0x011730, 0x011746,
	0x011800, 0x01183b,
	0x0118a0, 0x0118f2,
	0x0118ff, 0x011906,
	0x011909, 0x011909,
	0x01190c, 0x011938,
	0x01193b, 0x011946,
	0x011950, 0x011959,
	0x0119a0, 0x0119a7,
	0x0119aa, 0x0119d7,
	0x0119da, 0x0119e4,
	0x011a00, 0x011a47,
	0x011a50, 0x011aa2,
	0x011ab0, 0x011af8,
	0x011b00, 0x011b09,
	0x011b60, 0x011b67,
	0x011bc0, 0x011be1,
	0x011bf0, 0x011bf9,
	0x011c00, 0x011c45,
	0x011c50, 0x011c6c,
	0x011c70, 0x011c8f,
	0x011c92, 0x011cb6,
	0x011d00, 0x011d36,
	0x011d3a, 0x011d47,
	0x011d50, 0x011d59,
	0x011d60, 0x011d98,
	0x011da0, 0x011da9,
	0x011db0, 0x011ddb,
	0x011de0, 0x011de9,
	0x011ee0, 0x011ef8,
	0x011f00, 0x011f3a,
	0x011f3e, 0x011f5a,
	0x011fb0, 0x011fb0,
	0x011fc0, 0x011ff1,
	0x011fff, 0x012399,
	0x012400, 0x012474,
	0x012480, 0x012543,
	0x012f90, 0x012ff2,
	0x013000, 0x01342f,
	0x013440, 0x013455,
	0x013460, 0x0143fa,
	0x014400, 0x014646,
	0x016100, 0x016139,
	0x016800, 0x016a38,
	0x016a40, 0x016a69,
	0x016a6e, 0x016ac9,
	0x016ad0, 0x016aed,
	0x016af0, 0x016af5,
	0x016b00, 0x016b45,
	0x016b50, 0x016b77,
	0x016b7d, 0x016b8f,
	0x016d40, 0x016d79,
	0x016e40, 0x016e9a,
	0x016ea0, 0x016eb8,
	0x016ebb, 0x016ed3,
	0x016f00, 0x016f4a,
	0x016f4f, 0x016f87,
	0x016f8f, 0x016f9f,
	0x016fe0, 0x016fe4,
	0x016ff0, 0x016ff6,
	0x017000, 0x018cd5,
	0x018cff, 0x018d1e,
	0x018d80, 0x018df2,
	0x01aff0, 0x01b122,
	0x01b132, 0x01b132,
	0x01b150, 0x01b152,
	0x01b155, 0x01b155,
	0x01b164, 0x01b167,
	0x01b170, 0x01b2fb,
	0x01bc00, 0x01bc6a,
	0x01bc70, 0x01bc7c,
	0x01bc80, 0x01bc88,
	0x01bc90, 0x01bc99,
	0x01bc9c, 0x01bc9f,
	0x01cc00, 0x01ccfc,
	0x01cd00, 0x01ceb3,
	0x01ceba, 0x01ced0,
	0x01cee0, 0x01cef0,
	0x01cf00, 0x01cf2d,
	0x01cf30, 0x01cf46,
	0x01cf50, 0x01cfc3,
	0x01d000, 0x01d0f5,
	0x01d100, 0x01d126,
	0x01d129, 0x01d172,
	0x01d17b, 0x01d1ea,
	0x01d200, 0x01d245,
	0x01d2c0, 0x01d2d3,
	0x01d2e0, 0x01d2f3,
	0x01d300, 0x01d356,
	0x01d360, 0x01d378,
	0x01d400, 0x01d49f,
	0x01d4a2, 0x01d4a2,
	0x01d4a5, 0x01d4a6,
	0x01d4a9, 0x01d50a,
	0x01d50d, 0x01d546,
	0x01d54a, 0x01d6a5,
	0x01d6a8, 0x01d7cb,
	0x01d7ce, 0x01da8b,
	0x01da9b, 0x01daaf,
	0x01df00, 0x01df1e,
	0x01df25, 0x01df2a,
	0x01e000, 0x01e018,
	0x01e01b, 0x01e02a,
	0x01e030, 0x01e06d,
	0x01e08f, 0x01e08f,
	0x01e100, 0x01e12c,
	0x01e130, 0x01e13d,
	0x01e140, 0x01e149,
	0x01e14e, 0x01e14f,
	0x01e290, 0x01e2ae,
	0x01e2c0, 0x01e2f9,
	0x01e2ff, 0x01e2ff,
	0x01e4d0, 0x01e4f9,
	0x01e5d0, 0x01e5fa,
	0x01e5ff, 0x01e5ff,
	0x01e6c0, 0x01e6f5,
	0x01e6fe, 0x01e6ff,
	0x01e7e0, 0x01e8c4,
	0x01e8c7, 0x01e8d6,
	0x01e900, 0x01e94b,
	0x01e950, 0x01e959,
	0x01e95e, 0x01e95f,
	0x01ec71, 0x01ecb4,
	0x01ed01, 0x01ed3d,
	0x01ee00, 0x01ee24,
	0x01ee27, 0x01ee3b,
	0x01ee42, 0x01ee42,
	0x01ee47, 0x01ee54,
	0x01ee57, 0x01ee64,
	0x01ee67, 0x01ee9b,
	0x01eea1, 0x01eebb,
	0x01eef0, 0x01eef1,
	0x01f000, 0x01f02b,
	0x01f030, 0x01f093,
	0x01f0a0, 0x01f0ae,
	0x01f0b1, 0x01f0f5,
	0x01f100, 0x01f1ad,
	0x01f1e6, 0x01f202,
	0x01f210, 0x01f23b,
	0x01f240, 0x01f248,
	0x01f250, 0x01f251,
	0x01f260, 0x01f265,
	0x01f300, 0x01f6d8,
	0x01f6dc, 0x01f6ec,
	0x01f6f0, 0x01f6fc,
	0x01f700, 0x01f7d9,
	0x01f7e0, 0x01f7eb,
	0x01f7f0, 0x01f7f0,
	0x01f800, 0x01f80b,
	0x01f810, 0x01f847,
	0x01f850, 0x01f859,
	0x01f860, 0x01f887,
	0x01f890, 0x01f8ad,
	0x01f8b0, 0x01f8bb,
	0x01f8c0, 0x01f8c1,
	0x01f8d0, 0x01f8d8,
	0x01f900, 0x01fa57,
	0x01fa60, 0x01fa6d,
	0x01fa70, 0x01fa7c,
	0x01fa80, 0x01fa8a,
	0x01fa8e, 0x01fac8,
	0x01facd, 0x01fadc,
	0x01fadf, 0x01faea,
	0x01faef, 0x01faf8,
	0x01fb00, 0x01fbfa,
	0x020000, 0x02a6df,
	0x02a700, 0x02b81d,
	0x02b820, 0x02cead,
	0x02ceb0, 0x02ebe0,
	0x02ebf0, 0x02ee5d,
	0x02f800, 0x02fa1d,
	0x030000, 0x03134a,
	0x031350, 0x033479,
	0x0e0100, 0x0e01ef,
}

var isNotPrint32 = []uint16{ // add 0x10000 to each entry
	0x000c,
	0x0027,
	0x003b,
	0x003e,
	0x018f,
	0x039e,
	0x057b,
	0x058b,
	0x0593,
	0x0596,
	0x05a2,
	0x05b2,
	0x05ba,
	0x0786,
	0x07b1,
	0x0809,
	0x0836,
	0x0856,
	0x08f3,
	0x0a04,
	0x0a14,
	0x0a18,
	0x0e7f,
	0x0eaa,
	0x10bd,
	0x1135,
	0x11e0,
	0x1212,
	0x1287,
	0x1289,
	0x128e,
	0x129e,
	0x1304,
	0x1329,
	0x1331,
	0x1334,
	0x133a,
	0x138a,
	0x138f,
	0x13b6,
	0x13c1,
	0x13c6,
	0x13cb,
	0x13d6,
	0x145c,
	0x1914,
	0x1917,
	0x1936,
	0x1c09,
	0x1c37,
	0x1ca8,
	0x1d07,
	0x1d0a,
	0x1d3b,
	0x1d3e,
	0x1d66,
	0x1d69,
	0x1d8f,
	0x1d92,
	0x1f11,
	0x246f,
	0x6a5f,
	0x6abf,
	0x6b5a,
	0x6b62,
	0xaff4,
	0xaffc,
	0xafff,
	0xd455,
	0xd49d,
	0xd4ad,
	0xd4ba,
	0xd4bc,
	0xd4c4,
	0xd506,
	0xd515,
	0xd51d,
	0xd53a,
	0xd53f,
	0xd545,
	0xd551,
	0xdaa0,
	0xe007,
	0xe022,
	0xe025,
	0xe6df,
	0xe7e7,
	0xe7ec,
	0xe7ef,
	0xe7ff,
	0xee04,
	0xee20,
	0xee23,
	0xee28,
	0xee33,
	0xee38,
	0xee3a,
	0xee48,
	0xee4a,
	0xee4c,
	0xee50,
	0xee53,
	0xee58,
	0xee5a,
	0xee5c,
	0xee5e,
	0xee60,
	0xee63,
	0xee6b,
	0xee73,
	0xee78,
	0xee7d,
	0xee7f,
	0xee8a,
	0xeea4,
	0xeeaa,
	0xf0c0,
	0xf0d0,
	0xfac7,
	0xfb93,
}

// isGraphic lists the graphic runes not matched by IsPrint.
var isGraphic = []uint16{
	0x00a0,
	0x1680,
	0x2000,
	0x2001,
	0x2002,
	0x2003,
	0x2004,
	0x2005,
	0x2006,
	0x2007,
	0x2008,
	0x2009,
	0x200a,
	0x202f,
	0x205f,
	0x3000,
}
`
