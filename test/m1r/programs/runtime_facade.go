package main

import (
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	_ "unsafe"
)

//go:linkname nanotime runtime.nanotime
func nanotime() int64

// The legacy generators code outside std pulls (M3 gap G2).
//
//go:linkname fastrand runtime.fastrand
func fastrand() uint32

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

//go:linkname fastrand64 runtime.fastrand64
func fastrand64() uint64

func classify(f func()) {
	defer func() {
		r := recover()
		re, isRuntime := r.(runtime.Error)
		err, isError := r.(error)
		switch {
		case isRuntime:
			println("runtime.Error:", re.Error())
		case isError:
			println("error:", err.Error())
		default:
			println("not an error")
		}
	}()
	f()
}

type myErr struct{}

func (myErr) Error() string { return "mine" }

func main() {
	println(runtime.GOOS, runtime.GOARCH, runtime.Compiler)
	println(runtime.GOMAXPROCS(0) >= 1, runtime.NumCPU() >= 1)
	prev := runtime.GOMAXPROCS(2)
	println(runtime.GOMAXPROCS(prev) == 2, runtime.GOMAXPROCS(-1) == prev)

	t0 := nanotime()
	done := make(chan int)
	turns := 0
	go func() {
		for i := 0; i < 5; i++ {
			turns++
			runtime.Gosched()
		}
		done <- turns
	}()
	for i := 0; i < 5; i++ {
		runtime.Gosched()
	}
	println(<-done)
	runtime.GC()
	println(nanotime() >= t0)

	var p *int
	var m map[string]int
	var s []int
	zero := 0
	classify(func() { _ = *p })
	classify(func() { m["a"] = 1 })
	classify(func() { _ = s[3] })
	classify(func() { _ = 1 / zero })
	classify(func() { bits.Div64(1, 0, 0) })
	classify(func() { bits.Div64(9, 0, 3) })
	classify(func() { panic(myErr{}) })
	classify(func() { panic("text") })
	classify(func() { var e error; _ = e.(myErr) })

	// The values math/bits panics with are the runtime's own.
	var first, second any
	func() {
		defer func() { first = recover() }()
		bits.Div64(9, 0, 3)
	}()
	func() {
		defer func() { second = recover() }()
		bits.Div32(9, 0, 3)
	}()
	println(first == second, first != nil)

	surface()
}

// surface: the runtime API M3's gap G2 adds to the facade — memory
// statistics (JVM figures under gc's invariants), the runtime/debug knobs
// (gc's defaults, previous values returned), profiles, thread wiring,
// Clearenv, the legacy random generators, GOROOT and the build info.
func surface() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	println("memstats", ms.HeapAlloc > 0, ms.Alloc == ms.HeapAlloc, ms.Sys >= ms.HeapSys, ms.HeapSys >= ms.HeapInuse,
		ms.TotalAlloc >= ms.HeapAlloc, ms.EnableGC)
	forced := ms.NumForcedGC
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&ms)
	println("gc", ms.NumForcedGC-forced, ms.NumGC >= 2, ms.LastGC > 0, ms.GCCPUFraction >= 0 && ms.GCCPUFraction <= 1)
	var st debug.GCStats
	debug.ReadGCStats(&st)
	println("gcstats", st.NumGC >= 2, len(st.Pause) > 0, !st.LastGC.IsZero())
	debug.FreeOSMemory()

	println("debug", debug.SetGCPercent(50), debug.SetGCPercent(100), debug.SetMaxStack(1<<20), debug.SetMaxStack(1000000000),
		debug.SetMaxThreads(5000), debug.SetMaxThreads(10000), debug.SetMemoryLimit(-1))
	println("panic on fault", debug.SetPanicOnFault(true), debug.SetPanicOnFault(false))
	println("mutex profile", runtime.SetMutexProfileFraction(-1), runtime.SetMutexProfileFraction(5), runtime.SetMutexProfileFraction(0))
	n, ok := runtime.GoroutineProfile(nil)
	println("goroutine profile", n >= 1, ok)
	println("cgo", runtime.NumCgoCall())

	runtime.LockOSThread()
	runtime.UnlockOSThread()
	var pin runtime.Pinner
	x := new(int)
	pin.Pin(x)
	pin.Unpin()
	prev := runtime.GOMAXPROCS(0)
	runtime.SetDefaultGOMAXPROCS()
	println("default gomaxprocs", runtime.GOMAXPROCS(prev) >= 1)

	os.Setenv("GRAALGO_PROBE", "1")
	syscall.Clearenv()
	println("clearenv", os.Getenv("GRAALGO_PROBE") == "", len(os.Environ()))

	ok = true
	for range 1000 {
		ok = ok && fastrandn(10) < 10 && fastrandn(1) == 0
	}
	a, b := fastrand(), fastrand64()
	println("fastrand", ok, a != 0 || b != 0)

	// What the link records (cmd/link's own settings): the toolchain's root
	// and the module build info.
	_, err := os.Stat(filepath.Join(runtime.GOROOT(), "src", "runtime"))
	println("goroot", err == nil)
	bi, ok := debug.ReadBuildInfo()
	println("buildinfo", ok, bi.Path, bi.Main.Path, bi.GoVersion == runtime.Version())
}
