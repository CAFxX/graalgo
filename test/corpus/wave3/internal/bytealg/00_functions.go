package main

import (
	"bytes"
	"fmt"
	"internal/bytealg"
	"strings"
)

// SCOPE-LIMITED + NOGC: external programs cannot import internal/* at all
// (go tool restriction), so run_wave3.sh skips the gc leg (nogc:) and the
// graalgo leg (user imports of internal/* are not admitted; the package is
// synthetic-only, like internal/reflectlite).
//
// CORRECTNESS EVIDENCE: this program is self-checking — every bytealg
// result is cross-checked against the stdlib wrappers that use it. The
// tier1d harness (research scratch, removed with the phases/ layout)
// extracts the synthetic source to a GOPATH and runs this exact program
// under stock gc against it. Any MISMATCH line + panic = harness FAIL.
func main() {
	bad := 0
	check := func(name string, ok bool) {
		if !ok {
			fmt.Println("MISMATCH:", name)
			bad++
		}
	}
	inputs := [][]byte{
		{},
		{0},
		[]byte("hello world"),
		[]byte("aaaaa"),
		{0, 1, 2, 3, 2, 1, 0},
		bytes.Repeat([]byte("ab"), 1000),
		bytes.Repeat([]byte("x"), 4096),
	}
	seps := [][]byte{
		{}, {0}, []byte("o"), []byte("aa"), []byte("hello"),
		[]byte("z"), []byte("abab"), []byte("world"),
		bytes.Repeat([]byte("ab"), 500),
	}
	for _, in := range inputs {
		sin := string(in)
		for _, sep := range seps {
			ssep := string(sep)
			check("Index", bytealg.Index(in, sep) == bytes.Index(in, sep))
			check("IndexString", bytealg.IndexString(sin, ssep) == strings.Index(sin, ssep))
			// IndexRabinKarp/LastIndexRabinKarp carry gc's precondition
			// (0 < len(sep) <= len(s); bytes/strings enforce it before
			// calling). Only test within the precondition.
			if len(sep) > 0 && len(sep) <= len(in) {
				check("IndexRabinKarp", bytealg.IndexRabinKarp(in, sep) == bytes.Index(in, sep))
				check("IndexRabinKarpString", bytealg.IndexRabinKarp(sin, ssep) == strings.Index(sin, ssep))
				check("LastIndexRabinKarp", bytealg.LastIndexRabinKarp(in, sep) == bytes.LastIndex(in, sep))
				check("LastIndexRabinKarpString", bytealg.LastIndexRabinKarp(sin, ssep) == strings.LastIndex(sin, ssep))
			}
		}
		for c := 0; c < 256; c += 51 {
			cb := byte(c)
			check("IndexByte", bytealg.IndexByte(in, cb) == bytes.IndexByte(in, cb))
			check("IndexByteString", bytealg.IndexByteString(sin, cb) == strings.IndexByte(sin, cb))
			check("LastIndexByte", bytealg.LastIndexByte(in, cb) == bytes.LastIndexByte(in, cb))
			check("LastIndexByteString", bytealg.LastIndexByteString(sin, cb) == strings.LastIndexByte(sin, cb))
			check("Count", bytealg.Count(in, cb) == bytes.Count(in, []byte{cb}))
			check("CountString", bytealg.CountString(sin, cb) == strings.Count(sin, string([]byte{cb})))
		}
		check("EqualSelf", bytealg.Equal(in, in))
		check("CompareSelf", bytealg.Compare(in, in) == 0)
		check("CompareStringSelf", bytealg.CompareString(sin, sin) == 0)
		for _, other := range inputs {
			check("Compare", bytealg.Compare(in, other) == bytes.Compare(in, other))
			check("CompareString", bytealg.CompareString(sin, string(other)) == strings.Compare(sin, string(other)))
			check("Equal", bytealg.Equal(in, other) == bytes.Equal(in, other))
		}
	}
	check("Cutover0", bytealg.Cutover(0) == (0+16)/8)
	check("Cutover100", bytealg.Cutover(100) == (100+16)/8)
	check("Cutover1000000", bytealg.Cutover(1000000) == (1000000+16)/8)
	b := bytealg.MakeNoZero(64)
	check("MakeNoZeroLen", len(b) == 64)
	check("MakeNoZeroCap", cap(b) >= 64)
	b0 := bytealg.MakeNoZero(0)
	check("MakeNoZero0", len(b0) == 0)
	check("MaxBruteForce", bytealg.MaxBruteForce == 0)
	check("MaxLen", bytealg.MaxLen == 0)
	if bad > 0 {
		panic("bytealg self-check failed")
	}
	fmt.Println("ok")
}
