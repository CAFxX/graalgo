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
	decls := sortedTopDecls(pkg)

	// Reject //go:embed and //go:linkname before any node is emitted:
	// both would otherwise be silently dropped (the emitter never
	// reads Doc comments), producing a wrong program that reports
	// success. Real support needs design; until then, fail loudly.
	if err := e.rejectUnsupportedDirectives(pkg); err != nil {
		return nil, err
	}

	// Pass 1: declare all top-level funcs and globals (assigning object
	// ids) so bodies and initializers can reference them.
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			obj := e.info.Info.Defs[d.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", d.Name.Name)
			}
			if _, err := e.declareObject(obj); err != nil {
				return nil, err
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
				}
				for _, name := range vs.Names {
					if name.Name == "_" {
						continue
					}
					obj := e.info.Info.Defs[name]
					if obj == nil {
						return nil, fmt.Errorf("emit: no object for global %s", name.Name)
					}
					if _, err := e.declareObject(obj); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	var funcs []any
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

	globals, err := e.emitGlobals(decls)
	if err != nil {
		return nil, err
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

// sortedTopDecls returns every top-level declaration in the package in
// deterministic order: files sorted by name, then declarations sorted by
// source position. Non-function declarations are no longer skipped: the
// emitter needs top-level var declarations for Globals, and methods are
// FuncDecls like any other function. Sorting is structural — by filename
// and token position — never keyed on any package, symbol, or test name.
func sortedTopDecls(pkg *ast.Package) []ast.Decl {
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var decls []ast.Decl
	for _, name := range names {
		file := pkg.Files[name]
		ds := make([]ast.Decl, len(file.Decls))
		copy(ds, file.Decls)
		// Positions from one *ast.File share the same FileSet base, so
		// token.Pos comparison is a total source-position order within the
		// file. go/parser already produces Decls in source order; this sort
		// states the contract explicitly instead of relying on that
		// implementation detail.
		sort.SliceStable(ds, func(i, j int) bool { return ds[i].Pos() < ds[j].Pos() })
		decls = append(decls, ds...)
	}
	return decls
}

// emitGlobals emits one vardecl node per top-level var ValueSpec, in
// deterministic (file, position) order. Package-level vars are real
// definitions: a backend reading Globals must see them, with their
// initializers, or references to them would dangle (objects 26/27 with
// no definition anywhere, as the review found). Top-level const and type
// declarations need no Globals entry: consts are fully described in the
// object table (kind const, with value) and named types in the type
// table, both on demand when referenced.
func (e *Emitter) emitGlobals(decls []ast.Decl) ([]any, error) {
	globals := []any{}
	for _, d := range decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				return nil, fmt.Errorf("emit: unsupported var spec %T", spec)
			}
			n, err := e.emitValueSpec(vs)
			if err != nil {
				return nil, err
			}
			globals = append(globals, n)
		}
	}
	return globals, nil
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
func (e *Emitter) declareObject(obj types.Object) (int, error) {
	return e.declareObjectParam(obj, false)
}

// declareParam assigns an object id for a function parameter.
func (e *Emitter) declareParam(obj types.Object) (int, error) {
	return e.declareObjectParam(obj, true)
}

func (e *Emitter) declareObjectParam(obj types.Object, isParam bool) (int, error) {
	if id, ok := e.objIDs[obj]; ok {
		return id, nil
	}
	id := len(e.objects)
	e.objIDs[obj] = id
	// Placeholder; filled by describeObject below. Registered before
	// description so recursive references work.
	e.objects = append(e.objects, nil)
	desc, err := e.describeObject(obj, isParam)
	if err != nil {
		return 0, err
	}
	e.objects[id] = desc
	return id, nil
}

// describeObject describes one object for the object table. Every object
// kind the checker can produce gets its own kind here: a const, a type
// name, or a builtin emitted as "var" is a wrong program, not a missing
// feature (math.Pi, true, and the conversion type int(u) all came out as
// vars). An object kind with no case fails loudly instead of silently
// mis-describing.
func (e *Emitter) describeObject(obj types.Object, isParam bool) (map[string]any, error) {
	switch o := obj.(type) {
	case *types.Var:
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  e.typeID(o.Type()),
			"param": isParam,
		}, nil
	case *types.Func:
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": e.typeID(o.Type()),
		}, nil
	case *types.Const:
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  e.typeID(o.Type()),
			"value": o.Val().ExactString(),
		}, nil
	case *types.TypeName:
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": e.typeID(o.Type()),
		}, nil
	case *types.Builtin:
		// Builtins have no meaningful single type (len, unsafe.Sizeof);
		// the name is the whole description.
		return map[string]any{
			"kind": "builtin",
			"name": o.Name(),
		}, nil
	case *types.Nil:
		return map[string]any{
			"kind": "nil",
			"name": o.Name(),
		}, nil
	default:
		return nil, fmt.Errorf("emit: unsupported object %T %s", obj, obj.Name())
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
