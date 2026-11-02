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
	// We do two passes: first declare every top-level declaration's
	// objects (funcs, vars, consts, named types — assigning object ids),
	// then emit function bodies and global initializers. The second pass
	// may reference any top-level object regardless of source order.
	//
	// Deterministic emission order (V2-12): pkg.Files is a Go map, so
	// ranging over it directly would make function order and type/object
	// ids vary run to run. Files are visited sorted by name and
	// declarations sorted by source position, so the same package always
	// produces the same document — a prerequisite for reproducible builds
	// and snapshot testing.
	decls := sortedDecls(pkg)

	for _, d := range decls {
		if err := e.declareTopDecl(d); err != nil {
			return nil, err
		}
	}

	funcs := []any{}
	globals := []any{}
	for _, d := range decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			fn, err := e.emitFuncDecl(decl)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fn)
		case *ast.GenDecl:
			gs, err := e.emitTopGenDecl(decl)
			if err != nil {
				return nil, err
			}
			globals = append(globals, gs...)
		default:
			return nil, fmt.Errorf("emit: unsupported top-level decl %T", d)
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

// sortedDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Sorting is structural — by filename and token
// position — never keyed on any package, symbol, or test name. Unlike
// the previous sortedFuncDecls, no declaration kind is skipped: GenDecls
// (var/const/type) are part of the program, not noise.
func sortedDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		fds := make([]ast.Decl, len(file.Decls))
		copy(fds, file.Decls)
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

// declareTopDecl assigns object ids for one top-level declaration's
// defined objects, so later passes (including other globals' initializers)
// can reference them regardless of source order.
func (e *Emitter) declareTopDecl(d ast.Decl) error {
	switch decl := d.(type) {
	case *ast.FuncDecl:
		obj := e.info.Info.Defs[decl.Name]
		if obj == nil {
			return fmt.Errorf("emit: no object for func %s", decl.Name.Name)
		}
		e.declareObject(obj)
		return nil
	case *ast.GenDecl:
		switch decl.Tok {
		case token.IMPORT:
			// Import resolution is the checker's job; not emitted.
			return nil
		case token.VAR, token.CONST:
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					return fmt.Errorf("emit: unsupported top-level spec %T", spec)
				}
				for _, name := range vs.Names {
					if obj := e.info.Info.Defs[name]; obj != nil {
						e.declareObject(obj)
					}
				}
			}
			return nil
		case token.TYPE:
			for _, spec := range decl.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					return fmt.Errorf("emit: unsupported top-level spec %T", spec)
				}
				if obj := e.info.Info.Defs[ts.Name]; obj != nil {
					e.declareObject(obj)
				}
			}
			return nil
		default:
			return fmt.Errorf("emit: unsupported top-level GenDecl token %s", decl.Tok)
		}
	default:
		return fmt.Errorf("emit: unsupported top-level decl %T", d)
	}
}

// emitTopGenDecl emits one top-level GenDecl's specs as global nodes, in
// source order. One node per spec (never flattened across specs, for the
// same reason DeclStmt emits per-spec groups): a var block's specs have
// independent types and initializers, and a const block's specs have
// independent computed values (iota, repeated specs).
//
// Node shapes (all in Document.Globals, in source order):
//   - "globalvar":   {names, objs, type, values, expand} — same per-spec
//     shape as DeclStmt vardecl specs (see emitValueSpec). values are
//     initializer expressions; empty when the spec has none. Globals are
//     emitted in source order; the backend must still apply Go's
//     dependency-based initialization order — source order is the input
//     order it sorts from, not itself the initialization order.
//   - "globalconst": {names, objs, type, values} — values are one
//     "constval" node per name, taken from go/types (Const.Val()), not
//     from the AST: iota and repeated specs (e.g. `const (a = iota; b)`)
//     have no usable AST value for the later names, while go/types has
//     the computed value for every name.
//   - "globaltype":  {name, obj, type, alias} — the named type's object
//     and type-table id; alias is true for `type A = B`.
func (e *Emitter) emitTopGenDecl(gd *ast.GenDecl) ([]any, error) {
	switch gd.Tok {
	case token.IMPORT:
		return nil, nil
	case token.VAR:
		var out []any
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported top-level spec %T", spec)
			}
			fields, err := e.emitValueSpec(vs)
			if err != nil {
				return nil, err
			}
			out = append(out, e.node("globalvar", vs, fields))
		}
		return out, nil
	case token.CONST:
		var out []any
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported top-level spec %T", spec)
			}
			fields, err := e.emitConstSpec(vs)
			if err != nil {
				return nil, err
			}
			out = append(out, e.node("globalconst", vs, fields))
		}
		return out, nil
	case token.TYPE:
		var out []any
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported top-level spec %T", spec)
			}
			var obj any = -1
			var typ any
			var alias bool
			if o := e.info.Info.Defs[ts.Name]; o != nil {
				obj = e.declareObject(o)
				typ = e.typeID(o.Type())
				if tn, ok := o.(*types.TypeName); ok {
					alias = tn.IsAlias()
				}
			} else if tv, ok := e.info.Info.Types[ts.Type]; ok && tv.Type != nil {
				typ = e.typeID(tv.Type)
			}
			out = append(out, e.node("globaltype", ts, map[string]any{
				"name":  ts.Name.Name,
				"obj":   obj,
				"type":  typ,
				"alias": alias,
			}))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("emit: unsupported top-level GenDecl token %s", gd.Tok)
	}
}

// emitConstSpec emits one const spec's per-name computed values. Values
// come from go/types, never from the AST, for the reason documented on
// emitTopGenDecl: iota and repeated specs.
func (e *Emitter) emitConstSpec(vs *ast.ValueSpec) (map[string]any, error) {
	names := []string{}
	objs := []any{}
	values := []any{}
	var typ any
	if vs.Type != nil {
		if tv, ok := e.info.Info.Types[vs.Type]; ok && tv.Type != nil {
			typ = e.typeID(tv.Type)
		}
	}
	for _, name := range vs.Names {
		names = append(names, name.Name)
		obj := e.info.Info.Defs[name]
		if obj == nil {
			objs = append(objs, -1)
			values = append(values, nil)
			continue
		}
		objs = append(objs, e.declareObject(obj))
		if c, ok := obj.(*types.Const); ok && c.Val() != nil {
			values = append(values, e.node("constval", name, map[string]any{
				"value": c.Val().ExactString(),
				"type":  e.typeID(c.Type()),
			}))
		} else {
			values = append(values, nil)
		}
	}
	return map[string]any{
		"names":  names,
		"objs":   objs,
		"type":   typ,
		"values": values,
	}, nil
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
		m := map[string]any{
			"kind": "const",
			"name": o.Name(),
			"type": typ,
		}
		if o.Val() != nil {
			m["value"] = o.Val().ExactString()
		}
		return m
	case *types.TypeName:
		return map[string]any{
			"kind":  "type",
			"name":  o.Name(),
			"type":  typ,
			"alias": o.IsAlias(),
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
