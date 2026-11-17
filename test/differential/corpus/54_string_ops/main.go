// 54_string_ops: string concatenation, len, byte indexing, slicing,
// comparison, conversion round trips, range (runes and byte offsets),
// += in loops, and string-keyed switch.
package main

import "fmt"

func main() {
	s := "héllo, 世界!"
	fmt.Println(len(s), s[0], s[1], s[2], s[len(s)-1], s[1:3], s[7:], s[:0] == "")
	t := s + "+" + s[:5]
	fmt.Println(t, len(t))
	acc := ""
	for i := 0; i < 10; i++ {
		acc += string(rune('a' + i))
		if i%3 == 0 {
			acc += "|"
		}
	}
	fmt.Println(acc, len(acc))
	for i, r := range "aé世\U0001F600z" {
		fmt.Println("r", i, r, string(r))
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			n++
		}
	}
	fmt.Println("high bytes", n)
	words := []string{"go", "graal", "gc", "go", "", "Go"}
	for _, w := range words {
		switch w {
		case "go":
			fmt.Println("lower go")
		case "Go", "GO":
			fmt.Println("upper go")
		case "":
			fmt.Println("empty")
		default:
			fmt.Println("other", w, w < "go", w > "gc")
		}
	}
	b := []byte("abc")
	b = append(b, "def"...)
	fmt.Println(string(b), string(b[2:4]), cap(b) >= 6)
	rev := func(x string) string {
		r := []rune(x)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return string(r)
	}
	fmt.Println(rev(s), rev(""), rev("a"))
	fmt.Printf("%q %x %X % x %5s|%-5s|%.2s\n", s, "hi", "hi", "hi", "ab", "ab", s)
}
