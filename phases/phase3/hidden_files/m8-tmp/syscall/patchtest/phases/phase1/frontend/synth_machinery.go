package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sync"
)

// syntheticPackage is a parsed + type-checked synthetic package: the
// *types.Package the overlay importer hands out, plus the AST/Info the
// exporter needs to emit its real-Go declarations.
type syntheticPackage struct {
	path  string
	src   string
	pkg   *types.Package
	files []*ast.File
	info  *types.Info
	fset  *token.FileSet
	raw   []byte
}

var (
	synthMu    sync.Mutex
	synthCache = map[string]*syntheticPackage{}
)

// syntheticSources maps import path to embedded source.
var syntheticSources = map[string]string{
	"time":    syntheticTimeSource,
	"context": syntheticContextSource,
	// P3-M7 (wave 1): synthetic errors, sort, bytes, math, strings packages
	// (real-Go sources in synth_errors.go / synth_sort.go / synth_bytes.go /
	// synth_math.go / synth_strings.go, resolved via getSyntheticPackage so
	// the same *types.Package instance is shared between user type-check
	// and synthetic emission).
	"errors": syntheticErrorsSource,
	"sort":   syntheticSortSource,
	"bytes":  syntheticBytesSource,
	"math":   syntheticMathSource,
	"strings": syntheticStringsSource,
	// P3-M7 (wave 1): encoding/csv + encoding/gob (synth_enc_csv_gob.go).
	"encoding/csv": syntheticCsvSource,
	"encoding/gob": syntheticGobSource,
	// P3-M7 (wave 1): strconv (synth_strconv.go) and encoding/json
	// (synth_json.go). Pure-Go, self-contained.
	"strconv":        syntheticStrconvSource,
	"encoding/json": syntheticJsonSource,
	// P3-M7 (wave 1): synthetic encoding/xml package (real-Go source in
	// synth_xml.go). Self-contained (no imports); the token-stream
	// desugar lives in mrdesugar_m5.go (mrXmlToken).
	"encoding/xml": syntheticXmlSource,
	// P3-M7 (wave 1): the 8 encoding/* primitive packages (synth_base32.go
	// ... synth_encoding.go). encoding/pem imports synthetic
	// encoding/base64 (resolved via getSyntheticPackage, like the
	// bytes->errors dependency); the rest are self-contained. The
	// multi-result members desugar at call sites (Appendix M, mrM7 in
	// mrdesugar_m5.go); every other position fails fast.
	"encoding/base32":  syntheticBase32Source,
	"encoding/base64":  syntheticBase64Source,
	"encoding/hex":     syntheticHexSource,
	"encoding/binary":  syntheticBinarySource,
	"encoding/ascii85": syntheticAscii85Source,
	"encoding/pem":     syntheticPemSource,
	"encoding/asn1":    syntheticAsn1Source,
	"encoding":         syntheticEncodingSource,
	// P3-M8 (wave 2): synthetic syscall package (linux/amd64), real-Go
	// source in synth_syscall.go: Errno vocabulary + scalar services via
	// //go:linkname to the frozen graalgo.os.* table (os.md section 2.4, R1/R4).
	// Dependency: the synthetic source imports "io/fs" (Errno.Is
	// sentinels) - the os worker io/fs registration (R5) must land
	// first or together with this entry.
	"syscall": syntheticSyscallSource,
	"io/fs":   syntheticIoFsSource,
}
// the overlay importer, so the context source's `import "time"` resolves
// to the synthetic package (synthetic→synthetic imports).
// The same *types.Package instance is returned to every importer, so
// *types.Func/*types.TypeName objects are identical between the user's
// type-check and the synthetic sources' own.
func getSyntheticPackage(path string) *syntheticPackage {
	synthMu.Lock()
	if sp, ok := synthCache[path]; ok {
		synthMu.Unlock()
		return sp
	}
	synthMu.Unlock()
	sp := buildSyntheticPackage(path)
	synthMu.Lock()
	defer synthMu.Unlock()
	if prev, ok := synthCache[path]; ok {
		return prev
	}
	synthCache[path] = sp
	return sp
}

func buildSyntheticPackage(path string) *syntheticPackage {
	src, ok := syntheticSources[path]
	if !ok {
		panic("gofrontend: no synthetic source for " + path)
	}
	fset := token.NewFileSet()
	// P3-M8 (wave 2): parser.ParseComments so //go:linkname directives
	// are visible on fd.Doc when the exporter lowers bodyless linkname
	// functions (ADR-0004 D1; the synthetic syscall package binds the
	// frozen graalgo.os.* table this way). Comment-free synthetic
	// sources are unaffected.
	file, err := parser.ParseFile(fset, path+".go", src, parser.AllErrors|parser.ParseComments)
	if err != nil {
		panic("gofrontend: synthetic " + path + " parse error: " + err.Error())
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
	}
	conf := types.Config{Importer: overlayImporter{}}
	pkg, err := conf.Check(path, fset, []*ast.File{file}, info)
	if err != nil {
		panic("gofrontend: synthetic " + path + " type error: " + err.Error())
	}
	return &syntheticPackage{
		path: path, src: src, pkg: pkg,
		files: []*ast.File{file}, info: info, fset: fset,
		raw: []byte(src),
	}
}

// timeIntrinsicNames are the synthetic time package's clock/timer entry
// points (ADR-0005 D1): declared with exact gc signatures, always
// intercepted into the §D3 wire nodes, never exported as funcdecls.
var timeIntrinsicNames = map[string]bool{
	"Now": true, "Sleep": true, "After": true, "Tick": true,
	"NewTimer": true, "NewTicker": true, "AfterFunc": true,
}

// isSyntheticIntrinsic reports whether a synthetic package declaration is
// an intrinsic (intercepted at call sites, no funcdecl emitted).
func isSyntheticIntrinsic(path, name string) bool {
	if path == "time" {
		return timeIntrinsicNames[name]
	}
	if path == "math" {
		return mathIntrinsicNames[name]
	}
	return false
}

// timeTimerTickerNamed reports whether t unwraps (through one pointer) to
// the synthetic time.Timer or time.Ticker named type.
func timeTimerTickerNamed(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return timeTimerTickerNamedExact(t)
}

// timeTimerTickerNamedExact is timeTimerTickerNamed without pointer
// dereference: for value positions (var declarations, composite
// literals, struct fields, slice/array/map elements) where *time.Timer
// is legal but time.Timer is not in M5 scope.
func timeTimerTickerNamedExact(t types.Type) *types.Named {
	nt, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	obj := nt.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "time" {
		return nil
	}
	if obj.Name() != "Timer" && obj.Name() != "Ticker" {
		return nil
	}
	return nt
}

// synthGroup is one synthetic package's declarations imported into the
// document.
type synthGroup struct {
	sp  *syntheticPackage
	fis []*funcDeclInfo
}

// importSyntheticPackage merges a synthetic package's real-Go
// declarations into the document: package vars become globals (emitted
// immediately, in source order — they cannot depend on user vars),
// funcs/methods populate funcByObj/methodIndex (their bodies are emitted
// later by exportSyntheticBodies). Intrinsics are skipped. The exporter's
// per-file state is swapped for the duration.
func (e *exporter) importSyntheticPackage(sp *syntheticPackage) error {
	savedFset, savedSrc, savedInfo, savedPkg := e.fset, e.src, e.info, e.pkg
	savedSubst, savedSubstTypeIDs := e.subst, e.substTypeIDs
	e.fset, e.src, e.info, e.pkg = sp.fset, sp.raw, sp.info, sp.pkg
	e.subst, e.substTypeIDs = nil, nil
	defer func() {
		e.fset, e.src, e.info, e.pkg = savedFset, savedSrc, savedInfo, savedPkg
		e.subst, e.substTypeIDs = savedSubst, savedSubstTypeIDs
	}()
	g := synthGroup{sp: sp}
	// Two passes: funcs first (so var initializers can call them),
	// then package vars.
	for _, file := range sp.files {
		for _, decl := range file.Decls {
			d, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj, ok := e.info.Defs[d.Name].(*types.Func)
			if !ok {
				return scopeErrorf("synthetic %s: %s has no *types.Func object", sp.path, d.Name.Name)
			}
			// Intrinsics are package-level functions only; methods with the
			// same name (e.g. Time.After vs time.After) are real methods.
			if d.Recv == nil && isSyntheticIntrinsic(sp.path, obj.Name()) {
				continue
			}
			sig, ok := obj.Type().(*types.Signature)
			if !ok {
				return scopeErrorf("synthetic %s: %s has non-signature type", sp.path, obj.Name())
			}
			// P3-M5 (WS E, ADR-0005): multi-result declarations
			// (Context.Deadline, time.ParseDuration, Time.Date/Clock/Zone)
			// are declared with exact gc signatures for type-checking,
			// but v1 cannot emit or call multi-result functions (WS D;
			// the P3-M3 F4 fail-fast precedent). They are skipped here —
			// never in funcByObj/methodIndex. The Appendix M desugar avoids
			// interface conversions (uses concrete *cancelCtx/*timerCtx
			// directly), so P3-M3 method tables do not need the multi-result
			// Deadline methods. Every call site fails fast with a named M5
			// scope error (see isMultiResultSynthetic), or is desugared.
			if sig.Results().Len() > 1 {
				continue
			}
			funcID, err := e.defFunc(obj)
			if err != nil {
				return err
			}
			fi := &funcDeclInfo{fd: d, obj: obj, funcID: funcID, sig: sig}
			if d.Recv != nil {
				recv := sig.Recv()
				if recv == nil {
					return scopeErrorf("synthetic %s: method %s has no receiver var", sp.path, obj.Name())
				}
				fi.recv = recv
				rn := derefNamed(recv.Type())
				if rn == nil {
					return scopeErrorf("synthetic %s: method %s has non-named receiver type", sp.path, obj.Name())
				}
				e.methodIndex[methodKey{rn.Obj(), obj.Name()}] = fi
			}
			e.funcByObj[obj] = fi
			g.fis = append(g.fis, fi)
		}
	}
	// Package vars, in dependency order (M7): an initializer may read
	// another package-level var declared LATER in the source (e.g. the
	// synthetic json package's jsonOverThresh reads jsonPow2str) — the
	// same topological sort as user package vars (Go spec, Package
	// initialization). e.info is the synthetic package's info here
	// (swapped above), so dependency analysis resolves correctly.
	varInits := []ginit{}
	for _, file := range sp.files {
		for _, decl := range file.Decls {
			d, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch d.Tok {
			case token.VAR:
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						return scopeErrorf("synthetic %s: package-level var spec %T is not supported", sp.path, spec)
					}
					if len(vs.Values) != 0 && len(vs.Values) != len(vs.Names) {
						return scopeErrorf("synthetic %s: var spec with %d names and %d values is not supported",
							sp.path, len(vs.Names), len(vs.Values))
					}
					for i, n := range vs.Names {
						if n.Name == "_" {
							continue
						}
						v, ok := e.info.Defs[n].(*types.Var)
						if !ok {
							return scopeErrorf("synthetic %s: package var %q has no *types.Var object", sp.path, n.Name)
						}
						oid, err := e.defVar(v, false)
						if err != nil {
							return err
						}
						var init *Node
						var initExpr ast.Expr
						if len(vs.Values) > 0 {
							initExpr = vs.Values[i]
							x, err := e.exportExpr(initExpr)
							if err != nil {
								return err
							}
							init = x
						}
						varInits = append(varInits, ginit{oid: oid, init: init, expr: initExpr, pos: e.pos(n)})
					}
				}
			case token.CONST, token.TYPE, token.IMPORT:
				// Consts fold via go/types; named types register
				// lazily; imports carry no code.
			default:
				return scopeErrorf("synthetic %s: package-level %v is not supported", sp.path, d.Tok)
			}
		}
	}
	depFuncs := make(map[*types.Func]*ast.FuncDecl, len(g.fis))
	for _, fi := range g.fis {
		depFuncs[fi.obj] = fi.fd
	}
	ordered, err := orderVarInits(varInits, e.info, e.objIDs, func(oid int) string {
		return e.objects[oid].Name
	}, depFuncs)
	if err != nil {
		return err
	}
	for _, g := range ordered {
		e.synthGlobals = append(e.synthGlobals, GlobalEnt{Obj: g.oid, Init: g.init, Pos: g.pos})
	}
	e.synthGroups = append(e.synthGroups, g)
	return nil
}

// exportSyntheticBodies emits the imported synthetic funcdecls into
// doc.Funcs (after the user's). State is swapped per package.
func (e *exporter) exportSyntheticBodies(doc *Doc) error {
	for _, g := range e.synthGroups {
		savedFset, savedSrc, savedInfo, savedPkg := e.fset, e.src, e.info, e.pkg
		savedSubst, savedSubstTypeIDs := e.subst, e.substTypeIDs
		e.fset, e.src, e.info, e.pkg = g.sp.fset, g.sp.raw, g.sp.info, g.sp.pkg
		e.subst, e.substTypeIDs = nil, nil
		for _, fi := range g.fis {
			f, err := e.exportFuncDecl(fi)
			if err != nil {
				e.fset, e.src, e.info, e.pkg = savedFset, savedSrc, savedInfo, savedPkg
				e.subst, e.substTypeIDs = savedSubst, savedSubstTypeIDs
				return err
			}
			doc.Funcs = append(doc.Funcs, f)
		}
		e.fset, e.src, e.info, e.pkg = savedFset, savedSrc, savedInfo, savedPkg
		e.subst, e.substTypeIDs = savedSubst, savedSubstTypeIDs
	}
	return nil
}

// exportTimeIntrinsic lowers a call to a synthetic time intrinsic
// (ADR-0005 D1) into its §D3 wire node.
func (e *exporter) exportTimeIntrinsic(c *ast.CallExpr, name string) (*Node, error) {
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	duration := func() (*Node, error) {
		if len(args) != 1 {
			return nil, scopeErrorf("time.%s takes 1 argument", name)
		}
		return args[0], nil
	}
	switch name {
	case "Now":
		if len(args) != 0 {
			return nil, scopeErrorf("time.Now takes no arguments")
		}
		return e.node("timenow", c, map[string]any{"type": typ}), nil
	case "Sleep":
		d, err := duration()
		if err != nil {
			return nil, err
		}
		return e.node("timesleep", c, map[string]any{"duration": d}), nil
	case "After":
		d, err := duration()
		if err != nil {
			return nil, err
		}
		return e.node("timeafter", c, map[string]any{"duration": d, "type": typ}), nil
	case "Tick":
		d, err := duration()
		if err != nil {
			return nil, err
		}
		return e.node("timetick", c, map[string]any{"duration": d, "type": typ}), nil
	case "NewTimer":
		d, err := duration()
		if err != nil {
			return nil, err
		}
		return e.node("timenew", c, map[string]any{"kind": "timer", "duration": d, "type": typ}), nil
	case "NewTicker":
		d, err := duration()
		if err != nil {
			return nil, err
		}
		return e.node("timenew", c, map[string]any{"kind": "ticker", "duration": d, "type": typ}), nil
	case "AfterFunc":
		if len(args) != 2 {
			return nil, scopeErrorf("time.AfterFunc takes 2 arguments")
		}
		return e.node("timeafterfunc", c, map[string]any{
			"duration": args[0], "func": args[1], "type": typ,
		}), nil
	}
	return nil, scopeErrorf("time.%s is not a recognized intrinsic", name)
}

// exportMathIntrinsic lowers a call to a synthetic math intrinsic
// (P3-M7, ADR-0007 D5) into its wire node. Each intrinsic gets its own
// wire kind (the time-intrinsic precedent): "mathf64bits",
// "mathf64frombits", "mathf32bits", "mathf32frombits", "mathsqrt",
// "mathfma". Fields: the operand expression(s) ("x" / "x","y","z") and
// "type" (result type-table id, never guessed).
func (e *exporter) exportMathIntrinsic(c *ast.CallExpr, name string) (*Node, error) {
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	kind, ok := map[string]string{
		"Float64bits":     "mathf64bits",
		"Float64frombits": "mathf64frombits",
		"Float32bits":     "mathf32bits",
		"Float32frombits": "mathf32frombits",
		"Sqrt":            "mathsqrt",
		"FMA":             "mathfma",
	}[name]
	if !ok {
		return nil, scopeErrorf("math.%s is not a recognized intrinsic", name)
	}
	want := 1
	if name == "FMA" {
		want = 3
	}
	if len(args) != want {
		return nil, scopeErrorf("math.%s takes %d arguments", name, want)
	}
	fields := map[string]any{"x": args[0], "type": typ}
	if name == "FMA" {
		fields["y"] = args[1]
		fields["z"] = args[2]
	}
	return e.node(kind, c, fields), nil
}

// exportTimeOp lowers (*time.Timer).Stop/Reset and (*time.Ticker).Stop/
// Reset method calls (ADR-0005 D1) into the "timeop" wire node.
func (e *exporter) exportTimeOp(c *ast.CallExpr, sel *ast.SelectorExpr, nt *types.Named) (*Node, error) {
	op := sel.Sel.Name
	if op != "Stop" && op != "Reset" {
		return nil, scopeErrorf("time.%s.%s is not in M5 scope", nt.Obj().Name(), op)
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"op": op, "recv": recv}
	if op == "Reset" {
		if len(c.Args) != 1 {
			return nil, scopeErrorf("time.%s.Reset takes 1 argument", nt.Obj().Name())
		}
		d, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		fields["duration"] = d
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	// P3-M5: Ticker.Stop() is void — omit the type field (Java side makes it
	// optional). Timer.Stop() returns bool.
	if typ != nil {
		fields["type"] = typ
	}
	return e.node("timeop", c, fields), nil
}

// exportSyntheticPkgCall handles calls of the form time.X(...) /
// context.X(...) / errors.X(...) / sort.X(...) where X denotes a
// synthetic package member. It returns handled=false for type names
// (conversions take the normal path) and for non-synthetic packages.
func (e *exporter) exportSyntheticPkgCall(c *ast.CallExpr, sel *ast.SelectorExpr, pkgName *types.PkgName) (*Node, bool, error) {
	path := pkgName.Imported().Path()
	// P3-M7 (wave 1): the 8 encoding/* primitive packages route through
	// here so package-level calls (hex.Encode, binary.Uvarint, ...) lower
	// to direct calls of the imported synthetic funcdecls via
	// exportFuncCall, and multi-result members fail fast with the named
	// M7 scope error (2-/3-target assignments are desugared earlier, in
	// exportDefine/exportAssign).
	// (m7-wenc-core: "strings" was missing here too — with the
	// importer-selection fix, strings imports type-checked but every
	// strings.X(...) call then failed "selector call ... is not
	// supported". Fixed here; multi-result strings members now fail fast
	// with the named M7 error as the corpus README claims.)
	if path != "time" && path != "context" && path != "errors" && path != "sort" && path != "bytes" && path != "math" && path != "strings" &&
		path != "encoding/base32" && path != "encoding/base64" && path != "encoding/hex" && path != "encoding/binary" &&
		path != "encoding/ascii85" && path != "encoding/pem" && path != "encoding/asn1" && path != "encoding" && path != "encoding/xml" && path != "encoding/csv" && path != "encoding/gob" && path != "strconv" && path != "encoding/json" {
		return nil, false, nil
	}
	obj := e.info.Uses[sel.Sel]
	if tn, ok := obj.(*types.TypeName); ok {
		// P3-M5 (WS E, ADR-0005): qualified conversion (time.Duration(5),
		// time.Time(t)): the unwrapSelector block above would reject it
		// as an unsupported selector call, so handle it here directly.
		n, err := e.exportTypeConversion(c, tn)
		return n, true, err
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil, false, nil
	}
	if path == "time" && timeIntrinsicNames[fn.Name()] {
		n, err := e.exportTimeIntrinsic(c, fn.Name())
		return n, true, err
	}
	// P3-M7 (WS G, ADR-0007 D5): math intrinsics (bit conversions, Sqrt,
	// FMA) lower to the "math*" wire nodes.
	if path == "math" && mathIntrinsicNames[fn.Name()] {
		n, err := e.exportMathIntrinsic(c, fn.Name())
		return n, true, err
	}
	// P3-M5 (WS E, ADR-0005): multi-result synthetic functions
	// (time.ParseDuration) fail fast (WS D; F4 precedent).
	if isMultiResultSynthetic(fn) {
		return nil, true, multiResultSyntheticError(fn)
	}
	// Real-Go package-level function: direct call via funcByObj (the
	// declaration was imported by importSyntheticPackage).
	n, err := e.exportFuncCall(c, sel.Sel, fn)
	return n, true, err
}

// exportSyntheticQualified handles qualified identifiers in expression
// position: time.Second (const), context.Canceled (package var). Method
// expressions (time.Duration.String) are NOT idents on X and fall
// through to the normal path.
func (e *exporter) exportSyntheticQualified(x *ast.SelectorExpr, pkgName *types.PkgName) (*Node, error) {
	obj := e.info.Uses[x.Sel]
	switch obj := obj.(type) {
	case *types.Const:
		return e.foldedConst(x.Sel, obj.Val())
	case *types.Var:
		oid, ok := e.objIDs[obj]
		if !ok {
			return nil, scopeErrorf("gofrontend: synthetic package var %s.%s missing from object table",
				pkgName.Name(), obj.Name())
		}
		t, err := e.typeID(obj.Type())
		if err != nil {
			return nil, err
		}
		return e.node("ident", x, map[string]any{"name": x.Sel.Name, "obj": oid, "type": t}), nil
	case *types.Func:
		if obj.Pkg() != nil && obj.Pkg().Path() == "time" && timeIntrinsicNames[obj.Name()] {
			return nil, scopeErrorf("time.%s is an intrinsic and cannot be used in value position (M5)", obj.Name())
		}
		if obj.Pkg() != nil && obj.Pkg().Path() == "math" && mathIntrinsicNames[obj.Name()] {
			return nil, scopeErrorf("math.%s is an intrinsic and cannot be used in value position (M7)", obj.Name())
		}
		return nil, scopeErrorf("%s.%s in value position is not supported in frontend/v1 scope (M5)",
			pkgName.Name(), obj.Name())
	default:
		return nil, scopeErrorf("selector %s.%s is not supported in frontend/v1 scope (M5)",
			pkgName.Name(), x.Sel.Name)
	}
}

// isTimeIntrinsicFunc reports whether fn is a synthetic time intrinsic.
func isTimeIntrinsicFunc(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "time" && timeIntrinsicNames[fn.Name()]
}

// isMathIntrinsicFunc reports whether fn is a synthetic math intrinsic.
func isMathIntrinsicFunc(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "math" && mathIntrinsicNames[fn.Name()]
}

// isMultiResultSynthetic reports whether fn is a multi-result function or
// method declared by a synthetic time/context/errors/sort package
// (Context.Deadline, time.ParseDuration, Time.Date/Clock/Zone,
// sort.Find, ...). v1 cannot emit or call multi-result functions (WS D;
// the P3-M3 F4 fail-fast precedent): the declarations exist with exact gc
// signatures for type-checking, but every call site fails fast with a
// named scope error. Re-test when WS D lands multi-result calls.
func isMultiResultSynthetic(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	p := fn.Pkg().Path()
	if p != "time" && p != "context" && p != "errors" && p != "sort" && p != "bytes" && p != "math" && p != "strings" && p != "encoding/xml" &&
		p != "encoding/base32" && p != "encoding/base64" && p != "encoding/hex" && p != "encoding/binary" &&
		p != "encoding/ascii85" && p != "encoding/pem" && p != "encoding/asn1" && p != "encoding" && p != "encoding/csv" && p != "encoding/gob" && p != "strconv" && p != "encoding/json" {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	return sig.Results().Len() > 1
}

func multiResultSyntheticError(fn *types.Func) error {
	// P3-M7: the milestone tag follows the synthetic package (time/context
	// are M5, errors/sort/bytes/math/strings/encoding-xml and the 8
	// encoding/* primitives are wave-1 M7).
	milestone := "M5"
	if p := fn.Pkg().Path(); p == "errors" || p == "sort" || p == "bytes" || p == "math" || p == "strings" || p == "encoding/xml" ||
		p == "encoding/base32" || p == "encoding/base64" || p == "encoding/hex" || p == "encoding/binary" ||
		p == "encoding/ascii85" || p == "encoding/pem" || p == "encoding/asn1" || p == "encoding" || p == "encoding/csv" || p == "encoding/gob" || p == "strconv" || p == "encoding/json" {
		milestone = "M7"
	}
	// Qualify method names with the receiver type name
	// (bytes.Buffer.Read vs bytes.Reader.Read).
	name := fn.Name()
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		rt := sig.Recv().Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok {
			name = nt.Obj().Name() + "." + name
		}
	}
	return scopeErrorf("%s.%s is not in %s scope (multi-result calls need WS D; re-test when it lands)",
		fn.Pkg().Path(), name, milestone)
}

// timerScopeErrorf is the fail-fast for zero-value Timer/Ticker
// construction (ADR-0005 D1): gc's zero Timer is uninitialized (Stop
// panics); silently manufacturing a firing timer would be a silent
// divergence, so construction must go through time.NewTimer/NewTicker.
func timerScopeErrorf(nt *types.Named, position string) error {
	return scopeErrorf("time.%s as %s is not in M5 scope (zero timers are uninitialized in gc; use time.NewTimer/time.NewTicker)",
		nt.Obj().Name(), position)
}
