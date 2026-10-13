// 01_run_filter: -test.run filtering (top-level and subtests) via the
// caller-supplied matchString. Run with -test.run=TestB or -test.run=TestSub/b.
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

