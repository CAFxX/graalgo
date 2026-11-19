// Targets unicode table lookups: Is* predicates over many ranges (sampled
// across the whole code space), In/IsOneOf with scripts, SimpleFold orbits,
// To with each case, and category hashes so any table mismatch shows.
package main

import (
	"strconv"
	"unicode"
)

type pred struct {
	name string
	f    func(rune) bool
}

var preds = []pred{
	{"Letter", unicode.IsLetter},
	{"Digit", unicode.IsDigit},
	{"Number", unicode.IsNumber},
	{"Space", unicode.IsSpace},
	{"Upper", unicode.IsUpper},
	{"Lower", unicode.IsLower},
	{"Title", unicode.IsTitle},
	{"Punct", unicode.IsPunct},
	{"Symbol", unicode.IsSymbol},
	{"Mark", unicode.IsMark},
	{"Control", unicode.IsControl},
	{"Graphic", unicode.IsGraphic},
	{"Print", unicode.IsPrint},
	{"Han", func(r rune) bool { return unicode.Is(unicode.Han, r) }},
	{"Greek", func(r rune) bool { return unicode.In(r, unicode.Greek) }},
	{"CyrLat", func(r rune) bool { return unicode.In(r, unicode.Cyrillic, unicode.Latin) }},
	{"Nd", func(r rune) bool { return unicode.Is(unicode.Nd, r) }},
	{"Zs", func(r rune) bool { return unicode.Is(unicode.Zs, r) }},
	{"WhiteSpace", func(r rune) bool { return unicode.Is(unicode.White_Space, r) }},
	{"OneOf", func(r rune) bool { return unicode.IsOneOf([]*unicode.RangeTable{unicode.Sc, unicode.Sm}, r) }},
}

var probes = []rune{-1, 0, 9, 10, 0x1c, 0x20, '0', '9', 'A', 'z', '_', 0x7f, 0x85, 0xa0, 0xaa, 0xb2, 0xb5, 0xbd,
	0xc0, 0xd7, 0xdf, 0x130, 0x131, 0x1c5, 0x2b0, 0x300, 0x345, 0x370, 0x3a3, 0x3c2, 0x400, 0x660, 0x669, 0x6f0,
	0x966, 0xe50, 0x1680, 0x1c80, 0x2000, 0x200b, 0x2028, 0x2029, 0x202f, 0x205f, 0x20ac, 0x2126, 0x212a, 0x2160,
	0x2460, 0x3000, 0x3007, 0x4e00, 0x9fff, 0xd800, 0xe000, 0xfeff, 0xff10, 0xff21, 0xfffd, 0xffff, 0x10400,
	0x1d7ce, 0x1f600, 0x20000, 0xe0001, 0x10ffff, 0x110000}

func main() {
	for _, r := range probes {
		print(r, ":")
		for _, p := range preds {
			if p.f(r) {
				print(" ", p.name)
			}
		}
		println()
	}
	// full sweep with sampling: counts per predicate across ranges
	for _, p := range preds {
		cnt := 0
		h := uint32(2166136261)
		for r := rune(0); r < 0x110000; r += 7 {
			if p.f(r) {
				cnt++
				h = (h ^ uint32(r)) * 16777619
			}
		}
		println("sweep", p.name, cnt, h)
	}
	// dense sweep of BMP for letters/digits/space
	var lc, dc, sc, uc int
	for r := rune(0); r < 0x10000; r++ {
		if unicode.IsLetter(r) {
			lc++
		}
		if unicode.IsDigit(r) {
			dc++
		}
		if unicode.IsSpace(r) {
			sc++
		}
		if unicode.IsUpper(r) {
			uc++
		}
	}
	println("bmp", lc, dc, sc, uc)
	// SimpleFold orbits
	for _, r := range []rune{'A', 'a', 'K', 'k', 0x212a, 'S', 's', 0x17f, 0x3a3, 0x3c2, 0x3c3, 0x1c4, 0x1c5, 0x1c6, '1', -1, 0x110000, 0x10400, 0xdf, 0x1e9e, 0x3b8, 0x3d1, 0x3f4} {
		print("fold ", r, ":")
		x := r
		for i := 0; i < 6; i++ {
			x = unicode.SimpleFold(x)
			print(" ", x)
			if x == r {
				break
			}
		}
		println()
	}
	// To with each case constant
	for _, r := range []rune{'a', 'A', 0x1c5, 0xdf, 0x130, 0x131, 0x3c2, 0x10428, 0x1c80, 0x2c65, -1} {
		println("to", r, unicode.To(unicode.UpperCase, r), unicode.To(unicode.LowerCase, r), unicode.To(unicode.TitleCase, r), unicode.To(7, r))
	}
	// category tables as data
	println("version", unicode.Version)
	println("ranges", len(unicode.Latin.R16), len(unicode.Latin.R32), unicode.Latin.LatinOffset, len(unicode.Upper.R16))
	keys := []string{"Lu", "Ll", "Nd", "Zs", "Greek", "Han"}
	for _, k := range keys {
		if t, ok := unicode.Categories[k]; ok {
			println("cat", k, len(t.R16), len(t.R32))
		} else if t, ok := unicode.Scripts[k]; ok {
			println("script", k, len(t.R16), len(t.R32))
		}
	}
	println("maxrune", unicode.MaxRune, unicode.ReplacementChar, unicode.MaxASCII, unicode.MaxLatin1)
	println("quote", strconv.QuoteRune(0x2028), strconv.QuoteRuneToASCII(0x1f600), strconv.QuoteRuneToGraphic(0x200b), strconv.IsPrint(0xad), strconv.IsGraphic(0x3000))
}
