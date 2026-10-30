// P3-M9 (tier-1d): synthetic `internal/bytealg` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (see synth_ascii85.go for the pattern). Synthetic-only,
// like internal/reflectlite: user programs cannot import internal/* (the
// import allow-list does not admit it), but synthetic packages may —
// resolved via overlayImporter.Import("internal/bytealg").
//
// API: the gc 1.27.1 exported surface (Compare, CompareString, Count,
// CountString, Equal, Index, IndexString, Cutover, IndexByte,
// IndexByteString, LastIndexByte, LastIndexByteString, IndexRabinKarp,
// LastIndexRabinKarp, MakeNoZero, MaxBruteForce, MaxLen), implemented with
// the portable algorithms from gc's own *_generic.go fallbacks. gc's
// SIMD/assembly fast paths (compare_amd64.s etc.) are behaviorally
// identical by construction — bytealg's contract is pure function
// semantics, and every function here is a total pure function of its
// inputs.
//
// Deviations from gc (documented, none observable):
//   D1. Index/IndexString: gc's portable fallback panics("unimplemented")
//       (real platforms use assembly); ours is a correct portable brute
//       force. The declared contract "Requires 2 <= len(b) <= MaxLen" is
//       honored by all in-tree callers; our implementation is additionally
//       total (handles every length) — a strict superset of the contract.
//   D2. MaxBruteForce = 0 and MaxLen = 0 (zero value): exactly the gc
//       portable/generic build values (index_generic.go, bytealg.go).
//   D3. MakeNoZero: gc's is assembly and may return unzeroed memory; ours
//       is make([]byte, n) (zeroed, len n, cap n). The documented contract
//       (length n, capacity >= n) holds; no correct program observes the
//       backing bytes before writing them.
//   D4. The offsetPPC64*/offsetX86* consts (unsafe.Offsetof into
//       internal/cpu records, for assembly use) are omitted: there is no
//       assembly consumer in the synthetic tree. Re-test trigger: a
//       synthetic internal/cpu landing.
//
// Retirement: see ../../docs/arch/synth-retirement-tracker.md#internalbytealg for this package's root cause and retires-when trigger (internal/bytealg).
package main

// syntheticBytealgSource is the P3-M9 synthetic `internal/bytealg`
// package: exact gc 1.27.1 API; portable real-Go implementations.
const syntheticBytealgSource = `package bytealg

// MaxBruteForce is the maximum length below which brute force is used
// (portable gc value, cf. index_generic.go).
const MaxBruteForce = 0

// MaxLen is the maximum length of the string to be searched for
// (argument b) in Index. Portable gc value: 0 (unset).
var MaxLen int

// primeRK is the prime base used in Rabin-Karp algorithm.
const primeRK = 16777619

// Compare returns an integer comparing a and b lexicographically.
func Compare(a, b []byte) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	for i := 0; i < l; i++ {
		c1, c2 := a[i], b[i]
		if c1 < c2 {
			return -1
		}
		if c1 > c2 {
			return +1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return +1
	}
	return 0
}

// CompareString returns an integer comparing a and b lexicographically.
func CompareString(a, b string) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	for i := 0; i < l; i++ {
		c1, c2 := a[i], b[i]
		if c1 < c2 {
			return -1
		}
		if c1 > c2 {
			return +1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return +1
	}
	return 0
}

// Count counts the number of non-overlapping instances of c in b.
func Count(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

// CountString counts the number of non-overlapping instances of c in s.
func CountString(s string, c byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			n++
		}
	}
	return n
}

// Equal reports whether a and b are the same length and contain the same
// bytes.
func Equal(a, b []byte) bool {
	return string(a) == string(b)
}

// Index returns the index of the first instance of b in a, or -1 if b is
// not present in a. Portable brute force (deviation D1).
func Index(a, b []byte) int {
	if len(b) == 0 {
		return 0
	}
	if len(b) == 1 {
		return IndexByte(a, b[0])
	}
	if len(b) > len(a) {
		return -1
	}
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i] == b[0] && Equal(a[i:i+len(b)], b) {
			return i
		}
	}
	return -1
}

// IndexString returns the index of the first instance of b in a, or -1 if
// b is not present in a. Portable brute force (deviation D1).
func IndexString(a, b string) int {
	if len(b) == 0 {
		return 0
	}
	if len(b) == 1 {
		return IndexByteString(a, b[0])
	}
	if len(b) > len(a) {
		return -1
	}
	for i := 0; i+len(b) <= len(a); i++ {
		if a[i] == b[0] && a[i:i+len(b)] == b {
			return i
		}
	}
	return -1
}

// Cutover reports the number of failures of IndexByte we should tolerate
// before switching over to Index. n is the number of bytes processed so
// far. (Exact gc formula from index_amd64.go.)
func Cutover(n int) int {
	// 1 error per 8 characters, plus a few slop to start.
	return (n + 16) / 8
}

// IndexByte returns the index of the first instance of c in b, or -1 if c
// is not present in b.
func IndexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// IndexByteString returns the index of the first instance of c in s, or -1
// if c is not present in s.
func IndexByteString(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// LastIndexByte returns the index of the last instance of c in s, or -1 if
// c is not present in s.
func LastIndexByte(s []byte, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// LastIndexByteString returns the index of the last instance of c in s, or
// -1 if c is not present in s.
func LastIndexByteString(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// hashStr returns the hash and the appropriate multiplicative factor for
// use in Rabin-Karp algorithm.
func hashStr[T string | []byte](sep T) (uint32, uint32) {
	hash := uint32(0)
	for i := 0; i < len(sep); i++ {
		hash = hash*primeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, primeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// hashStrRev returns the hash of the reverse of sep and the appropriate
// multiplicative factor for use in Rabin-Karp algorithm.
func hashStrRev[T string | []byte](sep T) (uint32, uint32) {
	hash := uint32(0)
	for i := len(sep) - 1; i >= 0; i-- {
		hash = hash*primeRK + uint32(sep[i])
	}
	var pow, sq uint32 = 1, primeRK
	for i := len(sep); i > 0; i >>= 1 {
		if i&1 != 0 {
			pow *= sq
		}
		sq *= sq
	}
	return hash, pow
}

// IndexRabinKarp uses the Rabin-Karp search algorithm to return the index
// of the first occurrence of sep in s, or -1 if not present.
func IndexRabinKarp[T string | []byte](s, sep T) int {
	// Rabin-Karp search
	hashss, pow := hashStr(sep)
	n := len(sep)
	var h uint32
	for i := 0; i < n; i++ {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashss && string(s[:n]) == string(sep) {
		return 0
	}
	for i := n; i < len(s); {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashss && string(s[i-n:i]) == string(sep) {
			return i - n
		}
	}
	return -1
}

// LastIndexRabinKarp uses the Rabin-Karp search algorithm to return the
// last index of the occurrence of sep in s, or -1 if not present.
func LastIndexRabinKarp[T string | []byte](s, sep T) int {
	// Rabin-Karp search from the end of the string
	hashss, pow := hashStrRev(sep)
	n := len(sep)
	last := len(s) - n
	var h uint32
	for i := len(s) - 1; i >= last; i-- {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashss && string(s[last:]) == string(sep) {
		return last
	}
	for i := last - 1; i >= 0; i-- {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i+n])
		if h == hashss && string(s[i:i+n]) == string(sep) {
			return i
		}
	}
	return -1
}

// MakeNoZero makes a slice of length n and capacity of at least n bytes.
// gc's version may return unzeroed memory; ours is zeroed (deviation D3:
// the documented contract — length n, capacity >= n — holds either way).
func MakeNoZero(n int) []byte {
	return make([]byte, n)
}
`
