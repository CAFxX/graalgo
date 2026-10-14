package main

func main() {
	// nested loops with labeled break/continue
	total := 0
outer:
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			if j > i {
				continue outer
			}
			if i*j > 20 {
				break outer
			}
			total += i*10 + j
		}
	}
	// labeled continue on inner, unlabeled break
	t2 := 0
	for i := 0; i < 5; i++ {
	inner:
		for j := 0; j < 5; j++ {
			if j == 2 {
				continue inner
			}
			if j == 4 {
				break
			}
			t2 += i*5 + j
		}
	}
	_ = total
	_ = t2
}

// observe: total t2
