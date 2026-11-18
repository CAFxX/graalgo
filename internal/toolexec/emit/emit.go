// Package emit lowers a type-checked package (go/ast + go/types) to GIR
// (docs/arch/toolexec-design.md §3.1): a resolved typed AST in which every
// identifier is resolved (local slot, package object, imported symbol),
// every expression carries its type, constants are folded, selectors are
// resolved to field paths and static/interface method calls, implicit
// conversions to interface and channel types are explicit, closures carry
// their capture lists, and generic code stays uninstantiated with explicit
// type arguments at instantiation sites.
//
// This is the W2 emitter: it covers the language constructs whose lowering
// is mechanical. Anything it cannot lower faithfully is reported as an
// unsupported construct (Stats.Unsupported) and makes Emit fail; it never
// emits a placeholder. W3 completes it.
package emit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"graalgo-toolexec/ir"
)

// Config describes one package to emit.
type Config struct {
	Fset   *token.FileSet
	Pkg    *types.Package
	Info   *types.Info // needs Types, Defs, Uses, Implicits, Selections, Instances, InitOrder, FileVersions
	Files  []*ast.File // in command-line order
	Lang   string      // -lang
	Std    bool        // -std
	GOOS   string
	GOARCH string
	// FileName maps a source position's file name to the name recorded in
	// the IR (the -trimpath rewrite); nil keeps names unchanged.
	FileName func(string) string
}

// Stats counts what the emitter saw: every AST node kind in function
// bodies and initializers, and the unsupported constructs by reason.
type Stats struct {
	Nodes       map[string]int // ast node type name -> occurrences
	NodesOK     map[string]int // ... in function bodies emitted completely
	Unsupported map[string]int // reason -> occurrences
	Funcs       int            // function bodies (incl. literals) attempted
	FuncsOK     int            // ... emitted completely
	MaxDepth    int            // deepest node nesting emitted
}

func newStats() *Stats {
	return &Stats{Nodes: map[string]int{}, NodesOK: map[string]int{}, Unsupported: map[string]int{}}
}

// Add accumulates o into s.
func (s *Stats) Add(o *Stats) {
	for k, v := range o.Nodes {
		s.Nodes[k] += v
	}
	for k, v := range o.NodesOK {
		s.NodesOK[k] += v
	}
	for k, v := range o.Unsupported {
		s.Unsupported[k] += v
	}
	s.Funcs += o.Funcs
	s.FuncsOK += o.FuncsOK
	s.MaxDepth = max(s.MaxDepth, o.MaxDepth)
}

// unsupported is the panic value that aborts one function body.
type unsupported struct {
	reason string
	pos    token.Pos
}

// Error reports unsupported constructs.
type Error struct {
	First string
	Count int
}

func (e *Error) Error() string {
	if e.Count == 1 {
		return "unsupported: " + e.First
	}
	return fmt.Sprintf("unsupported: %s (and %d more)", e.First, e.Count-1)
}

type symKey struct {
	pkg, name string
	kind      ir.SymKind
}

type emitter struct {
	cfg   Config
	file  *ir.File
	stats *Stats
	sizes types.Sizes
	keyer ir.TypeKeyer

	typeIDs     map[string]ir.TypeID
	typeOf      []types.Type // TypeID-1 -> the go/types type it was interned from
	symIDs      map[symKey]uint32
	objIDs      map[types.Object]uint32
	funcIDs     map[*types.Func]uint32
	localTypes  map[*types.TypeName]string
	localTargs  map[*types.TypeName][]types.Type // local types of generic functions
	tparamOwner map[*types.TypeParam]string
	typedefs    []*types.TypeName
	linknames   map[string]string

	errFirst string
	errCount int
}

// Emit lowers the package. On unsupported constructs it returns the
// statistics and an *Error; the File is then incomplete and must not be
// used.
func Emit(cfg Config) (*ir.File, *Stats, error) {
	e := &emitter{
		cfg:         cfg,
		file:        &ir.File{GoVersion: cfg.Lang, GOOS: cfg.GOOS, GOARCH: cfg.GOARCH},
		stats:       newStats(),
		sizes:       types.SizesFor("gc", cfg.GOARCH),
		typeIDs:     map[string]ir.TypeID{},
		symIDs:      map[symKey]uint32{},
		objIDs:      map[types.Object]uint32{},
		funcIDs:     map[*types.Func]uint32{},
		localTypes:  map[*types.TypeName]string{},
		localTargs:  map[*types.TypeName][]types.Type{},
		tparamOwner: map[*types.TypeParam]string{},
		linknames:   map[string]string{},
	}
	e.keyer = ir.TypeKeyer{LocalName: e.localTypeName, TypeParamOwner: e.typeParamOwner,
		LocalTargs: func(tn *types.TypeName) []types.Type { return e.localTargs[tn] }}
	if e.sizes == nil {
		return nil, nil, fmt.Errorf("emit: no gc sizes for GOARCH %q", cfg.GOARCH)
	}
	e.run()
	if e.errCount > 0 {
		return nil, e.stats, &Error{First: e.errFirst, Count: e.errCount}
	}
	return e.file, e.stats, nil
}

func (e *emitter) unsupportedf(pos token.Pos, format string, a ...any) {
	panic(unsupported{reason: fmt.Sprintf(format, a...), pos: pos})
}

func (e *emitter) record(u unsupported) {
	e.stats.Unsupported[u.reason]++
	if e.errCount == 0 {
		e.errFirst = fmt.Sprintf("%s: %s", e.cfg.Fset.Position(u.pos), u.reason)
	}
	e.errCount++
}

// symbolName is gc's symbol name of a package-level function or method,
// without the package path.
func symbolName(f *types.Func) string {
	sig := f.Signature()
	if sig.Recv() == nil {
		return f.Name()
	}
	rt := sig.Recv().Type()
	ptr := false
	if p, ok := types.Unalias(rt).(*types.Pointer); ok {
		rt, ptr = p.Elem(), true
	}
	base := "?"
	if n, ok := types.Unalias(rt).(*types.Named); ok {
		base = n.Origin().Obj().Name()
	}
	if ptr {
		return "(*" + base + ")." + f.Name()
	}
	return base + "." + f.Name()
}

func pkgPath(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Path()
}

func (e *emitter) sym(pkg, name string, kind ir.SymKind) uint32 {
	k := symKey{pkg, name, kind}
	if id, ok := e.symIDs[k]; ok {
		return id
	}
	id := uint32(len(e.file.SymRefs))
	e.symIDs[k] = id
	e.file.SymRefs = append(e.file.SymRefs, ir.SymRef{Pkg: pkg, Name: name, Kind: kind})
	return id
}

func (e *emitter) methodSym(f *types.Func) uint32 {
	return e.sym(pkgPath(f.Pkg()), symbolName(f), ir.SymKindMethod)
}

func (e *emitter) objSym(obj types.Object) uint32 {
	var kind ir.SymKind
	switch obj.(type) {
	case *types.Func:
		kind = ir.SymKindFunc
	case *types.Var:
		kind = ir.SymKindVar
	case *types.Const:
		kind = ir.SymKindConst
	case *types.TypeName:
		kind = ir.SymKindTypeName
	}
	name := obj.Name()
	if tn, ok := obj.(*types.TypeName); ok {
		if l := e.localTypes[tn]; l != "" {
			name = l
		}
	}
	return e.sym(pkgPath(obj.Pkg()), name, kind)
}

func (e *emitter) localTypeName(tn *types.TypeName) string { return e.localTypes[tn] }

func (e *emitter) typeParamOwner(tp *types.TypeParam) string {
	if o, ok := e.tparamOwner[tp]; ok {
		return o
	}
	// A type parameter of an imported generic declaration that was not
	// registered through its owner: qualify by package and name.
	return pkgPath(tp.Obj().Pkg()) + ".?" + tp.Obj().Name()
}

// registerTypeParams records the owner symbol of a generic declaration's
// type parameters, for their canonical keys.
func (e *emitter) registerTypeParams(list *types.TypeParamList, owner string) {
	for i := range list.Len() {
		if _, ok := e.tparamOwner[list.At(i)]; !ok {
			e.tparamOwner[list.At(i)] = owner
		}
	}
}

func (e *emitter) registerFunc(f *types.Func) {
	sig := f.Signature()
	owner := pkgPath(f.Pkg()) + "." + symbolName(f)
	e.registerTypeParams(sig.TypeParams(), owner)
	e.registerTypeParams(sig.RecvTypeParams(), owner)
}

// ---------------------------------------------------------------- types

var basicKinds = map[types.BasicKind]ir.BasicKind{
	types.Bool: ir.BasicKindBool, types.Int: ir.BasicKindInt, types.Int8: ir.BasicKindInt8,
	types.Int16: ir.BasicKindInt16, types.Int32: ir.BasicKindInt32, types.Int64: ir.BasicKindInt64,
	types.Uint: ir.BasicKindUint, types.Uint8: ir.BasicKindUint8, types.Uint16: ir.BasicKindUint16,
	types.Uint32: ir.BasicKindUint32, types.Uint64: ir.BasicKindUint64, types.Uintptr: ir.BasicKindUintptr,
	types.Float32: ir.BasicKindFloat32, types.Float64: ir.BasicKindFloat64,
	types.Complex64: ir.BasicKindComplex64, types.Complex128: ir.BasicKindComplex128,
	types.String: ir.BasicKindString, types.UnsafePointer: ir.BasicKindUnsafePointer,
	types.UntypedBool: ir.BasicKindUntypedBool, types.UntypedInt: ir.BasicKindUntypedInt,
	types.UntypedRune: ir.BasicKindUntypedRune, types.UntypedFloat: ir.BasicKindUntypedFloat,
	types.UntypedComplex: ir.BasicKindUntypedComplex, types.UntypedString: ir.BasicKindUntypedString,
	types.UntypedNil: ir.BasicKindUntypedNil,
}

// typeID interns t in the type table. The entry is reserved before its
// structure is computed, so recursive references (a type parameter whose
// constraint mentions it) resolve.
func (e *emitter) typeID(t types.Type) ir.TypeID {
	if t == nil {
		return 0
	}
	t = types.Unalias(t)
	key := e.keyer.Key(t)
	if id, ok := e.typeIDs[key]; ok {
		return id
	}
	e.file.Types = append(e.file.Types, ir.TypeEntry{Key: key})
	e.typeOf = append(e.typeOf, t)
	id := ir.TypeID(len(e.file.Types))
	e.typeIDs[key] = id
	e.file.Types[id-1].Type = e.typeStructure(t)
	return id
}

func (e *emitter) typeIDs_(list []types.Type) []ir.TypeID {
	out := make([]ir.TypeID, len(list))
	for i, t := range list {
		out[i] = e.typeID(t)
	}
	return out
}

func (e *emitter) tupleIDs(t *types.Tuple) []ir.TypeID {
	out := make([]ir.TypeID, t.Len())
	for i := range t.Len() {
		out[i] = e.typeID(t.At(i).Type())
	}
	return out
}

func (e *emitter) typeListIDs(l *types.TypeList) []ir.TypeID {
	out := make([]ir.TypeID, l.Len())
	for i := range l.Len() {
		out[i] = e.typeID(l.At(i))
	}
	return out
}

func (e *emitter) tparamIDs(l *types.TypeParamList) []ir.TypeID {
	out := make([]ir.TypeID, l.Len())
	for i := range l.Len() {
		out[i] = e.typeID(l.At(i))
	}
	return out
}

func (e *emitter) typeStructure(t types.Type) ir.Type {
	switch t := t.(type) {
	case *types.Basic:
		return &ir.BasicType{Kind: basicKinds[t.Kind()]}
	case *types.Pointer:
		return &ir.PointerType{Elem: e.typeID(t.Elem())}
	case *types.Slice:
		return &ir.SliceType{Elem: e.typeID(t.Elem())}
	case *types.Array:
		return &ir.ArrayType{Len: uint64(t.Len()), Elem: e.typeID(t.Elem())}
	case *types.Map:
		return &ir.MapType{Key: e.typeID(t.Key()), Elem: e.typeID(t.Elem())}
	case *types.Chan:
		dir := map[types.ChanDir]ir.ChanDir{types.SendRecv: ir.ChanDirBoth, types.SendOnly: ir.ChanDirSend, types.RecvOnly: ir.ChanDirRecv}[t.Dir()]
		return &ir.ChanType{Dir: dir, Elem: e.typeID(t.Elem())}
	case *types.Struct:
		fs := make([]ir.Field, t.NumFields())
		for i := range t.NumFields() {
			f := t.Field(i)
			pkg := ""
			if !f.Exported() {
				pkg = pkgPath(f.Pkg())
			}
			fs[i] = ir.Field{Name: f.Name(), Pkg: pkg, Type: e.typeID(f.Type()), Embedded: f.Embedded(), Tag: t.Tag(i)}
		}
		return &ir.StructType{Fields: fs}
	case *types.Signature:
		return &ir.SignatureType{Params: e.tupleIDs(t.Params()), Results: e.tupleIDs(t.Results()), Variadic: t.Variadic()}
	case *types.Interface:
		ms := make([]ir.IfaceMethod, t.NumMethods())
		for i := range t.NumMethods() {
			m := t.Method(i)
			pkg := ""
			if !m.Exported() {
				pkg = pkgPath(m.Pkg())
			}
			ms[i] = ir.IfaceMethod{Name: m.Name(), Pkg: pkg, Sig: e.typeID(m.Type())}
		}
		// Embeddeds: the normalized type set (ir.InterfaceTypeSet), the
		// same value the canonical key encodes: `comparable` when
		// required, then one element for the term list (the type itself
		// for a single non-tilde term, else a Union).
		var emb []ir.TypeID
		ts := ir.InterfaceTypeSet(t)
		if ts.Comparable {
			emb = append(emb, e.typeID(types.Universe.Lookup("comparable").Type()))
		}
		if !ts.All {
			if len(ts.Terms) == 1 && !ts.Terms[0].Tilde() {
				emb = append(emb, e.typeID(ts.Terms[0].Type()))
			} else {
				emb = append(emb, e.typeID(types.NewUnion(ts.Terms)))
			}
		}
		return &ir.InterfaceType{Methods: ms, Embeddeds: emb, Implicit: t.IsImplicit()}
	case *types.Tuple:
		return &ir.TupleType{Elems: e.tupleIDs(t)}
	case *types.Named:
		obj := t.Origin().Obj()
		if obj.Pkg() == e.cfg.Pkg && t.Origin() == t {
			e.noteTypedef(obj)
		}
		if t.Origin() == t && t.TypeParams().Len() > 0 {
			e.registerTypeParams(t.TypeParams(), pkgPath(obj.Pkg())+"."+obj.Name())
		}
		if lt := e.localTargs[obj]; len(lt) > 0 && t.TypeArgs().Len() == 0 {
			return &ir.NamedType{Sym: e.objSym(obj), Targs: e.typeIDs_(lt)}
		}
		return &ir.NamedType{Sym: e.objSym(obj), Targs: e.typeListIDs(t.TypeArgs())}
	case *types.TypeParam:
		return &ir.TypeParamType{Owner: e.sym("", e.typeParamOwner(t), ir.SymKindTypeName), Index: uint64(t.Index()), Name: t.Obj().Name(), Constraint: e.typeID(t.Constraint())}
	case *types.Union:
		terms := make([]ir.Term, t.Len())
		for i := range t.Len() {
			terms[i] = ir.Term{Tilde: t.Term(i).Tilde(), Type: e.typeID(t.Term(i).Type())}
		}
		return &ir.UnionType{Terms: terms}
	}
	panic(fmt.Sprintf("emit: unexpected type %T", t))
}

func (e *emitter) noteTypedef(tn *types.TypeName) {
	if !slices.Contains(e.typedefs, tn) {
		e.typedefs = append(e.typedefs, tn)
	}
}

// ---------------------------------------------------------------- package

func (e *emitter) run() {
	cfg := e.cfg
	pkg := &e.file.Pkg
	pkg.Path = cfg.Pkg.Path()
	pkg.Name = cfg.Pkg.Name()
	pkg.Lang = cfg.Lang
	if cfg.Std {
		pkg.Flags |= 1
	}
	for _, f := range cfg.Files {
		pkg.Files = append(pkg.Files, e.fileName(cfg.Fset.File(f.Pos()).Name()))
	}
	for _, imp := range cfg.Pkg.Imports() {
		pkg.Imports = append(pkg.Imports, imp.Path())
	}
	slices.Sort(pkg.Imports)
	e.scanLinknames()
	e.nameLocalTypes()

	// FUNCINDEX entries for every function declaration, in file order.
	type decl struct {
		fd     *ast.FuncDecl
		obj    *types.Func // nil for init functions without an object
		isInit bool
	}
	var decls, initDecls []decl
	for _, f := range cfg.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj, _ := cfg.Info.Defs[fd.Name].(*types.Func)
			switch {
			case fd.Recv == nil && fd.Name.Name == "init":
				initDecls = append(initDecls, decl{fd, obj, true})
			case obj == nil || fd.Name.Name == "_":
				// blank functions and methods are unreachable
			default:
				decls = append(decls, decl{fd, obj, false})
			}
		}
	}
	decls = append(decls, initDecls...)
	for _, d := range decls {
		if !d.isInit {
			e.registerFunc(d.obj)
			e.funcIDs[d.obj] = uint32(len(e.file.Funcs))
		}
		e.file.Funcs = append(e.file.Funcs, ir.Func{})
		e.file.Bodies = append(e.file.Bodies, nil)
	}
	// OBJECTS: package scope in name order.
	scope := cfg.Pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		e.objIDs[obj] = uint32(len(e.file.Objects))
		e.file.Objects = append(e.file.Objects, ir.Object{})
	}
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		o := &e.file.Objects[e.objIDs[obj]]
		o.Name = name
		o.Fn = -1
		if obj.Exported() {
			o.Flags |= 1
		}
		switch obj := obj.(type) {
		case *types.Const:
			o.Kind = ir.SymKindConst
			o.Type = e.typeID(obj.Type())
			o.Value = constValue(obj.Val())
		case *types.Var:
			o.Kind = ir.SymKindVar
			o.Type = e.typeID(obj.Type())
		case *types.Func:
			o.Kind = ir.SymKindFunc
			o.Type = e.typeID(obj.Type())
			if id, ok := e.funcIDs[obj]; ok {
				o.Fn = int32(id)
			}
		case *types.TypeName:
			o.Kind = ir.SymKindTypeName
			o.Type = e.typeID(obj.Type())
			if !obj.IsAlias() {
				e.noteTypedef(obj)
			}
		}
	}
	initIdx := 0
	for i, d := range decls {
		fn := &e.file.Funcs[i]
		name := ""
		if !d.isInit {
			name = symbolName(d.obj)
		} else {
			name = "init." + strconv.Itoa(initIdx)
			initIdx++
			e.file.Init.Inits = append(e.file.Init.Inits, uint32(i))
		}
		fn.Name = name
		pos := cfg.Fset.Position(d.fd.Pos())
		fn.File, fn.Line = e.fileName(pos.Filename), uint64(pos.Line)
		fn.Linkname = e.linknames[d.fd.Name.Name]
		var sig *types.Signature
		if !d.isInit {
			sig = d.obj.Signature()
			fn.Sig = e.typeID(sig)
			if sig.Recv() != nil {
				fn.Recv = e.typeID(sig.Recv().Type())
				fn.Flags |= 4
				fn.Tparams = e.tparamIDs(sig.RecvTypeParams())
			}
			fn.Tparams = append(fn.Tparams, e.tparamIDs(sig.TypeParams())...)
			if len(fn.Tparams) > 0 {
				fn.Flags |= 2
			}
		} else {
			sig = types.NewSignatureType(nil, nil, nil, nil, nil, false)
			fn.Sig = e.typeID(sig)
			fn.Flags |= 8
		}
		if d.fd.Body == nil {
			fn.Flags |= 1
			continue
		}
		counts := e.countNodes(d.fd.Body)
		fs := e.newFunc(nil, sig, d.fd.Pos())
		e.body(fs, func() {
			if sig.Recv() != nil {
				fs.declareParam(sig.Recv(), 4)
				fs.body.Recv = int32(fs.locals[sig.Recv()])
			}
			for i := range sig.Params().Len() {
				fs.body.Params = append(fs.body.Params, fs.declareParam(sig.Params().At(i), 1))
			}
			for i := range sig.Results().Len() {
				fs.body.Results = append(fs.body.Results, fs.declareParam(sig.Results().At(i), 2))
			}
			fs.body.Body = fs.stmtList(d.fd.Body.List)
		})
		if fs.ok {
			e.file.Bodies[i] = fs.body
			fn.Syms, fn.Objs = fs.refs()
			e.addOK(counts)
		}
	}
	e.emitVarInit()
	e.emitTypedefs()
}

// emitVarInit synthesizes the package-variable initializer function from
// Info.InitOrder (design §3.6).
func (e *emitter) emitVarInit() {
	e.file.Init.VarInit = -1
	order := e.cfg.Info.InitOrder
	if len(order) == 0 {
		return
	}
	idx := len(e.file.Funcs)
	e.file.Funcs = append(e.file.Funcs, ir.Func{Name: "init·vars", Flags: 16, Sig: e.typeID(types.NewSignatureType(nil, nil, nil, nil, nil, false))})
	e.file.Bodies = append(e.file.Bodies, nil)
	fs := e.newFunc(nil, nil, order[0].Rhs.Pos())
	var initCounts []map[string]int
	e.body(fs, func() {
		for _, in := range order {
			initCounts = append(initCounts, e.countNodes(in.Rhs))
			var lhs []ir.Expr
			for _, v := range in.Lhs {
				if v.Name() == "_" {
					lhs = append(lhs, &ir.BlankExpr{Type: e.typeID(v.Type())})
				} else {
					lhs = append(lhs, fs.globalRef(v, in.Rhs.Pos()))
				}
			}
			var rhs ir.Expr
			if len(in.Lhs) == 1 {
				rhs = fs.convExpr(in.Rhs, in.Lhs[0].Type())
			} else {
				rhs = fs.commaOkExpr(in.Rhs)
			}
			fs.body.Body = append(fs.body.Body, &ir.AssignStmt{Pos: fs.pos(in.Rhs), Op: ir.AssignOpPlain, Lhs: lhs, Rhs: []ir.Expr{rhs}})
		}
	})
	if fs.ok {
		for _, c := range initCounts {
			e.addOK(c)
		}
		e.file.Bodies[idx] = fs.body
		e.file.Funcs[idx].Syms, e.file.Funcs[idx].Objs = fs.refs()
		pos := e.cfg.Fset.Position(order[0].Rhs.Pos())
		e.file.Funcs[idx].File, e.file.Funcs[idx].Line = e.fileName(pos.Filename), uint64(pos.Line)
	}
	e.file.Init.VarInit = int32(idx)
}

// emitTypedefs writes TYPEDEFS for every defined type declared in this
// package (package-level and local), and for every unnamed struct type in
// the type table whose value or pointer method set is non-empty (promoted
// methods, design §3.4: the backend never runs a method-set algorithm).
// Interning a type may discover more of either, so both lists are drained
// until they stop growing.
func (e *emitter) emitTypedefs() {
	nextType := 0
	for i := 0; i < len(e.typedefs) || nextType < len(e.typeOf); {
		if i < len(e.typedefs) {
			e.emitNamedTypedef(e.typedefs[i])
			i++
			continue
		}
		t := e.typeOf[nextType]
		nextType++
		if st, ok := t.(*types.Struct); ok {
			e.emitStructTypedef(st)
		}
	}
}

func (e *emitter) emitStructTypedef(st *types.Struct) {
	embeds := false
	for i := range st.NumFields() {
		embeds = embeds || st.Field(i).Embedded()
	}
	if !embeds {
		return
	}
	mset, ptrMset := e.methodSet(st), e.methodSet(types.NewPointer(st))
	if len(mset) == 0 && len(ptrMset) == 0 {
		return
	}
	td := ir.TypeDef{Type: e.typeID(st), Mset: mset, PtrMset: ptrMset, Size: -1, Align: -1}
	e.setLayout(&td, st)
	e.file.TypeDefs = append(e.file.TypeDefs, td)
}

func (e *emitter) setLayout(td *ir.TypeDef, t types.Type) {
	if mentionsTypeParam(t, map[types.Type]bool{}) {
		return
	}
	td.Size = e.sizes.Sizeof(t)
	td.Align = e.sizes.Alignof(t)
	if st, ok := t.Underlying().(*types.Struct); ok {
		fields := make([]*types.Var, st.NumFields())
		for k := range fields {
			fields[k] = st.Field(k)
		}
		td.Offsets = e.sizes.Offsetsof(fields)
	}
}

func (e *emitter) emitNamedTypedef(tn *types.TypeName) {
	named, ok := tn.Type().(*types.Named)
	if !ok {
		return
	}
	td := ir.TypeDef{Type: e.typeID(named), Underlying: e.typeID(named.Underlying()), Tparams: e.tparamIDs(named.TypeParams()), Size: -1, Align: -1}
	if lt := e.localTargs[tn]; len(lt) > 0 {
		// A local type of a generic function: generic in the function's
		// type parameters (one type per instantiation, as in gc).
		td.Tparams = e.typeIDs_(lt)
	}
	for j := range named.NumMethods() {
		m := named.Method(j)
		pkg := ""
		if !m.Exported() {
			pkg = pkgPath(m.Pkg())
		}
		_, ptr := m.Signature().Recv().Type().(*types.Pointer)
		id, ok := e.funcIDs[m]
		if !ok {
			continue // method declared as "_"
		}
		td.Methods = append(td.Methods, ir.Method{Name: m.Name(), Pkg: pkg, PtrRecv: ptr, Fn: id})
	}
	if _, isIface := named.Underlying().(*types.Interface); !isIface {
		td.Mset = e.methodSet(named)
		td.PtrMset = e.methodSet(types.NewPointer(named))
	}
	if len(td.Tparams) == 0 {
		e.setLayout(&td, named)
	}
	e.file.TypeDefs = append(e.file.TypeDefs, td)
}

func (e *emitter) methodSet(t types.Type) []ir.MsetEntry {
	ms := types.NewMethodSet(t)
	out := make([]ir.MsetEntry, 0, ms.Len())
	for i := range ms.Len() {
		sel := ms.At(i)
		m := sel.Obj().(*types.Func)
		pkg := ""
		if !m.Exported() {
			pkg = pkgPath(m.Pkg())
		}
		idx := sel.Index()
		path := make([]uint64, len(idx)-1)
		for k, v := range idx[:len(idx)-1] {
			path[k] = uint64(v)
		}
		ent := ir.MsetEntry{Name: m.Name(), Pkg: pkg, Path: path, Indirect: sel.Indirect(), Method: -1}
		recv := m.Signature().Recv().Type()
		if p, ok := recv.(*types.Pointer); ok {
			recv = p.Elem()
		}
		ent.RecvType = e.typeID(recv)
		if !types.IsInterface(recv) {
			ent.Method = int32(e.methodSym(m))
		}
		out = append(out, ent)
	}
	return out
}

func mentionsTypeParam(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch t := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := range t.TypeArgs().Len() {
			if mentionsTypeParam(t.TypeArgs().At(i), seen) {
				return true
			}
		}
		return t.TypeParams().Len() > 0 && t.TypeArgs().Len() == 0
	case *types.Pointer:
		return mentionsTypeParam(t.Elem(), seen)
	case *types.Slice:
		return mentionsTypeParam(t.Elem(), seen)
	case *types.Array:
		return mentionsTypeParam(t.Elem(), seen)
	case *types.Map:
		return mentionsTypeParam(t.Key(), seen) || mentionsTypeParam(t.Elem(), seen)
	case *types.Chan:
		return mentionsTypeParam(t.Elem(), seen)
	case *types.Struct:
		for i := range t.NumFields() {
			if mentionsTypeParam(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		for _, tup := range []*types.Tuple{t.Params(), t.Results()} {
			for i := range tup.Len() {
				if mentionsTypeParam(tup.At(i).Type(), seen) {
					return true
				}
			}
		}
	case *types.Interface:
		for i := range t.NumMethods() {
			if mentionsTypeParam(t.Method(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}

func (e *emitter) fileName(name string) string {
	if e.cfg.FileName != nil {
		return e.cfg.FileName(name)
	}
	return name
}

// scanLinknames collects //go:linkname directives (design §3.3).
func (e *emitter) scanLinknames() {
	for _, f := range e.cfg.Files {
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if !strings.HasPrefix(c.Text, "//go:linkname ") {
					continue
				}
				fields := strings.Fields(c.Text)
				ln := ir.Linkname{}
				if len(fields) >= 2 {
					ln.Local = fields[1]
				}
				if len(fields) >= 3 {
					ln.Remote = fields[2]
					e.linknames[ln.Local] = ln.Remote
				}
				e.file.Pkg.Linknames = append(e.file.Pkg.Linknames, ln)
			}
		}
	}
}

// nameLocalTypes gives every type declared inside a function a package-
// unique name: Name·Func·N, N counting local type declarations of the
// enclosing top-level function in source order (design §3.4).
func (e *emitter) nameLocalTypes() {
	for _, f := range e.cfg.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			owner := fd.Name.Name
			var tparams []types.Type
			if obj, ok := e.cfg.Info.Defs[fd.Name].(*types.Func); ok {
				owner = symbolName(obj)
				e.registerFunc(obj)
				sig := obj.Signature()
				for _, l := range []*types.TypeParamList{sig.RecvTypeParams(), sig.TypeParams()} {
					for i := range l.Len() {
						tparams = append(tparams, l.At(i))
					}
				}
			}
			n := 0
			ast.Inspect(fd.Body, func(node ast.Node) bool {
				ts, ok := node.(*ast.TypeSpec)
				if !ok {
					return true
				}
				if tn, ok := e.cfg.Info.Defs[ts.Name].(*types.TypeName); ok {
					e.localTypes[tn] = tn.Name() + "·" + owner + "·" + strconv.Itoa(n)
					if len(tparams) > 0 {
						e.localTargs[tn] = tparams
					}
					n++
				}
				return true
			})
		}
	}
}

// countNodes adds every AST node under n to the statistics and returns
// the counts of this subtree.
func (e *emitter) countNodes(n ast.Node) map[string]int {
	counts := map[string]int{}
	ast.Inspect(n, func(n ast.Node) bool {
		if n != nil {
			k := reflect.TypeOf(n).Elem().Name()
			e.stats.Nodes[k]++
			counts[k]++
		}
		return true
	})
	return counts
}

func (e *emitter) addOK(counts map[string]int) {
	for k, v := range counts {
		e.stats.NodesOK[k] += v
	}
}
