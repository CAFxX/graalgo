// Interface data words and identity: interfaces holding ints, strings,
// floats, structs, arrays, pointers, funcs, maps and chans copied and
// compared (== on same/different dynamic types, uncomparable panics),
// interface-to-interface conversion keeping the dynamic value, and
// pointer-keyed maps through any.
package main

import "strconv"

type shape interface{ area() float64 }
type named interface {
	shape
	name() string
}

type sq struct{ s float64 }
type poly struct {
	pts [3][2]float64
	tag string
}

func (q sq) area() float64    { return q.s * q.s }
func (q sq) name() string     { return "sq" + strconv.FormatFloat(q.s, 'g', -1, 64) }
func (p *poly) area() float64 { return p.pts[1][0] * p.pts[2][1] / 2 }
func (p *poly) name() string  { return "poly:" + p.tag }

type counter int

func (c *counter) area() float64 { *c++; return float64(*c) }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func same(a, b any) bool { return a == b }

func main() {
	var x, y any = 1000, 1000
	println("int", x == y, same(int32(5), int64(5)), same(uint8(255), byte(255)))
	s1 := strconv.Itoa(77) + "x"
	s2 := "77" + string('x')
	println("string", same(s1, s2), same(s1, []byte(s2)[0]), same("", ""))
	println("float", same(0.0, -1*0.0), same(float32(1), 1.0), same(1.0, 1))

	q := sq{2}
	var sh shape = q
	q.s = 3
	println("value boxed", sh.area(), q.area(), sh == shape(sq{2}), sh != shape(q))
	var nm named = sq{5}
	sh = nm
	println("iface to iface", sh.(named).name(), sh == shape(nm), any(sh) == any(nm))

	p := &poly{tag: "t"}
	p.pts[1][0], p.pts[2][1] = 4, 3
	var ps shape = p
	var pn named = p
	p.pts[1][0] = 10
	println("ptr boxed sees writes", ps.area(), pn.name(), ps == shape(pn), ps == shape(&poly{tag: "t"}))
	pcopy := *ps.(*poly)
	pcopy.tag = "copy"
	println("deref copy", p.tag, pcopy.tag)

	var c counter
	var cs shape = &c
	cs.area()
	cs.area()
	cs2 := cs
	cs2.area()
	println("shared ptr state", int(c), cs == cs2)

	f := func() {}
	try("func compare", func() { println(same(f, f)) })
	try("map compare", func() { m := map[int]int{}; println(same(m, m)) })
	try("slice compare", func() { println(same([]int{}, nil)) })
	println("func vs nil differ in type", same(f, nil))
	ch := make(chan int)
	var cha, chb any = ch, ch
	println("chan compare", cha == chb, same(ch, make(chan int)))
	try("func vs int", func() { println("diff types no panic", same(f, 1)) })

	arr := [2]shape{sq{1}, p}
	arr2 := arr
	println("[2]iface eq", arr == arr2, arr == [2]shape{sq{1}, &poly{}})
	arr2[0] = sq{1}
	println("[2]iface eq after reassign", arr == arr2)
	try("[2]iface uncomparable", func() {
		a := [2]any{sq{1}, map[int]int{}}
		println(a == a)
	})

	byPtr := map[any]string{}
	byPtr[p] = "p"
	byPtr[&poly{tag: "t"}] = "other"
	byPtr[q] = "q"
	byPtr[sq{3}] = "q2"
	byPtr[ch] = "ch"
	println("ptr keys", len(byPtr), byPtr[p], byPtr[sq{3}], byPtr[ch])
	byShape := map[shape]int{}
	byShape[sq{1}] = 1
	byShape[shape(sq{1})] = 2
	byShape[p] = 3
	byShape[ps] = 4
	byShape[&c] = 5
	println("shape keys", len(byShape), byShape[sq{1}], byShape[p], byShape[cs])

	var nilp *poly
	var nilShape shape = nilp
	println("typed nil", nilShape != nil, nilShape == shape(nilp), same(nilp, nil))
	try("typed nil call", func() { _ = nilShape.area() })
	var vals [4]any
	vals[0] = &vals
	vals[1] = vals[0]
	vals[2] = [1]any{vals[0]}
	vals[3] = vals[2]
	println("self ref", vals[0] == vals[1], vals[2] == vals[3])
	words := []any{int8(-1), uint16(65535), "s", 2.5, complex(1, 1), true, sq{}, [0]int{}, struct{}{}}
	cnt := 0
	for i := range words {
		for j := range words {
			if words[i] == words[j] {
				cnt++
			}
		}
	}
	println("pairwise equal", cnt)
}
