// Targets unicode classification and case mapping over the full rune range
// (Is* predicates, ToUpper/ToLower/ToTitle, SimpleFold orbits, In/IsOneOf
// with scripts) as per-predicate checksums, plus utf8 encode/decode/valid
// and utf16 encode/decode/surrogate sweeps including invalid inputs.
package main

import (
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

type acc struct {
	h uint64
	n int
}

func (a *acc) add(r rune) {
	a.h = (a.h ^ uint64(uint32(r))) * 1099511628211
	a.n++
}

// every rune of the BMP and SMP, then a stride through the upper planes
func next(r rune) rune {
	if r < 0x20000 {
		return r + 1
	}
	return r + 97
}

func main() {
	preds := []struct {
		name string
		f    func(rune) bool
	}{
		{"Letter", unicode.IsLetter}, {"Digit", unicode.IsDigit}, {"Number", unicode.IsNumber},
		{"Upper", unicode.IsUpper}, {"Lower", unicode.IsLower}, {"Title", unicode.IsTitle},
		{"Space", unicode.IsSpace}, {"Punct", unicode.IsPunct}, {"Symbol", unicode.IsSymbol},
		{"Mark", unicode.IsMark}, {"Control", unicode.IsControl}, {"Print", unicode.IsPrint},
		{"Graphic", unicode.IsGraphic},
		{"Han", func(r rune) bool { return unicode.Is(unicode.Han, r) }},
		{"Greek", func(r rune) bool { return unicode.In(r, unicode.Greek, unicode.Coptic) }},
		{"Cyrillic", func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }},
		{"Nd", func(r rune) bool { return unicode.Is(unicode.Nd, r) }},
		{"Zs", func(r rune) bool { return unicode.Is(unicode.Zs, r) }},
		{"Cf", func(r rune) bool { return unicode.Is(unicode.Cf, r) }},
		{"WhiteSpace", func(r rune) bool { return unicode.Is(unicode.White_Space, r) }},
		{"OneOf", func(r rune) bool { return unicode.IsOneOf([]*unicode.RangeTable{unicode.Arabic, unicode.Hebrew}, r) }},
	}
	accs := make([]acc, len(preds))
	for i := range accs {
		accs[i].h = 14695981039346656037
	}
	var up, lo, ti, fold acc
	up.h, lo.h, ti.h, fold.h = 14695981039346656037, 14695981039346656037, 14695981039346656037, 14695981039346656037
	for r := rune(-1); r <= unicode.MaxRune+1; r = next(r) {
		for i, p := range preds {
			if p.f(r) {
				accs[i].add(r)
			}
		}
		if u := unicode.ToUpper(r); u != r {
			up.add(r ^ u)
		}
		if l := unicode.ToLower(r); l != r {
			lo.add(r ^ l)
		}
		if t := unicode.ToTitle(r); t != r {
			ti.add(r ^ t)
		}
		if f := unicode.SimpleFold(r); f != r {
			fold.add(f)
		}
	}
	for i, p := range preds {
		println(p.name, accs[i].n, accs[i].h)
	}
	println("upper", up.n, up.h, "lower", lo.n, lo.h, "title", ti.n, ti.h, "fold", fold.n, fold.h)
	for _, r := range []rune{'k', 'K', 0x212a, 's', 0x17f, 0x1c4, 0x1c5, 0x1c6, 0x3a3, 0x3c2, 0x1e9e, 0xdf, -1, 0x110000} {
		print(r, ":", unicode.ToUpper(r), ",", unicode.ToLower(r), ",", unicode.ToTitle(r), " orbit")
		f := unicode.SimpleFold(r)
		for k := 0; k < 5 && f != r; k++ {
			print(" ", f)
			f = unicode.SimpleFold(f)
		}
		println()
	}
	println(unicode.TurkishCase.ToUpper('i'), unicode.TurkishCase.ToLower('I'), unicode.TurkishCase.ToTitle(0x131), unicode.To(unicode.UpperCase, 0x101))

	// utf8 sweep
	var h uint64 = 14695981039346656037
	buf := make([]byte, 0, 8)
	bad := 0
	for r := rune(-1); r <= unicode.MaxRune+1; r += 3 {
		buf = utf8.AppendRune(buf[:0], r)
		d, sz := utf8.DecodeRune(buf)
		dl, szl := utf8.DecodeLastRune(buf)
		h = (h ^ uint64(len(buf))<<32 ^ uint64(d)) * 1099511628211
		if d != dl || sz != szl || sz != len(buf) || utf8.RuneLen(r) != len(buf) && utf8.ValidRune(r) {
			bad++
		}
		if utf8.ValidRune(r) != (d == r) {
			bad++
		}
	}
	println("utf8", h, bad)
	// every 2-byte and 3-byte prefix pattern
	h = 14695981039346656037
	for a := 0; a < 256; a++ {
		for b := 0; b < 256; b += 3 {
			s := []byte{byte(a), byte(b), 0x80 | byte(a^b)&0x3f, 0xbf}
			r, n := utf8.DecodeRune(s)
			r2, n2 := utf8.DecodeRuneInString(string(s[:3]))
			h = (h ^ uint64(r)<<8 ^ uint64(n)) * 1099511628211
			h = (h ^ uint64(r2)<<8 ^ uint64(n2)) * 1099511628211
			if utf8.Valid(s) {
				h = (h ^ 1) * 1099511628211
			}
			if utf8.FullRune(s[:2]) {
				h = (h ^ 2) * 1099511628211
			}
			h = (h ^ uint64(utf8.RuneCount(s))) * 1099511628211
		}
	}
	println("utf8 bytes", h, utf8.RuneStart(0x80), utf8.RuneStart(0xc0), utf8.ValidString("a\xc0\x80"), utf8.RuneCountInString("\xf4\x90\x80\x80"))

	// utf16
	h = 14695981039346656037
	var rs []rune
	for r := rune(0xd000); r < 0x10100; r += 11 {
		rs = append(rs, r)
	}
	rs = append(rs, -5, 0x10ffff, 0x110000)
	enc := utf16.Encode(rs)
	for _, u := range enc {
		h = (h ^ uint64(u)) * 1099511628211
	}
	dec := utf16.Decode(enc)
	for _, r := range dec {
		h = (h ^ uint64(r)) * 1099511628211
	}
	println("utf16", len(enc), len(dec), h)
	r1, r2 := utf16.EncodeRune(0x1f600)
	println(r1, r2, utf16.DecodeRune(r1, r2), utf16.DecodeRune(r2, r1), utf16.IsSurrogate(0xdbff), utf16.IsSurrogate(0xe000), utf16.RuneLen(0x10000), utf16.RuneLen(0xd800))
	odd := utf16.Decode([]uint16{0xd800, 0x41, 0xdc00, 0xd83d, 0xde00, 0xdbff})
	for _, r := range odd {
		print(r, " ")
	}
	println()
	ap := utf16.AppendRune([]uint16{1}, 0x10437)
	ap = utf16.AppendRune(ap, 0xd800)
	for _, u := range ap {
		print(u, " ")
	}
	println()
}
