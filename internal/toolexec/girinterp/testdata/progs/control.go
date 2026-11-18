package main

// Shadowing, labeled break/continue, goto, switch fallthrough.

var x = 1

func shadow() {
	x := 2
	{
		x := 3
		x++
		println("inner", x)
	}
	if x := 10; x > 5 {
		println("if", x)
	} else {
		println("else", x)
	}
	println("outer", x)
}

func labels() {
	n := 0
outer:
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			if j == 3 {
				continue outer
			}
			if i == 3 {
				break outer
			}
			n += i*10 + j
		}
	}
	println("labels", n)
	i := 0
loop:
	if i < 4 {
		i++
		goto loop
	}
	println("goto", i)
	k := 0
	for {
		switch {
		case k > 3:
			goto done
		}
		k++
	}
done:
	println("done", k)
sw:
	switch k {
	case 4:
		for {
			break sw
		}
	}
}

func fall(v int) {
	switch v {
	case 0:
		println("zero")
		fallthrough
	case 1:
		println("one")
		fallthrough
	case 2:
		println("two")
	case 3:
		println("three")
		fallthrough
	default:
		println("default")
	}
}

func main() {
	shadow()
	labels()
	for i := 0; i < 5; i++ {
		fall(i)
	}
	println("global", x)
	switch y := x * 2; {
	case y > 1:
		println("tagless", y)
	}
}
