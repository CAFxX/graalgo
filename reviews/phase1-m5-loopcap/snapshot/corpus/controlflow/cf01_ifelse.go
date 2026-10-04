package main

func main() {
	a := 0
	if x := 15; x > 10 {
		a = 1
	} else if x > 5 {
		a = 2
	} else {
		a = 3
	}
	b := 0
	if a == 1 {
		b = 100
	}
	c := 0
	if false {
		c = 1
	} else {
		c = 2
	}
	d := 0
	if x := -3; x < 0 {
		d = x * -1
	}
	_ = a
	_ = b
	_ = c
	_ = d
}

// observe: a b c d
