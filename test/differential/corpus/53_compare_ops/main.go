// 53_compare_ops: every comparison operator at signed/unsigned boundary
// values (unsigned ordering of values with the top bit set), strings
// (bytewise, prefix ordering), bools, mixed-in branches and switch.
package main

import "fmt"

func cmp64(a, b int64) [6]bool   { return [6]bool{a == b, a != b, a < b, a <= b, a > b, a >= b} }
func cmpu64(a, b uint64) [6]bool { return [6]bool{a == b, a != b, a < b, a <= b, a > b, a >= b} }
func cmpu8(a, b uint8) [6]bool   { return [6]bool{a == b, a != b, a < b, a <= b, a > b, a >= b} }
func cmpi8(a, b int8) [6]bool    { return [6]bool{a == b, a != b, a < b, a <= b, a > b, a >= b} }
func cmps(a, b string) [6]bool   { return [6]bool{a == b, a != b, a < b, a <= b, a > b, a >= b} }

var trace []string

func probe(name string, v bool) bool {
	trace = append(trace, name)
	return v
}

func main() {
	iv := []int64{-9223372036854775808, -1, 0, 1, 9223372036854775807}
	for _, a := range iv {
		for _, b := range iv {
			fmt.Println("i64", a, b, cmp64(a, b))
		}
	}
	uv := []uint64{0, 1, 9223372036854775807, 9223372036854775808, 18446744073709551615}
	for _, a := range uv {
		for _, b := range uv {
			fmt.Println("u64", a, b, cmpu64(a, b))
		}
	}
	for _, a := range []uint8{0, 127, 128, 255} {
		for _, b := range []uint8{0, 127, 128, 255} {
			fmt.Println("u8", a, b, cmpu8(a, b), "i8", cmpi8(int8(a), int8(b)))
		}
	}
	sv := []string{"", "a", "ab", "b", "B", "\xff", "é", "a\x00"}
	for _, a := range sv {
		for _, b := range sv {
			fmt.Printf("s %q %q %v\n", a, b, cmps(a, b))
		}
	}
	t, f := true, false
	fmt.Println("bool", t == f, t != f, t == !f, !(t && f), t || f, f || f && t)
	n := 0
	for i := -5; i <= 5; i++ {
		switch {
		case i < -2:
			n += 1
		case i <= 0:
			n += 10
		case i == 3:
			n += 100
		case i >= 4:
			n += 1000
		default:
			n += 10000
		}
	}
	fmt.Println("switch", n)
	// && and || short-circuit: the right operand must not be evaluated.
	r1 := probe("a", false) && probe("b", true)
	r2 := probe("c", true) || probe("d", false)
	r3 := probe("e", true) && probe("f", false) || probe("g", true)
	fmt.Println("short", r1, r2, r3, trace)
}
