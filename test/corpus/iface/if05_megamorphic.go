package main

import "fmt"

type Writer interface{ Write([]byte) int }

type Buf struct{ n int }

func (b *Buf) Write(p []byte) int { b.n += len(p); return len(p) }

type Counter int

func (c Counter) Write(p []byte) int { return len(p) }

type Null struct{ dropped int }

func (n *Null) Write(p []byte) int { n.dropped += len(p); return 0 }

type Tag string

func (t Tag) Write(p []byte) int { return len(t) + len(p) }

// ONE call site below is fed four distinct concrete types: the PIC
// (limit 3) overflows into the megamorphic hash fallback.
func emit(w Writer) int {
	total := 0
	for i := 0; i < 4; i++ {
		total += w.Write([]byte("ab"))
	}
	return total
}

func main() {
	fmt.Println(emit(&Buf{}))
	fmt.Println(emit(Counter(0)))
	fmt.Println(emit(&Null{}))
	fmt.Println(emit(Tag("xy")))
}
