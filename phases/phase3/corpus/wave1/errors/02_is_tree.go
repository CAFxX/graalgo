package main

import "errors"

// er02: errors.Is over wrap trees — single Unwrap chains, a custom Is
// method, and a deep (200) nesting.

type wrapErr struct {
	msg string
	err error
}

func (w *wrapErr) Error() string { return w.msg + ": " + w.err.Error() }
func (w *wrapErr) Unwrap() error { return w.err }

type codeErr struct{ code int }

func (c *codeErr) Error() string { return "codeErr" }
func (c *codeErr) Is(target error) bool {
	t, ok := target.(*codeErr)
	return ok && t.code == c.code
}

func main() {
	sentinel := errors.New("sentinel")
	w1 := &wrapErr{msg: "w1", err: sentinel}
	w2 := &wrapErr{msg: "w2", err: w1}
	println(errors.Is(w2, sentinel))          // true (depth 2)
	println(errors.Is(w2, errors.New("no"))) // false
	println(errors.Is(w1, w2))               // false (w2 not in w1's tree)
	println(errors.Is(w2, w1))               // true (w1 in w2's tree)

	// Custom Is method: matches by code, not identity.
	c7 := &codeErr{code: 7}
	c7b := &codeErr{code: 7}
	c8 := &codeErr{code: 8}
	println(errors.Is(c7, c7b)) // true via Is method
	println(errors.Is(c7, c8))  // false
	wc := &wrapErr{msg: "wc", err: c7}
	println(errors.Is(wc, c7b)) // true through wrapper + Is method

	// Deep chain: 200 nested wrappers around the sentinel.
	deep := error(sentinel)
	for i := 0; i < 200; i++ {
		deep = &wrapErr{msg: "d", err: deep}
	}
	println(errors.Is(deep, sentinel)) // true
	println(errors.Is(deep, c7b))      // false
}
