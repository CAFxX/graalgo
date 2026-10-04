package main

// Complex arithmetic: complex/real/imag builtins and operations.
func main() {
	c := complex(1.5, 2.5)
	println("real:", real(c), "imag:", imag(c))

	c2 := complex(1, 2) + complex(3, 4)
	println("sum real:", real(c2), "imag:", imag(c2))

	c3 := complex(1, 2) * complex(3, 4)
	println("prod real:", real(c3), "imag:", imag(c3))

	// Division (gc lowers via complex division).
	c4 := complex(1, 2) / complex(3, 4)
	println("quot real:", real(c4), "imag:", imag(c4))
}
