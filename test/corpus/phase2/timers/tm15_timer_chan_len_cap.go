package main

import (
	"fmt"
	"time"
)

// tm15: timer channels present as unbuffered under gc — len() and cap()
// report 0 in every state (before firing, fired-but-undrained, drained),
// even though the channel physically holds one fire value. A graalgo
// timer channel must observe the same shape (DIV-M3-1).
func main() {
	t := time.NewTimer(100 * time.Millisecond)
	fmt.Println("timer pre-fire:", len(t.C), cap(t.C))
	time.Sleep(400 * time.Millisecond) // fired, undrained
	fmt.Println("timer fired undrained:", len(t.C), cap(t.C))
	<-t.C
	fmt.Println("timer drained:", len(t.C), cap(t.C))

	tk := time.NewTicker(20 * time.Millisecond)
	time.Sleep(400 * time.Millisecond) // many ticks; at most 1 buffered
	fmt.Println("ticker fired:", len(tk.C), cap(tk.C))
	tk.Stop() // drains the pending tick (Go 1.23+ parity)
	fmt.Println("ticker stopped:", len(tk.C), cap(tk.C))
	fmt.Println("done")
}
