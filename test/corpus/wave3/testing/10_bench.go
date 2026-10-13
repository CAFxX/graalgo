// 10_bench: -test.bench=. runs the benchmark once (N=1; no calibration in
// the synthetic testing — durations and ns/op are normalized by the package
// runner, see testing/.normalize.sed). b.N loop + SetBytes + ReportMetric.
package main

import (
	"fmt"
	"testing"
)

func BenchmarkAdd(b *testing.B) {
	b.SetBytes(8)
	sum := 0
	for i := 0; i < b.N; i++ {
		sum += i
	}
	b.ReportMetric(float64(sum), "total")
	fmt.Println("BENCH-RAN")
}

func main() {
	benches := []testing.InternalBenchmark{
		{"BenchmarkAdd", BenchmarkAdd},
	}
	testing.Main(nil, nil, benches, nil)
}

