// Targets range-over-string decoding: invalid bytes yield U+FFFD with a
// one-byte step, truncated sequences, overlong encodings, encoded
// surrogates, max rune boundaries, and index/rune pairs reported by range.
package main

import (
	"strconv"
)

var inputs = []string{
	"",
	"abc",
	"h\xc3\xa9llo",
	"\xff",
	"\xff\xfe\xfd",
	"\xc0\x80",         // overlong NUL
	"\xc1\xbf",         // overlong
	"\xe0\x80\x80",     // overlong 3-byte
	"\xe0\x9f\xbf",     // overlong 3-byte upper
	"\xe0\xa0\x80",     // U+0800 first valid
	"\xed\x9f\xbf",     // U+D7FF
	"\xed\xa0\x80",     // surrogate D800
	"\xed\xbf\xbf",     // surrogate DFFF
	"\xee\x80\x80",     // U+E000
	"\xf0\x80\x80\x80", // overlong 4
	"\xf0\x8f\xbf\xbf", // overlong 4 upper
	"\xf0\x90\x80\x80", // U+10000
	"\xf4\x8f\xbf\xbf", // U+10FFFF
	"\xf4\x90\x80\x80", // > max
	"\xf5\x80\x80\x80",
	"\xf8\x88\x80\x80\x80",
	"\xc3",
	"\xe2\x82",
	"\xf0\x9f\x98",
	"a\xe2\x82b",
	"\xe2\x82\xac\xe2\x82",
	"\x80\x80\x80",
	"\xc3\xa9\x80",
	"\xef\xbb\xbfBOM",
	"\xef\xbf\xbd",
	"\xef\xbf\xbe\xef\xbf\xbf",
	"日本\xff語",
	"\U0001F600x\U0001F601",
	"\x00\x7f\xc2\x80\xdf\xbf",
}

func rangeDump(s string) {
	print("in=", strconv.Quote(s), " len=", len(s), " :")
	n := 0
	for i, r := range s {
		print(" ", i, ":", int32(r))
		n++
	}
	println(" count=", n)
}

func rangeIndexOnly(s string) int {
	sum := 0
	for i := range s {
		sum = sum*31 + i
	}
	return sum
}

func rangeRuneOnly(s string) int64 {
	var sum int64
	for _, r := range s {
		sum = sum*7 + int64(r)
	}
	return sum
}

func manualDecode(s string) int {
	// counts invalid encodings via range: r == 0xFFFD with width 1
	bad := 0
	prev := -1
	prevR := rune(0)
	for i, r := range s {
		if prev >= 0 && prevR == 0xFFFD && i-prev == 1 {
			bad++
		}
		prev = i
		prevR = r
	}
	if prev >= 0 && prevR == 0xFFFD && len(s)-prev == 1 {
		bad++
	}
	return bad
}

func runeAtIndexes(s string) {
	// index into string at every byte gives raw bytes
	print("bytes:")
	for i := 0; i < len(s); i++ {
		print(" ", s[i])
	}
	println()
}

func main() {
	for _, s := range inputs {
		rangeDump(s)
		println("  idxsum=", rangeIndexOnly(s), " runesum=", rangeRuneOnly(s), " bad=", manualDecode(s))
		runeAtIndexes(s)
	}
	// range over a string built at runtime
	b := make([]byte, 0, 64)
	for i := 0; i < 40; i++ {
		b = append(b, byte(0x70+i*5))
	}
	s := string(b)
	rangeDump(s)
	// range over string with break/continue
	cnt := 0
	for i, r := range "a\xffb\xc3\xa9c\xed\xa0\x80d" {
		if r == 0xFFFD {
			cnt += i
			continue
		}
		if r == 'd' {
			println("break at", i)
			break
		}
	}
	println("cnt", cnt)
	// range over a string var reassigned in the loop: range expr evaluated once
	t := "xyz\xff"
	k := 0
	for i, r := range t {
		t = "q"
		k++
		println("reassign", i, r, len(t))
	}
	println("k", k)
	// range over string conversion of bytes
	bs := []byte{0xe2, 0x82, 0xac, 0xff, 0x41}
	for i, r := range string(bs) {
		bs[0] = 'Z'
		println("bs", i, r)
	}
	// range over string(rune slice)
	rs := []rune{0x41, -1, 0xD800, 0x110000, 0x10FFFF, 0}
	for i, r := range string(rs) {
		println("rs", i, r)
	}
	// range over int-converted strings
	for _, v := range []int{65, 0x20AC, 0xDFFF, -5, 0x7FFFFFFF} {
		str := string(rune(v))
		println("conv", v, strconv.Quote(str), len(str))
	}
}
