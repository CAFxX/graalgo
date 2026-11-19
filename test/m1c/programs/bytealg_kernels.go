//m1c:ldflags -checklinkname=0

// internal/bytealg's assembly functions (the registry's intrinsics), called
// directly through //go:linkname pulls — the only way to reach them before
// strings/bytes link (they import errors: W9a) — against gc's amd64 assembly.
// gc's linker refuses pulls of std internals (checklinkname); the directive
// above turns that off for both legs. Sweeps lengths across the assembly's
// block sizes (16/32/64) and, for Index, its domain 2 <= len(pattern) <=
// MaxLen <= len(s) (the callers' precondition; the assembly faults outside), sub-slices and sub-strings at odd
// offsets, unsigned byte order (0x00/0x7f/0x80/0xff) and empty operands; the
// output is a checksum per function plus the edge cases verbatim.
package main

import _ "unsafe"

//go:linkname compare internal/bytealg.Compare
func compare(a, b []byte) int

//go:linkname indexByte internal/bytealg.IndexByte
func indexByte(b []byte, c byte) int

//go:linkname indexByteString internal/bytealg.IndexByteString
func indexByteString(s string, c byte) int

//go:linkname count internal/bytealg.Count
func count(b []byte, c byte) int

//go:linkname countString internal/bytealg.CountString
func countString(s string, c byte) int

//go:linkname index internal/bytealg.Index
func index(a, b []byte) int

//go:linkname indexString internal/bytealg.IndexString
func indexString(a, b string) int

//go:linkname makeNoZero internal/bytealg.MakeNoZero
func makeNoZero(n int) []byte

type sum struct {
	name string
	h    uint64
	n    int
}

func (s *sum) add(v int) {
	s.h = s.h*1000003 ^ uint64(int64(v))
	s.n++
}

func (s *sum) print() { println(s.name, s.n, s.h) }

var seed uint32 = 1

func rnd() uint32 {
	seed = seed*1664525 + 1013904223
	return seed >> 8
}

// gen returns n bytes from a small alphabet so that matches are frequent.
func gen(n int, alpha string) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[rnd()%uint32(len(alpha))]
	}
	return b
}

var g []byte

func main() {
	alphas := []string{"ab", "abc\x00", "\x00\x7f\x80\xff", "xyzw"}
	cmp := &sum{name: "Compare"}
	ib := &sum{name: "IndexByte"}
	cnt := &sum{name: "Count"}
	idx := &sum{name: "Index"}
	for _, alpha := range alphas {
		for n := 0; n <= 130; n++ {
			buf := gen(n+7, alpha)
			for _, off := range []int{0, 1, 3, 7} {
				a := buf[off : off+n]
				s := string(buf)[off : off+n]
				for _, c := range []byte(alpha + "q") {
					ib.add(indexByte(a, c))
					ib.add(indexByteString(s, c))
					cnt.add(count(a, c))
					cnt.add(countString(s, c))
				}
				b := append([]byte(nil), a...)
				cmp.add(compare(a, b))
				if n > 0 {
					b[rnd()%uint32(n)] ^= 0x80
					cmp.add(compare(a, b))
					cmp.add(compare(b, a))
					cmp.add(compare(a, b[:n-1]))
					cmp.add(compare(a[:n-1], b))
				}
				for _, m := range []int{2, 3, 4, 8, 15, 16, 17, 24, 30, 31} { // the assembly's domain: 2 <= m <= MaxLen (31 without AVX2)
					if m > n {
						continue
					}
					at := int(rnd() % uint32(n-m+1))
					pat := a[at : at+m]
					idx.add(index(a, pat))
					idx.add(indexString(s, string(pat)))
					if m < n && m < 31 {
						miss := append(append([]byte(nil), pat...), 'Q')
						idx.add(index(a, miss))
						idx.add(indexString(s, string(miss)))
					}
				}
			}
		}
	}
	cmp.print()
	ib.print()
	cnt.print()
	idx.print()

	println(compare(nil, nil), compare(nil, []byte{}), compare([]byte{0}, nil), compare(nil, []byte{0}))
	println(compare([]byte{0x7f}, []byte{0x80}), compare([]byte{0xff}, []byte{0x00}), compare([]byte("abc"), []byte("abd")))
	println(indexByte(nil, 0), indexByteString("", 'a'), count(nil, 0), countString("", 0))
	println(indexString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab", "aaab"), indexString("xxabxxab", "ab"))
	for _, n := range []int{0, 1, 7, 8, 9, 24, 25, 100, 1000, 1023, 1025, 4097, 32760, 32761, 32768, 32769, 100000} {
		b := makeNoZero(n)
		println("MakeNoZero", n, len(b), cap(b))
	}
	// append's growth goes through the same size classes (gc growslice), up
	// to and past the largest small class.
	// (g is a package variable: a local one that does not escape gets gc's
	// 32-byte stack buffer for its first appends.)
	last := -1
	for len(g) < 70000 {
		g = append(g, 1)
		if cap(g) != last {
			last = cap(g)
			print(last, " ")
		}
	}
	println()
}
