// Targets panics raised while deferred calls run: a new panic in a defer
// replaces the in-flight one (recover sees the newest), panics during the
// defers of an already-recovered panic, re-panic of the recovered value, and
// that remaining defers of the frame still run in order.
package main

import (
	"errors"
	"strconv"
)

var trace []string

func t(s string) { trace = append(trace, s) }

func show(r any) string {
	switch v := r.(type) {
	case nil:
		return "<nil>"
	case string:
		return "string:" + v
	case int:
		return "int:" + strconv.Itoa(v)
	case error:
		return "error:" + v.Error()
	}
	return "other"
}

func replace() {
	defer func() { t("replace recovered " + show(recover())) }()
	defer t("replace d2")
	defer func() {
		t("replace d1 panics B")
		panic("B")
	}()
	panic("A")
}

func panicAfterRecover() {
	defer func() { t("par outer " + show(recover())) }()
	defer func() {
		r := recover()
		t("par inner " + show(r))
		panic(errors.New("second from defer"))
	}()
	panic(1)
}

func repanicSame() {
	defer func() { t("repanic outer " + show(recover())) }()
	defer func() {
		r := recover()
		t("repanic mid " + show(r))
		panic(r)
	}()
	panic("orig")
}

func deepChain(n int) {
	defer func() {
		if n == 0 {
			t("deep bottom " + show(recover()))
			return
		}
		panic("level" + strconv.Itoa(n))
	}()
	if n == 3 {
		panic("start")
	}
	deepChain(n + 1)
}

func recoverThenPanicInLaterDefer() {
	defer func() { t("later outer " + show(recover())) }()
	defer func() { panic("from-earlier-registered") }()
	defer func() { t("first-run recovered " + show(recover())) }()
	panic("x")
}

func panicInRecoveredFrameDefer() (res string) {
	defer func() {
		if r := recover(); r != nil {
			res = "caught " + show(r)
		}
	}()
	func() {
		defer func() {
			recover()
			defer func() { t("nested defer of recovering func") }()
		}()
		panic("inner-only")
	}()
	panic("after-inner")
}

func main() {
	replace()
	panicAfterRecover()
	repanicSame()
	deepChain(0)
	recoverThenPanicInLaterDefer()
	t(panicInRecoveredFrameDefer())
	for _, s := range trace {
		println(s)
	}
}
