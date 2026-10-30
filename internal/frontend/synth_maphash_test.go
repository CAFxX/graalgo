package main

// Review 2026-10-30 P1-15: hash/maphash WriteComparable panicked at
// runtime for any comparable type outside a hardcoded type switch —
// in particular named basic types (whose dynamic type never matches a
// case) and structs, the common map-key case — because gc hashes them
// via the runtime typehash. The fix routes those values through a
// structural reflect walk (hashReflectValue), so WriteComparable no
// longer panics for a comparable value — except where gc itself
// panics: an interface-held slice, map, or func (veto 2026-10-30 F1).
// These tests pin that:
//
//   - the synthetic source no longer contains the unsupported-type
//     panic and does contain the reflect fallback;
//   - a program calling WriteComparable with a struct, a named int
//     type, an array, an interface-held struct, and a pointer exports
//     without error (proving the reflect calls inside the generic
//     synthetic function are emittable).
//
// Runtime hash values are NOT pinned here (they differ from gc by
// design, per the synthetic package's documented VALUE DIVERGENCE);
// a JVM differential run must still confirm equal-values-hash-equally
// for the new shapes.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaphashWriteComparableNoPanicTrap(t *testing.T) {
	if strings.Contains(syntheticMaphashSource, "unsupported type (needs runtime typehash)") {
		t.Errorf("synthetic maphash source still contains the WriteComparable unsupported-type panic")
	}
	if !strings.Contains(syntheticMaphashSource, "hashReflectValue(h, reflect.ValueOf(x))") {
		t.Errorf("synthetic maphash source missing the reflect fallback call in WriteComparable")
	}
	if !strings.Contains(syntheticMaphashSource, "func hashReflectValue(h *Hash, v reflect.Value)") {
		t.Errorf("synthetic maphash source missing hashReflectValue")
	}
}

func TestMaphashWriteComparableExports(t *testing.T) {
	src := `package main

import "hash/maphash"

type MyInt int

type Key struct {
	a int
	b string
}

func main() {
	var h maphash.Hash
	h.SetSeed(maphash.MakeSeed())
	maphash.WriteComparable(&h, Key{a: 1, b: "x"})
	maphash.WriteComparable(&h, MyInt(7))
	maphash.WriteComparable(&h, [2]string{"a", ""})
	maphash.WriteComparable(&h, any(Key{a: 2, b: "y"}))
	n := 3
	maphash.WriteComparable(&h, &n)
	_ = h.Sum64()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("WriteComparable program should export, got: %v", err)
	}
}

// Veto 2026-10-30 F1/F2: behavioral tests for the synthetic maphash
// source itself. The source cannot run under gc unmodified — its
// Write/WriteString are fail-fast stubs served via the mrM8 desugar
// under graalgo — so the harness applies exactly the adaptation the
// desugar performs to a COPY of the source (h.Write -> h.WriteSingle,
// h.WriteString -> h.WriteStringSingle) and runs a probe program under
// gc. The adaptation is asserted, not assumed: every desugar-served
// call site must be rewritten and the stub definitions must survive
// untouched, so a future source change that moves those call sites
// fails loudly here instead of silently probing stale code. The probe
// exercises only pure-Go logic (the reflect walk, the float arms),
// which is runtime-independent; the desugar-served stubs themselves
// are covered by the export test above and the wave-3 corpus.
func runSyntheticMaphashProbe(t *testing.T, mode string) (string, error) {
	t.Helper()
	src := syntheticMaphashSource
	if strings.Count(src, "h.Write(") == 0 || strings.Count(src, "h.WriteString(") == 0 {
		t.Fatalf("synthetic maphash source lost its desugar-served call sites")
	}
	adapted := strings.ReplaceAll(src, "h.Write(", "h.WriteSingle(")
	adapted = strings.ReplaceAll(adapted, "h.WriteString(", "h.WriteStringSingle(")
	if strings.Contains(adapted, "h.Write(") || strings.Contains(adapted, "h.WriteString(") {
		t.Fatalf("adaptation left unrewritten desugar-served call sites")
	}
	if !strings.Contains(adapted, "func (h *Hash) Write(b []byte) (int, error)") ||
		!strings.Contains(adapted, "func (h *Hash) WriteString(s string) (int, error)") {
		t.Fatalf("adaptation clobbered the Write/WriteString stub definitions")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "maphash"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module mhprobe\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "maphash", "maphash.go"), []byte(adapted), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(maphashProbeMain), 0644); err != nil {
		t.Fatal(err)
	}
	goTool := filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")
	if _, err := os.Stat(goTool); err != nil {
		t.Fatalf("go toolchain not found at %s", goTool)
	}
	cmd := exec.Command(goTool, "run", ".", mode)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

const maphashProbeMain = `package main

import (
	"fmt"
	"math"
	"mhprobe/maphash"
	"os"
)

type keyAny struct{ V any }
type myFloat float64

func tryCase(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("%s PANIC: %v\n", name, r)
		} else {
			fmt.Printf("%s NO PANIC\n", name)
		}
	}()
	f()
}

func runPanics() {
	tryCase("slice", func() {
		var h maphash.Hash
		h.SetSeed(maphash.MakeSeed())
		maphash.WriteComparable(&h, keyAny{V: []int{1, 2}})
	})
	tryCase("map", func() {
		var h maphash.Hash
		h.SetSeed(maphash.MakeSeed())
		maphash.WriteComparable(&h, keyAny{V: map[string]int{"a": 1}})
	})
	tryCase("func", func() {
		var h maphash.Hash
		h.SetSeed(maphash.MakeSeed())
		maphash.WriteComparable(&h, keyAny{V: func() {}})
	})
	tryCase("cyclic", func() {
		var h maphash.Hash
		h.SetSeed(maphash.MakeSeed())
		// True self-referential cycle: x[0] holds x itself (an
		// append(x, x) construction would capture the pre-append
		// empty header instead and would not cycle).
		x := make([]any, 1)
		x[0] = x
		maphash.WriteComparable(&h, keyAny{V: x})
	})
}

func runNegzero() {
	seed := maphash.MakeSeed()
	eq := func(a, b func(h *maphash.Hash)) bool {
		var h maphash.Hash
		h.SetSeed(seed)
		a(&h)
		s1 := h.Sum64()
		h.Reset()
		b(&h)
		return s1 == h.Sum64()
	}
	check := func(name string, ok bool) {
		if ok {
			fmt.Printf("%s PASS\n", name)
		} else {
			fmt.Printf("%s FAIL\n", name)
		}
	}
	negZero64 := math.Copysign(0, -1)
	negZero32 := float32(math.Copysign(0, -1))
	check("float64", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, negZero64) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, 0.0) }))
	check("float32", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, negZero32) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, float32(0)) }))
	check("named", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, myFloat(negZero64)) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, myFloat(0)) }))
	check("structfield", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, struct{ F float64 }{F: negZero64}) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, struct{ F float64 }{F: 0}) }))
	check("complex128real", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex(negZero64, 1)) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex(0, 1)) }))
	check("complex128imag", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex(1, negZero64)) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex(1, 0)) }))
	check("complex64real", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex64(complex(negZero32, 1))) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex64(complex(float32(0), 1))) }))
	check("complex64imag", eq(
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex64(complex(1, negZero32))) },
		func(h *maphash.Hash) { maphash.WriteComparable(h, complex64(complex(1, float32(0)))) }))
}

func main() {
	switch os.Args[1] {
	case "panics":
		runPanics()
	case "negzero":
		runNegzero()
	default:
		fmt.Println("unknown mode")
		os.Exit(2)
	}
}
`

// Veto 2026-10-30 F1: interface-held slices, maps, and funcs must panic
// loudly with gc's message — never silently hash by identity, and never
// recurse unboundedly on a cyclic value (which would be a fatal stack
// overflow, not a panic). On the pre-clearance code these cases print
// NO PANIC (slice/map/func) or crash the probe (cyclic), so this test
// fails there.
func TestMaphashUnhashableInterfaceValuesPanic(t *testing.T) {
	out, err := runSyntheticMaphashProbe(t, "panics")
	if err != nil {
		t.Errorf("probe exited abnormally: %v\n%s", err, out)
	}
	for _, want := range []string{
		"slice PANIC: hash of unhashable type []int",
		"map PANIC: hash of unhashable type map[string]int",
		"func PANIC: hash of unhashable type func()",
		// reflect spells []any as []interface {}.
		"cyclic PANIC: hash of unhashable type []interface {}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in probe output:\n%s", want, out)
		}
	}
}

// Veto 2026-10-30 F2: -0.0 and +0.0 are equal in Go, so they must hash
// equally — scalar (fast path), named type, struct field (reflect
// path), and each complex component. On the pre-clearance code every
// case prints FAIL, so this test fails there.
func TestMaphashNegativeZeroHashesEqual(t *testing.T) {
	out, err := runSyntheticMaphashProbe(t, "negzero")
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
	for _, name := range []string{"float64", "float32", "named", "structfield", "complex128real", "complex128imag", "complex64real", "complex64imag"} {
		if !strings.Contains(out, name+" PASS") {
			t.Errorf("missing %s PASS in probe output:\n%s", name, out)
		}
	}
}
