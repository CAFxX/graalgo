//go:build benchpgo

// The profiling build of the gc+PGO leg (suite.py --pgo): a CPU profile of
// the whole run written to $BENCH_CPUPROFILE. gc only.
package h

import (
	"os"
	"runtime/pprof"
)

var profile *os.File

func init() {
	f, err := os.Create(os.Getenv("BENCH_CPUPROFILE"))
	if err != nil {
		panic(err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		panic(err)
	}
	profile = f
}

func finish() {
	pprof.StopCPUProfile()
	profile.Close()
}
