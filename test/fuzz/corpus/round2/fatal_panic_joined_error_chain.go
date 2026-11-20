// Fatal: a chain of two unrecovered panics: an errors.Join value with
// embedded newlines (gc indents continuation lines with a tab) replaced in
// a defer by a named bool; gc prints both values, oldest first. Warm-ups
// recover multi-line string and error panics and print their text.
package main

import (
	"errors"
	"strings"
)

type verdict bool

type multi []string

func (m multi) Error() string { return strings.Join(m, "\n") }

func catch(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func lines(v any) int {
	switch x := v.(type) {
	case string:
		return strings.Count(x, "\n") + 1
	case error:
		return strings.Count(x.Error(), "\n") + 1
	}
	return 0
}

func main() {
	println("string lines", lines(catch(func() { panic("a\nb\n\nc") })))
	println("multi lines", lines(catch(func() { panic(multi{"x", "y", "z"}) })))
	got := catch(func() {
		defer func() { panic(errors.Join(recover().(error), errors.New("again"))) }()
		panic(multi{"inner1", "inner2"})
	})
	println("rejoined lines", lines(got), got.(error).Error() == "inner1\ninner2\nagain")
	v := catch(func() { panic(verdict(true)) })
	println("verdict", bool(v.(verdict)))

	defer func() {
		println("second-level defer")
		panic(verdict(false))
	}()
	err := errors.Join(errors.New("line one"), errors.New("line two\nline three"), nil)
	println("err has", len(err.Error()), "bytes", lines(err), "lines")
	panic(err)
}
