package main

// complex64 multiplication: gc computes the product in float64 and rounds
// each part to float32 once; rounding every partial product separately
// differs in the last bit. Found by test/fuzz (BUG-0039).

func mul(a, b complex64) complex64 { return a * b }

func main() {
	a := complex64(complex(0.1, 0.2))
	b := complex64(complex(3, -4))
	println(mul(a, b))
	z := complex64(complex(0.6, 0.8))
	p := complex64(1)
	for i := 0; i < 12; i++ {
		p = mul(p, z)
		println(i, p)
	}
	x := complex64(complex(1.0000001, 3.3333333))
	y := complex64(complex(2.9999998, -0.7777777))
	println(mul(x, y), x*y*x)
}
