package main

// Interfaces: boxing, comparison, type switches, assertions, nil
// handling, range assignment into interface variables, switch with an
// interface case against a concrete tag, multi-value conversions.

type Shape interface{ Area() int }

type Sq struct{ s int }

func (q Sq) Area() int { return q.s * q.s }

type Rect struct{ w, h int }

func (r *Rect) Area() int { return r.w * r.h }

func two() (int, string) { return 7, "seven" }

var G1, G2 any = two()

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int, int8:
		return "integer"
	case string:
		return "string " + x
	case Shape:
		return "shape"
	case error:
		return "error " + x.Error()
	default:
		return "other"
	}
}

type E struct{ msg string }

func (e *E) Error() string { return e.msg }

func mayFail(f bool) error {
	var e *E
	if f {
		e = &E{"failed"}
	}
	return e // typed nil when !f
}

func main() {
	shapes := []Shape{Sq{2}, &Rect{2, 3}}
	t := 0
	for _, s := range shapes {
		t += s.Area()
	}
	println(t)
	var a, b any = 1, 1
	println(a == b, a == any(int8(1)), a != nil)
	println(describe(nil), describe(3), describe(int8(3)), describe("x"), describe(Sq{1}), describe(&E{"e"}), describe(2.5))
	err := mayFail(false)
	println(err == nil, err != nil)
	if s, ok := shapes[0].(Sq); ok {
		println("sq", s.s)
	}
	_, ok := shapes[1].(Sq)
	println(ok)
	var k, v any
	m := map[string]int{"only": 1}
	for k, v = range m {
	}
	println(k.(string), v.(int))
	var sh Shape
	for _, sh = range []*Rect{{1, 1}, {4, 5}} {
	}
	println(sh.Area())
	x := 3
	var target any = 3
	switch x {
	case target:
		println("matched interface case")
	default:
		println("no match")
	}
	var p *int
	switch p {
	case nil:
		println("nil pointer case")
	}
	var c, d any = two()
	println(c.(int), d.(string), G1.(int), G2.(string))
	m2 := map[any]int{1: 1, "1": 2, Sq{1}: 3}
	println(m2[1], m2["1"], m2[Sq{1}], len(m2))
	ch := make(chan any, 2)
	ch <- 5
	ch <- "s"
	close(ch)
	for e := range ch {
		println(describe(e))
	}
}
