// Targets strings searching and trimming: Index/LastIndex/IndexByte/
// IndexRune (incl. RuneError and invalid runes against invalid UTF-8),
// IndexAny/LastIndexAny/IndexFunc, Trim*/TrimFunc, Compare, Contains*.
package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var hay = []string{
	"",
	"a",
	"abcabc",
	"x\xffy\xffz",
	"\xef\xbf\xbd",
	"日本語日本",
	"a\xe2\x82\xacb\xe2\x82",
	"  \t trim me \n ",
	"xxhixx",
	"\xed\xa0\x80",
	strings.Repeat("ab", 40) + "abc" + strings.Repeat("ab", 40),
}

var needles = []string{"", "a", "abc", "\xff", "�", "本", "日本", "b\xe2\x82", "xx", "ab" + "abc", "zzz"}

var runes = []rune{'a', 'b', utf8.RuneError, 0xFF, '本', -1, 0xD800, 0x110000, '€', ' '}

func main() {
	for _, h := range hay {
		qh := strconv.Quote(h)
		for _, n := range needles {
			println("idx", qh, strconv.Quote(n), strings.Index(h, n), strings.LastIndex(h, n),
				strings.Contains(h, n), strings.HasPrefix(h, n), strings.HasSuffix(h, n),
				strings.IndexAny(h, n), strings.LastIndexAny(h, n), strings.ContainsAny(h, n))
		}
		for _, r := range runes {
			println("rune", qh, r, strings.IndexRune(h, r), strings.ContainsRune(h, r))
		}
		for _, c := range []byte{'a', 0xff, 0xe2, ' ', 0} {
			println("byte", qh, c, strings.IndexByte(h, c), strings.LastIndexByte(h, c))
		}
		println("func", qh, strings.IndexFunc(h, unicode.IsSpace), strings.LastIndexFunc(h, unicode.IsSpace),
			strings.IndexFunc(h, func(r rune) bool { return r == utf8.RuneError }),
			strings.LastIndexFunc(h, func(r rune) bool { return r == utf8.RuneError }),
			strings.ContainsFunc(h, unicode.IsUpper))
		println("trim", qh,
			strconv.Quote(strings.TrimSpace(h)),
			strconv.Quote(strings.Trim(h, "ax\xff")),
			strconv.Quote(strings.TrimLeft(h, "a日")),
			strconv.Quote(strings.TrimRight(h, "本z�")),
			strconv.Quote(strings.TrimPrefix(h, "x")),
			strconv.Quote(strings.TrimSuffix(h, "x")),
			strconv.Quote(strings.TrimFunc(h, func(r rune) bool { return r == utf8.RuneError || r == 'a' })),
			strconv.Quote(strings.Trim(h, "")))
	}
	// Compare
	cs := []string{"", "a", "ab", "b", "\xff", "\x00", "A", "aa"}
	for _, a := range cs {
		for _, b := range cs {
			print(strings.Compare(a, b)+1, " ")
		}
		println()
	}
	// LastIndex with large haystacks (Rabin-Karp paths)
	big := strings.Repeat("abcdefghij", 500)
	println("big", strings.Index(big, "jabcdefghija"), strings.LastIndex(big, "ghijab"), strings.Index(big, "ghijabcdefghijX"))
	big2 := strings.Repeat("a", 1000) + "b"
	println("big2", strings.Index(big2, strings.Repeat("a", 64)+"b"), strings.Index(big2, strings.Repeat("a", 1001)))
	println("big3", strings.LastIndex(big2, "aab"), strings.Count(big2, "aa"), strings.Count(big2, "a"))
	// IndexAny with long char set (ASCII set vs not)
	set := "zyxwvutsrqponmlkj"
	println("any", strings.IndexAny("abcdefghijklmnop", set), strings.IndexAny("abc日", set+"日"), strings.LastIndexAny("日abc日d", "日\xff"))
	println("anyinv", strings.IndexAny("ab\xffc", "\xff"), strings.IndexAny("ab�c", "\xff"), strings.IndexAny("ab\xffc", "�"))
	// ContainsRune / IndexRune on RuneError vs real U+FFFD
	println("rerr", strings.IndexRune("\xff", utf8.RuneError), strings.IndexRune("a�", utf8.RuneError), strings.IndexRune("\xc0\x80", 0))
	// EqualFold vs Index of folded
	println("trimchain", strconv.Quote(strings.TrimLeftFunc("  \u00a0\u2028x y ", unicode.IsSpace)), strconv.Quote(strings.TrimRightFunc("x\u3000\u0085", unicode.IsSpace)))
}
