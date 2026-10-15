package main

func main() {
	a := complex(1.5, -2.5)
	b := complex(3.0, 4.0)
	sum := a + b
	dif := a - b
	prod := a * b
	quot := a / b
	neg := -a
	mixr := a * 2
	mixl := 1 + b
	mixf := a / 2.5
	c := complex(float32(0.5), float32(1.5))
	d := complex(float32(-1.25), float32(2.75))
	sum32 := c + d
	dif32 := c - d
	prod32 := c * d
	quot32 := c / d
	neg32 := -d
	mix32 := c * 3
	wide := complex128(c)
	inc := complex(10.0, 20.0)
	inc++
	inc--
	inc += 2
	inc -= 1
	inc32 := complex(float32(1), float32(2))
	inc32++
	inc32--
	sum = sum
	dif = dif
	prod = prod
	quot = quot
	neg = neg
	mixr = mixr
	mixl = mixl
	mixf = mixf
	sum32 = sum32
	dif32 = dif32
	prod32 = prod32
	quot32 = quot32
	neg32 = neg32
	mix32 = mix32
	wide = wide
	inc = inc
	inc32 = inc32
}

// observe: sum dif prod quot neg mixr mixl mixf sum32 dif32 prod32 quot32 neg32 mix32 wide inc inc32
