// Targets rune/string conversions: string(rune) of invalid, negative and
// surrogate runes, []rune(s) of invalid UTF-8, string([]rune) round trips,
// []byte(s) independence, and string(byte) vs string(rune) widths.
package main

import (
	"strconv"
)

var runes = []rune{0, 1, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdbff, 0xdc00, 0xdfff, 0xe000,
	0xfffd, 0xfffe, 0xffff, 0x10000, 0x10ffff, 0x110000, -1, -0x80000000, 0x7fffffff, 0x1F600}

var strs = []string{"", "abc", "\xff", "a\xffb", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xc3", "€\xe2\x82", "日本語", "\xc0\xaf"}

//go:noinline
func toStr(r rune) string { return string(r) }

//go:noinline
func toStrInt(i int64) string { return string(rune(i)) }

//go:noinline
func toStrByte(b byte) string { return string(b) }

func runeSliceDump(rs []rune) {
	print("len=", len(rs), " [")
	for i, r := range rs {
		if i > 0 {
			print(" ")
		}
		print(int32(r))
	}
	println("]")
}

func main() {
	for _, r := range runes {
		s := toStr(r)
		println("rune", int32(r), strconv.Quote(s), len(s), strconv.QuoteToASCII(s))
	}
	for _, i := range []int64{-1, 0x41, 0x10ffff, 0x110000, 1 << 32, 1<<32 + 0x41, -1 << 40} {
		s := toStrInt(i)
		println("int64", i, strconv.Quote(s))
	}
	for b := 0; b < 256; b += 17 {
		s := toStrByte(byte(b))
		println("byte", b, strconv.Quote(s), len(s))
	}
	for _, s := range strs {
		rs := []rune(s)
		print("runes(", strconv.Quote(s), ") ")
		runeSliceDump(rs)
		back := string(rs)
		println("  back", strconv.Quote(back), back == s, len(back))
	}
	// string([]rune) with invalid runes
	rs := []rune{'a', -1, 0xD800, 0x110000, 'b', 0xDFFF, 0x10FFFF}
	s := string(rs)
	println("inv", strconv.Quote(s), len(s))
	rs2 := []rune(s)
	runeSliceDump(rs2)
	// empty / nil rune slices
	var nilr []rune
	println("nilrune", strconv.Quote(string(nilr)), len(string(nilr)))
	println("emptyrunes", len([]rune("")), []rune("") == nil)
	// []byte(s) is an independent copy
	src := "hello, world"
	b1 := []byte(src)
	b2 := []byte(src)
	b1[0] = 'J'
	b2[1] = 'A'
	println("copies", string(b1), string(b2), src)
	// string(b) is a copy too
	bb := []byte("mutable")
	str := string(bb)
	bb[0] = 'M'
	println("strcopy", str, string(bb))
	// []rune modifications don't affect source
	rr := []rune("héllo")
	rr[1] = 'E'
	println("rr", string(rr), len(rr))
	// append runes to []byte via string
	var out []byte
	for _, r := range runes {
		out = append(out, string(r)...)
	}
	println("appended", len(out), strconv.QuoteToASCII(string(out)))
	// conversion of big rune slices
	big := make([]rune, 1000)
	for i := range big {
		big[i] = rune(i * 1117 % 0x110000)
	}
	bs := string(big)
	println("big", len(bs), len([]rune(bs)))
	h := uint32(0)
	for i := 0; i < len(bs); i++ {
		h = h*16777619 ^ uint32(bs[i])
	}
	println("bighash", h)
	// rune comparisons to string elements
	cnt := 0
	for _, r := range bs {
		if r == 0xFFFD {
			cnt++
		}
	}
	println("fffd", cnt)
	// byte to rune widening
	var by byte = 0xE9
	println("widen", strconv.Quote(string(rune(by))), strconv.Quote(string(by)))
	// conversions in constant vs runtime context
	const cr = 0x20AC
	println("const", strconv.Quote(string(rune(cr))), strconv.Quote(toStr(cr)))
}
