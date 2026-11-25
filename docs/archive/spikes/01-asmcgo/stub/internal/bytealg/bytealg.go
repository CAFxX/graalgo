// Package bytealgform holds pure-Go formulations of the amd64 Plan 9
// assembly in src/internal/bytealg (indexbyte_amd64.s, index_amd64.s,
// compare_amd64.s, count_amd64.s, equal_amd64.s).
//
// Strategy: INTRINSIFY. On graalgo these become Truffle intrinsics (or are
// left as Go loops for Graal's auto-vectorizer); the formulations below are
// the semantic spec the intrinsic must implement. They are differential-
// tested against the real amd64 assembly via the public bytes/strings API
// in ../difftest.
//
// Why pure Go (not Java): the Truffle backend compiles Go source, so a Go
// formulation is directly consumable as the intrinsic's fallback and as the
// spec. The semantics (byte scan, lexicographic compare) have no
// platform-specific content, so no Java/JVM facility is needed to express
// them.
package bytealg

import gc "graalgos01stub/internal/runtime/gc"

// IndexByte returns the index of the first instance of c in b, or -1.
// Formulation of indexbytebody<> in indexbyte_amd64.s (SSE2 PCMPESTRI path
// for len>=16, scalar tail). The asm's observable contract is exactly this
// linear scan; vectorization only changes speed, not the result.
func IndexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// IndexByteString is IndexByte over a string.
func IndexByteString(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// LastIndexByte returns the index of the last instance of c in b, or -1.
// Formulation of the scalar loop used by bytes.LastIndexByte.
func LastIndexByte(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// LastIndexByteString is LastIndexByte over a string.
func LastIndexByteString(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// primeRK mirrors internal/bytealg.primeRK (Rabin-Karp base).
const primeRK = 16777619

func hashStr(sep []byte) (uint32, uint32) {
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

// Index returns the index of the first instance of sep in s, or -1.
// Formulation of indexbody<> in index_amd64.s. The amd64 asm implements a
// SIMD substring search; its observable contract is "first occurrence".
// This formulation uses the same two-tier structure as bytes.Index:
// brute force for short needles, Rabin-Karp for long ones (mirroring
// internal/bytealg.IndexRabinKarp, which is already pure Go upstream).
func Index(s, sep []byte) int {
	n := len(sep)
	switch {
	case n == 0:
		return 0
	case n == 1:
		return IndexByte(s, sep[0])
	case n == len(s):
		if Equal(s, sep) {
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	// Brute force for short needles (matches MaxBruteForce-tier behavior).
	if n < 32 {
		c0, c1 := sep[0], sep[1]
		for i := 0; i+n <= len(s); i++ {
			if s[i] == c0 && s[i+1] == c1 && Equal(s[i:i+n], sep) {
				return i
			}
		}
		return -1
	}
	// Rabin-Karp for long needles.
	hashsep, pow := hashStr(sep)
	var h uint32
	for i := 0; i < n; i++ {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashsep && Equal(s[:n], sep) {
		return 0
	}
	for i := n; i < len(s); {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashsep && Equal(s[i-n:i], sep) {
			return i - n
		}
	}
	return -1
}

// IndexString is Index over strings.
func IndexString(s, sep string) int {
	n := len(sep)
	switch {
	case n == 0:
		return 0
	case n == 1:
		return IndexByteString(s, sep[0])
	case n == len(s):
		if s == sep {
			return 0
		}
		return -1
	case n > len(s):
		return -1
	}
	sb := []byte(sep)
	if n < 32 {
		c0, c1 := sep[0], sep[1]
		for i := 0; i+n <= len(s); i++ {
			if s[i] == c0 && s[i+1] == c1 && s[i:i+n] == sep {
				return i
			}
		}
		return -1
	}
	hashsep, pow := hashStr(sb)
	var h uint32
	for i := 0; i < n; i++ {
		h = h*primeRK + uint32(s[i])
	}
	if h == hashsep && s[:n] == sep {
		return 0
	}
	for i := n; i < len(s); {
		h *= primeRK
		h += uint32(s[i])
		h -= pow * uint32(s[i-n])
		i++
		if h == hashsep && s[i-n:i] == sep {
			return i - n
		}
	}
	return -1
}

// Compare returns an integer comparing a and b lexicographically.
// Formulation of cmpbody<> in compare_amd64.s (which compares 8 bytes at a
// time, then falls back to bytewise). Observable contract: lexicographic
// order with length tiebreak; the &a[0]==&b[0] fast path is unobservable.
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
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return +1
	}
	return 0
}

// Equal reports whether a and b have equal length and contents.
// Formulation of memequal in equal_amd64.s (runtime·memequal via linkname).
func Equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		if x != b[i] {
			return false
		}
	}
	return true
}

// Count counts instances of c in b.
// Formulation of countbody<> in count_amd64.s (SSE2 popcount path).
func Count(b []byte, c byte) int {
	n := 0
	for _, x := range b {
		if x == c {
			n++
		}
	}
	return n
}

// CountString counts instances of c in s.
func CountString(s string, c byte) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			n++
		}
	}
	return n
}

// ---- spike additions: symbols bytes/strings need beyond the difftest set ----

// CompareString compares two strings, returning -1/0/+1.
func CompareString(a, b string) int {
	return Compare([]byte(a), []byte(b))
}

// IndexRabinKarp returns the index of the first instance of sep in s.
// Formulation: naive O(n*m) search; the Rabin-Karp rolling hash in the real
// implementation is a performance optimization with identical semantics.
func IndexRabinKarp[T string | []byte](s, sep T) int {
	return Index([]byte(s), []byte(sep))
}

// LastIndexRabinKarp returns the index of the last instance of sep in s.
func LastIndexRabinKarp[T string | []byte](s, sep T) int {
	sb, sepp := []byte(s), []byte(sep)
	n := len(sepp)
	if n == 0 {
		return len(sb)
	}
	if n > len(sb) {
		return -1
	}
	for i := len(sb) - n; i >= 0; i-- {
		if Equal(sb[i:i+n], sepp) {
			return i
		}
	}
	return -1
}

// MaxBruteForce mirrors the amd64 tuning constant: max input size for which
// bytes.Index calls bytealg.Index directly. Semantics-neutral threshold.
const MaxBruteForce = 64

// MaxLen mirrors the amd64 (AVX2) tuning constant: max needle length for the
// bytealg.Index path. Semantics-neutral threshold.
const MaxLen = 63

// Cutover reports the number of IndexByte failures to tolerate before
// switching to Index; n is bytes processed so far. Mirrors amd64 semantics.
func Cutover(n int) int {
	return (n + 16) / 8
}

// MakeNoZero returns a slice of length n. The real implementation
// (runtime.bytealg_MakeNoZero, linknamed from the runtime) skips zeroing and
// rounds the capacity UP to the allocator size class:
//
//	cap := roundupsize(uintptr(len), true)
//	return unsafe.Slice((*byte)(mallocgc(cap, nil, false)), cap)[:len]
//
// The "capacity of at least n" contract is load-bearing: strings.Builder.Grow
// relies on size-class rounding (TestBuilderGrowSizeclasses pins it). This
// formulation replicates the rounding with the runtime's own generated size
// class tables (internal/runtime/gc, copied verbatim). It returns zeroed
// memory, which is a conservative, always-correct instance of the "caller
// must overwrite before reading" contract.
func MakeNoZero(n int) []byte {
	c := roundUpSizeClass(n)
	b := make([]byte, c)
	return b[:n]
}

// roundUpSizeClass mirrors runtime.roundupsize(size, true) (noscan=true, the
// case for byte slices: no malloc header is added) using the generated tables.
func roundUpSizeClass(n int) int {
	const (
		smallSizeMax = 1024
		smallSizeDiv = 8
		largeSizeDiv = 128
		maxSmallSize = 32768
		pageSize     = 4096 // runtime.pageSize on amd64
	)
	if n <= maxSmallSize-8 {
		if n <= smallSizeMax-8 {
			return int(gc.SizeClassToSize[gc.SizeToSizeClass8[(n+smallSizeDiv-1)/smallSizeDiv]])
		}
		return int(gc.SizeClassToSize[gc.SizeToSizeClass128[(n-smallSizeMax+largeSizeDiv-1)/largeSizeDiv]])
	}
	return (n + pageSize - 1) &^ (pageSize - 1)
}
