package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSyntheticErrorsSortM7 verifies the P3-M7 wave-1 synthetic packages:
// a program importing "errors" and "sort" exports cleanly; the real-Go
// declarations become funcdecls in the wire doc; the multi-result
// members (sort.Find, errors.AsType) are never emitted as raw
// multi-result funcdecls — Find is pair-transformed via the generic MR
// machinery, AsType desugars through its m7 table entry (__asTypePub)
// with the stub skipped — and uncovered call sites fail fast (WS D);
// the fail-fast single-result member (errors.As) IS exported (its panic
// body runs if called), as are the now-real sort.Slice family members.
//
// Like TestSyntheticBytesM7, the multi-result check is scoped to the
// owning package AND to result count: bare names false-positive on
// transitive imports, and pair transformation legitimately leaves
// single-result (pair) funcdecls behind under the same names.
func TestSyntheticErrorsSortM7(t *testing.T) {
	src := `package main

import (
	"errors"
	"sort"
)

type revInts struct{ xs []int }

func (r revInts) Len() int           { return len(r.xs) }
func (r revInts) Less(i, j int) bool { return r.xs[i] > r.xs[j] }
func (r revInts) Swap(i, j int)      { r.xs[i], r.xs[j] = r.xs[j], r.xs[i] }

func main() {
	err1 := errors.New("boom")
	err2 := errors.New("bang")
	// P3-M9: variadic argument packing is now implemented; the spread
	// form is used here to also cover that path.
	errs := []error{err1, nil, err2}
	joined := errors.Join(errs...)
	println(joined.Error())
	println(errors.Is(joined, err1))
	println(errors.Unwrap(err1) == nil)
	println(errors.ErrUnsupported.Error())
	xs := []int{3, 1, 2}
	sort.Sort(sort.IntSlice(xs))
	println(xs[0], xs[1], xs[2])
	println(sort.SearchInts(xs, 2))
	println(sort.IntsAreSorted(xs))
	ys := []float64{3.5, 1.5}
	sort.Float64s(ys)
	println(ys[0] < ys[1])
	sort.Sort(revInts{xs: []int{1, 2, 3}})
	println(sort.IsSorted(sort.Reverse(sort.IntSlice([]int{1, 2, 3}))))
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synerrsort.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	// pkgResults records, per owning package and funcdecl name, the
	// largest result count seen, so the multi-result check below can
	// scope to the owning package.
	pkgResults := map[string]map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
						pp, _ := f["pkgpath"].(string)
						nr := 0
						if rs, ok := f["results"].([]any); ok {
							nr = len(rs)
						}
						if pkgResults[pp] == nil {
							pkgResults[pp] = map[string]int{}
						}
						if nr > pkgResults[pp][n] {
							pkgResults[pp][n] = nr
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	// Real-Go declarations must be exported as funcdecls. (Wire-doc
	// funcdecl names are bare — no package qualifier.)
	for _, want := range []string{
		"New", "Is", "Unwrap", "Join",
		"Sort", "Stable", "IsSorted", "Reverse",
		"Search", "SearchInts", "Ints", "IntsAreSorted",
		"Float64s",
	} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// Fail-fast single-result member (errors.As, panic body) and the
	// real sort.Slice family members are exported.
	for _, want := range []string{"As", "Slice", "SliceStable", "SliceIsSorted"} {
		if !names[want] {
			t.Errorf("wire doc missing funcdecl %q", want)
		}
	}
	// No multi-result member of errors/sort may leak as a raw
	// multi-result funcdecl in its owning package: Find is
	// pair-transformed (1 pair result) and AsType's stub is skipped.
	for _, c := range []struct {
		pkg, name string
	}{
		{"sort", "Find"},
		{"errors", "AsType"},
	} {
		if nr := pkgResults[c.pkg][c.name]; nr > 1 {
			t.Errorf("wire doc contains raw multi-result %s funcdecl %q (%d results)", c.pkg, c.name, nr)
		}
	}
}

// TestSyntheticSortFindSucceeds verifies a user call to sort.Find
// exports cleanly. Find used to be a fail-fast stub (its single-panic
// body blocked pair-transform registration, so every call site failed
// with the WS D scope error); it is now the real gc binary search,
// pair-transformed by the generic MR machinery. Export success is the
// regression signal: the old code failed ExportFile here. The wire doc
// must carry Find as a single-result (pair) funcdecl in the sort
// package, never as a raw two-result funcdecl.
func TestSyntheticSortFindSucceeds(t *testing.T) {
	src := `package main

import "sort"

func main() {
	i, found := sort.Find(10, func(i int) int { return 5 - i })
	println(i, found)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synfind.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile for sort.Find call: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	pkgResults := map[string]map[string]int{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						pp, _ := f["pkgpath"].(string)
						nr := 0
						if rs, ok := f["results"].([]any); ok {
							nr = len(rs)
						}
						if pkgResults[pp] == nil {
							pkgResults[pp] = map[string]int{}
						}
						if nr > pkgResults[pp][n] {
							pkgResults[pp][n] = nr
						}
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	if nr := pkgResults["sort"]["Find"]; nr != 1 {
		t.Errorf("wire doc sort.Find has %d results, want 1 (pair-transformed)", nr)
	}
}

// TestSyntheticSortSliceSucceeds verifies user calls to the sort.Slice
// family export cleanly. Slice/SliceStable/SliceIsSorted used to be
// fail-fast stubs ("re-test when reflect lands"); they are now real
// implementations on the reflectlite Swapper path. Export success is
// the regression signal for the call-site wiring; runtime behavior is
// covered by the wave-1 sort corpus (so10_slice_find.go).
func TestSyntheticSortSliceSucceeds(t *testing.T) {
	src := `package main

import "sort"

func main() {
	xs := []int{3, 1, 2}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	println(xs[0], xs[1], xs[2])
	println(sort.SliceIsSorted(xs, func(i, j int) bool { return xs[i] < xs[j] }))
	ys := []string{"b", "a"}
	sort.SliceStable(ys, func(i, j int) bool { return ys[i] < ys[j] })
	println(ys[0], ys[1])
	// A struct-slice call must export too: the frontend accepts every
	// element kind (only the backend Swapper's runtime support is
	// kind-narrow — see the so10 corpus note). Export, not runtime,
	// is what this test pins.
	type kv struct {
		key int
		val string
	}
	kvs := []kv{{2, "a"}, {1, "b"}}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].key < kvs[j].key })
	println(kvs[0].key)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synslice.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportFile(p); err != nil {
		t.Fatalf("ExportFile for sort.Slice family calls: %v", err)
	}
}
