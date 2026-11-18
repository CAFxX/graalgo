package main

// A typed nil pointer stored in an interface is not a nil interface: a
// type switch selects its pointer case (or an interface case it
// implements), never `case nil`.

import "fmt"

type T struct{ n int }

func (t *T) String() string { return "T" }

func classify(i any) string {
	switch v := i.(type) {
	case nil:
		return "nil"
	case *T:
		return fmt.Sprintf("*T nil=%v", v == nil)
	default:
		return "other"
	}
}

func main() {
	var p *T
	var i any = p
	fmt.Println(classify(i))
	fmt.Println(classify(nil))
	fmt.Println(classify(&T{1}))
	fmt.Println(i == nil)

	var s fmt.Stringer = p
	fmt.Println(s == nil)
	switch s.(type) {
	case nil:
		fmt.Println("s nil")
	case fmt.Stringer:
		fmt.Println("s Stringer")
	}
	switch i.(type) {
	case int:
		fmt.Println("int")
	case nil:
		fmt.Println("i nil")
	case fmt.Stringer:
		fmt.Println("i Stringer")
	}

	_, ok := i.(*T)
	fmt.Println("assert *T ok:", ok)
	_, ok = i.(fmt.Stringer)
	fmt.Println("assert Stringer ok:", ok)
	var e error
	_, ok = any(e).(error)
	fmt.Println("nil iface assert ok:", ok)
}
