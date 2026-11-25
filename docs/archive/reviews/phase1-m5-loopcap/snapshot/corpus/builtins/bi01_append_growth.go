package main

// Append growth: cap sequence must match gc's growslice exactly.
func main() {
	var s []int
	for i := 0; i < 20; i++ {
		s = append(s, i)
		if i == 0 || i == 1 || i == 2 || i == 4 || i == 8 || i == 16 || i == 19 {
			println("len:", len(s), "cap:", cap(s))
		}
	}
	// Sum to verify elements.
	sum := 0
	for _, v := range s {
		sum += v
	}
	println("sum:", sum)
}
