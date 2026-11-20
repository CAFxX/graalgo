// textproc: a word-frequency report (log/text-analysis utility class):
// strings.Fields, ToLower, map counting, sort.Slice, strconv, fmt.Sprintf
// into a strings.Builder.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"graalgo/bench/h"
)

var text string

func init() {
	seed := uint32(3)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	vocab := make([]string, 3000)
	for i := range vocab {
		w := make([]byte, 2+rnd(9))
		for j := range w {
			w[j] = byte('a' + rnd(26))
			if rnd(10) == 0 {
				w[j] -= 'a' - 'A'
			}
		}
		vocab[i] = string(w)
	}
	var sb strings.Builder
	for i := 0; i < 60000; i++ {
		// Zipf-ish: small indices are frequent.
		k := rnd(len(vocab))
		k = k * k / len(vocab)
		if rnd(20) == 0 {
			sb.WriteString(strconv.Itoa(rnd(100000)))
		} else {
			sb.WriteString(vocab[k])
		}
		if rnd(12) == 0 {
			sb.WriteString(".\n")
		} else {
			sb.WriteByte(' ')
		}
	}
	text = sb.String()
}

type wc struct {
	w string
	n int
}

func work() int {
	counts := map[string]int{}
	nums := 0
	for _, f := range strings.Fields(text) {
		f = strings.TrimSuffix(f, ".")
		if v, err := strconv.Atoi(f); err == nil {
			nums += v % 97
			continue
		}
		counts[strings.ToLower(f)]++
	}
	list := make([]wc, 0, len(counts))
	for w, n := range counts {
		list = append(list, wc{w, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].w < list[j].w
	})
	var out strings.Builder
	for i, e := range list {
		if i == 200 {
			break
		}
		out.WriteString(fmt.Sprintf("%4d %-12s %6d %5.2f%%\n", i+1, e.w, e.n, 100*float64(e.n)/float64(len(text))))
	}
	return out.Len() + len(list) + nums
}

func main() { h.Run("textproc", 10, work) }
