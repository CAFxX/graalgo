// Targets strings.Replacer (overlapping olds, empty old, priority order,
// byte/generic/single-string algorithms), Replace/ReplaceAll with empty
// old and n limits, strings.Map dropping and expanding runes, Repeat panics.
package main

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

func protect(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic str:", s)
			}
		}
	}()
	f()
}

var texts = []string{"", "a", "abc", "aaaa", "abcabcab", "hello world", "日本語日本", "\xffa\xff", "ababab", "<a&b>\"'"}

//go:noinline
func repeat(s string, n int) string { return strings.Repeat(s, n) }

func main() {
	reps := []*strings.Replacer{
		strings.NewReplacer("a", "1", "b", "2"),                     // byte replacer
		strings.NewReplacer("a", "", "b", "bb"),                     // byte string replacer
		strings.NewReplacer("ab", "X", "a", "Y", "abc", "Z"),        // generic, priority
		strings.NewReplacer("abc", "Z", "ab", "X"),                  // generic
		strings.NewReplacer("", "-"),                                // empty old
		strings.NewReplacer("", "-", "a", "A"),                      // empty with others
		strings.NewReplacer("a", "A", "", "-"),                      // order matters
		strings.NewReplacer("aa", "b"),                              // single string
		strings.NewReplacer("日本", "JP", "語", "go"),                  // multibyte
		strings.NewReplacer("\xff", "?"),                            // invalid byte
		strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;"), // html-ish
		strings.NewReplacer("a", "b", "a", "c"),                     // duplicate old: first wins
		strings.NewReplacer("ab", "ba", "ba", "ab"),
	}
	for i, r := range reps {
		for _, t := range texts {
			println("rep", i, strconv.Quote(t), strconv.Quote(r.Replace(t)))
		}
		var sb strings.Builder
		n, err := r.WriteString(&sb, "abcab\xff日本")
		println("write", i, n, err == nil, strconv.Quote(sb.String()))
	}
	protect("oddargs", func() { strings.NewReplacer("a") })
	// Replace with n limits
	for _, t := range texts {
		for _, old := range []string{"", "a", "ab", "\xff"} {
			for _, n := range []int{-1, 0, 1, 2} {
				println("Replace", strconv.Quote(t), strconv.Quote(old), n, strconv.Quote(strings.Replace(t, old, "<>", n)))
			}
		}
		println("ReplaceAll", strconv.Quote(strings.ReplaceAll(t, "", ".")))
	}
	// Map drop and expand
	drop := func(r rune) rune {
		if r == 'a' || r == utf8.RuneError {
			return -1
		}
		return r
	}
	expand := func(r rune) rune {
		if r < 0x80 {
			return r + 0x1F000
		}
		return r
	}
	invalid := func(r rune) rune {
		if r == 'b' {
			return 0xD800
		}
		if r == 'c' {
			return 0x110000
		}
		return r
	}
	for _, t := range texts {
		println("Map", strconv.Quote(t), strconv.Quote(strings.Map(drop, t)), strconv.QuoteToASCII(strings.Map(expand, t)), strconv.Quote(strings.Map(invalid, t)))
	}
	// identity Map returns same content
	s := "unchanged\xff"
	println("mapid", strings.Map(func(r rune) rune { return r }, s) == s)
	// Repeat
	for _, n := range []int{0, 1, 2, 5, 33} {
		r := repeat("ab\xff", n)
		println("Repeat", n, len(r), strconv.Quote(r[:min(len(r), 12)]))
	}
	println("RepeatEmpty", len(repeat("", 1<<40)))
	protect("negrepeat", func() { repeat("x", -1) })
	protect("overflow", func() { repeat("xx", 1<<62) })
	big := repeat("0123456789", 10000)
	println("bigrepeat", len(big), strings.Count(big, "90"), strings.LastIndex(big, "01"))
	// Builder
	var b strings.Builder
	println("builder0", b.Len(), b.Cap(), b.String() == "")
	b.Grow(10)
	println("grow", b.Len(), b.Cap() >= 10)
	b.WriteByte('x')
	b.WriteRune('é')
	b.WriteRune(-1)
	b.WriteRune(0xD800)
	b.WriteString("\xff")
	n, _ := b.Write([]byte("yz"))
	println("builder", b.Len(), n, strconv.Quote(b.String()))
	protect("grow-neg", func() { b.Grow(-1) })
	snap := b.String()
	b.WriteString("more")
	println("snapshot", strconv.Quote(snap), strconv.Quote(b.String()))
	b.Reset()
	println("reset", b.Len(), b.Cap())
	for i := 0; i < 1000; i++ {
		b.WriteString(strconv.Itoa(i))
	}
	println("builderbig", b.Len(), b.String()[2885:])
	protect("copy-builder", func() {
		var b1 strings.Builder
		b1.WriteString("a")
		b2 := b1
		b2.WriteString("b")
	})
}
