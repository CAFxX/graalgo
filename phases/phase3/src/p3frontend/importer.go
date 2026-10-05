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
//   - module-local packages are type-checked from source with go/types,
//     recursively, with results cached by import path;
//   - stdlib packages resolve (proving the GOROOT mapping) but fail with
//     a clear scope error: compiling the real stdlib is P3-M7+ work.
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
	}
	ref, err := imp.loader.ResolveImport(imp.mainDir, path)
	if err != nil {
		return nil, err
	}
	if ref.Kind == kindStdlib {
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
// phases/phase1/frontend/export.go's overlayImporter, reimplemented).
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
