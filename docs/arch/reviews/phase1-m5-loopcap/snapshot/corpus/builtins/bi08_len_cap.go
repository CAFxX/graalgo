package main

// len/cap across types: string (bytes), array, slice, map.
func main() {
	println("str:", len("héllo")) // 6 bytes
	println("empty:", len(""))

	arr := [5]int{1, 2, 3, 4, 5}
	println("arr len:", len(arr), "cap:", cap(arr))

	s := []int{1, 2, 3}
	println("slice len:", len(s), "cap:", cap(s))

	m := map[int]int{1: 10, 2: 20}
	println("map len:", len(m))
}
