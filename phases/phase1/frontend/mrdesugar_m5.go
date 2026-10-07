package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
)

// P3-M5 (WS E, ADR-0005 Appendix M): multi-result call-site desugar.
//
// v1 cannot emit or call multi-result functions (WS D; the P3-M3 F4
// fail-fast precedent). Five operations desugar at call sites —
// context.WithCancel, context.WithCancelCause, context.WithDeadline,
// context.WithTimeout, time.ParseDuration — keeping exact gc 1.27.1
// signatures (so type-checking works) with fail-fast bodies. When one
// of them is the RHS of a 2-target assignment (`:=` or `=`), the
// exporter desugars to the synthetic single-result helpers plus field
// extraction:
//
//	ctx, cancel := context.WithCancel(parent)
//	// →
//	__p5mr_N := context.__withCancel(parent) // *cancelCtx, real Go
//	ctx := __p5mr_N                         // concrete type, no Context()
//	                                        // boxing (afc0dfb amendment:
//	                                        // v1 cannot emit the
//	                                        // multi-result Deadline
//	                                        // method the interface
//	                                        // would require)
//	cancel := __p5mr_N.cancelFunc()          // CancelFunc
//
//	d, err := time.ParseDuration(s)
//	// →
//	__p5mr_N := time.__parseDuration(s)      // durationPair, real Go
//	d := __p5mr_N.d
//	err := __p5mr_N.err
//
// The sixth Appendix M operation, ctx.Deadline(), is NOT callable in M5:
// the afc0dfb amendment removed Deadline from the synthetic Context
// interface (v1 cannot emit multi-result methods, WS D), so an
// interface-typed call is a go/types type error that never reaches the
// exporter (see TestDeadlineNotCallableM5). The mrDeadline
// classification path below is retained defense-in-depth — it keeps the
// __deadlineOf desugar wired for the day WS D lands and Deadline returns
// to the interface (re-test trigger: add a 2-target ctx.Deadline()
// corpus test then). It is currently unreachable from user code: the
// synthetic interface omits Deadline, the concrete types are unexported,
// and a user interface re-adding Deadline fails isDeadlineMethod.
//
// P3-M7 (wave 1) extends the desugar beyond Appendix M:
//
//   - mrStrconv: the synthetic strconv package's multi-result functions
//     (ParseUint/ParseInt/Atoi/ParseBool/ParseFloat/ParseComplex/Unquote/
//     QuotedPrefix → __parseUintPub/__parseIntPub/__atoiPub/
//     __parseBoolPub/__parseFloat/__parseComplexPub/__unquotePub/
//     __quotedPrefix + pair structs with fields [v, err]; UnquoteChar →
//     __unquoteChar + unquoteCharPair [value, multibyte, tail, err]).
//     UnquoteChar is the first 4-result desugar: exportMultiResultDesugar
//     now checks arity against the plan (len(pairFields)) instead of a
//     hardcoded 2, so N-target assignments of pair-kind calls desugar.
//   - mrJson: the synthetic encoding/json package-level multi-result
//     functions (Marshal → __marshal/marshalPair, MarshalIndent →
//     __marshalIndent/marshalIndentPair, Indent → __indent/indentPair;
//     fields [data, err]).
//   - mrJsonMethod: the synthetic encoding/json multi-result methods
//     ((Number).Float64 → __numberFloat64, (Number).Int64 →
//     __numberInt64, (*Decoder).Token → __decoderToken,
//     (RawMessage).MarshalJSON → __rawMessageMarshalJSON; prependRecv
//     like mrXmlToken).
//
// Every other syntactic position keeps the fail-fast scope error. The
// helpers are real Go (no new Java nodes); the unexported names are
// resolved by the exporter directly off the synthetic package scope
// (user code can never name them — go/types rejects unexported
// cross-package references — so there is no collision).

// mrKind classifies the Appendix M operations: the 5 reachable ones
// plus mrDeadline, retained defense-in-depth for WS D (see the package
// header — currently unreachable from user code), plus the P3-M7 wave-1
// extensions mrStrconv/mrJson/mrJsonMethod.
type mrKind int

const (
	mrNone mrKind = iota
	mrWithCancel   // context.WithCancel / context.WithCancelCause
	mrWithDeadline // context.WithDeadline / context.WithTimeout
	mrDeadline     // ctx.Deadline() (method) — retained, unreachable in M5
	mrParseDur     // time.ParseDuration
	// P3-M7 (wave 1): xml.Decoder.Token / xml.Decoder.RawToken.
	mrXmlToken // xml.Decoder.Token() / xml.Decoder.RawToken()
	// P3-M7 (wave 1): synthetic strconv multi-result functions.
	mrStrconv // strconv.ParseUint/ParseInt/Atoi/ParseBool/ParseFloat/ParseComplex/Unquote/QuotedPrefix/UnquoteChar
	// P3-M7 (wave 1): synthetic encoding/json package-level multi-result
	// functions.
	mrJson // json.Marshal/MarshalIndent/Indent
	// P3-M7 (wave 1): synthetic encoding/json multi-result methods.
	mrJsonMethod // (json.Number).Float64/Int64, (*json.Decoder).Token, (json.RawMessage).MarshalJSON
)

// mrPlan is the desugar plan for one classified call.
type mrPlan struct {
	kind       mrKind
	helperPkg  string // "context" or "time"
	helperName string // "__withCancel", "__deadlineOf", ...
	// tmpType is the helper's single result type (the __p5mr_N temp).
	tmpType types.Type
	// For mrWithCancel/mrWithDeadline: the second extraction is a
	// method call on the temp (methodRecv.methodName()).
	methodRecv *types.Named
	methodName string
	// For mrDeadline/mrParseDur: the pair field names ([d, ok]/[d, err]).
	pairFields []string
	prependRecv bool
}

// classifyMultiResultCall recognizes the Appendix M operations (the 5
// reachable ones; the mrDeadline path is retained defense-in-depth for
// WS D — see the package header) plus the P3-M7 wave-1 extensions
// (mrStrconv, mrJson, mrJsonMethod — see the package header). It returns
// nil when c is not one of them.
func (e *exporter) classifyMultiResultCall(c *ast.CallExpr) *mrPlan {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	ctxPkg := getSyntheticPackage("context").pkg
	timePkg := getSyntheticPackage("time").pkg
	named := func(pkg *types.Package, name string) *types.Named {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		return nt
	}
	strconvPkg := getSyntheticPackage("strconv").pkg
	jsonPkg := getSyntheticPackage("encoding/json").pkg
	// Package-qualified call: context.WithCancel(...) etc.
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := e.info.Uses[id].(*types.PkgName); ok {
			obj := e.info.Uses[sel.Sel]
			fn, ok := obj.(*types.Func)
			if !ok {
				return nil
			}
			switch pn.Imported().Path() {
			case "context":
				switch fn.Name() {
				case "WithCancel", "WithCancelCause":
					method := "cancelFunc"
					if fn.Name() == "WithCancelCause" {
						method = "cancelCauseFunc"
					}
					nt := named(ctxPkg, "cancelCtx")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrWithCancel, helperPkg: "context",
						helperName: "__with" + fn.Name()[4:],
						tmpType:    types.NewPointer(nt),
						methodRecv: nt, methodName: method,
					}
				case "WithDeadline", "WithTimeout":
					nt := named(ctxPkg, "timerCtx")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrWithDeadline, helperPkg: "context",
						helperName: "__with" + fn.Name()[4:],
						tmpType:    types.NewPointer(nt),
						methodRecv: nt, methodName: "cancelFunc",
					}
				}
			case "time":
				if fn.Name() == "ParseDuration" {
					nt := named(timePkg, "durationPair")
					if nt == nil {
						return nil
					}
					return &mrPlan{
						kind: mrParseDur, helperPkg: "time",
						helperName: "__parseDuration",
						tmpType:    nt, pairFields: []string{"d", "err"},
					}
				}
			case "strconv":
				// P3-M7 (wave 1): multi-result strconv functions desugar
				// to their single-result __-helpers + pair structs.
				if plan := classifyStrconvFunc(strconvPkg, fn); plan != nil {
					return plan
				}
			case "encoding/json":
				// P3-M7 (wave 1): multi-result json package functions
				// desugar to their single-result __-helpers + pairs.
				if plan := classifyJsonFunc(jsonPkg, fn); plan != nil {
					return plan
				}
			}
			return nil
		}
	}
	// Method call: ctx.Deadline(). Must be the synthetic context method
	// (declared in package "context", 2 results) — a user type's
	// Deadline method is not desugared. NOTE: unreachable in M5 (the
	// synthetic Context interface omits Deadline post-afc0dfb, so no
	// user call site type-checks); retained for WS D — see the package
	// header.
	if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
		if f, ok := selection.Obj().(*types.Func); ok && isDeadlineMethod(f) {
			nt := named(ctxPkg, "deadlinePair")
			if nt == nil {
				return nil
			}
			return &mrPlan{
				kind: mrDeadline, helperPkg: "context",
				helperName: "__deadlineOf",
				tmpType:    nt, pairFields: []string{"d", "ok"},
			}
		}
		if f, ok := selection.Obj().(*types.Func); ok {
			if helper, ok := isXmlTokenMethod(f); ok {
				xmlPkg := getSyntheticPackage("encoding/xml").pkg
				nt := named(xmlPkg, "tokenPair")
				if nt == nil {
					return nil
				}
				return &mrPlan{
					kind: mrXmlToken, helperPkg: "encoding/xml",
					helperName: helper, prependRecv: true,
					tmpType:    nt, pairFields: []string{"tok", "err"},
				}
			}
			// P3-M7 (wave 1): multi-result encoding/json methods
			// ((Number).Float64/Int64, (*Decoder).Token) desugar to
			// their single-result __-helpers (prependRecv: the
			// receiver becomes the helper's first argument).
			if helper, pair, fields, ok := isJsonDesugarMethod(f); ok {
				nt := named(jsonPkg, pair)
				if nt == nil {
					return nil
				}
				return &mrPlan{
					kind: mrJsonMethod, helperPkg: "encoding/json",
					helperName: helper, prependRecv: true,
					tmpType:    nt, pairFields: fields,
				}
			}
		}
	}
	return nil
}

func isXmlTokenMethod(f *types.Func) (string, bool) {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "encoding/xml" {
		return "", false
	}
	if f.Name() != "Token" && f.Name() != "RawToken" {
		return "", false
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() != 2 {
		return "", false
	}
	recv := sig.Recv()
	if recv == nil {
		return "", false
	}
	pt, ok := recv.Type().(*types.Pointer)
	if !ok {
		return "", false
	}
	nt, ok := pt.Elem().(*types.Named)
	if !ok || nt.Obj() == nil || nt.Obj().Name() != "Decoder" {
		return "", false
	}
	if nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "encoding/xml" {
		return "", false
	}
	if f.Name() == "Token" {
		return "__tokenOf", true
	}
	return "__rawTokenOf", true
}

// strconvDesugarTarget is one multi-result strconv function's
// single-result __-helper, pair type, and pair field names.
type strconvDesugarTarget struct {
	helper string
	pair   string
	fields []string
}

// classifyStrconvFunc classifies the synthetic strconv package's
// multi-result functions (P3-M7 wave 1). Each keeps its exact gc 1.27.1
// signature for type-checking with a fail-fast body; call sites desugar
// to the single-result __-helper plus pair-field extraction.
// UnquoteChar is a 4-result call (fields value/multibyte/tail/err) —
// the arity check in exportMultiResultDesugar handles it.
func classifyStrconvFunc(strconvPkg *types.Package, fn *types.Func) *mrPlan {
	targets := map[string]strconvDesugarTarget{
		"ParseUint":    {"__parseUintPub", "parseUintPair", []string{"v", "err"}},
		"ParseInt":     {"__parseIntPub", "parseIntPair", []string{"v", "err"}},
		"Atoi":         {"__atoiPub", "atoiPair", []string{"v", "err"}},
		"ParseBool":    {"__parseBoolPub", "parseBoolPair", []string{"v", "err"}},
		"ParseFloat":   {"__parseFloat", "parseFloatPair", []string{"v", "err"}},
		"ParseComplex": {"__parseComplexPub", "parseComplexPair", []string{"v", "err"}},
		"Unquote":      {"__unquotePub", "unquotePair", []string{"v", "err"}},
		"QuotedPrefix": {"__quotedPrefix", "quotedPrefixPair", []string{"v", "err"}},
		"UnquoteChar":  {"__unquoteChar", "unquoteCharPair", []string{"value", "multibyte", "tail", "err"}},
	}
	t, ok := targets[fn.Name()]
	if !ok {
		return nil
	}
	obj := strconvPkg.Scope().Lookup(t.pair)
	if obj == nil {
		return nil
	}
	nt, _ := obj.Type().(*types.Named)
	if nt == nil {
		return nil
	}
	return &mrPlan{
		kind: mrStrconv, helperPkg: "strconv",
		helperName: t.helper,
		tmpType:    nt, pairFields: t.fields,
	}
}

// classifyJsonFunc classifies the synthetic encoding/json package's
// multi-result functions (P3-M7 wave 1): Marshal, MarshalIndent, Indent.
// Each keeps its exact gc 1.27.1 signature for type-checking with a
// fail-fast body; call sites desugar to the single-result __-helper
// plus pair-field extraction ([data, err]).
func classifyJsonFunc(jsonPkg *types.Package, fn *types.Func) *mrPlan {
	type target struct {
		helper string
		pair   string
	}
	targets := map[string]target{
		"Marshal":       {"__marshal", "marshalPair"},
		"MarshalIndent": {"__marshalIndent", "marshalIndentPair"},
		"Indent":        {"__indent", "indentPair"},
	}
	t, ok := targets[fn.Name()]
	if !ok {
		return nil
	}
	obj := jsonPkg.Scope().Lookup(t.pair)
	if obj == nil {
		return nil
	}
	nt, _ := obj.Type().(*types.Named)
	if nt == nil {
		return nil
	}
	return &mrPlan{
		kind: mrJson, helperPkg: "encoding/json",
		helperName: t.helper,
		tmpType:    nt, pairFields: []string{"data", "err"},
	}
}

// isJsonDesugarMethod classifies the synthetic encoding/json
// package's multi-result methods that desugar at call sites (P3-M7 wave
// 1): (Number).Float64, (Number).Int64 (value receivers), and
// (*Decoder).Token (pointer receiver). Returns the single-result helper
// name, the pair type name, and the pair field names. A user type's
// method of the same name is never classified: the receiver's named
// type must be the synthetic json package's Number/Decoder.
func isJsonDesugarMethod(f *types.Func) (helper, pair string, fields []string, ok bool) {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "encoding/json" {
		return "", "", nil, false
	}
	sig, ok := f.Type().(*types.Signature)
	if !ok || sig.Results().Len() != 2 {
		return "", "", nil, false
	}
	recv := sig.Recv()
	if recv == nil {
		return "", "", nil, false
	}
	rt := recv.Type()
	switch {
	case f.Name() == "Float64" || f.Name() == "Int64":
		// Value receivers: (Number).Float64, (Number).Int64.
		if _, isPtr := rt.(*types.Pointer); isPtr {
			return "", "", nil, false
		}
	case f.Name() == "MarshalJSON":
		// Value receiver: (RawMessage).MarshalJSON.
		if _, isPtr := rt.(*types.Pointer); isPtr {
			return "", "", nil, false
		}
	case f.Name() == "Token":
		// Pointer receiver: (*Decoder).Token.
		pt, isPtr := rt.(*types.Pointer)
		if !isPtr {
			return "", "", nil, false
		}
		rt = pt.Elem()
	default:
		return "", "", nil, false
	}
	nt, isNamed := rt.(*types.Named)
	if !isNamed || nt.Obj() == nil || nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "encoding/json" {
		return "", "", nil, false
	}
	switch {
	case f.Name() == "Float64" && nt.Obj().Name() == "Number":
		return "__numberFloat64", "numberFloat64Pair", []string{"f", "err"}, true
	case f.Name() == "Int64" && nt.Obj().Name() == "Number":
		return "__numberInt64", "numberInt64Pair", []string{"i", "err"}, true
	case f.Name() == "Token" && nt.Obj().Name() == "Decoder":
		return "__decoderToken", "decoderTokenPair", []string{"tok", "err"}, true
	case f.Name() == "MarshalJSON" && nt.Obj().Name() == "RawMessage":
		return "__rawMessageMarshalJSON", "rawMessageMarshalJSONPair", []string{"data", "err"}, true
	}
	return "", "", nil, false
}

// isDeadlineMethod reports whether f is the synthetic context package's
// Deadline method (2 results). The mrDeadline desugar path is retained
// defense-in-depth for WS D (see the package header); in M5 it is
// unreachable from user code, so the concrete types' fail-fast Deadline
// bodies never execute via this path either.
func isDeadlineMethod(f *types.Func) bool {
	if f == nil || f.Pkg() == nil || f.Pkg().Path() != "context" || f.Name() != "Deadline" {
		return false
	}
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Results().Len() == 2
}

// exportMultiResultDesugar implements the Appendix M desugar for
// N-target assignments (N=2 for the Appendix M operations; N=4 for
// strconv.UnquoteChar). It returns handled=false when s is not an
// N-target assignment of one of the classified operations.
func (e *exporter) exportMultiResultDesugar(s *ast.AssignStmt, define bool) (*Node, bool, error) {
	if len(s.Rhs) != 1 {
		return nil, false, nil
	}
	c, ok := unparen(s.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	plan := e.classifyMultiResultCall(c)
	if plan == nil {
		return nil, false, nil
	}
	// Arity: pair-struct kinds need exactly len(pairFields) targets;
	// method-extraction kinds (mrWithCancel/mrWithDeadline) need 2.
	// A classified call in any other arity (e.g. `v := strconv.Atoi(s)`)
	// is NOT desugared — it falls through to the fail-fast scope error
	// (isMultiResultSynthetic), never silently half-compiled.
	want := len(plan.pairFields)
	switch plan.kind {
	case mrWithCancel, mrWithDeadline:
		want = 2
	}
	if want == 0 || len(s.Lhs) != want {
		return nil, false, nil
	}
	n, err := e.desugarMultiResult(s, c, plan, define)
	return n, true, err
}

// desugarMultiResult emits the desugared block for a classified call.
func (e *exporter) desugarMultiResult(s *ast.AssignStmt, c *ast.CallExpr, plan *mrPlan, define bool) (*Node, error) {
	sp := getSyntheticPackage(plan.helperPkg)
	helperObj := sp.pkg.Scope().Lookup(plan.helperName)
	helperFn, ok := helperObj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: synthetic helper %s.%s missing", plan.helperPkg, plan.helperName)
	}
	// Helper call args: the original call's args, except for the
	// Deadline method where the receiver becomes the argument.
	var argExprs []ast.Expr
	if plan.kind == mrDeadline {
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: Deadline call has non-selector fun %T", c.Fun)
		}
		argExprs = []ast.Expr{sel.X}
	} else {
		argExprs = c.Args
	}
	if plan.prependRecv {
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: prependRecv call has non-selector fun %T", c.Fun)
		}
		argExprs = append([]ast.Expr{sel.X}, argExprs...)
	}
	args := make([]*Node, 0, len(argExprs))
	for _, a := range argExprs {
		x, err := e.exportExpr(a)
		if err != nil {
			return nil, err
		}
		args = append(args, x)
	}
	helperCall, tmpName, tmpID, tmpTid, err := e.synthHelperCall(c, helperFn, plan.tmpType, args)
	if err != nil {
		return nil, err
	}
	// __p5mr_N := <helper>(args)
	stmts := []*Node{e.node("vardecl", s, map[string]any{
		"names": []string{tmpName}, "objs": []int{tmpID},
		"define": true, "type": nil, "values": []*Node{helperCall},
	})}
	// The two extractions.
	var extracts []*Node
	switch plan.kind {
	case mrWithCancel, mrWithDeadline:
		// P3-M5: use the concrete *cancelCtx/*timerCtx directly (no
		// Context() interface conversion). The interface conversion would
		// require the multi-result Deadline method in the P3-M3 method
		// table (v1 cannot emit it); the concrete type supports all
		// method calls directly.
		conv := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
		mcall, err := e.synthMethodCall(c, tmpName, tmpID, tmpTid, plan.methodRecv, plan.methodName)
		if err != nil {
			return nil, err
		}
		extracts = []*Node{conv, mcall}
	case mrDeadline, mrParseDur, mrXmlToken, mrStrconv, mrJson, mrJsonMethod:
		for i, field := range plan.pairFields {
			sel, err := e.synthPairField(c, tmpName, tmpID, tmpTid, plan.tmpType, field, i)
			if err != nil {
				return nil, err
			}
			extracts = append(extracts, sel)
		}
	default:
		return nil, fmt.Errorf("gofrontend: unknown desugar kind %d", plan.kind)
	}
	// Assign the extractions to the two targets (blank skips).
	for i, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return nil, scopeErrorf("multi-result desugar with non-identifier lhs %T is not supported in frontend/v1 scope (WS D)", lhs)
		}
		if id.Name == "_" {
			continue
		}
		if define {
			v, ok := e.info.ObjectOf(id).(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: := name %q does not denote a variable", id.Name)
			}
			oid, err := e.defVar(v, false)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, e.node("vardecl", s, map[string]any{
				"names": []string{id.Name}, "objs": []int{oid},
				"define": true, "type": nil, "values": []*Node{extracts[i]},
			}))
		} else {
			lhsNode, err := e.exportAssignable(lhs)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, e.node("assign", s, map[string]any{
				"tok": "=", "lhs": []*Node{lhsNode}, "rhs": []*Node{extracts[i]},
			}))
		}
	}
	return e.node("block", s, map[string]any{"stmts": stmts}), nil
}

// synthHelperCall builds a "call" node to a synthetic package-level
// function (mirroring exportFuncCall's non-generic path) and declares
// the __p5mr_N temp holding its result, returning the call node plus
// the temp's name/obj/type ids.
func (e *exporter) synthHelperCall(c *ast.CallExpr, fn *types.Func, resultType types.Type, args []*Node) (*Node, string, int, int, error) {
	fi, ok := e.funcByObj[fn]
	if !ok {
		return nil, "", 0, 0, fmt.Errorf("gofrontend: synthetic helper %s not in funcByObj", fn.Name())
	}
	sigID, err := e.typeID(fn.Type())
	if err != nil {
		return nil, "", 0, 0, err
	}
	fun := e.node("ident", c, map[string]any{
		"name": fn.Name(), "obj": fi.funcID, "type": sigID,
	})
	resTid, err := e.typeID(resultType)
	if err != nil {
		return nil, "", 0, 0, err
	}
	call := e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": resTid,
	})
	e.chanSeq++
	tmpName := fmt.Sprintf("__p5mr_%d", e.chanSeq)
	v := types.NewVar(token.NoPos, nil, tmpName, resultType)
	tmpID, err := e.defVar(v, false)
	if err != nil {
		return nil, "", 0, 0, err
	}
	return call, tmpName, tmpID, resTid, nil
}

// synthMethodCall builds the __p5mr_N.method() call node (mirroring
// exportMethodCall's methodref construction).
func (e *exporter) synthMethodCall(c *ast.CallExpr, tmpName string, tmpID, tmpTid int, recv *types.Named, method string) (*Node, error) {
	fi, ok := e.methodIndex[methodKey{recv.Obj(), method}]
	if !ok || fi == nil {
		return nil, fmt.Errorf("gofrontend: synthetic method %s.%s not in methodIndex", recv.Obj().Name(), method)
	}
	sigID, err := e.freshSigID(fi.recv, fi.sig.Params(), fi.sig.Results(), fi.sig.Variadic())
	if err != nil {
		return nil, err
	}
	recvNode := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
	fun := e.node("methodref", c, map[string]any{"obj": fi.funcID, "recv": recvNode, "type": sigID})
	resTid, err := e.typeID(fi.sig.Results().At(0).Type())
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": []*Node{}, "type": resTid,
	}), nil
}

// synthPairField builds the __p5mr_N.<field> selector node for a pair
// struct (field index i).
func (e *exporter) synthPairField(c *ast.CallExpr, tmpName string, tmpID, tmpTid int, pairType types.Type, field string, i int) (*Node, error) {
	nt, ok := pairType.(*types.Named)
	if !ok {
		return nil, fmt.Errorf("gofrontend: pair type %v is not named", pairType)
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok || i < 0 || i >= st.NumFields() {
		return nil, fmt.Errorf("gofrontend: pair type %v has no field %d", pairType, i)
	}
	if st.Field(i).Name() != field {
		return nil, fmt.Errorf("gofrontend: pair field %d is %q, want %q", i, st.Field(i).Name(), field)
	}
	ftid, err := e.typeID(st.Field(i).Type())
	if err != nil {
		return nil, err
	}
	x := e.node("ident", c, map[string]any{"name": tmpName, "obj": tmpID, "type": tmpTid})
	return e.node("selector", c, map[string]any{
		"x": x, "field": field, "selpath": []any{i}, "type": ftid,
	}), nil
}
