// map[string]int: string hashing and comparison, lookups of present and
// absent keys.
package main

import (
	"strconv"

	"graalgo/bench/h"
)

var keys = func() []string {
	ks := make([]string, 20_000)
	for i := range ks {
		ks[i] = "key-" + strconv.Itoa(i*31)
	}
	return ks
}()

func work() int {
	m := make(map[string]int, len(keys))
	for i, k := range keys {
		m[k] = i
	}
	t := 0
	for r := 0; r < 5; r++ {
		for _, k := range keys {
			t += m[k]
		}
		if _, ok := m["absent"]; ok {
			t++
		}
	}
	return t + len(m)
}

func main() { h.Run("mapstr", 20, work) }
