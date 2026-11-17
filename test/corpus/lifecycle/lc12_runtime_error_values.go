package main

import (
	"fmt"
	"runtime"
)

func try(name string, f func()) {
	defer func() {
		r := recover()
		_, isErr := r.(error)
		_, isRt := r.(runtime.Error)
		fmt.Printf("%s: error=%v runtime.Error=%v", name, isErr, isRt)
		if e, ok := r.(error); ok {
			fmt.Printf(" msg=%q", e.Error())
		}
		if e, ok := r.(runtime.Error); ok {
			fmt.Printf(" rt=%q", e.Error())
		}
		fmt.Printf(" v=%v T=%T\n", r, r)
	}()
	f()
}

type I interface{ M() }

func main() {
	var s []int
	var m map[string]int
	var p *int
	var x any = "str"
	zero := 0
	try("index", func() { _ = s[5] })
	try("slice", func() { _ = s[1:5] })
	try("nilmap", func() { m["a"] = 1 })
	try("nilderef", func() { _ = *p })
	try("divzero", func() { _ = 10 / zero })
	try("assert", func() { _ = x.(int) })
	try("assertiface", func() { _ = x.(I) })
	try("panicnil", func() { panic(nil) })
	try("custom", func() { panic(fmt.Errorf("custom %d", 1)) })
	try("closed", func() { c := make(chan int); close(c); close(c) })
}
