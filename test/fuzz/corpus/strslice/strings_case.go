// Targets case mapping and folding: ToUpper/ToLower/ToTitle on special
// runes (ß, İ, ı, ǅ, Kelvin sign, ſ, Greek sigma), invalid UTF-8 inside,
// EqualFold across fold orbits, ToUpperSpecial, and byte-length changes.
package main

import (
	"bytes"
	"strconv"
	"strings"
	"unicode"
)

var samples = []string{
	"",
	"hello WORLD",
	"straße",
	"İstanbul",
	"ıi",
	"ǅǆǄ",
	"KÅ",                  // Kelvin, Angstrom
	"ſ",                   // long s
	"ΣΑΣ σας ς",           // sigma
	"ﬁ ﬃ",                 // ligatures
	"ǰ",                   // j caron
	"ⱥȺ",                  // length changes
	"ɐⱯ",                  // turned a
	"ẞ",                   // capital sharp s
	"a\xffb\xc3",          // invalid
	"\xed\xa0\x80X",       // surrogate encoding
	"ｱｲｳ ＡＢＣ ａｂｃ",         // fullwidth
	"𐐨𐐀",                  // Deseret
	"ᲀ",                   // Cyrillic small rounded ve
	"\u0345",              // combining ypogegrammeni
	"ΩωΩ",                 // Ohm sign
	"µΜμ",                 // micro
	"ŉ",                   // n apostrophe
	"ΐ",                   // greek iota with diaeresis and tonos
	"ABCDEFGHIJKLMNOPQRS", // ascii fast path
}

var foldPairs = [][2]string{
	{"k", "K"}, {"k", "K"}, {"K", "K"}, {"s", "ſ"}, {"S", "ſ"},
	{"ß", "ẞ"}, {"ß", "ss"}, {"σ", "ς"}, {"Σ", "ς"}, {"İ", "i"}, {"ı", "I"},
	{"ǅ", "ǆ"}, {"ǅ", "Ǆ"}, {"Å", "Å"}, {"å", "Å"}, {"Ω", "Ω"},
	{"µ", "μ"}, {"µ", "Μ"}, {"\xff", "\xff"}, {"\xff", "\xfe"}, {"\xff", "�"},
	{"Go", "GO"}, {"Go", "Goo"}, {"", ""}, {"ǰ", "J\u030c"}, {"ⱥ", "Ⱥ"},
	{"helloK", "HELLOk"}, {"𐐨", "𐐀"}, {"ᲀ", "в"}, {"ᲀ", "В"},
}

func q(s string) string { return strconv.QuoteToASCII(s) }

func main() {
	for _, s := range samples {
		u := strings.ToUpper(s)
		l := strings.ToLower(s)
		t := strings.ToTitle(s)
		println("case", q(s), q(u), q(l), q(t), len(s), len(u), len(l), len(t))
		println("  bytes", string(bytes.ToUpper([]byte(s))) == u, string(bytes.ToLower([]byte(s))) == l)
		println("  turk", q(strings.ToUpperSpecial(unicode.TurkishCase, s)), q(strings.ToLowerSpecial(unicode.TurkishCase, s)))
		println("  azeri", q(strings.ToTitleSpecial(unicode.AzeriCase, s)))
		println("  roundtrip", strings.ToLower(u) == l, strings.EqualFold(u, l), strings.EqualFold(s, u))
	}
	for _, p := range foldPairs {
		println("fold", q(p[0]), q(p[1]), strings.EqualFold(p[0], p[1]), bytes.EqualFold([]byte(p[0]), []byte(p[1])))
	}
	// per-rune mapping functions
	for _, r := range []rune{'a', 'Z', 'ß', 'İ', 'ı', 'ǅ', 0x212A, 'ſ', 'ς', 0x345, 0x1E9E, 0xFFFD, -1, 0x110000, 0x10400, 0x1C80, 'µ', 0xDF} {
		println("rune", r, unicode.ToUpper(r), unicode.ToLower(r), unicode.ToTitle(r),
			unicode.IsUpper(r), unicode.IsLower(r), unicode.IsTitle(r))
		println("  special", unicode.TurkishCase.ToUpper(r), unicode.TurkishCase.ToLower(r), unicode.TurkishCase.ToTitle(r))
	}
	// Title-like: uppercase first letter of each word manually
	for _, s := range []string{"hello world", "ǆemal ǉubljana", "ßtraße ﬁsh", "\xffabc def"} {
		var sb strings.Builder
		prevSpace := true
		for _, r := range s {
			if prevSpace {
				sb.WriteRune(unicode.ToTitle(r))
			} else {
				sb.WriteRune(r)
			}
			prevSpace = unicode.IsSpace(r)
		}
		println("title", q(sb.String()))
	}
	// large mixed strings exercising non-ASCII slow path after ASCII prefix
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteByte(byte('a' + i%26))
		if i%50 == 49 {
			sb.WriteString("İß\xff")
		}
	}
	big := sb.String()
	bu := strings.ToUpper(big)
	bl := strings.ToLower(bu)
	println("big", len(big), len(bu), len(bl), q(bu[240:]), q(bl[:20]))
	// Map-style case conversion with EqualFold of large strings
	println("bigfold", strings.EqualFold(big, bu), strings.EqualFold(big, bl))
}
