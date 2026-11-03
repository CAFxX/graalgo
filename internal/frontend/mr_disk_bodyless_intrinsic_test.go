package main

// P1-4 (REVIEW-2026-11-03-09): bodyless / linkname multi-result
// functions in disk packages.
//
// A bodyless multi-result declaration with no //go:linkname directive
// has no provider anywhere: no Go body, no linkname symbol for
// LinknameRootNode dispatch, no assembly (graalgo processes none).
// transformDiskMultiResult still generates its __-helper with a panic
// body (the declaration must exist for type-checking and for the
// library-internal call sites that the eager exporter emits), but a
// call to it from the compiled program's own code must fail at export
// time with a named scope error — gc's link error, not a shipped
// program that panics only when the call executes.
//
// A bodyless declaration WITH a //go:linkname directive is genuinely
// bound: its __-helper is emitted bodyless with a synthesized directive
// and binds through the existing linkname path, so calls to it must
// keep exporting.
//
// Scope note (measured against internal/cpu.cpuid): the refusal
// applies only to calls in the program's own code. Library bodies are
// exported eagerly (every funcdecl of every imported package), so an
// unbound call inside a library body may never execute (graalgo never
// calls internal/cpu.Initialize, so cpuid's panic helper is dead code
// in every program importing reflect). Failing those would break every
// importer of such a library. That library-internal case, and interface
// dispatch to an unbound concrete method (which resolves through the
// itab, not the desugar helper), remain open until the backend gains
// those intrinsics or the exporter gains reachability pruning.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiskMRBodylessUnboundFlag: the transform marks a bodyless
// multi-result declaration with no linkname directive as unbound, and
// a linkname-bound one as bound.
func TestDiskMRBodylessUnboundFlag(t *testing.T) {
	unbound := `package p

//go:noescape
func Unbound(a int) (int, error)
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", unbound, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs := transformDiskMultiResult("example.com/p14flag", []*ast.File{f})
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	if !specs[0].bodylessUnbound {
		t.Errorf("Unbound spec bodylessUnbound = false, want true")
	}

	bound := `package p

//go:linkname Bound example.com/p14flag.BoundProvider
func Bound(a int) (int, error)

func BoundProvider(a int) (int, error) { return a, nil }
`
	fset2 := token.NewFileSet()
	f2, err := parser.ParseFile(fset2, "p.go", bound, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	specs2 := transformDiskMultiResult("example.com/p14flag", []*ast.File{f2})
	var boundSpec *diskMRSpec
	for i := range specs2 {
		if specs2[i].name == "Bound" {
			boundSpec = &specs2[i]
		}
	}
	if boundSpec == nil {
		t.Fatalf("no spec for Bound (got %d specs)", len(specs2))
	}
	if boundSpec.bodylessUnbound {
		t.Errorf("Bound spec bodylessUnbound = true, want false (linkname-bound)")
	}
}

// writeP14DiskPackage writes a fake disk package under GOPATH/src and
// points GOPATH at it. The package carries all three shapes:
//   - Unbound: bodyless, no linkname — no provider anywhere.
//   - Bound: bodyless with //go:linkname to a real provider in the
//     same package — genuinely bound through the linkname path.
//   - Hello: an ordinary bodied function, so an importer that never
//     calls Unbound still exercises the package's export.
func writeP14DiskPackage(t *testing.T) {
	t.Helper()
	gopath := filepath.Join(t.TempDir(), "gopath")
	pkgDir := filepath.Join(gopath, "src", "example.com", "p14")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	src := `package p14

//go:noescape
func Unbound(a int) (int, error)

//go:linkname Bound example.com/p14.BoundProvider
func Bound(a int) (int, error)

func BoundProvider(a int) (int, error) { return a, nil }

func Hello() int { return 42 }
`
	if err := os.WriteFile(filepath.Join(pkgDir, "p14.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gopath)
}

func TestDiskMRBodylessUnboundCallFailsExport(t *testing.T) {
	writeP14DiskPackage(t)
	src := `package main

import "example.com/p14"

func main() {
	a, err := p14.Unbound(1)
	_, _ = a, err
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("ExportFile accepted a call to bodyless p14.Unbound; want a build-time failure, not a runnable program")
	}
	if !strings.Contains(err.Error(), "no Go body and no backend intrinsic") {
		t.Errorf("error does not name the missing intrinsic: %v", err)
	}
	if !strings.Contains(err.Error(), "p14.Unbound") {
		t.Errorf("error does not name p14.Unbound: %v", err)
	}
}

func TestDiskMRBodylessBoundCallStillExports(t *testing.T) {
	writeP14DiskPackage(t)
	src := `package main

import "example.com/p14"

func main() {
	a, err := p14.Bound(1)
	_, _ = a, err
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile rejected a call to linkname-bound p14.Bound: %v", err)
	}
	if !strings.Contains(out, "example.com/p14.BoundProvider") {
		t.Errorf("bound helper did not keep its linkname symbol in the export")
	}
}

func TestDiskMRBodylessUnboundImportStillExports(t *testing.T) {
	writeP14DiskPackage(t)
	// Importing the package and calling only its bodied function must
	// keep exporting: the unbound helper declaration is dead code for
	// this program (the reflect-import pattern), and failing here
	// would break every importer of a library that merely contains an
	// unbound bodyless declaration.
	src := `package main

import "example.com/p14"

func main() {
	_ = p14.Hello()
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile rejected an import of a package containing an uncalled unbound helper: %v", err)
	}
}
