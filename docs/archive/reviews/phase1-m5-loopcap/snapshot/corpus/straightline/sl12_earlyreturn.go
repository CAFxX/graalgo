package main

func main() {
	x := 1
	y := 2
	x = x + y
	{
		z := x * 10
		z = z
	}
	return
	x = 99
	y = 100
}

// observe: x y
