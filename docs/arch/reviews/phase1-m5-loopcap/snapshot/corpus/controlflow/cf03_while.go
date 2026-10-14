package main

func main() {
	// condition-only for (while style)
	i := 0
	sum := 0
	for i < 50 {
		i++
		if i%3 == 0 {
			continue
		}
		sum += i
	}
	// for with init but no post
	j := 0
	k := 0
	for t := 0; t < 20; {
		t += 2
		j++
		k += t
	}
	_ = i
	_ = sum
	_ = j
	_ = k
}

// observe: i sum j k
