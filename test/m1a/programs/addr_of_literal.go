// &T{...} for every composite type: the address of a new variable holding
// the value — also when T is a slice or map type (container/heap's usual
// pq := &PriorityQueue{}; toolexec-design §13, finding 2).
package main

type point struct{ x, y int }

type queue []int

func (q *queue) push(v int) { *q = append(*q, v) }
func (q queue) len() int    { return len(q) }

type set map[string]bool

func (s *set) add(k string) {
	if *s == nil {
		*s = set{}
	}
	(*s)[k] = true
}

type grid [2][2]int

func main() {
	q := &queue{}
	q.push(3)
	q.push(4)
	println(q.len(), (*q)[0], (*q)[1], len(*q))
	q2 := &queue{1, 2, 3}
	q3 := q2
	q3.push(9)
	println(len(*q2), (*q2)[3], q2 == q3)
	ps := &[]point{{1, 2}, {3, 4}}
	(*ps)[1].y = 40
	*ps = append(*ps, point{5, 6})
	println(len(*ps), (*ps)[1].y, (*ps)[2].x)
	empty := &[]string{}
	println(len(*empty), *empty == nil, empty != nil)

	s := &set{"a": true}
	s.add("b")
	println(len(*s), (*s)["a"], (*s)["b"], (*s)["c"])
	m := &map[int]point{1: {1, 1}}
	(*m)[2] = point{2, 2}
	println(len(*m), (*m)[2].x)

	p := &point{1, 2}
	p.x = 10
	g := &grid{{1, 2}, {3, 4}}
	g[1][0] = 30
	a := &[3]byte{1, 2, 3}
	a[2] = 9
	println(p.x, p.y, g[1][0], g[0][1], a[2], len(a))
	pp := &[]*point{{7, 8}, p}
	println((*pp)[0].y, (*pp)[1] == p)
	var i any = &queue{5}
	println(len(*i.(*queue)), (*i.(*queue))[0])
}
