package main

import "fmt"

// io.Writer-shaped (single-result — see if00) with three implementations:
// pointer-receiver accumulator, pointer-receiver discarder, value receiver.
type Writer interface{ Write([]byte) int }

type Buf struct{ s string }

func (b *Buf) Write(p []byte) int { b.s += string(p); return len(p) }

type Null struct{ n int }

func (n *Null) Write(p []byte) int { n.n += len(p); return len(p) }

type Twice struct{}

func (t Twice) Write(p []byte) int { return len(p) * 2 }

// Fprintf-style usage: fmt.Fprintf does not exist in the synthetic fmt
// overlay (seven D4 symbols only), and calling a user-defined variadic
// function with packed arguments is out of frontend/v1 scope — so each
// site composes fmt.Sprintf with Write directly, which is all Fprintf
// does.
func writeTo(w Writer, s string) int {
	return w.Write([]byte(s))
}

func main() {
	b := &Buf{}
	fmt.Println(writeTo(b, fmt.Sprintf("hello %s %d", "world", 42)))
	fmt.Println(b.s)
	n := &Null{}
	fmt.Println(writeTo(n, fmt.Sprintf("%d", 7)))
	fmt.Println(n.n)
	fmt.Println(writeTo(Twice{}, fmt.Sprintf("abc")))
}
