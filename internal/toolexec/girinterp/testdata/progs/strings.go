package main

// String and rune iteration, byte indexing, conversions.

func main() {
	s := "aé€😀\xff!"
	for i, r := range s {
		println(i, r)
	}
	for i := range len(s) {
		print(s[i], " ")
	}
	println()
	n := 0
	for range s {
		n++
	}
	println("runes", n, "bytes", len(s))
	b := []byte(s)
	b[0] = 'A'
	println(string(b), s[1:3] == "é", s[len(s)-1:])
	var rs []rune
	for _, r := range "xyz" {
		rs = append(rs, r+1)
	}
	println(string(rs))
	m := map[string]int{"a": 1}
	m["b"] += 2
	m["a"]++
	v, ok := m["c"]
	println(m["a"], m["b"], v, ok, len(m))
	delete(m, "a")
	println(len(m))
	str := ""
	for i := 0; i < 3; i++ {
		str += string(rune('0' + i))
	}
	println(str, str < "1", "abc" > "abb")
}
