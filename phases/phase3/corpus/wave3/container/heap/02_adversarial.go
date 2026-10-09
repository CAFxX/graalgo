// container/heap 02: adversarial — duplicates, LCG pseudo-random bulk
// push/pop verified against a sorted reference, Init on sorted/reverse
// inputs, Remove(0)==Pop equivalence.
package main

import (
	"container/heap"
	"fmt"
	"sort"
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

func isSortedAsc(s []int) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func main() {
	// All duplicates: pop order must be non-decreasing.
	h := &IntHeap{s: []int{3, 3, 3, 3, 3}}
	heap.Init(h)
	var out []int
	for h.Len() > 0 {
		out = append(out, heap.Pop(h).(int))
	}
	fmt.Println(isSortedAsc(out), len(out)) // true 5

	// Bulk: 300 LCG values, pop-all must equal sort.Ints of the input.
	var vals []int
	x := uint32(12345)
	for i := 0; i < 300; i++ {
		x = x*1103515245 + 12345
		vals = append(vals, int(x>>16)%1000-500)
	}
	h2 := &IntHeap{}
	for _, v := range vals {
		heap.Push(h2, v)
	}
	var popped []int
	for h2.Len() > 0 {
		popped = append(popped, heap.Pop(h2).(int))
	}
	ref := append([]int(nil), vals...)
	sort.Ints(ref)
	match := len(popped) == len(ref)
	for i := range popped {
		if popped[i] != ref[i] {
			match = false
		}
	}
	fmt.Println(match) // true

	// Init on already-sorted and reverse-sorted inputs.
	sorted := &IntHeap{s: []int{1, 2, 3, 4, 5}}
	heap.Init(sorted)
	fmt.Println(heap.Pop(sorted)) // 1
	rev := &IntHeap{s: []int{5, 4, 3, 2, 1}}
	heap.Init(rev)
	fmt.Println(heap.Pop(rev)) // 1

	// Remove(0) behaves like Pop: same value out of identical heaps.
	a := &IntHeap{s: []int{9, 4, 7, 1, 5}}
	b := &IntHeap{s: []int{9, 4, 7, 1, 5}}
	heap.Init(a)
	heap.Init(b)
	va := heap.Remove(a, 0).(int)
	vb := heap.Pop(b).(int)
	fmt.Println(va == vb, va) // true 1

	// Interleaved push/pop: the multiset out must equal the multiset in.
	m := &IntHeap{}
	x = 999
	var pushed []int
	var seq []int
	for i := 0; i < 100; i++ {
		x = x*1103515245 + 12345
		v := int(x>>16) % 200
		pushed = append(pushed, v)
		heap.Push(m, v)
		if i%3 == 0 {
			seq = append(seq, heap.Pop(m).(int))
		}
	}
	for m.Len() > 0 {
		seq = append(seq, heap.Pop(m).(int))
	}
	sort.Ints(pushed)
	sort.Ints(seq)
	same := len(pushed) == len(seq)
	for i := range pushed {
		if pushed[i] != seq[i] {
			same = false
		}
	}
	fmt.Println(same, len(seq)) // true 100
}
