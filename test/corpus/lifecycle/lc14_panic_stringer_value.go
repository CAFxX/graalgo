package main

type S struct{ n int }

func (s S) String() string { return "stringer!" }

func main() {
	panic(S{3})
}
