package main

type Stack[T any] struct {
	elems []T
	n     int
}

// MapTo is a generic method: it has the receiver's type param T plus its
// own type param U. Each distinct U instantiates a separate method body.
func (s Stack[T]) MapTo[U any](f func(T) U, out []U) {
	for i := 0; i < s.n; i++ {
		out[i] = f(s.elems[i])
	}
}

func main() {
	s := Stack[int]{elems: []int{1, 2, 3}, n: 3}
	strs := []string{"", "", ""}
	s.MapTo[string](func(x int) string {
		if x == 1 {
			return "a"
		}
		return "b"
	}, strs)
	a := 0
	if strs[0] == "a" && strs[1] == "b" && strs[2] == "b" {
		a = 1
	}
	nums := []int{0, 0, 0}
	s.MapTo[int](func(x int) int { return x * 10 }, nums)
	b := nums[0] + nums[1] + nums[2]
	a = a
	b = b
}

// verifies: GG-0014
// observe: a b
