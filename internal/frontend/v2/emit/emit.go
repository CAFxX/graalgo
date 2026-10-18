// Package emit implements stage 3 of the frontend/v2 pipeline: typed AST
// → in-memory backend program.
//
// There is no JSON wire format: v2 constructs the program representation
// directly in memory (his 2026-10-15 order: "that should obviously skip
// the json"). One AST node kind maps to one emit function — no monolith.
// There is no desugaring of stdlib calls, no package-name special cases,
// and no synthetic packages. What the source says is what the IR says.
package emit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"graalgo-frontend-v2/checker"
)

// Document is the top-level in-memory v2 program representation. It is
// never serialized: there is no JSON wire format in v2.
type Document struct {
	Frontend  string
	Generator map[string]any
	Package   map[string]any
	Types     []any
	Objects   []any
	Funcs     []any
	Globals   []any
}

// Emitter holds the state for one emission.
type Emitter struct {
	fset *token.FileSet
	info *checker.Result
	// files maps absolute path → index in package.files.
	files []string
	// Type table: go/types.Type → id.
	typeIDs map[types.Type]int
	types   []any
	// Object table: types.Object → id.
	objIDs  map[types.Object]int
	objects []any
}

// Emit runs stage 3 and returns the in-memory program document. The
// document is consumed directly by the backend via the Go→Java API;
// it is never serialized to JSON.
func Emit(cr *checker.Result, pkg *ast.Package, files []string) (*Document, error) {
	e := &Emitter{
		fset:    cr.Fset,
		info:    cr,
		files:   files,
		typeIDs: make(map[types.Type]int),
		objIDs:  make(map[types.Object]int),
	}
	return e.emitDocument(pkg)
}

func (e *Emitter) emitDocument(pkg *ast.Package) (*Document, error) {
	// Emit order: types and objects are populated lazily as nodes are
	// emitted (ids registered before description, so recursion works).
	// We do two passes: first declare all top-level funcs and globals
	// (assigning object ids), then emit bodies.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedFuncDecls(pkg)

	var funcs []any
	for _, fd := range decls {
		obj := e.info.Info.Defs[fd.Name]
		if obj == nil {
			return nil, fmt.Errorf("emit: no object for func %s", fd.Name.Name)
		}
		e.declareObject(obj)
	}

	// Package-level var declarations are emitted as globals (their
	// initializers are runtime effects). Top-level const/type declarations
	// are inert and skipped by sortedTopVarSpecs — never silently dropped
	// in a way that loses observable behavior.
	varSpecs, err := sortedTopVarSpecs(pkg)
	if err != nil {
		return nil, err
	}
	for _, vs := range varSpecs {
		for _, name := range vs.Names {
			if obj := e.info.Info.Defs[name]; obj != nil {
				e.declareObject(obj)
			}
		}
	}

	for _, fd := range decls {
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}

	globals := []any{}
	for _, vs := range varSpecs {
		g, err := e.emitGlobalVar(vs)
		if err != nil {
			return nil, err
		}
		globals = append(globals, g)
	}

	baseNames := make([]string, len(e.files))
	for i, f := range e.files {
		baseNames[i] = fileBase(f)
	}
	return &Document{
		Frontend: "v2",
		Generator: map[string]any{
			"tool":    "gofrontend-v2",
			"version": "0.1.0",
			"goroot":  "go1.27.1",
		},
		Package: map[string]any{
			"name":  pkg.Name,
			"files": baseNames,
			"lang":  "go1.27",
		},
		Types:   e.types,
		Objects: e.objects,
		Funcs:   funcs,
		Globals: globals,
	}, nil
}

// sortedFuncDecls returns every top-level function declaration in the
// package in deterministic order: files sorted by name, then declarations
// sorted by source position. Non-function declarations are skipped, as in
// the original emission loop. Sorting is structural — by filename and
// token position — never keyed on any package, symbol, or test name.
func sortedFuncDecls(pkg *ast.Package) []*ast.FuncDecl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []*ast.FuncDecl
	for _, name := range names {
		file := pkg.Files[name]
		var fds []*ast.FuncDecl
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fds = append(fds, fd)
		}
		// Positions from one *ast.File share the same FileSet base, so
		// token.Pos comparison is a total source-position order within the
		// file. go/parser already produces Decls in source order; this sort
		// states the contract explicitly instead of relying on that
		// implementation detail.
		sort.SliceStable(fds, func(i, j int) bool { return fds[i].Pos() < fds[j].Pos() })
		decls = append(decls, fds...)
	}
	return decls
}

// sortedTopVarSpecs returns every top-level var ValueSpec in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Top-level const and type declarations are not emitted,
// but they are also not rejected: they are semantically inert — a const's
// value is carried on every use (see describeObject) and a named type is
// fully described via go/types wherever it is referenced (see typeID) — so
// skipping the declaration site loses no observable behavior. (A var
// declaration is different: its initializer is a runtime effect, which is
// why vars are emitted as globals.) Import declarations are the checker's
// job and are skipped, never emitted.
func sortedTopVarSpecs(pkg *ast.Package) ([]*ast.ValueSpec, error) {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var specs []*ast.ValueSpec
	for _, name := range names {
		file := pkg.Files[name]
		type ordered struct {
			pos token.Pos
			vs  *ast.ValueSpec
		}
		var ord []ordered
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch gd.Tok {
			case token.VAR:
				for _, s := range gd.Specs {
					vs, ok := s.(*ast.ValueSpec)
					if !ok {
						return nil, fmt.Errorf("emit: unsupported var spec %T", s)
					}
					ord = append(ord, ordered{pos: vs.Pos(), vs: vs})
				}
			case token.IMPORT:
				// Not emitted; resolution is the checker's job.
			case token.CONST, token.TYPE:
				// Semantically inert declarations (see sortedTopVarSpecs):
				// skipped, never emitted, never rejected.
			default:
				return nil, fmt.Errorf("emit: unsupported top-level declaration %v", gd.Tok)
			}
		}
		// Positions from one *ast.File share the same FileSet base, so
		// token.Pos comparison is a total source-position order within the
		// file (same contract as sortedFuncDecls).
		sort.SliceStable(ord, func(i, j int) bool { return ord[i].pos < ord[j].pos })
		for _, o := range ord {
			specs = append(specs, o.vs)
		}
	}
	return specs, nil
}

func fileBase(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// pos renders a source position per §3 of the spec.
func (e *Emitter) pos(n ast.Node) map[string]any {
	p := e.fset.Position(n.Pos())
	end := e.fset.Position(n.End())
	fileIdx := 0
	for i, f := range e.files {
		if f == p.Filename {
			fileIdx = i
			break
		}
	}
	// col is rune-based (spec §3).
	return map[string]any{
		"file": fileIdx,
		"line": p.Line,
		"col":  runeCol(p),
		"off":  p.Offset,
		"len":  end.Offset - p.Offset,
	}
}

func runeCol(p token.Position) int {
	// token.Position.Column is byte-based; the spec wants rune-based.
	// Without the source line we approximate with the byte column.
	// TODO(v2): track source lines for exact rune columns.
	return p.Column
}

// declareObject assigns an object id if not already assigned.
func (e *Emitter) declareObject(obj types.Object) int {
	return e.declareObjectParam(obj, false)
}

// declareParam assigns an object id for a function parameter.
func (e *Emitter) declareParam(obj types.Object) int {
	return e.declareObjectParam(obj, true)
}

func (e *Emitter) declareObjectParam(obj types.Object, isParam bool) int {
	if id, ok := e.objIDs[obj]; ok {
		return id
	}
	id := len(e.objects)
	e.objIDs[obj] = id
	// Placeholder; filled by describeObject below. Registered before
	// description so recursive references work.
	e.objects = append(e.objects, nil)
	e.objects[id] = e.describeObject(obj, isParam)
	return id
}

func (e *Emitter) describeObject(obj types.Object, isParam bool) map[string]any {
	typ := e.typeID(obj.Type())
	switch o := obj.(type) {
	case *types.Var:
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  typ,
			"param": isParam,
		}
	case *types.Func:
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": typ,
		}
	case *types.Const:
		// Constants carry their value: describing them as "var" with no
		// value silently lost the value (e.g. math.Pi, true, iota uses).
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  typ,
			"value": o.Val().ExactString(),
		}
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
			"type": typ,
		}
	default:
		return map[string]any{
			"kind": "var",
			"name": o.Name(),
			"type": typ,
		}
	}
}

// typeID assigns a type-table id, registering before describing (recursion).
func (e *Emitter) typeID(t types.Type) int {
	if id, ok := e.typeIDs[t]; ok {
		return id
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	e.types[id] = e.describeType(t)
	return id
}

func (e *Emitter) describeType(t types.Type) map[string]any {
	switch tt := t.(type) {
	case *types.Basic:
		class := basicClass(tt)
		return map[string]any{
			"kind": "basic",
			"detail": map[string]any{
				"name":    tt.Name(),
				"class":   class,
				"untyped": tt.Info()&types.IsUntyped != 0,
			},
		}
	case *types.Pointer:
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}
	case *types.Slice:
		return map[string]any{
			"kind":   "slice",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}
	case *types.Array:
		return map[string]any{
			"kind":   "array",
			"detail": map[string]any{"len": tt.Len(), "elem": e.typeID(tt.Elem())},
		}
	case *types.Map:
		return map[string]any{
			"kind": "map",
			"detail": map[string]any{
				"key":  e.typeID(tt.Key()),
				"elem": e.typeID(tt.Elem()),
			},
		}
	case *types.Chan:
		var dir string
		switch tt.Dir() {
		case types.SendRecv:
			dir = "both"
		case types.SendOnly:
			dir = "send"
		case types.RecvOnly:
			dir = "recv"
		}
		return map[string]any{
			"kind":   "chan",
			"detail": map[string]any{"dir": dir, "elem": e.typeID(tt.Elem())},
		}
	case *types.Signature:
		params := []any{}
		for i := 0; i < tt.Params().Len(); i++ {
			p := tt.Params().At(i)
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": e.typeID(p.Type()),
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": e.typeID(r.Type()),
			})
		}
		return map[string]any{
			"kind": "signature",
			"detail": map[string]any{
				"params":   params,
				"results":  results,
				"variadic": tt.Variadic(),
			},
		}
	case *types.Named:
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    []any{},
			},
		}
	case *types.Alias:
		// In go1.27 the predeclared any is an alias (byte/rune are basics
		// with name overrides, handled by basicClass). Without this case
		// aliases fell into default and were mis-emitted as
		// {"kind":"basic","class":"unknown"}.
		return map[string]any{
			"kind": "alias",
			"detail": map[string]any{
				"name": aliasName(tt),
				"rhs":  e.typeID(tt.Rhs()),
			},
		}
	case *types.Tuple:
		// The result type of a call expression (e.g. the empty tuple for
		// println(...)). Without this case it fell into default and was
		// mis-emitted as {"kind":"basic","class":"unknown"}.
		vars := []any{}
		for i := 0; i < tt.Len(); i++ {
			v := tt.At(i)
			vars = append(vars, map[string]any{
				"name": v.Name(),
				"type": e.typeID(v.Type()),
			})
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"vars": vars},
		}
	case *types.Struct:
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     e.typeID(f.Type()),
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
				"offset":   0,
			})
		}
		return map[string]any{
			"kind":   "struct",
			"detail": map[string]any{"fields": fields},
		}
	case *types.Interface:
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  []any{},
				"embedded": []any{},
			},
		}
	default:
		return map[string]any{
			"kind":   "basic",
			"detail": map[string]any{"name": t.String(), "class": "unknown", "untyped": false},
		}
	}
}

func basicClass(b *types.Basic) string {
	// The go/types basic flags compose: unsigned integers carry
	// IsInteger|IsUnsigned, so single-flag equality tests miss them (uint
	// through uintptr, and byte, previously fell through to "unknown").
	// Test flags in specificity order instead.
	info := b.Info()
	switch {
	case info&types.IsBoolean != 0:
		return "bool"
	case info&types.IsUnsigned != 0:
		return "uint"
	case info&types.IsInteger != 0:
		return "int"
	case info&types.IsFloat != 0:
		return "float"
	case info&types.IsComplex != 0:
		return "complex"
	}
	if info&types.IsString != 0 {
		return "string"
	}
	if info&types.IsUntyped != 0 {
		return "untyped"
	}
	return "unknown"
}

func namedName(n *types.Named) string {
	if obj := n.Obj(); obj != nil {
		if pkg := obj.Pkg(); pkg != nil && pkg.Path() != "" {
			return pkg.Path() + "." + obj.Name()
		}
		return obj.Name()
	}
	return n.String()
}

// aliasName renders an alias's declared name with its package path,
// mirroring namedName.
func aliasName(a *types.Alias) string {
	if obj := a.Obj(); obj != nil {
		if pkg := obj.Pkg(); pkg != nil && pkg.Path() != "" {
			return pkg.Path() + "." + obj.Name()
		}
		return obj.Name()
	}
	return a.String()
}
