package p3frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

// Importer is a types.Importer that graduates the frozen overlayImporter:
//   - "runtime", "fmt", "sync" and "sync/atomic" resolve to the REAL
//     stdlib packages, type-checked from GOROOT source like any other
//     stdlib package (B-1/PKG-053 retired the frozen stubs; the p3frontend
//     fmt/sync/atomic/runtime mirrors were deleted with them);
//   - "unsafe" resolves to the P3-M8 package: the eight real compiler
//     builtins shared from types.Unsafe's scope plus `type Pointer =
//     unsafe.Pointer`;
//   - "errors" and "sort" resolve to the synthetic P3-M7 wave-1 packages
//     (real-Go implementations; fail-fast members keep exact gc 1.27.1
//     signatures);
//   - module-local packages are type-checked from source with go/types,
//     recursively, with results cached by import path;
//   - stdlib packages are type-checked from GOROOT source via checkRef,
//     recursively, with results cached by import path (the same mechanism
//     as module-local packages).
//
// Importer is single-threaded (go/types drives it synchronously).
type Importer struct {
	loader  *Loader
	mainDir string // anchors module-mode resolution
	fset    *token.FileSet

	pkgs  map[string]*types.Package
	infos map[string]*types.Info
	files map[string][]*ast.File
	stack []string // import paths currently being type-checked (cycle guard)
}

// NewImporter builds an Importer. fset is shared across every package so
// the flattener can merge declarations into one file for go/format.
func NewImporter(l *Loader, mainDir string, fset *token.FileSet) *Importer {
	return &Importer{
		loader:  l,
		mainDir: mainDir,
		fset:    fset,
		pkgs:    map[string]*types.Package{},
		infos:   map[string]*types.Info{},
		files:   map[string][]*ast.File{},
	}
}

// Import implements types.Importer.
func (imp *Importer) Import(path string) (*types.Package, error) {
	if pkg, ok := imp.pkgs[path]; ok {
		return pkg, nil
	}
	for _, p := range imp.stack {
		if p == path {
			cycle := append(append([]string{}, imp.stack...), path)
			return nil, fmt.Errorf("p3frontend: import cycle: %s", strings.Join(cycle, " -> "))
		}
	}
	switch path {
	case "time":
		pkg := syntheticTimePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "context":
		// Context references time.Time/time.Duration: ensure the
		// synthetic time package is built and cached first.
		timePkg, ok := imp.pkgs["time"]
		if !ok {
			timePkg = syntheticTimePackage()
			imp.pkgs["time"] = timePkg
		}
		pkg := syntheticContextPackage(timePkg)
		imp.pkgs[path] = pkg
		return pkg, nil
	case "errors":
		// P3-M7 (wave 1): synthetic errors package (real-Go New/Is/Unwrap/
		// Join/AsType; As fail-fast). Self-contained: no synthetic
		// dependencies.
		pkg := syntheticErrorsPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "sort":
		// P3-M7 (wave 1): synthetic sort package (real-Go pdqsort/stable/
		// search; Find/Slice/SliceStable/SliceIsSorted fail-fast).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticSortPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "io":
		// P3-M10: synthetic io package (minimal: Reader/Writer only;
		// see synth_io.go). Required by bytes/strings (P3-M8).
		pkg := syntheticIoPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "bytes":
		// P3-M7 (wave 1): synthetic bytes package (synth_bytes.go).
		// Requires the synthetic io package for io.Reader/io.Writer
		// (P3-M8).
		ioPkg, err := imp.Import("io")
		if err != nil {
			return nil, err
		}
		pkg := syntheticBytesPackage(ioPkg)
		imp.pkgs[path] = pkg
		return pkg, nil
	case "strings":
		// P3-M7 (wave 1): synthetic strings package (synth_strings.go).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticStringsPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "unsafe":
		pkg := syntheticUnsafePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "math":
		// P3-M7 (wave 1): synthetic math package (real-Go ports +
		// bit-conversion/Sqrt/FMA intrinsics; see synth_math.go).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticMathPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/xml":
		// P3-M7 (wave 1): synthetic encoding/xml (see synth_xml.go).
		// Self-contained: no synthetic->synthetic imports.
		pkg := syntheticXmlPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/csv":
		// P3-M7 (wave 1): synthetic encoding/csv (see synth_enc_csv_gob.go).
		pkg := syntheticCsvPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/gob":
		// P3-M7 (wave 1): synthetic encoding/gob (see synth_enc_csv_gob.go).
		pkg := syntheticGobPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "strconv":
		// P3-M7 (wave 1): synthetic strconv (see synth_strconv.go).
		pkg := syntheticStrconvPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	// "encoding/json" has NO case here (review-08-synth-json,
	// R08-S-01): the synthetic mirror (synth_json.go) was deleted
	// with the retirement of the synthetic source — real
	// encoding/json type-checks from GOROOT via the loader's stdlib
	// path below, like fmt/sync (REVIEW-2026-11-05-11).
	case "encoding/base32":
		// P3-M7 (wave 1): synthetic encoding/base32 (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticBase32Package()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/base64":
		// P3-M7 (wave 1): synthetic encoding/base64 (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticBase64Package()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/hex":
		// P3-M7 (wave 1): synthetic encoding/hex (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticHexPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/binary":
		// P3-M7 (wave 1): synthetic encoding/binary (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticBinaryPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/ascii85":
		// P3-M7 (wave 1): synthetic encoding/ascii85 (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticAscii85Package()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/pem":
		// P3-M7 (wave 1): synthetic encoding/pem (m7-wenc-core).
		// Imports synthetic encoding/base64: ensure the base64 mirror is
		// built and cached first (the context->time precedent).
		base64Pkg, ok := imp.pkgs["encoding/base64"]
		if !ok {
			base64Pkg = syntheticBase64Package()
			imp.pkgs["encoding/base64"] = base64Pkg
		}
		_ = base64Pkg
		pkg := syntheticPemPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding/asn1":
		// P3-M7 (wave 1): synthetic encoding/asn1 (m7-wenc-core).
		// Self-contained: no synthetic dependencies.
		pkg := syntheticAsn1Package()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "encoding":
		// P3-M7 (wave 1): synthetic top-level encoding (m7-wenc-core):
		// the six marshaling interfaces only.
		// Self-contained: no synthetic dependencies.
		pkg := syntheticEncodingPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	}
	ref, err := imp.loader.ResolveImport(imp.mainDir, path)
	if err != nil {
		return nil, err
	}
	if ref.Kind == kindStdlib {
		// Stdlib packages type-check from GOROOT source, exactly like
		// module-local packages (B-1/PKG-053: fmt/sync/sync-atomic/
		// runtime retired their synthetic mirrors).
		return imp.checkRef(ref)
	}
	if ref.Kind != kindLocal {
		return nil, fmt.Errorf("p3frontend: import %q: unexpected resolution kind", path)
	}
	return imp.checkRef(ref)
}

// ImportRef type-checks an already-resolved local package (used for the
// main package, which has no import path of its own).
func (imp *Importer) ImportRef(ref *PackageRef) (*types.Package, error) {
	if pkg, ok := imp.pkgs[ref.ImportPath]; ok {
		return pkg, nil
	}
	return imp.checkRef(ref)
}

// checkRef parses and type-checks one local package, recording its files
// and type info for the flattener.
func (imp *Importer) checkRef(ref *PackageRef) (*types.Package, error) {
	imp.stack = append(imp.stack, ref.ImportPath)
	defer func() { imp.stack = imp.stack[:len(imp.stack)-1] }()

	var files []*ast.File
	for _, f := range ref.Files {
		src, err := os.ReadFile(filepath.Join(ref.Dir, f))
		if err != nil {
			return nil, fmt.Errorf("p3frontend: reading %s: %w", f, err)
		}
		af, err := parser.ParseFile(imp.fset, filepath.Join(ref.Dir, f), src, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("p3frontend: parsing %s: %w", f, err)
		}
		files = append(files, af)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
	}
	conf := types.Config{
		Importer: imp,
		Sizes:    types.SizesFor("gc", "amd64"),
	}
	pkg, err := conf.Check(ref.ImportPath, imp.fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("p3frontend: type-checking %s: %w", ref.ImportPath, err)
	}
	imp.pkgs[ref.ImportPath] = pkg
	imp.infos[ref.ImportPath] = info
	imp.files[ref.ImportPath] = files
	return pkg, nil
}

// Package returns the type-checked package for a path previously imported.
func (imp *Importer) Package(path string) *types.Package { return imp.pkgs[path] }

// Info returns the type info recorded while checking path.
func (imp *Importer) Info(path string) *types.Info { return imp.infos[path] }

// Files returns the parsed files of a previously checked package.
func (imp *Importer) Files(path string) []*ast.File { return imp.files[path] }

// ---------------------------------------------------------------------------
// Synthetic packages defined in this file: only "unsafe" remains here
// (the P3-M8 package: eight builtins + Pointer). The runtime/fmt/sync/
// sync-atomic mirrors were DELETED with the fmt/sync retirement
// (REVIEW-2026-11-05-11) — those packages type-check from GOROOT now.
// The other synthetic mirrors live in synth_*.go.
// ---------------------------------------------------------------------------

// unsafeBuiltinNames are the eight unsafe builtins shared from types.Unsafe
// (the P3-M8 mechanism from internal/frontend/synth_unsafe.go: go/types
// populates types.Unsafe's scope in its universe init, so the synthetic
// package inserts the same objects — go/types then applies gc's own
// checking to them, e.g. constant-folding Sizeof/Alignof/Offsetof).
var unsafeBuiltinNames = []string{
	"Add", "Alignof", "Offsetof", "Sizeof",
	"Slice", "SliceData", "String", "StringData",
}

// syntheticUnsafePackage builds the P3-M8 synthetic "unsafe" package: the
// eight real compiler builtins (shared objects from types.Unsafe's scope)
// plus `type Pointer = unsafe.Pointer`. The empty P3-M4 stub is retired:
// real stdlib (e.g. internal/bytealg) uses unsafe.Offsetof.
func syntheticUnsafePackage() *types.Package {
	pkg := types.NewPackage("unsafe", "unsafe")
	for _, name := range unsafeBuiltinNames {
		obj := types.Unsafe.Scope().Lookup(name)
		if obj == nil {
			panic(fmt.Sprintf("p3frontend: synthetic unsafe package: types.Unsafe lacks builtin %s", name))
		}
		pkg.Scope().Insert(obj)
	}
	ptrName := types.NewTypeName(token.NoPos, pkg, "Pointer", nil)
	types.NewAlias(ptrName, types.Typ[types.UnsafePointer])
	pkg.Scope().Insert(ptrName)
	pkg.MarkComplete()
	return pkg
}
