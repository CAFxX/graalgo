package main

func main() {
	var i int = -5
	a := i + 3
	b := i - 3
	t := a + b
	var u uint = 7
	c := u + 3
	d := u - 10
	wsum := c + d
	var w uint64 = 0xFFFFFFFFFFFFFFFF
	du := w / 2
	mu := w % 3
	shu := w >> 1
	shu64 := w >> 64
	var v int64 = -8
	shs64 := v >> 64
	t = t
	wsum = wsum
	du = du
	mu = mu
	shu = shu
	shu64 = shu64
	shs64 = shs64
}

// observe: a b t c d wsum du mu shu shu64 shs64
