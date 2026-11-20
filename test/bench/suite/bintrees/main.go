// Binary trees (benchmarks game): allocation-heavy, pointer chasing.
package main

import "graalgo/bench/h"

type node struct{ l, r *node }

func make_(d int) *node {
	if d == 0 {
		return &node{}
	}
	return &node{make_(d - 1), make_(d - 1)}
}

func (n *node) check() int {
	if n.l == nil {
		return 1
	}
	return 1 + n.l.check() + n.r.check()
}

func work() int {
	const maxDepth = 14
	long := make_(maxDepth)
	t := 0
	for d := 4; d <= maxDepth; d += 2 {
		iters := 1 << (maxDepth - d + 4)
		for i := 0; i < iters; i++ {
			t += make_(d).check()
		}
	}
	return t + long.check()
}

func main() { h.Run("bintrees", 15, work) }
