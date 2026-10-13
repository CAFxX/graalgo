package main

import "fmt"

// Writer is io.Writer-shaped but single-result: frontend/v1 WS D allows
// at most one result per method, so io.Writer's (int, error) pair is out
// of scope. The dispatch mechanics are identical.
type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }
func (b *Buf) Len() int           { return b.n }

type Counter int

func (c Counter) Write(p []byte) int { return len(p) * 2 }

func emit(w Writer, s string) int { return w.Write([]byte(s)) }

func main() {
	// Pointer receiver through the interface.
	b := &Buf{}
	var w Writer = b
	fmt.Println(emit(w, "hello"))
	fmt.Println(b.Len())
	// Value receiver through the same interface.
	var w2 Writer = Counter(0)
	fmt.Println(emit(w2, "hello"))
	// Concrete passed where the interface is expected (call-site
	// argument adaptation).
	fmt.Println(emit(b, "bye"))
	fmt.Println(b.Len())
}
