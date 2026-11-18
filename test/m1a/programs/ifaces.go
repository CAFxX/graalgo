package main

type Shape interface {
	Area() int
	Name() string
}

type Named interface{ Name() string }

type base struct{ name string }

func (b base) Name() string { return b.name }

type Sq struct {
	base
	side int
}

func (s Sq) Area() int { return s.side * s.side }

type Circ struct {
	*base
	r int
}

func (c *Circ) Area() int { return 3 * c.r * c.r }

type MyInt int

func (m MyInt) Area() int    { return int(m) }
func (m MyInt) Name() string { return "myint" }

type errT struct{ code int }

func (e errT) Error() string { return "err" }

func total(shapes []Shape) (n int) {
	for _, s := range shapes {
		n += s.Area()
	}
	return
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case MyInt, uint8:
		return "myint-or-byte"
	case Shape:
		return "shape:" + x.Name()
	case error:
		return "error:" + x.Error()
	case []int:
		return "ints"
	case func() int:
		return "func"
	default:
		return "other"
	}
}

func main() {
	shapes := []Shape{Sq{base{"sq"}, 3}, &Circ{&base{"circ"}, 2}, MyInt(5)}
	println(total(shapes), shapes[0].Name(), shapes[1].Name(), shapes[2].Name())

	var n Named = shapes[1]
	s2, ok := n.(Shape)
	_, ok2 := n.(error)
	println(ok, ok2, s2.Area())
	sq, ok3 := shapes[0].(Sq)
	_, ok4 := shapes[0].(*Circ)
	println(sq.side, ok3, ok4)

	println(describe(nil), describe(1), describe(MyInt(1)), describe(uint8(1)), describe(Sq{}),
		describe(errT{1}), describe([]int{}), describe(func() int { return 1 }), describe(1.5))

	f := shapes[0].Area
	g := Sq.Area
	h := (*Circ).Area
	i := Shape.Name
	c := &Circ{&base{"c2"}, 1}
	bound := c.Area
	c.r = 10
	println(f(), g(Sq{side: 4}), h(c), i(shapes[2]), bound())

	var e1, e2 error = errT{1}, errT{1}
	var e3 error = errT{2}
	var a1, a2 any = 1, int64(1)
	println(e1 == e2, e1 == e3, a1 == a2, a1 == 1, e1 != nil)

	defer func() {
		r := recover()
		err := r.(error)
		println(err.Error())
		var z Shape
		defer func() { println(recover().(error).Error()) }()
		z.Area()
	}()
	var bad any = "str"
	_ = bad.(Shape)
}
