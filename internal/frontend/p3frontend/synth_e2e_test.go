package p3frontend

// P3FE-3: end-to-end acceptance test for every synthetic package the
// Importer serves.
//
// The drift tests (bytes_test.go, json_test.go, sort_test.go, ...) call
// the synthetic* constructors directly and compare scope contents; only
// TestSyncExportDir exercises the real pipeline — and sync happens to be
// one of the packages WITH MarkComplete. That structural blindness is why
// two P0-class defects survived undetected:
//
//   - P3FE-1: eight synthetic packages never call MarkComplete. go/types'
//     importPackage (resolver.go) silently DROPS an import whose package
//     is neither complete nor fake, so a main package that imports one of
//     them fails type-checking with "undefined: <pkg>" — but only through
//     the real pipeline, never in the constructor-level drift tests.
//   - P3FE-2: syntheticJsonPackage builds its OWN bytes.Buffer named type
//     instead of sharing the importer's canonical one, so a program that
//     imports both bytes and encoding/json and passes a *bytes.Buffer to
//     json.Compact fails type-checking on the type-identity mismatch.
//
// This test closes the gap: for EACH synthetic package served by
// (*Importer).Import it builds a scratch main package that imports the
// path and uses one representative member through the real MergeDir
// pipeline (loader -> importer -> flattener), and asserts (1) the merge
// succeeds and (2) the member selector in the main program resolves to
// the exact object in the synthetic package's scope. The json case
// additionally asserts cross-package type identity (P3FE-2).
//
// Scope note: the table drives MergeDir, not ExportDir. ExportDir hands
// the merged source to the frozen gofrontend binary, whose lowering scope
// is frozen (only e.g. sync/runtime/fmt lower today); a scope rejection
// there is expected behavior both before and after these fixes, so it
// carries no signal for this test. TestSyncExportDir already covers the
// ExportDir path for sync.
//
// The served set is derived from (*Importer).Import's own switch by
// TestSyntheticE2ECoversAllServedPackages below, so adding a new
// synthetic package without a table entry fails loudly instead of
// silently widening the blind spot again.

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// synthE2ECase is one synthetic package's end-to-end probe.
type synthE2ECase struct {
	path   string // import path served by (*Importer).Import
	member string // representative exported member exercised by src; "" = blank import only
	// wantKind is the expected object class of member in the synthetic
	// scope: "func" (*types.Func), "type" (*types.TypeName),
	// "var" (*types.Var). "" skips the kind check.
	wantKind string
	// src is the scratch main package. It must import path and use
	// path.member (unless member == "").
	src string
	// check runs extra assertions with the importer and the synthetic
	// package (e.g. cross-package type identity). May be nil.
	check func(t *testing.T, imp *Importer, synPkg *types.Package)
}

var synthE2ECases = []synthE2ECase{
	{
		path:     "runtime",
		member:   "Goexit",
		wantKind: "func",
		src: `package main

import "runtime"

func main() {
	runtime.Goexit()
}
`,
	},
	{
		path:     "fmt",
		member:   "Sprint",
		wantKind: "func",
		src: `package main

import "fmt"

func main() {
	var _ = fmt.Sprint("x")
}
`,
	},
	{
		path:     "sync",
		member:   "Mutex",
		wantKind: "type",
		src: `package main

import "sync"

func main() {
	var m sync.Mutex
	m.Lock()
	m.Unlock()
}
`,
	},
	{
		path:     "sync/atomic",
		member:   "Int64",
		wantKind: "type",
		src: `package main

import "sync/atomic"

func main() {
	var v atomic.Int64
	v.Store(3)
	_ = v.Load()
}
`,
	},
	{
		path:     "time",
		member:   "Now",
		wantKind: "func",
		src: `package main

import "time"

func main() {
	var _ = time.Now()
}
`,
	},
	{
		path:     "context",
		member:   "Background",
		wantKind: "func",
		src: `package main

import "context"

func main() {
	var _ = context.Background()
}
`,
	},
	{
		path:     "errors",
		member:   "New",
		wantKind: "func",
		src: `package main

import "errors"

func main() {
	var _ error = errors.New("boom")
}
`,
	},
	{
		path:     "sort",
		member:   "Ints",
		wantKind: "func",
		src: `package main

import "sort"

func main() {
	sort.Ints([]int{3, 1, 2})
}
`,
	},
	{
		path:     "io",
		member:   "ReadAll",
		wantKind: "func",
		src: `package main

import "io"

func main() {
	var r io.Reader
	_, _ = io.ReadAll(r)
}
`,
	},
	{
		path:     "bytes",
		member:   "Buffer",
		wantKind: "type",
		src: `package main

import "bytes"

func main() {
	var b bytes.Buffer
	_, _ = b.Write([]byte("xy"))
}
`,
	},
	{
		path:     "strings",
		member:   "Contains",
		wantKind: "func",
		src: `package main

import "strings"

func main() {
	var _ = strings.Contains("abc", "b")
}
`,
	},
	{
		// unsafe's synthetic package is deliberately empty: it exists
		// only so `import _ "unsafe"` (required by //go:linkname
		// directives) type-checks. The probe is the blank import
		// itself; there is no member to resolve.
		path:   "unsafe",
		member: "",
		src: `package main

import _ "unsafe"

func main() {}
`,
	},
	{
		path:     "math",
		member:   "Sqrt",
		wantKind: "func",
		src: `package main

import "math"

func main() {
	var _ = math.Sqrt(2.0)
}
`,
	},
	{
		path:     "encoding/xml",
		member:   "Marshal",
		wantKind: "func",
		src: `package main

import "encoding/xml"

func main() {
	var _, _ = xml.Marshal(42)
}
`,
	},
	{
		path:     "encoding/csv",
		member:   "NewReaderString",
		wantKind: "func",
		src: `package main

import "encoding/csv"

func main() {
	var _ = csv.NewReaderString("a,b\n")
}
`,
	},
	{
		path:     "encoding/gob",
		member:   "NewBufferEncoder",
		wantKind: "func",
		src: `package main

import "encoding/gob"

func main() {
	var _ = gob.NewBufferEncoder()
}
`,
	},
	{
		path:     "strconv",
		member:   "Itoa",
		wantKind: "func",
		src: `package main

import "strconv"

func main() {
	var _ = strconv.Itoa(42)
}
`,
	},
	{
		// P3FE-2 probe: the program imports BOTH bytes and
		// encoding/json and flows a *bytes.Buffer into json.Compact.
		// Pre-fix, json's Compact takes a *bytes.Buffer from a
		// same-named twin package, so the merge fails on the
		// type-identity mismatch; jsonBytesIdentity below pins the
		// shared instance directly.
		path:     "encoding/json",
		member:   "Compact",
		wantKind: "func",
		src: `package main

import (
	"bytes"
	"encoding/json"
)

func main() {
	var buf bytes.Buffer
	_ = json.Compact(&buf, []byte("{}"))
	var _, _ = json.Marshal(42)
}
`,
		check: jsonBytesIdentity,
	},
	{
		path:     "encoding/base32",
		member:   "StdEncoding",
		wantKind: "var",
		src: `package main

import "encoding/base32"

func main() {
	var _ = base32.StdEncoding
}
`,
	},
	{
		path:     "encoding/base64",
		member:   "StdEncoding",
		wantKind: "var",
		src: `package main

import "encoding/base64"

func main() {
	var _ = base64.StdEncoding
}
`,
	},
	{
		path:     "encoding/hex",
		member:   "EncodeToString",
		wantKind: "func",
		src: `package main

import "encoding/hex"

func main() {
	var _ = hex.EncodeToString([]byte("x"))
}
`,
	},
	{
		path:     "encoding/binary",
		member:   "BigEndian",
		wantKind: "var",
		src: `package main

import "encoding/binary"

func main() {
	var _ = binary.BigEndian
}
`,
	},
	{
		path:     "encoding/ascii85",
		member:   "Encode",
		wantKind: "func",
		src: `package main

import "encoding/ascii85"

func main() {
	var dst, src [16]byte
	_ = ascii85.Encode(dst[:], src[:])
}
`,
	},
	{
		path:     "encoding/pem",
		member:   "EncodeToMemory",
		wantKind: "func",
		src: `package main

import "encoding/pem"

func main() {
	var _ = pem.EncodeToMemory(&pem.Block{Type: "X"})
}
`,
	},
	{
		path:     "encoding/asn1",
		member:   "Marshal",
		wantKind: "func",
		src: `package main

import "encoding/asn1"

func main() {
	var _, _ = asn1.Marshal(42)
}
`,
	},
	{
		path:     "encoding",
		member:   "TextMarshaler",
		wantKind: "type",
		src: `package main

import "encoding"

func main() {
	var _ encoding.TextMarshaler
}
`,
	},
}

// jsonBytesIdentity is the P3FE-2 acceptance: encoding/json's Compact must
// take the importer's canonical bytes.Buffer — the same *types.Named
// instance the "bytes" case's program gets — not a same-named twin from a
// throwaway bytes package. A distinct-but-identical-looking named type
// would still fail user programs at the call site, so this asserts
// instance identity, not just types.Identical.
func jsonBytesIdentity(t *testing.T, imp *Importer, synPkg *types.Package) {
	t.Helper()
	bytesPkg, err := imp.Import("bytes")
	if err != nil {
		t.Fatalf("Import(bytes): %v", err)
	}
	bufObj := bytesPkg.Scope().Lookup("Buffer")
	if bufObj == nil {
		t.Fatal("bytes.Buffer missing from synthetic bytes scope")
	}
	bufNamed, ok := bufObj.(*types.TypeName).Type().(*types.Named)
	if !ok {
		t.Fatalf("bytes.Buffer is %T, want *types.Named", bufObj.Type())
	}
	compactObj := synPkg.Scope().Lookup("Compact")
	if compactObj == nil {
		t.Fatal("json.Compact missing from synthetic json scope")
	}
	sig, ok := compactObj.(*types.Func).Type().(*types.Signature)
	if !ok {
		t.Fatalf("json.Compact is %T, want *types.Signature", compactObj.Type())
	}
	dst := sig.Params().At(0).Type()
	ptr, ok := dst.(*types.Pointer)
	if !ok {
		t.Fatalf("json.Compact dst is %s, want pointer", types.TypeString(dst, nil))
	}
	if ptr.Elem() != types.Type(bufNamed) {
		t.Fatalf("json.Compact dst = %s: not the canonical bytes.Buffer named type (P3FE-2)",
			types.TypeString(dst, nil))
	}
}

// TestSyntheticE2ECoversAllServedPackages is the structural guard for the
// table above: the served set is derived from (*Importer).Import's own
// switch, so a new synthetic package without a table entry — or a stale
// entry for a removed one — fails here instead of silently widening the
// P3FE-3 blind spot again.
func TestSyntheticE2ECoversAllServedPackages(t *testing.T) {
	served := importerServedPaths(t)
	covered := map[string]bool{}
	for _, c := range synthE2ECases {
		if covered[c.path] {
			t.Errorf("duplicate E2E case for %q", c.path)
		}
		covered[c.path] = true
	}
	for _, p := range served {
		if !covered[p] {
			t.Errorf("importer serves %q but no E2E case covers it", p)
		}
	}
	for _, c := range synthE2ECases {
		found := false
		for _, p := range served {
			if p == c.path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("E2E case for %q: importer does not serve it (stale case)", c.path)
		}
	}
}

// importerServedPaths AST-parses importer.go (same directory as this test)
// and returns the import paths in (*Importer).Import's `switch path`:
// the exact set of synthetic packages the importer serves.
func importerServedPaths(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(thisFile), "importer.go"), nil, 0)
	if err != nil {
		t.Fatalf("parsing importer.go: %v", err)
	}
	var paths []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Import" || fn.Recv == nil {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if id, ok := star.X.(*ast.Ident); !ok || id.Name != "Importer" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			tag, ok := sw.Tag.(*ast.Ident)
			if !ok || tag.Name != "path" {
				return true
			}
			for _, stmt := range sw.Body.List {
				cc, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					lit, ok := e.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					p, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquoting case literal %s: %v", lit.Value, err)
					}
					paths = append(paths, p)
				}
			}
			return true
		})
	}
	if len(paths) == 0 {
		t.Fatal("no served paths found in (*Importer).Import")
	}
	return paths
}

// TestSyntheticPackagesEndToEnd drives every table case through the real
// MergeDir pipeline and asserts the merge succeeds and the representative
// member resolves to the expected object in the synthetic package scope.
func TestSyntheticPackagesEndToEnd(t *testing.T) {
	for _, tc := range synthE2ECases {
		// Subtest names cannot contain "/"; keep the path readable.
		t.Run(strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			dir := t.TempDir()
			formatted, err := format.Source([]byte(tc.src))
			if err != nil {
				t.Fatalf("test source does not gofmt: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "main.go"), formatted, 0644); err != nil {
				t.Fatal(err)
			}
			// (1) The merge succeeds: loader (synthetic resolution) +
			// importer (type-check against the synthetic packages) +
			// flattener. A synthetic package missing MarkComplete
			// makes go/types silently drop the import and fail here
			// (P3FE-1); a cross-package type-identity mismatch fails
			// here too (P3FE-2).
			if _, err := MergeDir(dir); err != nil {
				t.Fatalf("MergeDir: %v", err)
			}
			// (2) The member resolves to the expected object in the
			// synthetic package's scope.
			imp, mainRef := importMainForTest(t, dir)
			synPkg, err := imp.Import(tc.path)
			if err != nil {
				t.Fatalf("Import(%q): %v", tc.path, err)
			}
			if tc.member == "" {
				return // blank-import-only case (unsafe)
			}
			obj := synPkg.Scope().Lookup(tc.member)
			if obj == nil {
				t.Fatalf("%s.%s missing from synthetic package scope", tc.path, tc.member)
			}
			checkObjKind(t, tc, obj)
			assertSelectorResolves(t, imp, mainRef, synPkg, tc.member, obj)
			if tc.check != nil {
				tc.check(t, imp, synPkg)
			}
		})
	}
}

// importMainForTest replays MergeDir's loader/importer stages and returns
// the importer with the type-checked main package, for assertions.
func importMainForTest(t *testing.T, dir string) (*Importer, *PackageRef) {
	t.Helper()
	l := NewLoader()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	mainRef, err := l.mainRef(abs)
	if err != nil {
		t.Fatalf("mainRef: %v", err)
	}
	deps, err := l.LoadClosure(abs, mainRef)
	if err != nil {
		t.Fatalf("LoadClosure: %v", err)
	}
	fset := token.NewFileSet()
	imp := NewImporter(l, abs, fset)
	for _, dep := range deps {
		if _, err := imp.Import(dep.ImportPath); err != nil {
			t.Fatalf("Import(%q): %v", dep.ImportPath, err)
		}
	}
	if _, err := imp.ImportRef(mainRef); err != nil {
		t.Fatalf("ImportRef: %v", err)
	}
	return imp, mainRef
}

func checkObjKind(t *testing.T, tc synthE2ECase, obj types.Object) {
	t.Helper()
	switch tc.wantKind {
	case "func":
		if _, ok := obj.(*types.Func); !ok {
			t.Fatalf("%s.%s is %T, want *types.Func", tc.path, tc.member, obj)
		}
	case "type":
		if _, ok := obj.(*types.TypeName); !ok {
			t.Fatalf("%s.%s is %T, want *types.TypeName", tc.path, tc.member, obj)
		}
	case "var":
		if _, ok := obj.(*types.Var); !ok {
			t.Fatalf("%s.%s is %T, want *types.Var", tc.path, tc.member, obj)
		}
	default:
		t.Fatalf("test bug: unknown wantKind %q", tc.wantKind)
	}
}

// assertSelectorResolves verifies that the main package's path.member
// selector binds to obj — the exact object in the synthetic package's
// scope — and not to a same-named lookalike from another package
// instance.
func assertSelectorResolves(t *testing.T, imp *Importer, mainRef *PackageRef, synPkg *types.Package, member string, obj types.Object) {
	t.Helper()
	info := imp.Info(mainRef.ImportPath)
	if info == nil {
		t.Fatal("no type info recorded for main package")
	}
	pkgName := synPkg.Name()
	found := false
	for _, f := range imp.Files(mainRef.ImportPath) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok || x.Name != pkgName || sel.Sel.Name != member {
				return true
			}
			found = true
			if got := info.Uses[sel.Sel]; got != obj {
				t.Errorf("%s.%s resolved to %v, want the synthetic scope object %v",
					pkgName, member, got, obj)
			}
			return true
		})
	}
	if !found {
		t.Fatalf("test bug: no %s.%s selector found in main source", pkgName, member)
	}
}
