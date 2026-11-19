// internal/reflectlite reads the package qualifier of an unexported method
// name by copying four bytes into an int32 through a []byte over the
// variable (reflectlite.pkgPath: copy((*[4]byte)(unsafe.Pointer(&nameOff))[:],
// ...)) — a byte view of a word, which the flat value model does not give
// (one slice shape). A name carries the qualifier when the method does not
// belong to its interface type's package: an unexported method of an
// unnamed interface type. errors.As to such a target stops with the fatal
// error; with a defined interface type (first line) it runs.
package main

import "errors"

type E struct{}

func (E) Error() string { return "e" }
func (E) secret() int   { return 7 }

type named interface {
	error
	secret() int
}

func main() {
	var n named
	println(errors.As(error(E{}), &n), n != nil && n.secret() == 7)
	var a interface {
		error
		secret() int
	}
	println(errors.As(error(E{}), &a), a != nil && a.secret() == 7)
}
