// Extended XOF coverage vs gc: SHAKE256 hard-coded KATs (FIPS 202
// cross-checked via the empty-input prefix), chunked vs single reads,
// write-read-write continuity, and XOF clone. Hard-coded vectors (not
// impl-vs-impl) so the test is capable of failing.
package main

import (
	"bytes"
	"crypto/sha3"
	"encoding/hex"
	"fmt"
	"os"
)

var failures int

func fail(format string, args ...interface{}) {
	fmt.Printf("FAIL "+format+"\n", args...)
	failures++
}

// Generated from gc 1.27.1; empty-input prefix 46b9dd2b... cross-checked
// against the published FIPS 202 SHAKE256 value.
var shake256Kat = []struct {
	in   []byte
	size int
	want string
}{
	{[]byte(""), 32, "46b9dd2b0ba88d13233b3feb743eeb243fcd52ea62b81b82b50c27646ed5762f"},
	{[]byte(""), 64, "46b9dd2b0ba88d13233b3feb743eeb243fcd52ea62b81b82b50c27646ed5762fd75dc4ddd8c0f200cb05019d67b592f6fc821c49479ab48640292eacb3b7c4be"},
	{[]byte("abc"), 32, "483366601360a8771c6863080cc4114d8db44530f8f1e1ee4f94ea37e78b5739"},
	{[]byte("abc"), 64, "483366601360a8771c6863080cc4114d8db44530f8f1e1ee4f94ea37e78b5739d5a15bef186a5386c75744c0527e1faa9f8726e462a12a4feb06bd8801e751e4"},
}

func main() {
	// 1. SHAKE256 KATs.
	for _, v := range shake256Kat {
		h := sha3.NewSHAKE256()
		h.Write(v.in)
		got := make([]byte, v.size)
		h.Read(got)
		if hex.EncodeToString(got) != v.want {
			fail("shake256 inlen=%d size=%d", len(v.in), v.size)
		}
	}

	// 2. Chunked reads equal a single read (XOF stream continuity).
	for _, size := range []int{32, 64, 100} {
		h1 := sha3.NewSHAKE128()
		h1.Write([]byte("chunked-read-test-input"))
		whole := make([]byte, size)
		if _, err := h1.Read(whole); err != nil {
			fail("single read: %v", err)
		}
		h2 := sha3.NewSHAKE128()
		h2.Write([]byte("chunked-read-test-input"))
		chunked := make([]byte, 0, size)
		for len(chunked) < size {
			n := 7
			if size-len(chunked) < n {
				n = size - len(chunked)
			}
			buf := make([]byte, n)
			if _, err := h2.Read(buf); err != nil {
				fail("chunked read: %v", err)
			}
			chunked = append(chunked, buf...)
		}
		if !bytes.Equal(whole, chunked) {
			fail("chunked read != single read (size=%d)", size)
		}
	}

	// 3. Sequential reads continue the XOF stream: read(32)+read(32)
	// equals a single read(64). (Note: gc panics on Write-after-Read,
	// so interleaved writes are not valid XOF behavior.)
	h := sha3.NewSHAKE128()
	h.Write([]byte("sequential-reads"))
	r1 := make([]byte, 32)
	h.Read(r1)
	r2 := make([]byte, 32)
	h.Read(r2)
	ref := sha3.NewSHAKE128()
	ref.Write([]byte("sequential-reads"))
	want := make([]byte, 64)
	ref.Read(want)
	if !bytes.Equal(r1, want[:32]) {
		fail("sequential read part1 mismatch")
	}
	if !bytes.Equal(r2, want[32:]) {
		fail("sequential read part2 mismatch")
	}

	// 4. XOF clone is not covered: gc 1.27.1's *sha3.SHAKE has no Clone
	// method (only fixed-output *sha3.SHA3 implements hash.Cloner), so
	// there is no reference behavior to be differential against.

	if failures > 0 {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
