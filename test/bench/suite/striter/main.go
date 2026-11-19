// String iteration: range over runes, byte indexing, strings.Index/Count.
package main

import (
	"strings"

	"graalgo/bench/h"
)

var text = strings.Repeat("héllo wörld, the quick brown fox ✓ jumps; ", 2000)

func work() int {
	t := 0
	for k := 0; k < 20; k++ {
		t += pass()
	}
	return t
}

func pass() int {
	t := 0
	for _, r := range text {
		t += int(r)
	}
	for i := 0; i < len(text); i++ {
		if text[i] == ' ' {
			t++
		}
	}
	t += strings.Count(text, "fox") + strings.Index(text, "✓ jumps; héllo") + strings.LastIndex(text, "quick")
	return t
}

func main() { h.Run("striter", 30, work) }
