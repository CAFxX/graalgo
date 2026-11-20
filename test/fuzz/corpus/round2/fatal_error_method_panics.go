// Fatal: the panic value is an error whose Error method itself panics while
// the runtime prints it; gc turns that into "fatal error: panic while
// printing panic value: ..." (a throw, exit 2) after running the defers.
// Warm-ups recover the same kind of value and call Error safely.
package main

import (
	"errors"
	"strconv"
)

type flaky struct {
	calls *int
	limit int
}

func (f flaky) Error() string {
	*f.calls++
	if *f.calls > f.limit {
		panic("Error called " + strconv.Itoa(*f.calls) + " times")
	}
	return "flaky ok " + strconv.Itoa(*f.calls)
}

func safe(e error) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = "Error panicked: " + r.(string)
		}
	}()
	return e.Error()
}

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func main() {
	n := 0
	e := flaky{&n, 2}
	println("first call:", e.Error())
	got := catch(func() { panic(e) })
	err := got.(error)
	println("recovered value calls", safe(err), safe(err), n)
	wrapped := errors.Join(errors.New("outer"), err)
	println("joined", safe(wrapped))
	var target flaky
	println("as", errors.As(wrapped, &target), *target.calls)
	println("is", errors.Is(wrapped, e), errors.Is(wrapped, flaky{&n, 2}), errors.Is(wrapped, flaky{new(int), 2}))

	m := 0
	fresh := flaky{&m, 1}
	defer println("defer runs before the fatal print")
	println("fresh:", fresh.Error())
	panic(fresh)
}
