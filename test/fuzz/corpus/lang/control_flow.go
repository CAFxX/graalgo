// Targets control flow: labeled break/continue out of nested for/switch,
// goto forward/backward and goto-built loops, tagless switch, fallthrough
// chains, switch case evaluation order with side effects, and for loops with
// complex post statements.
package main

import "strconv"

var log string

func ev(tag string, v int) int {
	log += tag
	return v
}

func cond(tag string, b bool) bool {
	log += tag
	return b
}

//go:noinline
func sw(x int) string {
	log = ""
	switch ev("s", x) {
	case ev("a", 1), ev("b", 2):
		return "12:" + log
	case ev("c", 3):
		return "3:" + log
	case ev("d", 4), ev("e", 5), ev("f", 6):
		return "456:" + log
	}
	return "none:" + log
}

//go:noinline
func ft(x int) string {
	out := ""
	switch {
	case x > 10:
		out += "big,"
		fallthrough
	case x > 5:
		out += "mid,"
		fallthrough
	case x < 0:
		out += "neg?,"
	case x == 3:
		out += "three,"
		fallthrough
	default:
		out += "default,"
	case x == 4:
		out += "four,"
	}
	return out
}

func labeled() string {
	out := ""
outer:
	for i := 0; i < 5; i++ {
	inner:
		for j := 0; j < 5; j++ {
			switch {
			case j == i:
				continue outer
			case i+j == 6:
				break outer
			case j == 3:
				break inner
			case (i*j)%2 == 1:
				continue
			}
			out += strconv.Itoa(i) + strconv.Itoa(j) + " "
		}
		out += "| "
	}
	return out
}

func gotoLoop(n int) (sum int) {
	i := 0
top:
	if i >= n {
		goto done
	}
	if i%3 == 0 {
		i++
		goto top
	}
	sum += i
	i++
	goto top
done:
	return sum
}

func gotoForward(x int) string {
	s := ""
	if x > 0 {
		goto pos
	}
	s += "nonpos,"
	goto end
pos:
	s += "pos,"
	if x > 100 {
		goto end
	}
	s += "small,"
end:
	return s + "end"
}

func breakSwitchInFor() int {
	n := 0
	for i := 0; i < 10; i++ {
		switch i {
		case 3:
			break // only breaks the switch
		case 7:
			n += 100
		default:
			n++
		}
	}
	return n
}

func main() {
	for _, x := range []int{1, 2, 3, 5, 6, 9} {
		println(x, sw(x))
	}
	for _, x := range []int{20, 7, -2, 3, 4, 0} {
		println(x, ft(x))
	}
	println(labeled())
	println(gotoLoop(10), gotoLoop(0), gotoLoop(31))
	println(gotoForward(5), gotoForward(-1), gotoForward(500))
	println(breakSwitchInFor())

	// tagless switch short-circuit order
	log = ""
	switch {
	case cond("a", false) && cond("b", true):
	case cond("c", true) || cond("d", true):
		log += "!"
	case cond("e", true):
	}
	println(log)

	// complex post statements and multiple vars
	out := ""
	for i, j := 0, 10; i < j; i, j = i+2, j-1 {
		out += strconv.Itoa(i) + "-" + strconv.Itoa(j) + " "
	}
	println(out)
	n := 0
	for a, b := 1, 1; b < 200; a, b = b, a+b {
		n++
	}
	println(n)
	k := 0
	for k = 1; k < 1000; k <<= 1 {
		if k == 64 {
			break
		}
	}
	println(k)

	// switch with init stmt and shadowing
	x := 100
	switch x := 5; {
	case x > 3:
		println("inner x", x)
	}
	println("outer x", x)

	// switch on strings, duplicate-looking values with different types
	var ifc any = int64(1)
	switch ifc {
	case 1:
		println("int 1")
	case int64(1):
		println("int64 1")
	}
	switch v := ifc.(type) {
	case int, int32:
		println("int-ish")
	case int64:
		println("int64", v+1)
	}

	// labeled continue in range with closure-modified state
	total := 0
rows:
	for _, row := range [][]int{{1, 2, -1, 4}, {5, 6}, {-7, 8}, {9}} {
		for _, v := range row {
			if v < 0 {
				total -= 1000
				continue rows
			}
			total += v
		}
		total *= 2
	}
	println(total)

	// nested labeled breaks out of select-free blocks
	cnt := 0
L1:
	for {
	L2:
		for {
			cnt++
			switch {
			case cnt%5 == 0:
				break L2
			case cnt > 17:
				break L1
			}
		}
		cnt += 10
	}
	println(cnt)

	// for with function-call condition evaluated each iteration
	log = ""
	m := 0
	for ev("c", m) < 3 {
		m++
	}
	println(log, m)
}
