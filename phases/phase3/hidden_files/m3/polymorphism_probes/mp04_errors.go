package main

import "fmt"

type E1 struct{ msg string }

func (e E1) Error() string { return "e1:" + e.msg }

type E2 struct{ code int }

func (e E2) Error() string { return fmt.Sprint("e2:", e.code) }

type E3 struct{}

func (e E3) Error() string { return "e3" }

func fail(n int) error {
	switch n % 3 {
	case 0:
		return E1{msg: "boom"}
	case 1:
		return E2{code: n}
	default:
		return E3{}
	}
}

func main() {
	for i := 0; i < 9; i++ {
		err := fail(i)
		if err != nil {
			fmt.Println(err.Error())
		}
	}
}
