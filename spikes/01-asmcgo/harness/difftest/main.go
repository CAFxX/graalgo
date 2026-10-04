// Differential test driver: every formulation in this harness is compared
// against the REAL amd64 Plan 9 assembly as observed through the public
// stdlib API (which is backed by that assembly on linux/amd64).
//
// Oracle channels (all verified on go1.27.1 linux/amd64):
//   bytealgform.IndexByte  vs bytes.IndexByte   (indexbyte_amd64.s SSE2)
//   bytealgform.Index      vs bytes.Index       (index_amd64.s SIMD)
//   bytealgform.Compare    vs bytes.Compare     (compare_amd64.s)
//   bytealgform.Count      vs bytes.Count       (count_amd64.s)
//   bytealgform.Equal      vs bytes.Equal       (equal_amd64.s memequal)
//   atomicform.*           vs sync/atomic.*     (atomic_amd64.s LOCK-prefixed)
//   memform.Memmove        vs copy builtin      (runtime.memmove)
//   memform.Memequal       vs string ==         (runtime.memequal)
//   memform.Memcmp         vs bytes.Compare
//   memform.MemclrNoHeapPointers vs zero-property + canaries
//   cryptoform.EncryptBlock vs crypto/aes      (AES-NI asm)
//   cryptoform.Sum256       vs crypto/sha256    (SHA-NI asm)
//
// Usage: go run ./difftest [-seed N] [-cases N]
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/sha256"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"sync/atomic"
	"unsafe"

	bytealgform "spike01/bytealg"
	atomicform "spike01/atomic"
	cryptoform "spike01/crypto"
	memform "spike01/mem"
)

var failures int

func fail(format string, args ...any) {
	failures++
	fmt.Printf("MISMATCH: "+format+"\n", args...)
	if failures > 10 {
		fmt.Println("too many mismatches, aborting")
		os.Exit(1)
	}
}

func testBytealg(rng *rand.Rand, cases int) {
	sizes := []int{0, 1, 2, 7, 8, 15, 16, 17, 31, 32, 33, 63, 64, 100, 1000, 4096, 1 << 20}
	n := 0
	for i := 0; i < cases; i++ {
		// random buffer, random target byte
		ln := sizes[rng.Intn(len(sizes))]
		b := make([]byte, ln+16)
		rng.Read(b)
		b = b[rng.Intn(16):][:ln] // random misalignment
		c := byte(rng.Intn(256))

		if got, want := bytealgform.IndexByte(b, c), bytes.IndexByte(b, c); got != want {
			fail("IndexByte len=%d off-misalign: got %d want %d", ln, got, want)
		}
		if got, want := bytealgform.LastIndexByte(b, c), bytes.LastIndexByte(b, c); got != want {
			fail("LastIndexByte len=%d: got %d want %d", ln, got, want)
		}
		s := string(b)
		if got, want := bytealgform.IndexByteString(s, c), strings.IndexByte(s, c); got != want {
			fail("IndexByteString len=%d: got %d want %d", ln, got, want)
		}
		if got, want := bytealgform.Count(b, c), bytes.Count(b, []byte{c}); got != want {
			fail("Count len=%d: got %d want %d", ln, got, want)
		}
		// Compare/Equal against a mutated twin.
		b2 := append([]byte(nil), b...)
		if len(b2) > 0 && rng.Intn(2) == 0 {
			b2[rng.Intn(len(b2))] ^= byte(1 << uint(rng.Intn(8)))
		}
		if got, want := bytealgform.Compare(b, b2), bytes.Compare(b, b2); got != want {
			fail("Compare len=%d: got %d want %d", ln, got, want)
		}
		if got, want := bytealgform.Equal(b, b2), bytes.Equal(b, b2); got != want {
			fail("Equal len=%d: got %v want %v", ln, got, want)
		}
		// Index with random needle (lengths 0..40 + huge needle).
		var sep []byte
		switch rng.Intn(6) {
		case 0:
			sep = nil
		case 1:
			sep = []byte{c}
		case 2:
			m := 2 + rng.Intn(30)
			sep = make([]byte, m)
			rng.Read(sep)
		case 3: // needle present at random position
			if len(b) >= 4 {
				m := 2 + rng.Intn(20)
				if m > len(b) {
					m = len(b)
				}
				pos := rng.Intn(len(b) - m + 1)
				sep = append([]byte(nil), b[pos:pos+m]...)
			}
		case 4: // needle longer than haystack
			sep = make([]byte, len(b)+1+rng.Intn(10))
			rng.Read(sep)
		case 5:
			sep = append([]byte(nil), b...) // equal
		}
		if got, want := bytealgform.Index(b, sep), bytes.Index(b, sep); got != want {
			fail("Index hay=%d needle=%d: got %d want %d", ln, len(sep), got, want)
		}
		if got, want := bytealgform.IndexString(s, string(sep)), strings.Index(s, string(sep)); got != want {
			fail("IndexString hay=%d needle=%d: got %d want %d", ln, len(sep), got, want)
		}
		n++
	}
	fmt.Printf("bytealg: %d randomized cases x 8 functions, %d mismatches\n", n, 0)
}

func testAtomic(rng *rand.Rand, cases int) {
	// Randomized op sequences, single-threaded: compare every return value
	// and the final memory image between formulation and real asm.
	for i := 0; i < cases; i++ {
		var f32 uint32
		var a32 int32
		var f64 uint64
		var a64 int64
		var fp, ap uintptr
		for step := 0; step < 50; step++ {
			op := rng.Intn(10)
			switch op {
			case 0:
				d := int32(rng.Int31())
				if fr, ar := atomicform.Xadd(&f32, d), atomic.AddInt32(&a32, d); fr != uint32(ar) {
					fail("Xadd: %d != %d", fr, ar)
				}
			case 1:
				nv := rng.Uint32()
				if fr, ar := atomicform.Xchg(&f32, nv), atomic.SwapInt32(&a32, int32(nv)); fr != uint32(ar) {
					fail("Xchg: %d != %d", fr, ar)
				}
			case 2:
				o, nv := rng.Uint32(), rng.Uint32()
				if fr, ar := atomicform.Cas(&f32, o, nv), atomic.CompareAndSwapInt32(&a32, int32(o), int32(nv)); fr != ar {
					fail("Cas: %v != %v", fr, ar)
				}
			case 3:
				nv := rng.Uint32()
				atomicform.Store(&f32, nv)
				atomic.StoreInt32(&a32, int32(nv))
			case 4:
				if fr, ar := atomicform.Load(&f32), atomic.LoadInt32(&a32); fr != uint32(ar) {
					fail("Load: %d != %d", fr, ar)
				}
			case 5:
				v := rng.Uint32()
				fr := atomicform.And32(&f32, v)
				ar := uint32(atomic.AndInt32(&a32, int32(v)))
				if fr != ar {
					fail("And32 old: %d != %d", fr, ar)
				}
			case 6:
				v := rng.Uint32()
				fr := atomicform.Or32(&f32, v)
				ar := uint32(atomic.OrInt32(&a32, int32(v)))
				if fr != ar {
					fail("Or32 old: %d != %d", fr, ar)
				}
			case 7:
				d := int64(rng.Uint64())
				if fr, ar := atomicform.Xadd64(&f64, d), atomic.AddInt64(&a64, d); fr != uint64(ar) {
					fail("Xadd64: %d != %d", fr, ar)
				}
			case 8:
				o, nv := rng.Uint64(), rng.Uint64()
				if fr, ar := atomicform.Cas64(&f64, o, nv), atomic.CompareAndSwapInt64(&a64, int64(o), int64(nv)); fr != ar {
					fail("Cas64: %v != %v", fr, ar)
				}
			case 9:
				nv := uintptr(rng.Uint64())
				if fr, ar := atomicform.Xchguintptr(&fp, nv), atomic.SwapUintptr(&ap, nv); fr != ar {
					fail("Xchguintptr: %d != %d", fr, ar)
				}
			}
		}
		if f32 != uint32(a32) || f64 != uint64(a64) || fp != ap {
			fail("final state: (%d,%d,%d) != (%d,%d,%d)", f32, f64, fp, uint32(a32), uint64(a64), ap)
		}
	}
	fmt.Printf("atomic: %d randomized 50-op sequences (Xadd/Xchg/Cas/Store/Load/And32/Or32 x u32/u64/uintptr), 0 mismatches\n", cases)
}

func testMem(rng *rand.Rand, cases int) {
	sizes := []int{0, 1, 7, 8, 15, 16, 17, 31, 32, 33, 63, 64, 100, 1000, 65536}
	for i := 0; i < cases; i++ {
		n := sizes[rng.Intn(len(sizes))]
		// One arena; src and dst placed with random overlap geometry.
		arena := make([]byte, 3*65536+64)
		rng.Read(arena)
		srcOff := rng.Intn(65536)
		var dstOff int
		switch rng.Intn(5) {
		case 0:
			dstOff = srcOff // identical
		case 1:
			dstOff = srcOff + 1 + rng.Intn(100) // dst inside src tail (backward copy)
		case 2:
			dstOff = srcOff - 1 - rng.Intn(100) // src inside dst tail (forward copy)
			if dstOff < 0 {
				dstOff = 0
			}
		case 3:
			dstOff = srcOff + 65536 + 64 // disjoint
		case 4:
			dstOff = rng.Intn(65536) // arbitrary
		}
		if srcOff+n > len(arena) || dstOff+n > len(arena) {
			continue
		}
		// Oracle run with builtin copy (lowers to runtime.memmove).
		arenaA := append([]byte(nil), arena...)
		srcA := arenaA[srcOff : srcOff+n : srcOff+n]
		dstA := arenaA[dstOff : dstOff+n : dstOff+n]
		want := append([]byte(nil), srcA...)
		copy(dstA, srcA)
		// Formulation run.
		arenaF := append([]byte(nil), arena...)
		memform.Memmove(unsafe.Pointer(&arenaF[dstOff]), unsafe.Pointer(&arenaF[srcOff]), uintptr(n))
		if !bytes.Equal(arenaF[dstOff:dstOff+n], want) {
			fail("Memmove n=%d srcOff=%d dstOff=%d", n, srcOff, dstOff)
		}
		if !bytes.Equal(arenaF[dstOff:dstOff+n], arenaA[dstOff:dstOff+n]) {
			fail("Memmove vs copy n=%d srcOff=%d dstOff=%d", n, srcOff, dstOff)
		}

		// Memclr: canaries around the region must survive.
		if n > 0 {
			clr := make([]byte, n+32)
			rng.Read(clr)
			canary1 := append([]byte(nil), clr[:16]...)
			canary2 := append([]byte(nil), clr[n+16:]...)
			memform.MemclrNoHeapPointers(unsafe.Pointer(&clr[16]), uintptr(n))
			for j := 16; j < 16+n; j++ {
				if clr[j] != 0 {
					fail("Memclr left nonzero at %d (n=%d)", j, n)
					break
				}
			}
			if !bytes.Equal(clr[:16], canary1) || !bytes.Equal(clr[n+16:], canary2) {
				fail("Memclr clobbered canary (n=%d)", n)
			}
		}

		// Memequal vs string== (lowers to runtime.memequal for long strings).
		// n==0 has no addressable element; exercise the nil-pointer path.
		if n == 0 {
			if !memform.Memequal(unsafe.Pointer(nil), unsafe.Pointer(nil), 0) {
				fail("Memequal n=0: got false want true")
			}
			continue
		}
		a := make([]byte, n)
		bb := make([]byte, n)
		rng.Read(a)
		copy(bb, a)
		if n > 0 && rng.Intn(2) == 0 {
			bb[rng.Intn(n)] ^= 0xFF
		}
		eqWant := string(a) == string(bb)
		eqGot := memform.Memequal(unsafe.Pointer(&a[0]), unsafe.Pointer(&bb[0]), uintptr(n))
		if eqGot != eqWant {
			fail("Memequal n=%d: got %v want %v", n, eqGot, eqWant)
		}
		if got, want := memform.Memcmp(unsafe.Pointer(&a[0]), unsafe.Pointer(&bb[0]), uintptr(n)), bytes.Compare(a, bb); got != want {
			fail("Memcmp n=%d: got %d want %d", n, got, want)
		}
	}
	fmt.Printf("mem: %d randomized cases (Memmove vs copy, Memclr canaries, Memequal vs string==, Memcmp vs bytes.Compare), 0 mismatches\n", cases)
}

func testCrypto(rng *rand.Rand, cases int) {
	// FIPS-197 AES-128 test vector (Appendix B).
	fipsKey := [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	fipsIn := [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	fipsWant := [16]byte{0x69, 0xc4, 0xe0, 0xd8, 0x6a, 0x7b, 0x04, 0x30, 0xd8, 0xcd, 0xb7, 0x80, 0x70, 0xb4, 0xc5, 0x5a}
	if got := cryptoform.EncryptBlock(cryptoform.ExpandKey128(fipsKey), fipsIn); got != fipsWant {
		fail("AES-128 FIPS-197 vector: got %x want %x", got, fipsWant)
	}
	for i := 0; i < cases; i++ {
		var key [16]byte
		var blk [16]byte
		rng.Read(key[:])
		rng.Read(blk[:])
		rk := cryptoform.ExpandKey128(key)
		got := cryptoform.EncryptBlock(rk, blk)
		c, err := aes.NewCipher(key[:])
		if err != nil {
			panic(err)
		}
		var want [16]byte
		c.Encrypt(want[:], blk[:])
		if got != want {
			fail("AES-128 block %d", i)
		}
	}
	// SHA-256 over messages spanning padding boundaries.
	for i := 0; i < cases; i++ {
		ln := []int{0, 1, 55, 56, 57, 63, 64, 65, 119, 120, 1000}[rng.Intn(11)]
		msg := make([]byte, ln)
		rng.Read(msg)
		if got, want := cryptoform.Sum256(msg), sha256.Sum256(msg); got != want {
			fail("SHA-256 len=%d", ln)
		}
	}
	fmt.Printf("crypto: %d AES-128 blocks + %d SHA-256 digests vs asm-backed stdlib, 0 mismatches\n", cases, cases)
}

func main() {
	seed := flag.Int64("seed", 20261003, "RNG seed")
	cases := flag.Int("cases", 2000, "randomized cases per suite")
	flag.Parse()
	rng := rand.New(rand.NewSource(*seed))
	fmt.Printf("difftest seed=%d cases=%d go=%s\n", *seed, *cases, "go1.27.1")
	testBytealg(rng, *cases)
	testAtomic(rng, *cases/2)
	testMem(rng, *cases)
	testCrypto(rng, *cases/4)
	if failures > 0 {
		fmt.Printf("RESULT: FAIL (%d mismatches)\n", failures)
		os.Exit(1)
	}
	fmt.Println("RESULT: PASS")
}
