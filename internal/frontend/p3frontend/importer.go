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
//   - "runtime" and "fmt" resolve to synthetic packages with the exact
//     frozen signatures (reimplemented here from the freeze spec —
//     export.go's overlayImporter is unexported and frozen, so it cannot
//     be reused, only matched);
//   - "sync" and "sync/atomic" RETIRED (review-2026-11-04 NEW-P1-5,
//     mirroring the frontend/v1 retirement in commit 503608ea): both
//     resolve from GOROOT source via the general disk path below, so a
//     program sees the real gc surface (sync.Map/Pool/Cond, generic
//     sync/atomic.Pointer/Value) instead of the narrow M2/M4 mirror that
//     rejected them. The retired mirror code (syncMethodSpecs,
//     syntheticSyncPackage, syntheticAtomicPackage — including the
//     NEW-P2-12 panic on an unknown sync param type) is deleted, not kept;
//   - "errors" and "sort" resolve to the synthetic P3-M7 wave-1 packages
//     (real-Go implementations; fail-fast members keep exact gc 1.27.1
//     signatures);
//   - module-local packages are type-checked from source with go/types,
//     recursively, with results cached by import path;
//   - stdlib packages resolve (proving the GOROOT mapping) and are
//     type-checked from GOROOT source when they compile through this
//     importer (today: sync, sync/atomic); any other stdlib package fails
//     with a clear scope error: compiling the real stdlib is P3-M7+ work.
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
	case "runtime":
		pkg := syntheticRuntimePackage()
		imp.pkgs[path] = pkg
		return pkg, nil
	case "fmt":
		pkg := syntheticFmtPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
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
	case "encoding/json":
		// P3-M7 (wave 1): synthetic encoding/json (see synth_json.go).
		pkg := syntheticJsonPackage()
		imp.pkgs[path] = pkg
		return pkg, nil
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
		// Retired-from-synthetic packages (sync, sync/atomic) resolve
		// from GOROOT source via the general disk path: type-check the
		// real sources exactly like a local package. The flattener
		// never flattens stdlib (LoadClosure only returns kindLocal),
		// so the merged source keeps the import spec and the frozen
		// gofrontend — which retired the same synthetics in 503608ea —
		// compiles the same real package: one outcome per program,
		// whichever frontend checks it first.
		//
		// Any other stdlib package whose sources do not (yet)
		// type-check through this importer — e.g. because they need a
		// runtime member the synthetic runtime does not declare — keeps
		// the named M1 scope error instead of a confusing
		// types.Config failure. The retirement of sync/sync-atomic is
		// pinned by TestSyncRetiredToDisk, so a regression there fails
		// tests rather than silently re-narrowing the surface.
		if pkg, err := imp.checkRef(ref); err == nil {
			return pkg, nil
		}
		return nil, fmt.Errorf("p3frontend: import %q: stdlib package compilation lands in the P3-M7+ waves (M1 scope: synthetic runtime/fmt only; resolved at %s)", path, ref.Dir)
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
// Synthetic packages: exact frozen signatures (INTERFACE_FREEZE.md;
// internal/frontend/export.go's overlayImporter, reimplemented).
// ---------------------------------------------------------------------------

// syntheticRuntimePackage mirrors the frozen synthetic "runtime": Goexit,
// SetFinalizer, KeepAlive, GC — signatures match gc's runtime package.
func syntheticRuntimePackage() *types.Package {
	pkg := types.NewPackage("runtime", "runtime")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	mkSig := func(params ...*types.Var) *types.Signature {
		return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), nil, false)
	}
	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Goexit",
		types.NewSignatureType(nil, nil, nil, nil, nil, false)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "SetFinalizer", mkSig(
		newParam("obj", anyT),
		newParam("finalizer", anyT))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "KeepAlive", mkSig(
		newParam("x", anyT))))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "GC", mkSig()))
	// GOMAXPROCS (review-2026-11-04 NEW-P1-5): the real GOROOT sync
	// package (pool.go) calls runtime.GOMAXPROCS, so retiring the
	// synthetic sync requires the synthetic runtime to declare it with
	// gc's exact signature — otherwise the real sync sources fail this
	// importer's type-check and fall back to the stdlib scope error.
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "GOMAXPROCS",
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("n", types.Typ[types.Int])),
			types.NewTuple(newParam("", types.Typ[types.Int])), false)))
	pkg.MarkComplete()
	return pkg
}

// syntheticFmtPackage mirrors the frozen D4 fmt stub: exactly the seven
// pure functions with exact variadic signatures.
func syntheticFmtPackage() *types.Package {
	pkg := types.NewPackage("fmt", "fmt")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	anySlice := types.NewSlice(anyT)
	errorT := types.Universe.Lookup("error").Type()
	intT := types.Typ[types.Int]
	stringT := types.Typ[types.String]
	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insert := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, true)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	a := newParam("a", anySlice)
	format := newParam("format", stringT)
	n := newParam("n", intT)
	errRes := newParam("err", errorT)
	strRes := newParam("", stringT)
	errResOnly := newParam("", errorT)
	insert("Print", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Printf", types.NewTuple(format, a), types.NewTuple(n, errRes))
	insert("Println", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Sprint", types.NewTuple(a), types.NewTuple(strRes))
	insert("Sprintf", types.NewTuple(format, a), types.NewTuple(strRes))
	insert("Sprintln", types.NewTuple(a), types.NewTuple(strRes))
	insert("Errorf", types.NewTuple(format, a), types.NewTuple(errResOnly))
	pkg.MarkComplete()
	return pkg
}

// syntheticUnsafePackage returns go/types' canonical unsafe package
// (types.Unsafe — the same package the go/types source importer returns
// for "unsafe"), not a hand-built mirror. The frozen P3-M4 design kept it
// empty (only so `import _ "unsafe"` for //go:linkname directives
// type-checks), but the NEW-P1-5 retirement needs the real thing: the
// GOROOT sync closure uses unsafe.Pointer throughout, unsafe.Sizeof in
// constant position (sync/pool.go's array-length padding), and
// unsafe.Slice/String/Offsetof. go/types evaluates Sizeof/Alignof/
// Offsetof/Slice/String only for the *Builtin objects in types.Unsafe's
// scope — a hand-declared func with the same name would type-check in
// value position but fail every constant-position use. This is not a new
// synthetic surface: it is the checker's own unsafe package, shared
// read-only, and the frozen gofrontend downstream still applies its own
// (empty) unsafe contract to the merged source, so a program using
// unsafe members beyond the blank import still fails loudly there.
func syntheticUnsafePackage() *types.Package {
	return types.Unsafe
}
