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
	"os"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

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
	// err records the first type-emission error. Type emission has no
	// error channel (typeID is called from expression emission
	// throughout nodes.go), so unsupported types are recorded here and
	// surfaced by emitDocument after emission — a loud failure, never
	// a silent mis-emission.
	err error
	// srcCache caches source bytes per filename for rune-based
	// positions (spec §3). A nil entry is a cached read failure.
	srcCache map[string][]byte
}

// Emit runs stage 3 and returns the in-memory program document. The
// document is consumed directly by the backend via the Go→Java API;
// it is never serialized to JSON.
func Emit(cr *checker.Result, pkg *ast.Package, files []string) (*Document, error) {
	e := &Emitter{
		fset:     cr.Fset,
		info:     cr,
		files:    files,
		typeIDs:  make(map[types.Type]int),
		objIDs:   make(map[types.Object]int),
		srcCache: make(map[string][]byte),
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

	for _, fd := range decls {
		fn, err := e.emitFuncDecl(fd)
		if err != nil {
			return nil, err
		}
		funcs = append(funcs, fn)
	}
	if e.err != nil {
		// A type the emitter cannot describe (type parameter, union,
		// ...) was recorded during emission: fail loudly instead of
		// returning a document with bogus type entries. Structural
		// emit errors above take precedence — e.g. a generic
		// instantiation still reports "unsupported expr", not the
		// recorded type error.
		return nil, e.err
	}

	// Toolchain versions come from the real toolchain and the
	// checker's configured language version — never hardcoded.
	release, lang := goRelease()
	if e.info.GoVersion != "" {
		lang = e.info.GoVersion
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
			"goroot":  release,
		},
		Package: map[string]any{
			"name":  pkg.Name,
			"files": baseNames,
			"lang":  lang,
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
	// file indexes package.files; a node whose file is not in the
	// list is -1, never a silent alias for file 0.
	fileIdx := -1
	for i, f := range e.files {
		if f == p.Filename {
			fileIdx = i
			break
		}
	}
	// col is rune-based and len is the rune length (spec §3).
	col, length := e.runePosLen(p, end)
	return map[string]any{
		"file": fileIdx,
		"line": p.Line,
		"col":  col,
		"off":  p.Offset,
		"len":  length,
	}
}

// runePosLen returns the rune-based column and rune length for the
// node, per spec §3. token.Position.Column is byte-based, so the
// column is recomputed from the source line and the length from the
// source bytes. Source files are read once per file and cached; when
// the source cannot be read — e.g. an in-memory-only parse — it falls
// back to the byte-based values from go/token.
func (e *Emitter) runePosLen(p, end token.Position) (col, length int) {
	src, ok := e.sourceBytes(p.Filename)
	if !ok || p.Offset < 0 || end.Offset < p.Offset || end.Offset > len(src) {
		return p.Column, end.Offset - p.Offset
	}
	lineStart := p.Offset - (p.Column - 1)
	if lineStart < 0 || lineStart > p.Offset {
		return p.Column, end.Offset - p.Offset
	}
	return utf8.RuneCount(src[lineStart:p.Offset]) + 1, utf8.RuneCount(src[p.Offset:end.Offset])
}

// sourceBytes returns the source bytes of the named file, reading it
// once and caching the result (nil slice = cached read failure).
func (e *Emitter) sourceBytes(filename string) ([]byte, bool) {
	if e.srcCache == nil {
		e.srcCache = make(map[string][]byte)
	}
	if src, ok := e.srcCache[filename]; ok {
		return src, src != nil
	}
	src, err := os.ReadFile(filename)
	if err != nil {
		e.srcCache[filename] = nil
		return nil, false
	}
	e.srcCache[filename] = src
	return src, true
}

// goRelease returns the toolchain version that built this binary
// (runtime.Version, e.g. "go1.27.1") and the language version it
// implies ("go1.27").
func goRelease() (release, lang string) {
	release = runtime.Version()
	lang = release
	if strings.HasPrefix(release, "go") {
		rest := strings.TrimPrefix(release, "go")
		if parts := strings.SplitN(rest, ".", 3); len(parts) >= 2 {
			lang = "go" + parts[0] + "." + parts[1]
		}
	}
	return release, lang
}

// failf records the emitter's first error (see Emitter.err).
func (e *Emitter) failf(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf(format, args...)
	}
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
// Aliases canonicalize to the aliased type (types.Unalias, the single
// choke point for wire type ids): every spelling of one Go type — e.g.
// the universe alias `any` = interface{}, or a declared `type A = int`
// — maps to a single entry. In gc an alias IS the aliased type; a
// per-spelling entry would break type identity.
func (e *Emitter) typeID(t types.Type) int {
	t = types.Unalias(t)
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
			"detail": map[string]any{"elem": e.typeID(tt.Elem()), "methods": e.methodSetEntries(tt)},
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
	case *types.Tuple:
		// Call-result type: zero results (a void call) is the empty
		// tuple, N results the N-tuple. Previously mis-emitted as
		// {"kind":"basic","class":"unknown","name":"()"}.
		elems := []any{}
		for i := 0; i < tt.Len(); i++ {
			elems = append(elems, e.typeID(tt.At(i).Type()))
		}
		return map[string]any{
			"kind":   "tuple",
			"detail": map[string]any{"elems": elems},
		}
	case *types.Named:
		return map[string]any{
			"kind": "named",
			"detail": map[string]any{
				"name":       namedName(tt),
				"underlying": e.typeID(tt.Underlying()),
				"methods":    e.methodSetEntries(tt),
			},
		}
	case *types.Struct:
		offsets := structOffsets(tt)
		fields := []any{}
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fields = append(fields, map[string]any{
				"name":     f.Name(),
				"type":     e.typeID(f.Type()),
				"tag":      tt.Tag(i),
				"embedded": f.Embedded(),
				"offset":   offsets[i],
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
				"methods":  e.interfaceMethods(tt),
				"embedded": e.interfaceEmbedded(tt),
			},
		}
	default:
		// TypeParam, Union, and any future go/types kind: v2 has no
		// lowering for these yet, so fail loudly instead of emitting
		// a bogus {"kind":"basic","class":"unknown"} entry. A
		// *types.Tuple is a legitimate call-result type and has its
		// own case above; aliases are canonicalized by
		// types.Unalias in typeID and never reach this case.
		e.failf("emit: unsupported type %T (%s)", t, t.String())
		return map[string]any{
			"kind":   "unsupported",
			"detail": map[string]any{"type": t.String()},
		}
	}
}

// basicClass maps a *types.Basic to the wire class (v1 §4 class set).
// It tests the Info flags individually: the previous form switched on
// b.Info() & types.IsNumeric, whose value can never equal IsBoolean or
// IsUnsigned, so bool and every unsigned kind were misclassified
// (bool → "unknown", uint* → "int").
func basicClass(b *types.Basic) string {
	if b.Kind() == types.UnsafePointer {
		return "pointer"
	}
	info := b.Info()
	switch {
	case info&types.IsBoolean != 0:
		return "bool"
	case info&types.IsInteger != 0:
		if info&types.IsUnsigned != 0 {
			return "uint"
		}
		return "int"
	case info&types.IsFloat != 0:
		return "float"
	case info&types.IsComplex != 0:
		return "complex"
	case info&types.IsString != 0:
		return "string"
	case info&types.IsUntyped != 0:
		return "untyped"
	}
	return "unknown"
}

// methodSetEntries returns the method-table entries for t in
// types.NewMethodSet order (v1/ADR-0003): one entry per method,
// including promoted methods from embedded fields. Each entry is
// {name, obj, recv, index}: obj is the method's object id, recv the
// declared receiver kind ("value"|"pointer"), index the
// receiver-adaptation path — Selection.Index() minus its last entry
// ([] for direct methods, [0] for a method promoted via embedded
// field 0). On a named value type this is the value-receiver set; on
// a pointer type it is the full *T set (value + pointer receivers).
func (e *Emitter) methodSetEntries(t types.Type) []any {
	ms := types.NewMethodSet(t)
	methods := make([]any, 0, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		fn, ok := sel.Obj().(*types.Func)
		if !ok {
			// Method-set entries are always *types.Func; if that
			// ever stops holding, fail loudly rather than minting
			// a bogus entry.
			e.failf("emit: method %s of %s is not a *types.Func (%T)", sel.Obj().Name(), t.String(), sel.Obj())
			continue
		}
		recv := "value"
		if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
			if _, isPtr := sig.Recv().Type().(*types.Pointer); isPtr {
				recv = "pointer"
			}
		}
		idx := sel.Index()
		path := make([]int, 0, len(idx))
		if len(idx) > 0 {
			path = append(path, idx[:len(idx)-1]...)
		}
		methods = append(methods, map[string]any{
			"name":  fn.Name(),
			"obj":   e.declareObject(fn),
			"recv":  recv,
			"index": path,
		})
	}
	return methods
}

// interfaceMethods returns the method-table entries for an interface
// in types.NewMethodSet order (v1 §4). Interface-declared methods
// have no funcdecl, so each entry carries the method signature's type
// id instead of an object id: {name, sig}.
func (e *Emitter) interfaceMethods(t *types.Interface) []any {
	ms := types.NewMethodSet(t)
	methods := make([]any, 0, ms.Len())
	for i := 0; i < ms.Len(); i++ {
		fn, ok := ms.At(i).Obj().(*types.Func)
		if !ok {
			e.failf("emit: method %s of %s is not a *types.Func (%T)", ms.At(i).Obj().Name(), t.String(), ms.At(i).Obj())
			continue
		}
		methods = append(methods, map[string]any{
			"name": fn.Name(),
			"sig":  e.typeID(fn.Type()),
		})
	}
	return methods
}

// interfaceEmbedded returns the type ids of the interfaces embedded in t.
func (e *Emitter) interfaceEmbedded(t *types.Interface) []any {
	embedded := make([]any, 0, t.NumEmbeddeds())
	for i := 0; i < t.NumEmbeddeds(); i++ {
		embedded = append(embedded, e.typeID(t.EmbeddedType(i)))
	}
	return embedded
}

// structOffsets returns the byte offsets of t's fields under the gc
// sizes for the host architecture. -1 marks every field when offsets
// cannot be computed: unknown target sizes, or a field type containing
// a type parameter (go/types size computation panics on those).
func structOffsets(t *types.Struct) []int64 {
	offsets := make([]int64, t.NumFields())
	for i := range offsets {
		offsets[i] = -1
	}
	sizes := types.SizesFor("gc", runtime.GOARCH)
	if sizes == nil {
		return offsets
	}
	vars := make([]*types.Var, t.NumFields())
	for i := 0; i < t.NumFields(); i++ {
		if typeHasTypeParam(t.Field(i).Type()) {
			return offsets
		}
		vars[i] = t.Field(i)
	}
	return sizes.Offsetsof(vars)
}

// typeHasTypeParam reports whether t contains a *types.TypeParam
// (mirrors v1's sizesWouldPanic walk: sizes panic on type parameters).
func typeHasTypeParam(t types.Type) bool {
	seen := make(map[types.Type]bool)
	var walk func(types.Type) bool
	walk = func(t types.Type) bool {
		if t == nil {
			return false
		}
		if _, ok := t.(*types.TypeParam); ok {
			return true
		}
		if seen[t] {
			return false
		}
		seen[t] = true
		switch u := t.(type) {
		case *types.Named:
			return walk(u.Underlying())
		case *types.Array:
			return walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if walk(u.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
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
