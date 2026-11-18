package main

// The fatal-panic report prints error and Stringer values through their
// methods only when the method satisfies the interface by name AND signature
// (gc's preprintpanics type switch); other values print as "(T) address".
// Also a dynamic assertion to an interface whose method has the right name
// but another signature fails, as in gc.

type wrongError struct{ n int }

func (w wrongError) Error() int { return w.n } // not an error: wrong result type

type wrongStringer struct{}

func (wrongStringer) String(x int) string { return "never" } // not a Stringer

type goodStringer struct{}

func (goodStringer) String() string { return "good stringer" }

type errorInt interface{ Error() int }

func main() {
	var x any = wrongError{7}
	_, isError := x.(error)
	e, isErrorInt := x.(errorInt)
	println("is error:", isError, "is errorInt:", isErrorInt, e.Error())
	var s any = wrongStringer{}
	_, isStringer := s.(interface{ String() string })
	println("wrongStringer is Stringer:", isStringer)
	defer func() {
		r := recover()
		println("recovered good stringer:", r.(interface{ String() string }).String())
		defer func() {
			println("recovered wrongStringer:", recover() != nil)
			panic(&wrongError{3})
		}()
		panic(wrongStringer{})
	}()
	panic(goodStringer{})
}
