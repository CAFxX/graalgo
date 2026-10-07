package main

import (
	"fmt"
	"io"
)

// io07: the single-result constructors — LimitReader field wiring,
// TeeReader/NopCloser/MultiReader/MultiWriter/Discard shapes.
func main() {
	lr := io.LimitReader(nil, 42)
	if l, ok := lr.(*io.LimitedReader); ok {
		fmt.Println("limited", l.N, l.R == nil)
	} else {
		fmt.Println("limited: not a *LimitedReader")
	}
	fmt.Println("tee", io.TeeReader(nil, nil) != nil)
	fmt.Println("nopcloser", io.NopCloser(nil).Close() == nil)
	fmt.Println("multireader", io.MultiReader(nil, nil) != nil)
	fmt.Println("multiwriter", io.MultiWriter(nil, nil) != nil)
	fmt.Println("discard", io.Discard != nil)

	// MultiWriter flattens nested *multiWriter at construction.
	inner := io.MultiWriter(nil, nil)
	outer := io.MultiWriter(inner, nil)
	fmt.Println("nested", outer != nil)
}
