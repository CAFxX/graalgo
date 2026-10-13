package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }
func (b Buf) Size() int           { return b.n }

// MyWriter satisfies Writer purely by promotion from the embedded *Buf:
// dispatch must adapt the receiver to the embedded value (wire "index"
// path), not pass the outer struct.
type MyWriter struct{ *Buf }

// VSink promotes the value-receiver Size from the embedded Buf value.
type VSink struct{ Buf }

type Sizer interface{ Size() int }

func main() {
	mw := &MyWriter{}
	mw.Buf = &Buf{}
	var w Writer = mw
	fmt.Println(w.Write([]byte("hello")))
	// The write went through the promoted method into the embedded *Buf.
	fmt.Println(mw.Buf.n)
	vs := &VSink{}
	vs.Buf.n = 41
	var s Sizer = vs
	fmt.Println(s.Size())
}
