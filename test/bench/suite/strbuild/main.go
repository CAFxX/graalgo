// String building: strings.Builder, += concatenation, conversions.
package main

import (
	"strings"

	"graalgo/bench/h"
)

func work() int {
	var b strings.Builder
	for i := 0; i < 100_000; i++ {
		b.WriteString("ab")
		b.WriteByte(byte('0' + i%10))
	}
	s := b.String()
	t := len(s)
	acc := ""
	for i := 0; i < 2000; i++ {
		acc += string(rune('a' + i%26))
	}
	bs := []byte(s[:1000])
	return t + len(acc) + int(acc[1999]) + len(string(bs))
}

func main() { h.Run("strbuild", 30, work) }
