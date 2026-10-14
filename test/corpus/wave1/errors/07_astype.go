package main

import "errors"

// er07: errors.AsType — generic type-parameter match over the error tree.

type timeoutErr struct{ msg string }

func (e *timeoutErr) Error() string { return e.msg }

type wrapErr struct {
	msg string
	err error
}

func (e *wrapErr) Error() string { return e.msg }
func (e *wrapErr) Unwrap() error { return e.err }

// asMethodErr converts itself to *timeoutErr via a custom As method.
type asMethodErr struct{ code int }

func (e *asMethodErr) Error() string { return "asMethod" }
func (e *asMethodErr) As(target any) bool {
	if t, ok := target.(**timeoutErr); ok {
		*t = &timeoutErr{msg: "converted"}
		return true
	}
	return false
}

type timeoutIface interface {
	error
	Timeout() bool
}

type timeoutImpl struct{}

func (timeoutImpl) Error() string { return "timeout" }
func (timeoutImpl) Timeout() bool { return true }

func main() {
	t1 := &timeoutErr{msg: "t1"}
	w := &wrapErr{msg: "wrap", err: t1}
	j := errors.Join(errors.New("x"), w)

	// 1. direct concrete-type match
	if e, ok := errors.AsType[*timeoutErr](t1); ok {
		println("direct:", e.msg, ok)
	} else {
		println("direct: MISS")
	}
	// 2. match through an Unwrap chain
	if e, ok := errors.AsType[*timeoutErr](w); ok {
		println("wrapped:", e.msg, ok)
	} else {
		println("wrapped: MISS")
	}
	// 3. match through a Join tree (depth-first)
	if e, ok := errors.AsType[*timeoutErr](j); ok {
		println("join:", e.msg, ok)
	} else {
		println("join: MISS")
	}
	// 4. miss: wrong concrete type
	if _, ok := errors.AsType[*wrapErr](t1); ok {
		println("miss: FALSE-POSITIVE")
	} else {
		println("miss: ok")
	}
	// 5. nil err -> zero value, false
	if _, ok := errors.AsType[*timeoutErr](nil); ok {
		println("nil: FALSE-POSITIVE")
	} else {
		println("nil: ok")
	}
	// 6. interface E matches an implementing value
	if _, ok := errors.AsType[timeoutIface](timeoutImpl{}); ok {
		println("iface: ok")
	} else {
		println("iface: MISS")
	}
	// 7. E = error matches any non-nil error
	if e, ok := errors.AsType[error](t1); ok {
		println("anyerr:", e.Error(), ok)
	} else {
		println("anyerr: MISS")
	}
	// 8. custom As(any) method path (lazy *E initialization)
	am := &asMethodErr{code: 7}
	if e, ok := errors.AsType[*timeoutErr](am); ok {
		println("asmethod:", e.msg, ok)
	} else {
		println("asmethod: MISS")
	}
	// 9. first match in a Join tree wins (depth-first order)
	j2 := errors.Join(&wrapErr{msg: "first", err: &timeoutErr{msg: "inner1"}},
		&timeoutErr{msg: "second"})
	if e, ok := errors.AsType[*timeoutErr](j2); ok {
		println("firstwin:", e.msg, ok)
	} else {
		println("firstwin: MISS")
	}
	// 10. miss through a chain ending in nil Unwrap
	wn := &wrapErr{msg: "nilinner", err: nil}
	if _, ok := errors.AsType[*timeoutErr](wn); ok {
		println("nilunwrap: FALSE-POSITIVE")
	} else {
		println("nilunwrap: ok")
	}
}
