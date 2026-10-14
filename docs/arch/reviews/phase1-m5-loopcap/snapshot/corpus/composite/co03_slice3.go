package main

func main() {
	s := []int{1, 2, 3, 4, 5, 6}
	// 3-index slice: values correct, length via range count
	t := s[1:4:5]
	tv := 0
	tc := 0
	for _, v := range t {
		tv += v
		tc++
	}
	// full slice expression
	u := s[:]
	uv := 0
	for _, v := range u {
		uv += v
	}
	// slice of array
	var arr [6]int
	for i := range arr {
		arr[i] = i * 2
	}
	w := arr[2:5]
	wv := 0
	for _, v := range w {
		wv += v
	}
	_ = tv
	_ = tc
	_ = uv
	_ = wv
}

// observe: tv tc uv wv
