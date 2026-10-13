// 11_list: -test.list=Test prints matching test/benchmark/example names
// without running them (see 11_list.args). Regexp syntax here is the
// corpus matchString's (substring per element), identical in both legs.
package main

import (
	"fmt"
	"testing"
)

func TestAlpha(t *testing.T) {}
func TestBeta(t *testing.T)  {}

func BenchmarkAlpha(b *testing.B) {}

func ExampleAlpha() {
	fmt.Println("x")
	// Output: x
}

func main() {
	tests := []testing.InternalTest{
		{"TestAlpha", TestAlpha},
		{"TestBeta", TestBeta},
	}
	benches := []testing.InternalBenchmark{
		{"BenchmarkAlpha", BenchmarkAlpha},
	}
	examples := []testing.InternalExample{
		{"ExampleAlpha", ExampleAlpha, "x\n", false},
	}
	testing.Main(nil, tests, benches, examples)
}

