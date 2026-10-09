// container/heap 00: basic min-heap push/pop ordering over an IntHeap.
// Deterministic: fixed input, printed pop sequence.
//
// Note: the heap implementation uses a struct wrapper (not a bare
// slice) because the frontend forbids assignment to *ast.StarExpr in
// v1 scope (WS D), so `*h = append(*h, x)` is not expressible.
package main

import (
	"container/heap"
	"fmt"
)

type IntHeap struct{ s []int }

func (h *IntHeap) Len() int           { return len(h.s) }
func (h *IntHeap) Less(i, j int) bool { return h.s[i] < h.s[j] }
func (h *IntHeap) Swap(i, j int)      { h.s[i], h.s[j] = h.s[j], h.s[i] }
func (h *IntHeap) Push(x any)         { h.s = append(h.s, x.(int)) }
func (h *IntHeap) Pop() any {
	n := len(h.s)
	x := h.s[n-1]
	h.s = h.s[:n-1]
	return x
}

func drain(h *IntHeap) []int {
	var out []int
	for h.Len() > 0 {
		out = append(out, heap.Pop(h).(int))
	}
	return out
}

func main() {
	h := &IntHeap{s: []int{5, 3, 8, 1, 9, 2}}
	heap.Init(h)
	fmt.Println(drain(h)) // [1 2 3 5 8 9]

	// Push after drain; Len tracks.
	for _, v := range []int{4, 7, 0, 6} {
		heap.Push(h, v)
	}
	fmt.Println(h.Len())  // 4
	fmt.Println(drain(h)) // [0 4 6 7]
	fmt.Println(h.Len())  // 0

	// Init on empty and single-element heaps is a no-op.
	e := &IntHeap{}
	heap.Init(e)
	heap.Push(e, 42)
	fmt.Println(heap.Pop(e)) // 42
}
