// Targets interface type assertions (comma-ok and panicking, with exact
// "interface conversion" messages), nil interface vs typed nil pointer,
// type switches with multi-type cases and nil case, and assertions to
// interface types with missing methods.
package main

import "strconv"

type Shape interface {
	Area() int
}

type Named interface {
	Name() string
}

type NamedShape interface {
	Shape
	Named
}

type Sq struct{ S int }

func (s Sq) Area() int    { return s.S * s.S }
func (s Sq) Name() string { return "sq" + strconv.Itoa(s.S) }

type Rect struct{ W, H int }

func (r *Rect) Area() int { return r.W * r.H }

type MyInt int

func (m MyInt) Area() int { return int(m) }

type MyErr struct{ msg string }

func (e *MyErr) Error() string { return "myerr:" + e.msg }

func catch(tag string, f func()) {
	defer func() {
		r := recover()
		if r == nil {
			println(tag, "no panic")
			return
		}
		if e, ok := r.(interface{ Error() string }); ok {
			println(tag, e.Error())
		} else if s, ok := r.(string); ok {
			println(tag, "string:", s)
		} else {
			println(tag, "other")
		}
	}()
	f()
}

//go:noinline
func mayFail(fail bool) error {
	var e *MyErr
	if fail {
		e = &MyErr{"x"}
	}
	return e // typed nil when !fail
}

//go:noinline
func realNil(fail bool) error {
	if fail {
		return &MyErr{"y"}
	}
	return nil
}

func classify(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int, int8, int16:
		_ = x
		return "small-int"
	case *Rect:
		if x == nil {
			return "nil-rect"
		}
		return "rect" + strconv.Itoa(x.Area())
	case NamedShape:
		return "namedshape:" + x.Name()
	case Shape:
		return "shape" + strconv.Itoa(x.Area())
	case error:
		return "error:" + x.Error()
	case []int, map[string]int:
		return "container"
	case func() int:
		return "func" + strconv.Itoa(x())
	case struct{ A int }:
		return "anon" + strconv.Itoa(x.A)
	case [2]int:
		return "arr" + strconv.Itoa(x[0]+x[1])
	default:
		return "default"
	}
}

func main() {
	var s Shape = Sq{3}
	_, ok := s.(Named)
	println(ok)
	n := s.(Named)
	println(n.Name())
	_, ok = s.(*Rect)
	println(ok)
	s = &Rect{2, 5}
	_, ok = s.(Named)
	println(ok, s.Area())

	catch("concrete", func() { _ = s.(Sq) })
	catch("iface-missing", func() { _ = s.(Named) })
	catch("nil-iface", func() {
		var e Shape
		_ = e.(Sq)
	})
	catch("nil-to-iface", func() {
		var e any
		_ = e.(Shape)
	})
	catch("any-int", func() {
		var a any = 42
		_ = a.(string)
	})
	catch("named-int", func() {
		var a any = MyInt(4)
		_ = a.(int)
	})
	catch("ptr-vs-val", func() {
		var a any = Sq{1}
		_ = a.(*Sq)
	})
	catch("rect-val", func() {
		var a any = Rect{1, 1}
		_ = a.(Shape)
	})
	catch("ok-path", func() {
		var a any = MyInt(9)
		println(a.(Shape).Area())
	})
	catch("anon-struct", func() {
		var a any = struct{ A int }{1}
		_ = a.(struct{ B int })
	})
	catch("slice", func() {
		var a any = []string{"x"}
		_ = a.([]int)
	})
	catch("error-assert", func() {
		var a any = errorsNew("boom")
		_ = a.(*MyErr)
	})
	catch("call-nil-iface", func() {
		var sh Shape
		println(sh.Area())
	})
	catch("call-typed-nil", func() {
		var r *Rect
		var sh Shape = r
		println(sh.Area())
	})

	// typed nil vs nil
	e1 := mayFail(false)
	e2 := realNil(false)
	println(e1 == nil, e2 == nil, e1 != nil)
	var rp *Rect
	var sh Shape = rp
	println(sh == nil, sh != nil, classify(sh), classify(nil), classify(rp))
	println(mayFail(true) == nil, mayFail(true).Error())
	var me *MyErr
	println(e1 == error(me), any(rp) == any(me))

	vals := []any{
		nil, 1, int8(2), int16(3), int32(4), &Rect{3, 4}, Sq{5}, MyInt(6),
		&MyErr{"z"}, []int{}, map[string]int{}, func() int { return 7 },
		struct{ A int }{8}, [2]int{4, 5}, "str", 3.5, sh,
	}
	for i, v := range vals {
		println(i, classify(v))
	}

	// comma-ok with interface targets and zero-values
	var a any = Sq{4}
	if ns, ok := a.(NamedShape); ok {
		println("ns", ns.Area(), ns.Name())
	}
	r, ok := a.(*Rect)
	println(r == nil, ok)
	sq, ok := a.(Sq)
	println(sq.S, ok)
	mi, ok := a.(MyInt)
	println(mi, ok)
	str, ok := a.(string)
	println(len(str), ok)
	er, ok := a.(error)
	println(er == nil, ok)
}

type strErr string

func (s strErr) Error() string { return string(s) }

func errorsNew(s string) error { return strErr(s) }
