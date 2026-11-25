package main

func main() {
	type inner struct {
		a int
		b int
	}
	type outer struct {
		inner
		c int
	}

	// embedded field promotion: o.a resolves through inner
	o := outer{inner{1, 2}, 3}
	a := o.a + o.b + o.c
	// promoted field assignment
	o.a = 10
	b := o.a + o.inner.a
	// explicit path also works
	o.inner.b = 20
	c := o.b
	// zero outer has zero inner
	var z outer
	d := z.a + z.b + z.c
	_ = a
	_ = b
	_ = c
	_ = d
}

// observe: a b c d
