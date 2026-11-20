// container/heap and container/list holding structs with arrays and
// complex fields: Element.Value is an interface copy, heap.Fix after
// in-place mutation through a pointer, sort.Slice / slices.SortStableFunc
// swapping large struct elements, and stable order of equal keys.
package main

import (
	"container/heap"
	"container/list"
	"slices"
	"sort"
	"strconv"
)

type job struct {
	prio  float32
	data  [5]int
	z     complex64
	label string
	idx   int
}

type pq []*job

func (q pq) Len() int           { return len(q) }
func (q pq) Less(i, j int) bool { return q[i].prio < q[j].prio }
func (q pq) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].idx = i
	q[j].idx = j
}
func (q *pq) Push(x any) {
	j := x.(*job)
	j.idx = len(*q)
	*q = append(*q, j)
}
func (q *pq) Pop() any {
	old := *q
	n := len(old)
	j := old[n-1]
	old[n-1] = nil
	*q = old[:n-1]
	return j
}

type vheap [][3]int

func (h vheap) Len() int           { return len(h) }
func (h vheap) Less(i, j int) bool { return h[i][0]*h[i][1] < h[j][0]*h[j][1] }
func (h vheap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *vheap) Push(x any)        { *h = append(*h, x.([3]int)) }
func (h *vheap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func main() {
	q := &pq{}
	heap.Init(q)
	all := []*job{}
	for i := 0; i < 8; i++ {
		j := &job{prio: float32((i * 37) % 11), label: "j" + strconv.Itoa(i), z: complex(float32(i), 1)}
		j.data[i%5] = i
		heap.Push(q, j)
		all = append(all, j)
	}
	all[3].prio = -1
	heap.Fix(q, all[3].idx)
	all[5].data[0] = 99
	for q.Len() > 0 {
		j := heap.Pop(q).(*job)
		print(j.label, ":", j.prio, ":", j.data[0], " ")
	}
	println()

	h := &vheap{{3, 3, 0}, {1, 2, 1}, {2, 2, 2}, {5, 1, 3}}
	heap.Init(h)
	heap.Push(h, [3]int{0, 9, 4})
	top := (*h)[0]
	top[2] = -1
	println("heap top copy", (*h)[0][2], top[2])
	for h.Len() > 0 {
		v := heap.Pop(h).([3]int)
		print(v[2], " ")
	}
	println()

	l := list.New()
	arr := [3]int{1, 2, 3}
	e := l.PushBack(arr)
	arr[0] = 100
	l.PushFront(job{label: "front", data: [5]int{7}})
	l.PushBack(&arr)
	arr[1] = 200
	v := e.Value.([3]int)
	v[2] = 300
	println("list value copy", e.Value.([3]int)[0], e.Value.([3]int)[2], l.Back().Value.(*[3]int)[1])
	e.Value = [3]int{4, 5, 6}
	l.MoveToFront(e)
	fj := l.Front().Next().Value.(job)
	println("list order", l.Front().Value.([3]int)[0], fj.label, fj.data[0], l.Len())
	other := list.New()
	other.PushBack(1.5)
	l.PushBackList(other)
	l.PushFrontList(l)
	n := 0
	for x := l.Front(); x != nil; x = x.Next() {
		if _, ok := x.Value.(float64); ok {
			n++
		}
	}
	println("list lengths", l.Len(), n)
	println("remove foreign", l.Remove(other.Front()) == 1.5, l.Len(), other.Len())

	jobs := make([]job, 12)
	for i := range jobs {
		jobs[i] = job{prio: float32(i % 3), label: "s" + strconv.Itoa(i)}
		jobs[i].data[4] = i
		jobs[i].z = complex(0, float32(-i))
	}
	slices.SortStableFunc(jobs, func(a, b job) int {
		if a.prio < b.prio {
			return 1
		}
		if a.prio > b.prio {
			return -1
		}
		return 0
	})
	for _, j := range jobs {
		print(j.label, "/", j.data[4], " ")
	}
	println()
	sort.Slice(jobs, func(i, k int) bool { return imag(jobs[i].z) > imag(jobs[k].z) })
	println("sort.Slice", jobs[0].label, jobs[11].label, jobs[5].data[4], real(jobs[3].z))
	sort.SliceStable(jobs, func(i, k int) bool { return jobs[i].data[4]%4 < jobs[k].data[4]%4 })
	for _, j := range jobs {
		print(j.data[4], " ")
	}
	println()
	idx, found := slices.BinarySearchFunc([]job{{prio: 1}, {prio: 3}, {prio: 5}}, float32(3), func(j job, t float32) int {
		if j.prio < t {
			return -1
		}
		if j.prio > t {
			return 1
		}
		return 0
	})
	println("bsearch", idx, found)
}
