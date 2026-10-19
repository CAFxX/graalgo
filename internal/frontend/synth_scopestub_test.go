package main

// P1-11 (RV22): compile-time fail-fast for single/zero-result scope-gap
// stubs.
//
// A synthetic function whose body is exactly
// panic("... not in M{5,7,8,9} scope ...") with at most one result is
// declared with an exact gc signature for type-checking but is
// unimplemented in the current milestone. User references — calls and
// value positions — must fail at compile time with the stub's own
// message (milestone + re-test trigger), not compile and panic at
// runtime.
//
// Representative package: sort.
//   - sort.Slice / sort.SliceStable (0 results) and sort.SliceIsSorted
//     (1 result) are scope-gap stubs: references fail fast.
//   - sort.Find (multi-result stub) stays on the Appendix-M machinery
//     (desugar tables + isMultiResultSyntheticFailFast): the new
//     classifier must NOT claim it (non-interference, pinned here).
//   - sort.SearchInts (real body) still compiles (no false positive).
// Faithful gc-mirror panics (inside real bodies) are preserved:
//   - base32.NewEncoding's alphabet-validation panics are real gc
//     behavior — the classifier must not claim it.
//   - base32's single-result stub (encoder.Close) is claimed.

import (
	"go/types"
	"strings"
	"testing"
)

// scopeStubNames returns the function names recorded in the synthetic
// package's scope-stub manifest.
func scopeStubNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	sp := getSyntheticPackage(path)
	out := map[string]bool{}
	for fn := range sp.scopeStubs {
		out[fn.Name()] = true
	}
	return out
}

// lookupSyntheticFunc resolves a package-level synthetic function by
// name for scopeGapStub probing.
func lookupSyntheticFunc(t *testing.T, path, name string) *types.Func {
	t.Helper()
	sp := getSyntheticPackage(path)
	obj := sp.pkg.Scope().Lookup(name)
	fn, ok := obj.(*types.Func)
	if !ok || fn == nil {
		t.Fatalf("synthetic %s has no package-level func %q", path, name)
	}
	return fn
}

func TestScopeStubManifestSort(t *testing.T) {
	names := scopeStubNames(t, "sort")
	// The three single/zero-result stubs are claimed...
	for _, want := range []string{"Slice", "SliceStable", "SliceIsSorted"} {
		if !names[want] {
			t.Errorf("sort scope-stub manifest missing %q", want)
		}
	}
	// ...the multi-result stub is NOT (Appendix-M machinery owns it)...
	if names["Find"] {
		t.Errorf("sort scope-stub manifest must not claim multi-result Find")
	}
	// ...and real implementations are not stubs.
	for _, real := range []string{"SearchInts", "Sort", "IntsAreSorted"} {
		if names[real] {
			t.Errorf("sort scope-stub manifest must not claim real func %q", real)
		}
	}
}

func TestScopeStubManifestBase32(t *testing.T) {
	names := scopeStubNames(t, "encoding/base32")
	// encoder.Close is the single-result scope stub (method names are
	// recorded under their declared name).
	if !names["Close"] {
		t.Errorf("base32 scope-stub manifest missing method Close")
	}
	// NewEncoding has a real body (its alphabet-validation panics are
	// faithful gc mirrors) — must be preserved, not claimed.
	if names["NewEncoding"] {
		t.Errorf("base32 scope-stub manifest must not claim NewEncoding (faithful gc-mirror panics)")
	}
	// Multi-result members stay on the Appendix-M machinery.
	for _, mr := range []string{"Decode", "DecodeString", "AppendDecode"} {
		if names[mr] {
			t.Errorf("base32 scope-stub manifest must not claim multi-result %q", mr)
		}
	}
}

func TestScopeStubManifestMessages(t *testing.T) {
	// The recorded message is the stub's own: milestone + re-test
	// trigger, reused verbatim as the compile-time error.
	sp := getSyntheticPackage("sort")
	for fn, msg := range sp.scopeStubs {
		if !strings.Contains(msg, "M7 scope") {
			t.Errorf("%s: manifest message %q lacks the M7 milestone tag", fn.Name(), msg)
		}
		if !strings.Contains(msg, "re-test") {
			t.Errorf("%s: manifest message %q lacks the re-test trigger", fn.Name(), msg)
		}
		if !strings.Contains(msg, "sort."+fn.Name()) {
			t.Errorf("%s: manifest message %q does not name the function", fn.Name(), msg)
		}
	}
}

// exportScopeStubSrc exports src and returns the export error (nil on
// success).
func exportScopeStubSrc(t *testing.T, src string) error {
	t.Helper()
	_, err := ExportFile(writeIfaceProg(t, src))
	return err
}

func TestScopeStubCallFailsFast(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // function name expected in the error
	}{
		{"Slice", `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
`, "sort.Slice"},
		{"SliceStable", `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	sort.SliceStable(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
`, "sort.SliceStable"},
		{"SliceIsSortedExpr", `package main

import "sort"

func main() {
	xs := []int{1, 2, 3}
	println(sort.SliceIsSorted(xs, func(i, j int) bool { return xs[i] < xs[j] }))
}
`, "sort.SliceIsSorted"},
		{"SliceIsSortedIf", `package main

import "sort"

func main() {
	xs := []int{1, 2, 3}
	if sort.SliceIsSorted(xs, func(i, j int) bool { return xs[i] < xs[j] }) {
		println("sorted")
	}
}
`, "sort.SliceIsSorted"},
		{"SliceDefer", `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	defer sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
`, "sort.Slice"},
		{"SliceGo", `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	go sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
`, "sort.Slice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := exportScopeStubSrc(t, c.src)
			if err == nil {
				t.Fatalf("export succeeded for %s call, want compile-time scope error", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %s", err.Error(), c.want)
			}
			if !strings.Contains(err.Error(), "M7 scope") {
				t.Fatalf("error %q lacks the M7 scope attribution", err.Error())
			}
			if !strings.Contains(err.Error(), "re-test") {
				t.Fatalf("error %q lacks the re-test trigger", err.Error())
			}
		})
	}
}

func TestScopeStubValueFailsFast(t *testing.T) {
	// A scope-gap stub has no honest value form: forming the value
	// fails at compile time instead of emitting a reference to a panic
	// body.
	err := exportScopeStubSrc(t, `package main

import "sort"

func main() {
	f := sort.Slice
	_ = f
}
`)
	if err == nil {
		t.Fatal("export succeeded for sort.Slice in value position, want compile-time scope error")
	}
	if !strings.Contains(err.Error(), "sort.Slice") || !strings.Contains(err.Error(), "M7 scope") {
		t.Fatalf("unexpected error for sort.Slice value position: %v", err)
	}
}

func TestScopeStubRealFuncStillCompiles(t *testing.T) {
	// Real sort functions are unaffected: no false positives.
	err := exportScopeStubSrc(t, `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	println(sort.SearchInts(xs, 2))
	println(sort.IntsAreSorted(xs))
	sort.Sort(sort.IntSlice(xs))
	println(xs[0], xs[1], xs[2])
}
`)
	if err != nil {
		t.Fatalf("export failed for real sort functions: %v", err)
	}
}

func TestScopeStubFindNotClaimed(t *testing.T) {
	// sort.Find is a multi-result scope stub: the new classifier must
	// not claim it — the Appendix-M machinery (desugar tables +
	// isMultiResultSyntheticFailFast) owns multi-result members.
	// (That machinery is currently regressed by adc14e0f — the 8 red
	// tests triaged in the P1-11 design doc — so this test pins only
	// non-interference: whatever error surfaces must not come from
	// scopeGapStub.)
	e := &exporter{}
	fn := lookupSyntheticFunc(t, "sort", "Find")
	if msg, ok := e.scopeGapStub(fn); ok {
		t.Fatalf("scopeGapStub claimed multi-result sort.Find: %q", msg)
	}
	// And the direct unit check: single-result stubs are claimed.
	for _, name := range []string{"Slice", "SliceStable", "SliceIsSorted"} {
		fn := lookupSyntheticFunc(t, "sort", name)
		if msg, ok := e.scopeGapStub(fn); !ok {
			t.Fatalf("scopeGapStub did not claim sort.%s", name)
		} else if !strings.Contains(msg, "M7 scope") {
			t.Fatalf("sort.%s message %q lacks M7 scope", name, msg)
		}
	}
}
