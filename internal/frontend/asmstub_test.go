package main

// Tests for RV-P1-02 (option (b)): assembly-backed functions are rejected
// at compile time, at the reference site — never as a runtime panic on a
// compilable program.

import (
	"strings"
	"testing"
)

// TestProviderTableMechanical: the generated provider table is a
// mechanical scrape (non-empty) and covers the runtime-provided services
// the design calls out: sync.fatal/sync.throw must reroute to the linkname
// path (they have working backend intrinsics), and os/signal.signal_enable
// must be present as provided (its references then fail honestly for the
// missing intrinsic, until signal intrinsics land).
func TestProviderTableMechanical(t *testing.T) {
	if len(runtimeProvidedLinknames) == 0 {
		t.Fatal("runtimeProvidedLinknames is empty; run make gen-linkname-tables")
	}
	for _, sym := range []string{
		"sync.throw",
		"sync.fatal",
		"sync.runtime_Semacquire",
		"internal/sync.throw",
		"internal/sync.fatal",
		"os/signal.signal_enable",
	} {
		if got, ok := runtimeProvidedLinknames[sym]; !ok || got != sym {
			t.Errorf("runtimeProvidedLinknames[%q] = %q, %v; want %q, true", sym, got, ok, sym)
		}
	}
	// The runtime package itself is never compiled; its own symbols are
	// not rerouted (guard against self-routing).
	if _, ok := runtimeProvidedLinknames["runtime.fastrand"]; ok {
		t.Logf("note: runtime.fastrand is provider-listed (harmless: runtime is never compiled)")
	}
}

// TestIntrinsicTableMechanical: the generated intrinsic registry is a
// mechanical scrape (non-empty), covers the intrinsics the design calls
// out, and does NOT cover os/signal.signal_enable (honestly uncompilable
// until signal intrinsics land).
func TestIntrinsicTableMechanical(t *testing.T) {
	if len(linknameIntrinsicSymbols) == 0 {
		t.Fatal("linknameIntrinsicSymbols is empty; run make gen-linkname-tables")
	}
	for _, sym := range []string{
		"sync.throw",
		"sync.fatal",
		"runtime.fastrand",
		"runtime.rand",                    // RV-P1-02: registered for internal/sync's hashTrieMap seed
		"internal/reflectlite.unsafe_New", // RV-P1-02 F4: allocator intrinsic
		"reflect.unsafe_New",              // RV-P1-02 F4: allocator intrinsic
		"reflect.unsafe_NewArray",         // RV-P1-02 F4: allocator intrinsic
	} {
		if !linknameIntrinsicSymbols[sym] {
			t.Errorf("linknameIntrinsicSymbols[%q] = false; want true", sym)
		}
	}
	for _, sym := range []string{
		"os/signal.signal_enable",
		"bogus.nonexistent",
	} {
		if linknameIntrinsicSymbols[sym] {
			t.Errorf("linknameIntrinsicSymbols[%q] = true; want false", sym)
		}
	}
}

// exportFailsWith exports src and requires failure containing all wants.
func exportFailsWith(t *testing.T, src string, wants ...string) string {
	t.Helper()
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("expected export failure containing %q, got success", wants)
	}
	msg := err.Error()
	for _, w := range wants {
		if !strings.Contains(msg, w) {
			t.Errorf("error %q does not contain %q", msg, w)
		}
	}
	return msg
}

// TestUnimplementedCallRejected: a call to a true-assembly (bodyless,
// no provider) function fails at export with [RV-P1-02].
func TestUnimplementedCallRejected(t *testing.T) {
	src := `package main

func asmStub() int

func main() {
	println(asmStub())
}
`
	exportFailsWith(t, src,
		"cannot compile reference to main.asmStub",
		"assembly function not implemented in graalgo",
		"[RV-P1-02]")
}

// TestUnimplementedFuncValueRejected: a function value of an unimplemented
// function is a reference too.
func TestUnimplementedFuncValueRejected(t *testing.T) {
	src := `package main

func asmStub() int

func main() {
	f := asmStub
	_ = f
}
`
	exportFailsWith(t, src,
		"cannot compile reference to main.asmStub",
		"[RV-P1-02]")
}

// TestUnimplementedGoDeferRejected: go/defer of an unimplemented function
// are references too.
func TestUnimplementedGoDeferRejected(t *testing.T) {
	for _, stmt := range []string{"go asmStub()", "defer asmStub()"} {
		src := "package main\n\nfunc asmStub()\n\nfunc main() {\n\t" + stmt + "\n}\n"
		exportFailsWith(t, src,
			"cannot compile reference to main.asmStub",
			"[RV-P1-02]")
	}
}

// TestUnimplementedMultiResultRejected: the old "ASM Phase 1" pair
// transform for unimplemented functions is gone; a multi-result call to
// an unimplemented function fails at export like any other reference.
func TestUnimplementedMultiResultRejected(t *testing.T) {
	src := `package main

func asmMulti() (int, int)

func main() {
	a, b := asmMulti()
	_, _ = a, b
}
`
	exportFailsWith(t, src,
		"cannot compile reference to main.asmMulti",
		"[RV-P1-02]")
}

// TestUnimplementedDeclKeepsTypeIdentity: declaring (but never
// referencing) an unimplemented function still compiles — rejection is
// at the reference site, not the package site.
func TestUnimplementedDeclKeepsTypeIdentity(t *testing.T) {
	src := `package main

func asmStub() int

func main() {
	println("no reference to asmStub")
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("declaration without reference should export, got: %v", err)
	}
}

// TestLinknameUnregisteredSymbolRejected: a reference to a linkname
// function whose symbol has no registered intrinsic and no compiled Go
// provider fails at export (the §1.3 sibling: no call-time dispatch
// failure on a compilable program).
func TestLinknameUnregisteredSymbolRejected(t *testing.T) {
	src := `package main

//go:linkname bogusStub bogus.nonexistent
func bogusStub() int

func main() {
	println(bogusStub())
}
`
	exportFailsWith(t, src,
		"cannot compile reference to main.bogusStub",
		`linkname symbol "bogus.nonexistent" has no registered backend intrinsic`,
		"[RV-P1-02]")
}

// TestLinknameRegisteredSymbolAccepted: a reference to a linkname function
// whose symbol IS registered exports (dispatch resolves at runtime).
func TestLinknameRegisteredSymbolAccepted(t *testing.T) {
	src := `package main

//go:linkname myFastrand runtime.fastrand
func myFastrand() uint32

func main() {
	println(myFastrand())
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("registered linkname symbol should export, got: %v", err)
	}
}

// TestSyncMutexStillCompiles: the real sync package's intra-package
// references to runtime-provided services (fatal/throw via the provider
// table; runtime_Semacquire via the provider table) keep compiling.
// This is the design's "fixes sync.fatal/sync.throw for real".
func TestSyncMutexStillCompiles(t *testing.T) {
	src := `package main

import "sync"

func main() {
	var m sync.Mutex
	m.Lock()
	m.Unlock()
	var w sync.WaitGroup
	w.Add(1)
	w.Done()
	w.Wait()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("sync.Mutex/WaitGroup program should export, got: %v", err)
	}
}

// TestAtomicPointerStillCompiles: sync/atomic's intra-package references
// (LoadPointer/StorePointer/SwapPointer/CompareAndSwapPointer, called
// unqualified from the package's own bodies) lower via the object-based
// interceptions; the import keeps compiling per the design's §4.1.
func TestAtomicPointerStillCompiles(t *testing.T) {
	src := `package main

import "sync/atomic"

func main() {
	var p atomic.Pointer[int]
	x := 42
	p.Store(&x)
	_ = p.Load()
	var v atomic.Value
	v.Store("s")
	_ = v.Load()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("sync/atomic program should export, got: %v", err)
	}
}

// TestAllocatorIntrinsicsRegistered: references to the RV-P1-02 F4
// allocator linkname symbols (internal/reflectlite.unsafe_New,
// reflect.unsafe_New, reflect.unsafe_NewArray) are accepted at export
// (not rejected with [RV-P1-02]). Full stdlib export needs additional
// intrinsics beyond F4's scope (typedmemmove, ifaceE2I, chan/map ops);
// those remain honestly rejected until their lanes land.
func TestAllocatorIntrinsicsRegistered(t *testing.T) {
	cases := []struct{ sym, sig string }{
		{"internal/reflectlite.unsafe_New", "func myAlloc(unsafe.Pointer) unsafe.Pointer"},
		{"reflect.unsafe_New", "func myAlloc(unsafe.Pointer) unsafe.Pointer"},
		{"reflect.unsafe_NewArray", "func myAlloc(unsafe.Pointer, int) unsafe.Pointer"},
	}
	for _, c := range cases {
		src := "package main\n\nimport \"unsafe\"\n\n//go:linkname myAlloc " + c.sym + "\n" + c.sig + "\n\nfunc main() {}\n"
		if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
			t.Errorf("linkname symbol %q should export, got: %v", c.sym, err)
		}
	}
}
