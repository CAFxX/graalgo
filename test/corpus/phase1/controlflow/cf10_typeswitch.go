package main

func main() {
	type myint int

	var i any = 42
	r1 := 0
	switch v := i.(type) {
	case int:
		r1 = v + 1
	case string:
		r1 = -1
	default:
		r1 = -99
	}
	// named type is distinct from int
	var j any = myint(7)
	r2 := 0
	switch j.(type) {
	case int:
		r2 = 1
	case myint:
		r2 = 2
	default:
		r2 = 3
	}
	// case nil matches nil interface
	var k any
	r3 := 0
	switch k.(type) {
	case nil:
		r3 = 10
	default:
		r3 = 20
	}
	// multi-type clause binds the interface type
	var s any = "hi"
	r4 := 0
	switch v := s.(type) {
	case int, string:
		_ = v
		r4 = 100
	default:
		r4 = 200
	}
	// no binding form, default hit
	var f any = 1.5
	r5 := 0
	switch f.(type) {
	case int:
		r5 = 1
	case string:
		r5 = 2
	default:
		r5 = 3
	}
	// typed nil must NOT match case nil: it matches its dynamic type's clause
	var tn any = (*int)(nil)
	r6 := 0
	switch v := tn.(type) {
	case nil:
		r6 = 1
	case *int:
		if v == nil {
			r6 = 2
		} else {
			r6 = 3
		}
	default:
		r6 = 4
	}
	_ = r1
	_ = r2
	_ = r3
	_ = r4
	_ = r5
	_ = r6
}

// observe: r1 r2 r3 r4 r5 r6
