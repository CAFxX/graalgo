package main

// Tests for body-less multi-result function handling in the disk
// multi-result transform (transformDiskMultiResult in mr_disk.go).
//
// Body-less declarations (e.g. //go:noescape assembly stubs like
// reflect's chanrecv) have no Go body to rewrite, but their call sites
// still need desugaring. The transform must generate:
//   1. The pair type (so the __-helper return type exists)
//   2. The stub (for type-checking the original signature)
//   3. A __-helper: linkname-bound when the declaration carries a
//      //go:linkname binding (the backend intrinsic serves it), or a
//      fail-fast panic body marked unbound (P1-07) when no binding
//      exists — direct calls to an unbound helper are compile-time
//      errors (multiResultHelper), never user-runtime panics.
//
// Without this, the call site `selected, ok := chanrecv(...)` is not
// desugared, and the v1 frontend (which cannot emit multi-result calls)
// fails.
//
// Red-green: before the fix, transformDiskMultiResult skipped body-less
// functions (fd.Body == nil), so no spec was generated and the call site
// was not desugared.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestDiskMRBodylessMultiResult: a body-less multi-result function
// declaration gets a spec (pair type + helper + stub).
func TestDiskMRBodylessMultiResult(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	s := specs[0]
	if s.name != "chanrecv" {
		t.Errorf("spec name = %q, want %q", s.name, "chanrecv")
	}
	if s.pairType != "__chanrecvPair" {
		t.Errorf("pairType = %q, want %q", s.pairType, "__chanrecvPair")
	}
	if s.helper != "__chanrecv" {
		t.Errorf("helper = %q, want %q", s.helper, "__chanrecv")
	}
	if len(s.pairFields) != 2 {
		t.Errorf("pairFields len = %d, want 2", len(s.pairFields))
	}
}

// TestDiskMRBodylessTypeChecks: the rewritten AST (pair + panic helper +
// stub) type-checks cleanly.
func TestDiskMRBodylessTypeChecks(t *testing.T) {
	checkDiskMRClean(t, `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`)
}

// TestDiskMRBodylessCallSiteDesugared: a call to a body-less multi-result
// function is registered for desugaring (lookupDiskMR finds it).
func TestDiskMRBodylessCallSiteDesugared(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	// Clear any existing specs for this package to avoid interference.
	diskMRMu.Lock()
	oldSpecs := diskMRSpecs
	diskMRSpecs = nil
	diskMRMu.Unlock()
	defer func() {
		diskMRMu.Lock()
		diskMRSpecs = oldSpecs
		diskMRMu.Unlock()
	}()

	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	diskMRMu.Lock()
	diskMRSpecs = append(diskMRSpecs, specs...)
	diskMRMu.Unlock()

	spec := lookupDiskMR("example.com/p", "chanrecv", "")
	if spec == nil {
		t.Fatal("lookupDiskMR(example.com/p, chanrecv, \"\") = nil, want spec")
	}
	if spec.helper != "__chanrecv" {
		t.Errorf("spec helper = %q, want __chanrecv", spec.helper)
	}
}

// TestDiskMRBodylessNegative: single-result body-less functions are NOT
// transformed (no spec generated).
func TestDiskMRBodylessNegative(t *testing.T) {
	src := `package p

import "unsafe"

//go:noescape
func chansend0(ch unsafe.Pointer, val unsafe.Pointer, nb bool) bool
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
	if len(specs) != 0 {
		t.Fatalf("got %d specs for single-result body-less func, want 0", len(specs))
	}
}

// P1-07: a body-less multi-result declaration is unbound exactly when
// its __-helper gets the panic body — no //go:linkname binding (or a
// generic declaration, whose linkname binding is out of M4 scope).
// Linkname-bound helpers are bound; bodied functions are never unbound.
func TestDiskMRUnboundFlag(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "bodyless-no-directive",
			src: `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`,
			want: true,
		},
		{
			name: "bodyless-linkname",
			src: `package p

import "unsafe"

//go:linkname chanrecv runtime.chanrecv
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`,
			want: false,
		},
		{
			name: "bodyless-linknamestd",
			src: `package p

import "unsafe"

//go:linknamestd chanrecv
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`,
			want: false,
		},
		{
			name: "bodyless-generic-linkname",
			src: `package p

//go:linkname f runtime.f
func f[T any](x T) (T, bool)
`,
			want: true,
		},
		{
			name: "bodied",
			src: `package p

func f(a int) (int, bool) {
	return a, true
}
`,
			want: false,
		},
		{
			name: "bodyless-method-no-directive",
			src: `package p

type T struct{}

//go:noescape
func (t *T) M() (int, bool)
`,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			// ParseComments: the //go:linkname directive must survive
			// as the FuncDecl's Doc for mrLinknameSymbol to see it.
			f, err := parser.ParseFile(fset, "p.go", tc.src, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
			if len(specs) != 1 {
				t.Fatalf("got %d specs, want 1", len(specs))
			}
			if specs[0].unbound != tc.want {
				t.Errorf("spec.unbound = %v, want %v (helper %q)", specs[0].unbound, tc.want, specs[0].helper)
			}
		})
	}
}

// P1-07: resolving the __-helper for a call to an unbound EXPORTED
// body-less function is a compile-time error naming the function —
// not a call node to the panic body. An unexported unbound callee
// (the internal/cpu.cpuid precedent) passes the guard, as does a
// bound (bodied) helper, which proceeds to normal resolution (which
// fails here only because the test package is not a real disk
// package).
func TestDiskMRUnboundHelperCallErrors(t *testing.T) {
	swapSpecs := func(specs []diskMRSpec) func() {
		diskMRMu.Lock()
		old := diskMRSpecs
		diskMRSpecs = append([]diskMRSpec(nil), specs...)
		diskMRMu.Unlock()
		return func() {
			diskMRMu.Lock()
			diskMRSpecs = old
			diskMRMu.Unlock()
		}
	}
	parse := func(t *testing.T, src string) *ast.File {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		return f
	}

	t.Run("unbound-exported", func(t *testing.T) {
		f := parse(t, `package p

//go:noescape
func Foo(a int) (int, bool)
`)
		specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
		if len(specs) != 1 || !specs[0].unbound {
			t.Fatalf("want 1 unbound spec, got %+v", specs)
		}
		defer swapSpecs(specs)()
		plan := &mrPlan{kind: mrM7, helperPkg: "example.com/p", helperName: "__Foo", isDisk: true}
		_, err := (&exporter{}).multiResultHelper(plan)
		if err == nil {
			t.Fatal("multiResultHelper(unbound exported) = nil error, want compile-time error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "Foo") || !strings.Contains(msg, "backend intrinsic") {
			t.Errorf("error %q does not name the function and the missing intrinsic", msg)
		}
	})

	// The guard fires only for exported names: an unexported unbound
	// callee (the internal/cpu.cpuid precedent — user code cannot
	// name it, and the runtime never calls the entry points that
	// would reach it) passes the guard and proceeds to normal
	// resolution, which fails here only because the test package is
	// not a real disk package.
	t.Run("unexported-passes-guard", func(t *testing.T) {
		f := parse(t, `package p

import "unsafe"

//go:noescape
func chanrecv(ch unsafe.Pointer, nb bool, val unsafe.Pointer) (selected, received bool)
`)
		specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
		if len(specs) != 1 || !specs[0].unbound {
			t.Fatalf("want 1 unbound spec, got %+v", specs)
		}
		defer swapSpecs(specs)()
		plan := &mrPlan{kind: mrM7, helperPkg: "example.com/p", helperName: "__chanrecv", isDisk: true}
		_, err := (&exporter{}).multiResultHelper(plan)
		if err == nil {
			t.Fatal("multiResultHelper(unexported, fake package) = nil error, want disk-resolution error")
		}
		if strings.Contains(err.Error(), "backend intrinsic") || strings.Contains(err.Error(), "P1-07") {
			t.Errorf("unexported helper wrongly hit the unbound guard: %v", err)
		}
	})

	t.Run("bound-passes-guard", func(t *testing.T) {
		f := parse(t, `package p

func f(a int) (int, bool) {
	return a, true
}
`)
		specs := transformDiskMultiResult("example.com/p", []*ast.File{f})
		if len(specs) != 1 || specs[0].unbound {
			t.Fatalf("want 1 bound spec, got %+v", specs)
		}
		defer swapSpecs(specs)()
		plan := &mrPlan{kind: mrM7, helperPkg: "example.com/p", helperName: "__f", isDisk: true}
		_, err := (&exporter{}).multiResultHelper(plan)
		if err == nil {
			t.Fatal("multiResultHelper(bound, fake package) = nil error, want disk-resolution error")
		}
		if strings.Contains(err.Error(), "backend intrinsic") || strings.Contains(err.Error(), "P1-07") {
			t.Errorf("bound helper wrongly hit the unbound guard: %v", err)
		}
	})
}
