// regexscan: regexp over a generated log corpus (grep/log-processing class):
// MatchString, FindStringSubmatch and strconv on the captures.
package main

import (
	"regexp"
	"strconv"
	"strings"

	"graalgo/bench/h"
)

var (
	lines   []string
	reLevel = regexp.MustCompile(`level=(error|warn)`)
	reIP    = regexp.MustCompile(`ip=(\d+)\.(\d+)\.(\d+)\.(\d+)`)
	reLat   = regexp.MustCompile(`latency=(\d+)ms`)
	rePath  = regexp.MustCompile(`path=/api/v\d+/([a-z]+)/(\d+)`)
)

func init() {
	seed := uint32(11)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	levels := []string{"info", "info", "info", "debug", "warn", "error"}
	res := []string{"items", "users", "orders", "carts"}
	var sb strings.Builder
	for i := 0; i < 4000; i++ {
		sb.Reset()
		sb.WriteString("2026-11-20T12:")
		sb.WriteString(strconv.Itoa(10 + rnd(50)))
		sb.WriteString(" level=" + levels[rnd(len(levels))])
		sb.WriteString(` msg="request served" user=u` + strconv.Itoa(rnd(1000)))
		sb.WriteString(" ip=10." + strconv.Itoa(rnd(256)) + "." + strconv.Itoa(rnd(256)) + "." + strconv.Itoa(rnd(256)))
		sb.WriteString(" latency=" + strconv.Itoa(rnd(900)) + "ms")
		sb.WriteString(" path=/api/v" + strconv.Itoa(1+rnd(2)) + "/" + res[rnd(len(res))] + "/" + strconv.Itoa(rnd(100000)))
		lines = append(lines, sb.String())
	}
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func work() int {
	sum := 0
	for _, ln := range lines {
		if reLevel.MatchString(ln) {
			sum++
		}
		if m := reIP.FindStringSubmatch(ln); m != nil {
			sum += atoi(m[2]) + atoi(m[4])
		}
		if m := reLat.FindStringSubmatch(ln); m != nil {
			sum += atoi(m[1])
		}
		if m := rePath.FindStringSubmatch(ln); m != nil {
			sum += len(m[1]) + atoi(m[2])%7
		}
	}
	return sum
}

func main() { h.Run("regexscan", 10, work) }
