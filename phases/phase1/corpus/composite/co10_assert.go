package main

func main() {
	var i any = 42
	// single-valued assertion, correct type
	v := i.(int)
	a := v + 1
	// assertion to string
	var s any = "xy"
	w := s.(string)
	b := int(w[0]) + int(w[1])
	// comma-ok assertion, present
	x, ok := i.(int)
	c := x
	d := 0
	if ok {
		d = 1
	}
	// comma-ok assertion, wrong type
	y, ok2 := i.(string)
	_ = y
	e := 0
	if !ok2 {
		e = 1
	}
	// assertion on named type
	type myint int
	var n any = myint(9)
	z := n.(myint)
	f := int(z) + 1
	_ = a
	_ = b
	_ = c
	_ = d
	_ = e
	_ = f
}

// observe: a b c d e f
