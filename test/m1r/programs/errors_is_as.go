package main

// errors from upstream's source on descriptor images (toolexec W9a): the
// package initializer reads a type descriptor (reflectlite.TypeOf), Is asks
// Comparable, As walks pointer, interface and method tables and stores
// through reflectlite.Value.Set (typedmemmove, ifaceE2I).

import "errors"

type codeErr struct{ code int }

func (e *codeErr) Error() string { return "code error" }

type valErr struct {
	op   string
	n    int
	tags [2]string
}

func (e valErr) Error() string { return "val " + e.op }

type wrapErr struct {
	msg string
	err error
}

func (e *wrapErr) Error() string { return e.msg + ": " + e.err.Error() }
func (e *wrapErr) Unwrap() error { return e.err }

type multiErr struct{ errs []error }

func (e *multiErr) Error() string   { return "multi" }
func (e *multiErr) Unwrap() []error { return e.errs }

type isErr struct{ id int }

func (e isErr) Error() string { return "is" }
func (e isErr) Is(target error) bool {
	t, ok := target.(isErr)
	return ok && t.id/10 == e.id/10
}

type asErr struct{ to *codeErr }

func (e asErr) Error() string { return "as" }
func (e asErr) As(target any) bool {
	if p, ok := target.(**codeErr); ok {
		*p = e.to
		return true
	}
	return false
}

// An uncomparable error: Is must not compare it (it would panic).
type sliceErr []string

func (e sliceErr) Error() string { return "slice" }

type timeouter interface{ Timeout() bool }

type netErr struct{ t bool }

func (e netErr) Error() string { return "net" }
func (e netErr) Timeout() bool { return e.t }

type coder interface {
	error
	Code() int
}

type richErr struct{ c int }

func (e *richErr) Error() string { return "rich" }
func (e *richErr) Code() int     { return e.c }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if s, ok := r.(string); ok {
				println(name, "panic:", s)
			} else if e, ok := r.(error); ok {
				println(name, "panic error:", e.Error())
			} else {
				println(name, "panic: other")
			}
		}
	}()
	f()
}

var sentinel = errors.New("sentinel")

func main() {
	// New: distinct identities, equal text.
	a, b := errors.New("boom"), errors.New("boom")
	println(a.Error(), a == b, a == a, errors.Is(a, b), errors.Is(a, a))

	// Unwrap / Is through a chain.
	w1 := &wrapErr{"w1", sentinel}
	w2 := &wrapErr{"w2", w1}
	println(w2.Error())
	println(errors.Unwrap(w2) == error(w1), errors.Unwrap(sentinel) == nil, errors.Unwrap(nil) == nil)
	println(errors.Is(w2, sentinel), errors.Is(w2, w1), errors.Is(w1, w2), errors.Is(nil, sentinel), errors.Is(w2, nil), errors.Is(nil, nil))

	// Is with value errors (comparable structs with an array), a custom Is method, and uncomparable errors.
	v := valErr{"read", 3, [2]string{"x", "y"}}
	println(errors.Is(&wrapErr{"w", v}, valErr{"read", 3, [2]string{"x", "y"}}),
		errors.Is(&wrapErr{"w", v}, valErr{"read", 4, [2]string{"x", "y"}}))
	println(errors.Is(&wrapErr{"w", isErr{12}}, isErr{17}), errors.Is(isErr{12}, isErr{21}))
	println(errors.Is(sliceErr{"a"}, sliceErr{"a"}), errors.Is(&wrapErr{"w", sliceErr{"a"}}, sentinel))

	// As: pointer target, value target, interface targets, a custom As method.
	var ce *codeErr
	println(errors.As(&wrapErr{"w", &codeErr{7}}, &ce), ce != nil && ce.code == 7)
	ce = nil
	println(errors.As(w2, &ce), ce == nil)
	var ve valErr
	println(errors.As(&wrapErr{"w", v}, &ve), ve.op, ve.n, ve.tags[1])
	var to timeouter
	println(errors.As(&wrapErr{"w", netErr{true}}, &to), to != nil && to.Timeout())
	to = nil
	println(errors.As(w2, &to), to == nil)
	var co coder
	println(errors.As(&wrapErr{"w", &richErr{42}}, &co), co != nil && co.Code() == 42, co.Error())
	var anyErr error
	println(errors.As(w2, &anyErr), anyErr == error(w2))
	var we *wrapErr
	println(errors.As(w2, &we), we == w2)
	ce = nil
	target := &codeErr{99}
	println(errors.As(&wrapErr{"w", asErr{target}}, &ce), ce == target)
	println(errors.As(nil, &ce))

	// Join and multi-error trees.
	j := errors.Join(a, nil, w1)
	println(j.Error())
	println(errors.Is(j, sentinel), errors.Is(j, a), errors.Is(j, b), errors.Join(nil, nil) == nil)
	ce = nil
	m := &multiErr{[]error{errors.New("x"), &wrapErr{"deep", &codeErr{5}}, sentinel}}
	println(errors.Is(m, sentinel), errors.As(m, &ce), ce.code)
	if u, ok := j.(interface{ Unwrap() []error }); ok {
		println(len(u.Unwrap()))
	}
	println(errors.ErrUnsupported.Error(), errors.Is(&wrapErr{"w", errors.ErrUnsupported}, errors.ErrUnsupported))

	// As's argument checks panic with errors' own messages.
	try("nil target", func() { errors.As(a, nil) })
	try("non-pointer", func() { errors.As(a, ce) })
	var np *int
	try("non-error elem", func() { errors.As(a, &np) })
	var nilp **codeErr
	try("nil pointer", func() { errors.As(a, nilp) })
	try("uncomparable target", func() { println(errors.Is(sentinel, sliceErr{"a"})) })
}
