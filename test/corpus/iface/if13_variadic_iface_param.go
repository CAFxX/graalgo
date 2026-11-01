// G2 (2026-11-01): variadic interface methods with interface-typed
// FIXED parameters. The non-variadic analogue is if10; the variadic
// form previously skipped call-site boxing for the whole signature
// (IfaceDispatchBuilder returned null for variadic signatures), so
// the concrete argument arrived unboxed and the callee's use of it
// failed with "no method table installed for dynamic type
// \"interface\"". Covers: named-interface fixed parameter (non-spread,
// spread, and zero variadic args), empty-interface fixed parameter
// (type switch inside the callee), and a control method with no
// interface-typed fixed parameter.
package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

// Variadic interface method with a NAMED-interface fixed parameter.
type Copier interface {
	CopyFrom(src Writer, parts ...[]byte) int
}

type Dup struct{ n int }

func (d *Dup) CopyFrom(src Writer, parts ...[]byte) int {
	total := 0
	for _, p := range parts {
		total += src.Write(p)
	}
	d.n += total
	return d.n
}

// Variadic interface method with an EMPTY-interface fixed parameter.
type Describer interface{ Show(v any, nums ...int) int }

type Desc struct{}

func (Desc) Show(v any, nums ...int) int {
	code := 0
	switch v.(type) {
	case int:
		code = 1
	case string:
		code = 2
	default:
		code = 0
	}
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return code*100 + sum
}

// Control: variadic interface method with NO interface-typed fixed param.
type Summer interface {
	Sum(prefix string, xs ...int) int
}

type S struct{}

func (S) Sum(prefix string, xs ...int) int {
	_ = prefix
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

func main() {
	b := &Buf{}
	var c Copier = &Dup{}
	fmt.Println(c.CopyFrom(b, []byte("xy"), []byte("z")))
	fmt.Println(b.n)
	parts := [][]byte{[]byte("a"), []byte("bb")}
	fmt.Println(c.CopyFrom(b, parts...))
	fmt.Println(b.n)
	fmt.Println(c.CopyFrom(b))
	fmt.Println(b.n)
	var d Describer = Desc{}
	iv := 42
	sv := "hi"
	fmt.Println(d.Show(iv, 1, 2, 3))
	fmt.Println(d.Show(sv, 10))
	var s Summer = S{}
	fmt.Println(s.Sum("x", 1, 2, 3))
	fmt.Println(s.Sum("y"))
}
