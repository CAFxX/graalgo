package main

import (
	"fmt"
	"testing"
)

func TestDbg(t *testing.T) {
	out, err := ExportFile("/tmp/gn02.go")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(out[:200])
}
