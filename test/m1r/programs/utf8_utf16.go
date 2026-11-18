package main

import (
	"unicode/utf16"
	"unicode/utf8"
)

func main() {
	s := "héllo, 世界 \xff\xfe 🎉 á"
	println(utf8.RuneCountInString(s), utf8.ValidString(s), utf8.ValidString("ok"), len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		println(i, r, n, utf8.RuneLen(r), utf8.ValidRune(r), utf8.RuneStart(s[i]))
		i += n
	}
	b := []byte(s)
	for len(b) > 0 {
		r, n := utf8.DecodeLastRune(b)
		print(r, ":", n, " ")
		b = b[:len(b)-n]
	}
	println()
	var buf [utf8.UTFMax]byte
	for _, r := range []rune{0, 'a', 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xd800, 0xdfff, 0xe000, 0xffff, 0x10000, 0x10ffff, 0x110000, -1} {
		n := utf8.EncodeRune(buf[:], r)
		print(r, " ", n, " [")
		for _, c := range buf[:n] {
			print(c, " ")
		}
		println("]", utf8.FullRune(buf[:n]), utf8.FullRune(buf[:n-1]))
	}
	var out []byte
	out = utf8.AppendRune(out, 'π')
	out = utf8.AppendRune(out, 0x1F600)
	out = utf8.AppendRune(out, 'x')
	println(string(out), len(out), utf8.RuneCount(out), utf8.Valid(out))
	u := utf16.Encode([]rune("a🎉b\U0010ffff"))
	for _, c := range u {
		print(c, " ")
	}
	println(string(utf16.Decode(u)), utf16.IsSurrogate(0xd800), utf16.RuneLen(0x1F600))
	r1, r2 := utf16.EncodeRune(0x1F600)
	println(r1, r2, utf16.DecodeRune(r1, r2), utf16.DecodeRune('a', 'b'))
	u = utf16.AppendRune(nil, 0x10437)
	println(len(u), u[0], u[1])
}
