// Targets package errors: Is/As/Unwrap across single and multi-error
// (Unwrap() []error) trees, custom Is/As methods, Join with nils and its
// message, errors.As panics (nil target, non-pointer, non-error pointee),
// ErrUnsupported, comparable vs non-comparable error values, cycles of depth.
package main

import (
	"errors"
	"strconv"
	"strings"
)

type codeErr struct{ code int }

func (e codeErr) Error() string { return "code " + strconv.Itoa(e.code) }

type ptrErr struct{ msg string }

func (e *ptrErr) Error() string { return "ptr:" + e.msg }

type wrap struct {
	msg string
	err error
}

func (w *wrap) Error() string { return w.msg + ": " + w.err.Error() }
func (w *wrap) Unwrap() error { return w.err }

type multi struct{ errs []error }

func (m multi) Error() string {
	var s []string
	for _, e := range m.errs {
		if e == nil {
			s = append(s, "<nil>")
		} else {
			s = append(s, e.Error())
		}
	}
	return "multi[" + strings.Join(s, ",") + "]"
}
func (m multi) Unwrap() []error { return m.errs }

// matches any codeErr with the same code class (code/100)
type classErr struct{ class int }

func (c classErr) Error() string { return "class " + strconv.Itoa(c.class) }
func (c classErr) Is(target error) bool {
	ce, ok := target.(codeErr)
	return ok && ce.code/100 == c.class
}
func (c classErr) As(target any) bool {
	if p, ok := target.(*codeErr); ok {
		p.code = c.class * 100
		return true
	}
	return false
}

// non-comparable error type
type sliceErr []string

func (s sliceErr) Error() string { return strings.Join(s, "+") }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(interface{ Error() string }); ok {
				println(name, "panic:", e.Error())
			} else if s, ok := r.(string); ok {
				println(name, "panic:", s)
			} else {
				println(name, "panic: other")
			}
		}
	}()
	f()
	println(name, "no panic")
}

var sentinel = errors.New("sentinel")

func main() {
	e1 := &wrap{"outer", &wrap{"inner", sentinel}}
	println(e1.Error())
	println(errors.Is(e1, sentinel), errors.Is(e1, errors.New("sentinel")), errors.Unwrap(errors.Unwrap(e1)) == sentinel)
	println(errors.Unwrap(sentinel) == nil, errors.Is(nil, nil), errors.Is(nil, sentinel), errors.Is(sentinel, nil))

	j := errors.Join(nil, codeErr{404}, nil, &ptrErr{"x"}, e1)
	println(j.Error())
	println("join-nil", errors.Join() == nil, errors.Join(nil, nil) == nil)
	println(errors.Is(j, sentinel), errors.Is(j, codeErr{404}), errors.Is(j, codeErr{405}))
	println(errors.Unwrap(j) == nil)
	if u, ok := j.(interface{ Unwrap() []error }); ok {
		println("join-len", len(u.Unwrap()))
	}
	nested := errors.Join(errors.Join(errors.New("a"), errors.New("b")), errors.Join(errors.New("c")))
	println(strings.ReplaceAll(nested.Error(), "\n", "\\n"))

	var ce codeErr
	println(errors.As(j, &ce), ce.code)
	var pe *ptrErr
	println(errors.As(j, &pe), pe.msg)
	var w *wrap
	println(errors.As(j, &w), w.msg)
	var iface interface{ Unwrap() error }
	println(errors.As(j, &iface), iface.(error).Error())
	var tmp interface{ Timeout() bool }
	println(errors.As(j, &tmp), tmp == nil)

	cl := &wrap{"ctx", classErr{4}}
	println(errors.Is(cl, codeErr{404}), errors.Is(cl, codeErr{499}), errors.Is(cl, codeErr{500}), errors.Is(cl, classErr{4}))
	var ce2 codeErr
	println(errors.As(cl, &ce2), ce2.code)

	m := multi{[]error{nil, codeErr{1}, multi{[]error{&wrap{"deep", codeErr{2}}, sentinel}}}}
	println(m.Error())
	println(errors.Is(m, codeErr{2}), errors.Is(m, sentinel), errors.Is(m, codeErr{3}))
	var w2 *wrap
	println(errors.As(m, &w2), w2 != nil && w2.msg == "deep")
	var ce3 codeErr
	println(errors.As(m, &ce3), ce3.code) // depth-first: codeErr{1} first

	se := sliceErr{"p", "q"}
	wse := &wrap{"w", se}
	println(errors.Is(wse, se), errors.Is(wse, sentinel)) // non-comparable: no panic
	var se2 sliceErr
	println(errors.As(wse, &se2), len(se2))

	try("as-nil", func() { errors.As(sentinel, nil) })
	try("as-nonptr", func() { errors.As(sentinel, codeErr{}) })
	try("as-int", func() { var x int; errors.As(sentinel, &x) })
	try("as-nilptr", func() { var p *codeErr; errors.As(sentinel, p) })
	try("as-ok-iface", func() { var e error; println(errors.As(sentinel, &e), e == sentinel) })

	println(errors.ErrUnsupported.Error(), errors.Is(&wrap{"op", errors.ErrUnsupported}, errors.ErrUnsupported))
	println(errors.New("x") == errors.New("x"))

	// long chain
	var chain error = codeErr{7}
	for i := 0; i < 500; i++ {
		if i%50 == 0 {
			chain = errors.Join(chain, codeErr{i})
		} else {
			chain = &wrap{strconv.Itoa(i), chain}
		}
	}
	var h uint64 = 14695981039346656037
	msg := chain.Error()
	for i := 0; i < len(msg); i++ {
		h = (h ^ uint64(msg[i])) * 1099511628211
	}
	println("chain", len(msg), h, errors.Is(chain, codeErr{7}), errors.Is(chain, codeErr{450}), errors.Is(chain, codeErr{451}))
	var cc codeErr
	println(errors.As(chain, &cc), cc.code)
}
