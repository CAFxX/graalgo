package main

import (
	"container/heap"
	"sort"
)

type byLen []string

func (s byLen) Len() int           { return len(s) }
func (s byLen) Less(i, j int) bool { return len(s[i]) < len(s[j]) }
func (s byLen) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

type item struct {
	name string
	prio int
	idx  int
}
type pq []*item

func (q pq) Len() int           { return len(q) }
func (q pq) Less(i, j int) bool { return q[i].prio > q[j].prio }
func (q pq) Swap(i, j int)      { q[i], q[j] = q[j], q[i]; q[i].idx = i; q[j].idx = j }
func (q *pq) Push(x any)        { it := x.(*item); it.idx = len(*q); *q = append(*q, it) }
func (q *pq) Pop() any          { old := *q; n := len(old); it := old[n-1]; *q = old[:n-1]; return it }

func main() {
	ints := []int{5, 2, 8, 1, 9, 3, 7, 3, 0, -4, 6}
	sort.Ints(ints)
	for _, v := range ints {
		print(v, " ")
	}
	println(sort.IntsAreSorted(ints), sort.SearchInts(ints, 7), sort.SearchInts(ints, 4))
	strs := []string{"pear", "apple", "fig", "banana", "kiwi", "plum"}
	sort.Strings(strs)
	for _, v := range strs {
		print(v, " ")
	}
	println(sort.SearchStrings(strs, "kiwi"), sort.StringsAreSorted(strs))
	sort.Stable(byLen(strs))
	for _, v := range strs {
		print(v, " ")
	}
	println(sort.IsSorted(byLen(strs)))
	sort.Sort(sort.Reverse(sort.StringSlice(strs)))
	for _, v := range strs {
		print(v, " ")
	}
	println()
	fl := sort.Float64Slice{2.5, -1, 3.25, 0}
	fl.Sort()
	println(fl[0], fl[3], sort.SearchFloat64s(fl, 2.5), sort.Search(100, func(i int) bool { return i*i >= 50 }))
	i, found := sort.Find(len(strs), func(i int) int { return len("fig") - len(strs[i]) })
	println(i, found)
	big := make([]int, 2000)
	x := uint32(7)
	for i := range big {
		x = x*1664525 + 1013904223
		big[i] = int(x >> 12)
	}
	sort.Sort(sort.IntSlice(big))
	println(sort.IntsAreSorted(big), big[0], big[1999])

	var queue pq
	q := &queue
	heap.Init(q)
	for i, n := range []string{"low", "high", "mid", "urgent", "idle"} {
		heap.Push(q, &item{name: n, prio: []int{2, 8, 5, 10, 0}[i]})
	}
	it := (*q)[3]
	it.prio = 9
	heap.Fix(q, it.idx)
	heap.Remove(q, (*q)[q.Len()-1].idx)
	for q.Len() > 0 {
		it := heap.Pop(q).(*item)
		print(it.name, ":", it.prio, " ")
	}
	println()
}
