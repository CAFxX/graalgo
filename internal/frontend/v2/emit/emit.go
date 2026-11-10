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
	"strconv"

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
	// typeErr records the first unhandled type met during emission
	// (see typeID). Emit fails loudly on it instead of returning a
	// document that misdescribes the type.
	typeErr error
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
	// Fail loud on any unhandled type met while emitting funcs, globals,
	// or the tables themselves: the document is never returned with a
	// misdescribed type in it.
	if e.typeErr != nil {
		return nil, e.typeErr
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
			"name":    pkg.Name,
			"files":   baseNames,
			"lang":    "go1.27",
			"imports": e.emitImports(pkg),
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

// emitImports renders the package's import list: one entry per distinct
// (local name, import path) pair, in (path, name) order. The local name
// is the explicit alias when the file renames the import, else the
// imported package's declared name, taken from the type-checked package
// — so "gopkg.in/yaml.v2" maps to "yaml", not to the path base "v2".
// Without this list a pkgref node's local name ("f") cannot be resolved
// to the package it denotes, and two packages exporting same-named,
// same-signature funcs are indistinguishable in the document.
func (e *Emitter) emitImports(pkg *ast.Package) []any {
	type imp struct{ name, path string }
	seen := map[imp]bool{}
	var list []imp
	names := make([]string, 0, len(pkg.Files))
	for name := range pkg.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	// Declared names of the directly imported packages, by path.
	imported := map[string]string{}
	if e.info != nil && e.info.Pkg != nil {
		for _, p := range e.info.Pkg.Imports() {
			imported[p.Path()] = p.Name()
		}
	}
	for _, name := range names {
		for _, decl := range pkg.Files[name].Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.IMPORT {
				continue
			}
			for _, spec := range gd.Specs {
				is, ok := spec.(*ast.ImportSpec)
				if !ok {
					continue
				}
				path, err := strconv.Unquote(is.Path.Value)
				if err != nil {
					path = is.Path.Value
				}
				local := ""
				switch {
				case is.Name != nil:
					local = is.Name.Name
				case imported[path] != "":
					local = imported[path]
				default:
					local = fileBase(path)
				}
				im := imp{name: local, path: path}
				if seen[im] {
					continue
				}
				seen[im] = true
				list = append(list, im)
			}
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].path != list[j].path {
			return list[i].path < list[j].path
		}
		return list[i].name < list[j].name
	})
	out := []any{}
	for _, im := range list {
		out = append(out, map[string]any{"name": im.name, "path": im.path})
	}
	return out
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
			"kind":    "func",
			"name":    o.Name(),
			"type":    e.typeID(o.Type()),
			"pkgpath": pkgPath(o.Pkg()),
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
// An unhandled type records the first error in e.typeErr and yields a
// placeholder id; Emit fails loudly on it, so no document containing a
// misdescribed type can ever be returned. The placeholder keeps ids
// stable so the recursion guard above still works.
func (e *Emitter) typeID(t types.Type) int {
	if id, ok := e.typeIDs[t]; ok {
		return id
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	desc, err := e.describeType(t)
	if err != nil {
		if e.typeErr == nil {
			e.typeErr = err
		}
		e.types[id] = map[string]any{
			"kind":   "unhandled",
			"detail": map[string]any{"type": fmt.Sprintf("%v", t)},
		}
		return id
	}
	e.types[id] = desc
	return id
}

// describeType renders one type for the type table. Every types.Type the
// checker can produce gets its own kind here: a type with no case fails
// loudly (via typeID/Emit) instead of silently misdescribing itself as
// "basic", which a backend trusting kind:basic would miscompile.
func (e *Emitter) describeType(t types.Type) (map[string]any, error) {
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
		}, nil
	case *types.Pointer:
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}, nil
	case *types.Slice:
		return map[string]any{
			"kind":   "slice",
			"detail": map[string]any{"elem": e.typeID(tt.Elem())},
		}, nil
	case *types.Array:
		return map[string]any{
			"kind":   "array",
			"detail": map[string]any{"len": tt.Len(), "elem": e.typeID(tt.Elem())},
		}, nil
	case *types.Map:
		return map[string]any{
			"kind": "map",
			"detail": map[string]any{
				"key":  e.typeID(tt.Key()),
				"elem": e.typeID(tt.Elem()),
			},
		}, nil
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
		}, nil
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
		typeparams := []any{}
		if tps := tt.TypeParams(); tps != nil {
			for i := 0; i < tps.Len(); i++ {
				typeparams = append(typeparams, e.describeTypeParam(tps.At(i)))
			}
		}
		return map[string]any{
			"kind": "signature",
			"detail": map[string]any{
				"params":     params,
				"results":    results,
				"variadic":   tt.Variadic(),
				"typeparams": typeparams,
			},
		}, nil
	case *types.Named:
		// Methods are part of the named type's identity: a backend
		// resolving a method call from the type table needs them here.
		// The "type" of each entry is the method's full signature id —
		// the same id the funcdecl for a same-package method carries
		// as "sig" — so methods join to their declarations without
		// an object id (cross-package methods have no declaration
		// in this document at all).
		methods := []any{}
		for i := 0; i < tt.NumMethods(); i++ {
			m := tt.Method(i)
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"type": e.typeID(m.Type()),
			})
		}
		typeparams := []any{}
		if tps := tt.TypeParams(); tps != nil {
			for i := 0; i < tps.Len(); i++ {
				typeparams = append(typeparams, e.describeTypeParam(tps.At(i)))
			}
		}
		typeargs := []any{}
		if tas := tt.TypeArgs(); tas != nil {
			for i := 0; i < tas.Len(); i++ {
				typeargs = append(typeargs, e.typeID(tas.At(i)))
			}
		}
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    methods,
				"typeparams": typeparams,
				"typeargs":   typeargs,
			},
		}, nil
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
		}, nil
	case *types.Interface:
		// Explicit methods only: NumMethods counts the whole method
		// set (including methods promoted from embedded interfaces),
		// while ExplicitMethod(i) is valid only for the explicit
		// ones — ranging NumMethods with ExplicitMethod panics on an
		// interface with embedded interfaces. Promoted methods stay
		// reachable through the embedded type's own method list.
		methods := []any{}
		for i := 0; i < tt.NumExplicitMethods(); i++ {
			m := tt.ExplicitMethod(i)
			methods = append(methods, map[string]any{
				"name": m.Name(),
				"type": e.typeID(m.Type()),
			})
		}
		// Embedded types carry the rest of the method set: an
		// embedded interface's methods are reachable through its own
		// type id, and a union constraint (~int | ~string) is an
		// embedded *types.Union.
		embedded := []any{}
		for i := 0; i < tt.NumEmbeddeds(); i++ {
			embedded = append(embedded, map[string]any{
				"type": e.typeID(tt.EmbeddedType(i)),
			})
		}
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  methods,
				"embedded": embedded,
			},
		}, nil
	case *types.Alias:
		// Aliases are always materialized in go1.27 (gotypesalias
		// is Removed: 27), so `type A = B` reaches the emitter as
		// *types.Alias in ordinary code — never as "basic".
		name := ""
		if obj := tt.Obj(); obj != nil {
			name = obj.Name()
		}
		return map[string]any{
			"kind": "alias",
			"detail": map[string]any{
				"name":       name,
				"rhs":        e.typeID(tt.Rhs()),
				"underlying": e.typeID(tt.Underlying()),
			},
		}, nil
	case *types.TypeParam:
		return map[string]any{
			"kind":   "typeparam",
			"detail": e.describeTypeParam(tt),
		}, nil
	case *types.Union:
		terms := []any{}
		for i := 0; i < tt.Len(); i++ {
			term := tt.Term(i)
			terms = append(terms, map[string]any{
				"type":  e.typeID(term.Type()),
				"tilde": term.Tilde(),
			})
		}
		return map[string]any{
			"kind":   "union",
			"detail": map[string]any{"terms": terms},
		}, nil
	case *types.Tuple:
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
		}, nil
	default:
		return nil, fmt.Errorf("emit: unsupported type %T (%v)", t, t)
	}
}

// describeTypeParam renders one type parameter: its name, its
// constraint type, and its index in the declaring list.
func (e *Emitter) describeTypeParam(tp *types.TypeParam) map[string]any {
	name := ""
	if obj := tp.Obj(); obj != nil {
		name = obj.Name()
	}
	return map[string]any{
		"name":       name,
		"constraint": e.typeID(tp.Constraint()),
		"index":      tp.Index(),
	}
}

// pkgPath returns the import path of a package, or "" for universe-scope
// objects (builtins, nil) whose Pkg is nil.
func pkgPath(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	return pkg.Path()
}

func basicClass(b *types.Basic) string {
	// unsafe.Pointer carries no Info bits (Info 0), so detect it by kind
	// before consulting Info: it is the only basic type GoKind.fromBasic
	// accepts under class "pointer".
	if b.Kind() == types.UnsafePointer {
		return "pointer"
	}
	// Test the Info bits directly. Do NOT mask with IsNumeric first: the
	// mask strips IsBoolean and IsUnsigned, which made the bool and uint
	// arms dead (bool came out "unknown", every unsigned int "int").
	// IsUnsigned must be tested before IsInteger because unsigned types
	// carry both bits. Untyped kinds land in their base class (untyped
	// bool -> "bool", untyped int -> "int"), which is what
	// GoKind.fromBasic accepts; only untyped nil (no other bit set) falls
	// through to "unknown".
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
	case info&types.IsString != 0:
		return "string"
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
