package main

func main() {
	// range over array (copies the array: writes don't affect the original)
	var a [5]int
	for i := range a {
		a[i] = i * 3
	}
	asum := 0
	for _, v := range a {
		asum += v
	}
	// range over string: index is byte offset, value is rune
	str := "A\xc3\xa9Z"
	scount := 0
	sidx := 0
	rsum := 0
	for i, r := range str {
		scount++
		sidx += i
		rsum += int(r)
	}
	// range with non-define assignment
	t := 0
	tv := 0
	for t, tv = range a {
	}
	_ = asum
	_ = scount
	_ = sidx
	_ = rsum
	_ = t
	_ = tv
}

// observe: asum scount sidx rsum t tv
