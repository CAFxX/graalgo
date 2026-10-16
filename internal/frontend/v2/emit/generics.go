// Generics: monomorphization of generic functions at emit time.
//
// Design: docs/arch/frontend-v2-generics.md.
//
// Each distinct instantiation of a generic function with concrete type
// arguments is emitted as its own ordinary (non-generic) funcdecl with a
// mangled name (e.g. id[int]). Generic function declarations are
// registered as *templates* and are never emitted as funcdecls.
//
// Instantiation sites are detected via types.Info.Instances (the
// authoritative go/types record), never via syntax heuristics. While an
// instantiation body is emitted, an instContext substitutes every
// *types.TypeParam with the concrete type argument, so the object and type
// tables contain only concrete types. The binary wire format is unchanged:
// instantiations are plain funcdecls.
package emit

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"graalgo-frontend-v2/checker"
)

// maxInstantiationsPerTemplate bounds the number of distinct type-argument
// sets one generic function may be instantiated with in a single Emit.
// Without it, a self-feeding instantiation such as f[T] calling f[[]T]
// would enqueue ever-larger instantiations forever. Hitting the cap is a
// loud compile error, not a silent cutoff.
const maxInstantiationsPerTemplate = 256

// genericTemplate is a generic function declaration: the source that
// instantiations are monomorphized from. Templates are never emitted as
// funcdecls — they are not callable.
type genericTemplate struct {
	fd  *ast.FuncDecl
	fn  *types.Func
	sig *types.Signature // fn.Type().(*types.Signature); has type params
}

// funcWork is one function body awaiting emission.
type funcWork struct {
	fd    *ast.FuncDecl
	fn    *types.Func // object to describe; for instantiations, the fresh object
	tmpl  *genericTemplate
	targs []types.Type
	// objSubst maps original objects (params, named results) to fresh
	// objects with substituted types. Shared with the instContext at
	// emission time.
	objSubst map[types.Object]types.Object
	// memo canonicalizes substituted types for this instantiation.
	memo map[types.Type]types.Type
}

// typeSubst substitutes type parameters with concrete type arguments. It
// is total over all go/types constructors: every type it is given yields a
// type, so callers never need an error path.
type typeSubst struct {
	tparams *types.TypeParamList
	targs   []types.Type
	memo    map[types.Type]types.Type
	// typesMap is the checker's per-expression type info, used to read
	// declared parameter/result field types.
	typesMap map[ast.Expr]types.TypeAndValue
}

// instContext is the substitution environment active while emitting one
// instantiation body.
type instContext struct {
	*typeSubst
	// objSubst maps original objects (params, results, locals of the
	// generic function) to fresh objects with substituted types.
	objSubst map[types.Object]types.Object
}

// isGenericFunc reports whether fn is a generic function (declares type
// parameters). Structural: derived from the go/types signature, never from
// names or syntax.
func isGenericFunc(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.TypeParams() != nil && sig.TypeParams().Len() > 0
}

// registerTemplate registers a generic function declaration as an
// instantiation template. The template itself is never emitted.
func (e *Emitter) registerTemplate(fd *ast.FuncDecl, fn *types.Func) error {
	if fd.Recv != nil {
		return fmt.Errorf("emit: unsupported decl *ast.FuncDecl %s (generic methods are not yet supported)", fd.Name.Name)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.TypeParams() == nil || sig.TypeParams().Len() == 0 {
		return fmt.Errorf("emit: internal error: %s registered as generic template without type params", fd.Name.Name)
	}
	e.templates[fn] = &genericTemplate{fd: fd, fn: fn, sig: sig}
	return nil
}

// isGenericRecv reports whether a method declaration's receiver is an
// instantiated generic type (e.g. Box[T]).
func isGenericRecv(info *checker.Result, fd *ast.FuncDecl) bool {
	if fd.Recv == nil {
		return false
	}
	for _, field := range fd.Recv.List {
		tv, ok := info.Info.Types[field.Type]
		if !ok || tv.Type == nil {
			continue
		}
		t := types.Unalias(tv.Type)
		if p, ok := t.(*types.Pointer); ok {
			t = types.Unalias(p.Elem())
		}
		if n, ok := t.(*types.Named); ok && n.TypeArgs() != nil && n.TypeArgs().Len() > 0 {
			return true
		}
	}
	return false
}

// rejectGenericTypeDecl fails loudly on generic type declarations, which
// are not yet supported. Non-generic GenDecls keep existing behavior.
func rejectGenericTypeDecl(gd *ast.GenDecl) error {
	for _, spec := range gd.Specs {
		if ts, ok := spec.(*ast.TypeSpec); ok && ts.TypeParams != nil {
			return fmt.Errorf("emit: unsupported decl *ast.TypeSpec %s (generic type declarations are not yet supported)", ts.Name.Name)
		}
	}
	return nil
}

// qualifiedPkg renders a package for canonical type strings: full path,
// so identical instantiations in different packages mangle differently.
func qualifiedPkg(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Path()
}

// instantiationName mangles a generic function name with its concrete type
// arguments. Brackets are not legal in Go identifiers, so a mangled name
// can never collide with user-written code.
func instantiationName(base string, targs []types.Type) string {
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteByte('[')
	for i, t := range targs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(types.TypeString(t, qualifiedPkg))
	}
	sb.WriteByte(']')
	return sb.String()
}

// requestInstantiation returns the *types.Func object for the
// monomorphized instantiation of tmpl with targs, enqueueing its body for
// emission on first request. The cache is populated before the body is
// emitted, so recursive generics terminate.
func (e *Emitter) requestInstantiation(tmpl *genericTemplate, targs []types.Type, pos token.Pos) (*types.Func, error) {
	if len(targs) != tmpl.sig.TypeParams().Len() {
		return nil, fmt.Errorf("emit: internal error: instantiation of %s with %d type arguments for %d type parameters",
			tmpl.fn.Name(), len(targs), tmpl.sig.TypeParams().Len())
	}
	name := instantiationName(tmpl.fn.Name(), targs)
	if fn, ok := e.instCache[name]; ok {
		return fn, nil
	}
	if e.instCount[tmpl] >= maxInstantiationsPerTemplate {
		return nil, fmt.Errorf("emit: unsupported: instantiation of generic function %s exceeds %d distinct type-argument sets (possible infinite generic recursion)",
			tmpl.fn.Name(), maxInstantiationsPerTemplate)
	}
	e.instCount[tmpl]++

	ts := &typeSubst{
		tparams:  tmpl.sig.TypeParams(),
		targs:    targs,
		memo:     make(map[types.Type]types.Type),
		typesMap: e.info.Info.Types,
	}
	objSubst := make(map[types.Object]types.Object)

	// Fresh params and results with substituted types. The instantiated
	// signature is built from these same objects, so the signature's
	// params are identical to the declared param objects (the invariant
	// go/types maintains for source-checked functions).
	sigParams := e.freshParams(tmpl, ts, objSubst)
	sigResults := e.freshResults(tmpl, ts, objSubst)
	sig := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(sigParams...),
		types.NewTuple(sigResults...),
		tmpl.sig.Variadic(),
	)
	newFn := types.NewFunc(pos, tmpl.fn.Pkg(), name, sig)
	e.instCache[name] = newFn
	e.declareObject(newFn)
	e.worklist = append(e.worklist, funcWork{
		fd:       tmpl.fd,
		fn:       newFn,
		tmpl:     tmpl,
		targs:    targs,
		objSubst: objSubst,
		memo:     ts.memo,
	})
	return newFn, nil
}

// freshParams builds fresh parameter vars with substituted types, in
// positional order, recording named ones in subst.
func (e *Emitter) freshParams(tmpl *genericTemplate, ts *typeSubst, subst map[types.Object]types.Object) []*types.Var {
	var out []*types.Var
	if tmpl.fd.Type.Params == nil {
		return out
	}
	for _, field := range tmpl.fd.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, types.NewParam(field.Pos(), tmpl.fn.Pkg(), "", ts.fieldType(field)))
			continue
		}
		for _, name := range field.Names {
			var pt types.Type
			var orig *types.Var
			if obj, ok := e.info.Info.Defs[name].(*types.Var); ok && obj != nil {
				orig = obj
				pt = ts.subst(orig.Type())
			} else {
				pt = ts.fieldType(field)
			}
			fv := types.NewParam(name.Pos(), tmpl.fn.Pkg(), name.Name, pt)
			out = append(out, fv)
			if orig != nil {
				subst[orig] = fv
			}
		}
	}
	return out
}

// freshResults builds fresh result vars with substituted types, in
// positional order, recording named ones in subst.
func (e *Emitter) freshResults(tmpl *genericTemplate, ts *typeSubst, subst map[types.Object]types.Object) []*types.Var {
	var out []*types.Var
	if tmpl.fd.Type.Results == nil {
		return out
	}
	for _, field := range tmpl.fd.Type.Results.List {
		if len(field.Names) == 0 {
			out = append(out, types.NewVar(field.Pos(), tmpl.fn.Pkg(), "", ts.fieldType(field)))
			continue
		}
		for _, name := range field.Names {
			var rt types.Type
			var orig *types.Var
			if obj, ok := e.info.Info.Defs[name].(*types.Var); ok && obj != nil {
				orig = obj
				rt = ts.subst(orig.Type())
			} else {
				rt = ts.fieldType(field)
			}
			fv := types.NewVar(name.Pos(), tmpl.fn.Pkg(), name.Name, rt)
			out = append(out, fv)
			if orig != nil {
				subst[orig] = fv
			}
		}
	}
	return out
}

// fieldType substitutes the declared type of a parameter/result field.
func (ts *typeSubst) fieldType(field *ast.Field) types.Type {
	// The checker always records the field type; fall back to int only if
	// the impossible happens (keeps subst total).
	if tv, ok := ts.typesMap[field.Type]; ok && tv.Type != nil {
		return ts.subst(tv.Type)
	}
	return types.Typ[types.Int]
}

// subst replaces every *types.TypeParam bound by this substitution's type
// parameter list with the corresponding concrete type argument,
// recursively through all structural type constructors.
func (ts *typeSubst) subst(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	if s, ok := ts.memo[t]; ok {
		return s
	}
	s := ts.substUnmemo(t)
	ts.memo[t] = s
	return s
}

func (ts *typeSubst) substUnmemo(t types.Type) types.Type {
	t = types.Unalias(t)
	if tp, ok := t.(*types.TypeParam); ok {
		for i := 0; i < ts.tparams.Len(); i++ {
			if ts.tparams.At(i) == tp {
				return ts.targs[i]
			}
		}
		// A type parameter from another scope cannot occur in a valid
		// instantiation body; carry it through rather than dropping it.
		return t
	}
	switch tt := t.(type) {
	case *types.Basic:
		return tt
	case *types.Array:
		return types.NewArray(ts.subst(tt.Elem()), tt.Len())
	case *types.Slice:
		return types.NewSlice(ts.subst(tt.Elem()))
	case *types.Struct:
		fields := make([]*types.Var, tt.NumFields())
		tags := make([]string, tt.NumFields())
		for i := 0; i < tt.NumFields(); i++ {
			f := tt.Field(i)
			fields[i] = types.NewField(f.Pos(), f.Pkg(), f.Name(), ts.subst(f.Type()), f.Embedded())
			tags[i] = tt.Tag(i)
		}
		return types.NewStruct(fields, tags)
	case *types.Pointer:
		return types.NewPointer(ts.subst(tt.Elem()))
	case *types.Tuple:
		vars := make([]*types.Var, tt.Len())
		for i := 0; i < tt.Len(); i++ {
			v := tt.At(i)
			vars[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), ts.subst(v.Type()))
		}
		return types.NewTuple(vars...)
	case *types.Signature:
		var recv *types.Var
		if tt.Recv() != nil {
			r := tt.Recv()
			recv = types.NewVar(r.Pos(), r.Pkg(), r.Name(), ts.subst(r.Type()))
		}
		params := ts.substTuple(tt.Params())
		results := ts.substTuple(tt.Results())
		var tparams []*types.TypeParam
		if tt.TypeParams() != nil {
			tparams = make([]*types.TypeParam, tt.TypeParams().Len())
			for i := range tparams {
				tparams[i] = tt.TypeParams().At(i)
			}
		}
		return types.NewSignatureType(recv, nil, tparams, params, results, tt.Variadic())
	case *types.Interface:
		return tt
	case *types.Union:
		terms := make([]*types.Term, tt.Len())
		for i := 0; i < tt.Len(); i++ {
			term := tt.Term(i)
			terms[i] = types.NewTerm(term.Tilde(), ts.subst(term.Type()))
		}
		return types.NewUnion(terms)
	case *types.Map:
		return types.NewMap(ts.subst(tt.Key()), ts.subst(tt.Elem()))
	case *types.Chan:
		return types.NewChan(tt.Dir(), ts.subst(tt.Elem()))
	case *types.Named:
		if tt.TypeArgs() != nil && tt.TypeArgs().Len() > 0 {
			targs := make([]types.Type, tt.TypeArgs().Len())
			for i := range targs {
				targs[i] = ts.subst(tt.TypeArgs().At(i))
			}
			inst, err := types.Instantiate(nil, tt, targs, false)
			if err != nil {
				return tt
			}
			return inst
		}
		return tt
	default:
		return t
	}
}

// substTuple substitutes a parameter/result tuple.
func (ts *typeSubst) substTuple(tup *types.Tuple) *types.Tuple {
	if tup == nil {
		return nil
	}
	vars := make([]*types.Var, tup.Len())
	for i := 0; i < tup.Len(); i++ {
		v := tup.At(i)
		vars[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), ts.subst(v.Type()))
	}
	return types.NewTuple(vars...)
}

// instantiationOf returns the go/types instantiation record for a
// generic-function reference expression. In go1.27, Info.Instances is keyed
// by the *ast.Ident denoting the generic function or type — for id[int]
// that is the identifier id (the X of the IndexExpr), and likewise for
// id[int,string] and inferred id(42).
func (e *Emitter) instantiationOf(funX ast.Expr) (*ast.Ident, types.Instance, bool) {
	switch fx := funX.(type) {
	case *ast.Ident:
		if inst, ok := e.info.Info.Instances[fx]; ok {
			return fx, inst, true
		}
	case *ast.SelectorExpr:
		// pkg.Fn[T]: qualified reference to a generic function.
		if inst, ok := e.info.Info.Instances[fx.Sel]; ok {
			return fx.Sel, inst, true
		}
	}
	return nil, types.Instance{}, false
}

// emitInstantiation emits a generic function instantiation
// (id[int], pair[int,string], or inferred id(42)) as an ident referencing
// the monomorphized function.
func (e *Emitter) emitInstantiation(x ast.Expr, id *ast.Ident, inst types.Instance) (map[string]any, error) {
	obj := e.info.Info.Uses[id]
	if obj == nil {
		obj = e.info.Info.Defs[id]
	}
	fn, ok := obj.(*types.Func)
	if !ok || !isGenericFunc(fn) {
		return nil, fmt.Errorf("emit: internal error: instantiation target %s is not a generic function", id.Name)
	}
	tmpl, ok := e.templates[fn]
	if !ok {
		return nil, fmt.Errorf("emit: unsupported expr %T (instantiation of generic function %s from another package is not yet supported)", x, id.Name)
	}
	// Substitute type parameters inside the type arguments (nested
	// instantiation, e.g. id[T] inside twice[T]).
	targs := make([]types.Type, inst.TypeArgs.Len())
	for i := range targs {
		targs[i] = inst.TypeArgs.At(i)
		if e.inst != nil {
			targs[i] = e.inst.subst(targs[i])
		}
	}
	newFn, err := e.requestInstantiation(tmpl, targs, x.Pos())
	if err != nil {
		return nil, err
	}
	oid := e.declareObject(newFn)
	return e.node("ident", x, map[string]any{
		"name": newFn.Name(),
		"obj":  oid,
		"type": e.typeID(newFn.Type()),
	}), nil
}

// resolveObj maps an original object to its instantiation-local fresh
// object when one exists (parameters, results, and locals of the generic
// function being instantiated).
func (e *Emitter) resolveObj(obj types.Object) types.Object {
	if e.inst != nil {
		if fresh, ok := e.inst.objSubst[obj]; ok {
			return fresh
		}
	}
	return obj
}

// declareLocal declares a function-local variable. Inside a generic
// instantiation body it creates a fresh object with the substituted type
// on first use, so later references resolve to the concrete type.
func (e *Emitter) declareLocal(id *ast.Ident) int {
	obj := e.info.Info.Defs[id]
	if obj == nil {
		return -1
	}
	if e.inst != nil {
		if fresh, ok := e.inst.objSubst[obj]; ok {
			return e.declareObject(fresh)
		}
		fresh := types.NewVar(obj.Pos(), obj.Pkg(), obj.Name(), e.inst.subst(obj.Type()))
		e.inst.objSubst[obj] = fresh
		return e.declareObject(fresh)
	}
	return e.declareObject(obj)
}

// substTypeID returns the type-table id for t, substituting type
// parameters when inside a generic instantiation body.
func (e *Emitter) substTypeID(t types.Type) int {
	if e.inst != nil {
		t = e.inst.subst(t)
	}
	return e.typeID(t)
}
