package main

func main() {
	// infinite for with break
	n := 0
	count := 0
	for {
		n += 3
		count++
		if n >= 100 {
			break
		}
	}
	// break inside nested if
	m := 0
	for {
		m++
		if m > 5 {
			if m > 7 {
				break
			}
		}
		if m > 100 {
			break
		}
	}
	_ = n
	_ = count
	_ = m
}

// observe: n count m
