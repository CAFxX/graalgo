// Targets strings splitting: Split/SplitN/SplitAfter/SplitAfterN with
// n=-1,0,1,2,3 and empty separators (rune splitting of invalid UTF-8),
// Fields/FieldsFunc with unicode spaces, Cut/CutPrefix/CutSuffix, Count.
package main

import (
	"strconv"
	"strings"
	"unicode"
)

func show(name string, parts []string) {
	print(name, " n=", len(parts), " nil=", parts == nil, " [")
	for i, p := range parts {
		if i > 0 {
			print(",")
		}
		print(strconv.Quote(p))
	}
	println("]")
}

var inputs = []string{
	"",
	"a",
	"a,b,c",
	",a,,b,",
	",,,",
	"a\xff,b\xfe",
	"日本,語,\xe2\x82",
	"abcabcabc",
	"  lead and trail  ",
	"\u00a0nbsp\u2003em\u3000ideo\u0085nel\tx\ny\v\fz\r",
	"\u200bzwsp\u180emvs",
}

var seps = []string{"", ",", "abc", "a", "\xff", "日", ",,", "zz"}

func main() {
	for _, s := range inputs {
		for _, sep := range seps {
			tag := strconv.Quote(s) + "/" + strconv.Quote(sep)
			show("Split "+tag, strings.Split(s, sep))
			for _, n := range []int{-1, 0, 1, 2, 3} {
				show("SplitN"+strconv.Itoa(n)+" "+tag, strings.SplitN(s, sep, n))
			}
			show("SplitAfter "+tag, strings.SplitAfter(s, sep))
			show("SplitAfterN2 "+tag, strings.SplitAfterN(s, sep, 2))
			println("Count", tag, strings.Count(s, sep))
			before, after, found := strings.Cut(s, sep)
			println("Cut", tag, strconv.Quote(before), strconv.Quote(after), found)
		}
		show("Fields "+strconv.Quote(s), strings.Fields(s))
		show("FieldsFunc-punct "+strconv.Quote(s), strings.FieldsFunc(s, unicode.IsPunct))
		show("FieldsFunc-ffd "+strconv.Quote(s), strings.FieldsFunc(s, func(r rune) bool { return r == 0xFFFD }))
		var sb strings.Builder
		for f := range strings.FieldsSeq(s) {
			sb.WriteString("<" + f + ">")
		}
		println("FieldsSeq", strconv.Quote(sb.String()))
		sb.Reset()
		for p := range strings.SplitSeq(s, ",") {
			sb.WriteString("[" + p + "]")
		}
		println("SplitSeq", strconv.Quote(sb.String()))
	}
	for _, p := range []string{"", "a", "ab", "abc", "\xff"} {
		r1, ok1 := strings.CutPrefix("abcab", p)
		r2, ok2 := strings.CutSuffix("cabab", p)
		println("CutPrefix", strconv.Quote(p), r1, ok1, r2, ok2)
	}
	// Count with empty sep counts runes+1, including invalid bytes
	for _, s := range []string{"", "abc", "\xff\xfe", "日本", "a\xe2\x82b"} {
		println("CountEmpty", strconv.Quote(s), strings.Count(s, ""), len([]rune(s))+1)
	}
	// Join round trips
	parts := strings.Split("x,,y,", ",")
	println("join", strconv.Quote(strings.Join(parts, "+")), strconv.Quote(strings.Join(nil, "+")))
	println("join1", strings.Join([]string{"solo"}, "--"))
	// Lines
	var lb strings.Builder
	for l := range strings.Lines("a\nb\r\n\nlast") {
		lb.WriteString(strconv.Quote(l))
	}
	println("lines", lb.String())
	// Split result aliasing: modifying returned slice doesn't affect later splits
	a := strings.Split("p q r", " ")
	a[0] = "Z"
	b := strings.Split("p q r", " ")
	println("alias", a[0], b[0], cap(a) >= len(a))
}
