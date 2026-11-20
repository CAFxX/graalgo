// Targets core string operations: concatenation in loops, comparison
// operators on bytes (not runes), indexing, slicing with bounds, len of
// multi-byte strings, string out-of-range panics, and substring sharing.
package main

import (
	"strconv"
)

var words = []string{"", "a", "A", "ab", "abc", "b", "\xff", "\xc3\xa9", "\xef\xbf\xbd", "é", "e\u0301", "\x00", "\x7f", "\x80", "z", "zz", "日", "\U0001F600"}

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

//go:noinline
func idx(s string, i int) byte { return s[i] }

//go:noinline
func sub(s string, i, j int) string { return s[i:j] }

func cmpAll() {
	for _, a := range words {
		for _, b := range words {
			print(a < b, a <= b, a > b, a >= b, a == b, a != b)
			print(" ")
		}
		println()
	}
}

func concatLoop(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += strconv.Itoa(i)
		if i%3 == 0 {
			s += "é"
		}
		if i%7 == 0 {
			s = s + "\xff" + s[:1]
		}
	}
	return s
}

func main() {
	cmpAll()
	for _, w := range words {
		println("len", strconv.Quote(w), len(w))
	}
	s := concatLoop(200)
	println("concat", len(s))
	h := 0
	for i := 0; i < len(s); i++ {
		h = h*131 + int(s[i])
		h &= 0xFFFFFFF
	}
	println("hash", h)
	println(strconv.Quote(s[:50]))
	println(strconv.Quote(s[len(s)-30:]))
	// multi-operand concatenation
	a, b, c, d := "x", "", "日本", "\xff"
	t := a + b + c + d + a + c
	println("multi", strconv.Quote(t), len(t))
	// slicing across rune boundaries
	u := "日本語テキスト"
	for i := 0; i <= len(u); i += 2 {
		println("slice", i, strconv.Quote(u[i:]), strconv.Quote(u[:i]))
	}
	// indexing yields bytes
	for i := 0; i < len(u); i += 3 {
		println("idx", i, idx(u, i), idx(u, i+1))
	}
	// out of range
	protect("idx-oob", func() { println(idx(u, len(u))) })
	protect("idx-neg", func() { println(idx(u, -1)) })
	protect("sub-hi", func() { println(sub(u, 0, len(u)+1)) })
	protect("sub-inv", func() { println(sub(u, 5, 3)) })
	protect("sub-neg", func() { println(sub(u, -2, 3)) })
	protect("sub-ok", func() { println(strconv.Quote(sub(u, len(u), len(u)))) })
	protect("empty-idx", func() { println(idx("", 0)) })
	// string comparisons built at runtime
	x := string([]byte{0xff})
	y := string([]byte{0x7f})
	println("cmpbytes", x > y, x < "ÿ", "ÿ" < "Ā")
	// equality between differently-constructed strings
	p1 := "hel" + strconv.Itoa(1) + "o"
	p2 := string([]byte{'h', 'e', 'l', '1', 'o'})
	p3 := string([]rune{'h', 'e', 'l', '1', 'o'})
	println("eq", p1 == p2, p2 == p3, p1 < p3, p1 > p3)
	// += with self
	q := "ab"
	for i := 0; i < 5; i++ {
		q += q
	}
	println("self", len(q), q[:8])
	// string switch
	for _, w := range words {
		switch w {
		case "":
			println("sw empty")
		case "é", "e\u0301":
			println("sw eacute")
		case "\xff":
			println("sw ff")
		default:
			println("sw other", len(w))
		}
	}
	// strings as array of bytes in a loop of slicing
	r := "abcdefghij"
	for len(r) > 0 {
		print(r[0:1], len(r), " ")
		r = r[1:]
	}
	println()
	// byte conversion loops
	var buf [8]byte
	for i := range buf {
		buf[i] = byte('A' + i)
	}
	bs := string(buf[:])
	buf[0] = 'z'
	println("arr", bs, string(buf[2:5]))
	// comparisons with prefix relationships
	pre := []string{"abc", "abcd", "ab", "abd", "abc\x00"}
	for i := range pre {
		for j := range pre {
			if pre[i] < pre[j] {
				print(i, "<", j, " ")
			}
		}
	}
	println()
}
