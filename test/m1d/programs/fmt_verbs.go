// fmt_verbs.go — a matrix of fmt verbs, flags and operand kinds: Println,
// Printf, Sprintf, Errorf, Fprintf to os.Stdout/os.Stderr, Sscanf/Sscan;
// Stringer, error, GoStringer and Formatter; structs, maps, slices, pointers.
package main

import (
	"errors"
	"fmt"
	"os"
)

type P struct {
	X, Y int
	Name string
}
type S string

func (s S) String() string { return "S(" + string(s) + ")" }

type E struct{ code int }

func (e *E) Error() string { return fmt.Sprintf("E%d", e.code) }

type G int

func (g G) GoString() string { return "G!" }

type F struct{}

func (F) Format(s fmt.State, verb rune) { fmt.Fprintf(s, "F<%c>", verb) }

func main() {
	fmt.Println("a", 1, true, 2.5, nil, 'x', "s")
	fmt.Printf("%d %5d %-5d| %05d %x %X %o %b %c %U %q\n", 42, 42, 42, 42, 255, 255, 8, 5, 'A', 0x1F600, 'x')
	fmt.Printf("%s %q %10s|%-10s| %x %X\n", "hi", "hi\n", "r", "l", "hex", []byte("hex"))
	fmt.Printf("%f %.2f %8.3f %e %g %G %v\n", 3.14159, 3.14159, 3.14159, 1234.5678, 1e21, 1e-7, float32(0.1))
	fmt.Printf("%t %v %p\n", true, false, nil)
	p := P{1, 2, "n"}
	fmt.Printf("%v %+v %#v %T\n", p, p, p, p)
	fmt.Printf("%v %+v %#v %T\n", &p, &p, &p, &p)
	fmt.Printf("%v %#v %T\n", []int{1, 2, 3}, []string{"a", "b"}, []int(nil))
	fmt.Printf("%v %#v\n", map[string]int{"b": 2, "a": 1, "c": 3}, map[int]bool{2: true, 1: false})
	fmt.Printf("%v %s %d\n", S("x"), S("y"), G(3))
	fmt.Printf("%#v %v\n", G(3), F{})
	var err error = &E{7}
	fmt.Printf("%v %s %q\n", err, err, err)
	w := fmt.Errorf("wrap: %w", err)
	fmt.Println(w, errors.Is(w, err), errors.Unwrap(w) == err)
	fmt.Printf("%v %d %s\n", [3]int{1, 2, 3}, [2]int8{-1, 2}, [2]string{"a", "b"})
	fmt.Printf("%v %v %v\n", struct{}{}, struct{ A any }{nil}, struct{ p *int }{nil})
	fmt.Printf("%5t|%-7v|%+d|% d|%x\n", true, 12, 5, 5, -255)
	fmt.Printf("%!|%z|%d\n", 1)
	fmt.Printf("%d %s\n", "str", 5)
	fmt.Printf("%[2]d %[1]d %*d %.*f\n", 1, 2, 5, 42, 2, 3.14159)
	fmt.Printf("%v %v\n", complex(1, 2), int64(-1)<<63)
	fmt.Printf("%v %v %v\n", uint8(200), uintptr(77), 'a')
	var ip *int
	var e2 error
	fmt.Printf("%v %v %d\n", ip, e2, []any{1, "a", nil, 2.5})
	fmt.Fprintf(os.Stdout, "%s-%d\n", "out", 1)
	fmt.Fprintln(os.Stderr, "to stderr", 2)
	s := fmt.Sprint("a", 1, 2, "b", "c", 3.0)
	fmt.Println(s, len(s), fmt.Sprintln("x", 1))
	var a, b int
	var str string
	n, err := fmt.Sscanf("10 20 abc", "%d %d %s", &a, &b, &str)
	fmt.Println(n, err, a, b, str)
	n, err = fmt.Sscan("7 8", &a, &b)
	fmt.Println(n, err, a, b)
	fmt.Printf("%v %v\n", struct {
		A int
		B []string
		C map[string]P
		D *P
	}{1, []string{"x"}, map[string]P{"k": {1, 2, "z"}}, nil}, [][]int{{1}, {2, 3}})
	fmt.Printf("%08.3f|%+.2e|%x|%X|% x\n", 3.14159, 12345.678, "hello", "hello", "hello")
	fmt.Printf("%t %v\n", "notbool", make(chan int) == nil)
}
