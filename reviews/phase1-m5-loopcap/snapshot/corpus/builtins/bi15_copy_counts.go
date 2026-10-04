package main

// Copy between different-length slices; copy returns count.
func main() {
	src := []int{1, 2, 3, 4, 5}
	dst := make([]int, 3)
	n := copy(dst, src)
	println("n:", n, "dst:", dst[0], dst[1], dst[2])

	dst2 := make([]int, 10)
	n2 := copy(dst2, src)
	println("n2:", n2, "len:", len(dst2))
}
