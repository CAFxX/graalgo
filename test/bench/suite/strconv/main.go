// strconv: Itoa/Atoi, FormatInt/ParseInt in base 16, AppendInt, Quote.
package main

import (
	"strconv"

	"graalgo/bench/h"
)

func work() int {
	t := 0
	var buf []byte
	for i := -50_000; i < 50_000; i += 3 {
		s := strconv.Itoa(i * 37)
		n, err := strconv.Atoi(s)
		if err != nil {
			panic(err)
		}
		t += n
		x := strconv.FormatInt(int64(i), 16)
		v, _ := strconv.ParseInt(x, 16, 64)
		t += int(v)
		buf = strconv.AppendInt(buf[:0], int64(i), 10)
		t += len(buf)
	}
	t += len(strconv.Quote("tab\there \"quoted\" ✓"))
	return t
}

func main() { h.Run("strconv", 20, work) }
