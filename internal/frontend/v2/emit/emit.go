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
		if _, err := e.declareObject(obj); err != nil {
			return nil, err
		}
	}

	for _, fd := range decls {
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
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
		Globals: []any{},
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

// describeObject builds the object-table entry for obj.
//
// Every object kind the checker can produce must be handled explicitly:
// the old default branch emitted every unknown object as a value-less
// "var", silently dropping constants (e.g. `const k = 42`, `math.Pi`,
// `true`) and type names (e.g. `time.Duration` in a conversion).
// Anything still unhandled is a loud error, never a guess.
func (e *Emitter) describeObject(obj types.Object, isParam bool) (map[string]any, error) {
	typ, err := e.typeID(obj.Type())
	if err != nil {
		return nil, err
	}
	switch o := obj.(type) {
	case *types.Var:
		return map[string]any{
			"kind":  "var",
			"name":  o.Name(),
			"type":  typ,
			"param": isParam,
		}, nil
	case *types.Func:
		return map[string]any{
			"kind": "func",
			"name": o.Name(),
			"type": typ,
		}, nil
	case *types.Const:
		// Inline the constant value: uses of the constant refer to this
		// object id, and the backend substitutes the recorded value.
		// o.Val() is the constant's declared value (go/types guarantees
		// it is non-nil for *types.Const).
		return map[string]any{
			"kind":  "const",
			"name":  o.Name(),
			"type":  typ,
			"value": o.Val().ExactString(),
		}, nil
	case *types.TypeName:
		// A type name in value position (e.g. the fun of the conversion
		// time.Duration(x)) denotes a type reference.
		return map[string]any{
			"kind": "typename",
			"name": o.Name(),
			"type": typ,
		}, nil
	default:
		return nil, fmt.Errorf("emit: unsupported object kind %T (%s)", obj, obj.Name())
	}
}

// typeID assigns a type-table id, registering before describing (recursion).
func (e *Emitter) typeID(t types.Type) (int, error) {
	if id, ok := e.typeIDs[t]; ok {
		return id, nil
	}
	id := len(e.types)
	e.typeIDs[t] = id
	e.types = append(e.types, nil) // placeholder for recursion
	desc, err := e.describeType(t)
	if err != nil {
		return 0, err
	}
	e.types[id] = desc
	return id, nil
}

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
		params := []any{}
		for i := 0; i < tt.Params().Len(); i++ {
			p := tt.Params().At(i)
			pt, err := e.typeID(p.Type())
			if err != nil {
				return nil, err
			}
			params = append(params, map[string]any{
				"name": p.Name(),
				"type": pt,
			})
		}
		results := []any{}
		for i := 0; i < tt.Results().Len(); i++ {
			r := tt.Results().At(i)
			rt, err := e.typeID(r.Type())
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{
				"name": r.Name(),
				"type": rt,
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
			ft, err := e.typeID(f.Type())
			if err != nil {
				return nil, err
			}
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     ft,
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
		// Aliases are transparent: `any` is interface{}, `byte` is uint8,
		// `rune` is int32, and `type A = int` is int. Unaliasing loses
		// nothing semantically significant (the alias name is not part of
		// the type's meaning), so describe the actual type instead of
		// failing or emitting a bogus "unknown". This is structural, not
		// name-keyed: every alias is resolved the same way.
		return e.describeType(types.Unalias(t))
	case *types.Tuple:
		// Tuples surface as the type of a call expression: go/types models
		// a call's results as a tuple (empty for void calls like
		// println(x)). Describe it structurally — element type ids, empty
		// for no results — instead of a bogus "unknown".
		elems := []any{}
		for i := 0; i < tt.Len(); i++ {
			et, err := e.typeID(tt.At(i).Type())
			if err != nil {
				return nil, err
			}
			elems = append(elems, et)
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"elems": elems},
		}, nil
	default:
		// No silent "basic"/"unknown" fallback: an unhandled type kind
		// (type parameters, unions, aliases, ...) would be a lie in the
		// type table. Fail loudly instead.
		return nil, fmt.Errorf("emit: unsupported type kind %T (%s)", t, t.String())
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
