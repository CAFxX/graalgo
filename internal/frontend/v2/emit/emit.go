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

	// Pass 1: declare all top-level funcs and globals (assigning object
	// ids) so bodies and initializers can reference them.
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			obj := e.info.Info.Defs[d.Name]
			if obj == nil {
				return nil, fmt.Errorf("emit: no object for func %s", d.Name.Name)
			}
			// Reject a generic function before declareObject
			// describes its signature: describeType would reject
			// the signature too (backstop), but without the
			// function name. emitFuncDecl repeats this check as
			// the emitter-side precondition.
			if fn, ok := obj.(*types.Func); ok {
				if sig, ok := fn.Type().(*types.Signature); ok && sig.TypeParams().Len() > 0 {
					return nil, fmt.Errorf("emit: unsupported generic func %s (type parameters not supported)", fn.Name())
				}
			}
			if _, err := e.declareObject(obj); err != nil {
				return nil, err
			}
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				// Type declarations are otherwise invisible to the
				// emitter (only referenced types reach the type table),
				// so a generic type declared here would be silently
				// dropped — or, if referenced, fail only deep inside
				// describeType. Reject it at the declaration instead.
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						return nil, fmt.Errorf("emit: unsupported type spec %T", spec)
					}
					if err := e.checkTypeDecl(ts); err != nil {
						return nil, err
					}
				}
				continue
			}
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

// checkTypeDecl rejects a type declaration with type parameters: the v2
// emitter has no representation for generics, so a generic type must fail
// loudly at its declaration instead of being silently dropped (unreferenced)
// or failing later, in another language, at decode (referenced).
func (e *Emitter) checkTypeDecl(ts *ast.TypeSpec) error {
	obj := e.info.Info.Defs[ts.Name]
	if obj == nil {
		return fmt.Errorf("emit: no object for type %s", ts.Name.Name)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return fmt.Errorf("emit: %s is not a type name", ts.Name.Name)
	}
	switch t := tn.Type().(type) {
	case *types.Named:
		if t.TypeParams().Len() > 0 {
			return fmt.Errorf("emit: unsupported generic type %s (type parameters not supported)", tn.Name())
		}
	case *types.Alias:
		if t.TypeParams().Len() > 0 {
			return fmt.Errorf("emit: unsupported generic type %s (type parameters not supported)", tn.Name())
		}
	}
	return nil
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
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  tid,
			"param": isParam,
		}, nil
	case *types.Func:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": tid,
		}, nil
	case *types.Const:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  tid,
			"value": o.Val().ExactString(),
		}, nil
	case *types.TypeName:
		tid, err := e.typeID(o.Type())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": tid,
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
// A type that cannot be described (describeType error) fails the whole
// emission: the id is unregistered and the error propagates to Emit.
// The nil placeholder slot is left behind in e.types — the document is
// discarded on error and the Emitter is single-use, so it is never read.
func (e *Emitter) typeID(t types.Type) (int, error) {
	if id, ok := e.typeIDs[t]; ok {
		return id, nil
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	desc, err := e.describeType(t)
	if err != nil {
		delete(e.typeIDs, t)
		return 0, err
	}
	e.types[id] = desc
	return id, nil
}

// describeType describes one type for the type table. Every go/types kind
// the checker can produce gets its own kind here. A kind with no case
// (TypeParam, Union, ...) fails loudly instead of being emitted as
// a bogus basic/unknown entry: the Java consumer throws on class
// "unknown", so the old default produced documents that were undecodable
// while the emitter reported success.
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
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "pointer",
			"detail": map[string]any{"elem": elem},
		}, nil
	case *types.Slice:
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "slice",
			"detail": map[string]any{"elem": elem},
		}, nil
	case *types.Array:
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "array",
			"detail": map[string]any{"len": tt.Len(), "elem": elem},
		}, nil
	case *types.Map:
		key, err := e.typeID(tt.Key())
		if err != nil {
			return nil, err
		}
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "map",
			"detail": map[string]any{
				"key":  key,
				"elem": elem,
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
		elem, err := e.typeID(tt.Elem())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind":   "chan",
			"detail": map[string]any{"dir": dir, "elem": elem},
		}, nil
	case *types.Signature:
		// A signature with type parameters is a generic function: it
		// can only arise from a generic declaration, which emitFuncDecl
		// rejects with the function name. This is the backstop for a
		// generic signature reached any other way (e.g. through a
		// generic method receiver or a value of generic function type).
		if tt.TypeParams().Len() > 0 {
			return nil, fmt.Errorf("emit: unsupported generic signature %s (type parameters not supported)", tt.String())
		}
		params := []any{}
		for i := 0; i < tt.Params().Len(); i++ {
			p := tt.Params().At(i)
			pid, err := e.typeID(p.Type())
			if err != nil {
				return nil, err
			}
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": pid,
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			rid, err := e.typeID(r.Type())
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": rid,
			})
		}
		return map[string]any{
			"kind": "signature",
			"detail": map[string]any{
				"params":   params,
				"results":  results,
				"variadic": tt.Variadic(),
			},
		}, nil
	case *types.Named:
		underlying, err := e.typeID(tt.Underlying())
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": underlying,
				"methods":    []any{},
			},
		}, nil
	case *types.Struct:
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fid, err := e.typeID(f.Type())
			if err != nil {
				return nil, err
			}
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     fid,
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
		return map[string]any{
			"kind": "interface",
			"detail": map[string]any{
				"empty":    tt.Empty(),
				"methods":  []any{},
				"embedded": []any{},
			},
		}, nil
	case *types.Alias:
		// An alias IS its right-hand side (gc treats them as
		// identical), so describe the RHS instead of the alias: a
		// plain alias such as `type A = int` then emits exactly as
		// `int` does. The old default emitted the alias itself as
		// basic/unknown, which is undecodable. A generic alias
		// (`type L[T any] = []T`) still fails loudly: its RHS
		// contains the type parameter, which the default case
		// rejects (and its declaration is rejected earlier by
		// checkTypeDecl).
		return e.describeType(tt.Rhs())
	case *types.Tuple:
		// A tuple is the type of a call expression: the empty tuple
		// `()` for a call returning no values, `(int, string)` for
		// a multi-value call. It must be represented, not rejected:
		// every void call's type flows through exprType, so
		// rejecting tuples would reject every program containing
		// a call to a void function. The Java consumer maps an
		// unknown type kind to INTERFACE (NodeFactoryV2
		// goKindForTypeId default case), so a "tuple" entry
		// decodes; the old basic/unknown entry for `()` instead
		// threw in fromBasic for every non-builtin void call.
		elems := []any{}
		for i := 0; i < tt.Len(); i++ {
			eid, err := e.typeID(tt.At(i).Type())
			if err != nil {
				return nil, err
			}
			elems = append(elems, eid)
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"elems": elems},
		}, nil
	default:
		return nil, fmt.Errorf("emit: unsupported type %T %s", t, t.String())
	}
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
