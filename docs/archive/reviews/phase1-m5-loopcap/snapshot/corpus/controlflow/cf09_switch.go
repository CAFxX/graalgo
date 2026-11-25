package main

func main() {
	// expression switch with init, multiple cases, fallthrough, default
	r1 := 0
	switch x := 42; x {
	case 1, 2, 3:
		r1 = 10
	case 42:
		r1 = 20
		fallthrough
	case 43:
		r1 += 5
	default:
		r1 = -1
	}
	// switch without tag (boolean cases)
	r2 := 0
	y := 7
	switch {
	case y < 0:
		r2 = -10
	case y < 10:
		r2 = 10
	default:
		r2 = 99
	}
	// switch on string
	r3 := 0
	switch s := "b"; s {
	case "a":
		r3 = 1
	case "b":
		r3 = 2
	case "c":
		r3 = 3
	}
	// no match, no default
	r4 := 5
	switch r4 {
	case 1:
		r4 = 100
	case 2:
		r4 = 200
	}
	// break inside switch case
	r5 := 0
	switch v := 3; v {
	case 3:
		r5 = 30
		if true {
			break
		}
		r5 = 300
	default:
		r5 = -30
	}
	_ = r1
	_ = r2
	_ = r3
	_ = r4
	_ = r5
}

// observe: r1 r2 r3 r4 r5
