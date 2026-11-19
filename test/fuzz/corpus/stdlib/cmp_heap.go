// Targets cmp.Compare/Less/Or with NaN, -0, Inf and strings, plus
// container/heap: Init on unordered data, Push/Pop order, Fix after
// priority updates, Remove from the middle, a max-heap over structs with
// index tracking, and heap-sort checksums over 5000 elements.
package main

import (
	"cmp"
	"container/heap"
	"math"
)

type item struct {
	val   string
	prio  int
	seq   int
	index int
}

type pq []*item

func (q pq) Len() int { return len(q) }
func (q pq) Less(i, j int) bool {
	if q[i].prio != q[j].prio {
		return q[i].prio > q[j].prio
	}
	return q[i].seq < q[j].seq
}
func (q pq) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}
func (q *pq) Push(x any) {
	it := x.(*item)
	it.index = len(*q)
	*q = append(*q, it)
}
func (q *pq) Pop() any {
	old := *q
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	it.index = -1
	*q = old[:n-1]
	return it
}

type ih []int64

func (h ih) Len() int           { return len(h) }
func (h ih) Less(i, j int) bool { return h[i] < h[j] }
func (h ih) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *ih) Push(x any)        { *h = append(*h, x.(int64)) }
func (h *ih) Pop() any {
	o := *h
	v := o[len(o)-1]
	*h = o[:len(o)-1]
	return v
}

type fh []float64

func (h fh) Len() int           { return len(h) }
func (h fh) Less(i, j int) bool { return cmp.Less(h[i], h[j]) }
func (h fh) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fh) Push(x any)        { *h = append(*h, x.(float64)) }
func (h *fh) Pop() any {
	o := *h
	v := o[len(o)-1]
	*h = o[:len(o)-1]
	return v
}

func main() {
	nan := math.NaN()
	negz := math.Copysign(0, -1)
	inf := math.Inf(1)
	vals := []float64{nan, -inf, -1, negz, 0, 1, inf}
	for _, a := range vals {
		for _, b := range vals {
			print(cmp.Compare(a, b), cmp.Less(a, b), " ")
		}
		println()
	}
	println(cmp.Compare("a", "b"), cmp.Compare("b", "a"), cmp.Compare("", ""), cmp.Less("Z", "a"), cmp.Compare("\xff", "\u00ff"))
	println(cmp.Compare(int8(-128), int8(127)), cmp.Compare(uint64(1<<63), uint64(1)), cmp.Compare(uintptr(0), uintptr(0)))
	println(cmp.Or(0, 0, 3, 4), cmp.Or("", "x", "y"), cmp.Or[int](), cmp.Or(0.0, negz, 2.0) == 2.0)
	println(math.Float64bits(cmp.Or(negz, 5)))

	q := pq{}
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
	items := map[string]*item{}
	for i, w := range words {
		it := &item{val: w, prio: (i * 7) % 5, seq: i}
		items[w] = it
		q = append(q, it)
		it.index = i
	}
	heap.Init(&q)
	heap.Push(&q, &item{val: "kilo", prio: 3, seq: 10})
	items["delta"].prio = 100
	heap.Fix(&q, items["delta"].index)
	items["alpha"].prio = -5
	heap.Fix(&q, items["alpha"].index)
	rem := heap.Remove(&q, items["golf"].index).(*item)
	println("removed", rem.val, rem.index)
	for q.Len() > 0 {
		it := heap.Pop(&q).(*item)
		print(it.val, ":", it.prio, " ")
	}
	println()

	var h ih
	var s uint64 = 12345
	for i := 0; i < 5000; i++ {
		s = s*6364136223846793005 + 1
		heap.Push(&h, int64(s>>1)%100000-50000)
		if i%7 == 6 {
			heap.Remove(&h, int(s%uint64(h.Len())))
		}
		if i%11 == 10 {
			j := int(s>>8) % h.Len()
			h[j] = -h[j]
			heap.Fix(&h, j)
		}
	}
	var hs uint64 = 14695981039346656037
	prev := int64(math.MinInt64)
	ok := true
	n := 0
	for h.Len() > 0 {
		v := heap.Pop(&h).(int64)
		if v < prev {
			ok = false
		}
		prev = v
		hs = (hs ^ uint64(v)) * 1099511628211
		n++
	}
	println("heapsort", n, ok, hs)

	f := fh{3, nan, -1, negz, 0, nan, inf, -inf, 2}
	heap.Init(&f)
	for f.Len() > 0 {
		v := heap.Pop(&f).(float64)
		print(math.Float64bits(v), " ")
	}
	println()
	// Remove last and Fix on a single element
	one := ih{5}
	heap.Fix(&one, 0)
	println(heap.Remove(&one, 0).(int64), one.Len())
	two := ih{}
	heap.Init(&two)
	heap.Push(&two, int64(2))
	heap.Push(&two, int64(1))
	println(heap.Remove(&two, 1).(int64), heap.Pop(&two).(int64))
}
