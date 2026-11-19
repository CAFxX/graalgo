//m1c:requires descriptors

// strings and bytes (upstream's source) over internal/bytealg's intrinsics:
// Index/Count/Compare/Equal/Cut/Fields/Replace/Builder through the public API.
package main

import (
	"bytes"
	"strings"
)

func main() {
	s := strings.Repeat("abcab", 20) + "XYZ" + strings.Repeat("ab", 40)
	println(strings.Index(s, "XYZ"), strings.Index(s, "abcabX"), strings.Index(s, "nope"), strings.LastIndex(s, "abc"))
	println(strings.Count(s, "ab"), strings.Count(s, ""), strings.IndexByte(s, 'Z'), strings.Contains(s, "cabX"))
	println(strings.Compare("a", "b"), strings.Compare("\xff", "\x00"), strings.Compare("", ""))
	println(bytes.Compare([]byte{0x80}, []byte{0x7f}), bytes.Equal(nil, []byte{}), bytes.Equal([]byte("a"), []byte("b")))
	before, after, found := strings.Cut("key=value=x", "=")
	println(before, after, found)
	println(len(strings.Fields(" a b  c ")), strings.Replace(s[:20], "ab", "-", -1))
	println(bytes.Index([]byte(s), []byte("YZab")), bytes.Count([]byte(s), []byte("b")))
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("xy")
	}
	println(b.Len(), b.Cap(), strings.ToUpper(b.String()[:6]))
	long := strings.Repeat("q", 100) + strings.Repeat("w", 64)
	println(strings.Index(long, strings.Repeat("w", 40)), strings.Index(long, "qw"))
}
