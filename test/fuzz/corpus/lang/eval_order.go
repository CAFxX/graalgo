// Targets evaluation order: tuple assignments with index expressions that
// depend on assigned variables (a[i], i = ...), op-assignment with
// side-effecting index calls (evaluated once), short-circuit operators,
// call argument order, and composite literal element order.
package main

import "strconv"

var trace string

func t(tag string, v int) int {
	trace += tag
	return v
}

func tb(tag string, v bool) bool {
	trace += tag
	return v
}

type S struct {
	A, B int
	Arr  [4]int
}

var gs S
var calls int

func ptr() *S {
	calls++
	return &gs
}

func idx() int {
	calls++
	return calls % 4
}

func reset() { trace = "" }

func dump(a []int) string {
	s := ""
	for _, v := range a {
		s += strconv.Itoa(v) + ","
	}
	return s
}

func three() (int, int, int) {
	trace += "3"
	return 7, 8, 9
}

func main() {
	a := []int{0, 0, 0, 0, 0}
	i := 1
	a[i], i = 10, 3
	println(dump(a), i)
	i, a[i] = 0, 20
	println(dump(a), i)
	i = 2
	a[i], a[i+1], i = i, i*10, i+1
	println(dump(a), i)

	// swap with index aliasing
	b := []int{1, 2, 3}
	j := 0
	j, b[j] = b[j+1], b[j+2]
	println(dump(b), j)
	b[0], b[1], b[2] = b[2], b[0], b[1]
	println(dump(b))

	// op-assignment: index call evaluated once
	calls = 0
	arr := []int{0, 0, 0, 0}
	arr[idx()] += 5
	arr[idx()] *= 3
	arr[idx()]++
	println(dump(arr), calls)
	calls = 0
	ptr().A += 2
	ptr().Arr[idx()] -= 4
	ptr().B++
	println(gs.A, gs.B, gs.Arr[0], gs.Arr[1], gs.Arr[2], gs.Arr[3], calls)

	// operands of binary ops and call args left to right
	reset()
	r := t("a", 1) + t("b", 2)*t("c", 3) - t("d", 4)
	println(r, trace)
	reset()
	println(add3(t("x", 1), t("y", 2), t("z", 3)), trace)
	reset()
	m := map[int]int{t("k1", 1): t("v1", 10), t("k2", 2): t("v2", 20)}
	println(len(m), trace)
	reset()
	sl := []int{t("0", 0), 5: t("5", 5), t("6", 6), 2: t("2", 2)}
	println(len(sl), sl[5], sl[6], sl[2], trace)
	reset()
	st := S{B: t("B", 1), A: t("A", 2)}
	println(st.A, st.B, trace)

	// short-circuit
	reset()
	if tb("1", false) && tb("2", true) || tb("3", true) && tb("4", false) || tb("5", true) {
		trace += "!"
	}
	println(trace)
	reset()
	var np *S
	if np != nil && np.A > 0 {
		trace += "bad"
	}
	ok := np == nil || np.A > 0
	println(ok, trace)

	// multi-value assignment from function and swaps
	reset()
	var x, y, z int
	x, y, z = three()
	x, y, z = z, x, y
	println(x, y, z, trace)

	// map index assignment with self-reference
	mm := map[string]int{"a": 1}
	mm["a"], mm["b"] = mm["b"]+5, mm["a"]+7
	println(mm["a"], mm["b"])

	// assignment through pointer then index using updated value
	p := &a
	*p, a[0] = []int{9, 9}, 77 // a[0] uses old slice
	println(dump(a), dump(*p))

	// increment within index expression evaluated before assignment
	k := 0
	vals := []int{0, 0, 0}
	next := func() int { k++; return k }
	vals[next()] = next() * 10
	println(dump(vals), k)

	// defer argument evaluation time
	reset()
	func() {
		v := 1
		defer println("deferred v =", v, t("D", 0))
		v = 2
		trace += "body"
	}()
	println(trace)

	// string concatenation order with side effects
	reset()
	s := strconv.Itoa(t("L", 1)) + strconv.Itoa(t("M", 2)) + strconv.Itoa(t("R", 3))
	println(s, trace)

	// array value assignment: RHS evaluated fully before assignment
	av := [3]int{1, 2, 3}
	av[0], av[1], av[2] = av[1]+av[2], av[0]+av[2], av[0]+av[1]
	println(av[0], av[1], av[2])
	sv := S{A: 1, B: 2}
	sv.A, sv.B = sv.B, sv.A+sv.B
	println(sv.A, sv.B)
}

func add3(a, b, c int) int { return a*100 + b*10 + c }
