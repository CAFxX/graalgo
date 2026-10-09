// container/heap 01: Fix re-establishes order after a priority change;
// Remove extracts an arbitrary index. Uses a priority queue of items.
package main

import (
	"container/heap"
	"fmt"
)

type Item struct {
	value    string
	priority int
}

type PQ struct{ s []*Item }

func (pq *PQ) Len() int            { return len(pq.s) }
func (pq *PQ) Less(i, j int) bool  { return pq.s[i].priority < pq.s[j].priority }
func (pq *PQ) Swap(i, j int)        { pq.s[i], pq.s[j] = pq.s[j], pq.s[i] }
func (pq *PQ) Push(x any)          { pq.s = append(pq.s, x.(*Item)) }
func (pq *PQ) Pop() any {
	n := len(pq.s)
	x := pq.s[n-1]
	pq.s = pq.s[:n-1]
	return x
}

// indexOf finds the heap index of the item with the given value.
func indexOf(pq *PQ, value string) int {
	for i, it := range pq.s {
		if it.value == value {
			return i
		}
	}
	return -1
}

func main() {
	pq := &PQ{s: []*Item{
		&Item{value: "a", priority: 5},
		&Item{value: "b", priority: 1},
		&Item{value: "c", priority: 3},
		&Item{value: "d", priority: 4},
		&Item{value: "e", priority: 2},
	}}
	heap.Init(pq)

	// Lower "a" below everything via Fix: it must come out first.
	i := indexOf(pq, "a")
	pq.s[i].priority = 0
	heap.Fix(pq, i)
	fmt.Println(heap.Pop(pq).(*Item).value) // a

	// Raise "e" above everything via Fix: it must come out last.
	i = indexOf(pq, "e")
	pq.s[i].priority = 99
	heap.Fix(pq, i)
	var rest []string
	for pq.Len() > 0 {
		rest = append(rest, heap.Pop(pq).(*Item).value)
	}
	fmt.Println(rest) // [b c d e]

	// Remove: build again, remove "c" by index, check the rest.
	pq2 := &PQ{s: []*Item{
		&Item{value: "a", priority: 5},
		&Item{value: "b", priority: 1},
		&Item{value: "c", priority: 3},
		&Item{value: "d", priority: 4},
	}}
	heap.Init(pq2)
	removed := heap.Remove(pq2, indexOf(pq2, "c")).(*Item)
	fmt.Println(removed.value) // c
	rest = rest[:0]
	for pq2.Len() > 0 {
		rest = append(rest, heap.Pop(pq2).(*Item).value)
	}
	fmt.Println(rest) // [b d a]

	// Remove the last element == Pop path (n == i branch).
	pq3 := &PQ{s: []*Item{&Item{value: "x", priority: 1}, &Item{value: "y", priority: 2}}}
	heap.Init(pq3)
	fmt.Println(heap.Remove(pq3, 1).(*Item).value) // y
	fmt.Println(pq3.Len())                         // 1
}
