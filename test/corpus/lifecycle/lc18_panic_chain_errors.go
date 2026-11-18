package main

import (
	"errors"
	"fmt"
)

type E struct{ s string }

func (e *E) Error() string { return "E:" + e.s }

func main() {
	defer func() {
		panic(fmt.Errorf("third %v", recover()))
	}()
	defer func() {
		recover()
		panic(&E{"second"})
	}()
	panic(errors.New("first"))
}
