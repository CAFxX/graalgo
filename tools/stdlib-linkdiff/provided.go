package main

// ProvidedSet is the set of linkname target symbols graalgo provides,
// each with a provenance string naming the exact source it was
// extracted from. Extraction is mechanical (AST/regex over the graalgo
// tree), never hand-maintained, so per-release drift shows up as a
// snapshot diff rather than silent staleness.
//
// Sources (see README.md § "Provided set"):
//  1. internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java —
//     Linknames.register("symbol", ...) calls (Go→intrinsic direction).
//  2. internal/frontend/p3frontend/synth_time_context.go —
//     syntheticTimePackage, syntheticContextPackage.
//  3. internal/frontend/synth_time_context.go — syntheticTimeSource /
//     syntheticContextSource embedded Go sources (normative; the
//     p3frontend mirrors of (2) pin them via signature-equality tests).
//  4. M10: every embedded synthetic package in the syntheticSources
//     registry (internal/frontend/synth_machinery.go) — exported
//     symbols qualified by import path (see synth.go).
//
// NOT a source: the p3frontend fmt/sync/sync-atomic/runtime mirrors
// were retired (REVIEW-2026-11-05-11) — fmt, sync and sync/atomic now
// compile from GOROOT via the disk importer, so no synthetic source
// provides them (same precedent as reflect/net/http, see
// TestProvidedExtraction). The live runtime stub
// (internal/frontend/export.go syntheticRuntimePackage) is likewise not
// extracted. Consequence, measured 2026-11-05 against the vendored
// go1.27.1 tree: exactly three scanned linknames flip SATISFIED to
// MISSING — sync/atomic.{CompareAndSwap,Store,Swap}Uintptr from the
// shadowed runtime/atomic_pointer.go (shadowed-only bucket; the live
// MISSING count is unchanged). They ARE provided by real sync/atomic
// from disk; the tool just has no source that says so.
//
// Symbol forms: path.Name (func/var/const/type), path.Type.Method
// (value-receiver or interface method), path.(*Type).Method
// (pointer-receiver method) — the same qualification gc linkname
// targets use.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProvidedSet maps symbol -> provenance.
type ProvidedSet struct {
	Symbols map[string]string
	// Skipped records synthetic sources that failed to parse during
	// extraction (loud in the report; never silently dropped).
	Skipped []string
}

func (p *ProvidedSet) add(sym, prov string) {
	if p.Symbols == nil {
		p.Symbols = map[string]string{}
	}
	if prev, ok := p.Symbols[sym]; ok && prev != prov {
		prov = prev + "; " + prov
	}
	p.Symbols[sym] = prov
}

func (p *ProvidedSet) has(sym string) bool {
	_, ok := p.Symbols[sym]
	return ok
}

// loadProvided loads the provided set: from a snapshot file when
// --provided is given, otherwise by mechanical extraction from the
// graalgo tree at o.graalgo.
func loadProvided(o options) (*ProvidedSet, error) {
	if o.provided != "" {
		return readSnapshot(o.provided)
	}
	p := &ProvidedSet{}
	if err := p.extractRegistry(filepath.Join(o.graalgo, "internal", "backend", "interop", "src", "main", "java",
		"graalgo", "p3link", "LinknameIntrinsics.java")); err != nil {
		return nil, err
	}
	// Only synth_time_context.go is extracted: importer.go no longer
	// defines any synthetic-package builder (its fmt/sync/sync-atomic/
	// runtime mirrors were retired — see the header comment).
	p3fe := filepath.Join(o.graalgo, "internal", "frontend", "p3frontend")
	if err := p.extractP3FrontendFile(filepath.Join(p3fe, "synth_time_context.go")); err != nil {
		return nil, err
	}
	if err := p.extractEmbeddedSynthetics(filepath.Join(o.graalgo, "internal",
		"frontend", "synth_time_context.go")); err != nil {
		return nil, err
	}
	// M10 (WS I): the post-M9 embedded synthetic stdlib packages
	// (syntheticSources registry in synth_machinery.go). Mechanical
	// extraction like every other source — never hand-maintained.
	reg, err := loadSyntheticRegistry(o.graalgo)
	if err != nil {
		return nil, err
	}
	if err := p.extractSyntheticProvided(reg); err != nil {
		return nil, err
	}
	return p, nil
}

var registerRe = regexp.MustCompile(`Linknames\.register\("((?:[^"\\]|\\.)+)"`)

// extractRegistry pulls Go→intrinsic symbol names from the
// Linknames.register("symbol", ...) calls in LinknameIntrinsics.java.
func (p *ProvidedSet) extractRegistry(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	n := 0
	for _, m := range registerRe.FindAllSubmatch(src, -1) {
		sym, err := strconv.Unquote(`"` + string(m[1]) + `"`)
		if err != nil {
			return fmt.Errorf("unquote symbol in %s: %w", path, err)
		}
		p.add(sym, "p3link LinknameIntrinsics.java Linknames.register")
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: no Linknames.register calls found (source drift?)", path)
	}
	return nil
}

// synthFuncToPkg maps synthetic-package builder function names to the
// import path of the package they synthesize.
// synthFuncToPkg maps synthetic-package builder function names to the
// import path of the package they synthesize. Only time/context remain:
// the runtime/fmt/sync/sync-atomic builders were deleted with the
// fmt/sync retirement (REVIEW-2026-11-05-11).
var synthFuncToPkg = map[string]string{
	"syntheticTimePackage":    "time",
	"syntheticContextPackage": "context",
}

func isExported(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// extractP3FrontendFile extracts synthetic package surfaces from one
// p3frontend Go source file: the synthetic*Package builders in
// synthFuncToPkg (time/context only — the runtime/fmt/sync/
// sync-atomic builders were deleted with the fmt/sync retirement).
func (p *ProvidedSet) extractP3FrontendFile(path string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	base := filepath.Base(path)
	found := false
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		pkgPath, ok := synthFuncToPkg[d.Name.Name]
		if !ok || d.Body == nil {
			continue
		}
		found = true
		x := newSynthExtractor(pkgPath,
			fmt.Sprintf("synthetic %q: p3frontend/%s %s()", pkgPath, base, d.Name.Name), p)
		x.run(d.Body)
	}
	if !found {
		return fmt.Errorf("%s: no synthetic package surface found (source drift?)", path)
	}
	return nil
}

// synthExtractor accumulates one synthetic package's surface from the
// AST of its builder function body.
type synthExtractor struct {
	pkg  string
	out  *ProvidedSet
	prov string

	typeNameOf     map[string]string   // var -> T from v := types.NewTypeName(...,"T",...)
	namedOf        map[string]string   // var -> T from v := types.NewNamed(w,...), w->T
	listOf         map[string][]string // var -> methods from v := []*types.Func{...}
	fieldsOf       map[string][]string // var -> names from v := []struct{ name string ... }{...}
	wrapperOf      map[string]string   // var -> T from v := newWrapper("T")
	rangeSrc       map[string]string   // range var -> ranged var, for _, v := range X (prepass fallback; the walk threads the binding via walkCtx.rangeVars)
	ifaceListOf    map[string]string   // named var -> []*types.Func list var (interfaces)
	fiveOpsTypes   []string            // newWrapper("T") types passed to fiveOps(...)
	fiveOpsMethods []string            // method names added inside the fiveOps closure
	strSetOf       map[string][]string // var -> strings from v := map[K]string{...:"S"...}[k]
}

func newSynthExtractor(pkg, prov string, out *ProvidedSet) *synthExtractor {
	return &synthExtractor{
		pkg: pkg, prov: prov, out: out,
		typeNameOf: map[string]string{}, namedOf: map[string]string{},
		listOf: map[string][]string{}, fieldsOf: map[string][]string{},
		wrapperOf: map[string]string{}, rangeSrc: map[string]string{},
		ifaceListOf: map[string]string{}, strSetOf: map[string][]string{},
	}
}

// run executes the prepass (name tables) then the extraction walk.
func (x *synthExtractor) run(body *ast.BlockStmt) {
	x.prepass(body)
	x.walk(body, walkCtx{})
	x.emitInterfaceMethods()
}

// prepass builds the name tables the walk needs.
func (x *synthExtractor) prepass(body *ast.BlockStmt) {
	// ifaceVar -> listVar, from v := types.NewInterfaceType(listv,...)[.Complete()]
	ifaceVarList := map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			x.prepassAssign(s, ifaceVarList)
		case *ast.RangeStmt:
			if s.Tok == token.DEFINE {
				if id, ok := s.Value.(*ast.Ident); ok {
					if w, ok := identName(s.X); ok {
						x.rangeSrc[id.Name] = w
					}
				}
			}
		}
		return true
	})
	// named var -> list var, from v := types.NewNamed(tnv, ifacev, ...)
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := identName(as.Lhs[0])
		if !ok {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || callExprFun(call) != "types.NewNamed" || len(call.Args) < 2 {
			return true
		}
		ifaceVar, ok := identName(call.Args[1])
		if !ok {
			return true
		}
		if listVar, ok := ifaceVarList[ifaceVar]; ok {
			x.ifaceListOf[lhs] = listVar
		}
		return true
	})
}

func (x *synthExtractor) prepassAssign(s *ast.AssignStmt, ifaceVarList map[string]string) {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
		return
	}
	lhs, ok := identName(s.Lhs[0])
	if !ok {
		return
	}
	switch rhs := s.Rhs[0].(type) {
	case *ast.CallExpr:
		switch callExprFun(rhs) {
		case "types.NewTypeName":
			if name := nameArg(rhs, 2); name != "" {
				x.typeNameOf[lhs] = name
			}
		case "types.NewNamed":
			if len(rhs.Args) > 0 {
				if w, ok := identName(rhs.Args[0]); ok {
					if t, ok := x.typeNameOf[w]; ok {
						x.namedOf[lhs] = t
					}
				}
			}
		case "types.NewInterfaceType":
			if len(rhs.Args) > 0 {
				if w, ok := identName(rhs.Args[0]); ok {
					ifaceVarList[lhs] = w
				}
			}
		case "newWrapper":
			if name := nameArg(rhs, 0); name != "" {
				x.wrapperOf[lhs] = name
			}
		default:
			// Unwrap `.Complete()` on NewInterfaceType.
			if sel, ok := rhs.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Complete" {
				if inner, ok := sel.X.(*ast.CallExpr); ok &&
					callExprFun(inner) == "types.NewInterfaceType" && len(inner.Args) > 0 {
					if w, ok := identName(inner.Args[0]); ok {
						ifaceVarList[lhs] = w
					}
				}
			}
		}
	case *ast.CompositeLit:
		x.prepassComposite(lhs, rhs)
	}
}

// prepassComposite handles v := []*types.Func{...} and
// v := []struct{ name string; ... }{...} literals.
func (x *synthExtractor) prepassComposite(lhs string, lit *ast.CompositeLit) {
	arr, ok := lit.Type.(*ast.ArrayType)
	if !ok {
		return
	}
	switch et := arr.Elt.(type) {
	case *ast.StarExpr:
		sel, ok := et.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Func" {
			return
		}
		var ms []string
		for _, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				e = kv.Value
			}
			call, ok := e.(*ast.CallExpr)
			if !ok || callExprFun(call) != "types.NewFunc" {
				continue
			}
			if name := nameArg(call, 2); name != "" {
				ms = append(ms, name)
			}
		}
		x.listOf[lhs] = ms
	case *ast.StructType:
		var names []string
		for _, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				e = kv.Value
			}
			cl, ok := e.(*ast.CompositeLit)
			if !ok || len(cl.Elts) == 0 {
				continue
			}
			if bl, ok := cl.Elts[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if s, err := strconv.Unquote(bl.Value); err == nil {
					names = append(names, s)
				}
			}
		}
		x.fieldsOf[lhs] = names
	}
}

// walkCtx carries ancestor context for the extraction walk.
type walkCtx struct {
	inAddMethod bool // inside an X.AddMethod(...) call
	inFuncList  bool // inside a []*types.Func{...} literal
	inFiveOps   bool // inside the fiveOps closure body
	// rangeVars binds range variables to the table they range over,
	// for the enclosing range statement only (see walk's RangeStmt
	// case). It shadows the prepass's global rangeSrc, which is only
	// a fallback for shapes the walk does not thread.
	rangeVars map[string]string
}

// walk extracts package members and methods with ancestor context.
func (x *synthExtractor) walk(n ast.Node, ctx walkCtx) {
	if n == nil {
		return
	}
	switch s := n.(type) {
	case *ast.BlockStmt:
		for _, st := range s.List {
			x.walk(st, ctx)
		}
		return
	case *ast.FuncLit:
		// The fiveOps closure: its addMethod calls are handled via
		// the fiveOps call sites, not here.
		if ctx.inFiveOps {
			return
		}
	case *ast.RangeStmt:
		// Range context: `for _, v := range X` binds v to X for the
		// loop body only. The prepass's global rangeSrc (keyed by var
		// name) is wrong when two loops share a var name — the time
		// mirror ranges durationMethods and timeMethods both as `m`,
		// so the last loop won and the first loop's method names
		// resolved against the wrong table (every table-driven Time
		// method was attributed to time.Duration). Thread the binding
		// through the walk context instead (shadowing-safe: a nested
		// loop reusing the var name shadows within its own body).
		child := ctx
		if s.Tok == token.DEFINE {
			if v, ok := s.Value.(*ast.Ident); ok {
				if tab, ok := s.X.(*ast.Ident); ok {
					child.rangeVars = make(map[string]string, len(ctx.rangeVars)+1)
					for k, val := range ctx.rangeVars {
						child.rangeVars[k] = val
					}
					child.rangeVars[v.Name] = tab.Name
				}
			}
		}
		x.walk(s.Key, ctx)
		x.walk(s.Value, ctx)
		x.walk(s.X, ctx)
		x.walk(s.Body, child)
		return
	}
	// Detect the fiveOps closure assignment and map-index value sets.
	if as, ok := n.(*ast.AssignStmt); ok {
		if len(as.Lhs) == 1 && len(as.Rhs) == 1 {
			if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name == "fiveOps" {
				if _, ok := as.Rhs[0].(*ast.FuncLit); ok {
					x.walkFiveOpsClosure(as.Rhs[0].(*ast.FuncLit))
					return // handled; skip general walk of the closure
				}
			}
			// nm := map[K]string{k1: "S1", ...}[k] — value set for
			// "Prefix"+nm name construction (pattern formerly used by
			// the retired syntheticAtomicPackage builder).
			if id, ok := as.Lhs[0].(*ast.Ident); ok {
				if idx, ok := as.Rhs[0].(*ast.IndexExpr); ok {
					if ml, ok := idx.X.(*ast.CompositeLit); ok {
						if _, ok := ml.Type.(*ast.MapType); ok {
							var vals []string
							for _, e := range ml.Elts {
								if kv, ok := e.(*ast.KeyValueExpr); ok {
									if bl, ok := kv.Value.(*ast.BasicLit); ok && bl.Kind == token.STRING {
										if s, err := strconv.Unquote(bl.Value); err == nil {
											vals = append(vals, s)
										}
									}
								}
							}
							if len(vals) > 0 {
								x.strSetOf[id.Name] = vals
							}
						}
					}
				}
			}
		}
	}
	if call, ok := n.(*ast.CallExpr); ok {
		x.walkCall(call, ctx)
		// Recurse into call args with updated context.
		nc := ctx
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddMethod" {
			nc.inAddMethod = true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "addMethod" {
			nc.inAddMethod = true
		}
		x.walk(call.Fun, nc)
		for _, a := range call.Args {
			x.walk(a, nc)
		}
		return
	}
	if comp, ok := n.(*ast.CompositeLit); ok {
		nc := ctx
		if arr, ok := comp.Type.(*ast.ArrayType); ok {
			if star, ok := arr.Elt.(*ast.StarExpr); ok {
				if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Func" {
					nc.inFuncList = true
				}
			}
		}
		if comp.Type != nil {
			x.walk(comp.Type, nc)
		}
		for _, e := range comp.Elts {
			x.walk(e, nc)
		}
		return
	}
	// Generic recursion for all other nodes.
	ast.Inspect(n, func(m ast.Node) bool {
		if m == n {
			return true
		}
		x.walk(m, ctx)
		return false
	})
}

// walkFiveOpsClosure handles `fiveOps := func(named, t) { ... }`:
// method names from addMethod calls inside, applied to the wrapper
// types at fiveOps(newWrapper("T"), ...) call sites (collected by the
// caller of run via a second inspection — see below).
func (x *synthExtractor) walkFiveOpsClosure(fl *ast.FuncLit) {
	var methods []string
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := callExprFun(call)
		isAdd := fun == "addMethod"
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddMethod" {
			isAdd = true
		}
		if !isAdd || len(call.Args) < 2 {
			return true
		}
		if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if m, err := strconv.Unquote(lit.Value); err == nil && isExported(m) {
				methods = append(methods, m)
			}
		}
		return true
	})
	x.fiveOpsMethods = methods
}

// walkCall classifies one call expression.
func (x *synthExtractor) walkCall(call *ast.CallExpr, ctx walkCtx) {
	fun := callExprFun(call)

	// fiveOps(newWrapper("T"), ...): record the wrapper type.
	if fun == "fiveOps" && !ctx.inFiveOps {
		if len(call.Args) > 0 {
			if w, ok := call.Args[0].(*ast.CallExpr); ok && callExprFun(w) == "newWrapper" {
				if name := nameArg(w, 0); name != "" && isExported(name) {
					x.fiveOpsTypes = append(x.fiveOpsTypes, name)
					x.out.add(x.pkg+"."+name, x.prov+" type")
				}
			}
		}
		return
	}

	// Method definitions: X.AddMethod(...) or addMethod(x, "M", ...).
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AddMethod" {
		recv, _ := identName(sel.X)
		x.methodCall(recv, call, ctx)
		return
	}
	if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "addMethod" && !ctx.inFiveOps {
		// Direct wrapper method: addMethod(b, "M", ...), b in wrapperOf.
		if len(call.Args) >= 2 {
			recv, _ := identName(call.Args[0])
			if t, ok := x.wrapperOf[recv]; ok && isExported(t) {
				if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if m, err := strconv.Unquote(lit.Value); err == nil && isExported(m) {
						x.out.add(x.pkg+".(*"+t+")."+m, x.prov+" method")
					}
				}
			}
		}
		return
	}

	if ctx.inAddMethod || ctx.inFuncList || ctx.inFiveOps {
		return
	}
	switch fun {
	case "types.NewFunc", "types.NewVar", "types.NewConst":
		if name := nameArg(call, 2); name != "" && isExported(name) {
			x.out.add(x.pkg+"."+name, x.prov)
		} else if sel, ok := nameSelector(call, 2); ok {
			// c.name / m.name over a ranged []struct{name...} literal.
			for _, name := range x.selectorNames(sel, ctx) {
				if isExported(name) {
					x.out.add(x.pkg+"."+name, x.prov)
				}
			}
		}
	case "insert", "insertFunc", "addFunc":
		for _, name := range x.callNameSet(call) {
			if isExported(name) {
				x.out.add(x.pkg+"."+name, x.prov+" func")
			}
		}
	}
}

// callNameSet resolves the name argument of an insert/insertFunc/addFunc
// call: a string literal, or "Prefix"+v where v's value set was recorded
// from a map-index assignment.
func (x *synthExtractor) callNameSet(call *ast.CallExpr) []string {
	if len(call.Args) == 0 {
		return nil
	}
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return []string{s}
		}
		return nil
	}
	if bin, ok := call.Args[0].(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		lit, ok := bin.X.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return nil
		}
		prefix, err := strconv.Unquote(lit.Value)
		if err != nil {
			return nil
		}
		id, ok := bin.Y.(*ast.Ident)
		if !ok {
			return nil
		}
		var out []string
		for _, s := range x.strSetOf[id.Name] {
			out = append(out, prefix+s)
		}
		return out
	}
	return nil
}

// methodCall records methods from X.AddMethod(types.NewFunc(..., name, ...)).
func (x *synthExtractor) methodCall(recv string, call *ast.CallExpr, ctx walkCtx) {
	if len(call.Args) == 0 {
		return
	}
	nf, ok := call.Args[0].(*ast.CallExpr)
	if !ok || callExprFun(nf) != "types.NewFunc" {
		return
	}
	t := x.namedOf[recv]
	if t == "" {
		t = x.typeNameOf[recv]
	}
	if t == "" || !isExported(t) {
		return
	}
	for _, m := range x.newFuncNames(nf, ctx) {
		if !isExported(m) {
			continue
		}
		sym := x.pkg + "." + t + "." + m
		if methodIsPointer(nf) {
			sym = x.pkg + ".(*" + t + ")." + m
		}
		x.out.add(sym, x.prov+" method")
	}
}

// newFuncNames resolves the name argument of types.NewFunc: a string
// literal, or a v.name selector over a ranged []struct{name...} list.
func (x *synthExtractor) newFuncNames(nf *ast.CallExpr, ctx walkCtx) []string {
	if len(nf.Args) < 3 {
		return nil
	}
	switch a := nf.Args[2].(type) {
	case *ast.BasicLit:
		if a.Kind == token.STRING {
			if s, err := strconv.Unquote(a.Value); err == nil {
				return []string{s}
			}
		}
	case *ast.SelectorExpr:
		if id, ok := a.X.(*ast.Ident); ok {
			return x.selectorNames(id.Name, ctx)
		}
	}
	return nil
}

// selectorNames resolves `v.name` where `for _, v := range X` and X is
// a recorded []struct{name...} literal. The enclosing range's binding
// (walkCtx.rangeVars) wins; the prepass's global rangeSrc is only a
// fallback for shapes the walk does not thread.
func (x *synthExtractor) selectorNames(v string, ctx walkCtx) []string {
	if src, ok := ctx.rangeVars[v]; ok {
		return x.fieldsOf[src]
	}
	if src, ok := x.rangeSrc[v]; ok {
		return x.fieldsOf[src]
	}
	return nil
}

// nameSelector returns the range-variable name when arg i is a
// `v.name`-style selector.
func nameSelector(call *ast.CallExpr, i int) (string, bool) {
	if i >= len(call.Args) {
		return "", false
	}
	sel, ok := call.Args[i].(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// emitInterfaceMethods emits pkg.Iface.Method for interface types built
// via NewInterfaceType(listv) + NewNamed.
func (x *synthExtractor) emitInterfaceMethods() {
	for namedVar, listVar := range x.ifaceListOf {
		t := x.namedOf[namedVar]
		if t == "" {
			t = x.typeNameOf[namedVar]
		}
		if t == "" || !isExported(t) {
			continue
		}
		x.out.add(x.pkg+"."+t, x.prov+" type")
		for _, m := range x.listOf[listVar] {
			if isExported(m) {
				x.out.add(x.pkg+"."+t+"."+m, x.prov+" interface method")
			}
		}
	}
	// Package-level types from NewTypeName / newWrapper not otherwise
	// emitted (NewNamed covers most; this catches bare insertions).
	for _, t := range x.typeNameOf {
		if isExported(t) {
			x.out.add(x.pkg+"."+t, x.prov+" type")
		}
	}
	for _, t := range x.wrapperOf {
		if isExported(t) {
			x.out.add(x.pkg+"."+t, x.prov+" type")
		}
	}
	// fiveOps wrapper types × fiveOps methods (pointer receivers —
	// addMethod builds types.NewPointer receivers).
	for _, t := range x.fiveOpsTypes {
		for _, m := range x.fiveOpsMethods {
			x.out.add(x.pkg+".(*"+t+")."+m, x.prov+" method")
		}
	}
}

// methodIsPointer reports whether the NewFunc's signature carries a
// pointer receiver. The builders' convention: the receiver NewParam's
// type is an ident ending in "Ptr" (timerPtr, tickerPtr) for pointer
// receivers.
func methodIsPointer(nf *ast.CallExpr) bool {
	if len(nf.Args) < 4 {
		return false
	}
	sig, ok := nf.Args[3].(*ast.CallExpr)
	if !ok || callExprFun(sig) != "types.NewSignatureType" || len(sig.Args) == 0 {
		return false
	}
	recv, ok := sig.Args[0].(*ast.CallExpr)
	if !ok || callExprFun(recv) != "types.NewParam" || len(recv.Args) == 0 {
		return false
	}
	// The receiver TYPE is NewParam's last argument (pos, pkg, name,
	// type): a *Ptr-suffixed ident (timerPtr — pointer vars are named
	// *Ptr by convention) or an inline types.NewPointer(...) call.
	// (The old code read Args[2], the NAME — always "" for method
	// receivers — so it always returned false and every AddMethod
	// method was emitted in value form, e.g. time.Timer.Stop for the
	// pointer-receiver Stop.)
	typ := recv.Args[len(recv.Args)-1]
	if id, ok := typ.(*ast.Ident); ok {
		return strings.HasSuffix(id.Name, "Ptr")
	}
	if call, ok := typ.(*ast.CallExpr); ok {
		return callExprFun(call) == "types.NewPointer"
	}
	return false
}

// extractEmbeddedSynthetics extracts exported symbols from the embedded
// synthetic time/context Go sources in internal/frontend/
// synth_time_context.go (the syntheticTimeSource/syntheticContextSource
// string constants — the same technique p3frontend's timecontext_test.go
// uses). Exported package-level funcs/types/vars/consts become
// path.Name; exported methods of exported named types become
// path.T.M / path.(*T).M by receiver form.
func (p *ProvidedSet) extractEmbeddedSynthetics(path string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
				continue
			}
			name := vs.Names[0].Name
			if name != "syntheticTimeSource" && name != "syntheticContextSource" {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return fmt.Errorf("%s: %s is not a string literal", path, name)
			}
			src, err := strconv.Unquote(lit.Value)
			if err != nil {
				return fmt.Errorf("%s: unquote %s: %w", path, name, err)
			}
			found = true
			if err := p.extractGoSource(src, fmt.Sprintf(
				"synthetic: internal/frontend/synth_time_context.go %s", name)); err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("%s: synthetic sources not found (source drift?)", path)
	}
	return nil
}

// extractGoSource collects exported symbols from one parsed Go source,
// qualified by the source's own package clause.
func (p *ProvidedSet) extractGoSource(src, prov string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, 0)
	if err != nil {
		return fmt.Errorf("parse embedded synthetic source: %w", err)
	}
	return p.extractGoSourceFile(f, f.Name.Name, prov)
}

// extractGoSourceAs collects exported symbols from one Go source,
// qualified by the explicit package path (the qualifier linkname
// targets use — the embedded source's package clause may differ from
// its import path).
func (p *ProvidedSet) extractGoSourceAs(pkgPath, src, prov string) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "synth.go", src, 0)
	if err != nil {
		return fmt.Errorf("parse embedded synthetic source: %w", err)
	}
	return p.extractGoSourceFile(f, pkgPath, prov)
}

// extractGoSourceFile walks one parsed file's declarations.
func (p *ProvidedSet) extractGoSourceFile(f *ast.File, pkg, prov string) error {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !isExported(d.Name.Name) {
				continue
			}
			if d.Recv == nil {
				p.add(pkg+"."+d.Name.Name, prov+" func")
				continue
			}
			// Method: resolve receiver type name + pointer form.
			if len(d.Recv.List) == 0 {
				continue
			}
			t, ptr := recvTypeName(d.Recv.List[0].Type)
			if t == "" || !isExported(t) {
				continue
			}
			sym := pkg + "." + t + "." + d.Name.Name
			if ptr {
				sym = pkg + ".(*" + t + ")." + d.Name.Name
			}
			p.add(sym, prov+" method")
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if isExported(s.Name.Name) {
						p.add(pkg+"."+s.Name.Name, prov+" type")
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if isExported(n.Name) {
							p.add(pkg+"."+n.Name, prov)
						}
					}
				}
			}
		}
	}
	return nil
}

// recvTypeName returns the named type and pointer-ness of a receiver
// type expression.
func recvTypeName(e ast.Expr) (string, bool) {
	switch t := e.(type) {
	case *ast.StarExpr:
		name, _ := identName(t.X)
		return name, true
	case *ast.Ident:
		return t.Name, false
	}
	return "", false
}

// --- small AST helpers ---

func identName(e ast.Expr) (string, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// nameArg returns the unquoted string literal at argument position i.
func nameArg(call *ast.CallExpr, i int) string {
	if i >= len(call.Args) {
		return ""
	}
	lit, ok := call.Args[i].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

// callExprFun returns the dotted callee name of a call expression.
func callExprFun(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return id.Name + "." + f.Sel.Name
		}
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}
