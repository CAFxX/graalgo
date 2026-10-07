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
// P3-M7 (wave 1) extends Appendix M to the encoding/* synthetic
// packages (mrM7, m7MultiResultFuncs table below): their multi-result
// members keep exact gc signatures with fail-fast bodies, and the real
// implementations live in single-result __-helpers returning
// pair/triple structs. 2-target (and, for ascii85.Decode, 3-target)
// assignments desugar to a helper call plus field extractions; every
// other position fails fast with the named M7 scope error via
// isMultiResultSynthetic. asn1.Unmarshal is the deliberate exception:
// decoding stores through the recipient pointer, which v1 cannot
// express (WS D), so it is not in the table and fails fast everywhere.
//
// Every other syntactic position keeps the fail-fast scope error. The
// helpers are real Go (no new Java nodes); the unexported names are
// resolved by the exporter directly off the synthetic package scope
// (user code can never name them — go/types rejects unexported
// cross-package references — so there is no collision).

// mrKind classifies the Appendix M operations: the 5 reachable ones
// plus mrDeadline, retained defense-in-depth for WS D (see the package
// header — currently unreachable from user code).
type mrKind int

const (
	mrNone mrKind = iota
	mrWithCancel   // context.WithCancel / context.WithCancelCause
	mrWithDeadline // context.WithDeadline / context.WithTimeout
	mrDeadline     // ctx.Deadline() (method) — retained, unreachable in M5
	mrParseDur     // time.ParseDuration
	// P3-M7 (wave 1): xml.Decoder.Token / xml.Decoder.RawToken.
	mrXmlToken // xml.Decoder.Token() / xml.Decoder.RawToken()
	// P3-M7 (wave 1): csv.Reader.Read / csv.Reader.ReadAll.
	mrCsvRead // csv.Reader.Read() / csv.Reader.ReadAll()
	mrCsvFieldPos // csv.Reader.FieldPos
	// P3-M7 (wave 1): the encoding/* multi-result members (Decode,
	// DecodeString, AppendDecode, Uvarint, Varint, ascii85.Decode,
	// pem.Decode, asn1.Marshal/Unmarshal). One kind for all of them —
	// the pair/triple distinction is data (len(pairFields)), selected
	// from the m7MultiResultFuncs table below.
	mrM7
)

// m7MultiResultFunc describes one desugarable M7 multi-result function:
// the exact gc 1.27.1 multi-result signature is kept for type-checking
// with a fail-fast body, and the real implementation lives in the
// single-result __-helper returning the pair/triple struct.
type m7MultiResultFunc struct {
	pkgPath    string   // synthetic package path
	name       string   // function/method name
	recvType   string   // receiver named type ("" for package-level funcs)
	helper     string   // __-helper name
	pairType   string   // pair/triple struct type name
	pairFields []string // field names in result order (must match the
	// multi-result signature's result order positionally)
}

// m7MultiResultFuncs is the desugar table for the encoding/* packages.
// Every entry was verified against the synthetic source it names:
// helper name, pair struct name, and field names/order match the
// source's __-helper and pair struct exactly (e.g. base32's AppendDecode
// desugars through base32DecodeStringPair{b, err} — there is no
// base32AppendPair).
var m7MultiResultFuncs = []m7MultiResultFunc{
	// encoding/base32: methods on *Encoding.
	{"encoding/base32", "Decode", "Encoding", "__base32Decode", "base32DecodePair", []string{"n", "err"}},
	{"encoding/base32", "DecodeString", "Encoding", "__base32DecodeString", "base32DecodeStringPair", []string{"b", "err"}},
	{"encoding/base32", "AppendDecode", "Encoding", "__base32AppendDecode", "base32DecodeStringPair", []string{"b", "err"}},
	// encoding/base64: methods on *Encoding.
	{"encoding/base64", "Decode", "Encoding", "__base64Decode", "base64DecodePair", []string{"n", "err"}},
	{"encoding/base64", "DecodeString", "Encoding", "__base64DecodeString", "base64DecodeStringPair", []string{"b", "err"}},
	{"encoding/base64", "AppendDecode", "Encoding", "__base64AppendDecode", "base64DecodeStringPair", []string{"b", "err"}},
	// encoding/hex: package-level funcs.
	{"encoding/hex", "Decode", "", "__hexDecode", "hexDecodePair", []string{"n", "err"}},
	{"encoding/hex", "DecodeString", "", "__hexDecodeString", "hexDecodeStringPair", []string{"b", "err"}},
	{"encoding/hex", "AppendDecode", "", "__hexAppendDecode", "hexDecodeStringPair", []string{"b", "err"}},
	// encoding/binary: package-level funcs.
	{"encoding/binary", "Uvarint", "", "__binaryUvarint", "binaryUvarintPair", []string{"x", "n"}},
	{"encoding/binary", "Varint", "", "__binaryVarint", "binaryVarintPair", []string{"x", "n"}},
	// encoding/ascii85: package-level func, 3 results.
	{"encoding/ascii85", "Decode", "", "__ascii85Decode", "ascii85DecodeTriple", []string{"ndst", "nsrc", "err"}},
	// encoding/pem: package-level func.
	{"encoding/pem", "Decode", "", "__pemDecode", "pemDecodePair", []string{"p", "rest"}},
	// encoding/asn1: package-level func. The __-helper implements the
	// scalar cases in real Go and fails fast (needs reflect) for composite
	// types — the desugar applies, the reflect limit moves into the helper.
	// (Unmarshal is NOT desugared: decoding stores through the recipient
	// pointer, which v1 cannot express — WS D — so it fails fast at every
	// call site via isMultiResultSynthetic.)
	{"encoding/asn1", "Marshal", "", "__asn1Marshal", "asn1MarshalPair", []string{"b", "err"}},
	// NOTE: the top-level "encoding" package has no functions (only the six
	// marshaling interfaces), so it needs no desugar entry. Its interface
	// methods are multi-result and fail fast via isMultiResultSynthetic.
}

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
// WS D — see the package header). It returns nil when c is not one of
// them.
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
	// Package-qualified call: context.WithCancel(...) etc.
	if id, ok := sel.X.(*ast.Ident); ok {
		if pn, ok := e.info.Uses[id].(*types.PkgName); ok {
			obj := e.info.Uses[sel.Sel]
			fn, ok := obj.(*types.Func)
			if !ok {
				return nil
			}
			// P3-M7 (wave 1): encoding/* package-level multi-result
			// members (hex.Decode, binary.Uvarint, ascii85.Decode,
			// pem.Decode, asn1.Marshal/Unmarshal).
			if plan := classifyM7MultiResultCall(pn.Imported().Path(), fn); plan != nil {
				return plan
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
		}
		// P3-M7 (wave 1): csv.Reader.Read / csv.Reader.ReadAll.
		// The receiver's named type must match — a user method with the
		// same name is never desugared.
		if f, ok := selection.Obj().(*types.Func); ok && f.Pkg() != nil && f.Pkg().Path() == "encoding/csv" {
			if sig, ok := f.Type().(*types.Signature); ok && sig.Recv() != nil {
				if recvNamed, ok := sig.Recv().Type().(*types.Named); ok && recvNamed.Obj().Name() == "Reader" {
					if sel.Sel.Name == "Read" || sel.Sel.Name == "ReadAll" {
						csvPkg := getSyntheticPackage("encoding/csv").pkg
						nt := named(csvPkg, "csvReadPair")
						if nt == nil {
							return nil
						}
						return &mrPlan{
							kind: mrCsvRead, helperPkg: "encoding/csv",
							helperName: "csvReadPair", prependRecv: true,
							tmpType: nt, pairFields: []string{"rec", "err"},
						}
					}
				}
			}
		}
		// P3-M7 (wave 1): encoding/* multi-result methods
		// (base32/base64 (*Encoding).Decode/DecodeString/AppendDecode).
		// The receiver's named type must match the table — a user method
		// with the same name is never desugared.
		if f, ok := selection.Obj().(*types.Func); ok && f.Pkg() != nil {
			if plan := classifyM7MultiResultCall(f.Pkg().Path(), f); plan != nil {
				return plan
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

// classifyM7MultiResultCall recognizes the P3-M7 (wave 1) Appendix M
// operations (the m7MultiResultFuncs table). pkgPath is the called
// function's declaring package; for methods the receiver's named type
// must match the table entry — a user type with the same method name
// (e.g. a local Encoding.Decode) is never desugared. Returns nil when
// the call is not one of the table's operations.
func classifyM7MultiResultCall(pkgPath string, fn *types.Func) *mrPlan {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return nil
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil
	}
	recvName := ""
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			recvName = nt.Obj().Name()
		}
	}
	for _, mf := range m7MultiResultFuncs {
		if mf.pkgPath != pkgPath || mf.name != fn.Name() || mf.recvType != recvName {
			continue
		}
		// The result count must match the pair/triple arity: a signature
		// change in the synthetic source fails the desugar loudly instead
		// of extracting the wrong fields.
		if sig.Results().Len() != len(mf.pairFields) {
			return nil
		}
		sp := getSyntheticPackage(mf.pkgPath)
		obj := sp.pkg.Scope().Lookup(mf.pairType)
		if obj == nil {
			return nil
		}
		nt, _ := obj.Type().(*types.Named)
		if nt == nil {
			return nil
		}
		return &mrPlan{
			kind: mrM7, helperPkg: mf.pkgPath,
			helperName: mf.helper,
			tmpType:    nt, pairFields: mf.pairFields,
			prependRecv: mf.recvType != "",
		}
	}
	return nil
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
// N-target assignments. It returns handled=false when s is not an
// N-target assignment of one of the Appendix M operations.
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
	// The target count must match the plan's extraction count: 2 for the
	// M5 kinds and the 2-result M7 ops; 3 for the 3-result M7 op
	// (ascii85.Decode). Any other arity is not a desugar position.
	want := len(plan.pairFields)
	if plan.kind == mrWithCancel || plan.kind == mrWithDeadline {
		want = 2
	}
	if len(s.Lhs) != want {
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
	case mrDeadline, mrParseDur, mrXmlToken, mrCsvRead, mrCsvFieldPos, mrM7:
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
