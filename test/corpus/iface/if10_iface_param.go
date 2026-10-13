package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

// tee takes an interface parameter and is called with a CONCRETE argument:
// the call site must box the argument (factory adaptArgsForInterfaceParams,
// incl. named interface types).
func tee(w Writer, p []byte) int {
	a := w.Write(p)
	c := w.Write(p)
	return a + c
}

type Copier interface{ CopyFrom(src Writer, p []byte) int }

type Dup struct{ n int }

// CopyFrom is an INTERFACE METHOD with an interface-typed parameter,
// called with a concrete argument: the ifacemethod call site must box it
// (the P3 builder's adaptArgsForInterfaceParams replica).
func (d *Dup) CopyFrom(src Writer, p []byte) int {
	n := src.Write(p)
	d.n += n
	return d.n
}

func main() {
	b := &Buf{}
	fmt.Println(tee(b, []byte("hey")))
	fmt.Println(b.n)
	var c Copier = &Dup{}
	fmt.Println(c.CopyFrom(b, []byte("xy")))
	fmt.Println(b.n)
}
