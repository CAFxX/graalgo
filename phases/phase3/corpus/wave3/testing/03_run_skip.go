// 03_run_skip: -test.skip=TestB skips TestB but runs the rest (see 03_run_skip.args).
// (shared body with 01_run_filter.go; the corpus matchString is
// element-wise substring matching, identical in both legs.)
package main

import (
	"fmt"
	"testing"
)

func TestA(t *testing.T) { fmt.Println("A-RAN") }
func TestB(t *testing.T) { fmt.Println("B-RAN") }
func TestC(t *testing.T) { fmt.Println("C-RAN") }

func TestSub(t *testing.T) {
	t.Run("a", func(t *testing.T) { fmt.Println("SUB-A-RAN") })
	t.Run("b", func(t *testing.T) { fmt.Println("SUB-B-RAN") })
	t.Run("c", func(t *testing.T) { fmt.Println("SUB-C-RAN") })
}

func main() {
	tests := []testing.InternalTest{
		{"TestA", TestA},
		{"TestB", TestB},
		{"TestC", TestC},
		{"TestSub", TestSub},
	}
	testing.Main(nil, tests, nil, nil)
}

