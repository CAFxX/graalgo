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
	// We do three passes: first declare all top-level funcs and globals
	// (assigning object ids), then emit func bodies, then emit global
	// initializers. The first pass is what makes forward references
	// work: a func body or a global initializer may refer to a func or
	// global declared later in the package, and those objects must
	// already have ids. Top-level GenDecls are handled per kind in
	// declareTopGenDecl: vars go to Globals, imports and type
	// declarations need no top-level entry (see there), and const
	// declarations fail loudly. A package-level var used to vanish:
	// Globals was a hardcoded empty list while sortedFuncDecls skipped
	// GenDecls entirely.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedDecls(pkg)

	var funcs []any
	globals := []any{}
	for _, d := range decls {
		switch dd := d.(type) {
		case *ast.FuncDecl:
			obj := e.info.Info.Defs[dd.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", dd.Name.Name)
			}
			e.declareObject(obj)
		case *ast.GenDecl:
			if err := e.declareTopGenDecl(dd); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("emit: unsupported top-level decl %T", d)
		}
	}

	for _, d := range decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}

	for _, d := range decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported top-level var spec %T", spec)
			}
			g, err := e.emitGlobalVar(vs)
			if err != nil {
				return nil, err
			}
			globals = append(globals, g)
		}
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

// declareTopGenDecl declares the objects of one top-level GenDecl, or
// fails loudly for GenDecl kinds the emitter does not support. VAR specs
// are declared here (their initializers are emitted in the third pass of
// emitDocument). IMPORT GenDecls carry no IR: import resolution is the
// checker's job and package-qualified uses are emitted as pkgref nodes
// in func bodies, so skipping the import declaration itself drops
// nothing semantic. TYPE GenDecls likewise need no top-level entry:
// a named type's definition is materialized in the Types table via
// go/types the first time it is referenced (this is why the encode
// all-kinds program, which declares `type Point struct` at top level
// and uses it in a composite literal, emits correctly). CONST GenDecls
// are different: a const's value is not recoverable from the object
// table (describeObject has no value field for consts), so a const
// declaration has no faithful representation yet and is rejected
// rather than silently dropped — matching emitDeclStmt's rejection of
// local const declarations.
func (e *Emitter) declareTopGenDecl(gd *ast.GenDecl) error {
	switch gd.Tok {
	case token.IMPORT, token.TYPE:
		return nil
	case token.VAR:
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return fmt.Errorf("emit: unsupported top-level var spec %T", spec)
			}
			for _, name := range vs.Names {
				if name.Name == "_" {
					continue
				}
				obj := e.info.Info.Defs[name]
				if obj == nil {
					return fmt.Errorf("emit: no object for var %s", name.Name)
				}
				e.declareObject(obj)
			}
		}
		return nil
	default:
		return fmt.Errorf("emit: unsupported top-level decl %s", gd.Tok)
	}
}

// emitGlobalVar emits one top-level ValueSpec as a "vardecl" node, the
// same shape emitDeclStmt produces for a local var declaration: the
// declared names, their object ids, the (optional) explicit type, and
// the initializer expressions in source order. A spec with no
// initializer (e.g. `var s string`) has an empty values list; a spec
// with one multi-valued initializer for several names (e.g.
// `var a, b = f()`) carries that single initializer expression as-is,
// exactly as written. The caller has already declared every name, so
// the object ids here match the ids func bodies use for the same vars.
func (e *Emitter) emitGlobalVar(vs *ast.ValueSpec) (map[string]any, error) {
	names := []string{}
	objs := []any{}
	for _, name := range vs.Names {
		names = append(names, name.Name)
		if name.Name == "_" {
			objs = append(objs, -1)
			continue
		}
		if obj := e.info.Info.Defs[name]; obj != nil {
			objs = append(objs, e.declareObject(obj))
		} else {
			objs = append(objs, -1)
		}
	}
	var typ any
	if vs.Type != nil {
		if tv, ok := e.info.Info.Types[vs.Type]; ok {
			typ = e.typeID(tv.Type)
		}
	}
	values := []any{}
	for _, v := range vs.Values {
		vn, err := e.emitExpr(v)
		if err != nil {
			return nil, err
		}
		values = append(values, vn)
	}
	return e.node("vardecl", vs, map[string]any{
		"names":  names,
		"objs":   objs,
		"define": false,
		"type":   typ,
		"values": values,
	}), nil
}

// sortedDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Sorting is structural — by filename and token
// position — never keyed on any package, symbol, or test name.
func sortedDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		fds := append([]ast.Decl(nil), file.Decls...)
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
	switch b.Info() & types.IsNumeric {
	case types.IsBoolean:
		return "bool"
	case types.IsInteger:
		return "int"
	case types.IsUnsigned:
		return "uint"
	case types.IsFloat:
		return "float"
	case types.IsComplex:
		return "complex"
	}
	if b.Info()&types.IsString != 0 {
		return "string"
	}
	if b.Info()&types.IsUntyped != 0 {
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
