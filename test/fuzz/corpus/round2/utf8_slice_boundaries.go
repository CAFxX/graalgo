// UTF-8 decoding at arbitrary byte boundaries: range over s[i:j] cutting
// multi-byte runes, []rune / string(rune) conversions of invalid input,
// utf8 Decode*/Valid/FullRune, and strings.Builder with large growth.
package main

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

func walk(s string) string {
	var b strings.Builder
	for i, r := range s {
		b.WriteString(strconv.Itoa(i))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(int64(r), 16))
		b.WriteByte(' ')
	}
	return b.String()
}

func main() {
	s := "a\xc3\xa9\xe2\x82\xac\xf0\x9f\x98\x80z"
	println("len", len(s), utf8.RuneCountInString(s))
	for i := 0; i <= len(s); i++ {
		for j := i; j <= len(s); j += 3 {
			println(i, j, walk(s[i:j]), utf8.ValidString(s[i:j]), utf8.RuneCountInString(s[i:j]))
		}
	}

	bad := []string{"\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe2\x82", "\xf0\x9f\x98", "\x80\x80", "\xef\xbf\xbd", "\xf8\x88\x80\x80\x80"}
	for _, b := range bad {
		r, n := utf8.DecodeRuneInString(b)
		lr, ln := utf8.DecodeLastRuneInString(b)
		rs := []rune(b)
		back := string(rs)
		println(strconv.Quote(b), r, n, lr, ln, len(rs), len(back), utf8.FullRuneInString(b), walk(b))
	}

	println("string(rune) invalid", strconv.Quote(string(rune(-1))), strconv.Quote(string(rune(0xD800))), strconv.Quote(string(rune(0x110000))))
	rs := []rune{0x41, -5, 0xDFFF, 0x10FFFF, 0x1F600}
	println("runes->string", strconv.Quote(string(rs)), len(string(rs)))
	println("RuneLen", utf8.RuneLen(0xD800), utf8.RuneLen(0x10FFFF), utf8.RuneLen(-1), utf8.ValidRune(0xDFFF))
	buf := utf8.AppendRune(nil, 0xD800)
	println("AppendRune surrogate", strconv.Quote(string(buf)))
	var enc [4]byte
	n := utf8.EncodeRune(enc[:], 0x1F600)
	println("EncodeRune", n, enc[0], enc[3])

	bs := []byte(s)
	for len(bs) > 0 {
		r, size := utf8.DecodeLastRune(bs)
		print(r, "/", size, " ")
		bs = bs[:len(bs)-size]
	}
	println()
	println("bytes.Runes", len(bytes.Runes([]byte("\xe2\x82\xac\xe2\x82"))))
	println("ToValidUTF8", strconv.Quote(strings.ToValidUTF8("a\xff\xfeb\xe2\x82", "?")))
	println("Map", strconv.Quote(strings.Map(func(r rune) rune {
		if r == utf8.RuneError {
			return -1
		}
		return r + 1
	}, "ab\xffc")))

	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("\xe2\x82\xac")
		sb.WriteRune(rune('a' + i%26))
		if i%1000 == 999 {
			sb.WriteByte(0xff)
		}
	}
	big := sb.String()
	println("builder", sb.Len(), len(big), utf8.RuneCountInString(big), utf8.ValidString(big))
	cnt, errs := 0, 0
	for _, r := range big[1 : len(big)-1] {
		cnt++
		if r == utf8.RuneError {
			errs++
		}
	}
	println("range big", cnt, errs, strings.Count(big, "\xe2\x82\xaca"), strings.LastIndex(big, "\xff"))
	sb.Reset()
	sb.Grow(1 << 20)
	sb.WriteString(strings.Repeat("xy", 1<<18))
	println("grow", sb.Len(), big[len(big)-4:] == "\xe2\x82\xacz", strings.IndexRune(big, 'q'))
	var bb bytes.Buffer
	bb.WriteString(big[:7])
	r, size, err := bb.ReadRune()
	println("buffer ReadRune", r, size, err == nil, bb.Len())
	bb.UnreadRune()
	println("after unread", bb.Len())
}
