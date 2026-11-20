// bytes and strings operations: Index, Equal, ToUpper, Split, Fields,
// Replace, Contains.
package main

import (
	"bytes"
	"strings"

	"graalgo/bench/h"
)

var line = strings.Repeat("alpha beta gamma delta,", 200)
var blob = []byte(strings.Repeat("0123456789abcdef", 4096) + "needle")

func work() int {
	t := 0
	for r := 0; r < 200; r++ {
		t += bytes.Index(blob, []byte("needle"))
		if bytes.Equal(blob[:1024], blob[16:1040]) {
			t++
		}
		t += len(strings.Split(line, ","))
		t += len(strings.Fields(line))
		t += len(strings.ToUpper(line[:300]))
		t += len(strings.ReplaceAll(line, "beta", "BETA!"))
		if strings.Contains(line, "gamma delta,alpha") {
			t++
		}
	}
	return t
}

func main() { h.Run("bytesops", 30, work) }
