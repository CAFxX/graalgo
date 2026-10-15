// Package main implements gofrontend, the graalgo Go frontend service.
//
// Architecture (PLAN.md D1): the vendored go/* packages (see VENDORING.md)
// type-check one single-file package; this file walks the typed AST and
// emits the frontend/v1 JSON document described in ../docs/frontend-v1.md.
//
// Scope of this revision (M4 control flow + composite types, WS E):
//   - exactly one input file, no imports (import overlay arrives with WS G)
//   - exactly one function, `func main`, zero params, at most one result
//   - statements: blocks, var declarations (var / :=), assignments
//     (= and op=; lhs may be an identifier, index, or selector),
//     expression statements, return, ++/-- (identifier/index/selector),
//     if/else (with init), for (3-clause, condition-only, infinite),
//     range (slice/array/string/map; no channels), switch (expression +
//     type switches), break/continue (labeled and unlabeled), goto/labels,
//     fallthrough
//   - expressions: identifiers, basic literals, binary/unary ops,
//     parentheses, builtin calls (emitted; NodeFactory v1 rejects them),
//     composite literals (incl. keyed, nested elision, &T{}),
//     index (incl. comma-ok), slice expressions (2- and 3-index),
//     selectors (incl. embedded/promoted fields), type assertions
//     (incl. comma-ok)
//
// Anything outside this scope fails fast with an explicit error naming the
// workstream that extends the scope. There are no silently dropped nodes,
// except *ast.EmptyStmt which the exporter normalizes away (documented in
// docs/frontend-v1.md).
//
// D1 falsifier watch (GG-0012): this file uses only long-stable go/* APIs
// (parser.ParseFile, types.Config.Check, Info.{Types,Defs,Uses}). If a future
// re-vendoring breaks compilation or changes behavior of these APIs, STOP and
// report: that is a spike-3 toil re-measurement trigger, not a cue to adapt
// silently.
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// frontendVersion is the JSON protocol version emitted. Additive-only changes
// keep "v1"; any breaking change bumps to "v2" (docs/frontend-v1.md §8).
const frontendVersion = "v1"

// langVersion is the Go language version assumed when type-checking.
// Per-file -lang gates arrive with WS G (GG-0013); until then the vendored
// toolchain default applies.
const langVersion = "go1.27"

// ---------------------------------------------------------------------------
// JSON model (field-for-field contract: docs/frontend-v1.md).
// ---------------------------------------------------------------------------

// Doc is the root of a frontend/v1 document.
type Doc struct {
	Frontend  string      `json:"frontend"`
	Generator Generator   `json:"generator"`
	Package   Pkg         `json:"package"`
	Types     []TypeEnt   `json:"types"`
	Objects   []ObjectEnt `json:"objects"`
	Funcs     []*Node     `json:"funcs"`
	// Globals lists package-level variables in initialization
	// (dependency) order (M7). A nil Init means the zero value;
	// Obj == -1 means "evaluate Init for side effects, discard"
	// (package-level `var _ = ...`).
	Globals []GlobalEnt `json:"globals"`
}

// GlobalEnt is one package-level variable (M7): the object-table id of
// the variable plus its initializer expression (nil when absent).
type GlobalEnt struct {
	Obj  int   `json:"obj"`
	Init *Node `json:"init"`
	Pos  *Pos  `json:"pos,omitempty"`
}

type Generator struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	GoRoot  string `json:"goroot"`
}

type Pkg struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
	Lang  string   `json:"lang"`
}

// TypeEnt is one entry of the type table. Kind selects the Detail schema;
// every go/types type reachable from the program appears exactly once and
// expressions reference entries by index.
type TypeEnt struct {
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail"`
}

// ObjectEnt is one entry of the object table: variables ("var"), top-level
// functions and methods ("func", M6/WS H), and monomorphized instantiations
// ("inst", M6/WS H). Identifiers reference entries by index; the id is
// assigned at the declaration in a single deterministic AST walk, so ids are
// stable across runs.
type ObjectEnt struct {
	Kind  string `json:"kind"` // "var", "func", "inst"
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Param bool   `json:"param"`
	// InstOf and TypeArgs are meaningful only for kind "inst": the generic
	// function/method object this instantiation was monomorphized from, and
	// the concrete type-argument ids (receiver args then method args).
	InstOf   int   `json:"instof"`
	TypeArgs []int `json:"typeargs"`
}

// Node is one frontend/v1 AST node. Kind selects the Fields schema;
// the contract per kind is docs/frontend-v1.md, enforced by
// tools/validate_frontend_v1.py.
type Node struct {
	Kind   string         `json:"kind"`
	Pos    *Pos           `json:"pos,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// (FuncDecl/Param folded into Node: every node carries kind/pos/fields;
// see the Node type above. Params are emitted as kind:"param" nodes.)

// Pos is a source position: file index into Doc.Package.Files, 1-based
// line/column (go/token convention, but COLUMN IS RUNE-BASED — see below),
// byte offset, and rune length of the node's source text.
//
// M8: col and len are rune (character) counts, not bytes. Truffle's
// Source.createSection(line, col, len) is char-based; emitting byte-based
// values crashed it ("charIndex out of range") on any non-ASCII source.
// off remains the byte offset of the node's first character (NodeFactory
// does not consume it).
type Pos struct {
	File int `json:"file"`
	Line int `json:"line"`
	Col  int `json:"col"`
	Off  int `json:"off"`
	Len  int `json:"len"`
}

// ---------------------------------------------------------------------------
// Export entry point.
// ---------------------------------------------------------------------------

// failImporter rejects every import. The Phase-1 import overlay (WS G)
// replaces this; until then an import is a scope error, reported before
// type-checking so the failure is never a confusing types.Config error.
type failImporter struct{}

func (failImporter) Import(path string) (*types.Package, error) {
	return nil, fmt.Errorf("import %q: imports are not supported in frontend/v1 scope (WS G import overlay)", path)
}

// overlayImporter is the M5 (WS F) minimal import overlay, extended by M6
// (WS H) with the D4 fmt stub, by P3-M2b (WS D, ADR-0002) with the
// synthetic sync package, by P3-M4 (WS C, ADR-0004) with the synthetic
// sync/atomic package and the empty unsafe package (for //go:linkname's
// blank import), and by P3-M5 (WS E, ADR-0005) with the synthetic time and
// context packages (real-Go sources in synth_time_context.go, resolved via
// getSyntheticPackage so synthetic->synthetic imports work), and by P3-M7
// (wave 1) with the synthetic errors and sort packages (real-Go sources
// in synth_errors.go / synth_sort.go, likewise via getSyntheticPackage):
// it resolves exactly the synthetic package
// "runtime" exposing runtime.Goexit (plus the P2-M4 GC-interop surface
// and the stack-inspection family: Callers/CallersFrames/Caller/FuncForPC
// plus Frames/Frame/Func),
// the synthetic package "fmt" exposing the seven D4 symbols (Print,
// Printf, Println, Sprint, Sprintf, Sprintln, Errorf) with exact variadic
// signatures, the synthetic package "sync" exposing the four P3-M2b
// primitives (Mutex, RWMutex, WaitGroup, Once) with exact gc method
// signatures, the synthetic package "sync/atomic" exposing the P3-M4
// typed wrappers (Int64, Uint64, Int32, Uint32, Bool, Uintptr) plus the
// P3-M10 generic additions (Pointer[T], Value) with exact
// gc method signatures, the synthetic package "time" (ADR-0005 D1:
// Duration/Time/Month/Weekday as real Go, clock/timer intrinsics), the
// synthetic package "context" (ADR-0005 D2: real Go on sync.Mutex,
// channels, synthetic time, and P3-M3 interface dispatch), the synthetic
// package "errors" (P3-M7: New/Is/Unwrap/Join/AsType as real Go; As
// fail-fast — it needs reflect), the synthetic package "sort" (P3-M7:
// pdqsort/stable/search as real Go; Find/Slice/SliceStable/SliceIsSorted
// fail-fast — multi-result/reflect), the 8 P3-M7 (wave 1) encoding/*
// primitive packages (encoding/base32, encoding/base64, encoding/hex,
// encoding/binary, encoding/ascii85, encoding/pem, encoding/asn1,
// encoding) as real-Go sources via getSyntheticPackage (encoding/pem
// imports synthetic encoding/base64), and the empty synthetic package
// "unsafe".
// Every other import still fails via failImporter. WS G's full
// import overlay absorbs this carve-out.
// syntheticRuntimePackage builds the synthetic "runtime" package for the
// import overlay. The runtime package is inherently implementation-defined
// (gc's own runtime/ is machine-specific and cannot compile on graalgo),
// so the overlay declares its API surface with exact gc 1.27.1 signatures
// (verified against ~/tools/go-1.27.1/src/runtime/*.go) and the backend
// implements each member as an intrinsic wire node (see exportCall and
// graalgo.rtstack):
//   - Goexit (M5, WS F), the build constants GOARCH/GOOS and NumCPU
//     (P4 driver-harden), GOMAXPROCS (P4 driver-harden),
//   - the P2-M4 GC-interop surface (SetFinalizer/KeepAlive/GC, ADR-0002 D4),
//   - the stack-inspection family: Callers, CallersFrames, Caller,
//     FuncForPC, and the Frames/Frame/Func types with their methods.
//
// The multi-result members (Caller, (*Frames).Next, (*Func).FileLine) keep
// their exact multi-result signatures for type-checking; call sites
// desugar through the mrRuntimeStack plan (mrdesugar_m5.go) to the
// single-result intrinsic wire nodes returning pair structs
// (callerPair/framesNextPair/funcFileLinePair), exactly like the mrM7
// Appendix M desugar — except there is no real-Go __-helper (the overlay
// has no source): the wire node itself is the implementation.
func syntheticRuntimePackage() *types.Package {
	pkg := types.NewPackage("runtime", "runtime")
	strTyp := types.Typ[types.String]
	intTyp := types.Typ[types.Int]
	uintptrTyp := types.Typ[types.Uintptr]
	boolTyp := types.Typ[types.Bool]
	uint64Typ := types.Typ[types.Uint64]
	uint32Typ := types.Typ[types.Uint32]
	float64Typ := types.Typ[types.Float64]
	byteSlice := types.NewSlice(types.Typ[types.Byte])

	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(vs ...*types.Var) *types.Tuple {
		return types.NewTuple(vs...)
	}
	addFunc := func(name string, params, results *types.Tuple) *types.Func {
		fn := types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(nil, nil, nil, params, results, false))
		pkg.Scope().Insert(fn)
		return fn
	}
	newNamed := func(name string, underlying types.Type) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		return types.NewNamed(tname, underlying, nil)
	}
	newField := func(name string, t types.Type) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, false)
	}
	addMethod := func(named *types.Named, name string, params, results *types.Tuple) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(recv, nil, nil, params, results, false)))
	}

	// M5 (WS F): Goexit — the Phase-1 Goexit trigger.
	addFunc("Goexit", nil, nil)
	// P4 (driver-harden): the build constants and NumCPU. Real modules
	// (e.g. uuid's tests) read runtime.GOARCH/GOOS and call
	// runtime.NumCPU. Values match the host (linux/amd64); NumCPU
	// returns the host count via a wire node (see below).
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "GOARCH", strTyp,
		constant.MakeString("amd64")))
	pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, "GOOS", strTyp,
		constant.MakeString("linux")))
	addFunc("NumCPU", nil, tuple(param("", intTyp)))
	// P4 (driver-harden): GOMAXPROCS. Sets and returns the GOMAXPROCS
	// value; the backend tracks it as a no-op cell (no real scheduler
	// tuning on the JVM) but the call must type-check and round-trip.
	addFunc("GOMAXPROCS", tuple(param("n", intTyp)), tuple(param("", intTyp)))
	// P2-M4 (GC interop, ADR-0002 D4): the Go-observable GC surface.
	// Signatures match gc's runtime package exactly; the Java side is
	// graalgo.runtime.GcInterop (Phase-1 stub, Phase-2 real).
	anyT := types.NewInterfaceType(nil, nil).Complete()
	addFunc("SetFinalizer", tuple(param("obj", anyT), param("finalizer", anyT)), nil)
	addFunc("KeepAlive", tuple(param("x", anyT)), nil)
	addFunc("GC", nil, nil)

	// Stack-inspection family (unblocks internal/bisect via expvar <->
	// godebug, hence the Caddy import chain). gc 1.27.1 signatures,
	// verified against src/runtime/traceback.go and src/runtime/symtab.go.
	//
	// PCs are opaque: the backend interns each (function, file, line)
	// site to a dense id (graalgo.runtime.StackTrace). Interned ids are
	// only meaningful to CallersFrames/FuncForPC — exactly gc's contract
	// ("Iterating over the returned slice of PCs directly is
	// discouraged").
	uintptrSlice := types.NewSlice(uintptrTyp)
	addFunc("Callers", tuple(param("skip", intTyp), param("pc", uintptrSlice)),
		tuple(param("", intTyp)))
	// type Func: opaque (gc: unexported fields).
	fn := newNamed("Func", types.NewStruct(nil, nil))
	// type Frame: the gc struct shape, with exported fields so the
	// (*Frames).Next desugar can extract them from the pair.
	frame := newNamed("Frame", types.NewStruct([]*types.Var{
		newField("PC", uintptrTyp),
		newField("Func", types.NewPointer(fn)),
		newField("Function", strTyp),
		newField("File", strTyp),
		newField("Line", intTyp),
		newField("Entry", uintptrTyp),
	}, nil))
	// type Frames: opaque (gc: unexported fields).
	frames := newNamed("Frames", types.NewStruct(nil, nil))
	addFunc("CallersFrames", tuple(param("pcs", uintptrSlice)),
		tuple(param("", types.NewPointer(frames))))
	addFunc("Caller", tuple(param("skip", intTyp)), tuple(
		param("pc", uintptrTyp),
		param("file", strTyp),
		param("line", intTyp),
		param("ok", boolTyp)))
	addFunc("FuncForPC", tuple(param("pc", uintptrTyp)),
		tuple(param("", types.NewPointer(fn))))
	addMethod(frames, "Next", nil, tuple(param("", frame), param("", boolTyp)))
	addMethod(fn, "Name", nil, tuple(param("", strTyp)))
	addMethod(fn, "Entry", nil, tuple(param("", uintptrTyp)))
	addMethod(fn, "FileLine", tuple(param("pc", uintptrTyp)), tuple(
		param("file", strTyp),
		param("line", intTyp)))
	// Pair structs for the mrRuntimeStack desugar (unexported: user code
	// can never name them — go/types rejects unexported cross-package
	// references — so there is no collision).
	newNamed("callerPair", types.NewStruct([]*types.Var{
		newField("pc", uintptrTyp),
		newField("file", strTyp),
		newField("line", intTyp),
		newField("ok", boolTyp),
	}, nil))
	newNamed("framesNextPair", types.NewStruct([]*types.Var{
		newField("frame", frame),
		newField("more", boolTyp),
	}, nil))
	newNamed("funcFileLinePair", types.NewStruct([]*types.Var{
		newField("file", strTyp),
		newField("line", intTyp),
	}, nil))

	// MemStats (gc 1.27.1, src/runtime/mstats.go): the memory-statistics
	// struct. Field order and types match gc exactly. The backend
	// implements ReadMemStats as an intrinsic wire node populating the
	// struct from JVM/GC telemetry (see exportCall).
	bySizeElem := types.NewStruct([]*types.Var{
		newField("Size", uint32Typ),
		newField("Mallocs", uint64Typ),
		newField("Frees", uint64Typ),
	}, nil)
	memStats := newNamed("MemStats", types.NewStruct([]*types.Var{
		newField("Alloc", uint64Typ),
		newField("TotalAlloc", uint64Typ),
		newField("Sys", uint64Typ),
		newField("Lookups", uint64Typ),
		newField("Mallocs", uint64Typ),
		newField("Frees", uint64Typ),
		newField("HeapAlloc", uint64Typ),
		newField("HeapSys", uint64Typ),
		newField("HeapIdle", uint64Typ),
		newField("HeapInuse", uint64Typ),
		newField("HeapReleased", uint64Typ),
		newField("HeapObjects", uint64Typ),
		newField("StackInuse", uint64Typ),
		newField("StackSys", uint64Typ),
		newField("MSpanInuse", uint64Typ),
		newField("MSpanSys", uint64Typ),
		newField("MCacheInuse", uint64Typ),
		newField("MCacheSys", uint64Typ),
		newField("BuckHashSys", uint64Typ),
		newField("GCSys", uint64Typ),
		newField("OtherSys", uint64Typ),
		newField("NextGC", uint64Typ),
		newField("LastGC", uint64Typ),
		newField("PauseTotalNs", uint64Typ),
		newField("PauseNs", types.NewArray(uint64Typ, 256)),
		newField("PauseEnd", types.NewArray(uint64Typ, 256)),
		newField("NumGC", uint32Typ),
		newField("NumForcedGC", uint32Typ),
		newField("GCCPUFraction", float64Typ),
		newField("EnableGC", boolTyp),
		newField("DebugGC", boolTyp),
		newField("BySize", types.NewArray(bySizeElem, 61)),
	}, nil))
	// ReadMemStats (gc 1.27.1, src/runtime/mstats.go:356): populates m
	// with current memory statistics.
	addFunc("ReadMemStats", tuple(param("m", types.NewPointer(memStats))), nil)
	// GOROOT (gc 1.27.1, src/runtime/extern.go:354): returns the GOROOT
	// environment value.
	addFunc("GOROOT", nil, tuple(param("", strTyp)))
	// Version (gc 1.27.1, src/runtime/extern.go:375): returns the Go
	// version string.
	addFunc("Version", nil, tuple(param("", strTyp)))
	// Stack (gc 1.27.1, src/runtime/mprof.go:1690): formats a stack
	// trace into buf, returns bytes written.
	addFunc("Stack", tuple(param("buf", byteSlice), param("all", boolTyp)),
		tuple(param("", intTyp)))

	pkg.MarkComplete()
	return pkg
}

type overlayImporter struct{}

func (overlayImporter) Import(path string) (*types.Package, error) {
	// The 5 packages with dedicated synthetic constructors (not in
	// syntheticSources).
	switch path {
	case "runtime":
		return syntheticRuntimePackage(), nil
	case "fmt":
		return syntheticFmtPackage(), nil
	case "sync":
		return syntheticSyncPackage(), nil
	case "sync/atomic":
		return syntheticAtomicPackage(), nil
	case "unsafe":
		return syntheticUnsafePackage(), nil
	}
	// General (hunt-special-cases): any package with a synthetic source
	// resolves via getSyntheticPackage — no per-package switch needed.
	// This replaces the 300+ line per-package switch; new synthetic
	// packages work automatically when added to syntheticSources.
	if _, ok := syntheticSources[path]; ok {
		return getSyntheticPackage(path).pkg, nil
	}
	// WS G (P3-M9, module-probe lane): third-party imports resolve from
	// local disk (module cache/GOPATH) through the general overlay. A path
	// that resolves nowhere keeps the pre-existing WS G scope error;
	// a path that resolves but fails to build reports the real failure.
	if dp, err := getDiskPackage(path); err == nil {
		return dp.pkg, nil
	} else if !errors.Is(err, errDiskNotFound) {
		return nil, fmt.Errorf("gofrontend: import %q: %v", path, err)
	}
	return failImporter{}.Import(path)
}

// syntheticFmtPackage builds the D4 fmt stub package for the Phase-1 import
// overlay (PLAN.md D4): exactly the seven pure functions with exact
// variadic signatures. Calls to any other fmt.X are a scope error naming
// Phase 3.
func syntheticFmtPackage() *types.Package {
	pkg := types.NewPackage("fmt", "fmt")
	anyT := types.NewInterfaceType(nil, nil).Complete()
	anySlice := types.NewSlice(anyT)
	errorT := types.Universe.Lookup("error").Type()
	intT := types.Typ[types.Int]
	stringT := types.Typ[types.String]
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	insert := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, true)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	a := param("a", anySlice)
	format := param("format", stringT)
	n := param("n", intT)
	errRes := param("err", errorT)
	strRes := param("", stringT)
	errResOnly := param("", errorT)
	insert("Print", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Printf", types.NewTuple(format, a), types.NewTuple(n, errRes))
	insert("Println", types.NewTuple(a), types.NewTuple(n, errRes))
	insert("Sprint", types.NewTuple(a), types.NewTuple(strRes))
	insert("Sprintf", types.NewTuple(format, a), types.NewTuple(strRes))
	insert("Sprintln", types.NewTuple(a), types.NewTuple(strRes))
	insert("Errorf", types.NewTuple(format, a), types.NewTuple(errResOnly))
	// Gate-1 #4: the F-family (Fprint/Fprintf/Fprintln, gc 1.27.1 exact
	// signatures). The backend's FmtCallNode executes them (writes to the
	// io.Writer first arg via GoMethodDispatch); the exporter treats them
	// like the Print variants (statement position, results discarded).
	// The w parameter uses the synthetic io package's Writer type (io
	// does not import fmt — no init cycle).
	ioWriter := getSyntheticPackage("io").pkg.Scope().Lookup("Writer").Type()
	w := param("w", ioWriter)
	insert("Fprint", types.NewTuple(w, a), types.NewTuple(n, errRes))
	insert("Fprintf", types.NewTuple(w, format, a), types.NewTuple(n, errRes))
	insert("Fprintln", types.NewTuple(w, a), types.NewTuple(n, errRes))
	// P3-M9 (wave 3): the State/Scanner/Formatter interfaces (gc 1.27.1
	// exact signatures). math/big's Int/Float Format/Scan methods are
	// declared against them. Types only — the stub keeps just the seven
	// pure functions above.
	boolT := types.Typ[types.Bool]
	runeT := types.Typ[types.Rune]
	ifaceMethod := func(name string, params, results *types.Tuple) *types.Func {
		return types.NewFunc(token.NoPos, pkg, name,
			types.NewSignatureType(nil, nil, nil, params, results, false))
	}
	stateNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "State", nil), nil, nil)
	// gc 1.27.1: State embeds io.Writer (Write method) alongside
	// Width/Precision/Flag.
	byteSliceT := types.NewSlice(types.Typ[types.Byte])
	stateNamed.SetUnderlying(types.NewInterfaceType([]*types.Func{
		ifaceMethod("Write", types.NewTuple(param("b", byteSliceT)),
			types.NewTuple(param("n", intT), param("err", errorT))),
		ifaceMethod("Width", types.NewTuple(),
			types.NewTuple(param("wid", intT), param("ok", boolT))),
		ifaceMethod("Precision", types.NewTuple(),
			types.NewTuple(param("prec", intT), param("ok", boolT))),
		ifaceMethod("Flag", types.NewTuple(param("c", intT)),
			types.NewTuple(param("", boolT))),
	}, nil).Complete())
	pkg.Scope().Insert(stateNamed.Obj())
	// NOTE: declared after ScanState (below) would be cleaner, but the
	// Named is created first and the underlying is set after ScanState
	// exists; see the deferred SetUnderlying below.
	scannerNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Scanner", nil), nil, nil)
	pkg.Scope().Insert(scannerNamed.Obj())
	formatterNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Formatter", nil), nil, nil)
	formatterNamed.SetUnderlying(types.NewInterfaceType([]*types.Func{
		ifaceMethod("Format",
			types.NewTuple(param("f", stateNamed), param("verb", runeT)),
			types.NewTuple()),
	}, nil).Complete())
	pkg.Scope().Insert(formatterNamed.Obj())
	// BUG-MODPROBE-010 verification: fmt.Stringer was missing from the
	// synthetic fmt package (gc 1.27.1: type Stringer interface{ String() string }).
	// The arrdispatch interface-boxing battery references it as a type.
	stringerNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Stringer", nil), nil, nil)
	stringerNamed.SetUnderlying(types.NewInterfaceType([]*types.Func{
		ifaceMethod("String", types.NewTuple(), types.NewTuple(strRes)),
	}, nil).Complete())
	pkg.Scope().Insert(stringerNamed.Obj())
	// P3-M9 (wave 3): fmt.ScanState (gc 1.27.1 exact signature). math/big's
	// Int/Rat/Float Scan methods and intconv's byteReader embed it.
	scanStateNamed := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "ScanState", nil), nil, nil)
	runePred := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(param("r", runeT)),
		types.NewTuple(param("", boolT)), false)
	scanStateNamed.SetUnderlying(types.NewInterfaceType([]*types.Func{
		ifaceMethod("ReadRune", types.NewTuple(),
			types.NewTuple(param("r", runeT), param("size", intT), param("err", errorT))),
		ifaceMethod("UnreadRune", types.NewTuple(),
			types.NewTuple(param("", errorT))),
		ifaceMethod("SkipSpace", types.NewTuple(), types.NewTuple()),
		ifaceMethod("Token",
			types.NewTuple(param("skipSpace", boolT), param("f", runePred)),
			types.NewTuple(param("token", byteSliceT), param("err", errorT))),
		ifaceMethod("Width", types.NewTuple(),
			types.NewTuple(param("wid", intT), param("ok", boolT))),
		ifaceMethod("Read", types.NewTuple(param("buf", byteSliceT)),
			types.NewTuple(param("n", intT), param("err", errorT))),
	}, nil).Complete())
	pkg.Scope().Insert(scanStateNamed.Obj())
	// Scanner's method takes a ScanState (not State) — set now that
	// scanStateNamed exists.
	scannerNamed.SetUnderlying(types.NewInterfaceType([]*types.Func{
		ifaceMethod("Scan",
			types.NewTuple(param("state", scanStateNamed), param("verb", runeT)),
			types.NewTuple(param("", errorT))),
	}, nil).Complete())
	pkg.MarkComplete()
	return pkg
}

// fmtOverlayFuncs are the seven D4 symbols (PLAN.md D4) plus the F-family
// (Gate-1 #4: Fprint/Fprintf/Fprintln, executed by the backend's FmtCallNode).
var fmtOverlayFuncs = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Sprint": true, "Sprintf": true, "Sprintln": true, "Errorf": true,
	"Fprint": true, "Fprintf": true, "Fprintln": true,
}

// syncPrimitiveNames are the four P3-M2b (WS D, ADR-0002) primitives bound
// to the Phase-2 Java runtime (graalgo.runtime.sync), plus the P3-M11
// sync.Pool binding (graalgo.runtime.sync.GoPool).
var syncPrimitiveNames = map[string]bool{
	"Mutex": true, "RWMutex": true, "WaitGroup": true, "Once": true,
	"Pool": true,
	// fix-linkname (Caddy/godebug lane): sync.Map. The four multi-result
	// methods (Load, LoadOrStore, LoadAndDelete, Swap) desugar via
	// mrSyncMap (pair-returning syncop); the rest lower to syncop
	// directly.
	"Map": true,
}

// syncOpsInScope is the M2-supported method set per primitive (ADR-0002
// D6). Defined-but-out-of-scope methods (TryLock/TryRLock/RLocker,
// WaitGroup.Go) fail fast in exportSyncOp with a named scope error.
// P3-M11: Pool's field-level ops (GetNew/SetNew/CallNew for the New func
// field) are exporter-synthesized, not Go method calls, so they bypass
// exportSyncOp's scope check and need no entry here.
var syncOpsInScope = map[string]map[string]bool{
	"Mutex":     {"Lock": true, "Unlock": true},
	"RWMutex":   {"Lock": true, "Unlock": true, "RLock": true, "RUnlock": true},
	"WaitGroup": {"Add": true, "Done": true, "Wait": true},
	"Once":      {"Do": true},
	// P3-M9 (driver-harden, go-cmp lane): Pool.Get/Put. Pool.New is not
	// in scope (see syncPrimitiveNames).
	"Pool": {"Get": true, "Put": true},
	// fix-linkname (Caddy/godebug lane): sync.Map's full gc method set.
	// The four multi-result methods (Load, LoadOrStore, LoadAndDelete,
	// Swap) are desugared by the mrSyncMap plan, not executed here —
	// they are listed so the scope check passes and the classifier arm
	// (not this switch) handles them.
	"Map": {"Load": true, "Store": true, "LoadOrStore": true, "LoadAndDelete": true,
		"Delete": true, "Swap": true, "CompareAndSwap": true, "CompareAndDelete": true,
		"Range": true, "Clear": true},
}

// isSyncPoolNamed reports whether nt is the synthetic sync.Pool named
// type (the one primitive allowed in array-element position).
// (P3-M9 driver-harden, go-cmp lane; kept by the P3-M11 Pool upgrade.)
func isSyncPoolNamed(nt *types.Named) bool {
	obj := nt.Obj()
	return obj != nil && obj.Name() == "Pool" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "sync"
}

// syncPrimitiveNamed reports whether t is exactly one of the synthetic
// sync primitive named types (no pointer dereference).
func syncPrimitiveNamed(t types.Type) *types.Named {
	nt, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	obj := nt.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Name() != "sync" {
		return nil
	}
	if !syncPrimitiveNames[obj.Name()] {
		return nil
	}
	return nt
}

// syncPrimitiveRecv is syncPrimitiveNamed after one pointer dereference,
// for method-call receivers (all real sync methods have pointer receivers;
// the Go value IS the Java object, so &/* is a no-op at the value level).
func syncPrimitiveRecv(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return syncPrimitiveNamed(t)
}

// syncPoolNamed reports whether t is the synthetic sync.Pool named type,
// after one pointer dereference (Pool operations have pointer-receiver /
// pointer-identity semantics; the Go value IS the Java object, so &/* is
// a no-op at the value level — the ADR-0002 D1 value model).
func syncPoolNamed(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	nt := syncPrimitiveNamed(t)
	if nt == nil || nt.Obj().Name() != "Pool" {
		return nil
	}
	return nt
}

// poolNewSelector reports whether x selects the New field of the synthetic
// sync.Pool (a types.FieldVal selection), returning the Pool named type.
// Covers p.New and (&p).New / pp.New for *sync.Pool.
func (e *exporter) poolNewSelector(x ast.Expr) *types.Named {
	lx, ok := unparen(x).(*ast.SelectorExpr)
	if !ok || lx.Sel.Name != "New" {
		return nil
	}
	sel, ok := e.info.Selections[lx]
	if !ok || sel.Kind() != types.FieldVal {
		return nil
	}
	return syncPoolNamed(sel.Recv())
}

// syncFieldOp emits a field-level "syncop" wire node for the synthetic
// Pool's New func field (P3-M11): GetNew (read), SetNew (write, emitted as
// an expression statement by exportAssign), CallNew (p.New() invocation).
// resultType is the type-table id of the op's result, or nil for the void
// SetNew — mirroring exportSyncOpNodes' "type" shape (null for void ops).
func (e *exporter) syncFieldOp(pos ast.Node, op string, nt *types.Named, recv *Node, args []*Node, resultType any) *Node {
	return e.node("syncop", pos, map[string]any{
		"op": op, "synctyp": syntheticOpTypeName(nt),
		"recv": recv, "args": args, "type": resultType,
	})
}

// syntheticSyncPackage builds the P3-M2b (WS D, ADR-0002) synthetic "sync"
// package for the import overlay: the Phase-2-bound primitives with
// exact gc 1.27.1 method signatures (pointer receivers, like the real
// package; verified against ~/tools/go-1.27.1/src/sync/*.go). Out-of-scope
// methods (TryLock/TryRLock/RLocker, WaitGroup.Go, Pool.New) are defined
// with exact signatures so misuse fails fast with a named "not in M2 scope"
// error instead of a confusing "undefined" type error — except Pool.New,
// which cannot even be defined (fields are not storable on the opaque
// Java object), so it fails to type-check.
func syntheticSyncPackage() *types.Package {
	pkg := types.NewPackage("sync", "sync")
	intT := types.Typ[types.Int]
	boolT := types.Typ[types.Bool]
	funcT := types.NewSignatureType(nil, nil, nil, nil, nil, false) // func()
	emptyStruct := types.NewStruct(nil, nil)
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(v *types.Var) *types.Tuple {
		return types.NewTuple(v)
	}

	// Locker interface (sync.Locker): result type of RWMutex.RLocker only.
	// Never callable in M2 (RLocker is out of scope); the interface just
	// needs to exist for the signature to type-check.
	locker := types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, pkg, "Lock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		types.NewFunc(token.NoPos, pkg, "Unlock",
			types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, nil).Complete()

	newPrimitive := func(name string) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		return types.NewNamed(tname, emptyStruct, nil)
	}
	addMethod := func(named *types.Named, name string, params, results *types.Tuple) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		sig := types.NewSignatureType(recv, nil, nil, params, results, false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}

	mutex := newPrimitive("Mutex")
	addMethod(mutex, "Lock", nil, nil)
	addMethod(mutex, "Unlock", nil, nil)
	addMethod(mutex, "TryLock", nil, tuple(param("", boolT)))

	rwmutex := newPrimitive("RWMutex")
	addMethod(rwmutex, "Lock", nil, nil)
	addMethod(rwmutex, "Unlock", nil, nil)
	addMethod(rwmutex, "RLock", nil, nil)
	addMethod(rwmutex, "RUnlock", nil, nil)
	addMethod(rwmutex, "TryLock", nil, tuple(param("", boolT)))
	addMethod(rwmutex, "TryRLock", nil, tuple(param("", boolT)))
	addMethod(rwmutex, "RLocker", nil, tuple(param("", locker)))

	wg := newPrimitive("WaitGroup")
	addMethod(wg, "Add", tuple(param("delta", intT)), nil)
	addMethod(wg, "Done", nil, nil)
	addMethod(wg, "Wait", nil, nil)
	addMethod(wg, "Go", tuple(param("f", funcT)), nil)

	once := newPrimitive("Once")
	addMethod(once, "Do", tuple(param("f", funcT)), nil)

	// P3-M11: sync.Pool (gc 1.27.1 shape, verified against
	// ~/tools/go/src/sync/pool.go). The only exported field is
	// New func() any; noCopy/local/localSize/victim/victimSize are
	// unexported, declared so invalid literals fail exactly like gc
	// (unkeyed literals and non-New keys are compile errors — the
	// unexported fields can never be named from another package).
	// The Go value is the opaque GoPool Java object (ADR-0002 D1);
	// the unexported fields never materialize at runtime. Methods
	// Put(x any)/Get() any have exact gc signatures (pointer
	// receivers, like the real package).
	anyT := types.NewInterfaceType(nil, nil).Complete()
	newSig := types.NewSignatureType(nil, nil, nil, nil, tuple(param("", anyT)), false) // func() any
	noCopyName := types.NewTypeName(token.NoPos, pkg, "noCopy", nil)
	pkg.Scope().Insert(noCopyName)
	noCopyT := types.NewNamed(noCopyName, emptyStruct, nil)
	poolName := types.NewTypeName(token.NoPos, pkg, "Pool", nil)
	pkg.Scope().Insert(poolName)
	pool := types.NewNamed(poolName, types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "noCopy", noCopyT, false),
		types.NewField(token.NoPos, pkg, "local", types.Typ[types.UnsafePointer], false),
		types.NewField(token.NoPos, pkg, "localSize", types.Typ[types.Uintptr], false),
		types.NewField(token.NoPos, pkg, "victim", types.Typ[types.UnsafePointer], false),
		types.NewField(token.NoPos, pkg, "victimSize", types.Typ[types.Uintptr], false),
		types.NewField(token.NoPos, pkg, "New", newSig, false),
	}, nil), nil)
	addMethod(pool, "Put", tuple(param("x", anyT)), nil)
	addMethod(pool, "Get", nil, tuple(param("", anyT)))

	// P3-M9 (Go 1.21+): OnceFunc returns a function that invokes f only once.
	// Signature: func OnceFunc(f func()) func(). The runtime behavior is a
	// trivial once-wrapper: the returned closure captures a Once and calls
	// Do(f) on each invocation.
	onceFuncSig := types.NewSignatureType(nil, nil, nil,
		tuple(param("f", funcT)), tuple(param("", funcT)), false)
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "OnceFunc", onceFuncSig))

	// fix-linkname (Caddy/godebug lane): sync.Map. gc 1.27.1 exact method
	// signatures (all pointer receivers; verified against
	// ~/tools/go-1.27.1/src/sync/map.go). The Go value IS the Java
	// GoSyncMap object (opaque, ADR-0002 D1), like the other primitives.
	// The four multi-result methods (Load, LoadOrStore, LoadAndDelete,
	// Swap) desugar through the mrSyncMap plan (mrdesugar_m5.go): the
	// desugar builds the "syncop" wire node directly with the pair type
	// below, exactly like the mrRuntimeStack precedent — there is no
	// __-helper because the types-built overlay has no real-Go source.
	// Single-result and void methods lower to "syncop" unchanged.
	m := newPrimitive("Map")
	anyBoolPair := types.NewStruct([]*types.Var{
		types.NewField(token.NoPos, pkg, "v", anyT, false),
		types.NewField(token.NoPos, pkg, "ok", boolT, false),
	}, nil)
	mapPairName := types.NewTypeName(token.NoPos, pkg, "mapAnyBoolPair", nil)
	pkg.Scope().Insert(mapPairName)
	types.NewNamed(mapPairName, anyBoolPair, nil)
	// The mrSyncMap classifier (mrdesugar_m5.go) finds the pair type via
	// scope lookup, like classifyRuntimeStackMethod's named().
	addMethod(m, "Load", tuple(param("key", anyT)),
		types.NewTuple(param("value", anyT), param("ok", boolT)))
	addMethod(m, "Store", types.NewTuple(param("key", anyT), param("value", anyT)), nil)
	addMethod(m, "LoadOrStore", types.NewTuple(param("key", anyT), param("value", anyT)),
		types.NewTuple(param("actual", anyT), param("loaded", boolT)))
	addMethod(m, "LoadAndDelete", tuple(param("key", anyT)),
		types.NewTuple(param("value", anyT), param("loaded", boolT)))
	addMethod(m, "Delete", tuple(param("key", anyT)), nil)
	addMethod(m, "Swap", types.NewTuple(param("key", anyT), param("value", anyT)),
		types.NewTuple(param("previous", anyT), param("loaded", boolT)))
	addMethod(m, "CompareAndSwap",
		types.NewTuple(param("key", anyT), param("old", anyT), param("new", anyT)),
		tuple(param("swapped", boolT)))
	addMethod(m, "CompareAndDelete",
		types.NewTuple(param("key", anyT), param("old", anyT)),
		tuple(param("deleted", boolT)))
	addMethod(m, "Clear", nil, nil)
	rangeFuncSig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(param("key", anyT), param("value", anyT)),
		tuple(param("", boolT)), false)
	addMethod(m, "Range", tuple(param("f", rangeFuncSig)), nil)

	pkg.MarkComplete()
	return pkg
}

// atomicPrimitiveNames are the P3-M4 (WS C, ADR-0004) typed wrappers bound
// to the phase-1 JVM atomics (graalgo.runtime.Atomics), plus the P3-M10
// generic/noCopy additions: Pointer[T] (the generic typed pointer wrapper)
// and Value (the any-typed wrapper). Instantiations (Pointer[string], …)
// are *types.Named with type args, so the name check below still matches.
var atomicPrimitiveNames = map[string]bool{
	"Int64": true, "Uint64": true, "Int32": true, "Uint32": true, "Bool": true, "Uintptr": true,
	"Pointer": true, "Value": true,
}

// atomicOpsInScope is the M4-supported method set per wrapper (ADR-0004
// D2). And/Or are defined with exact gc signatures and rejected with a
// named M4 scope error (trivial future additions).
var atomicOpsInScope = map[string]map[string]bool{
	"Int64":   {"Load": true, "Store": true, "Add": true, "Swap": true, "CompareAndSwap": true},
	"Uint64":  {"Load": true, "Store": true, "Add": true, "Swap": true, "CompareAndSwap": true},
	"Int32":   {"Load": true, "Store": true, "Add": true, "Swap": true, "CompareAndSwap": true},
	"Uint32":  {"Load": true, "Store": true, "Add": true, "Swap": true, "CompareAndSwap": true},
	"Bool":    {"Load": true, "Store": true, "Swap": true, "CompareAndSwap": true},
	"Uintptr": {"Load": true, "Store": true, "Add": true, "Swap": true, "CompareAndSwap": true},
	// P3-M10: Pointer[T] and Value have no Add in gc (pointer/any
	// arithmetic is meaningless); the four reference ops are in scope.
	"Pointer": {"Load": true, "Store": true, "Swap": true, "CompareAndSwap": true},
	"Value":   {"Load": true, "Store": true, "Swap": true, "CompareAndSwap": true},
}

// atomicPrimitiveNamed reports whether t is exactly one of the synthetic
// sync/atomic wrapper named types (no pointer dereference).
func atomicPrimitiveNamed(t types.Type) *types.Named {
	nt, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	obj := nt.Obj()
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "sync/atomic" {
		return nil
	}
	if !atomicPrimitiveNames[obj.Name()] {
		return nil
	}
	return nt
}

// atomicPrimitiveRecv is atomicPrimitiveNamed after one pointer dereference,
// for method-call receivers (all atomic methods have pointer receivers;
// the Go value IS the Java atomic cell, so &/* is a no-op at the value
// level — the ADR-0002 D2/D1 value model).
func atomicPrimitiveRecv(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	return atomicPrimitiveNamed(t)
}

// syntheticPrimitiveNamed is syncPrimitiveNamed generalized to the
// sync/atomic overlay (P3-M4, ADR-0004).
func syntheticPrimitiveNamed(t types.Type) *types.Named {
	if nt := syncPrimitiveNamed(t); nt != nil {
		return nt
	}
	return atomicPrimitiveNamed(t)
}

// syntheticPrimitiveRecv is syncPrimitiveRecv generalized to the
// sync/atomic overlay (P3-M4, ADR-0004).
func syntheticPrimitiveRecv(t types.Type) *types.Named {
	if nt := syncPrimitiveRecv(t); nt != nil {
		return nt
	}
	return atomicPrimitiveRecv(t)
}

// runtimeStackMethod reports whether a MethodVal selection is one of the
// synthetic runtime stack-inspection methods ((*Frames).Next,
// (*Func).Name/Entry/FileLine), returning the method name. The check is
// structural on the receiver's named type (package path "runtime" plus the
// type name) — a user type with the same method names is never matched.
func runtimeStackMethod(selection *types.Selection) (string, bool) {
	recv := selection.Recv()
	if pt, ok := recv.(*types.Pointer); ok {
		recv = pt.Elem()
	}
	nt, ok := recv.(*types.Named)
	if !ok || nt.Obj() == nil || nt.Obj().Pkg() == nil || nt.Obj().Pkg().Path() != "runtime" {
		return "", false
	}
	switch mname := selection.Obj().Name(); {
	case nt.Obj().Name() == "Frames" && mname == "Next":
		return mname, true
	case nt.Obj().Name() == "Func" && (mname == "Name" || mname == "Entry" || mname == "FileLine"):
		return mname, true
	}
	return "", false
}

// syntheticOpTypeName returns the syncop "synctyp" for a synthetic primitive
// named type: the bare name for sync ("Mutex"), the "atomic."-qualified
// name for sync/atomic ("atomic.Int64").
func syntheticOpTypeName(nt *types.Named) string {
	if atomicPrimitiveNamed(nt) != nil {
		return "atomic." + nt.Obj().Name()
	}
	return nt.Obj().Name()
}

// syntheticScopeLabel returns the milestone scope label for a synthetic
// primitive's package: "M2" for sync, "M4" for sync/atomic (error text).
func syntheticScopeLabel(nt *types.Named) string {
	if atomicPrimitiveNamed(nt) != nil {
		return "M4"
	}
	return "M2"
}

// syntheticAtomicPackage builds the P3-M4 (WS C, ADR-0004) synthetic
// "sync/atomic" package for the import overlay: the six typed wrappers
// with exact gc 1.27.1 method signatures (pointer receivers, like the
// real package; verified against ~/tools/go-1.27.1/src/sync/atomic/type.go),
// plus the P3-M10 generic additions Pointer[T] (declared via the types
// API with a T-any type parameter; instantiations flow through the same
// overlay — atomicPrimitiveNamed matches the *types.Named instantiation)
// and Value (the any-typed wrapper), both with exact gc 1.27.1 method
// signatures (verified against ~/tools/go-1.27.1/src/sync/atomic/type.go
// and value.go). And/Or are defined with exact signatures so misuse fails
// fast with a named "not in M4 scope" error. Package-level functions
// (AddInt32, …) are defined with exact signatures and rejected at call
// sites (they need &x — WS D).
func syntheticAtomicPackage() *types.Package {
	pkg := types.NewPackage("sync/atomic", "atomic")
	int64T := types.Typ[types.Int64]
	uint64T := types.Typ[types.Uint64]
	int32T := types.Typ[types.Int32]
	uint32T := types.Typ[types.Uint32]
	boolT := types.Typ[types.Bool]
	uintptrT := types.Typ[types.Uintptr]
	emptyStruct := types.NewStruct(nil, nil)
	param := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	tuple := func(v *types.Var) *types.Tuple {
		return types.NewTuple(v)
	}

	newWrapper := func(name string) *types.Named {
		tname := types.NewTypeName(token.NoPos, pkg, name, nil)
		pkg.Scope().Insert(tname)
		return types.NewNamed(tname, emptyStruct, nil)
	}
	addMethod := func(named *types.Named, name string, params, results *types.Tuple) {
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(named))
		sig := types.NewSignatureType(recv, nil, nil, params, results, false)
		named.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	// The five M4 ops per wrapper: Load() T, Store(T), Add(T) T,
	// Swap(T) T, CompareAndSwap(T, T) bool. And/Or defined, rejected.
	fiveOps := func(named *types.Named, t types.Type) {
		boolRes := tuple(param("", boolT))
		addMethod(named, "Load", nil, tuple(param("", t)))
		addMethod(named, "Store", tuple(param("val", t)), nil)
		addMethod(named, "Add", tuple(param("delta", t)), tuple(param("new", t)))
		addMethod(named, "Swap", tuple(param("new", t)), tuple(param("old", t)))
		addMethod(named, "CompareAndSwap",
			types.NewTuple(param("old", t), param("new", t)), boolRes)
		addMethod(named, "And", tuple(param("mask", t)), tuple(param("old", t)))
		addMethod(named, "Or", tuple(param("mask", t)), tuple(param("old", t)))
	}
	fiveOps(newWrapper("Int64"), int64T)
	fiveOps(newWrapper("Uint64"), uint64T)
	fiveOps(newWrapper("Int32"), int32T)
	fiveOps(newWrapper("Uint32"), uint32T)
	fiveOps(newWrapper("Uintptr"), uintptrT)
	// Bool has no Add/And/Or in gc.
	b := newWrapper("Bool")
	boolRes := tuple(param("", boolT))
	addMethod(b, "Load", nil, tuple(param("", boolT)))
	addMethod(b, "Store", tuple(param("val", boolT)), nil)
	addMethod(b, "Swap", tuple(param("new", boolT)), tuple(param("old", boolT)))
	addMethod(b, "CompareAndSwap",
		types.NewTuple(param("old", boolT), param("new", boolT)), boolRes)

	// P3-M10: atomic.Pointer[T] — the generic typed pointer wrapper
	// (gc 1.27.1 sync/atomic/type.go: Load() *T, Store(*T), Swap(*T) *T,
	// CompareAndSwap(*T, *T) bool; no Add, like gc). Declared via the
	// types API: the named type carries a T-any type parameter, and the
	// method signatures reference the *types.TypeParam, so go/types
	// instantiates them for Pointer[string], Pointer[*T], … exactly as
	// it does for real generic sources. The overlay needs no other
	// change: atomicPrimitiveNamed matches the instantiation's
	// *types.Named by origin name, and the syncop wire node carries
	// synctyp "atomic.Pointer".
	anyT := types.NewInterfaceType(nil, nil).Complete()
	tname := types.NewTypeName(token.NoPos, pkg, "Pointer", nil)
	pkg.Scope().Insert(tname)
	ptrNamed := types.NewNamed(tname, emptyStruct, nil)
	tparam := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), anyT)
	ptrNamed.SetTypeParams([]*types.TypeParam{tparam})
	// addPtrMethod declares one method on Pointer[T]. The signature
	// carries its own receiver type parameters (rparams), as the checker
	// does for real generic methods (signature.go): the same *TypeParam
	// cannot be bound twice (bindTParams panics), and Named.expandMethod
	// substitutes the instantiation's type args via the signature's
	// rparams — so each method gets fresh T objects, and its
	// params/results are built against them. Without rparams the
	// signature would keep the raw *T at call sites.
	//
	// Every parameter is *T (named per gc); results are *T except
	// CompareAndSwap's bool. tStar marks the *T result slots.
	tStar := types.Type(nil)
	addPtrMethod := func(name string, pnames []string, rtypes []types.Type) {
		rtparam := types.NewTypeParam(types.NewTypeName(token.NoPos, pkg, "T", nil), anyT)
		tPtr := types.NewPointer(rtparam)
		pvars := make([]*types.Var, len(pnames))
		for i, pn := range pnames {
			pvars[i] = param(pn, tPtr)
		}
		rvars := make([]*types.Var, len(rtypes))
		for i, rt := range rtypes {
			if rt == tStar {
				rt = tPtr
			}
			rvars[i] = param("", rt)
		}
		recv := types.NewParam(token.NoPos, pkg, "", types.NewPointer(ptrNamed))
		var ptup, rtup *types.Tuple
		if len(pvars) > 0 {
			ptup = types.NewTuple(pvars...)
		}
		if len(rvars) > 0 {
			rtup = types.NewTuple(rvars...)
		}
		sig := types.NewSignatureType(recv, []*types.TypeParam{rtparam}, nil,
			ptup, rtup, false)
		ptrNamed.AddMethod(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	tBool := types.Typ[types.Bool]
	addPtrMethod("Load", nil, []types.Type{tStar})
	addPtrMethod("Store", []string{"val"}, nil)
	addPtrMethod("Swap", []string{"new"}, []types.Type{tStar})
	addPtrMethod("CompareAndSwap", []string{"old", "new"}, []types.Type{tBool})

	// P3-M10: atomic.Value — the any-typed wrapper (gc 1.27.1
	// sync/atomic/value.go: Load() any, Store(any), Swap(any) any,
	// CompareAndSwap(any, any) bool). The Go value IS the Java
	// AtomicReference cell (ADR-0002 D1), same as the typed wrappers.
	v := newWrapper("Value")
	addMethod(v, "Load", nil, tuple(param("", anyT)))
	addMethod(v, "Store", tuple(param("val", anyT)), nil)
	addMethod(v, "Swap", tuple(param("new", anyT)), tuple(param("old", anyT)))
	addMethod(v, "CompareAndSwap",
		types.NewTuple(param("old", anyT), param("new", anyT)), boolRes)

	// Package-level functions: exact gc signatures, rejected at call
	// sites (ADR-0004 D2 — they need &x, WS D).
	addFunc := func(name string, params, results *types.Tuple) {
		sig := types.NewSignatureType(nil, nil, nil, params, results, false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	for _, wt := range []types.Type{int32T, int64T} {
		nm := map[types.Type]string{int32T: "Int32", int64T: "Int64"}[wt]
		paddr := types.NewPointer(wt)
		addFunc("Load"+nm, tuple(param("addr", paddr)), tuple(param("", wt)))
		addFunc("Store"+nm, types.NewTuple(param("addr", paddr), param("val", wt)), nil)
		addFunc("Add"+nm, types.NewTuple(param("addr", paddr), param("delta", wt)), tuple(param("new", wt)))
		addFunc("Swap"+nm, types.NewTuple(param("addr", paddr), param("new", wt)), tuple(param("old", wt)))
		addFunc("CompareAndSwap"+nm,
			types.NewTuple(param("addr", paddr), param("old", wt), param("new", wt)), boolRes)
	}
	for _, wt := range []types.Type{uint32T, uint64T, uintptrT} {
		nm := map[types.Type]string{uint32T: "Uint32", uint64T: "Uint64", uintptrT: "Uintptr"}[wt]
		paddr := types.NewPointer(wt)
		addFunc("Load"+nm, tuple(param("addr", paddr)), tuple(param("", wt)))
		addFunc("Store"+nm, types.NewTuple(param("addr", paddr), param("val", wt)), nil)
		addFunc("Add"+nm, types.NewTuple(param("addr", paddr), param("delta", wt)), tuple(param("new", wt)))
		addFunc("Swap"+nm, types.NewTuple(param("addr", paddr), param("new", wt)), tuple(param("old", wt)))
		addFunc("CompareAndSwap"+nm,
			types.NewTuple(param("addr", paddr), param("old", wt), param("new", wt)), boolRes)
	}
	// Note: gc has no package-level bool atomics (only the atomic.Bool
	// wrapper type's methods); LoadBool/StoreBool/... are deliberately
	// absent so programs using them fail exactly like gc ("undefined").

	// P3-driver-harden (jsonlib lane): atomic.LoadPointer/StorePointer
	// with exact gc signatures. They lower in exportCall/exportStmt to
	// plain deref read/write (scoped divergence: atomicity without the
	// ordering guarantee); the rest of the package-level surface stays
	// M4.
	{
		unsafePtrT := types.Typ[types.UnsafePointer]
		pUnsafe := types.NewPointer(unsafePtrT)
		addFunc("LoadPointer", tuple(param("addr", pUnsafe)), tuple(param("", unsafePtrT)))
		addFunc("StorePointer", types.NewTuple(param("addr", pUnsafe), param("val", unsafePtrT)), nil)
	}

	pkg.MarkComplete()
	return pkg
}

// syntheticOpInScope reports whether op is an in-scope method of the
// synthetic primitive nt (P3-M4, ADR-0004: package-aware — sync/M2 vs
// sync/atomic/M4), failing fast with the named scope error otherwise.
func syntheticOpInScope(nt *types.Named, op string) error {
	typeName := nt.Obj().Name()
	if atomicPrimitiveNamed(nt) != nil {
		if !atomicOpsInScope[typeName][op] {
			return scopeErrorf("sync/atomic.%s.%s is not in M4 scope", typeName, op)
		}
		return nil
	}
	if !syncOpsInScope[typeName][op] {
		return scopeErrorf("sync.%s.%s is not in M2 scope", typeName, op)
	}
	return nil
}

// syntheticCompositeScopeError returns the D3 scope error for a synthetic
// primitive in a factory-zero-manufactured position (slice/array/map
// element, map key, struct field): the factory would manufacture an
// unusable empty-struct zero. Package-aware (sync/M2 vs sync/atomic/M4).
func syntheticCompositeScopeError(nt *types.Named, position string) error {
	if atomicPrimitiveNamed(nt) != nil {
		return scopeErrorf("sync/atomic.%s as %s is not in M4 scope", nt.Obj().Name(), position)
	}
	return scopeErrorf("sync.%s as %s is not in M2 scope", nt.Obj().Name(), position)
}

// ExportFile type-checks the single Go source file at path and returns the
// frontend/v1 JSON document.
func ExportFile(path string) (string, error) {
	return ExportFiles([]string{path})
}

// needsOverlayImporter reports whether import path p must resolve
// through the synthetic/disk overlay importer (WS G).
//
// General mechanism (hunt-special-cases): instead of a per-package
// allow-list, admit:
// 1. The 5 packages with dedicated synthetic constructors (not in
//    syntheticSources): runtime, fmt, sync, sync/atomic, unsafe.
// 2. Any package with a synthetic source (syntheticSources map) —
//    including future additions, no allow-list update needed.
// 3. Disk-importable third-party packages.
//
// Internal packages (internal/*, crypto/internal/*) have synthetic
// sources but are NOT user-importable; they are excluded here and in
// checkImportAllowed.
func needsOverlayImporter(p string) bool {
	// The 5 dedicated-constructor packages.
	if p == "runtime" || p == "fmt" || p == "sync" || p == "sync/atomic" || p == "unsafe" {
		return true
	}
	// General: any non-internal synthetic source.
	if _, ok := syntheticSources[p]; ok {
		if strings.HasPrefix(p, "internal/") || strings.HasPrefix(p, "crypto/internal/") {
			return false
		}
		return true
	}
	// WS G (P3-M9, module-probe lane): a disk-resolvable third-party
	// import also needs the overlay importer (its default case
	// builds the package from the module cache/GOPATH).
	return isDiskImportable(p)
}

// checkImportAllowed validates one import against the frontend/v1 import
// overlay (WS G): synthetic, disk-importable, or scope error.
//
// General mechanism (hunt-special-cases): instead of a per-package
// allow-list, admit:
// 1. The 5 packages with dedicated synthetic constructors: runtime, fmt,
//    sync, sync/atomic, unsafe.
// 2. Any package with a synthetic source (syntheticSources map) except
//    internal packages (internal/*, crypto/internal/*) which stay
//    synthetic-only.
// 3. Disk-importable third-party packages.
// Every other import is a scope error.
func checkImportAllowed(imp *ast.ImportSpec) error {
	path := strings.Trim(imp.Path.Value, "\"`")
	// The 5 dedicated-constructor packages.
	if path == "runtime" || path == "fmt" || path == "sync" || path == "sync/atomic" || path == "unsafe" {
		return nil
	}
	// General: any non-internal synthetic source.
	if _, ok := syntheticSources[path]; ok {
		if strings.HasPrefix(path, "internal/") || strings.HasPrefix(path, "crypto/internal/") {
			// Synthetic-only: user imports not admitted.
		} else {
			return nil
		}
	}
	// WS G (P3-M9, module-probe lane): third-party imports resolve
	// from local disk (module cache/GOPATH) through the general
	// import overlay; only truly unresolvable paths stay a scope
	// error.
	if isDiskImportable(path) {
		return nil
	}
	return fmt.Errorf("gofrontend: import %s: imports are not supported in frontend/v1 scope (WS G import overlay)", imp.Path.Value)
}

// ExportFiles type-checks the Go source files at paths — all files of one
// package — and returns the frontend/v1 JSON document.
//
// All files are parsed and type-checked together as a single package
// (go/types conf.Check over []*ast.File), so cross-file references
// resolve. All files must declare the same package name. Position "file"
// indices in the document index into the paths order.
func ExportFiles(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("gofrontend: no input files")
	}
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	srcs := make([][]byte, 0, len(paths))
	pkgName := ""
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("gofrontend: %v", err)
		}
		if fi.IsDir() {
			return "", fmt.Errorf("gofrontend: package directories are not supported in frontend/v1 scope (WS G): %s", path)
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("gofrontend: %v", err)
		}
		// P3-M4 (WS C, ADR-0004): parser.ParseComments populates doc comments
		// so //go:linkname directives are visible (fd.Doc).
		file, err := parser.ParseFile(fset, path, src, parser.AllErrors|parser.ParseComments)
		if err != nil {
			return "", fmt.Errorf("gofrontend: parse error: %v", err)
		}
		if pkgName == "" {
			pkgName = file.Name.Name
		} else if file.Name.Name != pkgName {
			return "", fmt.Errorf("gofrontend: %s declares package %s, expected %s", path, file.Name.Name, pkgName)
		}
		files = append(files, file)
		srcs = append(srcs, src)
	}
	for _, file := range files {
		for _, imp := range file.Imports {
			if err := checkImportAllowed(imp); err != nil {
				return "", err
			}
		}
	}

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		// M6 (WS H): go/types records generic instantiations here only
		// when the map is non-nil (verified by probe).
		Instances: make(map[*ast.Ident]types.Instance),
	}
	conf := types.Config{Importer: failImporter{}}
	// M5 (WS F): with the "runtime" import present, resolve it via the
	// synthetic overlay package (runtime.Goexit only).
	// M6 (WS H): with the "fmt" import present, resolve it via the
	// synthetic overlay package (D4 fmt stub).
	// P3-M2b (WS D, ADR-0002): with the "sync" import present, resolve it
	// via the synthetic overlay package (four primitives).
	// P3-M4 (WS C, ADR-0004): "sync/atomic" and "unsafe" likewise.
	// P3-M5 (WS E, ADR-0005): "time" and "context" likewise.
	// P3-M7 (wave 1): "errors", "sort", "bytes", "math", "strings",
	// "encoding/xml" likewise.
	// P3-M7 (wave 1): the 8 encoding/* primitive packages likewise.
	// (m7-wenc-core: "strings" was missing from this list — the allow-list
	// above admitted it but type-checking still used failImporter. Fixed
	// here; re-test trigger: a program importing "strings" must export.)
	// P3-M8 (wave 2): "io", "syscall", "os", "io/fs", "reflect", "hash",
	// "iter", and the crypto/* packages likewise.
	for _, file := range files {
		for _, imp := range file.Imports {
			if needsOverlayImporter(strings.Trim(imp.Path.Value, "\"`")) {
				conf.Importer = overlayImporter{}
			}
		}
	}
	pkg, err := conf.Check(pkgName, fset, files, info)
	if err != nil {
		return "", fmt.Errorf("gofrontend: type error: %v", err)
	}

	e := &exporter{
		fset:    fset,
		src:     srcs[0], // fallback for single-file paths; multi-file uses srcByFile
		info:    info,
		pkg:     pkg,
		typeIDs: make(map[types.Type]int),
		// P3-M9 (WS H): type parameters canonicalized by TypeName object.
		// go/types mints fresh *types.TypeParam per occurrence; without
		// this, F-bounded constraints (P Point[P]) recurse infinitely
		// because the P in the constraint is a different object.
		typeparamIDs: make(map[*types.TypeName]int),
		// P3-M9 (generics, issue48344): per-substitution type memo,
		// SHARED across instantiations (not reset per instantiation).
		// Keyed by substituted (canonicalized) type, so the same
		// logical type maps to one wire id everywhere; resetting it
		// per instantiation causes infinite describeType recursion.
		substTypeIDs: make(map[types.Type]int),
		substCanon:   make(map[string]types.Type),
		namedIDs:     make(map[string]int),
		// P3-M8-leftovers: anonymous *types.Interface deduplication by
		// structural key (sorted method implkeys). go/types mints a fresh
		// *types.Interface per syntactic occurrence, so without this the
		// type assertion and the method dispatch on an anonymous interface
		// get different wire ids → wrong itab → wrong dispatch results.
		ifaceIDs: make(map[string]int),
		objIDs:   make(map[types.Object]int),
		// P3-M9: synthetic package vars by (path, name) for cross-package
		// var references (type-checked in isolation, pointers not shared).
		synthVarIDs: make(map[string]int),
		// P3-M9: synthetic package funcs by (path, name) for cross-package
		// generic instantiations.
		synthFuncIDs: make(map[string]*funcDeclInfo),
		sizes:        types.SizesFor("gc", "amd64"),
		// P3-M8: track imported synthetic packages for idempotency.
		importedSynthetic: make(map[string]bool),
		// WS G (P3-M9): track importPkgTree walks for idempotency.
		importedPkgTree: make(map[string]bool),
		// P3-M9 (WS D): user-defined multi-result functions transformed
		// to pair-returning form.
		userMRFuncs: make(map[*types.Func]*userMRInfo),
	}
	// fileBases lists the input files in order; position "file" indices
	// index into this array (frontend/v1 wire format).
	fileBases := make([]string, 0, len(paths))
	for _, p := range paths {
		fileBases = append(fileBases, filepath.Base(p))
	}
	doc := Doc{
		Frontend:  frontendVersion,
		Generator: Generator{Tool: "gofrontend", Version: toolVersion, GoRoot: runtime.Version()},
		Package:   Pkg{Name: pkgName, Files: fileBases, Lang: langVersion},
		Types:     []TypeEnt{},
		Objects:   []ObjectEnt{},
		Funcs:     []*Node{},
	}
	// Per-file source bytes and file-index maps for positions.
	e.fileIndex = make(map[*token.File]int, len(files))
	e.srcByFile = make(map[*token.File][]byte, len(files))
	for i, file := range files {
		if tf := fset.File(file.Pos()); tf != nil {
			e.fileIndex[tf] = i
			e.srcByFile[tf] = srcs[i]
		}
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				// M5 (WS F): the single allowed import ("runtime", for
				// runtime.Goexit) is a GenDecl; it was validated above and
				// carries no code to export. Type declarations are also
				// GenDecls; v1 supports only struct types without methods
				// (named types for panic rendering and type assertions).
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
					continue
				}
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
					for _, spec := range gd.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							return "", scopeErrorf("package-level type spec %T is not supported (WS D)", spec)
						}
						tn, ok := e.info.Defs[ts.Name].(*types.TypeName)
						if !ok {
							return "", fmt.Errorf("gofrontend: type name %q has no *types.TypeName object", ts.Name.Name)
						}
						// P3-M9 (generics, Gap 2): skip generic template definitions.
						// tn.Type() is the *types.Named (e.g., nistPoint[T]); the
						// underlying interface/struct is e.info.Types[ts.Type].Type.
						// Only the Named tells us if it's a generic template.
						if named, ok := tn.Type().(*types.Named); ok {
							if origin := named.Origin(); origin == named {
								if tparams := origin.TypeParams(); tparams != nil && tparams.Len() > 0 {
									if named.TypeArgs() == nil || named.TypeArgs().Len() == 0 {
										continue
									}
								}
							}
						}
						if _, err := e.typeID(e.info.Types[ts.Type].Type); err != nil {
							return "", err
						}
					}
					continue
				}
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
					// M7: package-level constants need no codegen — every
					// reference folds through go/types (the *types.Const case
					// in exportIdent), which has already evaluated them
					// (iota included). Validate the specs are value specs.
					for _, spec := range gd.Specs {
						if _, ok := spec.(*ast.ValueSpec); !ok {
							return "", scopeErrorf("package-level const spec %T is not supported (M7)", spec)
						}
					}
					continue
				}
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
					// M7: package-level vars become globals; initializers are
					// exported after the declaration scan (dependency order).
					if err := e.collectPackageVars(gd); err != nil {
						return "", err
					}
					continue
				}
				return "", scopeErrorf("package-level %T is not supported in frontend/v1 scope (WS G/H)", decl)
			}
			_ = fd // collected below (M6, WS H)
		}
	}
	// M6 (WS H): collect function/method declarations, enumerate
	// monomorphized instantiations, then emit in source order (declared
	// functions) followed by deterministic instantiation order.
	funcs, funcByObj, methodIndex, err := e.collectFuncDecls(files)
	if err != nil {
		return "", err
	}
	e.funcByObj = funcByObj
	e.methodIndex = methodIndex
	// P3-M5 (WS E, ADR-0005): import the synthetic time + context
	// declarations (real-Go bodies) into the document's tables — but
	// ONLY when the user program imports them. Unconditional import
	// would bloat every document (and break golden tests); the context
	// package implies time (its bodies call time.AfterFunc etc.).
	// Their funcs/methods join funcByObj/methodIndex (shared *types
	// objects via the overlay importer's package cache) and their
	// package vars become document globals. Intrinsics (time.Now etc.)
	// are skipped — every call site is intercepted into wire nodes.
	needTime, needContext := false, false
	// WS G (P3-M9, module-probe lane): third-party direct imports that
	// survived the gate; imported after the synthetic direct imports.
	var needDisk []string
	needErrors, needSort, needBytes, needMath, needStrings, needXml, needCsv, needGob := false, false, false, false, false, false, false, false
	needStrconv, needJson := false, false
	needBase32, needBase64, needHex, needBinary := false, false, false, false
	needAscii85, needPem, needAsn1, needEncoding := false, false, false, false
	needIo := false
	// P3-M9 (tier1c): synthetic math/bits.
	needMathBits := false
	// P3-M9 (tier1c): synthetic math/cmplx.
	needMathCmplx := false
	// P3-M9 (wave 3): synthetic math/big (G-c hard gate) + math/rand
	// (minimal port) + internal/byteorder (vendored).
	needMathBig := false
	needMathRand := false
	needMathRandV2 := false
	needBufio := false
	needPath := false
	needTextScanner := false
	needRegexpSyntax := false
	// P3-M9 (tier-5: compress): synthetic compress/* packages.
	needFlate := false
	needGzip := false
	needZlib := false
	needLzw := false
	needBzip2 := false
	// P3-M9 (tier-5: net/http): synthetic net/http and net/http/httptest.
	needNetHttp := false
	needNetHttpHttptest := false
	// P3-M9 (tier-4: net): synthetic net package (Dial/Listen/TCP/UDP).
	needNet := false
	needByteorder := false
	needSyscall := false
	needOs := false
	needIoFs := false
	needReflect := false
	needCryptoSha256 := false
	needHash := false
	needIter := false
	// P3-M8b: the remaining synthetic crypto packages (the corpus closure).
	needCrypto := false
	needCryptoAes := false
	needCryptoCipher := false
	needCryptoHmac := false
	needCryptoMd5 := false
	needCryptoRand := false
	needCryptoSha1 := false
	needCryptoSha3 := false
	needCryptoSha512 := false
	needCryptoSubtle := false
	// P3-M9 (tier-5): asymmetric crypto public packages.
	needCryptoElliptic := false
	needCryptoEcdh := false
	needCryptoDsa := false
	needCryptoEcdsa := false
	needCryptoEd25519 := false
	needCryptoRsa := false
	needCryptoHkdf := false
	needCryptoPbkdf2 := false
	needCryptoX509 := false
	needCryptoTls := false
	needCryptobyte := false
	needCryptobyteAsn1 := false
	// P3-M9 (wave 3, tier-1a worker): the synthetic cmp/slices/maps/unicode
	// packages (the corpus closure).
	needCmp := false
	needSlices := false
	needMaps := false
	needUnicode := false
	needUnicodeUtf8 := false
	needUnicodeUtf16 := false
	// P3-M9 (wave 3, tier-1b worker): the synthetic container/* and
	// hash/* packages (the corpus closure).
	needContainerHeap := false
	needContainerList := false
	needContainerRing := false
	needHashAdler32 := false
	needHashCrc32 := false
	needHashCrc64 := false
	needHashFnv := false
	needHashMaphash := false
	// P3-M9: synthetic flag, testing, testing/quick, testing/iotest
	// (the go-test trial prerequisite packages).
	needFlag := false
	needTesting := false
	needTestingQuick := false
	needTestingIotest := false
	// P3-M9 (tier-1d): leaf utilities unique/structs.
	needUnique := false
	needStructs := false
	for _, file := range files {
		for _, imp := range file.Imports {
			switch strings.Trim(imp.Path.Value, "\"`") {
			case "time":
				needTime = true
			case "context":
				needTime, needContext = true, true
			case "errors":
				needErrors = true
			case "sort":
				needSort = true
			case "bytes":
				// bytes' ErrTooLarge initializer calls errors.New.
				// NB: requires m7-errors-sort (integration dependency).
				// P3-M9: synthetic bytes imports io (Reader/Writer, io.EOF).
				needBytes = true
				needErrors = true
				needIo = true
			case "math":
				needMath = true
			// P3-M9 (tier1c): synthetic math/bits (imports synthetic errors
			// for the reconstructed overflow/divide sentinels).
			case "math/bits":
				needMathBits = true
				needErrors = true
			// P3-M9 (tier1c): synthetic math/cmplx (imports synthetic math
			// and math/bits).
			case "math/cmplx":
				needMathCmplx = true
				needMath = true
				needMathBits = true
				needErrors = true
			case "strings":
				needStrings = true
				// P3-M8: the synthetic strings source imports errors (gc-exact
				// errors.New in Reader.ReadAt/Seek) — bytes/strconv precedent.
				needErrors = true
				// P3-M8: strings also imports io (io.EOF in Reader.Read).
				needIo = true
			case "encoding/xml":
				needXml = true
			case "strconv":
				// strconv's ErrRange/ErrSyntax initializers call errors.New.
				needStrconv = true
				needErrors = true
			case "encoding/json":
				needJson = true
			case "encoding/csv":
				needCsv = true
				needErrors = true
			case "encoding/gob":
				needGob = true
				needErrors = true
			case "encoding/base32":
				needBase32 = true
			case "encoding/base64":
				needBase64 = true
			case "encoding/hex":
				needHex = true
			case "encoding/binary":
				needBinary = true
			case "encoding/ascii85":
				needAscii85 = true
			case "encoding/pem":
				// pem's bodies reference base64.CorruptInputError and
				// base64.StdEncoding (type conversion, package var, methods).
				// NB: requires m7-wenc-core (integration dependency).
				needPem = true
				needBase64 = true
			case "encoding/asn1":
				needAsn1 = true
			case "encoding":
				needEncoding = true
			case "io":
				needIo = true
			// P3-M8 (wave 2): a user import of "syscall" pulls in the
			// synthetic package (synth_syscall.go).
			case "syscall":
				needSyscall = true
			// P3-M8b: a user import of "os" pulls in the synthetic package
			// (synth_os.go). Its source imports errors, io, io/fs, syscall,
			// time; those are imported by the needOs block below in
			// dependency order.
			case "os":
				needOs = true
			// P3-M8 (wave 2): a user import of "io/fs" pulls in the minimal
			// synthetic io/fs core (synth_iofs.go). Its Err* sentinel
			// initializers call errors.New; the source also imports time.
			case "io/fs":
				needIoFs = true
				needErrors = true
				needTime = true
			// real-reflect: a user import of "reflect" resolves the real GOROOT
			// package via the disk importer (synthetic reflect retired).
			case "reflect":
				needReflect = true
			// P3-M8 (wave 2): a user import of "crypto/sha256" pulls in the
			// synthetic package (synth_crypto_sha256.go). The source imports
			// crypto, errors, hash; hash imports io; crypto imports hash, io,
			// strconv.
			case "crypto/sha256":
				needCryptoSha256 = true
				needErrors = true
				needStrconv = true
			// P3-M8b: a user import of any other synthetic crypto package
			// pulls it in (synth_crypto*.go). Dependency chains are imported
			// in the need-blocks below (importSyntheticPackage is idempotent).
			case "crypto":
				needCrypto = true
			case "crypto/aes":
				needCryptoAes = true
			case "crypto/cipher":
				needCryptoCipher = true
			case "crypto/hmac":
				needCryptoHmac = true
			case "crypto/md5":
				needCryptoMd5 = true
			case "crypto/rand":
				needCryptoRand = true
			case "crypto/sha1":
				needCryptoSha1 = true
			case "crypto/sha3":
				needCryptoSha3 = true
			case "crypto/sha512":
				needCryptoSha512 = true
			case "crypto/subtle":
				needCryptoSubtle = true
			// P3-M9 (tier-5): asymmetric crypto public packages.
			case "crypto/elliptic":
				needCryptoElliptic = true
			case "crypto/ecdh":
				needCryptoEcdh = true
			case "crypto/dsa":
				needCryptoDsa = true
			case "crypto/ecdsa":
				needCryptoEcdsa = true
			case "crypto/ed25519":
				needCryptoEd25519 = true
			case "crypto/rsa":
				needCryptoRsa = true
			case "crypto/hkdf":
				needCryptoHkdf = true
			case "crypto/pbkdf2":
				needCryptoPbkdf2 = true
			case "crypto/x509":
				needCryptoX509 = true
			case "crypto/tls":
				needCryptoTls = true
			case "golang.org/x/crypto/cryptobyte":
				needCryptobyte = true
			case "golang.org/x/crypto/cryptobyte/asn1":
				needCryptobyteAsn1 = true
			// P3-M8 (wave 2): direct user imports of the synthetic hash and
			// iter support packages.
			case "hash":
				needHash = true
				needErrors = true
				needIo = true
			case "iter":
				needIter = true
			// P3-M9 (wave 3, tier-1a worker): direct user imports of the
			// synthetic cmp/slices/maps/unicode packages. slices pulls in
			// cmp and iter (its source imports both); maps pulls in iter.
			case "cmp":
				needCmp = true
			case "slices":
				needSlices = true
				needCmp = true
				needIter = true
			case "maps":
				needMaps = true
				needIter = true
			case "unicode":
				needUnicode = true
			case "unicode/utf8":
				needUnicodeUtf8 = true
			case "unicode/utf16":
				needUnicodeUtf16 = true
			// P3-M9: a user import of "flag" pulls in the synthetic package
			// (synth_flag.go). The source imports encoding, errors, io, os,
			// reflect, sort, strconv, strings, time; the os chain (errors,
			// time, io, io/fs, syscall, strconv) is imported by the needFlag
			// block below in dependency order (importSyntheticPackage is
			// idempotent, so overlap with other need-blocks is safe).
			case "flag":
				needFlag = true
			// P3-M9: a user import of "testing" pulls in the synthetic package
			// (synth_testing.go). The source imports flag plus bytes, context,
			// errors, io, os, runtime (frozen), strconv, strings, sync,
			// sync/atomic, time.
			case "testing":
				needTesting = true
				needFlag = true
			// P3-M9: a user import of "testing/quick" pulls in the synthetic
			// package (synth_testing_quick.go). The source imports flag
			// (-quickchecks), fmt, math, reflect, strings.
			case "testing/quick":
				needTestingQuick = true
				needFlag = true
			// P3-M9: a user import of "testing/iotest" pulls in the synthetic
			// package (synth_testing_iotest.go). The source imports bytes,
			// errors, fmt, io, os.
			case "testing/iotest":
				needTestingIotest = true
			// P3-M9 (tier-1d): leaf utilities unique/structs.
			case "unique":
				needUnique = true
			case "structs":
				needStructs = true
			// P3-M9 (wave 3): direct user imports of synthetic math/big /
			// math/rand. math/big pulls its full synthetic dependency
			// closure (bits/rand/byteorder + the M7/M8 packages it uses).
			case "math/big":
				needMathBig = true
				needMathBits = true
				needMathRand = true
				needByteorder = true
				needBytes = true
				needErrors = true
				needIo = true
				needMath = true
				needStrconv = true
				needStrings = true
			case "math/rand":
				needMathRand = true
				// P3-M9 (tier1c): the full math/rand port imports the
				// synthetic math package (exp.go/normal.go/zipf.go).
				needMath = true
			case "math/rand/v2":
				needMathRandV2 = true
				// P3-M9 (tier1c): v2 imports synthetic math, math/bits and
				// internal/byteorder.
				needMath = true
				needMathBits = true
				needByteorder = true
			case "bufio":
				needBufio = true
				// P3-M9 (tier1c): bufio imports bytes, errors, io, strings
				// (all in overlay or synthetic).
				needBytes = true
				needErrors = true
				needIo = true
				needStrings = true
			case "path":
				needPath = true
				// P3-M9 (tier1c): path imports strings (synthetic).
				needStrings = true
			case "text/scanner":
				needTextScanner = true
			case "regexp/syntax":
				needRegexpSyntax = true
			// P3-M9 (wave 3, tier-1b): direct user imports of the synthetic
			// container/* and hash/* packages. container/heap pulls in sort;
			// hash/* pull in hash (+errors/io via the needHash chain);
			// maphash additionally pulls in crypto/rand and math.
			case "container/heap":
				needContainerHeap = true
				needSort = true
			case "container/list":
				needContainerList = true
			case "container/ring":
				needContainerRing = true
			case "hash/adler32":
				needHashAdler32 = true
				needHash = true
				needErrors = true
				needIo = true
			case "hash/crc32":
				needHashCrc32 = true
				needHash = true
				needErrors = true
				needIo = true
			case "hash/crc64":
				needHashCrc64 = true
				needHash = true
				needErrors = true
				needIo = true
			case "hash/fnv":
				needHashFnv = true
				needHash = true
				needErrors = true
				needIo = true
			case "hash/maphash":
				needHashMaphash = true
				needHash = true
				needCryptoRand = true
				needMath = true
			// P3-M9 (tier-5: compress): synthetic compress/* packages.
			// flate imports bufio/errors/fmt/io/math/math/bits/slices/
			// strconv/sync; gzip adds compress/flate/hash/crc32/time;
			// zlib adds compress/flate/hash/hash/adler32; lzw needs
			// bufio/errors/fmt/io; bzip2 needs bufio/cmp/io/slices.
			case "compress/flate":
				needFlate = true
				needBufio = true
				needErrors = true
				needIo = true
				needMath = true
				needMathBits = true
				needSlices = true
				needStrconv = true
				// sync is a builtin stub (syntheticSyncPackage), always
				// available via the overlay importer.
			case "compress/gzip":
				needGzip = true
				needFlate = true
				needBufio = true
				needErrors = true
				needIo = true
				needTime = true
				needHashCrc32 = true
				needHash = true
				// P3-M9: flate (via needFlate) needs math, math/bits, strconv.
				needMath = true
				needMathBits = true
				needStrconv = true
				// P3-M9: gzip imports encoding/binary (binary.LittleEndian).
				needBinary = true
			case "compress/zlib":
				needZlib = true
				needFlate = true
				needBufio = true
				needErrors = true
				needIo = true
				needHashAdler32 = true
				needHash = true
				// P3-M9: flate (via needFlate) needs math, math/bits, strconv.
				needMath = true
				needMathBits = true
				needStrconv = true
				// P3-M9: zlib imports encoding/binary (binary.LittleEndian).
				needBinary = true
			case "compress/lzw":
				needLzw = true
				needBufio = true
				needErrors = true
				needIo = true
			case "compress/bzip2":
				needBzip2 = true
				needBufio = true
				needCmp = true
				needIo = true
				needSlices = true
			// P3-M9 (tier-4: net): synthetic net (Dial/Listen/TCP/UDP).
			// net imports errors, internal/poll, io, strconv, strings,
			// syscall, time.
			case "net":
				needNet = true
				needErrors = true
				needIo = true
				needStrconv = true
				needStrings = true
				needTime = true
				// syscall NOT imported: pre-existing io/fs export issue.
				// Errno constants are defined locally in the net source.
			// P3-M9 (tier-5: net/http): synthetic net/http and net/http/httptest.
			// net/http imports bufio, bytes, errors, io, net, net/textproto,
			// net/url, strconv, strings, sync, time. The client delegates to
			// java.net.http.HttpClient via the graalgo.http.client.do intrinsic.
			// httptest imports bytes, io, net, net/http, net/url, strings, sync.
			case "net/http":
				needNetHttp = true
				needNet = true
				needBufio = true
				needBytes = true
				needErrors = true
				needIo = true
				needStrconv = true
				needStrings = true
				needTime = true
				// net, net/textproto, net/url resolve via the overlay importer
			case "net/http/httptest":
				needNetHttpHttptest = true
				needNetHttp = true
				needBufio = true
				needBytes = true
				needErrors = true
				needIo = true
				needStrconv = true
				needStrings = true
				needTime = true
			default:
				// WS G (P3-M9, module-probe lane): any other import that
				// survived the ExportFile gate is a disk-resolvable third-party
				// package — import its transitive closure through the general
				// overlay (dependencies first), after the synthetic direct
				// imports below. Dedupe; importPkgTree is idempotent anyway.
				p := strings.Trim(imp.Path.Value, "\"`")
				dup := false
				for _, q := range needDisk {
					if q == p {
						dup = true
						break
					}
				}
				if !dup {
					needDisk = append(needDisk, p)
				}
			}
		}
	}
	if needTime {
		if err := e.importSyntheticPackage(getSyntheticPackage("time")); err != nil {
			return "", err
		}
	}
	if needContext {
		if err := e.importSyntheticPackage(getSyntheticPackage("context")); err != nil {
			return "", err
		}
	}
	// P3-M9 (RC-6): the fmt.State implementation (internal/graalgo/fmtstate)
	// must be in the export so Fmt.handleMethods can box a PrinterState as
	// fmt.State for fmt.Formatter dispatch. The package is synthetic-only
	// (never imported by user code) and tiny, so import it unconditionally;
	// importSyntheticPackage is idempotent.
	if err := e.importSyntheticPackage(getSyntheticPackage("internal/graalgo/fmtstate")); err != nil {
		return "", err
	}
	// P3-M7 (wave 1): synthetic errors/sort are self-contained (no
	// synthetic->synthetic imports), so no ordering constraints.
	if needErrors {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
	}
	if needSort {
		if err := e.importSyntheticPackage(getSyntheticPackage("sort")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): synthetic bytes package (synth_bytes.go).
	// Requires m7-errors-sort for the errors dependency.
	if needBytes {
		if err := e.importSyntheticPackage(getSyntheticPackage("bytes")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): the synthetic math package (see synth_math.go).
	// Self-contained: no synthetic->synthetic imports, so no ordering
	// constraints.
	if needMath {
		if err := e.importSyntheticPackage(getSyntheticPackage("math")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): the synthetic math/bits package (see
	// synth_mathbits.go). Imports the synthetic errors package (for the
	// reconstructed overflow/divide sentinels), so errors must be
	// imported first.
	if needMathBits {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/bits")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): the synthetic math/cmplx package (see
	// synth_mathcmplx.go). Imports the synthetic math and math/bits
	// packages, so those (and their deps) must be imported first.
	if needMathCmplx {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/bits")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/cmplx")); err != nil {
			return "", err
		}
	}
	// P3-M9: synthetic math/rand (full port, tier1c; imports the synthetic
	// math package) and internal/byteorder (vendored; no deps).
	if needMathRand {
		if err := e.importSyntheticPackage(getSyntheticPackage("math")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/rand")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): synthetic math/rand/v2 (full port; imports synthetic
	// math, math/bits and internal/byteorder).
	if needMathRandV2 {
		if err := e.importSyntheticPackage(getSyntheticPackage("math")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/bits")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("internal/byteorder")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("math/rand/v2")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): synthetic bufio (full port; imports bytes which is
	// emitted via needBytes).
	if needBufio {
		if err := e.importSyntheticPackage(getSyntheticPackage("bufio")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier-5: compress): synthetic compress/* packages (see
	// synth_compress_{flate,gzip,zlib,lzw,bzip2}.go). flate first (gzip
	// and zlib import it); the other deps (bufio/errors/io/math/
	// math/bits/slices/strconv/hash/hash/adler32/hash/crc32/time) are
	// imported by their own need-flags above.
	if needFlate {
		if err := e.importSyntheticPackage(getSyntheticPackage("compress/flate")); err != nil {
			return "", err
		}
	}
	if needGzip {
		if err := e.importSyntheticPackage(getSyntheticPackage("compress/gzip")); err != nil {
			return "", err
		}
	}
	if needZlib {
		if err := e.importSyntheticPackage(getSyntheticPackage("compress/zlib")); err != nil {
			return "", err
		}
	}
	if needLzw {
		if err := e.importSyntheticPackage(getSyntheticPackage("compress/lzw")); err != nil {
			return "", err
		}
	}
	if needBzip2 {
		if err := e.importSyntheticPackage(getSyntheticPackage("compress/bzip2")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier-5: net/http): synthetic net/http and net/http/httptest.
	// The client delegates to java.net.http.HttpClient via the
	// graalgo.http.client.do intrinsic (registered in LinknameIntrinsics).
	// P3-M10: importSyntheticPackage does not recurse into imports, so
	// the block lists net/http's full transitive closure explicitly
	// (topological order, leaves first) — cf. the needCryptoRand block.
	// Without net/url et al., method resolution fails with
	// "gofrontend: method X not found on Y" for any program importing
	// net/http.
	if needNetHttp {
		for _, p := range []string{"errors", "io", "strconv", "bytes", "strings", "time",
			"bufio", "cmp", "iter", "internal/bytealg", "internal/byteorder",
			"math", "math/bits", "unique", "slices", "net/netip",
			"net/textproto", "net/url", "net", "net/http"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needNetHttpHttptest {
		if err := e.importSyntheticPackage(getSyntheticPackage("net/http/httptest")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): synthetic path.
	if needPath {
		if err := e.importSyntheticPackage(getSyntheticPackage("path")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): synthetic text/scanner.
	if needTextScanner {
		if err := e.importSyntheticPackage(getSyntheticPackage("text/scanner")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier1c): synthetic regexp/syntax (scope-limited).
	if needRegexpSyntax {
		if err := e.importSyntheticPackage(getSyntheticPackage("regexp/syntax")); err != nil {
			return "", err
		}
	}
	if needByteorder {
		if err := e.importSyntheticPackage(getSyntheticPackage("internal/byteorder")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3): synthetic math/big (G-c hard gate). Deps: errors,
	// io, bytes, math, strconv, strings (M7/M8 synthetics), math/bits,
	// math/rand, internal/byteorder (above). fmt/sync resolve via the
	// overlayImporter to the programmatic M5/M2b synthetics (no
	// importSyntheticPackage needed).
	if needMathBig {
		for _, p := range []string{"errors", "io", "bytes", "math", "strconv", "strings", "math/bits", "math/rand", "internal/byteorder", "math/big"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M8 (wave 2): synthetic io (depends on synthetic errors).
	// Must come before strings (which imports io for io.EOF).
	if needIo {
		// P3-M8: io's error vars use errors.New — import errors first.
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): the synthetic strings package (see synth_strings.go).
	// P3-M8: imports synthetic io (io.EOF), so io must be imported first.
	if needStrings {
		if err := e.importSyntheticPackage(getSyntheticPackage("strings")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): synthetic encoding/xml (see synth_xml.go).
	// Self-contained: no synthetic dependencies.
	if needXml {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/xml")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): encoding/csv + encoding/gob (see synth_enc_csv_gob.go).
	if needCsv {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/csv")); err != nil {
			return "", err
		}
	}
	if needGob {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/gob")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): strconv + encoding/json (see synth_strconv.go,
	// synth_json.go).
	if needStrconv {
		if err := e.importSyntheticPackage(getSyntheticPackage("strconv")); err != nil {
			return "", err
		}
	}
	if needJson {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/json")); err != nil {
			return "", err
		}
	}
	// P3-M7 (wave 1): the 8 encoding/* primitive packages (see
	// synth_base32.go ... synth_encoding.go). The multi-result members
	// (Decode/DecodeString/AppendDecode, Uvarint/Varint, ascii85.Decode,
	// pem.Decode, asn1.Marshal/Unmarshal) are skipped at import (WS D);
	// 2-/3-target call sites desugar via Appendix M (mrM7 in
	// mrdesugar_m5.go). encoding/pem implies encoding/base64 (its bodies
	// reference base64.CorruptInputError and base64.StdEncoding).
	if needBase32 {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/base32")); err != nil {
			return "", err
		}
	}
	if needBase64 {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/base64")); err != nil {
			return "", err
		}
	}
	if needHex {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/hex")); err != nil {
			return "", err
		}
	}
	if needBinary {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/binary")); err != nil {
			return "", err
		}
	}
	if needAscii85 {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/ascii85")); err != nil {
			return "", err
		}
	}
	if needPem {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/pem")); err != nil {
			return "", err
		}
	}
	if needAsn1 {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding/asn1")); err != nil {
			return "", err
		}
	}
	if needEncoding {
		if err := e.importSyntheticPackage(getSyntheticPackage("encoding")); err != nil {
			return "", err
		}
	}
	// (needIo handled before strings import above)
	// P3-M8 (wave 2): synthetic hash package (synth_crypto_hash.go).
	// Imports io (needIo block above imports errors first, then io).
	if needHash {
		if err := e.importSyntheticPackage(getSyntheticPackage("hash")); err != nil {
			return "", err
		}
	}
	// P3-M8 (wave 2): synthetic iter package (synth_reflect.go).
	// Self-contained: no synthetic imports.
	if needIter {
		if err := e.importSyntheticPackage(getSyntheticPackage("iter")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3, tier-1a worker): synthetic cmp package (synth_cmp.go).
	// Self-contained: no synthetic imports.
	if needCmp {
		if err := e.importSyntheticPackage(getSyntheticPackage("cmp")); err != nil {
			return "", err
		}
	}
	if needIter {
		if err := e.importSyntheticPackage(getSyntheticPackage("iter")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3, tier-1a worker): synthetic slices package
	// (synth_slices.go). The source imports cmp and iter; both are
	// imported before slices itself (package-level initializers may call
	// synthetic functions).
	if needSlices {
		if err := e.importSyntheticPackage(getSyntheticPackage("cmp")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("iter")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("slices")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3, tier-1a worker): synthetic maps package
	// (synth_maps.go). The source imports iter.
	if needMaps {
		if err := e.importSyntheticPackage(getSyntheticPackage("iter")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("maps")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3, tier-1a worker): synthetic unicode packages
	// (synth_unicode*.go). Self-contained: no synthetic imports
	// (utf16 deliberately duplicates the surrogate constants to avoid
	// depending on package unicode, mirroring gc).
	if needUnicode {
		if err := e.importSyntheticPackage(getSyntheticPackage("unicode")); err != nil {
			return "", err
		}
	}
	if needUnicodeUtf8 {
		if err := e.importSyntheticPackage(getSyntheticPackage("unicode/utf8")); err != nil {
			return "", err
		}
	}
	if needUnicodeUtf16 {
		if err := e.importSyntheticPackage(getSyntheticPackage("unicode/utf16")); err != nil {
			return "", err
		}
	}
	// real-reflect: the real reflect package compiles from GOROOT via the
	// disk importer. importPkgTree merges its transitive closure
	// dependencies-first through the existing overlay cases and the disk
	// importer; idempotent via e.importedPkgTree.
	if needReflect {
		if err := e.importPkgTree("reflect"); err != nil {
			return "", err
		}
	}
	// P3-M8 (wave 2): synthetic io/fs core (synth_iofs.go). Depends on
	// the synthetic errors package (its Err* sentinel initializers call
	// errors.New) and imports time (time.Time in the FileInfo/DirEntry
	// signatures), so errors and time are imported first.
	if needIoFs {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("time")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io/fs")); err != nil {
			return "", err
		}
	}
	// P3-M8 (wave 2): synthetic syscall (depends on the synthetic io/fs
	// registration for its Errno.Is sentinels - os worker, R5).
	if needSyscall {
		// P3-M8: the synthetic syscall source references strconv.Itoa,
		// errors.ErrUnsupported, and the io/fs sentinels (fs.ErrPermission
		// etc. in Errno.Is); import them directly. Ordering matters:
		// package-level var initializers may call synthetic functions, so
		// each dependency is imported before its dependents: errors
		// (strconv's ErrRange/ErrSyntax and io/fs's ErrInvalid/ErrClosed
		// initializers call errors.New), time (imported by io/fs), io/fs
		// (syscall's Errno.Is references its Err* sentinels), strconv
		// (syscall's Errno.Error calls strconv.Itoa), then syscall itself.
		// (The needStrconv/needErrors blocks above only ran if the user
		// imported those packages directly — not for a pure
		// `import "syscall"` program.)
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("time")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io/fs")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("strconv")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("syscall")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier-4: net): synthetic net package (Dial/Listen/TCP/UDP).
	// Import after its dependencies: errors, io, strconv, strings,
	// syscall, time (all handled by needX blocks above). The net package
	// drives the netpoller via linknames (no internal/poll import: its
	// FD struct contains sync.Mutex, rejected by the frontend M2 scope).
	if needNet {
		if err := e.importSyntheticPackage(getSyntheticPackage("net")); err != nil {
			return "", err
		}
	}
	// P3-M8b: synthetic os (synth_os.go). Dependency chain, imported in
	// order: errors (os error vars use errors.New), time (os imports time),
	// io (os imports io), io/fs (os imports io/fs), syscall (os imports
	// syscall for Errno), strconv (os uses strconv.Itoa), then os itself.
	// importSyntheticPackage is idempotent, so overlap with other
	// need-blocks is safe.
	if needOs {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("time")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io/fs")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("syscall")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("strconv")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("os")); err != nil {
			return "", err
		}
	}
	// P3-M9: synthetic flag (synth_flag.go). Dependency chain, imported
	// in order: errors, time, io, io/fs, syscall, strconv, os (the os
	// chain: os imports errors, time, io, io/fs, syscall, strconv),
	// reflect (imports errors, iter, strconv), sort, encoding, then
	// flag itself. importSyntheticPackage is idempotent, so overlap
	// with other need-blocks is safe.
	if needFlag {
		for _, p := range []string{"errors", "time", "io", "io/fs", "syscall", "strconv", "os", "iter", "reflect", "sort", "encoding", "flag"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M9: synthetic testing (synth_testing.go). Dependency chain:
	// the full flag chain above, plus bytes (indenter), context
	// (T.Context), then testing itself. sync/sync/atomic/runtime/fmt/
	// strings are frozen or M2/M4 primitives, always available.
	if needTesting {
		for _, p := range []string{"errors", "time", "io", "io/fs", "syscall", "strconv", "os", "iter", "reflect", "sort", "encoding", "flag", "bytes", "context", "testing"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M9: synthetic testing/quick (synth_testing_quick.go).
	// Dependency chain: the flag chain (quick registers -quickchecks),
	// reflect (value generation), then testing/quick itself.
	if needTestingQuick {
		for _, p := range []string{"errors", "time", "io", "io/fs", "syscall", "strconv", "os", "iter", "reflect", "sort", "encoding", "flag", "testing/quick"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M9: synthetic testing/iotest (synth_testing_iotest.go).
	// Dependency chain: errors, io, bytes, the os chain, then
	// testing/iotest itself.
	if needTestingIotest {
		for _, p := range []string{"errors", "time", "io", "io/fs", "syscall", "strconv", "os", "bytes", "testing/iotest"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M9 (tier-1d): leaf utilities unique/structs.
	if needUnique {
		if err := e.importSyntheticPackage(getSyntheticPackage("unique")); err != nil {
			return "", err
		}
	}
	if needStructs {
		if err := e.importSyntheticPackage(getSyntheticPackage("structs")); err != nil {
			return "", err
		}
	}
	// P3-M8 (wave 2): synthetic crypto/sha256 (synth_crypto_sha256.go).
	// Dependency chain, imported in order: errors (sha256's
	// package-level code calls errors.New); io (imported by hash);
	// hash (sha256's New/digest use hash.Hash and the __hash*
	// helpers); strconv (imported by crypto); crypto (sha256 calls
	// crypto.RegisterHash); then crypto/sha256 itself.
	if needCryptoSha256 {
		if err := e.importSyntheticPackage(getSyntheticPackage("errors")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("io")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("hash")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("strconv")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("crypto")); err != nil {
			return "", err
		}
		if err := e.importSyntheticPackage(getSyntheticPackage("crypto/sha256")); err != nil {
			return "", err
		}
	}
	// P3-M8b: the remaining synthetic crypto packages (synth_crypto*.go).
	// Each block imports its full dependency chain in topological order
	// (importSyntheticPackage is idempotent, so overlap is safe):
	// errors <- io <- hash <- strconv <- crypto <- subtle <- cipher <- aes;
	// md5/sha1/sha512 <- {crypto,errors,hash}; sha3 <- bytes too;
	// hmac <- {subtle,errors,hash}; rand <- io.
	if needCrypto {
		for _, p := range []string{"errors", "io", "hash", "strconv", "crypto"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoSubtle {
		if err := e.importSyntheticPackage(getSyntheticPackage("crypto/subtle")); err != nil {
			return "", err
		}
	}
	// P3-M9 (tier-5): asymmetric crypto. The public packages form a
	// dependency closure over the internal fips140/* packages; import
	// the full closure when any of them is needed (idempotent).
	if needCryptoElliptic || needCryptoEcdh || needCryptoDsa || needCryptoEcdsa || needCryptoEd25519 || needCryptoRsa || needCryptoHkdf || needCryptoPbkdf2 || needCryptoX509 || needCryptoTls || needCryptobyte || needCryptobyteAsn1 {
		for _, p := range []string{
			// P3-M9 (tier-5b): the synthetic sources' own imports, in
			// TOPOLOGICAL order — package-level var initializers (e.g.
			// rsa.ErrMessageTooLong = errors.New(...), bytes.ErrTooLarge)
			// are exported during importSyntheticPackage, so a package
			// must come after its deps; otherwise qualified calls fail
			// "call to unknown function".
			// (importSyntheticPackage is idempotent; fmt is overlay-
			// handled and needs no import; sync is a special overlay
			// package, not a synthetic source.)
			"errors",
			"time", // P3-M9: x509.Certificate uses time.Time (NotBefore/NotAfter); methods must be in methodIndex
			"math",
			"math/bits",
			"math/rand",
			"math/rand/v2",
			"strconv",
			"internal/byteorder",
			"crypto/subtle",
			"crypto/rand",
			"io",   // needs errors
			"hash", // needs io
			"bytes",
			"strings", // needs errors, io; math/big needs strings (NewReader)
			"crypto",
			// P3-M9 (tier-5 x509): the synthetic x509 source imports
			// crypto/sha1 and crypto/sha256 (hashFromSigAlg uses their
			// New funcs in value position, which needs funcByObj).
			"crypto/sha1",
			"crypto/sha256",
			"crypto/sha512",
			"crypto/cipher",
			"math/big",
			"crypto/internal/fips140",
			"crypto/internal/fips140/check",
			"crypto/internal/fips140/alias",
			"crypto/internal/fips140only",
			"crypto/internal/fips140cache",
			"crypto/internal/fips140hash",
			"crypto/internal/fips140/drbg",
			"crypto/internal/rand",
			"crypto/internal/boring/bbig",
			"crypto/internal/boring",
			"crypto/internal/constanttime",
			"crypto/internal/fips140deps/byteorder",
			"crypto/internal/fips140/nistec/fiat",
			"crypto/internal/fips140/nistec",
			"crypto/internal/fips140/edwards25519/field",
			"crypto/internal/fips140/edwards25519",
			"crypto/internal/fips140/bigmod",
			"crypto/internal/fips140/subtle",
			"crypto/internal/fips140/sha256",
			"crypto/internal/fips140/sha512",
			"crypto/internal/fips140/sha3",
			"crypto/internal/fips140/hmac",
			"crypto/internal/fips140/hkdf",
			"crypto/internal/fips140/pbkdf2",
			"crypto/internal/fips140/ecdsa",
			"crypto/internal/fips140/ed25519",
			"crypto/internal/fips140/rsa",
			"golang.org/x/crypto/cryptobyte",
			"golang.org/x/crypto/cryptobyte/asn1",
			"crypto/ecdh",
			"crypto/elliptic",
			"crypto/dsa",
			"crypto/ecdsa",
			"crypto/ed25519",
			"crypto/rsa",
			"crypto/hkdf",
			"crypto/pbkdf2",
			// P3-M9 (tier-5): synthetic net (x509/tls sources call net.Dialer
			// methods; methods must be in methodIndex before those packages).
			"net",
			// P3-M9 (tier-5): the synthetic tls source imports "os"
			// (LoadX509KeyPair reads cert/key files); os needs io/fs
			// (fs.ErrInvalid in ErrInvalid's initializer) and syscall.
			"io/fs",
			"syscall",
			"os",
			// P3-M9 (tier-5): synthetic crypto/x509 (Java delegation via
			// graalgo.x509.* intrinsics). pkix first (x509 imports it).
			"crypto/x509/pkix",
			"crypto/x509",
			// P3-M9 (tier-5): synthetic crypto/tls (SSLEngine delegation
			// via graalgo.tls.* intrinsics; imports crypto/x509).
			"crypto/tls",
		} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoCipher {
		// cipher also imports the synthetic bytes package (bytes.Clone).
		for _, p := range []string{"errors", "io", "bytes", "crypto/subtle", "crypto/cipher"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoAes {
		for _, p := range []string{"errors", "io", "bytes", "crypto/subtle", "crypto/cipher", "strconv", "crypto/aes"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoMd5 {
		for _, p := range []string{"errors", "io", "hash", "strconv", "crypto", "crypto/md5"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoSha1 {
		for _, p := range []string{"errors", "io", "hash", "strconv", "crypto", "crypto/sha1"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoSha3 {
		// sha3 also imports the synthetic bytes package (bytes.Clone).
		for _, p := range []string{"errors", "io", "hash", "strconv", "bytes", "crypto", "crypto/sha3"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoSha512 {
		for _, p := range []string{"errors", "io", "hash", "strconv", "crypto", "crypto/sha512"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoHmac {
		for _, p := range []string{"errors", "io", "hash", "crypto/subtle", "crypto/hmac"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needCryptoRand {
		// P3-M9: synthetic crypto/rand's __mrPrime calls big.Int methods
		// (SetBytes, ProbablyPrime), so math/big's full closure must be
		// imported here. Without it, method resolution fails with
		// "gofrontend: method SetBytes/ProbablyPrime not found on Int"
		// for any program importing crypto/rand (directly, or via
		// hash/maphash's MakeSeed entropy path).
		for _, p := range []string{"errors", "io", "bytes", "math", "strconv", "strings", "math/bits", "math/rand", "internal/byteorder", "math/big", "crypto/rand"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// P3-M9 (wave 3, tier-1b): synthetic container/* packages
	// (synth_container_*.go). heap imports the synthetic sort package,
	// imported by its own need-block above (importSyntheticPackage is
	// idempotent); list and ring are self-contained.
	if needContainerHeap {
		if err := e.importSyntheticPackage(getSyntheticPackage("container/heap")); err != nil {
			return "", err
		}
	}
	if needContainerList {
		if err := e.importSyntheticPackage(getSyntheticPackage("container/list")); err != nil {
			return "", err
		}
	}
	if needContainerRing {
		if err := e.importSyntheticPackage(getSyntheticPackage("container/ring")); err != nil {
			return "", err
		}
	}
	// P3-M9 (wave 3, tier-1b): synthetic hash/* packages
	// (synth_hash_*.go). They import the synthetic hash, errors and io
	// packages, imported by their own need-blocks above.
	if needHashAdler32 {
		for _, p := range []string{"errors", "hash", "hash/adler32"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needHashCrc32 {
		for _, p := range []string{"errors", "hash", "hash/crc32"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needHashCrc64 {
		for _, p := range []string{"errors", "hash", "hash/crc64"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	if needHashFnv {
		for _, p := range []string{"errors", "hash", "hash/fnv"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// hash/maphash additionally imports synthetic crypto/rand (MakeSeed
	// entropy) and math (Float64bits intrinsics). The needCryptoRand block
	// above also runs for maphash (the "hash/maphash" import case sets
	// needCryptoRand) and brings math/big's closure for __mrPrime; the
	// explicit list here stays self-sufficient regardless of block order.
	if needHashMaphash {
		for _, p := range []string{"errors", "hash", "crypto/rand", "math", "hash/maphash"} {
			if err := e.importSyntheticPackage(getSyntheticPackage(p)); err != nil {
				return "", err
			}
		}
	}
	// WS G (P3-M9, module-probe lane): third-party direct imports, after
	// the synthetic direct imports above. importPkgTree merges each
	// package's transitive closure dependencies-first; idempotent via
	// e.importedSynthetic, so overlap with the synthetic flow is safe.
	// Dedupe while preserving first-seen (source) order for determinism.
	for _, p := range needDisk {
		if err := e.importPkgTree(p); err != nil {
			return "", err
		}
	}
	// P3-M3 (WS D, ADR-0003): the "methods" lists on named/pointer entries
	// are computed from funcByObj, which is only populated now. Any type
	// described before this point (the GenDecl TYPE scan above,
	// collectPackageVars) computed its methods against an empty funcByObj
	// and would otherwise keep a permanently empty list (named entries
	// memoize by qualified name, so it is never recomputed). Recompute
	// every named/pointer entry described so far; entries described later
	// (during body export) already compute against the populated map.
	if err := e.recomputeMethodTables(); err != nil {
		return "", err
	}
	insts, err := e.enumerateInstantiations(files, funcs)
	if err != nil {
		return "", err
	}
	// P3-M9: instByKey may already hold on-demand instantiations created
	// during synthetic import (generic __-helper desugars); preserve
	// them — enumerateInstantiations seeded its dedup set from instByKey
	// so insts carries no duplicates.
	if e.instByKey == nil {
		e.instByKey = make(map[string]*instantiation, len(insts))
	}
	for _, inst := range insts {
		e.instByKey[inst.key] = inst
	}
	// Assign instantiation object ids before any body is emitted: call
	// sites resolve to these ids during body export.
	for _, inst := range insts {
		if err := e.prepareInstantiation(inst); err != nil {
			return "", err
		}
	}
	// P3-M9 (instantiation method tables): method-table root seeding ran
	// inside enumerateInstantiations, so instByKey now holds every
	// template-method monomorphization for the types described so far.
	// Arm on-demand monomorphization for types described later (during
	// body export) and rebuild the method tables: template-method
	// entries now resolve to instantiation object ids instead of being
	// skipped.
	e.mtOnDemand = true
	if err := e.recomputeMethodTables(); err != nil {
		return "", err
	}
	// BUG-MODPROBE-010: register pair types for user-defined multi-result
	// functions BEFORE package-level var initializers are exported.
	// Initializers like `var x = Must(Parse(...))` hoist the inner
	// multi-result call via classifyMultiResultCall, which consults
	// userMRFuncs — previously populated only inside exportFuncDecl,
	// which runs after exportPackageVars, so the call failed fast.
	if err := e.registerUserMRPairs(funcs); err != nil {
		return "", err
	}
	// M7: package-level vars — dependency-order initialization. This runs
	// after instantiation enumeration so initializers can call
	// package-level functions (funcByObj is populated) and generic
	// instantiations inside initializers are already enqueued.
	globals, err := e.exportPackageVars(funcs)
	if err != nil {
		return "", err
	}
	// NOTE: doc.Globals is assembled after all exports below (see below).
	hasMain := false
	for _, fi := range funcs {
		if fi.isTemplate {
			continue
		}
		f, err := e.exportFuncDecl(fi)
		if err != nil {
			return "", err
		}
		doc.Funcs = append(doc.Funcs, f)
		if fi.fd.Name.Name == "main" && fi.recv == nil {
			hasMain = true
		}
	}
	for _, inst := range insts {
		f, err := e.exportInstantiation(inst)
		if err != nil {
			return "", err
		}
		doc.Funcs = append(doc.Funcs, f)
	}
	// P3-M5 (WS E, ADR-0005): emit the synthetic time/context funcdecls
	// after the user's (call sites resolve by object id, so order is
	// irrelevant; appending keeps user code first for readability).
	if err := e.exportSyntheticBodies(&doc); err != nil {
		return "", err
	}
	// P3-M9 (instantiation method tables): emit the bodies of on-demand
	// monomorphizations queued while types were described during body
	// export, then rebuild the method tables a final time so late
	// types carry resolved entries.
	if err := e.drainMethodTablePending(&doc); err != nil {
		return "", err
	}
	if err := e.recomputeMethodTables(); err != nil {
		return "", err
	}
	if !hasMain {
		return "", scopeErrorf("no func main found (frontend/v1 scope: single-file program with func main)")
	}
	// P3-M5 (WS E, ADR-0005): synthetic package globals first — user
	// initializers may reference them (e.g. var e error =
	// context.Canceled); synthetic initializers never reference user
	// vars. Each synthetic package's globals are dependency-ordered at
	// import time (importSyntheticPackage), like user package vars.
	// Assembled HERE (after all exports) and non-aliasing: synthetic
	// packages can be imported lazily during body export, appending to
	// e.synthGlobals; the old append(e.synthGlobals, globals...) shared
	// the backing array, so late appends clobbered the user's globals
	// in doc.Globals (frontend bug: "ident: reference to unknown
	// object id 0" for the first package-level var).
	// Only prepend when non-empty to preserve the "globals": [] wire
	// shape for programs that don't import time/context.
	if len(e.synthGlobals) > 0 {
		doc.Globals = make([]GlobalEnt, 0, len(e.synthGlobals)+len(globals))
		doc.Globals = append(doc.Globals, e.synthGlobals...)
		doc.Globals = append(doc.Globals, globals...)
	} else {
		doc.Globals = globals
	}
	doc.Types = e.types
	doc.Objects = e.objects
	if doc.Objects == nil {
		doc.Objects = []ObjectEnt{}
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("gofrontend: JSON encoding failed: %v", err)
	}
	return string(out), nil
}

func scopeErrorf(format string, args ...any) error {
	return fmt.Errorf("gofrontend: "+format, args...)
}

// checkVariadicCall is deprecated: non-spread variadic calls are now
// handled via packVariadicArgs (P3-M9). Kept for backward compatibility
// with tests that assert the old scope error; new code should use
// exportCallArgsPacked instead.
func checkVariadicCall(c *ast.CallExpr, sig *types.Signature) error {
	if sig != nil && sig.Variadic() && !c.Ellipsis.IsValid() {
		return scopeErrorf("call to variadic function without ... spread" +
			" (argument packing) is not supported in frontend/v1 scope (WS D)")
	}
	return nil
}

// packVariadicArgs packs trailing arguments into a slice literal for a
// non-spread variadic call (P3-M9, WS D).
//
// For func f(a int, b ...string) called as f(1, "x", "y"):
// - sig.Params() = [a int, b []string] (variadic param is []string in go/types)
// - args = [1, "x", "y"] (already-exported nodes)
// - Returns [1, []string{"x", "y"}] where the slice is a complit node.
//
// The spread form f(s...) passes the slice directly and needs no packing.
// (The fmt overlay handles its own variadic builtins and is not routed
// through this function.)
func (e *exporter) packVariadicArgs(c *ast.CallExpr, sig *types.Signature, args []*Node) ([]*Node, error) {
	if sig == nil || !sig.Variadic() || c.Ellipsis.IsValid() {
		return args, nil
	}
	n := sig.Params().Len()
	if n == 0 {
		return nil, fmt.Errorf("gofrontend: variadic signature with no params (exporter bug)")
	}
	// The last param is the []T slice type in go/types.
	sliceType := sig.Params().At(n - 1).Type()
	sliceTypeID, err := e.typeID(sliceType)
	if err != nil {
		return nil, err
	}
	// Fixed args are the first n-1; the rest are variadic and get packed.
	numFixed := n - 1
	if len(args) < numFixed {
		return nil, fmt.Errorf("gofrontend: not enough arguments for variadic call (exporter bug)")
	}
	variadicNodes := args[numFixed:]
	// Create a composite literal []T{...} with the variadic args as values.
	// The "values" field holds []*Node which serializes to JSON array.
	packNode := e.node("complit", c, map[string]any{
		"type":   sliceTypeID,
		"keyed":  false,
		"values": variadicNodes,
	})
	packed := make([]*Node, 0, numFixed+1)
	packed = append(packed, args[:numFixed]...)
	packed = append(packed, packNode)
	return packed, nil
}

// exportCallArgsPacked exports call arguments, packing trailing args into
// a slice literal for non-spread variadic calls (P3-M9, WS D).
// If sig is nil or not variadic (or the call uses ... spread), this is
// equivalent to exportCallArgs.
func (e *exporter) exportCallArgsPacked(c *ast.CallExpr, sig *types.Signature) ([]*Node, error) {
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	return e.packVariadicArgs(c, sig, args)
}

// ---------------------------------------------------------------------------
// Package-level variables (M7).
// ---------------------------------------------------------------------------

// collectPackageVars records one package-level `var` GenDecl. Variables are
// defined in the object table now (so a later initializer can reference an
// earlier variable); initializer export and dependency ordering happen in
// exportPackageVars after the full declaration scan.
func (e *exporter) collectPackageVars(gd *ast.GenDecl) error {
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return scopeErrorf("package-level var spec %T is not supported (M7)", spec)
		}
		pv := &pkgVarDecl{spec: vs}
		for _, n := range vs.Names {
			if n.Name == "_" {
				continue
			}
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return fmt.Errorf("gofrontend: package var %q has no *types.Var object", n.Name)
			}
			oid, err := e.defVar(v, false)
			if err != nil {
				return err
			}
			pv.vars = append(pv.vars, v)
			pv.oids = append(pv.oids, oid)
		}
		e.pkgVars = append(e.pkgVars, pv)
	}
	return nil
}

// exportPackageVars exports initializers for the collected package-level
// vars and returns the document's Globals in initialization (dependency)
// order.
//
// Go spec (Package initialization): "Given the list of all variables, the
// initialization order is determined by analyzing dependencies" — x depends
// on y when x's initializer refers to y (only references to package-level
// variables are ordering constraints, and functions need no initialization).
// The dependency walk covers the whole initializer expression, including
// funclit bodies (ast.Inspect does not skip them) — matching gc, which
// rejects `var f = func() int { return g }; var g = f` as an initialization
// cycle. Unconstrained variables initialize in declaration order. A
// dependency cycle is a build error, matching gc's "initialization cycle"
// rejection.
// ginit is one package-level variable initializer: the object-table id
// (or -1 for a blank `var _ = ...`), the exported initializer node (nil
// when absent — zero value), the source expression for dependency
// analysis (nil when absent), and the declaration position.
type ginit struct {
	oid  int
	init *Node
	expr ast.Expr
	pos  *Pos
	// BUG-MODPROBE-010: temp globals hoisted from multi-result calls in
	// argument position within this initializer's expression
	// (var x = f(g())). Emitted immediately before this entry, in order;
	// the temps are uniquely named and unobservable elsewhere, so they
	// need no dependency analysis.
	pre []ginit
}

// varDepAnalyzer finds the package-level variables an initializer
// depends on: variables referenced directly in the expression, plus
// variables referenced (transitively) in the bodies of package-level
// functions the initializer calls. This mirrors gc's initialization
// dependency analysis (Go spec, Package initialization: "Dependency
// analysis is performed per package; only references referring to
// variables, functions, and (non-interface) methods declared in the
// current package are considered") — e.g. `var a = f()` where f reads
// package var b makes a depend on b, even when b is declared later.
type varDepAnalyzer struct {
	info        *types.Info
	objIDs      map[types.Object]int
	pkgVar      map[int]bool
	depFuncs    map[*types.Func]*ast.FuncDecl
	seenVar     map[int]bool
	visitedFunc map[*types.Func]bool
	out         []int
}

func (a *varDepAnalyzer) scan(n ast.Node) {
	ast.Inspect(n, func(m ast.Node) bool {
		id, ok := m.(*ast.Ident)
		if !ok {
			return true
		}
		switch obj := a.info.Uses[id].(type) {
		case *types.Var:
			if oid, ok := a.objIDs[obj]; ok && a.pkgVar[oid] && !a.seenVar[oid] {
				a.seenVar[oid] = true
				a.out = append(a.out, oid)
			}
		case *types.Func:
			a.scanFunc(obj)
		}
		return true
	})
}

func (a *varDepAnalyzer) scanFunc(fn *types.Func) {
	if a.visitedFunc[fn] {
		return
	}
	fd, ok := a.depFuncs[fn]
	if !ok {
		// Not a package-level function with a body here (builtin,
		// intrinsic, or another package's function): it cannot
		// reference this package's variables.
		return
	}
	a.visitedFunc[fn] = true
	if fd.Body != nil {
		a.scan(fd.Body)
	}
}

// orderVarInits topologically sorts package-level variable initializers
// so each runs after the package-level variables its source expression
// refers to (Go spec, Package initialization). The order is stable by
// declaration order (matching gc's source-order tie-breaking). info must
// be the type-check info of the package the expressions belong to;
// objIDs maps *types.Var to object-table ids; objName names an object id
// for cycle errors; depFuncs maps the package's functions to their
// bodies for transitive dependency analysis (nil disables it). A
// dependency cycle fails fast with a named error.
func orderVarInits(inits []ginit, info *types.Info, objIDs map[types.Object]int, objName func(int) string, depFuncs map[*types.Func]*ast.FuncDecl) ([]ginit, error) {
	// Dependency analysis: the package-level variables each initializer
	// refers to, directly or through called package-level functions
	// (selectors and calls are covered because the referenced variable
	// appears as an *ast.Ident; method calls via the Sel ident).
	pkgVar := map[int]bool{}
	for _, g := range inits {
		if g.oid >= 0 {
			pkgVar[g.oid] = true
		}
	}
	deps := make([][]int, len(inits))
	for i, g := range inits {
		if g.expr == nil {
			continue
		}
		da := &varDepAnalyzer{
			info: info, objIDs: objIDs, pkgVar: pkgVar, depFuncs: depFuncs,
			seenVar: map[int]bool{}, visitedFunc: map[*types.Func]bool{},
		}
		da.scan(g.expr)
		deps[i] = da.out
	}

	// Topological order, stable by declaration order: at each step the
	// earliest-declared variable whose dependencies are all initialized
	// goes next. A self-reference is a cycle, like any other.
	oidx := map[int]int{}
	for i, g := range inits {
		if g.oid >= 0 {
			oidx[g.oid] = i
		}
	}
	indeg := make([]int, len(inits))
	dependents := make([][]int, len(inits))
	for i, ds := range deps {
		for _, d := range ds {
			j := oidx[d]
			dependents[j] = append(dependents[j], i)
			indeg[i]++
		}
	}
	order := make([]int, 0, len(inits))
	done := make([]bool, len(inits))
	for len(order) < len(inits) {
		next := -1
		for i := range inits {
			if !done[i] && indeg[i] == 0 {
				next = i
				break
			}
		}
		if next < 0 {
			break // cycle; reported below
		}
		done[next] = true
		order = append(order, next)
		for _, k := range dependents[next] {
			indeg[k]--
		}
	}
	if len(order) < len(inits) {
		cycle := []string{}
		for i := range inits {
			if indeg[i] > 0 && inits[i].oid >= 0 {
				cycle = append(cycle, objName(inits[i].oid))
			}
		}
		return nil, scopeErrorf("initialization cycle involving package-level var(s): %s",
			strings.Join(cycle, ", "))
	}

	out := make([]ginit, 0, len(inits))
	for _, i := range order {
		out = append(out, inits[i])
	}
	return out, nil
}

func (e *exporter) exportPackageVars(funcs []*funcDeclInfo) ([]GlobalEnt, error) {
	inits := []ginit{}
	for _, pv := range e.pkgVars {
		vs := pv.spec
		specPos := e.pos(vs)
		if len(vs.Values) != 0 && len(vs.Values) != len(vs.Names) {
			// Multi-valued initializers (`var a, b = f()`) are WS D.
			return nil, scopeErrorf(
				"package-level var with multi-valued initializer is not supported in v1 (WS D)")
		}
		for i, n := range vs.Names {
			var initNode *Node
			var initExpr ast.Expr
			// BUG-MODPROBE-010: temp globals hoisted while exporting this
			// name's initializer (drained from e.mrHoistPre below).
			var hoistPre []ginit
			// P3-M5 (WS E, ADR-0005 D1): package-level `var t time.Timer`
			// / `var t time.Ticker` (with or without init) is not in M5
			// scope (mirrors the local-var path).
			if v, ok := e.info.Defs[n].(*types.Var); ok {
				if nt := timeTimerTickerNamedExact(v.Type()); nt != nil {
					return nil, timerScopeErrorf(nt, "var declaration")
				}
			}
			if i < len(vs.Values) {
				initExpr = vs.Values[i]
				preBase := len(e.mrHoistPre)
				x, err := e.exportExpr(initExpr)
				if err != nil {
					return nil, err
				}
				// BUG-MODPROBE-010: multi-result calls in argument position
				// at package level (var x = f(g())) hoist the inner call to
				// a synthesized package-level temp global. The
				// statement-level flush (exportStmt) has no package-init
				// equivalent, so hoisted temps are drained here and emitted
				// immediately before this variable's initializer (Go
				// evaluation order within one initializer is preserved).
				for _, pn := range e.mrHoistPre[preBase:] {
					pg, perr := packageHoistTemp(pn)
					if perr != nil {
						return nil, perr
					}
					hoistPre = append(hoistPre, pg)
				}
				e.mrHoistPre = e.mrHoistPre[:preBase]
				initNode = x
			} else {
				// P3-M2b (ADR-0002 D3): package-level `var m sync.Mutex`
				// (no init) normalizes to an implicit syncnew — gc's
				// ready-to-use zero value — mirroring the local-var path.
				// Without it the frozen factory's struct zero (GoStruct)
				// would be installed and every method call would CCE.
				if v, ok := e.info.Defs[n].(*types.Var); ok {
					if nt := syntheticPrimitiveNamed(v.Type()); nt != nil {
						tid, err := e.typeID(v.Type())
						if err != nil {
							return nil, err
						}
						initNode = e.node("syncnew", vs, map[string]any{
							"synctyp": syntheticOpTypeName(nt), "type": tid,
						})
					}
				}
			}
			if n.Name == "_" {
				// Blank: no variable, but the initializer is evaluated
				// for side effects (Go spec, Package initialization).
				if initNode != nil {
					inits = append(inits, ginit{oid: -1, init: initNode, expr: initExpr, pos: specPos, pre: hoistPre})
				}
				continue
			}
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: package var %q has no *types.Var object", n.Name)
			}
			oid, ok := e.objIDs[v]
			if !ok {
				return nil, fmt.Errorf("gofrontend: package var %q missing from object table", n.Name)
			}
			inits = append(inits, ginit{oid: oid, init: initNode, expr: initExpr, pos: specPos, pre: hoistPre})
		}
	}

	// M7: dependency-order initialization (Go spec, Package
	// initialization), stable by declaration order. Dependencies are
	// followed through called package-level functions (transitive).
	depFuncs := make(map[*types.Func]*ast.FuncDecl, len(funcs))
	for _, fi := range funcs {
		depFuncs[fi.obj] = fi.fd
	}
	ordered, err := orderVarInits(inits, e.info, e.objIDs, func(oid int) string {
		return e.objects[oid].Name
	}, depFuncs)
	if err != nil {
		return nil, err
	}

	out := make([]GlobalEnt, 0, len(ordered))
	for _, g := range ordered {
		// BUG-MODPROBE-010: hoisted temps evaluate immediately before
		// their consuming initializer (Go evaluation order within one
		// initializer); the temps are uniquely named and unobservable
		// elsewhere, so they ride along instead of being sorted.
		for _, p := range g.pre {
			out = append(out, GlobalEnt{Obj: p.oid, Init: p.init, Pos: p.pos})
		}
		out = append(out, GlobalEnt{Obj: g.oid, Init: g.init, Pos: g.pos})
	}
	// Go spec (Package initialization): after the package's variables
	// are initialized, its init() functions run in declaration order.
	// They are emitted as side-effect-only globals entries (Obj == -1)
	// carrying a void call node — the factory evaluates them in
	// sequence before main, like blank-var initializers.
	for _, fi := range funcs {
		if fi.fd.Recv == nil && fi.obj.Name() == "init" {
			ent, err := e.initCallEnt(fi)
			if err != nil {
				return nil, err
			}
			out = append(out, ent)
		}
	}
	return out, nil
}

// initCallEnt builds a globals-sequence entry that invokes a
// package-level init() function for its side effects. The call node
// mirrors exportFuncCall's direct-call shape (ident fun + no args +
// nil result type for the void signature).
func (e *exporter) initCallEnt(fi *funcDeclInfo) (GlobalEnt, error) {
	sigID, err := e.typeID(fi.obj.Type())
	if err != nil {
		return GlobalEnt{}, err
	}
	fun := e.node("ident", fi.fd.Name, map[string]any{
		"name": fi.obj.Name(), "obj": fi.funcID, "type": sigID,
	})
	call := e.node("call", fi.fd, map[string]any{
		"fun": fun, "args": []*Node{}, "type": nil,
	})
	return GlobalEnt{Obj: -1, Init: call, Pos: e.pos(fi.fd)}, nil
}

// ---------------------------------------------------------------------------
// Exporter state.
// ---------------------------------------------------------------------------

type exporter struct {
	fset *token.FileSet
	src  []byte // raw source bytes of the single input file (M8: rune-based positions)
	// fileIndex maps each input file's *token.File to its index in the
	// files order; srcByFile maps it to that file's source bytes.
	// Multi-file packages need per-file bytes for e.pos() rune columns.
	fileIndex map[*token.File]int
	srcByFile map[*token.File][]byte
	info      *types.Info
	pkg       *types.Package // the type-checked package (P3-M3: method-set lookup visibility)
	types     []TypeEnt
	typeList  []types.Type // parallel to types: the go/types type per id (P3-M3)
	typeIDs   map[types.Type]int
	// P3-M9 (WS H): canonical IDs for type parameters by TypeName.
	// Different *types.TypeParam objects denoting the same parameter
	// must share one wire ID, or F-bounded constraints recurse forever.
	typeparamIDs map[*types.TypeName]int
	objects      []ObjectEnt
	objIDs       map[types.Object]int
	// P3-M9: synthetic package vars by (package path, var name). Synthetic
	// packages are type-checked in isolation, so *types.Var pointers aren't
	// shared across packages. When resolving a cross-package var reference
	// (e.g., binary.LittleEndian from gzip), fall back to this map if the
	// pointer lookup misses.
	synthVarIDs map[string]int
	// P3-M9: synthetic package funcs by (package path, func name). Same
	// isolation issue as vars; used when resolving generic instantiations
	// from importing packages (e.g., slices.SortFunc from flate).
	synthFuncIDs map[string]*funcDeclInfo
	sizes        types.Sizes
	// M6 (WS H): active type-parameter substitution while exporting a
	// monomorphized instantiation body (nil outside instantiations). See
	// typeSubst below.
	subst *typeSubst
	// M6 (WS H): per-instantiation type memo, keyed by unsubstituted type.
	// The global typeIDs memo is only valid without substitution.
	substTypeIDs map[types.Type]int
	// P3-M9 (generics): canonicalization cache for the fresh go/types
	// objects substType mints. types.Instantiate/NewPointer/NewSlice/...
	// create a NEW object per call, so two substitutions of the same
	// logical type yield different objects — defeating the
	// object-identity memos (substTypeIDs) and causing infinite regress:
	// describing *Foo[int] -> method table -> on-demand instantiation
	// -> receiver typeID -> substType mints a fresh *Foo[int] -> memo
	// miss -> describe again, forever (issue48344, issue54535). The
	// cache makes substType return the identical object for structurally
	// identical results. Keyed by substCanonKey (type params by object
	// identity, so distinct parameters never conflate).
	substCanon map[string]types.Type
	// M6 (WS H, issue52026): named types deduplicated by qualified name
	// (including substituted type arguments). A substituted *types.Named
	// (e.g. Some[T] under T=int) is a fresh go/types object, but it denotes
	// the same type as the Some[int] from non-generic code — they must share
	// one wire type id, or type assertions on monomorphized values fail.
	namedIDs map[string]int
	// P3-M8-leftovers: anonymous *types.Interface deduplication by
	// structural key (sorted method implkeys). go/types mints a fresh
	// *types.Interface per syntactic occurrence, so without this the
	// type assertion and the method dispatch on an anonymous interface
	// get different wire ids → wrong itab → wrong dispatch results.
	ifaceIDs map[string]int
	// M6 (WS H): top-level function/method declarations by object, and
	// monomorphized instantiations by dedup key (see enumerateInstantiations).
	funcByObj map[*types.Func]*funcDeclInfo
	// M6 (WS H): method declarations by (receiver type name, method name).
	methodIndex map[methodKey]*funcDeclInfo
	instByKey   map[string]*instantiation
	// P3-M9 (instantiation method tables): on-demand monomorphizations
	// created for template methods of concrete generic instantiations
	// whose types are described after instantiation enumeration (during
	// body export). They are prepared immediately (so the method-table
	// entry under construction and any later body-export lookup see a
	// valid instID) and queued here for body emission by
	// drainMethodTablePending after all bodies are exported.
	mtPending []*instantiation
	// P3-M9 (instantiation method tables): when true, methodTableEntries
	// monomorphizes template methods of concrete generic instantiations
	// instead of skipping them. Armed after instByKey is populated (and
	// the method-table root seeding inside enumerateInstantiations has
	// run); before that the old skip behavior applies.
	mtOnDemand bool
	// M7: package-level var declarations in source order (collected by
	// collectPackageVars during the declaration scan; exported in
	// dependency order by exportPackageVars after the scan).
	pkgVars []*pkgVarDecl
	// P3-M8: synthetic packages already imported (importSyntheticPackage
	// is idempotent; prevents duplicate globals when a dependency like
	// errors is imported both directly and via io).
	importedSynthetic map[string]bool
	// WS G (P3-M9, module-probe lane): import paths already walked by
	// importPkgTree (synthetic, stub, or disk). Separate from
	// importedSynthetic: stubs (runtime/fmt/sync/sync-atomic/unsafe)
	// have no syntheticPackage, and enumerateInstantiations assumes
	// every importedSynthetic key resolves via getSyntheticPackage.
	importedPkgTree map[string]bool
	// P3-M2remed (P0-3): sequence counter for synthetic temporaries
	// hoisted by exportSyncGoStmt (__sync_go_arg<seq>_<i>); keeps the
	// names unique across go statements in one export.
	syncGoSeq int
	// P3-M5 (WS E, ADR-0005 App C): sequence counter for synthetic
	// temporaries created by the channel-ops lowering (__rng_*,
	// __select_blank_*, __close_*); keeps the names unique across
	// selects/ranges/closes in one export.
	chanSeq int
	// P3-M5 (WS E, ADR-0005): synthetic time/context package state.
	// synthGlobals holds the synthetic packages' package-level globals
	// in declaration order (exported during importSyntheticPackage;
	// prepended to doc.Globals so user initializers referencing e.g.
	// context.Canceled see initialized values). synthGroups holds the
	// imported funcdecls per synthetic package; their bodies are emitted
	// by exportSyntheticBodies after the user's.
	synthGlobals []GlobalEnt
	synthGroups  []synthGroup
	// P3-M9 (WS D): user-defined multi-result functions transformed to
	// pair-returning form. Maps *types.Func -> pair info for call-site
	// desugar. Unlike the synthetic table-driven mechanism, this is
	// automatic: any user-defined 2+ result function gets a synthesized
	// pair struct.
	userMRFuncs map[*types.Func]*userMRInfo
	// P3-M9 (driver-harden): name-based fallback for synthetic MR funcs.
	// Synthetic packages are type-checked separately from their callers,
	// P3-M9 (WS D): the userMRInfo for the function whose body is
	// currently being exported (nil outside such functions). Used to
	// transform `return a, b` into a pair composite literal.
	currentMR *userMRInfo
	// BUG-MODPROBE-010: side channel for temp vardecls hoisted from
	// multi-result calls in argument position (f(g()) desugar). exportCallArgs
	// appends the temp vardecls here; exportStmt flushes them by wrapping the
	// statement in a block. Each exportStmt only flushes preStmts added during
	// its own execution (tracked via length), so nested statements flush
	// correctly.
	mrHoistPre []*Node
}

// userMRInfo describes a user-defined multi-result function transformed
// to pair-returning form (P3-M9 WS D).
type userMRInfo struct {
	pairType   *types.Named     // synthesized pair struct (fields f0, f1, ...)
	pairFields []string         // field names in result order
	origSig    *types.Signature // original multi-result signature
}

// M10 (P1-4): struct-nesting depth cap for offset computation.
// Measured: 10-deep ~165us, 20-deep ~810ms (2^k blowup in go/types'
// unmemoized gcSizes). 16 is safely below the pain point; deeper structs
// get -1 offsets (position-indexed, never byte-offset, per the M6 comment
// below).
const maxStructDepth = 16

// structValueDepth estimates the value-nesting depth of t without calling
// Sizeof (which is exponential on deep nesting). Structs add one level per
// nesting; arrays add one; other composites are O(1). Cycle-safe via the
// depth cap: returns maxStructDepth+1 if the cap is exceeded.
func structValueDepth(t types.Type, depth int) int {
	if depth > maxStructDepth {
		return depth
	}
	switch t := t.(type) {
	case *types.Named:
		return structValueDepth(t.Underlying(), depth)
	case *types.Struct:
		max := depth
		for i := 0; i < t.NumFields(); i++ {
			if d := structValueDepth(t.Field(i).Type(), depth+1); d > max {
				max = d
			}
			if max > maxStructDepth {
				return max
			}
		}
		return max
	case *types.Array:
		return structValueDepth(t.Elem(), depth+1)
	default:
		return depth
	}
}

// pkgVarDecl is one package-level `var` spec (M7).
type pkgVarDecl struct {
	spec *ast.ValueSpec
	vars []*types.Var // non-blank declared variables, in source order
	oids []int        // object ids parallel to vars
	// blankInit is set when the spec declares only blank identifiers:
	// the values are still evaluated for side effects (Go spec).
	blankInit []ast.Expr
}

// ---------------------------------------------------------------------------
// Type-parameter substitution (M6, WS H).
// ---------------------------------------------------------------------------
//
// Monomorphization (PLAN.md D2) emits one function-body subtree per
// go/types instantiation. A typeSubst binds a template's type parameters to
// their actual arguments; the chain links nested instantiations (a generic
// function instantiated from inside another generic function's body). Bind
// values are written in the PARENT scope (they come from info.Instances
// recorded inside the parent's body), so resolve() returns both the bound
// type and the scope to interpret it in.
//
// Termination: every instantiation the enumerator produces is fully
// concrete — go/types rejects instantiation cycles ("instantiation cycle"
// is a type error, verified against gc 1.27.1), so the fixpoint below always
// terminates for accepted programs.

type typeSubst struct {
	parent *typeSubst
	bind   map[*types.TypeParam]types.Type
}

// resolve returns the type bound to tp and the scope it lives in
// (nil scope = top level, no substitution active).
func (s *typeSubst) resolve(tp *types.TypeParam) (types.Type, *typeSubst) {
	for ss := s; ss != nil; ss = ss.parent {
		if c, ok := ss.bind[tp]; ok {
			return c, ss.parent
		}
	}
	return nil, nil
}

// resolveByName resolves a type parameter by name through the subst chain.
// P3-M9 (generics isNaN fix): go/types TypeParam object identity is not
// stable across the synthetic package's type-checking passes, so the nested
// call's T (from e.info.Instances) may not pointer-match the T in subst.bind
// (from fi.sig.TypeParams()). Match by name instead.
func resolveByName(s *typeSubst, name string) types.Type {
	for ss := s; ss != nil; ss = ss.parent {
		for tp, c := range ss.bind {
			if tp.Obj().Name() == name {
				return c
			}
		}
	}
	return nil
}

// bindParams builds the bind map pairing a type-parameter list with its
// actual arguments.
func bindParams(tparams *types.TypeParamList, targs []types.Type) map[*types.TypeParam]types.Type {
	bind := make(map[*types.TypeParam]types.Type, len(targs))
	for i := 0; i < tparams.Len() && i < len(targs); i++ {
		bind[tparams.At(i)] = targs[i]
	}
	return bind
}

// substString renders t as a canonical structural string with type
// parameters resolved through s. It is the dedup key for instantiations
// (go/types object identity is not stable across use sites) and the basis
// for instantiation display names. Two structurally identical types render
// identically, so identical instantiations discovered via different paths
// deduplicate.
func substString(t types.Type, s *typeSubst) string {
	var sb strings.Builder
	writeSubstString(&sb, t, s)
	return sb.String()
}

func writeSubstString(sb *strings.Builder, t types.Type, s *typeSubst) {
	switch t := t.(type) {
	case *types.Alias:
		// P3-M8b: unwrap aliases so os.FileMode and fs.FileMode produce
		// identical keys (alias identity is not type identity).
		writeSubstString(sb, types.Unalias(t), s)
		return
	case *types.TypeParam:
		if s != nil {
			if c, scope := s.resolve(t); c != nil {
				writeSubstString(sb, c, scope)
				return
			}
		}
		// Unbound under substitution: defensive marker. Instantiation keys
		// containing it are rejected by the enumerator (isConcreteKey).
		sb.WriteString("!typeparam:" + t.Obj().Name() + "!")
	case *types.Named:
		sb.WriteString("named:" + qualifiedName(t.Obj()) + "[")
		args := t.TypeArgs()
		for i := 0; i < args.Len(); i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			writeSubstString(sb, args.At(i), s)
		}
		sb.WriteString("]")
	case *types.Pointer:
		sb.WriteString("ptr:")
		writeSubstString(sb, t.Elem(), s)
	case *types.Slice:
		sb.WriteString("slice:")
		writeSubstString(sb, t.Elem(), s)
	case *types.Array:
		sb.WriteString("array:")
		sb.WriteString(fmt.Sprint(t.Len()))
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Map:
		sb.WriteString("map:")
		writeSubstString(sb, t.Key(), s)
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Chan:
		sb.WriteString("chan:")
		sb.WriteString(fmt.Sprint(t.Dir()))
		sb.WriteString(":")
		writeSubstString(sb, t.Elem(), s)
	case *types.Signature:
		sb.WriteString("sig:(")
		writeSubstTuple(sb, t.Params(), s)
		sb.WriteString("):(")
		writeSubstTuple(sb, t.Results(), s)
		sb.WriteString(")")
		if t.Variadic() {
			sb.WriteString(":variadic")
		}
	case *types.Basic:
		sb.WriteString("basic:" + t.Name())
	default:
		// Interfaces, unions, aliases: go/types rendering is deterministic
		// and these never carry substitutable type params in v1 scope.
		sb.WriteString("other:" + t.String())
	}
}

func writeSubstTuple(sb *strings.Builder, tup *types.Tuple, s *typeSubst) {
	for i := 0; i < tup.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		writeSubstString(sb, tup.At(i).Type(), s)
	}
}

// isConcreteKey reports whether a dedup key resolved fully (no unbound
// type parameter markers).
func isConcreteKey(key string) bool {
	return !strings.Contains(key, "!typeparam:")
}

// joinSubstKeys renders type args for keys and display names.
func joinSubstKeys(targs []types.Type, s *typeSubst) string {
	parts := make([]string, len(targs))
	for i, ta := range targs {
		parts[i] = substString(ta, s)
	}
	return strings.Join(parts, ",")
}

func (e *exporter) pos(n ast.Node) *Pos {
	s := e.fset.Position(n.Pos())
	t := e.fset.Position(n.End())
	col, length := s.Column, t.Offset-s.Offset
	// Multi-file: the position's file index and that file's source bytes.
	// Falls back to e.src (single-file or synthetic-package swap) when the
	// maps are nil or the file isn't an input file.
	fileIdx := 0
	src := e.src
	if tf := e.fset.File(n.Pos()); tf != nil {
		if idx, ok := e.fileIndex[tf]; ok {
			fileIdx = idx
		}
		if b, ok := e.srcByFile[tf]; ok {
			src = b
		}
	}
	// M8: Truffle Source.createSection(line, col, len) is char-based, but
	// go/token positions are byte-based. For non-ASCII sources the byte
	// length can exceed the char length, and createSection throws
	// IllegalArgumentException ("charIndex out of range") — a hard crash on
	// valid programs (found by the M8 fuzzer: fmt.Println("日本語")).
	// Emit rune-based col/len; off stays a byte offset (NodeFactory ignores it).
	if src != nil && s.IsValid() && t.IsValid() {
		if f := e.fset.File(n.Pos()); f != nil && s.Offset <= t.Offset && t.Offset <= len(src) {
			if lo := f.Offset(f.LineStart(s.Line)); lo <= s.Offset {
				col = utf8.RuneCount(src[lo:s.Offset]) + 1
			}
			length = utf8.RuneCount(src[s.Offset:t.Offset])
		}
	}
	return &Pos{File: fileIdx, Line: s.Line, Col: col, Off: s.Offset, Len: length}
}

func (e *exporter) node(kind string, n ast.Node, fields map[string]any) *Node {
	return &Node{Kind: kind, Pos: e.pos(n), Fields: fields}
}

// defVar records a *types.Var in the object table (idempotent) and returns
// its object id.
func (e *exporter) defVar(v *types.Var, param bool) (int, error) {
	if id, ok := e.objIDs[v]; ok {
		return id, nil
	}
	t, err := e.typeID(v.Type())
	if err != nil {
		return 0, err
	}
	id := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{Kind: "var", Name: v.Name(), Type: t, Param: param})
	e.objIDs[v] = id
	return id, nil
}

// defFunc records a top-level function or method object in the object table
// (idempotent) and returns its object id (M6, WS H).
func (e *exporter) defFunc(obj *types.Func) (int, error) {
	if id, ok := e.objIDs[obj]; ok {
		return id, nil
	}
	var t int
	var err error
	if sig, ok := obj.Type().(*types.Signature); ok && signatureHasTypeParam(sig) {
		// P3-M9 (tier-1d): generic template (function or method on a generic
		// type — the backend never uses the template's type (instantiations
		// get fresh substituted sigIDs via prepareInstantiation; the template
		// funcdecl itself is skipped). Emitting the generic signature would
		// put unsubstituted typeparams in the type table, which the backend's
		// ReflectTypes rejects ("unsupported type kind typeparam"). Use an
		// empty signature as an unreferenced placeholder.
		t, err = e.typeID(types.NewSignatureType(nil, nil, nil, nil, nil, false))
	} else {
		t, err = e.typeID(obj.Type())
	}
	if err != nil {
		return 0, err
	}
	id := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{Kind: "func", Name: obj.Name(), Type: t})
	e.objIDs[obj] = id
	return id, nil
}

// signatureHasTypeParam reports whether a signature (including its receiver)
// mentions any type parameter. Used to keep template types out of the type
// table (see defFunc).
func signatureHasTypeParam(sig *types.Signature) bool {
	if sig.TypeParams().Len() > 0 || sig.RecvTypeParams().Len() > 0 {
		return true
	}
	var walk func(t types.Type) bool
	seen := make(map[types.Type]bool)
	walk = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if _, ok := t.(*types.TypeParam); ok {
			return true
		}
		switch u := t.(type) {
		case *types.Named:
			for i := 0; i < u.TypeArgs().Len(); i++ {
				if walk(u.TypeArgs().At(i)) {
					return true
				}
			}
			return walk(u.Underlying())
		case *types.Pointer:
			return walk(u.Elem())
		case *types.Slice:
			return walk(u.Elem())
		case *types.Array:
			return walk(u.Elem())
		case *types.Map:
			return walk(u.Key()) || walk(u.Elem())
		case *types.Chan:
			return walk(u.Elem())
		case *types.Struct:
			for i := 0; i < u.NumFields(); i++ {
				if walk(u.Field(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			if u.Recv() != nil && walk(u.Recv().Type()) {
				return true
			}
			for i := 0; i < u.Params().Len(); i++ {
				if walk(u.Params().At(i).Type()) {
					return true
				}
			}
			for i := 0; i < u.Results().Len(); i++ {
				if walk(u.Results().At(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return walk(sig)
}

// ---------------------------------------------------------------------------
// Type table.
// ---------------------------------------------------------------------------

// sizesWouldPanic reports whether asking go/types for the size/alignment of
// t would panic: gcSizes asserts on bare type parameters, which are reached
// by descending through array element types, struct field types, and named
// type underlyings. (Slice/map/pointer/chan positions are fine — gcSizes
// returns WordSize without descending.)
func sizesWouldPanic(t types.Type) bool {
	seen := make(map[types.Type]bool)
	var walk func(t types.Type) bool
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

// typeID returns the type-table id for t, serializing t on first sight.
// Recursive types are handled by registering the id before describing.
//
// M6 (WS H): while exporting a monomorphized instantiation (e.subst != nil),
// bound type parameters resolve through the substitution chain before the
// per-instantiation memo is consulted. The global typeIDs memo is only
// valid without substitution: the same unsubstituted type denotes different
// concrete types under different substitutions.
// substType returns t with every type parameter bound in e.subst replaced
// by its binding (M6, WS H — issue52026). Bindings resolve in the scope
// where they were written (the chain links nested instantiations). Returns
// t unchanged when no substitution is active or t contains no bound type
// parameter. Composite types are rebuilt only when a component changed, so
// concrete types keep their canonical go/types identity.

// substCanonKey returns a structural identity string for a substituted
// type. Composite structure is by name; type parameters are keyed by
// object identity (%p of the TypeName) so two distinct parameters that
// happen to share a name never conflate. Only used to canonicalize the
// fresh objects substType mints within a single exporter run.
func substCanonKey(t types.Type) string {
	switch t := t.(type) {
	case *types.Basic:
		return "b:" + t.Name()
	case *types.Named:
		var sb strings.Builder
		sb.WriteString("n:")
		sb.WriteString(qualifiedName(t.Obj()))
		if targs := t.TypeArgs(); targs != nil && targs.Len() > 0 {
			sb.WriteByte('[')
			for i := 0; i < targs.Len(); i++ {
				if i > 0 {
					sb.WriteByte(',')
				}
				sb.WriteString(substCanonKey(targs.At(i)))
			}
			sb.WriteByte(']')
		}
		return sb.String()
	case *types.Pointer:
		return "p:" + substCanonKey(t.Elem())
	case *types.Slice:
		return "s:" + substCanonKey(t.Elem())
	case *types.Array:
		return "a:" + strconv.FormatInt(t.Len(), 10) + ":" + substCanonKey(t.Elem())
	case *types.Map:
		return "m:" + substCanonKey(t.Key()) + ":" + substCanonKey(t.Elem())
	case *types.Chan:
		return "c:" + strconv.Itoa(int(t.Dir())) + ":" + substCanonKey(t.Elem())
	case *types.TypeParam:
		return fmt.Sprintf("tp:%p", t.Obj())
	default:
		return "x:" + types.TypeString(t, nil)
	}
}

// canonSubst canonicalizes a freshly-minted substituted type: the identical
// object is returned for structurally identical results, so the
// object-identity memos recognize them.
func (e *exporter) canonSubst(t types.Type) types.Type {
	if e.substCanon == nil {
		e.substCanon = make(map[string]types.Type)
	}
	k := substCanonKey(t)
	if c, ok := e.substCanon[k]; ok {
		return c
	}
	e.substCanon[k] = t
	return t
}

func (e *exporter) substType(t types.Type) types.Type {
	if e.subst == nil {
		return t
	}
	switch t := t.(type) {
	case *types.TypeParam:
		if c, scope := e.subst.resolve(t); c != nil {
			saved := e.subst
			e.subst = scope
			c = e.substType(c)
			e.subst = saved
			return c
		}
		return t
	case *types.Named:
		targs := t.TypeArgs()
		if targs == nil || targs.Len() == 0 {
			return t
		}
		changed := false
		newArgs := make([]types.Type, targs.Len())
		for i := 0; i < targs.Len(); i++ {
			newArgs[i] = e.substType(targs.At(i))
			if newArgs[i] != targs.At(i) {
				changed = true
			}
		}
		if !changed {
			return t
		}
		inst, err := types.Instantiate(nil, t.Origin(), newArgs, false)
		if err != nil {
			return t // defensive; describeType fails loudly on bad types
		}
		return e.canonSubst(inst)
	case *types.Pointer:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return e.canonSubst(types.NewPointer(elem))
		}
		return t
	case *types.Slice:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return e.canonSubst(types.NewSlice(elem))
		}
		return t
	case *types.Array:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return e.canonSubst(types.NewArray(elem, t.Len()))
		}
		return t
	case *types.Map:
		key, elem := e.substType(t.Key()), e.substType(t.Elem())
		if key != t.Key() || elem != t.Elem() {
			return e.canonSubst(types.NewMap(key, elem))
		}
		return t
	case *types.Chan:
		if elem := e.substType(t.Elem()); elem != t.Elem() {
			return e.canonSubst(types.NewChan(t.Dir(), elem))
		}
		return t
	case *types.Signature:
		params, results := t.Params(), t.Results()
		changed := false
		newParams := make([]*types.Var, params.Len())
		for i := 0; i < params.Len(); i++ {
			v := params.At(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				newParams[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), nt)
			} else {
				newParams[i] = v
			}
		}
		newResults := make([]*types.Var, results.Len())
		for i := 0; i < results.Len(); i++ {
			v := results.At(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				newResults[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), nt)
			} else {
				newResults[i] = v
			}
		}
		if !changed {
			return t
		}
		return types.NewSignatureType(t.Recv(), typeParamSlice(t.RecvTypeParams()),
			typeParamSlice(t.TypeParams()), types.NewTuple(newParams...),
			types.NewTuple(newResults...), t.Variadic())
	case *types.Struct:
		changed := false
		fields := make([]*types.Var, t.NumFields())
		tags := make([]string, t.NumFields())
		for i := 0; i < t.NumFields(); i++ {
			v := t.Field(i)
			if nt := e.substType(v.Type()); nt != v.Type() {
				changed = true
				fields[i] = types.NewField(v.Pos(), v.Pkg(), v.Name(), nt, v.Embedded())
			} else {
				fields[i] = v
			}
			tags[i] = t.Tag(i)
		}
		if !changed {
			return t
		}
		return types.NewStruct(fields, tags)
	default:
		return t
	}
}

// typeParamSlice converts a *types.TypeParamList to a slice.
func typeParamSlice(l *types.TypeParamList) []*types.TypeParam {
	if l == nil {
		return nil
	}
	s := make([]*types.TypeParam, l.Len())
	for i := 0; i < l.Len(); i++ {
		s[i] = l.At(i)
	}
	return s
}

// namedTypeName renders the qualified name of a named type including its
// (already substituted) type arguments — the canonical identity key.
func namedTypeName(nt *types.Named) string {
	name := qualifiedName(nt.Obj())
	if targs := nt.TypeArgs(); targs != nil && targs.Len() > 0 {
		args := make([]string, targs.Len())
		for i := 0; i < targs.Len(); i++ {
			args[i] = qualifiedNameForType(targs.At(i))
		}
		name += "[" + strings.Join(args, ", ") + "]"
	}
	return name
}

func (e *exporter) typeID(t types.Type) (int, error) {
	// P3-M8 (reviewer-2 P1-4): types.Invalid must never reach the wire.
	// The only known producer was exportIdent's *types.Builtin case
	// (go/types models every builtin's type as Typ[Invalid]); it no
	// longer serializes obj.Type(). Any other path reaching here with
	// Invalid is a genuine failed resolution — fail loud with the wire
	// id instead of laundering it into a bogus "invalid type" entry.
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.Invalid {
		return 0, fmt.Errorf("gofrontend: types.Invalid reached typeID (would be types[%d]); refusing to serialize", len(e.types))
	}
	// P3-M8b: unwrap aliases to unsafe.Pointer (from the synthetic unsafe
	// package) directly to the basic type. The alias indirection through
	// the type table confuses kindOf; the basic type is the canonical form.
	if a, ok := t.(*types.Alias); ok {
		if b, ok := types.Unalias(a).(*types.Basic); ok && b.Kind() == types.UnsafePointer {
			t = b
		}
	}
	// P3-M9 (WS H, graalgo-wsh-fallback): apply substitution BEFORE the
	// TypeParam check. When in an instantiation (e.subst != nil), a type
	// parameter like P must resolve to its binding (e.g., *IntPtr) before
	// we decide whether to emit a "typeparam" entry. Otherwise,
	// monomorphized bodies leak "typeparam" kinds into the type table,
	// which the Java backend rejects ("unknown composite type kind").
	// Unbound type params (subst returns them unchanged) still fall
	// through to the canonicalization below.
	if e.subst != nil {
		t = e.substType(t)
	}
	// P3-M9 (WS H): canonicalize type parameters by TypeName object.
	// go/types mints a fresh *types.TypeParam per syntactic occurrence;
	// without canonicalization, F-bounded constraints (P Point[P]) cause
	// infinite recursion because each occurrence gets a different ID.
	if tp, ok := t.(*types.TypeParam); ok {
		if id, ok := e.typeparamIDs[tp.Obj()]; ok {
			return id, nil
		}
		// Allocate ID and memoize BEFORE describing, so recursive
		// references (constraint mentions the parameter) hit the memo.
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.typeList = append(e.typeList, t)
		e.typeparamIDs[tp.Obj()] = id
		// Also record in typeIDs for the specific object, in case
		// the same object is seen again.
		e.typeIDs[t] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if e.subst != nil {
		// M6 (WS H, issue52026): substitute bound type parameters before
		// the memo lookup, so Some[T] under T=int denotes Some[int].
		// The substituted *types.Named is a fresh object — deduplicate
		// named types by qualified name, or the monomorphized body and
		// the call site would mint different ids for the same type.
		t = e.substType(t)
		if nt, ok := t.(*types.Named); ok {
			name := namedTypeName(nt)
			if id, ok := e.namedIDs[name]; ok {
				return id, nil
			}
			id := len(e.types)
			e.types = append(e.types, TypeEnt{})
			e.typeList = append(e.typeList, t)
			e.namedIDs[name] = id
			ent, err := e.describeType(t)
			if err != nil {
				return 0, err
			}
			e.types[id] = ent
			return id, nil
		}
		if id, ok := e.substTypeIDs[t]; ok {
			return id, nil
		}
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.typeList = append(e.typeList, t)
		e.substTypeIDs[t] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if nt, ok := t.(*types.Named); ok {
		// Named types deduplicate by qualified name globally as well, so a
		// substituted Some[int] (via namedIDs) shares the id with the
		// Some[int] from non-generic code (issue52026).
		name := namedTypeName(nt)
		if id, ok := e.namedIDs[name]; ok {
			return id, nil
		}
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.typeList = append(e.typeList, t)
		e.namedIDs[name] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if it, ok := t.(*types.Interface); ok {
		// P3-M8-leftovers: deduplicate anonymous interfaces by structural
		// key (sorted method implkeys). go/types mints a fresh *types.Interface
		// per syntactic occurrence; without this, a type assertion
		// `x.(interface{ M() })` and the subsequent `y.M()` dispatch mint
		// different wire ids, the itab lookup misses, and dispatch returns
		// garbage (observed: 0 instead of 5 for WriteSingle).
		key := ifaceStructKey(it)
		if id, ok := e.ifaceIDs[key]; ok {
			return id, nil
		}
		id := len(e.types)
		e.types = append(e.types, TypeEnt{})
		e.typeList = append(e.typeList, t)
		e.ifaceIDs[key] = id
		e.typeIDs[t] = id
		ent, err := e.describeType(t)
		if err != nil {
			return 0, err
		}
		e.types[id] = ent
		return id, nil
	}
	if id, ok := e.typeIDs[t]; ok {
		return id, nil
	}
	id := len(e.types)
	e.types = append(e.types, TypeEnt{})
	e.typeList = append(e.typeList, t)
	e.typeIDs[t] = id
	ent, err := e.describeType(t)
	if err != nil {
		return 0, err
	}
	e.types[id] = ent
	return id, nil
}

func basicClass(b *types.Basic) (class string, untyped bool) {
	if b.Kind() == types.UnsafePointer {
		return "pointer", false
	}
	info := b.Info()
	untyped = info&types.IsUntyped != 0
	switch {
	case info&types.IsBoolean != 0:
		return "bool", untyped
	case info&types.IsInteger != 0:
		if info&types.IsUnsigned != 0 {
			return "uint", untyped
		}
		return "int", untyped
	case info&types.IsFloat != 0:
		return "float", untyped
	case info&types.IsComplex != 0:
		return "complex", untyped
	case info&types.IsString != 0:
		return "string", untyped
	}
	return "unknown", untyped
}

func qualifiedName(obj *types.TypeName) string {
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path() + "." + obj.Name()
	}
	return obj.Name()
}

// qualifiedNameForType returns the qualified name for a type argument
// (e.g. "main.foo" for type foo int). Used in instantiated type names.
// Named type arguments render WITH their own type arguments (via
// namedTypeName): dropping them collapses Box[Box[int]] and Box[Box[string]]
// to the same dedup key, which mints a cyclic type graph and overflows the
// frontend (M10 self-attack: 4-deep Box nesting). The recursion terminates:
// type arguments form a finite tree.
func qualifiedNameForType(t types.Type) string {
	if n, ok := t.(*types.Named); ok {
		return namedTypeName(n)
	}
	if b, ok := t.(*types.Basic); ok {
		return b.Name()
	}
	return types.TypeString(t, nil)
}

func (e *exporter) tupleList(t *types.Tuple) ([]any, error) {
	out := make([]any, 0, t.Len())
	for i := 0; i < t.Len(); i++ {
		v := t.At(i)
		tid, err := e.typeID(v.Type())
		if err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"name": v.Name(), "type": tid})
	}
	return out, nil
}

// pkgPathOf returns the package path of a named type.
//
// A nil Pkg is legitimate in exactly one case: the universe-scope
// predeclared named types. go/types builds `error` and `comparable`
// as NewNamed with a nil package (universe.go); they have no package
// by definition, and gc's reflect likewise reports PkgPath "" for
// them. It is NOT a "synthetic bootstrap" case: every synthetic named
// type (sync primitives, io/os wrappers, …) is created with its
// synthetic package, so Pkg() is non-nil.
//
// Any other named type with nil Pkg (or nil Obj) is an exporter bug —
// fail loud instead of emitting a silent "". (Reviewer-2 P2-4: the old
// code laundered every such case into pkgpath "".)
func pkgPathOf(t *types.Named) (string, error) {
	obj := t.Obj()
	if obj == nil {
		return "", fmt.Errorf("gofrontend: named type with nil type name: %s", t)
	}
	if pkg := obj.Pkg(); pkg != nil {
		return pkg.Path(), nil
	}
	// Universe scope: only the predeclared named types are legitimate.
	if name := obj.Name(); name == "error" || name == "comparable" {
		return "", nil
	}
	return "", fmt.Errorf("gofrontend: named type %q has nil package", obj.Name())
}

func (e *exporter) describeType(t types.Type) (TypeEnt, error) {
	mk := func(kind string, detail map[string]any) TypeEnt {
		return TypeEnt{Kind: kind, Detail: detail}
	}
	switch t := t.(type) {
	case *types.Basic:
		class, untyped := basicClass(t)
		if t.Kind() == types.Invalid && os.Getenv("GRAALGO_TRACE_INVALID") == "1" {
			fmt.Fprintf(os.Stderr, "TRACE-INVALID basic Invalid at describeType\n%s\n", debug.Stack())
		}
		return mk("basic", map[string]any{
			"name": t.Name(), "class": class, "untyped": untyped,
		}), nil
	case *types.Named:
		// M6 (WS H): named types may have methods (emitted as funcdecls /
		// instantiations; dispatch resolves by object id).
		// P3-M3 (WS D, ADR-0003): the "methods" list is populated here —
		// one {"name","obj","recv","index"} per method in types.NewMethodSet order
		// (covers promoted/embedded methods, which methodIndex lacks),
		// resolved to declarations via funcByObj. "obj" is the funcdecl
		// object id; "recv" is the declared receiver kind
		// ("value"|"pointer"). Interface-declared methods (no funcdecl)
		// and template methods are skipped.
		// M6: instantiated generic types (e.g. F[foo]) include the type
		// arguments in the name for %T formatting (issue49547).
		// P2-3: single renderer (namedTypeName) — a second copy here would
		// silently mint two wire ids for one type if they ever drifted,
		// breaking the t == dyn identity B2a/B4 rest on.
		//
		// P3-M9 (generics, Gap 2): monomorphize generic type definitions.
		// If this Named is an instantiation (has type arguments), bind the
		// origin's type parameters to the type arguments and describe the
		// underlying type with substitution active. This yields concrete
		// field types (e.g., newPoint func() *nistec.P256Point instead of
		// func() Point), so no "typeparam" kinds leak into the type table
		// — the Java backend rejects them ("unknown composite type kind").
		// Generic template definitions themselves are skipped at type
		// declaration processing (see exportStmt GenDecl TYPE case).
		//
		// P3-M9 (generics, funcs): if this is a generic template definition
		// (origin == t, has type params, no type args), do NOT describe its
		// underlying type — that would leak typeparams. Return an empty
		// struct placeholder. Templates are never used directly; only
		// instantiations should be described. This handles synthetic generic
		// pair structs (e.g., __mrnistCurve_pointFromAffinePair[Point])
		// that are referenced during on-demand type description.
		if origin := t.Origin(); origin == t {
			if tparams := origin.TypeParams(); tparams != nil && tparams.Len() > 0 {
				if targs := t.TypeArgs(); targs == nil || targs.Len() == 0 {
					// Generic template: return empty struct placeholder.
					// The template itself is never emitted; only
					// instantiations (which have type args) are described.
					return mk("struct", map[string]any{
						"fields": []any{},
					}), nil
				}
			}
		}
		underlying := t.Underlying()
		savedSubst := e.subst
		savedSubstTypeIDs := e.substTypeIDs
		if targs := t.TypeArgs(); targs != nil && targs.Len() > 0 {
			if origin := t.Origin(); origin != t {
				if tparams := origin.TypeParams(); tparams != nil && tparams.Len() > 0 {
					targsList := make([]types.Type, targs.Len())
					for i := range targsList {
						ta := targs.At(i)
						// P3-M9 (generics, funcs): resolve type args through
						// the current substitution. If the type arg is a
						// typeparam (e.g., Point from an enclosing generic
						// function), resolve it to the concrete type via
						// the active subst chain. Otherwise the binding
						// would be Point->Point (still a typeparam), leaking
						// "typeparam" kinds into the type table.
						if tp, ok := ta.(*types.TypeParam); ok {
							if resolved := resolveByName(e.subst, tp.Obj().Name()); resolved != nil {
								ta = resolved
							}
						}
						targsList[i] = ta
					}
					bind := bindParams(tparams, targsList)
					e.subst = &typeSubst{parent: savedSubst, bind: bind}
					e.substTypeIDs = make(map[types.Type]int)
				}
			}
		}
		under, err := e.typeID(underlying)
		e.subst = savedSubst
		e.substTypeIDs = savedSubstTypeIDs
		if err != nil {
			return TypeEnt{}, err
		}
		methods, tmplSkipped, err := e.methodTableEntries(t)
		if err != nil {
			return TypeEnt{}, err
		}
		pkgPath, err := pkgPathOf(t)
		if err != nil {
			return TypeEnt{}, err
		}
		detail := map[string]any{
			"name": namedTypeName(t), "underlying": under, "methods": methods,
			// P3-M8 (wave 2, reflect): package path for Type.PkgPath.
			// pkgPathOf fails loud on unexpected nil Pkg (reviewer-2 P2-4);
			// "" is legitimate only for universe predeclared (error).
			"pkgpath": pkgPath,
		}
		if tmplSkipped {
			// P3-M3 remediation (R1 P1-2): the frontend fails fast
			// with the named P3-M3 scope error when such an
			// instantiation is boxed into a non-empty interface.
			detail["templateMethodsSkipped"] = true
		}
		return mk("named", detail), nil
	case *types.Pointer:
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		// P3-M3 (WS D, ADR-0003): the same "methods" field as named
		// entries, holding the full *T method set (value + pointer
		// receivers) for itab construction.
		methods, tmplSkipped, err := e.methodTableEntries(t)
		if err != nil {
			return TypeEnt{}, err
		}
		detail := map[string]any{"elem": elem, "methods": methods}
		if tmplSkipped {
			// P3-M3 remediation (R1 P1-2): see the "named" case above.
			detail["templateMethodsSkipped"] = true
		}
		return mk("pointer", detail), nil
	case *types.Slice:
		// P3-M2b (ADR-0002 D3): the factory manufactures element zeros
		// (frozen); a sync primitive element would zero to an unusable
		// empty struct. *sync.X elements are fine (nil is correct).
		if nt := syntheticPrimitiveNamed(t.Elem()); nt != nil {
			return TypeEnt{}, syntheticCompositeScopeError(nt, "a slice element")
		}
		// P3-M5 (WS E, ADR-0005 D1): time.Timer/Ticker values in
		// factory-zero-manufactured positions are not in M5 scope.
		if nt := timeTimerTickerNamedExact(t.Elem()); nt != nil {
			return TypeEnt{}, timerScopeErrorf(nt, "a slice element")
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("slice", map[string]any{"elem": elem}), nil
	case *types.Array:
		// P3-M2b (ADR-0002 D3): as for slices — array zeros are factory-made.
		if nt := syntheticPrimitiveNamed(t.Elem()); nt != nil {
			// P3-M9 (driver-harden, go-cmp lane): sync.Pool is the
			// exception — the factory manufactures a distinct GoPool
			// per slot (NodeFactory.arrayZeroTemplate/zeroArrayFor),
			// so [N]sync.Pool (regexp's matchPool) works. Other
			// primitives still scope-error: their zeros would be
			// unusable empty structs.
			if !isSyncPoolNamed(nt) {
				return TypeEnt{}, syntheticCompositeScopeError(nt, "an array element")
			}
		}
		if nt := timeTimerTickerNamedExact(t.Elem()); nt != nil {
			return TypeEnt{}, timerScopeErrorf(nt, "an array element")
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("array", map[string]any{"len": t.Len(), "elem": elem}), nil
	case *types.Map:
		// P3-M2b (ADR-0002 D3): missing-key reads manufacture the element
		// zero in the factory; keys compare by value semantics the opaque
		// model cannot provide. Both positions scope-error.
		if nt := syntheticPrimitiveNamed(t.Key()); nt != nil {
			return TypeEnt{}, syntheticCompositeScopeError(nt, "a map key")
		}
		if nt := syntheticPrimitiveNamed(t.Elem()); nt != nil {
			return TypeEnt{}, syntheticCompositeScopeError(nt, "a map element")
		}
		if nt := timeTimerTickerNamedExact(t.Key()); nt != nil {
			return TypeEnt{}, timerScopeErrorf(nt, "a map key")
		}
		if nt := timeTimerTickerNamedExact(t.Elem()); nt != nil {
			return TypeEnt{}, timerScopeErrorf(nt, "a map element")
		}
		key, err := e.typeID(t.Key())
		if err != nil {
			return TypeEnt{}, err
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("map", map[string]any{"key": key, "elem": elem}), nil
	case *types.Chan:
		var dir string
		switch t.Dir() {
		case types.SendRecv:
			dir = "both"
		case types.SendOnly:
			dir = "send"
		case types.RecvOnly:
			dir = "recv"
		}
		elem, err := e.typeID(t.Elem())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("chan", map[string]any{"dir": dir, "elem": elem}), nil
	case *types.Struct:
		fields := make([]any, 0, t.NumFields())
		vars := make([]*types.Var, 0, t.NumFields())
		for i := 0; i < t.NumFields(); i++ {
			// P3-M2b (ADR-0002 D3): the factory manufactures field zeros.
			// P3-M9 (driver-harden, go-cmp lane): sync primitive fields are
			// now allowed — the factory's fieldZeroTemplate manufactures a
			// usable Java primitive object (not an empty struct). *sync.X
			// fields were already fine (nil is correct).
			if nt := timeTimerTickerNamedExact(t.Field(i).Type()); nt != nil {
				return TypeEnt{}, timerScopeErrorf(nt, "a struct field")
			}
			vars = append(vars, t.Field(i))
		}
		// M6 (WS H): go/types' gcSizes asserts on bare type parameters
		// (Alignof/Sizeof panic), reachable by descending through array
		// element and struct field types — e.g. struct{ t T }. Field
		// offsets of a generic struct are instantiation-dependent anyway,
		// so they are not computed: the wire carries -1 ("unknown"; the
		// factory indexes fields by position, never by byte offset).
		offsets := make([]int64, len(vars))
		generic := false
		for _, v := range vars {
			if sizesWouldPanic(v.Type()) {
				generic = true
				break
			}
		}
		if generic {
			for i := range offsets {
				offsets[i] = -1
			}
		} else if structValueDepth(t, 0) > maxStructDepth {
			// M10 (P1-4): depth cap. Computing real offsets here would
			// invoke go/types' exponential Sizeof; -1 is safe (the
			// factory indexes fields by position, never by byte offset).
			for i := range offsets {
				offsets[i] = -1
			}
		} else {
			offsets = e.sizes.Offsetsof(vars)
		}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			ft, err := e.typeID(f.Type())
			if err != nil {
				return TypeEnt{}, err
			}
			// P3-M8 (wave 2, reflect): "exported" and "pkgpath" for
			// unexported-field flagRO parity (StructField.PkgPath).
			pkgPath := ""
			if !f.Exported() && f.Pkg() != nil {
				pkgPath = f.Pkg().Path()
			}
			fields = append(fields, map[string]any{
				"name": f.Name(), "type": ft, "tag": t.Tag(i),
				"embedded": f.Embedded(), "offset": offsets[i],
				"exported": f.Exported(), "pkgpath": pkgPath,
			})
		}
		return mk("struct", map[string]any{"fields": fields}), nil
	case *types.Signature:
		// P3-M9 (generics, funcs): skip generic function template signatures.
		// A signature with type params (e.g., func Sign[P Point[P]]) is a
		// template, never used directly — only monomorphized instantiations
		// appear in valid code. Describing the template would leak
		// "typeparam" kinds into the type table, which the Java backend
		// rejects ("unknown composite type kind"). Return an empty
		// signature placeholder (mirrors defFunc's handling); the template
		// itself is never emitted (see exportFuncDecl/exportSyntheticBodies
		// isTemplate skips).
		if t.TypeParams() != nil && t.TypeParams().Len() > 0 {
			return mk("signature", map[string]any{
				"params": []any{}, "results": []any{}, "variadic": false,
			}), nil
		}
		params, err := e.tupleList(t.Params())
		if err != nil {
			return TypeEnt{}, err
		}
		results, err := e.tupleList(t.Results())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("signature", map[string]any{
			"params": params, "results": results, "variadic": t.Variadic(),
		}), nil
	case *types.Interface:
		methods := make([]any, 0, t.NumMethods())
		for i := 0; i < t.NumMethods(); i++ {
			m := t.Method(i)
			sig, err := e.typeID(m.Type())
			if err != nil {
				return TypeEnt{}, err
			}
			// P3-M3: "implkey" is the method-identity key (name +
			// structural signature, aliases expanded) for the runtime
			// implements check — names alone are unsound (R1 P0-1).
			// t.Method(i) is *types.Func; interface method signatures
			// never declare a receiver.
			msig, ok := m.Type().(*types.Signature)
			if !ok {
				return TypeEnt{}, fmt.Errorf("gofrontend: interface method %s has non-signature type %T",
					m.Name(), m.Type())
			}
			methods = append(methods, map[string]any{"name": m.Name(), "sig": sig, "implkey": implKeyOf(m, msig)})
		}
		embedded := make([]any, 0, t.NumEmbeddeds())
		for i := 0; i < t.NumEmbeddeds(); i++ {
			id, err := e.typeID(t.EmbeddedType(i))
			if err != nil {
				return TypeEnt{}, err
			}
			embedded = append(embedded, id)
		}
		return mk("interface", map[string]any{
			"empty":   t.NumMethods() == 0 && t.NumEmbeddeds() == 0,
			"methods": methods, "embedded": embedded,
		}), nil
	case *types.Union:
		// P3-M9 (generics): union types appear only in generic constraints
		// (e.g., *P224Point | *P256Point in nistPoint[Point]). Constraints
		// are compile-time only; the runtime never creates or dispatches
		// on a union-typed value. The Java backend has no union kind.
		// Emit an empty interface placeholder.
		return mk("interface", map[string]any{
			"embedded": []any{}, "empty": true, "methods": []any{},
		}), nil
	case *types.TypeParam:
		c, err := e.typeID(t.Constraint())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("typeparam", map[string]any{"name": t.Obj().Name(), "constraint": c}), nil
	case *types.Alias:
		rhs, err := e.typeID(t.Rhs())
		if err != nil {
			return TypeEnt{}, err
		}
		return mk("alias", map[string]any{"name": qualifiedName(t.Obj()), "rhs": rhs}), nil
	default:
		return TypeEnt{}, fmt.Errorf("gofrontend: unsupported go/types type %T", t)
	}
}

// ---------------------------------------------------------------------------
// Functions and methods (M6, WS H).
// ---------------------------------------------------------------------------

// funcDeclInfo is one top-level function or method declaration.
type funcDeclInfo struct {
	fd     *ast.FuncDecl
	obj    *types.Func
	funcID int // object-table id of the "func" entry
	sig    *types.Signature
	recv   *types.Var // nil for plain functions (methods: sig.Recv())
	// isTemplate: generic function, method on a generic type, or generic
	// method — emitted only via monomorphized instantiations, never
	// directly.
	isTemplate bool
	// synthPkg: non-nil for declarations imported from a synthetic
	// package (P3-M9 tier-1d): exportInstantiation swaps to the synthetic
	// package's fset/src/info when emitting the body.
	synthPkg *syntheticPackage
	// file: the declaration's own *ast.File within synthPkg (WS G:
	// multi-file disk packages need per-file e.src for e.pos()).
	file *ast.File
}

// instantiation is one monomorphized function/method body to emit
// (PLAN.md D2: one RootNode per go/types instantiation).
type instantiation struct {
	decl *funcDeclInfo
	// subst binds the template's type params (function params; for
	// methods, receiver-origin params then method params) to actual args.
	// Values live in the parent scope (chained substitution).
	subst *typeSubst
	// rawTargs are the actual argument types in bind order, for the wire
	// "typeargs" field (resolved through subst at emission).
	rawTargs []types.Type
	key      string // dedup key (deterministic)
	name     string // display name, e.g. "Min[int]", "Stack[int].Size"
	instID   int    // object-table id of the "inst" entry (assigned at emission)
	sigID    int    // type-table id of the instantiated signature (assigned at emission)
	targIDs  []int  // type-table ids of rawTargs (assigned at emission)
}

// methodKey indexes method declarations by receiver type name + method name.
type methodKey struct {
	recv types.Object // the receiver's *types.TypeName object
	name string
}

// findMethodByName is the P3-M8b fallback for methodIndex misses caused by
// *types.TypeName object identity mismatches (e.g. alias unwrapping across
// separately type-checked package instances). It matches by package path +
// type name + method name strings. Only used on direct-lookup miss.
func (e *exporter) findMethodByName(recvObj types.Object, mname string) *funcDeclInfo {
	tn, ok := recvObj.(*types.TypeName)
	if !ok {
		return nil
	}
	pkg := tn.Pkg()
	if pkg == nil {
		return nil
	}
	wantPkg, wantName := pkg.Path(), tn.Name()
	for key, fi := range e.methodIndex {
		if key.name != mname {
			continue
		}
		ktn, ok := key.recv.(*types.TypeName)
		if !ok || ktn.Pkg() == nil {
			continue
		}
		if ktn.Pkg().Path() == wantPkg && ktn.Name() == wantName {
			return fi
		}
	}
	return nil
}

// derefNamed strips one pointer layer and reports the named type, if any.
func derefNamed(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	// P3-M8b: unwrap type aliases (e.g. os.FileMode = fs.FileMode).
	// go/types represents alias uses as *types.Alias; the method set
	// lives on the aliased named type.
	for {
		a, ok := t.(*types.Alias)
		if !ok {
			break
		}
		t = a.Rhs()
	}
	n, _ := t.(*types.Named)
	return n
}

// bindRecvParams binds the type parameters appearing in a method
// declaration's receiver type (the actual *types.TypeParam objects in the
// declared receiver's type arguments — note these are NOT identical to the
// origin's type params, verified by probe) to the call-site receiver's
// type arguments.
func bindRecvParams(bind map[*types.TypeParam]types.Type, fi *funcDeclInfo, callRN *types.Named) error {
	declRN := derefNamed(fi.recv.Type())
	if declRN == nil {
		return fmt.Errorf("gofrontend: method %s has non-named declared receiver", fi.obj.Name())
	}
	declArgs := typeList(declRN.TypeArgs())
	callArgs := typeList(callRN.TypeArgs())
	if len(declArgs) != len(callArgs) {
		return fmt.Errorf("gofrontend: receiver arity mismatch for method %s", fi.obj.Name())
	}
	for i := range declArgs {
		tp, ok := declArgs[i].(*types.TypeParam)
		if !ok {
			continue // concrete in the declaration; nothing to bind
		}
		bind[tp] = callArgs[i]
	}
	return nil
}

// typeList converts a *types.TypeList to a slice.
func typeList(l *types.TypeList) []types.Type {
	if l == nil {
		return nil
	}
	out := make([]types.Type, l.Len())
	for i := 0; i < l.Len(); i++ {
		out[i] = l.At(i)
	}
	return out
}

// denotedFunc returns the *types.Func denoted by id (Uses or Defs).
func denotedFunc(info *types.Info, id *ast.Ident) (*types.Func, bool) {
	obj := info.Uses[id]
	if obj == nil {
		obj = info.Defs[id]
	}
	fn, ok := obj.(*types.Func)
	return fn, ok
}

// unwrapSelector strips index/paren wrappers to find a selector expression
// (method calls with explicit type arguments: s.M[T](...)).
func unwrapSelector(fun ast.Expr) (*ast.SelectorExpr, bool) {
	for {
		switch f := fun.(type) {
		case *ast.SelectorExpr:
			return f, true
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		case *ast.ParenExpr:
			fun = f.X
		default:
			return nil, false
		}
	}
}

// callTargetIdent unwraps index/paren expressions to the called identifier,
// or nil for non-identifier call targets (funclits, selector calls, ...).
func callTargetIdent(fun ast.Expr) *ast.Ident {
	for {
		switch f := fun.(type) {
		case *ast.Ident:
			return f
		case *ast.IndexExpr:
			fun = f.X
		case *ast.IndexListExpr:
			fun = f.X
		case *ast.ParenExpr:
			fun = f.X
		default:
			return nil
		}
	}
}

// collectFuncDecls walks the file's top-level declarations, registering
// every function/method object and indexing method declarations.
func (e *exporter) collectFuncDecls(files []*ast.File) (funcs []*funcDeclInfo, funcByObj map[*types.Func]*funcDeclInfo, methodIndex map[methodKey]*funcDeclInfo, err error) {
	funcByObj = make(map[*types.Func]*funcDeclInfo)
	methodIndex = make(map[methodKey]*funcDeclInfo)
	for _, file := range files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			obj, ok := e.info.Defs[fd.Name].(*types.Func)
			if !ok {
				return nil, nil, nil, fmt.Errorf("gofrontend: %s has no *types.Func object", fd.Name.Name)
			}
			funcID, err := e.defFunc(obj)
			if err != nil {
				return nil, nil, nil, err
			}
			sig, ok := obj.Type().(*types.Signature)
			if !ok {
				return nil, nil, nil, fmt.Errorf("gofrontend: %s has non-signature type %s", fd.Name.Name, obj.Type())
			}
			fi := &funcDeclInfo{fd: fd, obj: obj, funcID: funcID, sig: sig}
			if fd.Recv != nil {
				fi.recv = sig.Recv()
				if fi.recv == nil {
					return nil, nil, nil, fmt.Errorf("gofrontend: method %s has no receiver var", fd.Name.Name)
				}
				rn := derefNamed(fi.recv.Type())
				if rn == nil {
					return nil, nil, nil, scopeErrorf("method %s has non-named receiver type %s (WS D)", fd.Name.Name, fi.recv.Type())
				}
				methodIndex[methodKey{rn.Obj(), obj.Name()}] = fi
				fi.isTemplate = sig.TypeParams().Len() > 0 || rn.Origin().TypeParams().Len() > 0
			} else {
				fi.isTemplate = sig.TypeParams().Len() > 0
			}
			funcs = append(funcs, fi)
			funcByObj[obj] = fi
		}
	}
	return funcs, funcByObj, methodIndex, nil
}

// instKeyForFunc builds the dedup key for a generic-function instantiation.
func instKeyForFunc(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) string {
	sub := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
	// P3-M9 (generics): the key must include the package path. Two generic
	// functions with the same name in different packages (e.g.
	// crypto/internal/fips140/ecdsa.GenerateKey[P] and
	// crypto/internal/fips140/ecdh.GenerateKey[P]) are different functions;
	// a bare-name key collides and the second instantiation silently reuses
	// the first one's body (wrong types, wrong field indices — e.g. ecdh's
	// Curve.N at field 2 vs ecdsa's Curve.N at field 3).
	return "F:" + funcQualifiedName(fi.obj) + "|" + joinSubstKeys(targs, sub)
}

// funcQualifiedName returns the package-qualified name of a function,
// e.g. "crypto/internal/fips140/ecdsa.GenerateKey".
func funcQualifiedName(fn *types.Func) string {
	if pkg := fn.Pkg(); pkg != nil {
		return pkg.Path() + "." + fn.Name()
	}
	return fn.Name()
}

// instNameForFunc builds the display name, e.g. "Min[int]".
func instNameForFunc(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) string {
	sub := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
	pretty := make([]string, len(targs))
	for i, ta := range targs {
		pretty[i] = prettyType(ta, sub)
	}
	// P3-M9 (generics): qualify with the package path, matching the
	// instantiation key — same-named generics in different packages get
	// distinct display names.
	return funcQualifiedName(fi.obj) + "[" + strings.Join(pretty, ", ") + "]"
}

// prettyType renders a type for display names (Go-like, type params
// resolved).
func prettyType(t types.Type, s *typeSubst) string {
	if tp, ok := t.(*types.TypeParam); ok && s != nil {
		if c, scope := s.resolve(tp); c != nil {
			return prettyType(c, scope)
		}
	}
	return types.TypeString(t, nil)
}

// methodCall describes one resolved method call site.
type methodCall struct {
	decl      *funcDeclInfo
	subst     *typeSubst // binds receiver-origin + method type params
	key       string     // dedup key ("" when !needsInst)
	name      string     // display name ("" when !needsInst)
	needsInst bool
	// panicMsg, when non-empty, marks a synthetic method with no backend
	// implementation (e.g. (*runtime.Func).Name). methodRefNode emits a
	// runtime panic instead of a method call.
	panicMsg string
	// rawTargsForEmit are the actual argument types in bind order, for the
	// wire "typeargs" field (valid when needsInst).
	rawTargsForEmit []types.Type
	// recvPath is the receiver adaptation path for promoted methods
	// (GAP-6): go/types' Selection.Index minus the method's own index,
	// e.g. [0] for a method promoted via embedded field 0. Empty for
	// direct methods. The caller (methodRefNode) rewrites the receiver
	// expression by walking this path, mirroring methodTableEntries.
	recvPath []int
	// ifaceFallback is set when devirtualization fails (the type
	// parameter's concrete type is not a named type) but the method can
	// be dispatched via the constraint interface (WS H F-bounded
	// fallback). When non-nil, the caller (methodRefNode) emits an
	// "ifacemethod" node instead of a static methodref. The interface is
	// the type parameter's constraint (must have an interface underlying).
	ifaceFallback types.Type
}

// resolveMethodCall resolves a method call (selection kind MethodVal) to its
// declaration and, when the receiver type or the method is generic, to a
// monomorphized instantiation. subst is the enclosing scope's substitution
// (nil at top level). methodTargs are the method's own type arguments from
// info.Instances[sel.Sel] (nil for non-generic methods).
//
// Receiver-based dispatch only: the receiver expression's type must match the
// declared receiver type after substitution (checked by the caller); implicit
// &/* adjustments are a scope error (WS D).
func (e *exporter) resolveMethodCall(sel *ast.SelectorExpr, selection *types.Selection, methodTargs []types.Type, subst *typeSubst) (*methodCall, error) {
	mname := sel.Sel.Name
	rn := derefNamed(selection.Recv())
	// P3-M9 (driver-harden, go-cmp lane): (*runtime.Func).Name(). The type
	// surface exists (synthetic, via AddMethod) but the method is not in
	// methodIndex. Return a marker methodCall; methodRefNode emits a
	// runtime panic for it.
	if rn != nil && rn.Obj().Name() == "Func" && rn.Obj().Pkg() != nil &&
		rn.Obj().Pkg().Path() == "runtime" && mname == "Name" {
		return &methodCall{panicMsg: "runtime.Func.Name not implemented (no PC-to-function table)"}, nil
	}
	if rn != nil {
		fi, ok := e.methodIndex[methodKey{rn.Obj(), mname}]
		if !ok {
			// P3-M8b: pointer-identity fallback. methodIndex keys use
			// *types.TypeName object identity, but alias unwrapping
			// (derefNamed) can yield a Named whose Obj differs from the
			// instance used at import time (e.g. os.FileMode = fs.FileMode
			// resolving through a separately type-checked package). Fall
			// back to a name-based search: same package path, same type
			// name, same method name. This is O(n) but only on miss.
			fi = e.findMethodByName(rn.Obj(), mname)
			ok = fi != nil
		}
		if !ok {
			// GAP-6: methodIndex holds only direct methods; the method
			// may be promoted through one or more embedded fields.
			return e.resolvePromotedMethodCall(sel, selection, rn, mname, methodTargs, subst)
		}
		if !fi.isTemplate {
			return &methodCall{decl: fi, subst: subst}, nil
		}
		bind := make(map[*types.TypeParam]types.Type)
		var rawTargs []types.Type
		if err := bindRecvParams(bind, fi, rn); err != nil {
			return nil, err
		}
		rawTargs = append(rawTargs, typeList(rn.TypeArgs())...)
		mtps := fi.sig.TypeParams()
		if mtps.Len() > 0 {
			if len(methodTargs) != mtps.Len() {
				return nil, fmt.Errorf("gofrontend: generic method %s called without recorded type args", mname)
			}
			for i := 0; i < mtps.Len(); i++ {
				bind[mtps.At(i)] = methodTargs[i]
			}
			rawTargs = append(rawTargs, methodTargs...)
		}
		sub := &typeSubst{parent: subst, bind: bind}
		recvKey := substString(selection.Recv(), sub)
		key := "M:" + recvKey + "." + mname
		name := prettyType(selection.Recv(), sub) + "." + mname
		if len(methodTargs) > 0 {
			key += "|" + joinSubstKeys(methodTargs, sub)
			pretty := make([]string, len(methodTargs))
			for i, ta := range methodTargs {
				pretty[i] = prettyType(ta, sub)
			}
			name += "[" + strings.Join(pretty, ", ") + "]"
		}
		if !isConcreteKey(key) {
			return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of method %s", mname)
		}
		return &methodCall{decl: fi, subst: sub, key: key, name: name, needsInst: true,
			rawTargsForEmit: rawTargs}, nil
	}
	// The receiver is not a named type: it must be a type parameter being
	// instantiated (bound-method call on a type param, e.g. v.String() with
	// v: T). Resolve the concrete type through the substitution and look
	// the method up in its method set (devirtualized per instantiation).
	tp, ok := selection.Recv().(*types.TypeParam)
	if !ok {
		// real-reflect: anonymous struct receivers (e.g. reflect's
		// package-level funcLookupCache var of type struct{sync.Mutex;
		// m sync.Map} calling the promoted Lock) resolve through the
		// promoted-method path — anonymous structs have no direct
		// methods, so any method is promoted via embedding.
		if _, isStruct := types.Unalias(selection.Recv()).Underlying().(*types.Struct); isStruct {
			return e.resolvePromotedMethodCall(sel, selection, nil, mname, methodTargs, subst)
		}
		return nil, scopeErrorf("method call %s on non-named receiver %s (WS H)", mname, selection.Recv())
	}
	if subst == nil {
		return nil, fmt.Errorf("gofrontend: method call on type parameter %s outside generic context", tp.Obj().Name())
	}
	c, scope := subst.resolve(tp)
	if c == nil {
		return nil, fmt.Errorf("gofrontend: unbound type parameter %s in method call", tp.Obj().Name())
	}
	cn := derefNamed(c)
	if cn == nil {
		// P3-M9 (WS H, graalgo-wsh-fallback): F-bounded fallback. The
		// type parameter's concrete type is not a named type (e.g.,
		// bound to another type parameter, or an interface), so
		// devirtualization is impossible. Fall back to interface
		// dispatch via the constraint interface: the method is looked
		// up in the constraint's method set, and the caller emits an
		// "ifacemethod" node.
		//
		// The constraint comes from the type parameter's declaration
		// (tp.Constraint()), not from the substituted type c, because
		// c may itself be an unresolved type parameter.
		constraint := tp.Constraint()
		if constraint == nil {
			return nil, scopeErrorf("method %s on type parameter with non-named argument type and no constraint (WS H)", mname)
		}
		// The constraint must have an interface underlying for
		// ifacemethod dispatch. For F-bounded constraints like
		// nistPoint[Point], the constraint is a generic interface
		// instantiation — its underlying is the interface.
		if _, ok := constraint.Underlying().(*types.Interface); !ok {
			return nil, scopeErrorf("method %s on type parameter with non-interface constraint %s (WS H)", mname, types.TypeString(constraint, nil))
		}
		// Verify the method exists in the constraint's method set.
		if msel := types.NewMethodSet(constraint).Lookup(e.pkg, mname); msel == nil {
			return nil, fmt.Errorf("gofrontend: method %s not in constraint %s of type parameter %s",
				mname, types.TypeString(constraint, nil), tp.Obj().Name())
		}
		return &methodCall{ifaceFallback: constraint, subst: subst}, nil
	}
	// M6: look the method up in the declaration index by (origin type
	// name, method name). We do NOT use types.NewMethodSet here: for an
	// instantiated receiver go/types returns *types.Func objects that are
	// not identical to the declared ones (verified by probe), so a
	// funcByObj lookup by object identity fails. The index is keyed by the
	// origin's TypeName, so partial-instantiation receivers (e.g.
	// Ledger[T, _]) resolve correctly.
	//
	// P3-M9 (graalgo-hash-iface-fix): If the resolved type is a named
	// interface (e.g., a type parameter H constrained by hash.Hash,
	// instantiated with H = hash.Hash itself), methodIndex has no entry:
	// it holds only concrete method declarations (func (d *digest)
	// Size()), not interface method specs (Size() int inside type Hash
	// interface). Fall back to interface dispatch on the resolved
	// interface type, verifying the method exists in its method set.
	if _, ok := cn.Underlying().(*types.Interface); ok {
		if msel := types.NewMethodSet(c).Lookup(e.pkg, mname); msel == nil {
			return nil, fmt.Errorf("gofrontend: method %s not in interface %s",
				mname, types.TypeString(c, nil))
		}
		return &methodCall{ifaceFallback: c, subst: subst}, nil
	}
	fi, ok := e.methodIndex[methodKey{cn.Obj(), mname}]
	if !ok {
		return nil, fmt.Errorf("gofrontend: type %s has no method %s", substString(c, scope), mname)
	}
	if !fi.isTemplate {
		return &methodCall{decl: fi, subst: subst}, nil
	}
	// Template reached via a type parameter: the receiver args come from
	// the concrete named type.
	bind := make(map[*types.TypeParam]types.Type)
	var rawTargs []types.Type
	if err := bindRecvParams(bind, fi, cn); err != nil {
		return nil, err
	}
	rawTargs = append(rawTargs, typeList(cn.TypeArgs())...)
	mtps := fi.sig.TypeParams()
	if mtps.Len() > 0 {
		if len(methodTargs) != mtps.Len() {
			return nil, fmt.Errorf("gofrontend: generic method %s called without recorded type args", mname)
		}
		for i := 0; i < mtps.Len(); i++ {
			bind[mtps.At(i)] = methodTargs[i]
		}
		rawTargs = append(rawTargs, methodTargs...)
	}
	sub := &typeSubst{parent: scope, bind: bind}
	recvKey := substString(c, scope)
	key := "M:" + recvKey + "." + mname
	name := prettyType(c, scope) + "." + mname
	if len(methodTargs) > 0 {
		key += "|" + joinSubstKeys(methodTargs, sub)
		pretty := make([]string, len(methodTargs))
		for i, ta := range methodTargs {
			pretty[i] = prettyType(ta, sub)
		}
		name += "[" + strings.Join(pretty, ", ") + "]"
	}
	if !isConcreteKey(key) {
		return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of method %s", mname)
	}
	return &methodCall{decl: fi, subst: sub, key: key, name: name, needsInst: true,
		rawTargsForEmit: rawTargs}, nil
}

// resolvePromotedMethodCall resolves a method call whose method is promoted
// through one or more embedded fields (GAP-6). methodIndex holds only
// direct methods, so this path resolves through types.NewMethodSet on the
// receiver type (which includes promoted methods) and adapts the receiver
// along the Selection.Index path, mirroring methodTableEntries: the
// adaptation path is Selection.Index() minus its last entry (the method's
// own index in the declaring type) — e.g. [0] for a method promoted via
// embedded field 0, [0 0] for two levels. The returned methodCall carries
// the path in recvPath; methodRefNode rewrites the receiver expression by
// walking it (a "selector" wire node with selpath), so the call is exactly
// o.Inner.M(args) at the factory.
//
// Ambiguous selectors never resolve: go/types rejects them at type-check
// time ("ambiguous selector") and they are absent from the method set, so
// no candidate is ever silently picked. A nil selection object (defensive:
// should be unreachable after type-check) fails fast with a named error.
//
// Anything unresolvable here reports the historical "method %s not found
// on %s" error, so genuinely missing methods fail exactly as before.
func (e *exporter) resolvePromotedMethodCall(sel *ast.SelectorExpr, selection *types.Selection, rn *types.Named, mname string, methodTargs []types.Type, subst *typeSubst) (*methodCall, error) {
	// rn may be nil for anonymous receivers (e.g. a package-level var of
	// anonymous struct type with an embedded sync.Mutex, as in
	// reflect's funcLookupCache); the type string is used in errors then.
	recvName := selection.Recv().String()
	if rn != nil {
		recvName = rn.Obj().Name()
	}
	if mname == "Kind" {
	}
	notFound := func() (*methodCall, error) {
		return nil, fmt.Errorf("gofrontend: method %s not found on %s", mname, recvName)
	}
	// Defensive: an ambiguous selector has no denoted method object.
	// go/types rejects these during type-checking, so this is
	// unreachable in practice — but resolving one would mean silently
	// picking a candidate, which must never happen.
	if selection.Obj() == nil {
		return nil, fmt.Errorf("gofrontend: ambiguous selector %s on %s: no candidate chosen", mname, recvName)
	}
	var msel *types.Selection
	ms := types.NewMethodSet(selection.Recv())
	for i := 0; i < ms.Len(); i++ {
		if s := ms.At(i); s.Obj().Name() == mname {
			msel = s
			break
		}
	}
	if msel == nil {
		// go/types records Selection.Recv() as the UNADJUSTED receiver
		// type: a pointer-receiver method promoted via a VALUE-embedded
		// field is absent from methodset(Recv) — it lives only in
		// methodset(*Recv), and the call relies on gc's implicit & of
		// the receiver. When the receiver expression is addressable,
		// take the selection from the pointer method set; the shared tail
		// below adapts along the same embedding path, and methodRefNode's
		// implicitAddrOf supplies the &. Otherwise fail fast with the named scope
		// error — never a confusing "not found" for a method that
		// exists.
		pms := types.NewMethodSet(types.NewPointer(selection.Recv()))
		for i := 0; i < pms.Len(); i++ {
			if s := pms.At(i); s.Obj().Name() == mname {
				if !isAddressable(sel.X, e.info) {
					return nil, scopeErrorf("method call %s needs an implicit address-of or deref on the receiver (WS D)", mname)
				}
				msel = s
				break
			}
		}
		if msel == nil {
			return notFound()
		}
	}
	index := msel.Index()
	if len(index) < 2 {
		// A direct method would have hit methodIndex in the caller;
		// never silently re-resolve through a different path.
		return notFound()
	}
	path := index[:len(index)-1]
	// The declaring named type: walk the receiver type along the
	// adaptation path (embeddedFieldType substitutes type arguments for
	// instantiated receivers, e.g. GInner[int] under GOuter[int]).
	// P3-M9 (driver-harden, go-cmp lane): if the embedded field is an
	// INTERFACE (e.g. go-cmp's reporter embedding reporterIface), the
	// method is interface-promoted. Dispatch via the interface using
	// the ifaceFallback mechanism; the receiver is adapted via recvPath
	// in methodRefNode.
	fieldType := embeddedFieldType(selection.Recv(), path)
	if fieldType != nil {
		if _, ok := fieldType.Underlying().(*types.Interface); ok {
			// Verify the method is in the interface's method set.
			if msel := types.NewMethodSet(fieldType).Lookup(e.pkg, mname); msel == nil {
				return notFound()
			}
			return &methodCall{ifaceFallback: fieldType, subst: subst, recvPath: path}, nil
		}
	}
	dn := derefNamed(fieldType)
	if dn == nil {
		return notFound()
	}
	fi, ok := e.methodIndex[methodKey{dn.Obj(), mname}]
	if !ok {
		return notFound()
	}
	// The instantiation (if any) is keyed by the DECLARING receiver, not
	// the outer one: bindRecvParams binds the declaring origin's type
	// parameters from the walked type's arguments.
	if !fi.isTemplate {
		return &methodCall{decl: fi, subst: subst, recvPath: path}, nil
	}
	bind := make(map[*types.TypeParam]types.Type)
	var rawTargs []types.Type
	if err := bindRecvParams(bind, fi, dn); err != nil {
		return nil, err
	}
	rawTargs = append(rawTargs, typeList(dn.TypeArgs())...)
	mtps := fi.sig.TypeParams()
	if mtps.Len() > 0 {
		if len(methodTargs) != mtps.Len() {
			return nil, fmt.Errorf("gofrontend: generic method %s called without recorded type args", mname)
		}
		for i := 0; i < mtps.Len(); i++ {
			bind[mtps.At(i)] = methodTargs[i]
		}
		rawTargs = append(rawTargs, methodTargs...)
	}
	sub := &typeSubst{parent: subst, bind: bind}
	recvKey := substString(dn, sub)
	key := "M:" + recvKey + "." + mname
	name := prettyType(dn, sub) + "." + mname
	if len(methodTargs) > 0 {
		key += "|" + joinSubstKeys(methodTargs, sub)
		pretty := make([]string, len(methodTargs))
		for i, ta := range methodTargs {
			pretty[i] = prettyType(ta, sub)
		}
		name += "[" + strings.Join(pretty, ", ") + "]"
	}
	if !isConcreteKey(key) {
		return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of method %s", mname)
	}
	return &methodCall{decl: fi, subst: sub, key: key, name: name, needsInst: true,
		rawTargsForEmit: rawTargs, recvPath: path}, nil
}

// enumerateInstantiations computes the full monomorphization fixpoint: every
// (generic function, type args) and (method, receiver args, method args)
// the program needs. Roots come from info.Instances / info.Selections in
// non-generic code; nested instantiations inside generic bodies are found by
// walking each template with its substitution.
//
// Termination is guaranteed by go/types: instantiation cycles are type
// errors, so the graph is finite for every accepted program. A defensive cap
// guards against enumerator bugs (internal error, never silent).
// scanRoots collects generic-instantiation roots from one package's type
// info and files: generic-function calls and generic-method calls in
// non-generic code. The user file is scanned with its own info; imported
// synthetic packages are scanned with theirs (P3-M9 tier-5: vendored stdlib
// sources call generic helpers from non-generic code — package-level var
// initializers, __mr wrappers — which the user-file-only scan never sees,
// failing export with "not enumerated (internal error)"). Idents inside
// generic templates are skipped (found via the fixpoint, not as roots).
func (e *exporter) scanRoots(info *types.Info, files []*ast.File, tmplFuncs []*funcDeclInfo, enqueue func(*instantiation), newFuncInst func(*funcDeclInfo, []types.Type, *typeSubst) (*instantiation, error)) error {
	saved := e.info
	e.info = info
	defer func() { e.info = saved }()

	// idents inside generic templates: instantiations recorded there are
	// nested (found in the fixpoint), not roots.
	inTemplate := make(map[*ast.Ident]bool)
	for _, fi := range tmplFuncs {
		if !fi.isTemplate {
			continue
		}
		ast.Inspect(fi.fd, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				inTemplate[id] = true
			}
			return true
		})
	}
	// Sel ident -> SelectorExpr (method instantiations via info.Instances).
	selOf := make(map[*ast.Ident]*ast.SelectorExpr)
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				selOf[sel.Sel] = sel
			}
			return true
		})
	}

	// Roots: generic-function instantiations in non-generic code.
	for id, inst := range e.info.Instances {
		if inTemplate[id] {
			continue
		}
		handled := false
		if sel, ok := selOf[id]; ok {
			// P3-M9: distinguish method selections from package-qualified
			// generic functions (slices.Sort, maps.Keys, ...). go/types
			// records Selections only for method/field selections; a
			// package qualifier (sel.X is an Ident denoting the package)
			// resolves via the denoted func object, like a plain ident.
			isMethodSel := true
			if x, ok := sel.X.(*ast.Ident); ok {
				if _, isPkg := e.info.Uses[x].(*types.PkgName); isPkg {
					isMethodSel = false
				}
			}
			if isMethodSel {
				// Generic method via its Sel ident.
				selection, ok := e.info.Selections[sel]
				if !ok {
					return fmt.Errorf("gofrontend: no selection for %s", sel.Sel.Name)
				}
				// P3-M7: synthetic multi-result methods (bytes.Buffer.Read, etc.)
				// fail fast at the call site (WS D) — they are not in methodIndex
				// (skipped during synthetic import) and must not be resolved here.
				// The named M7 scope error is reported during main export.
				if f, ok := selection.Obj().(*types.Func); ok && e.isMultiResultSyntheticFailFast(f) {
					continue
				}
				mc, err := e.resolveMethodCall(sel, selection, typeList(inst.TypeArgs), nil)
				if err != nil {
					return err
				}
				if mc.needsInst {
					enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
				}
				handled = true
			}
		}
		if handled {
			continue
		}
		fn, ok := denotedFunc(e.info, id)
		if !ok {
			continue // type instantiation (only the type table needs it)
		}
		// P3-M9 (tier-5b): synthetic multi-result package funcs (e.g.
		// fips140/ecdh.GenerateKey) are skipped during synthetic import
		// (never in funcByObj); the Appendix M desugar intercepts call
		// sites via the table. Skip them here too.
		if e.isMultiResultSyntheticFailFast(fn) {
			continue
		}
		fi, ok := e.funcByObj[fn]
		if !ok {
			// P3-M9: fallback to (package path, func name) lookup for
			// synthetic generic functions. Synthetic packages are
			// type-checked in isolation, so the *types.Func pointer from
			// the importing package differs from the one registered at
			// import time.
			if e.synthFuncIDs != nil && fn.Pkg() != nil {
				key := fn.Pkg().Path() + "." + fn.Name()
				fi, ok = e.synthFuncIDs[key]
			}
		}
		if !ok {
			// WS G (GAP-GENERIC-MR): a generic instantiation of a disk
			// multi-result stub (transformDiskMultiResult) is redirected
			// to the generic __-helper: the stub is skipped at import
			// (never in funcByObj); the helper carries the real body and
			// is instantiated for the call-site type args.
			if hfi, ok2 := e.diskMRHelperFi(fn); ok2 {
				fi, ok = hfi, true
			}
		}
		if !ok {
			pkgPath := ""
			if fn.Pkg() != nil {
				pkgPath = fn.Pkg().Path()
			}
			return fmt.Errorf("gofrontend: call to unknown function %s (pkg %s)", fn.Name(), pkgPath)
		}
		if !fi.isTemplate {
			continue // non-generic: emitted from its declaration
		}
		ni, err := newFuncInst(fi, typeList(inst.TypeArgs), nil)
		if err != nil {
			// P3-M9 (generics isNaN fix): if the type args are unbound,
			// this is not a true root — it's a nested call inside a generic
			// template that inTemplate missed (pointer mismatch). Skip it;
			// the fixpoint will find it when the outer generic is instantiated.
			if strings.Contains(err.Error(), "unbound type parameter") {
				continue
			}
			return err
		}
		enqueue(ni)
	}
	// Roots: method calls in non-generic code.
	for sel, selection := range e.info.Selections {
		if inTemplate[sel.Sel] {
			continue
		}
		if selection.Kind() != types.MethodVal {
			continue
		}
		// P3-M2b (ADR-0002): sync primitive method calls lower to syncop
		// nodes, never through resolveMethodCall/methodIndex (the
		// synthetic methods have no funcdecl to instantiate).
		if syntheticPrimitiveRecv(selection.Recv()) != nil {
			continue
		}
		// real-reflect: promoted sync primitive methods (e.g.
		// funcLookupCache.Lock()) also lower to syncop, never through
		// resolveMethodCall/methodIndex.
		if nt, _ := promotedSyncPrimitive(selection); nt != nil {
			continue
		}
		// P3-M3 (WS D, ADR-0003): interface method calls lower to
		// ifacemethod nodes, never through resolveMethodCall/methodIndex
		// (dispatch is dynamic via the itab; nothing to instantiate).
		if recvIface(selection.Recv()) != nil {
			continue
		}
		// P3-M7: synthetic multi-result methods (bytes.Buffer.Read, etc.)
		// fail fast at the call site (WS D) — they are not in methodIndex
		// (skipped during synthetic import) and must not be resolved here.
		if f, ok := selection.Obj().(*types.Func); ok && e.isMultiResultSyntheticFailFast(f) {
			continue
		}
		// mrRuntimeStack: the synthetic runtime stack-inspection methods
		// ((*Frames).Next, (*Func).Name/Entry/FileLine) lower to
		// intrinsic wire nodes, never through resolveMethodCall/
		// methodIndex (the synthetic runtime overlay declares no
		// funcdecls to instantiate).
		if _, isRt := runtimeStackMethod(selection); isRt {
			continue
		}
		var mtargs []types.Type
		if ii, ok := e.info.Instances[sel.Sel]; ok {
			mtargs = typeList(ii.TypeArgs)
		}
		mc, err := e.resolveMethodCall(sel, selection, mtargs, nil)
		if err != nil {
			// real-reflect: the scan also covers imported packages
			// (tier-5). A method that cannot be resolved here (e.g. a
			// promoted method from a dependency whose methods are not
			// yet in methodIndex) is not a generic-instantiation root;
			// skip it — the body export will resolve (or fail fast
			// with a proper error) when the method is actually
			// emitted. Failing the whole enumeration on such methods
			// blocks real-reflect compilation.
			continue
		}
		if mc.needsInst {
			enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
		}
	}
	return nil
}

// newFuncInstantiation builds the instantiation record for a generic
// function template with the given type arguments (factored out of
// enumerateInstantiations so the method-table drain can reuse it).
func (e *exporter) newFuncInstantiation(fi *funcDeclInfo, targs []types.Type, subst *typeSubst) (*instantiation, error) {
	key := instKeyForFunc(fi, targs, subst)
	if !isConcreteKey(key) {
		return nil, fmt.Errorf("gofrontend: unbound type parameter in instantiation of %s", fi.obj.Name())
	}
	sub2 := &typeSubst{parent: subst, bind: bindParams(fi.sig.TypeParams(), targs)}
	return &instantiation{
		decl: fi, subst: sub2, rawTargs: targs,
		key: key, name: instNameForFunc(fi, targs, subst),
	}, nil
}

// seedMethodTableRoots enqueues monomorphizations for the template
// methods of every concrete generic instantiation described so far
// (P3-M9 instantiation method tables). Interface dispatch lowers to the
// itab built from the wire method tables, so each template method needs
// an emitted instantiation — exactly as if it had been called directly.
// The existing fixpoint below discovers nested instantiations inside
// these bodies; the existing prepare/emission loops handle the rest.
func (e *exporter) seedMethodTableRoots(enqueue func(*instantiation)) error {
	for _, t := range e.typeList {
		if t == nil || !isGenericInstantiation(t) {
			continue
		}
		ms := types.NewMethodSet(t)
		for i := 0; i < ms.Len(); i++ {
			sel := ms.At(i)
			mobj, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}
			inst, err := e.buildMethodTableInst(t, sel, mobj)
			if err != nil {
				return err
			}
			if inst != nil {
				enqueue(inst)
			}
		}
	}
	return nil
}

// buildMethodTableInst computes the monomorphization for one template
// method of a concrete generic instantiation t (sel/mobj from
// types.NewMethodSet(t)). It covers the three template shapes
// methodTableEntries encounters:
//
//  1. direct template hit in funcByObj (generic methods keep their
//     origin object — type params cannot be substituted);
//  2. multi-result template mapped to its generic __-helper
//     (helperMapped): the helper is a plain generic function
//     instantiated with the receiver's type arguments;
//  3. instantiated method-set copies that miss funcByObj (the common
//     case): the template is resolved via methodIndex, extended to
//     promoted methods by walking the selection's embedded path
//     (mirrors resolvePromotedMethodCall).
//
// It returns (nil, nil) when the method cannot be monomorphized here:
// generic methods with their own type params (no type arguments are
// available from the method table), unresolvable templates, or
// non-concrete keys. Callers keep the old skip-and-mark behavior then.
func (e *exporter) buildMethodTableInst(t types.Type, sel *types.Selection, mobj *types.Func) (*instantiation, error) {
	mname := mobj.Name()
	// The declaring receiver: t itself for direct methods; for promoted
	// methods walk the embedded field path (embeddedFieldType substitutes
	// type arguments for instantiated receivers).
	recvT := t
	if index := sel.Index(); len(index) > 1 {
		recvT = embeddedFieldType(t, index[:len(index)-1])
		if recvT == nil {
			return nil, nil
		}
	}
	rn := derefNamed(recvT)
	if rn == nil || !isGenericInstantiation(rn) {
		return nil, nil
	}
	// Resolve the receiver's type arguments through the ambient
	// substitution (methodTableEntries can run inside an instantiation
	// body where the type mentions bound type parameters).
	recvTargs := make([]types.Type, 0, rn.TypeArgs().Len())
	for _, ta := range typeList(rn.TypeArgs()) {
		recvTargs = append(recvTargs, e.substType(ta))
	}
	// Identify the template: direct funcByObj hit, the multi-result
	// helper, or the methodIndex lookup for instantiated copies.
	fi, ok := e.funcByObj[mobj]
	helperMapped := false
	if !ok {
		if hfi, ok := e.multiResultHelperFunc(mobj); ok {
			fi, helperMapped = hfi, true
		} else if tfi := e.methodTableTemplate(rn, mname); tfi != nil {
			fi = tfi
		} else {
			return nil, nil
		}
	}
	if !fi.isTemplate {
		return nil, nil
	}
	if helperMapped {
		// The __-helper is a plain generic function parameterized like
		// the receiver; instantiate it with the receiver's type args
		// (mirrors the direct-call desugar's targsFromRecv). A
		// non-concrete key means "not monomorphizable here", not an
		// exporter bug.
		inst, err := e.newFuncInstantiation(fi, recvTargs, e.subst)
		if err != nil {
			if strings.Contains(err.Error(), "unbound type parameter") {
				return nil, nil
			}
			return nil, err
		}
		return inst, nil
	}
	if fi.sig.TypeParams().Len() > 0 {
		// Generic method: no type arguments available from the method
		// table — genuinely not monomorphizable (Go also forbids type
		// parameters on interface methods, so it can never dispatch).
		return nil, nil
	}
	// Method instantiation (mirrors resolveMethodCall): bind the
	// declaration's receiver type parameters to the concrete receiver's
	// type arguments.
	declRN := derefNamed(fi.recv.Type())
	if declRN == nil {
		return nil, fmt.Errorf("gofrontend: method %s has non-named declared receiver", fi.obj.Name())
	}
	declArgs := typeList(declRN.TypeArgs())
	if len(declArgs) != len(recvTargs) {
		return nil, fmt.Errorf("gofrontend: receiver arity mismatch for method %s", fi.obj.Name())
	}
	bind := make(map[*types.TypeParam]types.Type)
	for i := range declArgs {
		tp, ok := declArgs[i].(*types.TypeParam)
		if !ok {
			continue // concrete in the declaration; nothing to bind
		}
		bind[tp] = recvTargs[i]
	}
	sub := &typeSubst{parent: e.subst, bind: bind}
	recvKey := substString(recvT, sub)
	key := "M:" + recvKey + "." + mname
	if !isConcreteKey(key) {
		return nil, nil
	}
	return &instantiation{
		decl: fi, subst: sub, rawTargs: recvTargs,
		key: key, name: prettyType(recvT, sub) + "." + mname,
	}, nil
}

// methodTableTemplate resolves the template declaration for a method-set
// member that missed funcByObj (instantiated copies never hit it) and has
// no multi-result helper. It mirrors resolveMethodCall's methodIndex
// lookup (including the P3-M8b pointer-identity fallback). rn is the
// declaring receiver's named type (already walked past promotion).
func (e *exporter) methodTableTemplate(rn *types.Named, mname string) *funcDeclInfo {
	if fi, ok := e.methodIndex[methodKey{rn.Obj(), mname}]; ok {
		return fi
	}
	return e.findMethodByName(rn.Obj(), mname)
}

// ensureFuncInstantiation returns the object id for a generic function
// instantiation, creating it on demand when absent from instByKey. A
// newly created instantiation is prepared immediately (its instID is
// referenced by the caller) and queued on e.mtPending for body emission
// by drainMethodTablePending.
func (e *exporter) ensureFuncInstantiation(fi *funcDeclInfo, targs []types.Type) (int, error) {
	inst, err := e.newFuncInstantiation(fi, targs, e.subst)
	if err != nil {
		return 0, err
	}
	// P3-M9: the desugar can run during synthetic import, before
	// enumerateInstantiations populates instByKey.
	if e.instByKey == nil {
		e.instByKey = make(map[string]*instantiation)
	}
	if existing, ok := e.instByKey[inst.key]; ok {
		return existing.instID, nil
	}
	if err := e.prepareInstantiation(inst); err != nil {
		return 0, err
	}
	e.instByKey[inst.key] = inst
	e.mtPending = append(e.mtPending, inst)
	return inst.instID, nil
}

// helperCallTargs extracts the type arguments for a generic __-helper
// call from the original call expression: instance type args for Ident
// calls, the receiver's type args (plus the method's own, if any) for
// method calls — resolved through the ambient substitution.
func (e *exporter) helperCallTargs(c *ast.CallExpr) ([]types.Type, error) {
	var targs []types.Type
	fun := c.Fun
	// P4: generic instantiation calls (foo[T](x), pkg.Fn[T](x)) present
	// Fun as *ast.IndexExpr / *ast.IndexListExpr — unwrap to the
	// underlying ident/selector before classifying (cf. unwrapSelector
	// in exportCall).
	if sel, ok := unwrapSelector(fun); ok {
		fun = sel
	} else if id := callTargetIdent(fun); id != nil {
		fun = id
	}
	if id, ok := fun.(*ast.Ident); ok {
		targs = targsFromInstances(e, id)
	} else if sel, ok := fun.(*ast.SelectorExpr); ok {
		// Package-qualified call (pkg.Fn): not a method selection;
		// type args come from the Sel ident's instance info.
		if x, ok := sel.X.(*ast.Ident); ok {
			if _, isPkg := e.info.Uses[x].(*types.PkgName); isPkg {
				targs = targsFromInstances(e, sel.Sel)
			}
		}
		if targs == nil {
			selection, ok := e.info.Selections[sel]
			if !ok {
				return nil, fmt.Errorf("gofrontend: no selection for helper method call")
			}
			targs = targsFromRecv(selection.Recv())
			if ii, ok := e.info.Instances[sel.Sel]; ok {
				targs = append(targs, typeList(ii.TypeArgs)...)
			}
		}
	} else {
		return nil, fmt.Errorf("gofrontend: helper call has non-ident/selector fun %T", c.Fun)
	}
	out := make([]types.Type, len(targs))
	for i, ta := range targs {
		out[i] = e.substType(ta)
	}
	return out, nil
}

// drainMethodTablePending emits the bodies of on-demand method-table
// instantiations created during body export (e.mtPending): nested
// instantiations are discovered with the same fixpoint as
// enumerateInstantiations, then each new instantiation is prepared and
// emitted. Repeats until no new pending instantiations appear
// (exportInstantiation can describe new types, whose method tables can
// queue more).
func (e *exporter) drainMethodTablePending(doc *Doc) error {
	for len(e.mtPending) > 0 {
		batch := e.mtPending
		e.mtPending = nil
		seen := make(map[string]bool, len(batch))
		order := make([]*instantiation, 0, len(batch))
		var nested []*instantiation
		for _, inst := range batch {
			seen[inst.key] = true
			order = append(order, inst)
		}
		enqueue := func(inst *instantiation) {
			if seen[inst.key] {
				return
			}
			seen[inst.key] = true
			if _, ok := e.instByKey[inst.key]; ok {
				return
			}
			e.instByKey[inst.key] = inst
			nested = append(nested, inst)
			order = append(order, inst)
		}
		for i := 0; i < len(order); i++ {
			if len(order) > 100000 {
				return fmt.Errorf("gofrontend: instantiation explosion (%d): internal error", len(order))
			}
			if err := e.findNested(order[i], enqueue, e.newFuncInstantiation); err != nil {
				return err
			}
		}
		// The batch was prepared at creation (its instIDs are already
		// referenced by method tables); only nested discoveries need it.
		for _, inst := range nested {
			if err := e.prepareInstantiation(inst); err != nil {
				return err
			}
		}
		for _, inst := range order {
			f, err := e.exportInstantiation(inst)
			if err != nil {
				return err
			}
			doc.Funcs = append(doc.Funcs, f)
		}
	}
	return nil
}

func (e *exporter) enumerateInstantiations(files []*ast.File, funcs []*funcDeclInfo) ([]*instantiation, error) {
	// Roots are collected from the user files and from every imported
	// synthetic package's own body (see scanRoots).
	seen := make(map[string]*instantiation)
	// P3-M9: on-demand instantiations created during synthetic import
	// (generic __-helper desugars) pre-seed the dedup set so roots are
	// not duplicated.
	for key, inst := range e.instByKey {
		seen[key] = inst
	}
	var order []*instantiation
	enqueue := func(inst *instantiation) {
		if _, ok := seen[inst.key]; ok {
			return
		}
		seen[inst.key] = inst
		order = append(order, inst)
	}
	newFuncInst := e.newFuncInstantiation

	if err := e.scanRoots(e.info, files, funcs, enqueue, newFuncInst); err != nil {
		return nil, err
	}
	// P3-M9 (tier-5): synthetic packages root their own generic
	// instantiations (see scanRoots). Deterministic order.
	// WS G (P3-M9): importedSynthetic also holds disk-built packages;
	// resolve through importedPackage (synthetic or disk).
	spaths := make([]string, 0, len(e.importedSynthetic))
	for p := range e.importedSynthetic {
		spaths = append(spaths, p)
	}
	sort.Strings(spaths)
	for _, p := range spaths {
		sp, err := e.importedPackage(p)
		if err != nil {
			return nil, err
		}
		var sfuncs []*funcDeclInfo
		for _, fi := range e.funcByObj {
			if fi.synthPkg == sp {
				sfuncs = append(sfuncs, fi)
			}
		}
		if err := e.scanRoots(sp.info, sp.files, sfuncs, enqueue, newFuncInst); err != nil {
			return nil, err
		}
	}
	// Fixpoint: nested instantiations inside generic bodies.
	// P3-M9 (instantiation method tables): seed monomorphizations for
	// the template methods of concrete generic instantiations first, so
	// the fixpoint discovers nested instantiations inside their bodies.
	if err := e.seedMethodTableRoots(enqueue); err != nil {
		return nil, err
	}
	for i := 0; i < len(order); i++ {
		if len(order) > 100000 {
			return nil, fmt.Errorf("gofrontend: instantiation explosion (%d): internal error", len(order))
		}
		if err := e.findNested(order[i], enqueue, newFuncInst); err != nil {
			return nil, err
		}
	}
	// Deterministic emission order (TestDeterministic).
	sort.Slice(order, func(i, j int) bool { return order[i].name < order[j].name })
	return order, nil
}

// findNested walks one instantiation's template body for nested generic
// calls and method calls, enqueueing their (substituted) instantiations.
func (e *exporter) findNested(inst *instantiation, enqueue func(*instantiation), newFuncInst func(*funcDeclInfo, []types.Type, *typeSubst) (*instantiation, error)) error {
	// P3-M9 (tier-1a): a synthetic template's body lives in the synthetic
	// package's AST — swap to its fset/src/info/pkg (like
	// exportInstantiation), not the user's, so nested generic calls
	// resolve against the right type info.
	sp := inst.decl.synthPkg
	var savedFset *token.FileSet
	var savedSrc []byte
	var savedInfo *types.Info
	var savedPkg *types.Package
	var savedFileIndex map[*token.File]int
	var savedSrcByFile map[*token.File][]byte
	if sp != nil {
		savedFset, savedSrc, savedInfo, savedPkg, savedFileIndex, savedSrcByFile = e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile
		e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile = sp.fset, sp.srcForDecl(inst.decl.fd), sp.info, sp.pkg, nil, nil
		defer func() {
			e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile = savedFset, savedSrc, savedInfo, savedPkg, savedFileIndex, savedSrcByFile
		}()
	}
	subst := inst.subst
	var err error
	ast.Inspect(inst.decl.fd, func(n ast.Node) bool {
		if err != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			id := callTargetIdent(n.Fun)
			if id == nil {
				return true
			}
			ii, ok := e.info.Instances[id]
			if !ok {
				return true
			}
			fn, ok := denotedFunc(e.info, id)
			if !ok {
				return true // type instantiation
			}
			// P3-M9 (tier-5b): synthetic multi-result funcs are not in
			// funcByObj (skipped during import); the desugar handles them.
			if e.isMultiResultSyntheticFailFast(fn) {
				return true
			}
			fi, ok := e.funcByObj[fn]
			if !ok {
				// P3-M9: fallback to (package path, func name) for synthetic
				// funcs (type-checked in isolation, pointers not shared).
				if e.synthFuncIDs != nil && fn.Pkg() != nil {
					key := fn.Pkg().Path() + "." + fn.Name()
					fi, ok = e.synthFuncIDs[key]
				}
			}
			if !ok {
				err = fmt.Errorf("gofrontend: call to unknown function %s", fn.Name())
				return false
			}
			if !fi.isTemplate {
				return true
			}
			// P3-M9 (generics isNaN fix): apply outer substitution to nested
			// call's type args. When Less[float64] calls isNaN(x) with x: T,
			// ii.TypeArgs contains T (unbound). Substitute T -> float64 first,
			// otherwise newFuncInst fails with "unbound type parameter".
			// Note: go/types object identity for TypeParams is not stable
			// across the synthetic package's type-checking passes, so we
			// match by name, not by pointer.
			targs := typeList(ii.TypeArgs)
			if subst != nil {
				for i, ta := range targs {
					if tp, ok := ta.(*types.TypeParam); ok {
						if resolved := resolveByName(subst, tp.Obj().Name()); resolved != nil {
							targs[i] = resolved
						}
					}
				}
			}
			ni, nerr := newFuncInst(fi, targs, subst)
			if nerr != nil {
				err = nerr
				return false
			}
			enqueue(ni)
		case *ast.SelectorExpr:
			selection, ok := e.info.Selections[n]
			if !ok || selection.Kind() != types.MethodVal {
				return true
			}
			// P3-M2b (ADR-0002): sync primitives bypass the method index
			// (same as the roots loop above).
			if syntheticPrimitiveRecv(selection.Recv()) != nil {
				return true
			}
			// P3-M3 (WS D, ADR-0003): interface method calls need no
			// instantiation (same as the roots loop above).
			if recvIface(selection.Recv()) != nil {
				return true
			}
			var mtargs []types.Type
			if ii, ok := e.info.Instances[n.Sel]; ok {
				mtargs = typeList(ii.TypeArgs)
			}
			mc, merr := e.resolveMethodCall(n, selection, mtargs, subst)
			if merr != nil {
				err = merr
				return false
			}
			if mc.needsInst {
				enqueue(&instantiation{decl: mc.decl, subst: mc.subst, rawTargs: mc.rawTargsForEmit, key: mc.key, name: mc.name})
			}
		case *ast.Ident:
			// Generic function in value position (e.g. min[T] passed as an
			// argument inside a generic body): go/types records it in
			// info.Instances, but the CallExpr case above only sees call
			// targets, so the instantiation would never be enqueued and
			// export later fails with "not enumerated (internal error)".
			// Method idents (Sel of a SelectorExpr) are excluded: method
			// calls/values go through the SelectorExpr case.
			ii, ok := e.info.Instances[n]
			if !ok {
				return true
			}
			fn, ok := denotedFunc(e.info, n)
			if !ok {
				return true
			}
			fi, ok := e.funcByObj[fn]
			if !ok {
				// Fallback to (package path, func name) for functions
				// from packages type-checked in isolation.
				if e.synthFuncIDs != nil && fn.Pkg() != nil {
					key := fn.Pkg().Path() + "." + fn.Name()
					fi, ok = e.synthFuncIDs[key]
				}
			}
			if !ok || !fi.isTemplate || fi.recv != nil {
				return true
			}
			// P3-M9 (generics isNaN fix): apply outer substitution by name
			// (see CallExpr case above).
			targs2 := typeList(ii.TypeArgs)
			if subst != nil {
				for i, ta := range targs2 {
					if tp, ok := ta.(*types.TypeParam); ok {
						if resolved := resolveByName(subst, tp.Obj().Name()); resolved != nil {
							targs2[i] = resolved
						}
					}
				}
			}
			ni, nerr := newFuncInst(fi, targs2, subst)
			if nerr != nil {
				err = nerr
				return false
			}
			enqueue(ni)
		}
		return true
	})
	return err
}

// synthesizeUserPairType creates a named struct type for a user-defined
// multi-result function's results (P3-M9 WS D). The struct has fields
// f0, f1, ... with the result types in order. The name is unique per
// function to avoid collisions.
func (e *exporter) synthesizeUserPairType(fi *funcDeclInfo) (*types.Named, []string, error) {
	sig := fi.sig
	// P3-M9 (generics, funcs): fail fast if called for a generic template.
	// Pair structs for templates would have typeparam field types, leaking
	// "typeparam" kinds into the type table. Templates are skipped in
	// exportFuncDecl/exportSyntheticBodies (isTemplate check), so this
	// should never happen — but guard here for safety.
	if sig.TypeParams() != nil && sig.TypeParams().Len() > 0 {
		return nil, nil, fmt.Errorf("gofrontend: synthesizeUserPairType called for generic template %s (should be skipped)", fi.obj.Name())
	}
	n := sig.Results().Len()
	fields := make([]*types.Var, n)
	fieldNames := make([]string, n)
	for i := 0; i < n; i++ {
		fname := fmt.Sprintf("f%d", i)
		fieldNames[i] = fname
		// Use the result variable's type; fall back to the tuple type.
		rt := sig.Results().At(i).Type()
		fields[i] = types.NewField(token.NoPos, e.pkg, fname, rt, false)
	}
	st := types.NewStruct(fields, nil)
	// Unique name: __wsd_pair_<pkg>_<func> (methods include receiver).
	name := fmt.Sprintf("__wsd_pair_%s", fi.obj.Name())
	if fi.recv != nil {
		rt := fi.recv.Type()
		if pt, ok := rt.(*types.Pointer); ok {
			rt = pt.Elem()
		}
		if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
			name = fmt.Sprintf("__wsd_pair_%s_%s", nt.Obj().Name(), fi.obj.Name())
		}
	}
	// Ensure uniqueness in case of collisions (e.g., generic instantiations).
	base := name
	for i := 1; ; i++ {
		if e.pkg.Scope().Lookup(name) == nil {
			break
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
	tname := types.NewTypeName(token.NoPos, e.pkg, name, nil)
	named := types.NewNamed(tname, st, nil)
	e.pkg.Scope().Insert(tname)
	return named, fieldNames, nil
}

// exportFuncDecl emits a non-generic top-level function or method. Methods
// export their receiver as params[0] (the wire "signature" matches).
func (e *exporter) exportFuncDecl(fi *funcDeclInfo) (*Node, error) {
	fd := fi.fd
	// WS G (P3-M9): skip disk-package multi-result stubs. The stub
	// (original name, multi-result signature) exists for type-checking
	// and method resolution; the desugar rewrites all calls to the
	// __-helper. The v1 exporter cannot emit multi-result functions.
	if fi.obj != nil && fi.obj.Pkg() != nil {
		pkgPath := fi.obj.Pkg().Path()
		name := fi.obj.Name()
		if !strings.HasPrefix(name, "__") {
			recvType := ""
			if fi.recv != nil {
				// Get receiver base type name.
				rt := fi.recv.Type()
				if pt, ok := rt.(*types.Pointer); ok {
					rt = pt.Elem()
				}
				if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
					recvType = nt.Obj().Name()
				}
			}
			if lookupDiskMR(pkgPath, name, recvType) != nil {
				// Stub: skip export (desugar handles all calls).
				return nil, nil
			}
		}
	}
	if fd.Body == nil {
		// real-reflect (Phase 1): internal/abi FuncPCABI0/FuncPCABIInternal
		// are gc compile intrinsics declared bodyless. Emit a funcdecl
		// whose body is the "abifuncpc" wire node, so the function table
		// stays dense and value-position uses work through the normal
		// func-value path. Call sites are still intercepted directly
		// (see exportAbiFuncPCIntrinsic).
		if fi.obj != nil && isAbiFuncPCIntrinsicFunc(fi.obj) {
			return e.exportAbiFuncPCDecl(fi)
		}
		// real-reflect (Phase 3): a bodyless declaration in an imported
		// (synthetic/disk) package is always a gc-backend/asm/linkname
		// implementation — gc itself would fail to link it without the
		// backend. internal/abi FuncPC* intrinsics keep their dedicated
		// wire node (above); //go:linkname declarations keep theirs
		// (below); every other bodyless imported declaration (runtime/sys
		// intrinsics like GetCallerPC, //go:noescape asm stubs like
		// maps.memHashAES, plain stubs like
		// maps.checkMasksAndShiftsAlignment) emits a funcdecl whose body
		// is a fail-fast trap (see exportIntrinsicTrapDecl). Scoped to
		// imported declarations (fi.synthPkg != nil); user code keeps the
		// historical "without body" scope error via exportLinknameDecl.
		if fi.obj != nil && fi.synthPkg != nil {
			if symbol, lerr := linknameDirective(fd, funcPkgPath(e, fi)); lerr != nil {
				return nil, lerr
			} else if symbol != "" && (fi.isTemplate || fi.sig.Results().Len() <= 1) {
				return e.exportLinknameDecl(fi)
			}
			// Multi-result linkname stubs in imported packages (reflect's
			// badlinkname.go: "Do not call them in this package") cannot
			// use the linkname wire shape (frontend/v1: at most one
			// result). The fail-fast trap is unreachable in practice.
			return e.exportIntrinsicTrapDecl(fi)
		}
		// P3-M4 (WS C, ADR-0004 D1): a bodyless function carrying
		// //go:linkname binds to an intrinsic by symbol name.
		return e.exportLinknameDecl(fi)
	}
	params := []*Node{}
	if fi.recv != nil {
		rp, err := e.exportParamVar(fd, fi.recv)
		if err != nil {
			return nil, err
		}
		params = append(params, rp)
	}
	pp, err := e.exportSignatureVars(fd.Type, fi.sig.Params())
	if err != nil {
		return nil, err
	}
	params = append(params, pp...)
	results, err := e.exportSignatureVars(fd.Type, fi.sig.Results())
	if err != nil {
		return nil, err
	}
	// Save the original results (with named result vars) before the
	// multi-result transform below replaces them with the pair.
	origResults := results
	// P3-M9 (WS D): user-defined multi-result functions are automatically
	// transformed to pair-returning form. Synthesize a pair struct type,
	// change the signature to return it, and transform `return a, b` in
	// the body. This extends the WS D pair-desugar (previously synthetic
	// only) to user code.
	var mrInfo *userMRInfo
	sigParams := fi.sig.Params()
	sigResults := fi.sig.Results()
	// P3-M9 (WS D): user-defined multi-result functions are transformed
	// to pair-returning form. Synthetic packages are included unless
	// table-driven (Gate-1 #2: regexp/syntax etc. have real Go source
	// with no static table coverage). Generic templates are skipped.
	synthTableDriven := fi.synthPkg != nil && isTableDrivenMR(fi)
	if len(results) > 1 && !fi.isTemplate && !synthTableDriven {
		// P3-M9 (WS D): user-defined multi-result functions are transformed
		// to pair-returning form. Synthetic packages are excluded (they use
		// the table-driven mechanism). Generic templates are skipped.
		// BUG-MODPROBE-010: the pair type is normally pre-registered
		// by registerUserMRPairs (before package var initializers are
		// exported); registerUserMRFunc reuses that entry and only
		// synthesizes when exportFuncDecl runs outside that flow.
		var rerr error
		mrInfo, rerr = e.registerUserMRFunc(fi)
		if rerr != nil {
			return nil, rerr
		}
		if mrInfo == nil {
			return nil, fmt.Errorf("gofrontend: no pair registration for multi-result func %s (exporter bug)", fi.obj.Name())
		}
		// New signature: same params, single pair result.
		pairVar := types.NewParam(token.NoPos, e.pkg, "", mrInfo.pairType)
		sigResults = types.NewTuple(pairVar)
		// Export a single result var for the pair type.
		pairResult, err := e.exportParamVar(fd.Type, pairVar)
		if err != nil {
			return nil, err
		}
		results = []*Node{pairResult}
	}
	sig, err := e.freshSigID(fi.recv, sigParams, sigResults, fi.sig.Variadic())
	if err != nil {
		return nil, err
	}
	// Set the current transform so return statements are rewritten.
	prevMR := e.currentMR
	if mrInfo != nil {
		e.currentMR = mrInfo
	}
	body, err := e.exportBlock(fd.Body)
	e.currentMR = prevMR
	if err != nil {
		return nil, err
	}
	// If this was a multi-result transform and the original results had
	// names, define them as locals at the start of the body so assignments
	// to named results work. The `return` packs them into the pair.
	if mrInfo != nil {
		var namedDecls []*Node
		for _, r := range origResults {
			name, _ := r.Fields["name"].(string)
			if name == "" {
				continue
			}
			oid, _ := r.Fields["obj"].(int)
			tid, _ := r.Fields["type"].(int)
			// Define as a local var (no initializer; zero value).
			namedDecls = append(namedDecls, e.node("vardecl", fd.Type, map[string]any{
				"names": []string{name}, "objs": []int{oid},
				"define": true, "type": tid, "values": []*Node{},
			}))
		}
		if len(namedDecls) > 0 {
			stmts := body.Fields["stmts"].([]*Node)
			body = e.node("block", fd.Body, map[string]any{
				"stmts": append(namedDecls, stmts...),
			})
		}
	}
	fields := map[string]any{
		"name": fi.obj.Name(), "obj": fi.funcID, "sig": sig, "params": params, "body": body,
		// Cross-package linkname resolution (Go->Go direction) binds every
		// compiled function under pkgpath.name; the path is additive.
		"pkgpath": funcPkgPath(e, fi),
	}
	if len(results) > 0 {
		fields["results"] = results
	}
	return e.node("funcdecl", fd, fields), nil
}

// linknameDirective scans fd's doc comment for gc's //go:linkname
// directive ("//go:linkname <local> [<symbol>]"). Returns "" when absent.
// gc allows the symbol to be omitted: the target defaults to the
// package's own default object symbol (pkgPath + "." + local), which
// marks the local symbol as referenceable by that name from another
// package — the internal/godebug <-> runtime split uses exactly this
// form (godebug declares "//go:linkname setUpdate", runtime provides it
// via "//go:linkname godebug_setUpdate internal/godebug.setUpdate").
// A malformed directive or a local name that does not match the declared
// function is a scope error (gc rejects both at compile time).
func linknameDirective(fd *ast.FuncDecl, pkgPath string) (string, error) {
	if fd.Doc == nil {
		return "", nil
	}
	for _, c := range fd.Doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if !strings.HasPrefix(text, "go:linkname ") {
			continue
		}
		fields := strings.Fields(text[len("go:linkname "):])
		if len(fields) < 1 || len(fields) > 2 {
			return "", scopeErrorf("//go:linkname on func %s must have one or two fields (local [symbol])", fd.Name.Name)
		}
		if fields[0] != fd.Name.Name {
			return "", scopeErrorf("//go:linkname local name %q does not match func %s", fields[0], fd.Name.Name)
		}
		if len(fields) == 2 {
			return fields[1], nil
		}
		// Single-field form: the package's default object symbol, mirroring
		// gc's objabi.PathToPrefix(pkgpath) + "." + local (PathToPrefix is
		// the identity for paths needing no escaping; graalgo's symbol
		// namespace uses raw import paths throughout, e.g.
		// "internal/poll.runtime_pollOpen").
		return pkgPath + "." + fields[0], nil
	}
	return "", nil
}

// funcPkgPath returns the import path of the package that declares fi,
// for linkname default symbols and the funcdecl "pkgpath" wire field
// (cross-package linkname resolution binds every compiled function under
// pkgpath.name). The object's own package is authoritative; the
// exporter's current package is the fallback (user file: "main").
func funcPkgPath(e *exporter, fi *funcDeclInfo) string {
	if fi != nil && fi.obj != nil && fi.obj.Pkg() != nil {
		return fi.obj.Pkg().Path()
	}
	if e != nil && e.pkg != nil {
		return e.pkg.Path()
	}
	return ""
}

// exportLinknameDecl emits a bodyless function (P3-M4, WS C, ADR-0004 D1)
// as a funcdecl with no "body". A bodyless function carrying //go:linkname
// gets the additive "linkname" field and the factory builds a
// LinknameRootNode dispatching by symbol name. A bodyless function WITHOUT
// the directive (typically an assembly implementation in a .s file that
// graalgo does not process) gets the additive "unimplemented" field; the
// factory builds a stub that panics if called. This is general: it applies
// uniformly to all bodyless functions, preserving their type identity so
// references and value positions work, while failing loudly (never
// silently) if the unimplemented function is actually invoked.
func (e *exporter) exportLinknameDecl(fi *funcDeclInfo) (*Node, error) {
	fd := fi.fd
	pkgPath := funcPkgPath(e, fi)
	symbol, err := linknameDirective(fd, pkgPath)
	if err != nil {
		return nil, err
	}
	// P3-M9 (RC-6): //go:linkname on methods is allowed (previously M4
	// scope). The receiver is passed as the first arg; intrinsics that
	// don't need it (e.g. fmtstate.PrinterState methods, which operate on
	// the thread-local Printer) ignore it.
	if fi.isTemplate && symbol != "" {
		return nil, scopeErrorf("//go:linkname on generic func %s is not in M4 scope", fd.Name.Name)
	}
	params, err := e.exportSignatureVars(fd.Type, fi.sig.Params())
	if err != nil {
		return nil, err
	}
	results, err := e.exportSignatureVars(fd.Type, fi.sig.Results())
	if err != nil {
		return nil, err
	}
	sig, err := e.freshSigID(nil, fi.sig.Params(), fi.sig.Results(), fi.sig.Variadic())
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"name": fi.obj.Name(), "obj": fi.funcID, "sig": sig, "params": params,
		"pkgpath": pkgPath,
	}
	if len(results) > 0 {
		fields["results"] = results
	}
	if symbol == "" {
		// No //go:linkname: assembly-implemented (or otherwise
		// bodyless) function. Mark unimplemented; the backend panics
		// if called. The multi-result WS D restriction does not
		// apply: no body is built.
		fields["unimplemented"] = true
	} else {
		if len(results) > 1 {
			return nil, scopeErrorf("func %s has %d results; at most one result is supported in frontend/v1 scope (WS D)", fd.Name.Name, len(results))
		}
		fields["linkname"] = symbol
	}
	return e.node("funcdecl", fd, fields), nil
}

// exportInstantiation emits one monomorphized instantiation: a funcdecl with
// "instof"/"typeargs", an "inst" object entry, and fresh object ids for the
// template's variables (the *types.Var objects are shared across
// instantiations).
//
// The "inst" object entry (ids) is assigned by prepareInstantiation, which
// runs for all instantiations before any body is emitted: call sites in
// already-emitted bodies resolve to these ids.
func (e *exporter) prepareInstantiation(inst *instantiation) error {
	// Scope substitution to this instantiation.
	savedSubst := e.subst
	e.subst = inst.subst
	// P3-M9 (generics, issue48344): do NOT reset substTypeIDs here.
	// The memo is keyed by substituted (canonicalized) type, so entries
	// are valid across instantiations — the same canonical *Foo[int]
	// must map to the same wire id everywhere. Resetting it per
	// instantiation wipes the in-progress entry for a type whose
	// describeType is on the stack, and the on-demand method-table
	// path (methodTableEntries -> methodTableOnDemand ->
	// prepareInstantiation -> freshSigID -> typeID) then re-describes
	// the same type forever: describe, method table, instantiate,
	// receiver typeID, describe, ... (stack overflow). The subst
	// scoping above is still required; only the memo reset is removed.
	// P3-M9 (x509): but ensure the memo is non-nil — synthetic machinery
	// nils subst/substTypeIDs during synthetic emission, and a generic
	// helper instantiation via synthHelperCallNode can reach typeID with
	// subst != nil and a nil memo (nil-map write panic).
	if e.subst != nil && e.substTypeIDs == nil {
		e.substTypeIDs = make(map[types.Type]int)
	}
	defer func() {
		e.subst = savedSubst
	}()

	var recv *types.Var
	if inst.decl.recv != nil {
		recv = inst.decl.recv
	}
	sigID, err := e.freshSigID(recv, inst.decl.sig.Params(), inst.decl.sig.Results(), inst.decl.sig.Variadic())
	if err != nil {
		return err
	}
	targIDs := make([]int, len(inst.rawTargs))
	for i, ta := range inst.rawTargs {
		id, err := e.typeID(ta)
		if err != nil {
			return err
		}
		targIDs[i] = id
	}
	instID := len(e.objects)
	e.objects = append(e.objects, ObjectEnt{
		Kind: "inst", Name: inst.name, Type: sigID,
		InstOf: inst.decl.funcID, TypeArgs: targIDs,
	})
	inst.instID = instID
	inst.sigID = sigID
	inst.targIDs = targIDs
	return nil
}

func (e *exporter) exportInstantiation(inst *instantiation) (*Node, error) {
	fd := inst.decl.fd
	if fd.Body == nil {
		return nil, scopeErrorf("func %s without body", fd.Name.Name)
	}
	// P3-M9 (tier-1d): a synthetic template's body lives in the synthetic
	// package's AST — swap to its fset/src/info/pkg (the
	// exportSyntheticBodies swap), not the user's, so positions and type
	// info resolve against the right file.
	sp := inst.decl.synthPkg
	var savedFset *token.FileSet
	var savedSrc []byte
	var savedInfo *types.Info
	var savedPkg *types.Package
	var savedFileIndex map[*token.File]int
	var savedSrcByFile map[*token.File][]byte
	if sp != nil {
		savedFset, savedSrc, savedInfo, savedPkg, savedFileIndex, savedSrcByFile = e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile
		e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile = sp.fset, sp.srcForDecl(inst.decl.fd), sp.info, sp.pkg, nil, nil
	}
	// Scope object ids and substitution to this instantiation.
	savedObjs := e.objIDs
	if sp != nil {
		// Synthetic template bodies may reference synthetic package
		// globals (unique.uniqueMu): seed from the ambient table
		// (populated at importSyntheticPackage time) instead of starting
		// empty, or "used before declaration" fires.
		seeded := make(map[types.Object]int, len(savedObjs))
		for k, v := range savedObjs {
			seeded[k] = v
		}
		e.objIDs = seeded
	} else {
		e.objIDs = make(map[types.Object]int)
	}
	savedSubst := e.subst
	e.subst = inst.subst
	savedSubstTypes := e.substTypeIDs
	e.substTypeIDs = make(map[types.Type]int)
	defer func() {
		e.objIDs = savedObjs
		e.subst = savedSubst
		e.substTypeIDs = savedSubstTypes
		if sp != nil {
			e.fset, e.src, e.info, e.pkg, e.fileIndex, e.srcByFile = savedFset, savedSrc, savedInfo, savedPkg, savedFileIndex, savedSrcByFile
		}
	}()

	params := []*Node{}
	if inst.decl.recv != nil {
		rp, err := e.exportParamVar(fd, inst.decl.recv)
		if err != nil {
			return nil, err
		}
		params = append(params, rp)
	}
	pp, err := e.exportSignatureVars(fd.Type, inst.decl.sig.Params())
	if err != nil {
		return nil, err
	}
	params = append(params, pp...)
	results, err := e.exportSignatureVars(fd.Type, inst.decl.sig.Results())
	if err != nil {
		return nil, err
	}
	if len(results) > 1 {
		return nil, scopeErrorf("func %s has %d results; at most one result is supported in frontend/v1 scope (WS D)", inst.name, len(results))
	}
	body, err := e.exportBlock(fd.Body)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"name": inst.name, "obj": inst.instID, "instof": inst.decl.funcID,
		"typeargs": inst.targIDs, "sig": inst.sigID, "params": params, "body": body,
		// Same cross-package binding as funcdecl (see exportFuncDecl).
		"pkgpath": funcPkgPath(e, inst.decl),
	}
	if len(results) > 0 {
		fields["results"] = results
	}
	return e.node("funcdecl", fd, fields), nil
}

// exportParamVar emits one kind:"param" node for a *types.Var.
func (e *exporter) exportParamVar(pos ast.Node, v *types.Var) (*Node, error) {
	id, err := e.defVar(v, true)
	if err != nil {
		return nil, err
	}
	tid, err := e.typeID(v.Type())
	if err != nil {
		return nil, err
	}
	return e.node("param", pos, map[string]any{
		"name": v.Name(), "obj": id, "type": tid,
	}), nil
}

// freshSigID emits a fresh (unmemoized) signature type-table entry. Used for
// funcdecl "sig" fields so the entry always matches the emitted params
// (methods: receiver prepended as params[0]; instantiations: substituted).
func (e *exporter) freshSigID(recv *types.Var, params, results *types.Tuple, variadic bool) (int, error) {
	pp, err := e.tupleList(params)
	if err != nil {
		return 0, err
	}
	if recv != nil {
		rid, err := e.typeID(recv.Type())
		if err != nil {
			return 0, err
		}
		pp = append([]any{map[string]any{"name": recv.Name(), "type": rid}}, pp...)
	}
	rr, err := e.tupleList(results)
	if err != nil {
		return 0, err
	}
	id := len(e.types)
	e.types = append(e.types, TypeEnt{Kind: "signature", Detail: map[string]any{
		"params": pp, "results": rr, "variadic": variadic,
	}})
	// R2 P2-3: keep e.typeList parallel to e.types (recomputeMethodTables
	// indexes e.types by e.typeList position). Signature entries have no
	// go/types type to record — the placeholder preserves the invariant;
	// the sole typeList consumer skips nil via its type switch.
	e.typeList = append(e.typeList, nil)
	return id, nil
}

// ---------------------------------------------------------------------------
// Statements.
// ---------------------------------------------------------------------------

func (e *exporter) exportBlock(b *ast.BlockStmt) (*Node, error) {
	stmts := make([]*Node, 0, len(b.List))
	for _, s := range b.List {
		n, err := e.exportStmt(s)
		if err != nil {
			return nil, err
		}
		if n == nil {
			continue // *ast.EmptyStmt: normalized away (documented)
		}
		stmts = append(stmts, n)
	}
	return e.node("block", b, map[string]any{"stmts": stmts}), nil
}

// exportStmt exports a statement.
// BUG-MODPROBE-010: flushes temp vardecls hoisted from multi-result calls
// in argument position (f(g()) desugar via exportCallArgs -> e.mrHoistPre).
// Only preStmts added during THIS statement's export are flushed (tracked
// via length); nested exportStmt calls flush their own. The statement is
// wrapped in a block with the hoisted vardecls first.
func (e *exporter) exportStmt(s ast.Stmt) (*Node, error) {
	preLen := len(e.mrHoistPre)
	n, err := e.exportStmtInner(s)
	if err != nil {
		return nil, err
	}
	if len(e.mrHoistPre) > preLen {
		newPre := append([]*Node{}, e.mrHoistPre[preLen:]...)
		e.mrHoistPre = e.mrHoistPre[:preLen]
		if n == nil {
			return e.node("block", s, map[string]any{"stmts": newPre}), nil
		}
		stmts := append(newPre, n)
		return e.node("block", s, map[string]any{"stmts": stmts}), nil
	}
	return n, nil
}

func (e *exporter) exportStmtInner(s ast.Stmt) (*Node, error) {
	switch s := s.(type) {
	case *ast.EmptyStmt:
		return nil, nil // normalized away (documented in docs/frontend-v1.md)
	case *ast.BlockStmt:
		return e.exportBlock(s)
	case *ast.DeclStmt:
		gd, ok := s.Decl.(*ast.GenDecl)
		if !ok {
			return nil, scopeErrorf("only var/type declarations are supported in frontend/v1 scope (WS G/H)")
		}
		switch gd.Tok {
		case token.VAR:
			return e.exportVarGenDecl(gd)
		case token.TYPE:
			// Local type declarations are runtime no-ops (the type
			// table carries the types); normalized away like EmptyStmt
			// (documented in docs/frontend-v1.md). Each spec's declared
			// (possibly named) type is registered eagerly so later
			// references resolve to stable ids.
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					return nil, scopeErrorf("non-type type spec %T is not supported in frontend/v1 scope", spec)
				}
				tn, ok := e.info.Defs[ts.Name].(*types.TypeName)
				if !ok {
					return nil, fmt.Errorf("gofrontend: type name %q has no *types.TypeName object", ts.Name.Name)
				}
				// P3-M9 (generics, Gap 2): skip generic template definitions.
				// A generic Named (e.g., nistCurve[Point]) is never used
				// directly in valid Go code — only its instantiations
				// (e.g., nistCurve[MyPoint]) appear. Describing the template
				// would leak "typeparam" kinds into the type table, which
				// the Java backend rejects. Instantiations are described
				// on demand via typeID with monomorphization (see
				// describeType).
				if named, ok := tn.Type().(*types.Named); ok {
					if origin := named.Origin(); origin == named {
						if tparams := origin.TypeParams(); tparams != nil && tparams.Len() > 0 {
							if named.TypeArgs() == nil || named.TypeArgs().Len() == 0 {
								continue
							}
						}
					}
				}
				if _, err := e.typeID(tn.Type()); err != nil {
					return nil, err
				}
			}
			return nil, nil
		case token.CONST:
			// P3-M7 (WS G): function-local const declarations (used by
			// the synthetic math package, e.g. polynomial coefficients)
			// are compile-time only; normalized away like EmptyStmt.
			return nil, nil
		default:
			return nil, scopeErrorf("only var/type declarations are supported in frontend/v1 scope (WS G/H)")
		}
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			return e.exportDefine(s)
		}
		return e.exportAssign(s)
	case *ast.ExprStmt:
		// P3-M5 (WS E, ADR-0005 App C): close(ch) lowers to the
		// "chanclose" statement node (not wrapped in exprstmt). The
		// frozen builtin registry is untouched — interception by ident.
		if c, ok := isCloseCall(e.info, s.X); ok {
			return e.exportChanCloseCall(c)
		}
		// P3-driver-harden (jsonlib lane): atomic.StorePointer(addr, val)
		// as a bare statement lowers to *addr = val (statement position
		// only; expression use is a gc type error).
		if c, ok := unparen(s.X).(*ast.CallExpr); ok {
			if n, handled, err := e.exportAtomicStorePointerStmt(s, c); handled {
				return n, err
			}
		}
		// P3-M8 (WS D): a concrete multi-result call as a bare statement
		// (w.Write(data) on *io.PipeWriter — legal Go, results
		// discarded) desugars to the __-helper call with the pair
		// dropped. Gated on the concrete helper-call plans (mrM7,
		// mrXmlToken): the M5 kinds keep their pinned statement-position
		// fail-fast (TestMultiResultFailFastKept), and interface calls
		// keep the D1 exportIfaceMethodCall path via exportExpr below.
		if c, ok := unparen(s.X).(*ast.CallExpr); ok {
			if plan := e.classifyMultiResultCall(c); plan != nil &&
				(plan.kind == mrM7 || plan.kind == mrXmlToken) {
				return e.exportBareMultiResultCall(s, c, plan)
			}
		}
		x, err := e.exportExpr(s.X)
		if err != nil {
			return nil, err
		}
		return e.node("exprstmt", s, map[string]any{"x": x}), nil
	case *ast.ReturnStmt:
		// BUG-MODPROBE-010 (generalized): a multi-result call in return
		// position — e.g. `return json.Marshal(x)` — desugars via
		// hoistMRCallArg like the assignment case: hoist to a temp,
		// return the unpacked fields. Only the single-result form can
		// hold a multi-result call (Go forbids mixing); the hoisted temp
		// rides e.mrHoistPre so exportStmt wraps it in a block before the
		// return. In a transformed user MR function, the unpacked values
		// are packed into the function's pair (the backend's return takes
		// 0/1 results).
		if len(s.Results) == 1 {
			if c, ok := unparen(s.Results[0]).(*ast.CallExpr); ok {
				if plan := e.classifyMultiResultCall(c); plan != nil {
					unpacked, pre, err := e.hoistMRCallArg(c, plan)
					if err != nil {
						return nil, err
					}
					e.mrHoistPre = append(e.mrHoistPre, pre...)
					if e.currentMR != nil {
						mr := e.currentMR
						pairTypeID, err := e.typeID(mr.pairType)
						if err != nil {
							return nil, err
						}
						pairNode := e.node("complit", s, map[string]any{
							"type":      pairTypeID,
							"keyed":     true,
							"values":    unpacked,
							"fieldkeys": mr.pairFields,
						})
						return e.node("return", s, map[string]any{"results": []*Node{pairNode}}), nil
					}
					return e.node("return", s, map[string]any{"results": unpacked}), nil
				}
			}
		}
		results := make([]*Node, 0, len(s.Results))
		for _, r := range s.Results {
			x, err := e.exportExpr(r)
			if err != nil {
				return nil, err
			}
			results = append(results, x)
		}
		// P3-M9 (WS D): in a transformed user multi-result function,
		// `return a, b` becomes `return Pair{f0: a, f1: b}`.
		if e.currentMR != nil && len(results) > 1 {
			mr := e.currentMR
			pairTypeID, err := e.typeID(mr.pairType)
			if err != nil {
				return nil, err
			}
			pairNode := e.node("complit", s, map[string]any{
				"type":      pairTypeID,
				"keyed":     true,
				"values":    results,
				"fieldkeys": mr.pairFields,
			})
			return e.node("return", s, map[string]any{"results": []*Node{pairNode}}), nil
		}
		return e.node("return", s, map[string]any{"results": results}), nil
	case *ast.IncDecStmt:
		x, err := e.exportAssignable(s.X)
		if err != nil {
			return nil, err
		}
		return e.node("incdec", s, map[string]any{"tok": s.Tok.String(), "x": x}), nil
	case *ast.IfStmt:
		return e.exportIf(s)
	case *ast.ForStmt:
		return e.exportFor(s)
	case *ast.RangeStmt:
		return e.exportRange(s)
	case *ast.SwitchStmt:
		return e.exportSwitch(s)
	case *ast.TypeSwitchStmt:
		return e.exportTypeSwitch(s)
	case *ast.BranchStmt:
		return e.exportBranch(s)
	case *ast.LabeledStmt:
		return e.exportLabeled(s)
	case *ast.DeferStmt:
		return e.exportDeferStmt(s)
	case *ast.GoStmt:
		return e.exportGoStmt(s)
	case *ast.SendStmt:
		// P3-M5 (WS E, ADR-0005 App C): ch <- v lowers to "send".
		return e.exportSend(s)
	case *ast.SelectStmt:
		// P3-M5 (WS E, ADR-0005 App C): select lowers to "select".
		return e.exportSelect(s)
	default:
		return nil, scopeErrorf("%T is not supported in frontend/v1 scope (%s)", s, stmtOwner(s))
	}
}

// exportDeferStmt exports `defer f(args)` (M5, WS F). The deferred call is
// exported as a call node; the call/args evaluation timing (evaluate now,
// invoke at return/unwind) is the factory's job.
func (e *exporter) exportDeferStmt(s *ast.DeferStmt) (*Node, error) {
	// P3-M5 (WS E, ADR-0005 App C): `defer close(ch)` desugars through a
	// funclit (defer-time channel evaluation, close at return time).
	if c, ok := isCloseCall(e.info, s.Call); ok {
		return e.exportCloseDeferGo(s, c, true)
	}
	// P3-M2b (ADR-0002): `defer m.Unlock()` — the frozen buildDefer only
	// understands funclit/ident/builtin deferred calls, so a deferred sync
	// method call desugars to
	//   defer func(__sync_recv, __sync_arg0, ...) {
	//       __sync_recv.Op(__sync_arg0, ...)
	//   }(recv, args...)
	// The receiver and arguments are evaluated at defer time (Go
	// semantics); the call runs at return/unwind. Param passing (not
	// capture) so a receiver reassigned before return still unlocks the
	// deferred-time object.
	if sel, ok := unwrapSelector(s.Call.Fun); ok {
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if nt := syntheticPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncDefer(s, sel, selection, nt)
			}
			// real-reflect: promoted sync primitive methods in defer
			// position (e.g. defer funcLookupCache.Unlock()).
			if nt, path := promotedSyncPrimitive(selection); nt != nil {
				return e.exportPromotedSyncDefer(s, sel, selection, nt, path)
			}
		}
	}
	call, err := e.exportCall(s.Call)
	if err != nil {
		return nil, err
	}
	// P3-M9: deferred interface method calls (e.g. `defer hash.Reset()`)
	// export as ifacemethod nodes, which the Java buildDefer does not
	// understand (it requires call.fun). Wrap in a funclit so the defer
	// gets a standard call node; the receiver is captured from the
	// enclosing scope (evaluated at defer time per Go semantics, since
	// the funclit call itself is what gets deferred).
	if call.Kind == "ifacemethod" {
		body := e.node("block", s.Call, map[string]any{
			"stmts": []*Node{e.node("exprstmt", s.Call, map[string]any{"x": call})},
		})
		funclit := e.node("funclit", s.Call, map[string]any{
			"params": []*Node{}, "results": []*Node{}, "body": body,
		})
		call = e.node("call", s.Call, map[string]any{
			"fun": funclit, "args": []*Node{}, "type": nil,
		})
	}
	return e.node("defer", s, map[string]any{"call": call}), nil
}

// exportSyncDefer desugars `defer m.Op(args)` on a sync primitive (see
// exportDeferStmt). The returned node is a "defer" wrapping a "call" of a
// synthesized funclit — all additive v1 wire, no factory change.
//
// Receiver semantics (verified against gc 1.27.1): a method with pointer
// receiver called on a value-typed receiver R uses &R — the ADDRESS — so a
// reassigned R still unlocks the current object. The desugar therefore
// CAPTURES a value-typed ident receiver in the closure body (evaluated at
// return time, like &R). A pointer-typed receiver (or a non-ident receiver,
// i.e. a temporary that cannot be reassigned) is passed as a param,
// evaluated at defer time. Args are always params (evaluated at defer
// time, Go semantics).
func (e *exporter) exportSyncDefer(s *ast.DeferStmt, sel *ast.SelectorExpr, selection *types.Selection, nt *types.Named) (*Node, error) {
	c := s.Call
	typeName := nt.Obj().Name()
	op := sel.Sel.Name
	if err := syntheticOpInScope(nt, op); err != nil {
		return nil, err
	}
	argNodes, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// The method's *types.Func gives the declared parameter types.
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s is not a *types.Func", typeName, op)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s has non-signature type", typeName, op)
	}
	recvType := e.info.Types[sel.X].Type
	if recvType == nil {
		return nil, fmt.Errorf("gofrontend: no type info for sync defer receiver")
	}
	// Value-typed ident receiver: capture (address semantics).
	// Otherwise: evaluate at defer time via a param.
	_, recvIsPtr := recvType.(*types.Pointer)
	_, recvIsIdent := sel.X.(*ast.Ident)
	captureRecv := !recvIsPtr && recvIsIdent
	var recvNode *Node
	var recvParam *Node
	var recvIdent *Node
	outerArgs := []*Node{}
	if captureRecv {
		// The original receiver expression goes in the body and is
		// captured by the closure — no param, no outer arg.
		recvNode, err = e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
	} else {
		recvNode, err = e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
		outerArgs = append(outerArgs, recvNode)
		v := types.NewParam(token.NoPos, nil, "__sync_recv", recvType)
		id, err := e.defVar(v, true)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(recvType)
		if err != nil {
			return nil, err
		}
		recvParam = e.node("param", c, map[string]any{
			"name": "__sync_recv", "obj": id, "type": tid,
		})
		recvIdent = e.node("ident", c, map[string]any{
			"name": "__sync_recv", "obj": id, "type": tid,
		})
	}
	// Args are always params (evaluated at defer time).
	params := []*Node{}
	idents := []*Node{}
	if recvParam != nil {
		params = append(params, recvParam)
		idents = append(idents, recvIdent)
	}
	if sig.Params().Len() != len(argNodes) {
		return nil, fmt.Errorf("gofrontend: sync.%s.%s arity mismatch (internal error)", typeName, op)
	}
	for i := range argNodes {
		v := types.NewParam(token.NoPos, nil, fmt.Sprintf("__sync_arg%d", i), sig.Params().At(i).Type())
		id, err := e.defVar(v, true)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(sig.Params().At(i).Type())
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("__sync_arg%d", i)
		params = append(params, e.node("param", c, map[string]any{
			"name": name, "obj": id, "type": tid,
		}))
		idents = append(idents, e.node("ident", c, map[string]any{
			"name": name, "obj": id, "type": tid,
		}))
		outerArgs = append(outerArgs, argNodes[i])
	}
	// Body receiver: the captured original, or the param ident.
	bodyRecv := recvNode
	if recvIdent != nil {
		bodyRecv = recvIdent
	}
	// Body args: param idents (skip the receiver's if present).
	bodyArgs := idents
	if recvParam != nil {
		bodyArgs = idents[1:]
	}
	inner := e.node("syncop", c, map[string]any{
		"op": op, "synctyp": syntheticOpTypeName(nt),
		"recv": bodyRecv, "args": bodyArgs, "type": nil,
	})
	body := e.node("block", c, map[string]any{
		"stmts": []*Node{e.node("exprstmt", c, map[string]any{"x": inner})},
	})
	funclit := e.node("funclit", c, map[string]any{
		"params": params, "results": []*Node{}, "body": body,
	})
	call := e.node("call", c, map[string]any{
		"fun": funclit, "args": outerArgs, "type": nil,
	})
	return e.node("defer", s, map[string]any{"call": call}), nil
}

// exportPromotedSyncDefer desugars `defer x.Op(args)` where the method is
// promoted through embedding from a sync primitive (real-reflect:
// defer funcLookupCache.Unlock()). The receiver is adapted along the
// embedding path to the primitive, then the standard sync defer desugar
// applies. The receiver here is always a package-level var (addressable);
// it is captured by the closure (evaluated at return time, like &R).
func (e *exporter) exportPromotedSyncDefer(s *ast.DeferStmt, sel *ast.SelectorExpr, selection *types.Selection, nt *types.Named, path []int) (*Node, error) {
	c := s.Call
	op := sel.Sel.Name
	if err := syntheticOpInScope(nt, op); err != nil {
		return nil, err
	}
	// Adapt the receiver along the embedding path.
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	adapted := embeddedFieldType(selection.Recv(), path)
	if adapted == nil {
		return nil, fmt.Errorf("gofrontend: cannot adapt receiver for promoted sync defer %s (exporter bug)", op)
	}
	tid, err := e.typeID(adapted)
	if err != nil {
		return nil, err
	}
	apath := make([]any, len(path))
	for i, v := range path {
		apath[i] = v
	}
	recv = e.node("selector", sel, map[string]any{
		"x": recv, "field": embeddedFieldName(selection.Recv(), path),
		"selpath": apath, "type": tid,
	})
	// Args are params (evaluated at defer time).
	argNodes, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s is not a *types.Func", nt.Obj().Name(), op)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s has non-signature type", nt.Obj().Name(), op)
	}
	params := []*Node{}
	idents := []*Node{}
	if sig.Params().Len() != len(argNodes) {
		return nil, fmt.Errorf("gofrontend: sync.%s.%s arity mismatch (internal error)", nt.Obj().Name(), op)
	}
	outerArgs := []*Node{}
	for i := range argNodes {
		v := types.NewParam(token.NoPos, nil, fmt.Sprintf("__sync_arg%d", i), sig.Params().At(i).Type())
		id, err := e.defVar(v, true)
		if err != nil {
			return nil, err
		}
		ptid, err := e.typeID(sig.Params().At(i).Type())
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("__sync_arg%d", i)
		params = append(params, e.node("param", c, map[string]any{
			"name": name, "obj": id, "type": ptid,
		}))
		idents = append(idents, e.node("ident", c, map[string]any{
			"name": name, "obj": id, "type": ptid,
		}))
		outerArgs = append(outerArgs, argNodes[i])
	}
	// The adapted receiver is captured (package var, addressable).
	inner := e.node("syncop", c, map[string]any{
		"op": op, "synctyp": syntheticOpTypeName(nt),
		"recv": recv, "args": idents, "type": nil,
	})
	body := e.node("block", c, map[string]any{
		"stmts": []*Node{e.node("exprstmt", c, map[string]any{"x": inner})},
	})
	funclit := e.node("funclit", c, map[string]any{
		"params": params, "results": []*Node{}, "body": body,
	})
	call := e.node("call", c, map[string]any{
		"fun": funclit, "args": outerArgs, "type": nil,
	})
	return e.node("defer", s, map[string]any{"call": call}), nil
}

// exportGoStmt exports `go f(args)` (P2-M1, WS A). The call is exported as
// a call node; the Phase-2 factory builds a GoStatementNode that evaluates
// the function value in the spawning goroutine and hands it to the
// scheduler. Wire stays v1: "gostmt" is an additive node kind (freeze
// §2.3 — old consumers reject it as unknown, which is the correct
// forward-compatibility behavior for a kind they predate).
func (e *exporter) exportGoStmt(s *ast.GoStmt) (*Node, error) {
	// P3-M5 (WS E, ADR-0005 App C): `go close(ch)` desugars through a
	// funclit with the channel hoisted (go-time evaluation).
	if c, ok := isCloseCall(e.info, s.Call); ok {
		return e.exportCloseDeferGo(s, c, false)
	}
	// P3-M2b (ADR-0002): `go m.Op(args)` — GoStatementNode only builds
	// zero-arg spawns (P2-M1), so a direct sync method go-statement
	// desugars via exportSyncGoStmt: the receiver/args are evaluated at
	// go-statement time into hoisted temporaries (Go spec: "the function
	// value and parameters are evaluated as usual in the calling
	// goroutine"), then a zero-arg funclit spawn runs the syncop against
	// them. A value-typed ident receiver is captured by reference (gc's
	// &m address semantics, cf. sy12).
	if sel, ok := unwrapSelector(s.Call.Fun); ok {
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if nt := syntheticPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncGoStmt(s, sel, selection, nt)
			}
		}
	}
	call, err := e.exportCall(s.Call)
	if err != nil {
		return nil, err
	}
	// P3-M5 (WS E): `go f(args)` with args desugars through a funclit.
	// GoStatementNode (P2-M1) only builds zero-arg spawns; the function
	// value and args are evaluated at go time (Go spec) into hoisted
	// temps, then a zero-arg funclit runs the call. (Specific cases like
	// `go close(ch)` and sync primitives are handled above.)
	if len(s.Call.Args) > 0 {
		return e.exportGoWithArgs(s)
	}
	return e.node("gostmt", s, map[string]any{"call": call}), nil
}

// exportGoWithArgs desugars `go f(args)` (args non-empty) to hoisted
// temps + `go func() { f(temps) }()`. The temps are evaluated in the
// calling goroutine at go-statement time (Go spec); the funclit captures
// them by reference (they are never reassigned, so the goroutine sees
// the go-time values).
func (e *exporter) exportGoWithArgs(s *ast.GoStmt) (*Node, error) {
	c := s.Call
	e.chanSeq++
	seq := e.chanSeq
	vardecls := make([]*Node, 0, len(c.Args)+1)
	// Hoist the function value FIRST (Go spec: the function value is
	// evaluated before the arguments, all in the calling goroutine).
	// P3-M5 remediation (reviewer1 P2-3): the old code evaluated the fun
	// expression inside the spawned funclit for non-ident forms.
	funNode, err := e.hoistGoFunValue(s, c, seq, &vardecls)
	if err != nil {
		return nil, err
	}
	// Hoist args.
	tempNames := make([]string, 0, len(c.Args))
	tempObjs := make([]int, 0, len(c.Args))
	tempIdents := make([]*Node, 0, len(c.Args))
	for i, arg := range c.Args {
		tv, ok := e.info.Types[arg]
		if !ok || tv.Type == nil {
			return nil, fmt.Errorf("gofrontend: no type info for go arg %d", i)
		}
		name := fmt.Sprintf("__go_arg_%d_%d", seq, i)
		v := types.NewVar(token.NoPos, nil, name, tv.Type)
		oid, err := e.defVar(v, false)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(tv.Type)
		if err != nil {
			return nil, err
		}
		argx, err := e.exportExpr(arg)
		if err != nil {
			return nil, err
		}
		vardecls = append(vardecls, e.node("vardecl", s, map[string]any{
			"names": []string{name}, "objs": []int{oid},
			"define": true, "type": nil, "values": []*Node{argx},
		}))
		tempNames = append(tempNames, name)
		tempObjs = append(tempObjs, oid)
		tempIdents = append(tempIdents, e.node("ident", s, map[string]any{
			"name": name, "obj": oid, "type": tid,
		}))
	}
	// Build the call f(temps) with the hoisted fun node.
	callType, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	innerCall := e.node("call", s, map[string]any{
		"fun": funNode, "args": tempIdents, "type": callType,
	})
	// Funclit body: the call as an expression statement.
	body := e.node("block", s, map[string]any{"stmts": []*Node{
		e.node("exprstmt", s, map[string]any{"x": innerCall}),
	}})
	funclit := e.node("funclit", s, map[string]any{
		"params": []*Node{}, "results": []*Node{}, "body": body,
	})
	// go func() { ... }().
	goCall := e.node("call", s, map[string]any{
		"fun": funclit, "args": []*Node{}, "type": nil,
	})
	gostmt := e.node("gostmt", s, map[string]any{"call": goCall})
	// Block: hoists + gostmt.
	stmts := append(vardecls, gostmt)
	return e.node("block", s, map[string]any{"stmts": stmts}), nil
}

// hoistGoFunValue evaluates the go statement's function value in the
// calling goroutine (Go spec: "the function value and parameters are
// evaluated as usual in the calling goroutine") into a hoisted temporary,
// and returns the fun node for the zero-arg funclit's inner call. The
// hoist vardecl(s) are appended to *vardecls ahead of the argument
// hoists (function value evaluates before arguments).
//
//   - Declared function ident (`go f(args)`): the function value is
//     fixed — reference it directly, as before.
//   - Func-valued ident (`go fv(args)` where fv is a variable):
//     hoist the variable's go-time value (a reassignment racing the
//     goroutine must not change which function runs).
//   - Method call (`go obj.M(args)`, plain MethodVal): hoist the
//     receiver to `__go_recv_<seq>` and build a methodref against the
//     temp (same resolution as exportMethodCall). This also retires the
//     old WS H "method value (bound receiver) is not supported" fail-fast
//     for this shape — a method call is not a method value.
//   - Anything else: hoist the whole fun expression to `__go_fun_<seq>`
//     (covers func-valued fields, index/call results, ...). Shapes whose
//     fun expression cannot be exported (interface method values,
//     synthetic time/timer methods, multi-result synthetics, builtins)
//     keep their existing fail-fast via exportExpr.
//
// (P3-M5 remediation, reviewer1 P2-3.)
func (e *exporter) hoistGoFunValue(s *ast.GoStmt, c *ast.CallExpr, seq int, vardecls *[]*Node) (*Node, error) {
	switch fun := c.Fun.(type) {
	case *ast.Ident:
		obj := e.info.ObjectOf(fun)
		if obj == nil {
			return nil, fmt.Errorf("gofrontend: no object for go func %q", fun.Name)
		}
		if _, ok := obj.(*types.Func); ok {
			// Declared function: fixed function value.
			oid, ok := e.objIDs[obj]
			if !ok {
				return nil, fmt.Errorf("gofrontend: go func %q not declared", fun.Name)
			}
			tid, err := e.typeID(obj.Type())
			if err != nil {
				return nil, err
			}
			return e.node("ident", s, map[string]any{"name": fun.Name, "obj": oid, "type": tid}), nil
		}
		if _, ok := obj.(*types.Var); !ok {
			// Builtins, consts, typenames, ...: keep the historical
			// fail-fast (previously "go func %q not declared").
			return nil, fmt.Errorf("gofrontend: go func %q not declared", fun.Name)
		}
		// Func variable: hoist its go-time value.
		return e.hoistGoFunExpr(s, c.Fun, seq, vardecls)
	case *ast.SelectorExpr:
		if selection, ok := e.info.Selections[fun]; ok && selection.Kind() == types.MethodVal &&
			syntheticPrimitiveRecv(selection.Recv()) == nil &&
			timeTimerTickerNamed(selection.Recv()) == nil &&
			recvIface(selection.Recv()) == nil &&
			!isMultiResultSyntheticFunc(selection.Obj()) {
			return e.hoistGoMethodRecv(s, c, fun, selection, seq, vardecls)
		}
		return e.hoistGoFunExpr(s, c.Fun, seq, vardecls)
	default:
		return e.hoistGoFunExpr(s, c.Fun, seq, vardecls)
	}
}

// hoistGoFunExpr hoists an arbitrary function-value expression to
// `__go_fun_<seq>` at go-statement time and returns an ident to the temp.
func (e *exporter) hoistGoFunExpr(s *ast.GoStmt, funExpr ast.Expr, seq int, vardecls *[]*Node) (*Node, error) {
	tv, ok := e.info.Types[funExpr]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for go func value")
	}
	funx, err := e.exportExpr(funExpr)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("__go_fun_%d", seq)
	v := types.NewVar(token.NoPos, nil, name, tv.Type)
	oid, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	tid, err := e.typeID(tv.Type)
	if err != nil {
		return nil, err
	}
	*vardecls = append(*vardecls, e.node("vardecl", s, map[string]any{
		"names": []string{name}, "objs": []int{oid},
		"define": true, "type": nil, "values": []*Node{funx},
	}))
	return e.node("ident", s, map[string]any{"name": name, "obj": oid, "type": tid}), nil
}

// hoistGoMethodRecv hoists a method call's receiver to `__go_recv_<seq>`
// at go-statement time and returns a methodref against the temp (using
// exportMethodCall's exact resolution via methodRefNode).
func (e *exporter) hoistGoMethodRecv(s *ast.GoStmt, c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection, seq int, vardecls *[]*Node) (*Node, error) {
	xt, ok := e.info.Types[sel.X]
	if !ok || xt.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for go method receiver")
	}
	recvx, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("__go_recv_%d", seq)
	v := types.NewVar(token.NoPos, nil, name, xt.Type)
	oid, err := e.defVar(v, false)
	if err != nil {
		return nil, err
	}
	tid, err := e.typeID(xt.Type)
	if err != nil {
		return nil, err
	}
	*vardecls = append(*vardecls, e.node("vardecl", s, map[string]any{
		"names": []string{name}, "objs": []int{oid},
		"define": true, "type": nil, "values": []*Node{recvx},
	}))
	recvIdent := e.node("ident", s, map[string]any{"name": name, "obj": oid, "type": tid})
	return e.methodRefNode(c, sel, selection, recvIdent)
}

// isMultiResultSyntheticFunc reports whether obj is one of the synthetic
// multi-result functions/methods (fail-fast under WS D).
func isMultiResultSyntheticFunc(obj types.Object) bool {
	f, ok := obj.(*types.Func)
	return ok && isMultiResultSynthetic(f)
}

// exportSyncGoStmt desugars `go m.Op(args)` on a sync primitive (see
// exportGoStmt): a block that first hoists every argument into a synthetic
// temporary (`__sync_go_arg<seq>_<i> := <arg>`, via defVar + a vardecl node
// like exportDefine) — the Go spec evaluates the function value and
// parameters in the calling goroutine — then a zero-arg funclit spawn
// (GoStatementNode's P2-M1 contract) whose body references the
// temporaries. The temporaries are never reassigned, so the closure's
// by-reference capture reads the go-time values. All additive v1 wire, no
// factory change.
//
// Receiver: a value-typed ident receiver keeps the current behavior —
// captured by reference in the body (gc's &m address semantics, cf.
// exportSyncDefer and sy12: the address is fixed at go time, the object is
// read at run time). A pointer-typed or non-ident receiver is hoisted
// into a `__sync_go_recv<seq>` temporary the same way as the args (same
// evaluation-timing rule: `go getMu().Unlock()` must call getMu at go
// time).
func (e *exporter) exportSyncGoStmt(s *ast.GoStmt, sel *ast.SelectorExpr, selection *types.Selection, nt *types.Named) (*Node, error) {
	c := s.Call
	typeName := nt.Obj().Name()
	op := sel.Sel.Name
	if err := syntheticOpInScope(nt, op); err != nil {
		return nil, err
	}
	argNodes, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// The method's *types.Func gives the declared parameter types for the
	// synthetic temporaries.
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s is not a *types.Func", typeName, op)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: sync method %s.%s has non-signature type", typeName, op)
	}
	if sig.Params().Len() != len(argNodes) {
		return nil, fmt.Errorf("gofrontend: sync.%s.%s arity mismatch (internal error)", typeName, op)
	}
	recvType := e.info.Types[sel.X].Type
	if recvType == nil {
		return nil, fmt.Errorf("gofrontend: no type info for sync go receiver")
	}
	_, recvIsPtr := recvType.(*types.Pointer)
	_, recvIsIdent := sel.X.(*ast.Ident)
	captureRecv := !recvIsPtr && recvIsIdent

	seq := e.syncGoSeq
	e.syncGoSeq++

	names := []string{}
	objs := []int{}
	values := []*Node{}
	// hoist evaluates val now (go-statement time) into a synthetic
	// temporary and returns an ident node referencing it for the body.
	hoist := func(tag string, i int, typ types.Type, val *Node) (*Node, error) {
		name := fmt.Sprintf("__sync_go_%s%d_%d", tag, seq, i)
		v := types.NewVar(token.NoPos, nil, name, typ)
		id, err := e.defVar(v, false)
		if err != nil {
			return nil, err
		}
		tid, err := e.typeID(typ)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
		objs = append(objs, id)
		values = append(values, val)
		return e.node("ident", c, map[string]any{
			"name": name, "obj": id, "type": tid,
		}), nil
	}

	var bodyRecv *Node
	if captureRecv {
		// Value-typed ident: captured by reference (&m address semantics).
		bodyRecv, err = e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
	} else {
		recvNode, err := e.exportExpr(sel.X)
		if err != nil {
			return nil, err
		}
		bodyRecv, err = hoist("recv", 0, recvType, recvNode)
		if err != nil {
			return nil, err
		}
	}
	bodyArgs := make([]*Node, len(argNodes))
	for i := range argNodes {
		bodyArgs[i], err = hoist("arg", i, sig.Params().At(i).Type(), argNodes[i])
		if err != nil {
			return nil, err
		}
	}
	inner, err := e.exportSyncOpNodes(c, sel, nt, bodyRecv, bodyArgs)
	if err != nil {
		return nil, err
	}
	body := e.node("block", c, map[string]any{
		"stmts": []*Node{e.node("exprstmt", c, map[string]any{"x": inner})},
	})
	funclit := e.node("funclit", c, map[string]any{
		"params": []*Node{}, "results": []*Node{}, "body": body,
	})
	call := e.node("call", c, map[string]any{
		"fun": funclit, "args": []*Node{}, "type": nil,
	})
	spawn := e.node("gostmt", s, map[string]any{"call": call})
	stmts := []*Node{spawn}
	if len(names) > 0 {
		hoistDecl := e.node("vardecl", s, map[string]any{
			"names": names, "objs": objs, "define": true, "type": nil, "values": values,
		})
		stmts = []*Node{hoistDecl, spawn}
	}
	return e.node("block", s, map[string]any{"stmts": stmts}), nil
}

// stmtOwner attributes an unsupported statement to its workstream.
func stmtOwner(s ast.Stmt) string {
	switch s.(type) {
	case *ast.GoStmt:
		return "Phase 2 (goroutines; M5 task brief)"
	default:
		return "a later workstream"
	}
}

// exportVarGenDecl handles `var` declarations (GenDecl form).
func (e *exporter) exportVarGenDecl(gd *ast.GenDecl) (*Node, error) {
	// A `var (...)` block with multiple specs emits one vardecl per spec
	// (the backend requires values/objs length match; specs with no init
	// have empty values).
	var decls []*Node
	for _, spec := range gd.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			return nil, scopeErrorf("non-value var spec %T is not supported in frontend/v1 scope", spec)
		}
		d, err := e.exportVarSpec(vs)
		if err != nil {
			return nil, err
		}
		decls = append(decls, d)
	}
	if len(decls) == 1 {
		return decls[0], nil
	}
	return e.node("block", gd, map[string]any{"stmts": decls}), nil
}

// exportVarSpec exports a single *ast.ValueSpec as a vardecl node.
func (e *exporter) exportVarSpec(vs *ast.ValueSpec) (*Node, error) {
	names := []string{}
	objs := []int{}
	var typ any
	values := make([]*Node, 0)
		var typeTid int
		var typeIsSync *types.Named
		var typeIsTimer *types.Named
		hasType := false
		if vs.Type != nil {
			tid, err := e.exprType(vs.Type)
			if err != nil {
				return nil, err
			}
			typ = tid
			typeTid, hasType = tid, true
			if tv, ok := e.info.Types[vs.Type]; ok && tv.Type != nil {
				// P3-M4 (WS C, ADR-0004): the sync/atomic overlay shares
				// the D3 zero-value normalization.
				typeIsSync = syntheticPrimitiveNamed(tv.Type)
				// P3-M5 (WS E, ADR-0005 D1): time.Timer/Ticker value
				// positions are not in M5 scope (checked below).
				typeIsTimer = timeTimerTickerNamedExact(tv.Type)
			}
		}
		for _, n := range vs.Names {
			v, ok := e.info.Defs[n].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: var name %q has no *types.Var object", n.Name)
			}
			id, err := e.defVar(v, false)
			if err != nil {
				return nil, err
			}
			names = append(names, n.Name)
			objs = append(objs, id)
		}
		for _, v := range vs.Values {
			// P3-M8 (WS D; REVIEW-1 P2-5): a multi-result interface call
			// in a `var` declaration (ValueSpec) position is a named loud
			// limitation. exportMultiResultDesugar handles only
			// *ast.AssignStmt (2-target := and =, incl. if-init) —
			// consistent with the pre-existing M5/M7 concrete desugar.
			// The ifacemethod node yields a single pair-typed value which
			// cannot be split across the declared names, so exporting it
			// would be a silent wire-shape divergence: fail fast
			// instead. (The REVIEW-1 P1 fix routed interface calls to
			// exportIfaceMethodCall before the synthetic fail-fast; this
			// check restores loudness for the ValueSpec position.)
			// Re-test trigger: extend exportMultiResultDesugar to
			// ValueSpec positions (see DESIGN.md §5).
			if c, ok := unparen(v).(*ast.CallExpr); ok {
				if plan := e.classifyMultiResultCall(c); plan != nil && plan.kind == mrIfaceMethod {
					return nil, scopeErrorf("multi-result interface call in var declaration is not supported in frontend/v1 scope (M8 limitation: only 2-target := and = desugar; see DESIGN.md §5)")
				}
			}
			x, err := e.exportExpr(v)
			if err != nil {
				return nil, err
			}
			values = append(values, x)
		}
		// P3-M2b (ADR-0002 D3): `var m sync.Mutex` (no init) normalizes to
		// an implicit syncnew per name — gc's ready-to-use zero value. The
		// factory's struct zero would be an unusable empty struct, and the
		// factory is frozen, so the exporter supplies the init. One
		// syncnew per name: each variable is a distinct mutex.
		// P3-M5 (WS E, ADR-0005 D1): `var t time.Timer` / `var t
		// time.Ticker` (with or without init) is not in M5 scope — gc's
		// zero Timer is uninitialized; use time.NewTimer/NewTicker.
		if hasType && typeIsTimer != nil {
			return nil, timerScopeErrorf(typeIsTimer, "var declaration")
		}
		if hasType && typeIsSync != nil && len(vs.Values) == 0 {
			for range vs.Names {
				values = append(values, e.node("syncnew", vs, map[string]any{
					"synctyp": syntheticOpTypeName(typeIsSync), "type": typeTid,
				}))
			}
		}
	return e.node("vardecl", vs, map[string]any{
		"names": names, "objs": objs, "define": false, "type": typ, "values": values,
	}), nil
}

// exportDefine handles `:=` (short variable declarations).
func (e *exporter) exportDefine(s *ast.AssignStmt) (*Node, error) {
	// P3-M5 (WS E, ADR-0005 App M): multi-result time/context calls in
	// 2-target position desugar to single-result helper calls.
	if n, handled, err := e.exportMultiResultDesugar(s, true); handled {
		return n, err
	}
	// P3-M5 (WS E, ADR-0005 App C): v, ok := <-ch desugars to a
	// single-case, no-default select (recv{commaOk:true} is not
	// emittable — the frozen factory only knows index/typeassert).
	if isCommaOkRecv(s) {
		return e.exportRecvCommaOkDesugar(s, true)
	}
	names := make([]string, 0, len(s.Lhs))
	objs := make([]int, 0, len(s.Lhs))
	for _, lhs := range s.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return nil, scopeErrorf(":= with non-identifier lhs %T is not supported in frontend/v1 scope (WS D)", lhs)
		}
		// A redeclared variable (same block, same type, at least one new
		// variable on the left) is recorded in Uses, not Defs, by go/types;
		// ObjectOf covers both cases and yields the same *types.Var.
		v, ok := e.info.ObjectOf(id).(*types.Var)
		if !ok {
			return nil, fmt.Errorf("gofrontend: := name %q does not denote a variable", id.Name)
		}
		oid, err := e.defVar(v, false)
		if err != nil {
			return nil, err
		}
		names = append(names, id.Name)
		objs = append(objs, oid)
	}
	values := make([]*Node, 0, len(s.Rhs))
	commaOk := len(s.Lhs) == 2 && len(s.Rhs) == 1 && isCommaOkRhs(s.Rhs[0])
	for _, rhs := range s.Rhs {
		var x *Node
		var err error
		if commaOk {
			x, err = e.exportCommaOk(unparen(rhs))
		} else {
			x, err = e.exportExpr(rhs)
		}
		if err != nil {
			return nil, err
		}
		values = append(values, x)
	}
	return e.node("vardecl", s, map[string]any{
		"names": names, "objs": objs, "define": true, "type": nil, "values": values,
	}), nil
}

// exportAssign handles `=` and op-assignments. The lhs may be a plain
// identifier, an index expression, or a selector (M4, WS E); op-assignments
// require a single lhs (go/types: "requires single-valued expressions").
func (e *exporter) exportAssign(s *ast.AssignStmt) (*Node, error) {
	// P3-M11 (sync.Pool): `p.New = f` — the Pool value is the opaque
	// GoPool Java object (ADR-0002 D1); the generic selector-lvalue path
	// cannot write its New field. Lower the single simple assignment to
	// the SetNew syncop, emitted directly as the statement (the same
	// statement shape a void syncop like p.Put(x) takes).
	if s.Tok == token.ASSIGN && len(s.Lhs) == 1 && len(s.Rhs) == 1 {
		if nt := e.poolNewSelector(s.Lhs[0]); nt != nil {
			lx := unparen(s.Lhs[0]).(*ast.SelectorExpr)
			recv, err := e.exportExpr(lx.X)
			if err != nil {
				return nil, err
			}
			val, err := e.exportExpr(s.Rhs[0])
			if err != nil {
				return nil, err
			}
			inner := e.syncFieldOp(s, "SetNew", nt, recv, []*Node{val}, nil)
			return e.node("exprstmt", s, map[string]any{"x": inner}), nil
		}
	}
	// Any other assignment shape touching Pool.New (multi-target,
	// op-assign) is a named loud limitation: the two-phase general
	// lvalue path cannot address the opaque field.
	for _, lhs := range s.Lhs {
		if e.poolNewSelector(lhs) != nil {
			return nil, scopeErrorf("assignment to sync.Pool.New is only supported as a single `p.New = f` statement (P3-M11 scope)")
		}
	}
	// P3-M5 (WS E, ADR-0005 App M): multi-result time/context calls in
	// 2-target position desugar to single-result helper calls.
	if n, handled, err := e.exportMultiResultDesugar(s, false); handled {
		return n, err
	}
	// P3-M5 (WS E, ADR-0005 App C): v, ok = <-ch desugars to a
	// single-case, no-default select (recv{commaOk:true} is not
	// emittable — the frozen factory only knows index/typeassert).
	if isCommaOkRecv(s) {
		return e.exportRecvCommaOkDesugar(s, false)
	}
	commaOk := len(s.Lhs) == 2 && len(s.Rhs) == 1 && isCommaOkRhs(s.Rhs[0])
	lhs := make([]*Node, 0, len(s.Lhs))
	for _, l := range s.Lhs {
		x, err := e.exportAssignable(l)
		if err != nil {
			return nil, err
		}
		lhs = append(lhs, x)
	}
	rhs := make([]*Node, 0, len(s.Rhs))
	for _, r := range s.Rhs {
		var x *Node
		var err error
		if commaOk {
			x, err = e.exportCommaOk(unparen(r))
		} else {
			x, err = e.exportExpr(r)
		}
		if err != nil {
			return nil, err
		}
		rhs = append(rhs, x)
	}
	return e.node("assign", s, map[string]any{
		"tok": s.Tok.String(), "lhs": lhs, "rhs": rhs,
	}), nil
}

// exportAssignable exports an assignment lhs: identifier, index,
// selector, or dereference expression. The blank identifier becomes a
// "blank" node (the value is evaluated and discarded).
func (e *exporter) exportAssignable(l ast.Expr) (*Node, error) {
	if id, ok := l.(*ast.Ident); ok && id.Name == "_" {
		return e.node("blank", id, map[string]any{}), nil
	}
	switch l := l.(type) {
	case *ast.Ident:
		return e.exportIdent(l)
	case *ast.IndexExpr:
		return e.exportIndex(l, false)
	case *ast.SelectorExpr:
		return e.exportSelector(l)
	case *ast.StarExpr:
		return e.exportDerefAssignable(l)
	default:
		return nil, scopeErrorf("assignment to %T is not supported in frontend/v1 scope (WS D)", l)
	}
}

// exportDerefAssignable exports a pointer-dereference assignment lhs
// (*p). Emits a "deref" wire node carrying the pointer expression and
// the element type (the type of *p).
func (e *exporter) exportDerefAssignable(s *ast.StarExpr) (*Node, error) {
	pn, err := e.exportExpr(s.X)
	if err != nil {
		return nil, err
	}
	tid, err := e.exprType(s)
	if err != nil {
		return nil, err
	}
	return e.node("deref", s, map[string]any{"x": pn, "type": tid}), nil
}

// exportDeref exports a pointer-dereference read (*p). Emits the same
// "deref" wire node as the assignment lhs; the factory builds a read
// node from it in expression position.
func (e *exporter) exportDeref(s *ast.StarExpr) (*Node, error) {
	return e.exportDerefAssignable(s)
}

// exportCommaOk exports a two-valued map-index or type-assertion rhs
// (already unparenthesized by the caller). Channel receives (<-ch) are
// intercepted earlier by exportRecvCommaOkDesugar (ADR-0005 App C).
func (e *exporter) exportCommaOk(r ast.Expr) (*Node, error) {
	switch r := r.(type) {
	case *ast.IndexExpr:
		return e.exportIndex(r, true)
	case *ast.TypeAssertExpr:
		return e.exportTypeAssert(r, true)
	default:
		return nil, fmt.Errorf("gofrontend: not a comma-ok expression: %T", r)
	}
}

// ---------------------------------------------------------------------------
// Control-flow statements (M4, WS E).
// ---------------------------------------------------------------------------

// exportOptStmt exports a statement that may be nil (if/for/switch init,
// for post), returning nil for the absent case.
func (e *exporter) exportOptStmt(s ast.Stmt) (*Node, error) {
	if s == nil {
		return nil, nil
	}
	return e.exportStmt(s)
}

// exportIf handles if/else (else-if chains recurse as nested "if" nodes).
func (e *exporter) exportIf(s *ast.IfStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	cond, err := e.exportExpr(s.Cond)
	if err != nil {
		return nil, err
	}
	then, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	var else_ *Node
	if s.Else != nil {
		switch t := s.Else.(type) {
		case *ast.BlockStmt:
			else_, err = e.exportBlock(t)
		case *ast.IfStmt:
			else_, err = e.exportIf(t)
		default:
			return nil, fmt.Errorf("gofrontend: else branch is %T", s.Else)
		}
		if err != nil {
			return nil, err
		}
	}
	return e.node("if", s, map[string]any{
		"init": init, "cond": cond, "then": then, "else": else_,
	}), nil
}

// exportFor handles the three for forms: 3-clause, condition-only, and
// infinite (absent cond/post are null on the wire).
func (e *exporter) exportFor(s *ast.ForStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var cond *Node
	if s.Cond != nil {
		cond, err = e.exportExpr(s.Cond)
		if err != nil {
			return nil, err
		}
	}
	post, err := e.exportOptStmt(s.Post)
	if err != nil {
		return nil, err
	}
	body, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	return e.node("for", s, map[string]any{
		"init": init, "cond": cond, "post": post, "body": body,
	}), nil
}

// rangeVarId maps a range-clause Key/Value expression to an object-table id:
// nil for an absent position or the blank identifier.
func (e *exporter) rangeVarId(x ast.Expr) (any, error) {
	if x == nil {
		return nil, nil
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return nil, scopeErrorf("range variable %T is not supported in frontend/v1 scope", x)
	}
	if id.Name == "_" {
		return nil, nil
	}
	v, ok := e.info.ObjectOf(id).(*types.Var)
	if !ok {
		return nil, fmt.Errorf("gofrontend: range variable %q does not denote a variable", id.Name)
	}
	return e.defVar(v, false)
}

// exportRange handles `for k, v := range x` and `for k, v = range x`
// (define=false). Key/value are object ids or null (absent or blank).
func (e *exporter) exportRange(s *ast.RangeStmt) (*Node, error) {
	// P3-M5 (WS E, ADR-0005 App C): `for v := range ch` desugars to a
	// for+select loop (the v1 "range" node has no channel support).
	if tv, ok := e.info.Types[s.X]; ok && tv.Type != nil {
		if _, ok := tv.Type.Underlying().(*types.Chan); ok {
			return e.exportRangeChan(s)
		}
	}
	var define bool
	switch s.Tok {
	case token.DEFINE:
		define = true
	case token.ASSIGN:
		define = false
	case token.ILLEGAL:
		// `for range x`: no key/value variables, no definition.
		define = false
	default:
		return nil, fmt.Errorf("gofrontend: range clause token %v", s.Tok)
	}
	key, err := e.rangeVarId(s.Key)
	if err != nil {
		return nil, err
	}
	value, err := e.rangeVarId(s.Value)
	if err != nil {
		return nil, err
	}
	x, err := e.exportExpr(s.X)
	if err != nil {
		return nil, err
	}
	// P3-M8b: default untyped range targets (e.g. `for _, c := range "lit"`
	// or range over an untyped string const). Go semantics: untyped constants
	// assume their default type in value context. The Java AST builder only
	// accepts typed kinds (STRING, not UNTYPED_STRING) for range.
	if tv, ok := e.info.Types[s.X]; ok && tv.Type != nil {
		if b, ok := tv.Type.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			def := types.Default(tv.Type)
			if def != tv.Type {
				tid, err := e.typeID(def)
				if err == nil {
					x.Fields["type"] = tid
				}
			}
		}
	}
	body, err := e.exportBlock(s.Body)
	if err != nil {
		return nil, err
	}
	return e.node("range", s, map[string]any{
		"key": key, "value": value, "define": define, "x": x, "body": body,
	}), nil
}

// exportStmtList exports a statement list as a "block" node positioned at
// `at` (used for case-clause bodies, which are not *ast.BlockStmt).
func (e *exporter) exportStmtList(list []ast.Stmt, at ast.Node) (*Node, error) {
	stmts := make([]*Node, 0, len(list))
	for _, s := range list {
		n, err := e.exportStmt(s)
		if err != nil {
			return nil, err
		}
		if n == nil {
			continue // *ast.EmptyStmt: normalized away (documented)
		}
		stmts = append(stmts, n)
	}
	return e.node("block", at, map[string]any{"stmts": stmts}), nil
}

// exportSwitch handles expression switches (a nil tag is `switch { }`).
func (e *exporter) exportSwitch(s *ast.SwitchStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var tag *Node
	if s.Tag != nil {
		tag, err = e.exportExpr(s.Tag)
		if err != nil {
			return nil, err
		}
	}
	clauses := make([]*Node, 0, len(s.Body.List))
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: switch body holds %T", stmt)
		}
		cases := make([]*Node, 0, len(cc.List))
		for _, c := range cc.List {
			x, err := e.exportExpr(c)
			if err != nil {
				return nil, err
			}
			cases = append(cases, x)
		}
		body, err := e.exportStmtList(cc.Body, cc)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, e.node("caseclause", cc, map[string]any{
			"cases": cases, "default": len(cc.List) == 0, "body": body,
		}))
	}
	return e.node("switch", s, map[string]any{
		"init": init, "tag": tag, "clauses": clauses,
	}), nil
}

// exportTypeSwitch handles `switch [v :=] x.(type)`. The guard variable
// (when declared) is one implicit *types.Var per clause
// (info.Implicits[clause]), already typed with the clause's type by
// go/types — single-type clauses get the clause type, multi-type/nil/
// default clauses the guard (interface) type. Each is registered in the
// object table before its clause body is exported, so references resolve
// through the normal exportIdent path.
func (e *exporter) exportTypeSwitch(s *ast.TypeSwitchStmt) (*Node, error) {
	init, err := e.exportOptStmt(s.Init)
	if err != nil {
		return nil, err
	}
	var tag ast.Expr
	hasGuard := false
	switch a := s.Assign.(type) {
	case *ast.ExprStmt:
		ta, ok := a.X.(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard is %T, want x.(type)", a.X)
		}
		tag = ta.X
	case *ast.AssignStmt:
		if a.Tok != token.DEFINE || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return nil, fmt.Errorf("gofrontend: type switch guard must be `v := x.(type)`")
		}
		ta, ok := a.Rhs[0].(*ast.TypeAssertExpr)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard rhs is %T, want x.(type)", a.Rhs[0])
		}
		if _, ok := a.Lhs[0].(*ast.Ident); !ok {
			return nil, fmt.Errorf("gofrontend: type switch guard lhs is %T, want an identifier", a.Lhs[0])
		}
		hasGuard = true
		tag = ta.X
	default:
		return nil, fmt.Errorf("gofrontend: type switch guard is %T", s.Assign)
	}
	tagNode, err := e.exportExpr(tag)
	if err != nil {
		return nil, err
	}
	clauses := make([]*Node, 0, len(s.Body.List))
	for _, stmt := range s.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("gofrontend: type switch body holds %T", stmt)
		}
		isDefault := len(cc.List) == 0
		var typeIDs []int
		nilCase := false
		for _, te := range cc.List {
			if id, ok := te.(*ast.Ident); ok && id.Name == "nil" {
				nilCase = true
				continue
			}
			tid, err := e.exprType(te)
			if err != nil {
				return nil, err
			}
			typeIDs = append(typeIDs, tid)
		}
		if nilCase && len(typeIDs) > 0 {
			return nil, fmt.Errorf("gofrontend: `case nil` combined with types")
		}
		var bindObj any
		var bindType any
		if hasGuard {
			// go/types materializes one implicit Var per clause, typed
			// with the clause's type. Register it before exporting the
			// body so guard references resolve to this clause's id.
			iv, ok := e.info.Implicits[cc].(*types.Var)
			if !ok {
				return nil, fmt.Errorf("gofrontend: type switch clause has no implicit guard variable")
			}
			oid, err := e.defVar(iv, false)
			if err != nil {
				return nil, err
			}
			btid, err := e.typeID(iv.Type())
			if err != nil {
				return nil, err
			}
			bindObj, bindType = oid, btid
		}
		body, err := e.exportStmtList(cc.Body, cc)
		if err != nil {
			return nil, err
		}
		clauses = append(clauses, e.node("typecase", cc, map[string]any{
			"types": typeIDs, "nilcase": nilCase, "default": isDefault,
			"bindobj": bindObj, "bindtype": bindType, "body": body,
		}))
	}
	return e.node("typeswitch", s, map[string]any{
		"init": init, "tag": tagNode, "clauses": clauses,
	}), nil
}

// exportBranch handles break/continue/goto/fallthrough (label or null).
func (e *exporter) exportBranch(s *ast.BranchStmt) (*Node, error) {
	var label any
	if s.Label != nil {
		label = s.Label.Name
	}
	return e.node("branch", s, map[string]any{"op": s.Tok.String(), "label": label}), nil
}

// exportLabeled handles `L: stmt`.
func (e *exporter) exportLabeled(s *ast.LabeledStmt) (*Node, error) {
	inner, err := e.exportStmt(s.Stmt)
	if err != nil {
		return nil, err
	}
	return e.node("labeled", s, map[string]any{"label": s.Label.Name, "stmt": inner}), nil
}

// ---------------------------------------------------------------------------
// Expressions.
// ---------------------------------------------------------------------------

// exprType returns the type-table id for the type of expression x.
func (e *exporter) exprType(x ast.Expr) (int, error) {
	if tv, ok := e.info.Types[x]; ok && tv.Type != nil {
		return e.typeID(tv.Type)
	}
	// Definition-site idents (params, := lhs) may lack a Types entry.
	if id, ok := x.(*ast.Ident); ok {
		if obj := e.info.ObjectOf(id); obj != nil {
			return e.typeID(obj.Type())
		}
	}
	return 0, fmt.Errorf("gofrontend: no type info for expression %T", x)
}

// foldedConst emits a go/types constant-folded binary or paren expression
// as a basiclit.
//
// Rationale (M3, WS D): untyped constants can exceed 64 bits (1<<70),
// constant comparisons are decided on infinite-precision values
// ((1<<64) < 1 is false), and constant float arithmetic is exact-rational
// (0.1+0.2 folds to the rational 0.3). Evaluating such expressions through
// the operator nodes would silently diverge from gc, so the exporter
// propagates go/types' already-folded value; NodeFactory converts it to the
// use-site type with correctly-rounded parsing. Unary expressions are NOT
// folded: -, ^, !, + are exact on the low 64 bits / booleans, so the factory
// evaluates them without divergence. "value" is empty (no single source
// literal); "const" carries go/constant ExactString. Additive producer
// behavior — the wire format is unchanged (docs/frontend-v1.md).
func (e *exporter) foldedConst(x ast.Expr, v constant.Value) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"value": "", "const": v.ExactString(), "type": t}
	if v.Kind() == constant.String {
		fields["sbytes"] = hex.EncodeToString([]byte(constant.StringVal(v)))
	}
	if v.Kind() == constant.Int || v.Kind() == constant.Float {
		f32, _ := constant.Float32Val(v)
		f64, _ := constant.Float64Val(v)
		fields["f32bits"] = int32(math.Float32bits(f32))
		fields["f64bits"] = int64(math.Float64bits(f64))
	}
	// M7: complex constants carry the real/imag parts as bit patterns.
	// Gate on the expression's TYPE, not just the constant kind: a folded
	// int/float constant in complex context (e.g. the operand of
	// complex128(1<<70), which go/types types as complex128) needs the
	// creal/cimag keys the factory requires for every complex-typed
	// basiclit — mirroring the *ast.BasicLit case below. constant.Real/Imag
	// pass non-complex values through (real part as-is, imag part zero).
	complexTyped := v.Kind() == constant.Complex
	if !complexTyped {
		if tv, ok := e.info.Types[x]; ok && isComplexType(tv.Type) {
			complexTyped = true
		}
	}
	if complexTyped {
		realPart := constant.Real(v)
		imagPart := constant.Imag(v)
		r32, _ := constant.Float32Val(realPart)
		r64, _ := constant.Float64Val(realPart)
		i32, _ := constant.Float32Val(imagPart)
		i64, _ := constant.Float64Val(imagPart)
		fields["creal32"] = int32(math.Float32bits(r32))
		fields["creal64"] = int64(math.Float64bits(r64))
		fields["cimag32"] = int32(math.Float32bits(i32))
		fields["cimag64"] = int64(math.Float64bits(i64))
	}
	return e.node("basiclit", x, fields), nil
}

// isComplexType reports whether t's underlying type is a complex basic
// type (M7: complex basiclit emission gates on the type kind, matching
// the factory's buildBasicLit).
func isComplexType(t types.Type) bool {
	if t == nil {
		return false
	}
	if b, ok := t.Underlying().(*types.Basic); ok {
		return b.Info()&types.IsComplex != 0
	}
	return false
}

func (e *exporter) exportExpr(x ast.Expr) (*Node, error) {
	switch x := x.(type) {
	case *ast.Ident:
		return e.exportIdent(x)
	case *ast.BasicLit:
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		fields := map[string]any{"value": x.Value, "type": t}
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			fields["const"] = tv.Value.ExactString()
			if tv.Value.Kind() == constant.String {
				// Byte-exact payload for string constants: ExactString is a
				// quoted Go literal (lossy to unquote by hand, and JSON
				// itself cannot carry invalid-UTF-8 bytes), so the exporter
				// hex-encodes the exact bytes (M3, WS C/D; additive field).
				fields["sbytes"] = hex.EncodeToString([]byte(constant.StringVal(tv.Value)))
			}
			if tv.Value.Kind() == constant.Int || tv.Value.Kind() == constant.Float {
				// Correctly-rounded float values of the constant, computed
				// with go/constant itself (the same rounding go/types uses
				// for constant→float conversions). Bits are reinterpreted
				// as signed ints for JSON (the factory reinterprets back).
				// This sidesteps ExactString's "a/b" fraction form, which
				// no correctly-rounded decimal parse could recover.
				f32, _ := constant.Float32Val(tv.Value)
				f64, _ := constant.Float64Val(tv.Value)
				fields["f32bits"] = int32(math.Float32bits(f32))
				fields["f64bits"] = int64(math.Float64bits(f64))
			}
			if isComplexType(tv.Type) {
				// M7: complex-typed literals carry the real/imag parts as
				// bit patterns, mirroring foldedConst. Gate on the TYPE
				// kind, not the constant kind: a literal like the bare 0
				// in parseComplexPair{0, err} is an int-kind constant in
				// complex128 context, and the factory requires
				// creal64/cimag64 for every complex-typed basiclit.
				// constant.Real/Imag pass non-complex values through
				// (real part as-is, imag part zero).
				realPart := constant.Real(tv.Value)
				imagPart := constant.Imag(tv.Value)
				r32, _ := constant.Float32Val(realPart)
				r64, _ := constant.Float64Val(realPart)
				i32, _ := constant.Float32Val(imagPart)
				i64, _ := constant.Float64Val(imagPart)
				fields["creal32"] = int32(math.Float32bits(r32))
				fields["creal64"] = int64(math.Float64bits(r64))
				fields["cimag32"] = int32(math.Float32bits(i32))
				fields["cimag64"] = int64(math.Float64bits(i64))
			}
		}
		return e.node("basiclit", x, fields), nil
	case *ast.BinaryExpr:
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		l, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		r, err := e.exportExpr(x.Y)
		if err != nil {
			return nil, err
		}
		return e.node("binary", x, map[string]any{"op": x.Op.String(), "x": l, "y": r, "type": t}), nil
	case *ast.UnaryExpr:
		// P3-M5 (WS E, ADR-0005 App C): <-ch lowers to the additive
		// "recv" wire node (the frozen UnaryOpNode never implemented <-).
		if x.Op == token.ARROW {
			return e.exportRecv(x)
		}
		if x.Op == token.AND {
			// &T{...}: normalize to a pointer-typed composite literal
			// (M4, WS E).
			if cl, ok := x.X.(*ast.CompositeLit); ok {
				t, err := e.exprType(x)
				if err != nil {
					return nil, err
				}
				return e.exportCompositeLitAs(cl, t)
			}
			// P3-M9: & on an addressable expression (ident, selector,
			// index, deref) lowers to the "addrof" wire node (same as
			// the implicit address-of for method calls). & on anything
			// else stays a WS D scope error.
			if isAddressable(x.X, e.info) {
				tid, err := e.exprType(x)
				if err != nil {
					return nil, err
				}
				// P3-M9: &arr[i] needs an element pointer (writes through
				// *p must update arr[i], not a copy). Emit array/index
				// separately; NodeFactory builds a GoPointer that
				// reads/writes the array slot.
				if idx, ok := x.X.(*ast.IndexExpr); ok {
					arrN, err := e.exportExpr(idx.X)
					if err != nil {
						return nil, err
					}
					idxN, err := e.exportExpr(idx.Index)
					if err != nil {
						return nil, err
					}
					return e.node("addrof", x, map[string]any{
						"array": arrN, "index": idxN, "type": tid,
					}), nil
				}
				xn, err := e.exportExpr(x.X)
				if err != nil {
					return nil, err
				}
				return e.node("addrof", x, map[string]any{
					"x": xn, "type": tid,
				}), nil
			}
			return nil, scopeErrorf("& on %T is not supported in frontend/v1 scope (WS D)", x.X)
		}
		// M8 closeout: constant-fold like BinaryExpr/ParenExpr. go/types
		// evaluates -0.0 to +0.0 (and all constant arithmetic exactly);
		// emitting a runtime negation would produce IEEE -0.0 instead.
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		o, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		return e.node("unary", x, map[string]any{"op": x.Op.String(), "x": o, "type": t}), nil
	case *ast.CompositeLit:
		return e.exportCompositeLit(x)
	case *ast.IndexExpr:
		// M6 (WS H): a generic function in value position (e.g. min[int]
		// passed as an argument) is an IndexExpr whose X denotes a generic
		// function recorded in info.Instances. It is a func value, not an
		// index operation: route it through the func-value path instead
		// of exportIndex (which would try to export the type argument as
		// an expression).
		if id, ok := x.X.(*ast.Ident); ok {
			if _, isInst := e.info.Instances[id]; isInst {
				if fn, ok := denotedFunc(e.info, id); ok {
					if fi, ok := e.funcByObj[fn]; ok && fi.isTemplate && fi.recv == nil {
						fields := map[string]any{"name": id.Name}
						if err := e.exportFuncValue(id, fn, fields); err != nil {
							return nil, err
						}
						return e.node("ident", x, fields), nil
					}
				}
			}
		}
		return e.exportIndex(x, false)
	case *ast.SliceExpr:
		return e.exportSlice(x)
	case *ast.SelectorExpr:
		return e.exportSelector(x)
	case *ast.StarExpr:
		return e.exportDeref(x)
	case *ast.TypeAssertExpr:
		return e.exportTypeAssert(x, false)
	case *ast.ParenExpr:
		if tv, ok := e.info.Types[x]; ok && tv.Value != nil {
			return e.foldedConst(x, tv.Value)
		}
		t, err := e.exprType(x)
		if err != nil {
			return nil, err
		}
		o, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		return e.node("paren", x, map[string]any{"x": o, "type": t}), nil
	case *ast.CallExpr:
		return e.exportCall(x)
	case *ast.FuncLit:
		return e.exportFuncLit(x)
	default:
		return nil, scopeErrorf("%T is not supported in frontend/v1 scope (%s)", x, exprOwner(x))
	}
}

// exprOwner attributes an unsupported expression to its workstream.
func exprOwner(x ast.Expr) string {
	switch x.(type) {
	case *ast.FuncType, *ast.StarExpr, *ast.IndexListExpr:
		return "WS D"
	case *ast.ChanType, *ast.MapType, *ast.ArrayType, *ast.StructType,
		*ast.InterfaceType:
		return "WS C/D"
	default:
		return "a later workstream"
	}
}

// exportIdent exports an identifier in expression position: a use of a
// declared variable, or a builtin function name.
func (e *exporter) exportIdent(id *ast.Ident) (*Node, error) {
	if _, ok := e.info.Defs[id]; ok {
		return nil, fmt.Errorf("gofrontend: definition-site identifier %q used as expression", id.Name)
	}
	obj, ok := e.info.Uses[id]
	if !ok {
		return nil, fmt.Errorf("gofrontend: unresolved identifier %q", id.Name)
	}
	fields := map[string]any{"name": id.Name}
	switch obj := obj.(type) {
	case *types.Var:
		oid, ok := e.objIDs[obj]
		if !ok {
			return nil, fmt.Errorf("gofrontend: variable %q used before declaration", id.Name)
		}
		t, err := e.typeID(obj.Type())
		if err != nil {
			return nil, err
		}
		fields["obj"] = oid
		fields["type"] = t
	case *types.Builtin:
		// P3-M8 (reviewer-2 P1-4): builtins have no go/types signature —
		// go/types NewBuiltin sets typ: Typ[Invalid] (object.go), so
		// obj.Type() is ALWAYS types.Typ[types.Invalid] here, not a
		// failed resolution. Serializing it minted a bogus "invalid type"
		// wire entry that ReflectTypes.java laundered into a fake
		// INTERFACE kind. The ident's type is dead on the wire: builtin
		// calls dispatch via the call node's "builtin" marker
		// (buildBuiltinCall), and a standalone builtin ident fails in
		// buildIdent on the missing "obj" field before "type" is read.
		// So the builtin ident form carries no "type" field.
		fields["builtin"] = obj.Name()
	case *types.Const:
		// Constants (universe true/false, package-level and local consts):
		// emitted as a folded basiclit so the factory's typed-constant
		// path handles them (M3, WS D). foldedConst supplies the binary
		// payloads (sbytes/f32bits/f64bits) the factory requires.
		return e.foldedConst(id, obj.Val())
	case *types.Nil:
		// M5 (WS F): the nil identifier in expression position. Typed as
		// the empty interface; the factory's NilNode evaluates to null,
		// which panic(nil) converts to *PanicNilError and which compares
		// equal to nil interfaces. (Full nil typing is WS D.)
		t, err := e.typeID(types.NewInterfaceType(nil, nil))
		if err != nil {
			return nil, err
		}
		return e.node("nil", id, map[string]any{"type": t}), nil
	case *types.Func:
		// M6 (WS H): reference to a named function or method (value
		// position, e.g. f := Min[int]). Emitted as an ident whose obj is
		// the function/instantiation object id; the factory builds a
		// func-value constant. Method values (bound receivers) are not
		// supported (WS H follow-up).
		if err := e.exportFuncValue(id, obj, fields); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("gofrontend: identifier %q denotes %T", id.Name, obj)
	}
	return e.node("ident", id, fields), nil
}

// exportFuncValue fills fields for a reference to a named function or
// method in value position (e.g. f := Min[int], or Min[int] passed as an
// argument). Generic functions resolve to their monomorphized
// instantiation via info.Instances.
func (e *exporter) exportFuncValue(id *ast.Ident, obj *types.Func, fields map[string]any) error {
	fi, ok := e.funcByObj[obj]
	if !ok {
		// Fallback to (package path, func name) for functions from
		// packages type-checked in isolation (synthetic or disk): the
		// *types.Func pointer from the importing package differs from
		// the one registered at import time.
		if e.synthFuncIDs != nil && obj.Pkg() != nil {
			key := obj.Pkg().Path() + "." + obj.Name()
			fi, ok = e.synthFuncIDs[key]
		}
	}
	if !ok {
		return fmt.Errorf("gofrontend: unknown function %s", obj.Name())
	}
	if fi.recv != nil {
		return scopeErrorf("method value %s (bound receiver) is not supported in frontend/v1 scope (WS H)", id.Name)
	}
	if fi.isTemplate {
		ii, ok := e.info.Instances[id]
		if !ok {
			return scopeErrorf("generic function value %s without instantiation is not supported (WS H)", id.Name)
		}
		targs := typeList(ii.TypeArgs)
		key := instKeyForFunc(fi, targs, e.subst)
		inst, ok := e.instByKey[key]
		if !ok {
			// WS G (P3-M9, module-probe lane): generic calls in imported
			// (synthetic or disk) package var initializers export during
			// importSyntheticPackage, before enumerateInstantiations runs.
			// Create the instantiation on demand (queued for body emission
			// via mtPending); the enumerator dedups it later.
			if _, err := e.ensureFuncInstantiation(fi, targs); err != nil {
				return err
			}
			inst, ok = e.instByKey[key]
			if !ok {
				return fmt.Errorf("gofrontend: instantiation %s not enumerated (internal error)", key)
			}
		}
		fields["obj"] = inst.instID
		fields["type"] = inst.sigID
	} else {
		t, err := e.typeID(obj.Type())
		if err != nil {
			return err
		}
		oid, err := e.defFunc(obj)
		if err != nil {
			return err
		}
		fields["obj"] = oid
		fields["type"] = t
	}
	return nil
}

// exportCall exports a call expression: either a Go conversion T(x) (the
// call target denotes a *types.TypeName), emitted as a "convert" node
// (M3, WS D — new node kind, additive per docs/frontend-v1.md §8), a call
// to runtime.Goexit (M5, WS F — the Phase-1 Goexit trigger), a call to a
// builtin function, a call of a function literal, or a call through a
// func-valued variable (M5, WS F — deferred calls and recursion). Other
// call shapes are scope errors (WS D).
func (e *exporter) exportCall(c *ast.CallExpr) (*Node, error) {
	// M5 (WS F): runtime.Goexit() — the Phase-1 Goexit trigger. Emitted as
	// a "call" node with the goexit marker; the factory builds GoexitNode.
	// M6 (WS H): method calls — possibly wrapped in an index expression
	// for explicit type arguments (s.M[T](...)).
	if sel, ok := unwrapSelector(c.Fun); ok {
		// M6 (WS H): fmt.X via the D4 import overlay.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "fmt" {
			if _, ok := e.info.Uses[pkgIdent].(*types.PkgName); ok {
				return e.exportFmtCall(c, sel)
			}
		}
		// P3-M4 (WS C, ADR-0004 D2): package-level sync/atomic functions
		// are defined with exact gc signatures but not implemented — they
		// need &x (WS D). Fail fast with the named scope error, never a
		// confusing "undefined".
		//
		// P3-driver-harden (jsonlib lane): atomic.LoadPointer lowers to
		// a plain deref read (scoped divergence: atomicity without gc's
		// ordering guarantee); atomic.StorePointer is statement-only and
		// handled in exportStmt.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "atomic" {
			if obj, ok := e.info.Uses[sel.Sel].(*types.Func); ok && obj.Pkg() != nil && obj.Pkg().Path() == "sync/atomic" {
				if sel.Sel.Name == "LoadPointer" {
					return e.exportAtomicLoadPointer(c)
				}
				// fix-linkname (Caddy/godebug lane): package-level atomic
				// word/bool ops (LoadUint64, StoreInt32, …). The address
				// operand evaluates to a GoPointer; the "atomicaddr"
				// wire node wraps it in a delegating AtomicLong /
				// AtomicInteger cell, and the existing "syncop" atomic
				// machinery (AtomicOpNode) performs the op — the same
				// node that serves the typed wrappers. RMW ops
				// (Add/Swap/CompareAndSwap) serialize on a shared lock
				// (documented in GoPointerCell64); Load/Store are plain
				// volatile-cell accesses, the exportAtomicLoadPointer
				// precedent.
				if n, handled, err := e.exportAtomicWordOp(c, sel.Sel.Name); handled {
					return n, err
				}
				return nil, scopeErrorf("sync/atomic.%s is not in M4 scope (package-level atomics need addressable operands, WS D)", sel.Sel.Name)
			}
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "runtime" && sel.Sel.Name == "Goexit" {
			if obj, ok := e.info.Uses[sel.Sel].(*types.Func); !ok || obj.Pkg() == nil || obj.Pkg().Name() != "runtime" {
				return nil, scopeErrorf("runtime.Goexit did not resolve to the overlay package")
			}
			if len(c.Args) != 0 {
				return nil, fmt.Errorf("gofrontend: runtime.Goexit takes no arguments")
			}
			return e.node("call", c, map[string]any{"goexit": true, "args": []*Node{}}), nil
		}
		// P3-M9 (driver-harden, go-cmp lane) note: runtime.FuncForPC was
		// once rewritten to a panic ("no PC-to-function table"). The
		// stack-inspection lane implements it for real via the
		// "runtimefuncforpc" wire node (PC interning in
		// graalgo.runtime.StackTrace), so the call falls through to the
		// general runtime interception below.
		// P2-M4 (GC interop, ADR-0002 D4): runtime.SetFinalizer/KeepAlive/GC.
		// Emitted as "call" nodes with the gcinterop marker naming the
		// intrinsic; the factory builds the corresponding CallNode which
		// delegates to graalgo.runtime.GcInterop. Marker is additive on the
		// v1 wire format (goexit-marker precedent) — no version bump.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "runtime" {
			if obj, ok := e.info.Uses[sel.Sel].(*types.Func); ok && obj.Pkg() != nil && obj.Pkg().Name() == "runtime" {
				switch sel.Sel.Name {
				case "SetFinalizer", "KeepAlive", "GC":
					want := map[string]int{"SetFinalizer": 2, "KeepAlive": 1, "GC": 0}[sel.Sel.Name]
					if len(c.Args) != want {
						return nil, fmt.Errorf("gofrontend: runtime.%s takes %d arguments", sel.Sel.Name, want)
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					return e.node("call", c, map[string]any{
						"gcinterop": sel.Sel.Name, "args": args,
					}), nil
				}
				// P4 (driver-harden): runtime.NumCPU(). Emitted as an
				// additive "runtimenumcpu" wire node; the backend returns
				// the host processor count.
				if sel.Sel.Name == "NumCPU" {
					if len(c.Args) != 0 {
						return nil, fmt.Errorf("gofrontend: runtime.NumCPU takes no arguments")
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimenumcpu", c, map[string]any{"type": typ}), nil
				}
				// P4 (driver-harden): runtime.GOMAXPROCS(n). Emitted as an
				// additive "runtimegomaxprocs" wire node.
				if sel.Sel.Name == "GOMAXPROCS" {
					if len(c.Args) != 1 {
						return nil, fmt.Errorf("gofrontend: runtime.GOMAXPROCS takes one argument")
					}
					arg, err := e.exportExpr(c.Args[0])
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimegomaxprocs", c, map[string]any{"x": arg, "type": typ}), nil
				}
				// MemStats family (unblocks runtime/debug, hence Badger via
				// testify): runtime.ReadMemStats, GOROOT, Version, Stack.
				// Emitted as additive wire nodes; the backend implements
				// them as intrinsics (see graalgo.runtime.MemStatsInterop).
				if sel.Sel.Name == "ReadMemStats" {
					if len(c.Args) != 1 {
						return nil, fmt.Errorf("gofrontend: runtime.ReadMemStats takes one argument")
					}
					arg, err := e.exportExpr(c.Args[0])
					if err != nil {
						return nil, err
					}
					return e.node("runtimereadmemstats", c, map[string]any{"m": arg}), nil
				}
				if sel.Sel.Name == "GOROOT" {
					if len(c.Args) != 0 {
						return nil, fmt.Errorf("gofrontend: runtime.GOROOT takes no arguments")
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimegoroot", c, map[string]any{"type": typ}), nil
				}
				if sel.Sel.Name == "Version" {
					if len(c.Args) != 0 {
						return nil, fmt.Errorf("gofrontend: runtime.Version takes no arguments")
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimeversion", c, map[string]any{"type": typ}), nil
				}
				if sel.Sel.Name == "Stack" {
					if len(c.Args) != 2 {
						return nil, fmt.Errorf("gofrontend: runtime.Stack takes two arguments")
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimestack", c, map[string]any{"args": args, "type": typ}), nil
				}
				// Stack-inspection family: runtime.Callers/CallersFrames/
				// FuncForPC are single-result intrinsics, emitted as
				// additive wire nodes (graalgo.rtstack). runtime.Caller,
				// (*Frames).Next and (*Func).FileLine are multi-result:
				// only the N-target assignment desugar (mrRuntimeStack in
				// mrdesugar_m5.go) may lower them — any other position
				// fails fast here with the named scope error.
				if sel.Sel.Name == "Callers" {
					if len(c.Args) != 2 {
						return nil, fmt.Errorf("gofrontend: runtime.Callers takes 2 arguments")
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimecallers", c, map[string]any{
						"skip": args[0], "pc": args[1], "type": typ,
					}), nil
				}
				if sel.Sel.Name == "CallersFrames" {
					if len(c.Args) != 1 {
						return nil, fmt.Errorf("gofrontend: runtime.CallersFrames takes 1 argument")
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimecallersframes", c, map[string]any{
						"pcs": args[0], "type": typ,
					}), nil
				}
				if sel.Sel.Name == "FuncForPC" {
					if len(c.Args) != 1 {
						return nil, fmt.Errorf("gofrontend: runtime.FuncForPC takes 1 argument")
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("runtimefuncforpc", c, map[string]any{
						"pc": args[0], "type": typ,
					}), nil
				}
				if sel.Sel.Name == "Caller" {
					return nil, scopeErrorf("runtime.Caller is multi-result: use pc, file, line, ok := runtime.Caller(skip) (4-target assignment) in frontend/v1 scope (WS D)")
				}
			}
		}
		// M6 (WS H): method call — receiver-based dispatch resolved by
		// go/types to a (possibly instantiated) method.
		// P3-M2b (sync bindings, ADR-0002 D2): method calls on the
		// synthetic sync primitives lower to additive "syncop" wire nodes,
		// bypassing exportMethodCall's WS D no-implicit-& rule.
		// P3-M5 (WS E, ADR-0005 D1): time.X(...) package-level calls are
		// intercepted above (exportSyntheticPkgCall); the real-Go
		// package-level functions (time.Since/Until/ParseDuration,
		// context.*) lower to direct calls of the imported synthetic
		// funcdecls via exportFuncCall.
		if pkgIdent, ok := sel.X.(*ast.Ident); ok {
			if pkgName, ok := e.info.Uses[pkgIdent].(*types.PkgName); ok {
				if n, handled, err := e.exportSyntheticPkgCall(c, sel, pkgName); handled {
					return n, err
				}
			}
		}
		// Stack-inspection methods: (*Func).Name/(*Func).Entry are
		// single-result intrinsics, emitted as additive wire nodes
		// (graalgo.rtstack). (*Frames).Next and (*Func).FileLine are
		// multi-result: only the mrRuntimeStack desugar may lower them
		// (direct exportCall positions fail fast with the named scope
		// error below).
		if selection, ok := e.info.Selections[sel]; ok && selection.Kind() == types.MethodVal {
			if mname, isRt := runtimeStackMethod(selection); isRt {
				switch mname {
				case "Name", "Entry":
					recv, err := e.exportExpr(sel.X)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					kind := "runtimefuncname"
					if mname == "Entry" {
						kind = "runtimefuncentry"
					}
					return e.node(kind, c, map[string]any{"f": recv, "type": typ}), nil
				default:
					return nil, scopeErrorf("runtime.%s is multi-result: use an N-target assignment (frame, more := frames.Next(); file, line := fn.FileLine(pc)) in frontend/v1 scope (WS D)", mname)
				}
			}
			if nt := syntheticPrimitiveRecv(selection.Recv()); nt != nil {
				return e.exportSyncOp(c, sel, nt)
			}
			// real-reflect: promoted sync primitive methods (e.g.
			// reflect's funcLookupCache.Lock() where funcLookupCache is
			// a package var of anonymous struct type embedding
			// sync.Mutex). The method resolves through the embedding
			// path to a sync primitive; adapt the receiver along the
			// path and emit the syncop node.
			if nt, path := promotedSyncPrimitive(selection); nt != nil {
				recv, err := e.exportExpr(sel.X)
				if err != nil {
					return nil, err
				}
				if len(path) > 0 {
					adapted := embeddedFieldType(selection.Recv(), path)
					if adapted == nil {
						return nil, fmt.Errorf("gofrontend: cannot adapt receiver for promoted sync method %s (exporter bug)", sel.Sel.Name)
					}
					tid, err := e.typeID(adapted)
					if err != nil {
						return nil, err
					}
					apath := make([]any, len(path))
					for i, v := range path {
						apath[i] = v
					}
					recv = e.node("selector", sel, map[string]any{
						"x": recv, "field": embeddedFieldName(selection.Recv(), path),
						"selpath": apath, "type": tid,
					})
				}
				args, err := e.exportCallArgs(c)
				if err != nil {
					return nil, err
				}
				return e.exportSyncOpNodes(c, sel, nt, recv, args)
			}
			// P3-M5 (WS E, ADR-0005 D1): (*time.Timer).Stop/Reset and
			// (*time.Ticker).Stop/Reset lower to additive "timeop" wire
			// nodes (the Go values are opaque Java objects).
			if nt := timeTimerTickerNamed(selection.Recv()); nt != nil {
				return e.exportTimeOp(c, sel, nt)
			}
			// P3-M8 (WS D): interface calls route to exportIfaceMethodCall FIRST —
			// it owns the multi-result decision (pairPlan dispatch for table
			// methods, fail-fast otherwise). The fail-fast below then applies
			// only to concrete (non-interface) calls.
			if recvIface(selection.Recv()) != nil {
				return e.exportIfaceMethodCall(c, sel, selection)
			}
			// P3-M5 (WS E, ADR-0005): multi-result synthetic methods
			// (Context.Deadline, Time.Date/...) fail fast (WS D; the
			// P3-M3 F4 precedent) — the shape must not reach dispatch.
			// (P3-M8: interface methods are handled above.)
			// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): if the
			// receiver is a type parameter, don't fail fast here — fall
			// through to exportMethodCall, which routes through
			// resolveMethodCall's WS H ifacemethod fallback (now
			// multi-result capable via the pair plan).
			if f, ok := selection.Obj().(*types.Func); ok && e.isMultiResultSyntheticFailFast(f) {
				if _, isTP := selection.Recv().(*types.TypeParam); !isTP {
					return nil, multiResultSyntheticError(f)
				}
			}
		}
		selection, ok := e.info.Selections[sel]
		if !ok {
			return nil, scopeErrorf("selector call %s.%s is not supported in frontend/v1 scope (WS D)",
				sel.X, sel.Sel.Name)
		}
		// P3-M9 (WS D, graalgo-ordinverse-generic): the selector may denote
		// a struct field of function type (e.g., c.ordInverse where
		// ordInverse is func(*[4]uint64)). go/types records field
		// selections in info.Selections with Kind() == FieldVal (not
		// MethodVal), so methodRefNode would reject them. Export as field
		// access followed by a call instead.
		// (The 13b0beb fix placed this logic inside the !ok branch, but
		// that branch is dead for fields — go/types always records a
		// Selections entry for field selections. This is the correct
		// placement.)
		if selection.Kind() == types.FieldVal {
			// P3-M11 (sync.Pool): `p.New()` — invoking the New func
			// field. The field read alone cannot serve as the call fun
			// (buildCall's fun kinds are closed), so lower the whole
			// call to the CallNew syncop.
			if nt := e.poolNewSelector(c.Fun); nt != nil {
				recv, err := e.exportExpr(sel.X)
				if err != nil {
					return nil, err
				}
				args, err := e.exportCallArgs(c)
				if err != nil {
					return nil, err
				}
				typ, err := e.callType(c)
				if err != nil {
					return nil, err
				}
				return e.syncFieldOp(c, "CallNew", nt, recv, args, typ), nil
			}
			if _, ok := selection.Obj().(*types.Var); ok {
				if _, ok := selection.Type().(*types.Signature); ok {
					// It's a function-typed field. Use exportSelector for
					// the field access, then call the resulting function
					// value.
					fieldNode, err := e.exportSelector(sel)
					if err != nil {
						return nil, err
					}
					args, err := e.exportCallArgs(c)
					if err != nil {
						return nil, err
					}
					resType, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					fields := map[string]any{
						"fun": fieldNode, "args": args,
					}
					if resType != nil {
						fields["type"] = resType
					}
					return e.node("call", c, fields), nil
				}
				// The field itself is not func-typed, but the call target
				// (selector plus index/paren wrappers) evaluates to a
				// function value — e.g. ms.less[k](p, q) where less is
				// []func(...), or m.handlers[name](args). The wrapper
				// selects a func value out of the field's type. Export the
				// whole target expression and emit a plain call node —
				// the computed-func-value shape the factory already
				// builds (func-valued variable / reflect precedent).
				// Parens are transparent: unwrap them so the factory sees
				// the operative fun shape (index, never paren).
				funExpr := c.Fun
				for {
					if p, ok := funExpr.(*ast.ParenExpr); ok {
						funExpr = p.X
					} else {
						break
					}
				}
				if tv, ok := e.info.Types[funExpr]; ok && tv.Type != nil {
					if sig, ok := tv.Type.Underlying().(*types.Signature); ok {
						fun, err := e.exportExpr(funExpr)
						if err != nil {
							return nil, err
						}
						args, err := e.exportCallArgsPacked(c, sig)
						if err != nil {
							return nil, err
						}
						typ, err := e.callType(c)
						if err != nil {
							return nil, err
						}
						return e.node("call", c, map[string]any{
							"fun": fun, "args": args, "type": typ,
						}), nil
					}
				}
			}
			// Non-function field in call position: fall through to the
			// scope error below (invalid Go, but give a named error).
			return nil, scopeErrorf("selector call %s.%s is not supported in frontend/v1 scope (WS D)",
				sel.X, sel.Sel.Name)
		}
		// P3-M3 (WS D, ADR-0003): method expressions on interfaces
		// (Writer.Write(w, ...)) keep failing, but with a named P3-M3
		// scope error (greppable re-test trigger) instead of the WS D
		// one below.
		if selection.Kind() == types.MethodExpr && recvIface(selection.Recv()) != nil {
			return nil, scopeErrorf("method expression %s is not supported: method expression via interface (P3-M3 scope)", sel.Sel.Name)
		}
		return e.exportMethodCall(c, sel, selection)
	}
	// M5 (WS F): immediately-called function literal, e.g. the canonical
	// deferred shape `defer func() { ... }()`.
	if lit, ok := c.Fun.(*ast.FuncLit); ok {
		fun, err := e.exportFuncLit(lit)
		if err != nil {
			return nil, err
		}
		var litSig *types.Signature
		if tv, ok := e.info.Types[lit]; ok && tv.Type != nil {
			if sig, ok := tv.Type.(*types.Signature); ok {
				litSig = sig
			}
		}
		args, err := e.exportCallArgsPacked(c, litSig)
		if err != nil {
			return nil, err
		}
		typ, err := e.callType(c)
		if err != nil {
			return nil, err
		}
		return e.node("call", c, map[string]any{
			"fun": fun, "args": args, "type": typ,
		}), nil
	}
	funIdent := callTargetIdent(c.Fun)
	if funIdent == nil {
		// M7: conversion with a composite type target, e.g. []byte("hi").
		// The target is an *ast.ArrayType/*ast.MapType, not an identifier.
		if tv, ok := e.info.Types[c.Fun]; ok && tv.Type != nil {
			// P3-M8 (wave 2, reflect): call to a computed func value,
			// e.g. reflect.Swapper(s)(0, 2) — c.Fun is a call expression
			// whose type is a signature, not a composite type target.
			// Export the fun expression and emit a plain call node
			// (mirrors the func-valued variable branch below).
			// real-reflect (Phase 3): a CONVERSION to a func type, e.g.
			// (func())(nil) in reflect.FuncOf, has a Signature-typed
			// Fun too — but the Fun is a TYPE (IsType), not a value, so
			// it must take the conversion path below, not the call path.
			if !tv.IsType() {
				if sig, ok := tv.Type.Underlying().(*types.Signature); ok {
					fun, err := e.exportExpr(c.Fun)
					if err != nil {
						return nil, err
					}
					args, err := e.exportCallArgsPacked(c, sig)
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("call", c, map[string]any{
						"fun": fun, "args": args, "type": typ,
					}), nil
				}
			}
			if len(c.Args) != 1 {
				return nil, fmt.Errorf("gofrontend: conversion has %d arguments (want 1)", len(c.Args))
			}
			// `T(nil)` — the nil must be typed as T, not as the empty
			// interface (the default nil export). Otherwise the backend
			// sees INTERFACE -> T (illegal).
			var x *Node
			if id, ok := c.Args[0].(*ast.Ident); ok && id.Name == "nil" {
				to, err := e.typeID(tv.Type)
				if err != nil {
					return nil, err
				}
				x = e.node("nil", id, map[string]any{"type": to})
			} else {
				var err error
				x, err = e.exportExpr(c.Args[0])
				if err != nil {
					return nil, err
				}
			}
			to, err := e.typeID(tv.Type)
			if err != nil {
				return nil, err
			}
			// P3-M8: unsafe.Pointer conversions (synth_unsafe.go); see hunk 1.
			if n, handled, err := e.exportUnsafeConversion(c, tv.Type, x, to); handled {
				return n, err
			}
			// M7: "from" records the source type id.
			var from any
			if atv, ok := e.info.Types[c.Args[0]]; ok && atv.Type != nil {
				fid, err := e.typeID(atv.Type)
				if err != nil {
					return nil, err
				}
				from = fid
			}
			fields := map[string]any{"x": x, "to": to, "type": to}
			if from != nil {
				fields["from"] = from
			}
			return e.node("convert", c, fields), nil
		}
		return nil, scopeErrorf("non-identifier call target %T is not supported in frontend/v1 scope (WS D)", c.Fun)
	}
	if tn, ok := e.info.Uses[funIdent].(*types.TypeName); ok {
		return e.exportTypeConversion(c, tn)
	}
	if v, ok := e.info.Uses[funIdent].(*types.Var); ok {
		// M5 (WS F): call through a func-valued variable (recursion,
		// `defer f()`). The variable's signature type is already in the
		// type table; the factory builds a func-value call node.
		// P3-M5 (WS E, ADR-0005 App M): the variable may have a NAMED
		// func type (context.CancelFunc) — unwrap to the underlying
		// signature.
		sig, ok := v.Type().Underlying().(*types.Signature)
		if !ok {
			return nil, scopeErrorf("call of non-function variable %s is not supported in frontend/v1 scope (WS D)",
				funIdent.Name)
		}
		fun, err := e.exportIdent(funIdent)
		if err != nil {
			return nil, err
		}
		args, err := e.exportCallArgsPacked(c, sig)
		if err != nil {
			return nil, err
		}
		typ, err := e.callType(c)
		if err != nil {
			return nil, err
		}
		return e.node("call", c, map[string]any{
			"fun": fun, "args": args, "type": typ,
		}), nil
	}
	if fn, ok := e.info.Uses[funIdent].(*types.Func); ok {
		// M6 (WS H): call to a named top-level function (or a
		// monomorphized generic instantiation). The call target resolves
		// by object id; the factory builds a direct func-value call.
		return e.exportFuncCall(c, funIdent, fn)
	}
	b, ok := e.info.Uses[funIdent].(*types.Builtin)
	if !ok {
		return nil, scopeErrorf("calls to non-builtin functions are not supported in frontend/v1 scope (WS D)")
	}
	// M8: fail fast on builtins outside the v1 surface (fuzzer find: clear/
	// min/max used to slip through and die as "internal error building AST"
	// in NodeFactory). The surface is freeze-stub.md "Builtin nodes".
	switch b.Name() {
	case "len", "cap", "make", "new", "append", "copy", "delete", "clear",
		"complex", "real", "imag", "print", "println", "panic", "recover",
		"min", "max":
	default:
		return nil, scopeErrorf("call to builtin %q is not supported in frontend/v1 scope (WS G intrinsic surface)", b.Name())
	}
	fun, err := e.exportIdent(funIdent)
	if err != nil {
		return nil, err
	}
	// M7: new(T) and make(T, ...) take a type as the first argument.
	// The type is recorded in the result type id (via callType); only
	// the value arguments are exported as expressions.
	// Non-nil so empty args marshal as [] rather than null.
	args := []*Node{}
	switch b.Name() {
	case "new":
		// new(T): no value arguments; the pointer type is the result.
		// P3-M2b (ADR-0002 D3): new(sync.X) constructs the primitive.
		// P3-M4 (WS C, ADR-0004): new(atomic.X) likewise.
		if len(c.Args) == 1 {
			if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
				if nt := syntheticPrimitiveNamed(tv.Type); nt != nil {
					ntyp, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					return e.node("syncnew", c, map[string]any{
						"synctyp": syntheticOpTypeName(nt), "type": ntyp,
					}), nil
				}
				// P3-M5 (WS E, ADR-0005 D1): new(time.Timer/Ticker) is
				// not in M5 scope (gc's zero Timer is uninitialized).
				if nt := timeTimerTickerNamedExact(tv.Type); nt != nil {
					return nil, timerScopeErrorf(nt, "new()")
				}
			}
		}
	case "len", "cap":
		// P3-M5 (WS E, ADR-0005 App C): len(cap)/cap(chan) lower to the
		// additive "chanlen"/"chancap" nodes — the frozen LenNode rejects
		// CHAN (WS G). DIV-M3-1: timer channels report 0 via TimerChan.
		// Named channel types match via Underlying (P2-2).
		if len(c.Args) == 1 {
			if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
				if _, ok := tv.Type.Underlying().(*types.Chan); ok {
					x, err := e.exportExpr(c.Args[0])
					if err != nil {
						return nil, err
					}
					typ, err := e.callType(c)
					if err != nil {
						return nil, err
					}
					kind := "chanlen"
					if b.Name() == "cap" {
						kind = "chancap"
					}
					return e.node(kind, c, map[string]any{"x": x, "type": typ}), nil
				}
			}
		}
		// Non-chan: fall through to default builtin handling (frozen LenNode).
		var err error
		args, err = e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
	case "make":
		// make(T, len[, cap]): skip the type, export len/cap.
		if len(c.Args) < 1 {
			return nil, scopeErrorf("make without a type argument")
		}
		// P3-M5 (WS E, ADR-0005 App C): make(chan T[, n]) lowers to the
		// additive "makechan" node — the frozen builtin make path
		// rejects chan types, so we never send it one. Named channel
		// types match via Underlying (P2-2).
		if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
			if ct, ok := tv.Type.Underlying().(*types.Chan); ok {
				return e.exportMakeChan(c, ct)
			}
		}
		for _, a := range c.Args[1:] {
			x, err := e.exportExpr(a)
			if err != nil {
				return nil, err
			}
			args = append(args, x)
		}
	case "min", "max":
		// P4 (driver-harden): min/max lower to the additive "minmax" wire
		// node — the frozen v1 builtin path rejects them (WS G), but real
		// modules (e.g. uuid) use min. General: works for all ordered
		// types with Go 1.21 NaN-propagating float semantics.
		if len(c.Args) < 1 {
			return nil, scopeErrorf("%s requires at least one argument", b.Name())
		}
		var err error
		args, err = e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
		typ, err := e.callType(c)
		if err != nil {
			return nil, err
		}
		return e.node("minmax", c, map[string]any{
			"op": b.Name(), "args": args, "type": typ,
		}), nil
	case "clear":
		// P4 (driver-harden): clear lowers to the additive "clear" wire
		// node — the frozen v1 builtin path rejects it (WS G), but real
		// modules use it. General: maps and slices per Go 1.21 semantics.
		if len(c.Args) != 1 {
			return nil, scopeErrorf("clear requires exactly one argument")
		}
		x, err := e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
		return e.node("clear", c, map[string]any{"x": x}), nil
	default:
		var err error
		args, err = e.exportCallArgs(c)
		if err != nil {
			return nil, err
		}
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{
		"fun": fun, "args": args, "builtin": b.Name(), "type": typ,
	}
	// M7: mark append(s, x...) spread; the factory expands the string
	// into individual bytes.
	if b.Name() == "append" && c.Ellipsis.IsValid() {
		fields["ellipsis"] = true
	}
	return e.node("call", c, fields), nil
}

// exportAtomicLoadPointer lowers atomic.LoadPointer(addr) to *addr: a
// plain deref read of the unsafe.Pointer. Scoped divergence from gc (see
// the exportCall interception): atomicity without the ordering guarantee.
func (e *exporter) exportAtomicLoadPointer(c *ast.CallExpr) (*Node, error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("gofrontend: atomic.LoadPointer takes 1 argument")
	}
	px, err := e.exportExpr(c.Args[0])
	if err != nil {
		return nil, err
	}
	tid, err := e.typeID(types.Typ[types.UnsafePointer])
	if err != nil {
		return nil, err
	}
	return e.node("deref", c, map[string]any{"x": px, "type": tid}), nil
}

// atomicWordOp describes one package-level sync/atomic word/bool
// operation (fix-linkname): the syncop op name, the atomic synctyp
// (shared with the typed wrappers so AtomicOpNode serves both), the
// atomicaddr cell kind, and the value-argument count after the address.
type atomicWordOp struct {
	op      string // syncop op: Load/Store/Add/Swap/CompareAndSwap
	synctyp string // e.g. "atomic.Uint64"
	cell    string // atomicaddr cell kind: "int64", "int32", "bool"
	nargs   int    // value args after addr
}

// atomicWordOps is the package-level atomic word/bool surface lowered to
// the atomicaddr+syncop wire nodes (fix-linkname, Caddy/godebug lane).
// gc's exact function names (verified against
// ~/tools/go-1.27.1/src/sync/atomic/doc.go). And/Or, the Pointer/Value
// package funcs, and anything else stay M4: the caller fails fast with
// the named scope error.
var atomicWordOps = map[string]atomicWordOp{
	"LoadInt32":             {"Load", "atomic.Int32", "int32", 0},
	"StoreInt32":            {"Store", "atomic.Int32", "int32", 1},
	"AddInt32":              {"Add", "atomic.Int32", "int32", 1},
	"SwapInt32":             {"Swap", "atomic.Int32", "int32", 1},
	"CompareAndSwapInt32":   {"CompareAndSwap", "atomic.Int32", "int32", 2},
	"LoadInt64":             {"Load", "atomic.Int64", "int64", 0},
	"StoreInt64":            {"Store", "atomic.Int64", "int64", 1},
	"AddInt64":              {"Add", "atomic.Int64", "int64", 1},
	"SwapInt64":             {"Swap", "atomic.Int64", "int64", 1},
	"CompareAndSwapInt64":   {"CompareAndSwap", "atomic.Int64", "int64", 2},
	"LoadUint32":            {"Load", "atomic.Uint32", "int32", 0},
	"StoreUint32":           {"Store", "atomic.Uint32", "int32", 1},
	"AddUint32":             {"Add", "atomic.Uint32", "int32", 1},
	"SwapUint32":            {"Swap", "atomic.Uint32", "int32", 1},
	"CompareAndSwapUint32":  {"CompareAndSwap", "atomic.Uint32", "int32", 2},
	"LoadUint64":            {"Load", "atomic.Uint64", "int64", 0},
	"StoreUint64":           {"Store", "atomic.Uint64", "int64", 1},
	"AddUint64":             {"Add", "atomic.Uint64", "int64", 1},
	"SwapUint64":            {"Swap", "atomic.Uint64", "int64", 1},
	"CompareAndSwapUint64":  {"CompareAndSwap", "atomic.Uint64", "int64", 2},
	"LoadUintptr":           {"Load", "atomic.Uintptr", "int64", 0},
	"StoreUintptr":          {"Store", "atomic.Uintptr", "int64", 1},
	"AddUintptr":            {"Add", "atomic.Uintptr", "int64", 1},
	"SwapUintptr":           {"Swap", "atomic.Uintptr", "int64", 1},
	"CompareAndSwapUintptr": {"CompareAndSwap", "atomic.Uintptr", "int64", 2},
	// Note: no Bool entries — gc has no package-level bool atomics.
}

// exportAtomicWordOp lowers a package-level sync/atomic word/bool call
// (atomic.LoadUint64(&x), …) to the atomicaddr+syncop wire nodes.
// The address operand is exported as a *T expression (a GoPointer at
// runtime); "atomicaddr" wraps it in a delegating AtomicLong /
// AtomicInteger cell so the existing AtomicOpNode serves the op
// unchanged. Returns handled=false for names outside atomicWordOps.
func (e *exporter) exportAtomicWordOp(c *ast.CallExpr, name string) (*Node, bool, error) {
	desc, ok := atomicWordOps[name]
	if !ok {
		return nil, false, nil
	}
	if len(c.Args) != desc.nargs+1 {
		return nil, true, fmt.Errorf("gofrontend: atomic.%s takes %d arguments, got %d",
			name, desc.nargs+1, len(c.Args))
	}
	addr, err := e.exportExpr(c.Args[0])
	if err != nil {
		return nil, true, err
	}
	var args = make([]*Node, 0, len(c.Args)-1)
	for _, a := range c.Args[1:] {
		x, err := e.exportExpr(a)
		if err != nil {
			return nil, true, err
		}
		args = append(args, x)
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, true, err
	}
	recv := e.node("atomicaddr", c, map[string]any{"addr": addr, "cell": desc.cell})
	return e.node("syncop", c, map[string]any{
		"op": desc.op, "synctyp": desc.synctyp, "recv": recv, "args": args, "type": typ,
	}), true, nil
}

// exportAtomicStorePointerStmt lowers a bare-statement
// atomic.StorePointer(addr, val) call to *addr = val: a plain deref write
// of the unsafe.Pointer. Same scoped divergence as exportAtomicLoadPointer.
func (e *exporter) exportAtomicStorePointerStmt(s *ast.ExprStmt, c *ast.CallExpr) (*Node, bool, error) {
	sel, ok := unwrapSelector(c.Fun)
	if !ok {
		return nil, false, nil
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || pkgIdent.Name != "atomic" {
		return nil, false, nil
	}
	obj, ok := e.info.Uses[sel.Sel].(*types.Func)
	if !ok || obj.Pkg() == nil || obj.Pkg().Path() != "sync/atomic" || sel.Sel.Name != "StorePointer" {
		return nil, false, nil
	}
	if len(c.Args) != 2 {
		return nil, true, fmt.Errorf("gofrontend: atomic.StorePointer takes 2 arguments")
	}
	px, err := e.exportExpr(c.Args[0])
	if err != nil {
		return nil, true, err
	}
	vx, err := e.exportExpr(c.Args[1])
	if err != nil {
		return nil, true, err
	}
	tid, err := e.typeID(types.Typ[types.UnsafePointer])
	if err != nil {
		return nil, true, err
	}
	lhs := e.node("deref", c, map[string]any{"x": px, "type": tid})
	return e.node("assign", s, map[string]any{
		"tok": "=", "lhs": []*Node{lhs}, "rhs": []*Node{vx},
	}), true, nil
}

// exportFmtCall exports a call to one of the seven D4 fmt stub functions
// (M6, WS H — the Phase-1 fmt import overlay, PLAN.md D4). Any other fmt.X
// is a scope error naming Phase 3.
func (e *exporter) exportFmtCall(c *ast.CallExpr, sel *ast.SelectorExpr) (*Node, error) {
	name := sel.Sel.Name
	if !fmtOverlayFuncs[name] {
		return nil, scopeErrorf("fmt.%s is not supported in frontend/v1 scope (Phase 3)", name)
	}
	if obj, ok := e.info.Uses[sel.Sel].(*types.Func); !ok || obj.Pkg() == nil || obj.Pkg().Name() != "fmt" {
		return nil, scopeErrorf("fmt.%s did not resolve to the overlay package", name)
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	// The Print variants return (int, error); Phase 1 supports them in
	// statement position only (results discarded) — the wire type is null.
	// The F-family (Fprint/Fprintf/Fprintln) is the same: the backend's
	// FmtCallNode writes to the writer and discards (n, err).
	// The Sprint variants and Errorf return a single value.
	var typ any
	switch name {
	case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
		typ = nil
	default:
		typ, err = e.callType(c)
		if err != nil {
			return nil, err
		}
	}
	return e.node("fmtcall", c, map[string]any{"func": name, "args": args, "type": typ}), nil
}

// exportFuncCall exports a call to a named top-level function (M6, WS H).
// Generic functions resolve to their monomorphized instantiation via
// info.Instances; the call target is an "ident" node whose obj is the
// function/instantiation object id.
func (e *exporter) exportFuncCall(c *ast.CallExpr, funIdent *ast.Ident, fn *types.Func) (*Node, error) {
	// P3-M5 (WS E, ADR-0005 D1): calls to synthetic time intrinsics from
	// inside the synthetic packages (e.g. Since calling Now) lower to
	// the §D3 wire nodes. Neither is exported as a funcdecl, so they must
	// be intercepted before the funcByObj lookup.
	if isTimeIntrinsicFunc(fn) {
		return e.exportTimeIntrinsic(c, fn.Name())
	}
	// P3-M7 (WS G, ADR-0007 D5): math intrinsics called from within the
	// synthetic math package (e.g. Float64bits in Abs) lower to the
	// "math*" wire nodes. Intercepted before the funcByObj lookup.
	if isMathIntrinsicFunc(fn) {
		return e.exportMathIntrinsic(c, fn.Name())
	}
	// P3-M8 (wave 2, reflect): reflect intrinsics called from within the
	// synthetic reflect package (e.g. __reflect_tKind in rtype.Kind) lower
	// to the "reflectop" wire node. Intercepted before the funcByObj lookup.
	if isReflectIntrinsicFunc(fn) {
		return e.exportReflectIntrinsic(c, fn.Name())
	}
	// P3-M9 (unsafe patterns): crypto/internal/fips140/alias.AnyOverlap and
	// InexactOverlap use uintptr(unsafe.Pointer(...)) which traps under D5.
	// Lower to the "sliceoverlap" wire node which implements overlap detection
	// directly on GoSlice values. Intercepted before the funcByObj lookup.
	if isAliasIntrinsicFunc(fn) {
		return e.exportAliasIntrinsic(c, fn.Name())
	}
	// real-reflect (Phase 1): internal/abi FuncPCABI0/FuncPCABIInternal are
	// gc compile intrinsics declared bodyless. Lower to the "abifuncpc"
	// wire node (opaque stable func id). Intercepted before the funcByObj
	// lookup; the declarations emit a synthesized intrinsic body (see
	// exportAbiFuncPCDecl).
	if isAbiFuncPCIntrinsicFunc(fn) {
		return e.exportAbiFuncPCIntrinsic(c, fn.Name())
	}
	// P3-M5 (WS E, ADR-0005): multi-result synthetic functions fail fast
	// (WS D; F4 precedent) — e.g. a synthetic-internal call to
	// time.ParseDuration.
	if e.isMultiResultSyntheticFailFast(fn) {
		return nil, multiResultSyntheticError(fn)
	}
	fi, ok := e.funcByObj[fn]
	if !ok {
		// P3-M9: fallback to (package path, func name) for synthetic
		// funcs (type-checked in isolation, pointers not shared).
		if e.synthFuncIDs != nil && fn.Pkg() != nil {
			key := fn.Pkg().Path() + "." + fn.Name()
			fi, ok = e.synthFuncIDs[key]
			// P3-M9: fn.Pkg() can be wrong when the same function name
			// exists in multiple synthetic packages (e.g. P256 in both
			// crypto/elliptic and crypto/ecdh). If the direct lookup fails,
			// try to resolve via the call's package qualifier.
			if !ok {
				if sel, isSel := c.Fun.(*ast.SelectorExpr); isSel {
					if pkgIdent, isIdent := sel.X.(*ast.Ident); isIdent {
						if pkgName, isPkg := e.info.Uses[pkgIdent].(*types.PkgName); isPkg {
							altKey := pkgName.Imported().Path() + "." + fn.Name()
							fi, ok = e.synthFuncIDs[altKey]
						}
					}
				}
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("gofrontend: call to unknown function %s", fn.Name())
	}
	var fnSig *types.Signature
	if sig, ok := fn.Type().(*types.Signature); ok {
		fnSig = sig
	}
	var objID, sigID int
	if fi.isTemplate {
		ii, ok := e.info.Instances[funIdent]
		if !ok {
			return nil, fmt.Errorf("gofrontend: no instantiation recorded for call to generic function %s", fn.Name())
		}
		targs := typeList(ii.TypeArgs)
		key := instKeyForFunc(fi, targs, e.subst)
		inst, ok := e.instByKey[key]
		if !ok {
			// WS G (P3-M9, module-probe lane): generic calls in imported
			// (synthetic or disk) package var initializers export during
			// importSyntheticPackage, before enumerateInstantiations runs.
			// Create the instantiation on demand (queued for body emission
			// via mtPending); the enumerator dedups it later.
			if _, err := e.ensureFuncInstantiation(fi, targs); err != nil {
				return nil, err
			}
			inst, ok = e.instByKey[key]
			if !ok {
				return nil, fmt.Errorf("gofrontend: instantiation %s not enumerated (internal error)", key)
			}
		}
		objID = inst.instID
		sigID = inst.sigID
	} else {
		objID = fi.funcID
		var err error
		sigID, err = e.typeID(fn.Type())
		if err != nil {
			return nil, err
		}
	}
	fun := e.node("ident", funIdent, map[string]any{
		"name": fn.Name(), "obj": objID, "type": sigID,
	})
	args, err := e.exportCallArgsPacked(c, fnSig)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": typ,
	}), nil
}

// methodRefNode resolves a MethodVal selector to a "methodref" fun node
// carrying the resolved function/instantiation object id, with a
// caller-supplied receiver node (the receiver is NOT exported here).
// It mirrors exportMethodCall's resolution exactly; the factory
// evaluates the receiver first and passes it as params[0].
// Used by exportGoWithArgs to hoist the receiver to go-statement time
// (P3-M5 remediation, reviewer1 P2-3).

// implicitAddrOf implements Go's implicit address-of for method calls
// (P3-M9, WS D). If the method's declared receiver is *T but the receiver
// expression has type T and is addressable, wrap the receiver node in an
// "addrof" wire node (x.M() → (&x).M()). Returns true if the adaptation
// was applied, false if the types don't match the implicit-& pattern.
func (e *exporter) implicitAddrOf(sel *ast.SelectorExpr, recv **Node, recvType *types.Type, declRecv types.Type, subst *typeSubst) bool {
	// declRecv is the method's declared receiver type (after subst).
	// Check if it's a pointer type.
	declPtr, ok := declRecv.(*types.Pointer)
	if !ok {
		// P3-M9: implicit deref for method calls (WS D). If the method's
		// declared receiver is T (value) but the receiver expression has
		// type *T (pointer), dereference implicitly per Go spec
		// (p.M() → (*p).M()).
		return e.implicitDeref(sel, recv, recvType, declRecv, subst)
	}
	// Check if recvType matches the pointer's element type.
	declElemStr := substString(declPtr.Elem(), subst)
	recvStr := substString(*recvType, e.subst)
	if declElemStr != recvStr {
		return false
	}
	// Check if sel.X is addressable.
	if !isAddressable(sel.X, e.info) {
		return false
	}
	// Wrap in addrof node.
	tid, err := e.typeID(declRecv)
	if err != nil {
		return false
	}
	*recv = e.node("addrof", sel.X, map[string]any{
		"x":    *recv,
		"type": tid,
	})
	*recvType = declRecv
	return true
}

// implicitDeref implements Go's implicit dereference for method calls
// (P3-M9, WS D). If the method's declared receiver is T (value type) but
// the receiver expression has type *T (pointer), wrap in a "deref" wire
// node (p.M() → (*p).M()). Returns true if applied.
func (e *exporter) implicitDeref(sel *ast.SelectorExpr, recv **Node, recvType *types.Type, declRecv types.Type, subst *typeSubst) bool {
	// recvType must be a pointer type.
	recvPtr, ok := (*recvType).(*types.Pointer)
	if !ok {
		return false
	}
	// Check if declRecv matches the pointer's element type.
	recvElemStr := substString(recvPtr.Elem(), e.subst)
	declStr := substString(declRecv, subst)
	if recvElemStr != declStr {
		return false
	}
	// Wrap in deref node.
	tid, err := e.typeID(declRecv)
	if err != nil {
		return false
	}
	*recv = e.node("deref", sel.X, map[string]any{
		"x":    *recv,
		"type": tid,
	})
	*recvType = declRecv
	return true
}

// isAddressable reports whether an expression is addressable per Go spec.
// Addressable expressions: variables, pointer indirections, slice indexing,
// field selectors of addressable values, and composite literals (via &T{}).
func isAddressable(x ast.Expr, info *types.Info) bool {
	switch x := x.(type) {
	case *ast.ParenExpr:
		// (x) is addressable iff x is — parentheses are transparent.
		return isAddressable(x.X, info)
	case *ast.Ident:
		// Variables are addressable; constants, builtins, and nil are not.
		if obj := info.ObjectOf(x); obj != nil {
			_, isVar := obj.(*types.Var)
			return isVar
		}
		return false
	case *ast.SelectorExpr:
		// x.f is addressable if x is addressable.
		if isAddressable(x.X, info) {
			return true
		}
		// p.f where p has pointer type is shorthand for (*p).f; a
		// pointer indirection is always addressable, so the selected
		// field is addressable regardless of whether p itself is.
		// Covers type assertions to pointer types, e.g.
		// &mt.(*rtype).t in reflect (method.go) — the assertion is not
		// itself an addressable expression, but its pointer-typed
		// result selects an addressable field.
		if t := info.TypeOf(x.X); t != nil {
			if _, ok := types.Unalias(t).(*types.Pointer); ok {
				return true
			}
		}
		// real-reflect (Phase 1): &(*T)(unsafe.Pointer(p)).f — the
		// descriptor-downcast pattern used throughout internal/abi
		// (Type.Uncommon) and reflect. The unsafe.Pointer<->*T casts
		// reinterpret (not copy) the memory p points to — the handle
		// model round-trips the same storage (unsafefromptr/
		// unsafetooptr) — so the field selection addresses real storage.
		return isUnsafeReinterpret(x.X, info)
	case *ast.IndexExpr:
		// s[i] is addressable if s is a slice, array, or pointer to array
		// (not a map or string). P3-M9: arrays were missing — &powers[i]
		// on [1<<n]nat failed (math/big karatsuba). P3-M9: pointer-to-array
		// was missing — tables[i].Select where tables is *[N]p224Table
		// failed (nistec).
		if tv, ok := info.Types[x.X]; ok && tv.Type != nil {
			ut := tv.Type.Underlying()
			switch ut.(type) {
			case *types.Slice, *types.Array:
				return true
			case *types.Pointer:
				// Pointer to array: (*tables)[i] is addressable.
				if _, isArr := ut.(*types.Pointer).Elem().Underlying().(*types.Array); isArr {
					return true
				}
			}
		}
		return false
	case *ast.StarExpr:
		// *p is addressable (it's a dereference of a pointer).
		return true
	default:
		return false
	}
}

// isUnsafeReinterpret reports whether x is a conversion of the form
// (*T)(unsafe.Pointer(y)) where T is a struct type: a reinterpretation of
// y's memory, not a copy. The unsafe.Pointer<->*T casts round-trip the
// same storage in the handle model (unsafefromptr/unsafetooptr), so a
// field selected through such a cast addresses real storage — the
// descriptor-downcast pattern (&(*structTypeUncommon)(unsafe.Pointer(t)).u)
// used by internal/abi (Type.Uncommon) and reflect.
func isUnsafeReinterpret(x ast.Expr, info *types.Info) bool {
	call, ok := x.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	// The target must be a pointer-to-struct type (a conversion, not a
	// call — a call target's type is a *types.Signature).
	dst, ok := types.Unalias(info.TypeOf(call.Fun)).(*types.Pointer)
	if !ok {
		return false
	}
	if _, ok := types.Unalias(dst.Elem()).Underlying().(*types.Struct); !ok {
		return false
	}
	// The source must be unsafe.Pointer.
	src := info.TypeOf(call.Args[0])
	return src != nil && types.Unalias(src) == types.Typ[types.UnsafePointer]
}

func (e *exporter) methodRefNode(c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection, recv *Node) (*Node, error) {
	if selection.Kind() != types.MethodVal {
		return nil, scopeErrorf("selector call %s.%s is not supported in frontend/v1 scope (WS D)",
			sel.X, sel.Sel.Name)
	}
	var mtargs []types.Type
	if ii, ok := e.info.Instances[sel.Sel]; ok {
		mtargs = typeList(ii.TypeArgs)
	}
	mc, err := e.resolveMethodCall(sel, selection, mtargs, e.subst)
	if err != nil {
		return nil, err
	}
	// P3-M9: synthetic method with no backend (panicMsg). Emit a runtime
	// panic so the enclosing body exports; an actual call fails fast.
	if mc.panicMsg != "" {
		panicCall := &ast.CallExpr{
			Fun: ast.NewIdent("panic"),
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.STRING, Value: `"` + mc.panicMsg + `"`},
			},
		}
		return e.exportCall(panicCall)
	}
	// P3-M9 (WS H, graalgo-wsh-fallback): ifacemethod fallback. When
	// devirtualization fails (type parameter's concrete type is not a
	// named type), dispatch via the constraint interface. The ifacemethod
	// node is a complete call (not a function reference), so return it
	// directly; the caller (exportMethodCall) detects the kind and skips
	// the "call" wrapper.
	// P3-M9 (driver-harden, go-cmp lane): for interface-promoted methods
	// (embedded interface fields), adapt the receiver by walking recvPath
	// with a selector node, so dispatch happens on the field, not the
	// outer struct.
	if mc.ifaceFallback != nil {
		if len(mc.recvPath) > 0 {
			adapted := embeddedFieldType(selection.Recv(), mc.recvPath)
			if adapted == nil {
				return nil, fmt.Errorf("gofrontend: cannot adapt receiver for promoted interface method %s (exporter bug)", sel.Sel.Name)
			}
			tid, err := e.typeID(adapted)
			if err != nil {
				return nil, err
			}
			path := make([]any, len(mc.recvPath))
			for i, v := range mc.recvPath {
				path[i] = v
			}
			recv = e.node("selector", sel, map[string]any{
				"x": recv, "field": embeddedFieldName(selection.Recv(), mc.recvPath),
				"selpath": path, "type": tid,
			})
		}
		return e.exportIfaceFallbackCall(c, sel, selection, recv, mc.ifaceFallback)
	}
	// P3-M9: variadic method calls pack trailing args into a slice literal
	// (the spread form passes the slice directly and needs no packing).
	// The packing is applied at the exportCallArgs site below via
	// exportCallArgsPacked.
	declRecv := mc.decl.sig.Recv().Type()
	xt, ok := e.info.Types[sel.X]
	if !ok || xt.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for method receiver")
	}
	// GAP-6: a promoted method's declared receiver is the embedded type,
	// not sel.X's type. Adapt the receiver expression by walking recvPath
	// with a "selector" wire node — the factory's SelectorNode walks
	// selpath with pointer auto-deref at every level, exactly like a
	// field selection — and check the declared receiver against the
	// ADAPTED type, not the outer one. A promoted pointer-receiver
	// method via a value-embedded field (or value receiver via a
	// pointer-embedded field) still fails fast below with the WS D scope
	// error: the adaptation never invents an implicit &/*.
	recvType := xt.Type
	if len(mc.recvPath) > 0 {
		adapted := embeddedFieldType(selection.Recv(), mc.recvPath)
		if adapted == nil {
			return nil, fmt.Errorf("gofrontend: cannot adapt receiver for promoted method %s (exporter bug)", sel.Sel.Name)
		}
		tid, err := e.typeID(adapted)
		if err != nil {
			return nil, err
		}
		path := make([]any, len(mc.recvPath))
		for i, v := range mc.recvPath {
			path[i] = v
		}
		recv = e.node("selector", sel, map[string]any{
			"x": recv, "field": embeddedFieldName(selection.Recv(), mc.recvPath),
			"selpath": path, "type": tid,
		})
		recvType = adapted
	}
	if substString(declRecv, mc.subst) != substString(recvType, e.subst) {
		// P3-M9: implicit address-of for method receivers (WS D).
		// If the method has a pointer receiver (*T) but the receiver
		// expression has type T and is addressable, take the address
		// implicitly per Go spec (x.M() → (&x).M()).
		if !e.implicitAddrOf(sel, &recv, &recvType, declRecv, mc.subst) {
			return nil, scopeErrorf("method call %s needs an implicit address-of or deref on the receiver (WS D)", sel.Sel.Name)
		}
	}
	var objID, sigID int
	if mc.needsInst {
		inst, ok := e.instByKey[mc.key]
		if !ok {
			return nil, fmt.Errorf("gofrontend: method instantiation %s not enumerated (internal error)", mc.key)
		}
		objID = inst.instID
		sigID = inst.sigID
	} else {
		objID = mc.decl.funcID
		// P1-1 (M6 remediation): wire the method's signature type id on the
		// methodref node (receiver as params[0], matching the funcdecl's
		// "sig") so the factory can adapt concrete arguments passed to
		// interface-typed parameters — the same call-site boxing direct
		// calls get. Without it the root fell back to inferring the
		// descriptor from the Java carrier type and a later type switch
		// silently took the wrong branch. The signature is concrete here
		// (!isTemplate), so the active substitution is the identity on it.
		var err error
		sigID, err = e.freshSigID(mc.decl.recv, mc.decl.sig.Params(), mc.decl.sig.Results(),
			mc.decl.sig.Variadic())
		if err != nil {
			return nil, err
		}
	}
	return e.node("methodref", sel, map[string]any{"obj": objID, "recv": recv, "type": sigID}), nil
}

// exportPrimMethodValue lowers a method value x.M on a synthetic sync /
// sync/atomic primitive (fix-linkname) to a funclit capturing the
// receiver and performing the syncop. The general methodValueNode path
// cannot serve these: the methods have no funcdecl (the overlay is
// types-built), and pointer-receiver methods on value receivers need
// gc's implicit & — which is a no-op at the value level anyway (the Go
// value IS the Java primitive object, ADR-0002 D1), so the receiver is
// used directly. The receiver is hoisted (evaluated once, Go semantics)
// via mrHoistPre, like exportSyncOnceFunc.
// Multi-result primitive methods (sync.Map.Load, …) are a named loud
// limitation: the funclit would need the pair desugar in return
// position (WS D).
// Returns handled=false when the selection is not a synthetic
// primitive method.
func (e *exporter) exportPrimMethodValue(x *ast.SelectorExpr, sel *types.Selection, recv *Node, t int) (*Node, bool, error) {
	nt := syntheticPrimitiveRecv(sel.Recv())
	if nt == nil {
		return nil, false, nil
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return nil, false, nil
	}
	if err := syntheticOpInScope(nt, fn.Name()); err != nil {
		return nil, true, err
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, true, fmt.Errorf("gofrontend: primitive method %s has non-signature type", fn.Name())
	}
	if sig.Results().Len() > 1 {
		return nil, true, scopeErrorf("method value %s is multi-result: not supported in frontend/v1 scope (WS D)", fn.Name())
	}
	// Hoist the receiver: evaluated once at method-value creation.
	recvType := sel.Recv()
	recvTid, err := e.typeID(recvType)
	if err != nil {
		return nil, true, err
	}
	e.chanSeq++
	recvName := fmt.Sprintf("__primrecv_%d", e.chanSeq)
	recvVar := types.NewVar(token.NoPos, nil, recvName, recvType)
	recvID, err := e.defVar(recvVar, false)
	if err != nil {
		return nil, true, err
	}
	e.mrHoistPre = append(e.mrHoistPre, e.node("vardecl", x, map[string]any{
		"names": []string{recvName}, "objs": []int{recvID},
		"define": true, "type": nil, "values": []*Node{recv},
	}))
	recvIdent := e.node("ident", x, map[string]any{
		"name": recvName, "obj": recvID, "type": recvTid,
	})
	// Funclit params/results from the method signature (minus receiver).
	params, err := e.exportSignatureVars(x, sig.Params())
	if err != nil {
		return nil, true, err
	}
	results, err := e.exportSignatureVars(x, sig.Results())
	if err != nil {
		return nil, true, err
	}
	var args = make([]*Node, 0, len(params))
	for _, p := range params {
		args = append(args, e.node("ident", x, map[string]any{
			"name": p.Fields["name"], "obj": p.Fields["obj"], "type": p.Fields["type"],
		}))
	}
	var resultTyp any
	if sig.Results().Len() == 1 {
		resultTyp, err = e.typeID(sig.Results().At(0).Type())
		if err != nil {
			return nil, true, err
		}
	}
	op := e.node("syncop", x, map[string]any{
		"op": fn.Name(), "synctyp": syntheticOpTypeName(nt),
		"recv": recvIdent, "args": args, "type": resultTyp,
	})
	var body *Node
	if sig.Results().Len() == 0 {
		body = e.node("block", x, map[string]any{
			"stmts": []*Node{e.node("exprstmt", x, map[string]any{"x": op})},
		})
	} else {
		body = e.node("block", x, map[string]any{
			"stmts": []*Node{e.node("return", x, map[string]any{"results": []*Node{op}})},
		})
	}
	return e.node("funclit", x, map[string]any{
		"params": params, "results": results, "body": body, "type": t,
	}), true, nil
}

// methodValueNode exports a method value x.M in value position (selection
// kind MethodVal) as a "methodvalue" wire node (P3-M9, WS H follow-up).
// It mirrors methodRefNode's resolution exactly — resolveMethodCall,
// promoted-method receiver adaptation (GAP-6), implicit address-of (WS D),
// generic instantiation — but instead of a "methodref" call target it
// produces an expression node evaluating to a GoFunc with the receiver
// bound: the factory evaluates recv once at creation time and every
// invocation of the resulting function value prepends the bound receiver
// to the call arguments (see GoCalls.invoke and MethodValueNode).
// The "type" field is the method value's static func type (no receiver),
// from the selector expression's type.
func (e *exporter) methodValueNode(sel *ast.SelectorExpr, selection *types.Selection, recv *Node, t int) (*Node, error) {
	// fix-linkname: method values on synthetic sync/sync/atomic
	// primitives lower to a capturing funclit (no funcdecl exists for
	// the overlay's types-built methods).
	if n, handled, err := e.exportPrimMethodValue(sel, selection, recv, t); handled {
		return n, err
	}
	var mtargs []types.Type
	if ii, ok := e.info.Instances[sel.Sel]; ok {
		mtargs = typeList(ii.TypeArgs)
	}
	mc, err := e.resolveMethodCall(sel, selection, mtargs, e.subst)
	if err != nil {
		// P3-M9: resolveMethodCall's implicit-& fail-fast says "method
		// call"; in the method-value context it must say "method value"
		// (the test pins the attribution).
		if strings.Contains(err.Error(), "method call ") && strings.Contains(err.Error(), "needs an implicit") {
			return nil, scopeErrorf("method value %s needs an implicit address-of or deref on the receiver (WS D)", sel.Sel.Name)
		}
		return nil, err
	}
	// The ifacemethod fallback is call-only: a method value on a type
	// parameter would need itab dispatch deferred to call time, which is
	// out of scope. Fail fast with a named error.
	if mc.ifaceFallback != nil {
		return nil, scopeErrorf("method value %s via type parameter is not supported (WS H: iface fallback is call-only)", sel.Sel.Name)
	}
	declRecv := mc.decl.sig.Recv().Type()
	xt, ok := e.info.Types[sel.X]
	if !ok || xt.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for method receiver")
	}
	// GAP-6: same promoted-method receiver adaptation as methodRefNode.
	recvType := xt.Type
	if len(mc.recvPath) > 0 {
		adapted := embeddedFieldType(selection.Recv(), mc.recvPath)
		if adapted == nil {
			return nil, fmt.Errorf("gofrontend: cannot adapt receiver for promoted method %s (exporter bug)", sel.Sel.Name)
		}
		tid, err := e.typeID(adapted)
		if err != nil {
			return nil, err
		}
		path := make([]any, len(mc.recvPath))
		for i, v := range mc.recvPath {
			path[i] = v
		}
		recv = e.node("selector", sel, map[string]any{
			"x": recv, "field": embeddedFieldName(selection.Recv(), mc.recvPath),
			"selpath": path, "type": tid,
		})
		recvType = adapted
	}
	if substString(declRecv, mc.subst) != substString(recvType, e.subst) {
		// P3-M9: implicit address-of for method receivers (WS D), same as
		// method calls: x.M → (&x).M when M has a pointer receiver and x
		// is addressable.
		if !e.implicitAddrOf(sel, &recv, &recvType, declRecv, mc.subst) {
			return nil, scopeErrorf("method value %s needs an implicit address-of or deref on the receiver (WS D)", sel.Sel.Name)
		}
	}
	var objID int
	if mc.needsInst {
		inst, ok := e.instByKey[mc.key]
		if !ok {
			return nil, fmt.Errorf("gofrontend: method instantiation %s not enumerated (internal error)", mc.key)
		}
		objID = inst.instID
	} else {
		objID = mc.decl.funcID
	}
	return e.node("methodvalue", sel, map[string]any{"obj": objID, "recv": recv, "type": t}), nil
}

// exportMethodCall exports a method call x.M(args) (M6, WS H). The receiver
// x is emitted as a "methodref" fun target carrying the resolved
// function/instantiation object id; the factory evaluates the receiver
// first and passes it as the first argument (params[0] of the method's
// funcdecl). Receiver-based dispatch only: the receiver type must match the
// declared receiver type after substitution — implicit &/* adjustments are
// a scope error (WS D).
func (e *exporter) exportMethodCall(c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection) (*Node, error) {
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	fun, err := e.methodRefNode(c, sel, selection, recv)
	if err != nil {
		return nil, err
	}
	// P3-M9 (WS H, graalgo-wsh-fallback): the ifacemethod fallback node
	// is a complete call — return it directly instead of wrapping in a
	// "call" node.
	if fun.Kind == "ifacemethod" {
		return fun, nil
	}
	// P3-M9: pack variadic method args. The signature's Params() excludes
	// the receiver (it's in Recv()), matching c.Args which excludes the
	// receiver expression (sel.X is exported separately above).
	var methodSig *types.Signature
	if fn, ok := selection.Obj().(*types.Func); ok {
		if sig, ok := fn.Type().(*types.Signature); ok {
			methodSig = sig
		}
	}
	args, err := e.exportCallArgsPacked(c, methodSig)
	if err != nil {
		return nil, err
	}
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	return e.node("call", c, map[string]any{
		"fun": fun, "args": args, "type": typ,
	}), nil
}

// ---------------------------------------------------------------------------
// Interface dispatch (P3-M3, WS D, ADR-0003).
// ---------------------------------------------------------------------------

// recvIface reports the interface underlying t, or nil. This is the P3-M3
// discriminator between interface method calls (dynamic dispatch via the
// itab) and concrete method calls (static dispatch via methodref): the
// receiver's static type must have an interface underlying — a named
// concrete type never routes here. (types.AsInterface does not exist in
// this toolchain's go/types, so this is the underlying check the ADR
// allows.)
func recvIface(t types.Type) *types.Interface {
	if t == nil {
		return nil
	}
	// P3-M3: a type parameter is not an interface for dispatch purposes,
	// even though its constraint's underlying may be one. Bound method
	// calls (v.M() with v: T) devirtualize per monomorphized
	// instantiation (M6 design) via the methodref path — routing them
	// through ifacemethod would emit a wire node the Phase-1/2 consumers
	// cannot build (regression vs the pre-P3-M3 exporter).
	if _, ok := t.(*types.TypeParam); ok {
		return nil
	}
	if i, ok := t.Underlying().(*types.Interface); ok {
		return i
	}
	return nil
}

// embeddedFieldType walks a struct field-index path (P3-M3, WS D),
// dereferencing pointers and named types between steps; it returns the
// field type at the end of the path, or nil when the path cannot be
// resolved (defensive: the caller then skips its check rather than
// failing a program it cannot analyze).
func embeddedFieldType(t types.Type, path []int) types.Type {
	for _, idx := range path {
		for {
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
				continue
			}
			if n, ok := t.(*types.Named); ok {
				t = n.Underlying()
				continue
			}
			// real-reflect (Phase 3): aliases are transparent for field
			// walking (e.g. reflect.funcType = abi.FuncType — the
			// receiver of promoted Common/Kind calls in makefunc.go).
			if a, ok := t.(*types.Alias); ok {
				t = a.Rhs()
				continue
			}
			break
		}
		st, ok := t.(*types.Struct)
		if !ok || idx < 0 || idx >= st.NumFields() {
			return nil
		}
		t = st.Field(idx).Type()
	}
	return t
}

// embeddedFieldName returns the name of the struct field at the end of a
// field-index path (GAP-6), or "" when the path cannot be resolved. It
// mirrors embeddedFieldType's walk so the two stay consistent.
func embeddedFieldName(t types.Type, path []int) string {
	name := ""
	for _, idx := range path {
		for {
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
				continue
			}
			if n, ok := t.(*types.Named); ok {
				t = n.Underlying()
				continue
			}
			// real-reflect (Phase 3): aliases are transparent for field
			// walking — mirrors embeddedFieldType.
			if a, ok := t.(*types.Alias); ok {
				t = a.Rhs()
				continue
			}
			break
		}
		st, ok := t.(*types.Struct)
		if !ok || idx < 0 || idx >= st.NumFields() {
			return ""
		}
		name = st.Field(idx).Name()
		t = st.Field(idx).Type()
	}
	return name
}

// promotedSyncPrimitive reports whether the method denoted by selection is
// promoted through embedding from a synthetic sync/sync/atomic primitive
// (real-reflect: reflect's funcLookupCache.Lock()). It returns the
// primitive's Named type and the embedding path (Selection.Index minus
// the method's own index), or (nil, nil) when the method is not promoted
// from a primitive.
func promotedSyncPrimitive(selection *types.Selection) (*types.Named, []int) {
	if selection == nil || selection.Obj() == nil {
		return nil, nil
	}
	mname := selection.Obj().Name()
	// Search the value method set first, then the pointer method set
	// (pointer-receiver methods promoted via value-embedded fields, with
	// gc's implicit & on addressable receivers).
	for _, recv := range []types.Type{selection.Recv(), types.NewPointer(selection.Recv())} {
		ms := types.NewMethodSet(recv)
		for i := 0; i < ms.Len(); i++ {
			s := ms.At(i)
			if s.Obj().Name() != mname {
				continue
			}
			index := s.Index()
			if len(index) < 2 {
				// Direct method, not promoted.
				return nil, nil
			}
			path := index[:len(index)-1]
			fieldType := embeddedFieldType(selection.Recv(), path)
			if dn := derefNamed(fieldType); dn != nil {
				if nt := syntheticPrimitiveRecv(dn); nt != nil {
					return nt, path
				}
			}
			return nil, nil
		}
	}
	return nil, nil
}

// recomputeMethodTables recomputes the wire "methods" list of every
// named/pointer type-table entry described so far (P3-M3, WS D,
// ADR-0003). Called once from ExportFile after funcByObj is populated;
// see the call site.
func (e *exporter) recomputeMethodTables() error {
	for id, t := range e.typeList {
		switch t.(type) {
		case *types.Named, *types.Pointer:
			methods, tmplSkipped, err := e.methodTableEntries(t)
			if err != nil {
				return err
			}
			e.types[id].Detail["methods"] = methods
			// P3-M3 remediation (R1 P1-2): keep the
			// "templateMethodsSkipped" marker in sync — the recompute
			// pass is authoritative (funcByObj is populated here).
			if tmplSkipped {
				e.types[id].Detail["templateMethodsSkipped"] = true
			} else {
				delete(e.types[id].Detail, "templateMethodsSkipped")
			}
		}
	}
	return nil
}

// implKeyOf returns the method-identity key for the P3-M3 implements check
// (ADR-0003): the method name (unexported names qualified with their package
// path, as gc compares them) plus a structural signature key WITHOUT the
// receiver (interface method signatures never declare one; the caller passes
// the receiver-stripped signature). Two methods share an implkey iff gc
// considers their (name, signature) pairs identical: aliases are expanded —
// e.g. func (T) Write(s B) with `type B = []byte` implements
// interface{ Write([]byte) int } — and parameter names are ignored (gc
// compares types, not names). The "\x00" separator cannot collide: neither
// method names nor signature keys contain it.
// ifaceStructKey renders an interface type structurally for deduplication:
// "iface{" + sorted method implkeys + "}". Embedded types are expanded via
// go/types' MethodSet semantics (t.Method includes promoted methods).
func ifaceStructKey(t *types.Interface) string {
	keys := make([]string, 0, t.NumMethods())
	for i := 0; i < t.NumMethods(); i++ {
		m := t.Method(i)
		msig, ok := m.Type().(*types.Signature)
		if !ok {
			continue
		}
		keys = append(keys, implKeyOf(m, msig))
	}
	sort.Strings(keys)
	return "iface{" + strings.Join(keys, ";") + "}"
}

func implKeyOf(mobj *types.Func, sig *types.Signature) string {
	name := mobj.Name()
	if !mobj.Exported() {
		if pkg := mobj.Pkg(); pkg != nil {
			name = pkg.Path() + "." + name
		}
	}
	return name + "\x00" + sigKeyOf(sig)
}

// sigKeyOf renders a method signature structurally (receiver excluded):
// "func(" params ")(" results ")", with "..." for the variadic tail.
// Parameter/result names are ignored.
func sigKeyOf(sig *types.Signature) string {
	var sb strings.Builder
	sb.WriteString("func(")
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		pt := params.At(i).Type()
		if sig.Variadic() && i == params.Len()-1 {
			// go/types represents the variadic tail as []T; render as
			// ...T so f(...int) and f([]int) key differently (gc
			// distinguishes them).
			if s, ok := pt.(*types.Slice); ok {
				sb.WriteString("..." + typeKeyOf(s.Elem()))
			} else {
				sb.WriteString(typeKeyOf(pt)) // unreachable per go/types invariant
			}
		} else {
			sb.WriteString(typeKeyOf(pt))
		}
	}
	sb.WriteString(")(")
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(typeKeyOf(results.At(i).Type()))
	}
	sb.WriteString(")")
	return sb.String()
}

// typeKeyOf renders a type structurally for signature comparison. Aliases
// are expanded (types.Unalias); named types render as qualified name +
// type arguments (never expanded — expansion would not terminate on
// recursive named types, and qualified names are unique within a program);
// composites recurse. Two types share a key iff they are identical in gc's
// sense; no position or naming trivia leaks in.
func typeKeyOf(t types.Type) string {
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		// Key by kind, not name: "byte" and "uint8" are identical types
		// (same BasicKind) but have different Basic names — keying by
		// name would wrongly reject `Write([]uint8)` as an implementation
		// of `interface{ Write([]byte) }` (gc: implements).
		return "basic:" + strconv.Itoa(int(t.Kind()))
	case *types.Named:
		var sb strings.Builder
		sb.WriteString("named:" + qualifiedName(t.Obj()))
		if targs := t.TypeArgs(); targs != nil && targs.Len() > 0 {
			sb.WriteString("[")
			for i := 0; i < targs.Len(); i++ {
				if i > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(typeKeyOf(targs.At(i)))
			}
			sb.WriteString("]")
		}
		return sb.String()
	case *types.Pointer:
		return "ptr(" + typeKeyOf(t.Elem()) + ")"
	case *types.Slice:
		return "slice(" + typeKeyOf(t.Elem()) + ")"
	case *types.Array:
		return "array(" + strconv.FormatInt(t.Len(), 10) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Map:
		return "map(" + typeKeyOf(t.Key()) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Chan:
		return "chan(" + strconv.FormatInt(int64(t.Dir()), 10) + "," + typeKeyOf(t.Elem()) + ")"
	case *types.Signature:
		// Func-typed parameter/result: signatures nest without receivers.
		return sigKeyOf(t)
	case *types.Struct:
		var sb strings.Builder
		sb.WriteString("struct(")
		for i := 0; i < t.NumFields(); i++ {
			if i > 0 {
				sb.WriteString(";")
			}
			f := t.Field(i)
			sb.WriteString(f.Name() + ":" + typeKeyOf(f.Type()))
			if tag := t.Tag(i); tag != "" {
				sb.WriteString("`" + tag + "`")
			}
		}
		sb.WriteString(")")
		return sb.String()
	case *types.Interface:
		if t.Empty() {
			return "interface{}"
		}
		ms := types.NewMethodSet(t)
		keys := make([]string, 0, ms.Len())
		for i := 0; i < ms.Len(); i++ {
			sel := ms.At(i)
			mo, ok := sel.Obj().(*types.Func)
			if !ok {
				continue
			}
			msig, ok := mo.Type().(*types.Signature)
			if !ok {
				continue
			}
			keys = append(keys, implKeyOf(mo, msig))
		}
		sort.Strings(keys)
		return "interface(" + strings.Join(keys, ";") + ")"
	case *types.Union:
		var sb strings.Builder
		sb.WriteString("union(")
		for i := 0; i < t.Len(); i++ {
			if i > 0 {
				sb.WriteString(";")
			}
			term := t.Term(i)
			if term.Tilde() {
				sb.WriteString("~")
			}
			sb.WriteString(typeKeyOf(term.Type()))
		}
		sb.WriteString(")")
		return sb.String()
	case *types.TypeParam:
		// Unreachable in practice: mentionsTypeParam fails template
		// methods before any implkey is computed. Total function anyway.
		return "typeparam:" + t.Obj().Name()
	}
	return "unknown:" + fmt.Sprintf("%T", t)
}

// methodTableEntries computes the wire "methods" list for a named type or
// a pointer type (P3-M3, WS D, ADR-0003): one
// {"name","obj","recv","index","implkey","addrOf"} entry per method in
// types.NewMethodSet(t), resolved to declarations via funcByObj (this
// covers promoted/embedded methods, which methodIndex lacks). "obj" is the
// funcdecl object id; "recv" is the declared receiver kind ("value" or
// "pointer"); "index" is the receiver adaptation path: Selection.Index()
// minus its last entry (the method's own index in the declaring type) —
// empty for direct methods, e.g. [0] for a method promoted via embedded
// field 0, null for unsupported promotions. "addrOf" is true when the
// dispatcher must take the address of the promoted field value: a
// pointer-receiver method promoted via a value-embedded struct field
// (P3-M9). The Java dispatcher walks "index" to adapt the receiver
// (P3-M3). Interface-declared methods (no funcdecl exists to point at)
// are skipped. Template methods of concrete generic instantiations are
// monomorphized (P3-M9 instantiation method tables): "obj" is the
// instantiation's object id. Sorted by method name for determinism.
//
// The second return value reports whether any template method could not
// be monomorphized for this type (P3-M3 remediation, R1 P1-2): the caller
// records it as "templateMethodsSkipped" on the entry detail so the
// frontend can fail fast with the named P3-M3 scope error when such an
// instantiation is boxed into a non-empty interface, instead of dying at
// first dispatch with a misleading "no method table installed" error.
// methodEntry is one wire method-table entry under construction
// (factored from methodTableEntries so the monomorphization paths share
// the entry-building logic).
type methodEntry struct {
	name        string
	obj         int
	recv        string
	index       []int
	implkey     string
	unsupported bool
	addrOf      bool
	// embeddedIface is the type-table id of the embedded interface's
	// interface-kind entry when the method is promoted via an embedded
	// interface field (P3-M9 redispatch, see embeddedInterfacePromotion);
	// -1 for ordinary entries. The dispatcher carries no GoFunc for
	// such entries (obj is -1): it reads the embedding path to the
	// interface field value and dispatches the method on it.
	embeddedIface int
}

// methodTableEntry builds one wire method-table entry for mobj with an
// already-resolved obj (funcdecl id or instantiation id). fi is the
// declaration the receiver kind is derived from (template or helper).
func (e *exporter) methodTableEntry(t types.Type, sel *types.Selection, mobj *types.Func, fi *funcDeclInfo, obj int, helperMapped bool) (methodEntry, error) {
	recv := "value"
	if helperMapped {
		// The helper is a plain package-level function; the
		// dispatcher's receiver adaptation must mirror the
		// method's DECLARED receiver so the helper receives the
		// same shape the concrete desugar passes it.
		if msig, ok := mobj.Type().(*types.Signature); ok && msig.Recv() != nil {
			if _, ok := msig.Recv().Type().(*types.Pointer); ok {
				recv = "pointer"
			}
		}
	} else if fi.recv != nil {
		if _, ok := fi.recv.Type().(*types.Pointer); ok {
			recv = "pointer"
		}
	}
	// Selection.Index() for a method is [embedded field path...,
	// method index in the declaring type] (go/types selection.go):
	// the receiver adaptation path is everything but the last entry —
	// empty for direct methods, e.g. [0] for a method promoted via
	// embedded field 0. Normalize nil to [] so the wire field is
	// always an array, never null.
	index := sel.Index()
	var path []int
	if len(index) > 1 {
		path = index[:len(index)-1]
	} else {
		path = []int{}
	}
	// P3-M3: promoted method with a pointer receiver via a
	// value-embedded field: the dispatcher must take the address of
	// the promoted field value (gc's implicit &). P3-M9: this is
	// sound for struct-typed fields (read by reference — the live
	// field object), so mark supported with "addrOf": true. For
	// non-struct fields (primitives etc.) address-taking would need
	// writeback (out of scope): mark unsupported ("index": null)
	// rather than failing the export — the failure fires only if the
	// method is actually dispatched through an interface (fail fast
	// at dispatch, named P3-M3 scope error — R2 P2-7 narrowed the
	// old whole-itab fail-fast to per-method) — never for programs
	// that merely declare the type.
	unsupported := false
	addrOf := false
	if len(path) > 0 && recv == "pointer" {
		if ft := embeddedFieldType(t, path); ft != nil {
			for {
				if nt, ok := ft.(*types.Named); ok {
					ft = nt.Underlying()
					continue
				}
				break
			}
			if _, ok := ft.(*types.Pointer); !ok {
				if _, ok := ft.(*types.Struct); ok {
					addrOf = true
				} else {
					unsupported = true
				}
			}
		}
	}
	// The *types.Signature on a *types.Func method object carries the
	// declared receiver; sigKeyOf renders only params/results/variadic,
	// so no stripping is needed for the implements-check identity key.
	msig, ok := mobj.Type().(*types.Signature)
	if !ok {
		return methodEntry{}, fmt.Errorf("gofrontend: method %s has non-signature type %T (exporter bug)",
			mobj.Name(), mobj.Type())
	}
	return methodEntry{name: mobj.Name(), obj: obj, recv: recv, index: path,
		implkey: implKeyOf(mobj, msig), unsupported: unsupported, addrOf: addrOf,
		embeddedIface: -1}, nil
}

// embeddedInterfacePromotion reports whether the method selection sel on
// type t is a method promoted via an embedded interface field (e.g.
// (*crypto/tls.listener).Addr, promoted from the embedded net.Listener).
// It returns the embedded field's interface type (unwrapped to the
// *types.Interface, mirroring the ifacemethod "iface" id convention) and
// the field path from t to that interface value. It returns (nil, nil)
// when the method is not promoted through an embedded interface —
// including embedding *I (pointer-to-interface), which keeps the old
// skip behavior (fail fast at dispatch, never silent).
func embeddedInterfacePromotion(t types.Type, sel *types.Selection) (types.Type, []int) {
	idx := sel.Index()
	if len(idx) < 2 {
		return nil, nil // not promoted
	}
	path := idx[:len(idx)-1]
	cur := t
	ifacePath := make([]int, 0, len(path))
	for _, fi := range path {
		if p, ok := cur.(*types.Pointer); ok {
			cur = p.Elem()
		}
		if n, ok := cur.(*types.Named); ok {
			cur = n.Underlying()
		}
		st, ok := cur.(*types.Struct)
		if !ok {
			return nil, nil
		}
		if fi < 0 || fi >= st.NumFields() {
			return nil, nil
		}
		f := st.Field(fi)
		ifacePath = append(ifacePath, fi)
		ft := types.Unalias(f.Type())
		if nt, ok := ft.(*types.Named); ok {
			if ui, ok := nt.Underlying().(*types.Interface); ok {
				return ui, ifacePath
			}
			// Named non-interface field: keep walking — a struct
			// can embed an interface deeper down the path.
			cur = f.Type()
			continue
		}
		if _, ok := ft.(*types.Interface); ok {
			return ft, ifacePath
		}
		cur = f.Type()
	}
	return nil, nil
}

// methodTableOnDemand monomorphizes a template method for the method
// table of a concrete generic instantiation described after
// instantiation enumeration (e.mtOnDemand must be true). It returns the
// instantiation — the existing one from instByKey, or a newly created
// one — or (nil, nil) when the method cannot be monomorphized (the
// caller keeps the skip-and-mark behavior).
//
// A newly created instantiation is prepared immediately: its instID is
// referenced by the method-table entry under construction and may be
// looked up by body export after this returns. Body emission is deferred
// to drainMethodTablePending.
func (e *exporter) methodTableOnDemand(t types.Type, sel *types.Selection, mobj *types.Func) (*instantiation, error) {
	inst, err := e.buildMethodTableInst(t, sel, mobj)
	if err != nil || inst == nil {
		return inst, err
	}
	if existing, ok := e.instByKey[inst.key]; ok {
		return existing, nil
	}
	if err := e.prepareInstantiation(inst); err != nil {
		return nil, err
	}
	e.instByKey[inst.key] = inst
	e.mtPending = append(e.mtPending, inst)
	return inst, nil
}

func (e *exporter) methodTableEntries(t types.Type) ([]any, bool, error) {
	ms := types.NewMethodSet(t)
	entries := make([]methodEntry, 0, ms.Len())
	tmplSkipped := false
	for i := 0; i < ms.Len(); i++ {
		sel := ms.At(i)
		mobj, ok := sel.Obj().(*types.Func)
		if !ok {
			continue
		}
		fi, ok := e.funcByObj[mobj]
		helperMapped := false
		if !ok {
			// P3-M8 (WS D): multi-result synthetic methods are skipped
			// from funcByObj (v1 cannot emit multi-result funcdecls),
			// but their pair-returning __-helpers are emitted. Map the
			// method to the helper so interface dispatch can target it:
			// the dispatcher invokes the helper with the receiver data
			// as params[0] (exactly the helper's prependRecv calling
			// convention) and the node yields the pair, so r.Read(buf)
			// through an interface produces the same pair shape as the
			// concrete Appendix M desugar. Methods with no helper keep
			// the old skip (dispatch fails fast with "missing method"
			// — loud, never silent).
			var hfi *funcDeclInfo
			hfi, ok = e.multiResultHelperFunc(mobj)
			if !ok {
				// P3-M9 (instantiation method tables): the method-set
				// object of an instantiated type is a copy that never
				// hits funcByObj. Resolve the template and monomorphize.
				if e.mtOnDemand && isGenericInstantiation(t) {
					inst, err := e.methodTableOnDemand(t, sel, mobj)
					if err != nil {
						return nil, false, err
					}
					if inst == nil {
						tmplSkipped = true
						continue
					}
					en, err := e.methodTableEntry(t, sel, mobj, inst.decl, inst.instID, false)
					if err != nil {
						return nil, false, err
					}
					entries = append(entries, en)
					continue
				}
				// P3-M9 (x509/TLS lane): method promoted via an embedded
				// interface field (e.g. (*crypto/tls.listener).Addr from
				// the embedded net.Listener). The method object is the
				// interface's declaration — no funcdecl exists — so the
				// old code skipped it and interface dispatch failed
				// fast with 'missing method "Addr" for dynamic type
				// "*crypto/tls.listener"'. Emit a redispatch entry
				// (obj -1): the dispatcher reads the embedding path to
				// the interface field value and dispatches the method on
				// it, exactly like the direct l.Listener.Addr() call.
				// Methods not promoted through an embedded interface
				// keep the old skip (fail fast at dispatch, never
				// silent).
				if embIface, embPath := embeddedInterfacePromotion(t, sel); embIface != nil {
					embID, err := e.typeID(embIface)
					if err != nil {
						return nil, false, err
					}
					msig, ok := mobj.Type().(*types.Signature)
					if !ok {
						return nil, false, fmt.Errorf("gofrontend: method %s has non-signature type %T (exporter bug)",
							mobj.Name(), mobj.Type())
					}
					entries = append(entries, methodEntry{
						name: mobj.Name(), obj: -1, recv: "value",
						index: embPath, implkey: implKeyOf(mobj, msig),
						embeddedIface: embID,
					})
					continue
				}
				continue
			}
			fi = hfi
			helperMapped = true
		}
		if fi.isTemplate {
			// P3-M9 (instantiation method tables): monomorphize template
			// methods of concrete generic instantiations instead of
			// skipping them — exactly like resolveMethodCall does for
			// direct calls. The entry references the instantiation's
			// object id so interface dispatch (itab) targets emitted
			// code. Anything not monomorphizable keeps the old
			// skip-and-mark (fail fast at boxing, never silent).
			if e.mtOnDemand && isGenericInstantiation(t) {
				inst, err := e.methodTableOnDemand(t, sel, mobj)
				if err != nil {
					return nil, false, err
				}
				if inst == nil {
					tmplSkipped = true
					continue
				}
				en, err := e.methodTableEntry(t, sel, mobj, fi, inst.instID, helperMapped)
				if err != nil {
					return nil, false, err
				}
				entries = append(entries, en)
				continue
			}
			tmplSkipped = true
			continue
		}
		en, err := e.methodTableEntry(t, sel, mobj, fi, fi.funcID, helperMapped)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, en)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	out := make([]any, 0, len(entries))
	for _, en := range entries {
		var idx any
		if en.unsupported {
			idx = nil
		} else {
			arr := make([]any, len(en.index))
			for i, v := range en.index {
				arr[i] = v
			}
			idx = arr
		}
		m := map[string]any{"name": en.name, "obj": en.obj, "recv": en.recv,
			"index": idx, "implkey": en.implkey, "addrOf": en.addrOf}
		// P3-M9: embedded-interface redispatch entries carry the
		// embedded interface's interface-kind type-table id (obj is
		// -1: no GoFunc to invoke). Only present on redispatch
		// entries — ordinary entries keep the old wire shape.
		if en.embeddedIface >= 0 {
			m["embeddedIface"] = en.embeddedIface
		}
		out = append(out, m)
	}
	return out, tmplSkipped, nil
}

// isGenericInstantiation reports whether t is a generic instantiation
// (a *types.Named with type arguments, or a pointer to one).
func isGenericInstantiation(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		return n.TypeArgs() != nil && n.TypeArgs().Len() > 0
	}
	return false
}

// resolveIfaceMethod looks the method up in the method set of the
// receiver's static interface type (P3-M3, WS D, ADR-0003) and returns its
// signature (no receiver — interface method signatures never declare one).
// Generic (template) methods via interfaces are out of scope: a named P3-M3
// scope error (greppable re-test trigger), never silent.
func (e *exporter) resolveIfaceMethod(iface types.Type, mname string) (*types.Signature, error) {
	msel := types.NewMethodSet(iface).Lookup(e.pkg, mname)
	if msel == nil {
		return nil, fmt.Errorf("gofrontend: method %s not in method set of %s",
			mname, types.TypeString(iface, nil))
	}
	mobj, ok := msel.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("gofrontend: %s is not a method of %s",
			mname, types.TypeString(iface, nil))
	}
	sig, ok := mobj.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: method %s of %s has non-signature type %T",
			mname, types.TypeString(iface, nil), mobj.Type())
	}
	if sig.TypeParams().Len() > 0 {
		return nil, scopeErrorf("method %s is not supported: generic method via interface (P3-M3 scope)", mname)
	}
	// A template method (declared on a generic type) reaching interface
	// dispatch without a binding substitution would leak unbound type
	// parameters into the wire signature — fail fast with the named
	// scope error instead of emitting a corrupt signature. Inside a
	// monomorphized body the active substitution binds them (e.g. I[S]
	// under S=int), so only genuinely unbound parameters trip this.
	if csig, ok := e.substType(sig).(*types.Signature); ok {
		sig = csig
	}
	if mentionsTypeParam(sig) {
		return nil, scopeErrorf("method %s is not supported: generic method via interface (P3-M3 scope)", mname)
	}
	return sig, nil
}

// mentionsTypeParam reports whether t contains a *types.TypeParam anywhere.
// Used by the P3-M3 interface-dispatch path to fail fast on template
// methods that would otherwise leak unbound type parameters into the wire
// signature.
func mentionsTypeParam(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeParam:
		return true
	case *types.Alias:
		// R2 P2-4: an alias could smuggle a type parameter past the
		// fail-fast (e.g. a substituted alias in a method signature).
		return mentionsTypeParam(types.Unalias(t))
	case *types.Union:
		// R2 P2-4: unions cannot appear in method signatures today,
		// but harden anyway — total function over go/types.
		for i := 0; i < t.Len(); i++ {
			if mentionsTypeParam(t.Term(i).Type()) {
				return true
			}
		}
		return false
	case *types.Named:
		if targs := t.TypeArgs(); targs != nil {
			for i := 0; i < targs.Len(); i++ {
				if mentionsTypeParam(targs.At(i)) {
					return true
				}
			}
		}
		return false
	case *types.Pointer:
		return mentionsTypeParam(t.Elem())
	case *types.Slice:
		return mentionsTypeParam(t.Elem())
	case *types.Array:
		return mentionsTypeParam(t.Elem())
	case *types.Map:
		return mentionsTypeParam(t.Key()) || mentionsTypeParam(t.Elem())
	case *types.Chan:
		return mentionsTypeParam(t.Elem())
	case *types.Signature:
		return mentionsTypeParam(t.Params()) || mentionsTypeParam(t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if mentionsTypeParam(t.At(i).Type()) {
				return true
			}
		}
		return false
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if mentionsTypeParam(t.Field(i).Type()) {
				return true
			}
		}
		return false
	}
	return false
}

// exportIfaceMethodCall exports an interface method call w.M(args)
// (P3-M3, WS D, ADR-0003) as an additive "ifacemethod" wire node:
//
//	{"kind": "ifacemethod", "fields": {
//	  "recv": <node>, "method": "Write", "iface": <type id>,
//	  "args": [<node>], "sig": <type id>, "type": <type id or null>}}
//
// The receiver evaluates to a GoInterface; the factory dispatches through
// the itab built from the interface's method list. The "iface" field is the
// *interface* type-table id (the method list source): when the receiver's
// static type is a named interface type (e.g. main.Writer), the exporter
// emits the id of its underlying interface entry — the Java consumer reads
// "methods" from the interface entry and does not follow "underlying" for
// named entries (ADR-0003 §2.1 wire). The "sig" field is the method
// signature type id (no receiver) for call-site argument adaptation — the
// methodref precedent. The "type" field is the result type id, null when
// the call has no results. Variadic misuse fails fast via
// checkVariadicCall, as for concrete method calls.
func (e *exporter) exportIfaceMethodCall(c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection) (*Node, error) {
	mname := sel.Sel.Name
	sig, err := e.resolveIfaceMethod(selection.Recv(), mname)
	if err != nil {
		return nil, err
	}
	// P3-M8 (WS D): multi-result interface methods with a dispatched pair
	// (io.Reader.Read, io.Writer.Write — see m8IfaceMultiResultMethods)
	// lower to the ifacemethod dispatch: the itab maps the method to the
	// concrete type's pair-returning __-helper (wired via
	// methodTableEntries), so the node yields the pair and field
	// extraction proceeds exactly like the concrete Appendix M desugar.
	// The node's "type" is the pair type id — callType scope-errors on
	// multi-valued calls, so it is bypassed here. Any other multi-result
	// interface method keeps the fail-fast below (the P3-M5 F4
	// precedent).
	var pairPlan *mrPlan
	if sig.Results().Len() > 1 {
		pairPlan = e.classifyIfaceMultiResultCall(selection)
		// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): if the
		// strict classification fails (e.g., generic Point[P] interface
		// with type-parameter results), try the lenient fallback
		// classifier.
		if pairPlan == nil {
			pairPlan = e.classifyIfaceFallbackMultiResultCall(selection)
		}
		// WS G (P3-M9, BUG-MODPROBE-005): disk-package interface
		// methods with multi-result (e.g.
		// database/sql/driver.ValueConverter.ConvertValue), registered
		// by transformDiskMultiResult.
		if pairPlan == nil {
			pairPlan = e.classifyDiskIfaceMultiResultCall(selection)
		}
		if pairPlan == nil {
			if f, ok := selection.Obj().(*types.Func); ok && e.isMultiResultSyntheticFailFast(f) {
				return nil, multiResultSyntheticError(f)
			}
			return nil, scopeErrorf("interface method %s has %d results; at most one result is supported in frontend/v1 scope (WS D)",
				mname, sig.Results().Len())
		}
	}
	// P3-M9: variadic interface method calls pack trailing args into a
	// slice literal (the spread form passes the slice directly).
	// The packing is applied at the exportCallArgs site below via
	// exportCallArgsPacked.
	// ADR-0003 wire: "iface" is the interface type-table id, not the id
	// of a named type whose underlying is the interface. The dispatch
	// site's caller has already established an interface underlying;
	// anything else reaching here is an exporter bug — fail fast, never
	// emit a corrupt id. Aliases (type W = Writer) unwrap first: the
	// receiver's static type may be a *types.Alias whose underlying is
	// the interface (R1 P1-1).
	ifaceType := types.Unalias(selection.Recv())
	// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): if the receiver
	// is a type parameter, use its constraint interface for the itab.
	// This happens for generic Point[P] methods dispatched via the
	// interface path.
	if tp, ok := ifaceType.(*types.TypeParam); ok {
		ifaceType = tp.Constraint()
		if ifaceType == nil {
			return nil, fmt.Errorf("gofrontend: type parameter %s has no constraint (exporter bug)",
				types.TypeString(tp, nil))
		}
		ifaceType = types.Unalias(ifaceType)
	}
	if nt, ok := ifaceType.(*types.Named); ok {
		ui, ok := nt.Underlying().(*types.Interface)
		if !ok {
			return nil, fmt.Errorf("gofrontend: receiver static type %s is named with non-interface underlying %T (exporter bug)",
				types.TypeString(ifaceType, nil), nt.Underlying())
		}
		ifaceType = ui
	} else if _, ok := ifaceType.(*types.Interface); !ok {
		return nil, fmt.Errorf("gofrontend: receiver static type %s is not an interface (exporter bug)",
			types.TypeString(ifaceType, nil))
	}
	ifaceID, err := e.typeID(ifaceType)
	if err != nil {
		return nil, err
	}
	sigID, err := e.typeID(sig)
	if err != nil {
		return nil, err
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	// P3-M9: pack variadic interface method args.
	args, err := e.exportCallArgsPacked(c, sig)
	if err != nil {
		return nil, err
	}
	var typ any
	if pairPlan != nil {
		// P3-M8 (WS D): the dispatch yields the interface pair, not the
		// multi-valued call result (callType scope-errors on tuples).
		tid, err := e.typeID(pairPlan.tmpType)
		if err != nil {
			return nil, err
		}
		typ = tid
	} else {
		typ, err = e.callType(c)
		if err != nil {
			return nil, err
		}
	}
	return e.node("ifacemethod", c, map[string]any{
		"recv": recv, "method": mname, "iface": ifaceID,
		"args": args, "sig": sigID, "type": typ,
	}), nil
}

// exportIfaceFallbackCall exports a WS H F-bounded fallback method call:
// a method called on a type parameter whose concrete type is not a named
// type (devirtualization impossible). Dispatch goes through the constraint
// interface's itab, like a regular interface method call.
//
// Unlike exportIfaceMethodCall, the receiver's static type is a type
// parameter (not an interface), and the interface is the constraint
// (already validated by resolveMethodCall). The recv node is already
// exported by the caller.
func (e *exporter) exportIfaceFallbackCall(c *ast.CallExpr, sel *ast.SelectorExpr, selection *types.Selection, recv *Node, constraint types.Type) (*Node, error) {
	mname := sel.Sel.Name
	sig, err := e.resolveIfaceMethod(constraint, mname)
	if err != nil {
		return nil, err
	}
	// P3-M9 (WS H + WS D, graalgo-wsh-wsd-interaction): multi-result
	// methods via the fallback use the pair plan from
	// classifyIfaceFallbackMultiResultCall (lenient on types for generic
	// constraints), mirroring exportIfaceMethodCall's pairPlan path.
	// The itab maps the method to the concrete type's pair-returning
	// __-helper, so the node yields the pair and field extraction
	// proceeds like the concrete Appendix M desugar.
	var pairPlan *mrPlan
	if sig.Results().Len() > 1 {
		pairPlan = e.classifyIfaceFallbackMultiResultCall(selection)
		if pairPlan == nil {
			return nil, scopeErrorf("method %s via type-parameter constraint has %d results; at most one result is supported in frontend/v1 scope (WS D)",
				mname, sig.Results().Len())
		}
	}
	// P3-M9: variadic type-param method calls pack trailing args.
	// The packing is applied at the exportCallArgs site below.
	// The constraint must have an interface underlying (validated by
	// resolveMethodCall, but re-check here for safety).
	ifaceType := types.Unalias(constraint)
	if nt, ok := ifaceType.(*types.Named); ok {
		ui, ok := nt.Underlying().(*types.Interface)
		if !ok {
			return nil, fmt.Errorf("gofrontend: constraint %s is named with non-interface underlying (exporter bug)",
				types.TypeString(ifaceType, nil))
		}
		ifaceType = ui
	} else if _, ok := ifaceType.(*types.Interface); !ok {
		return nil, fmt.Errorf("gofrontend: constraint %s is not an interface (exporter bug)",
			types.TypeString(ifaceType, nil))
	}
	ifaceID, err := e.typeID(ifaceType)
	if err != nil {
		return nil, err
	}
	sigID, err := e.typeID(sig)
	if err != nil {
		return nil, err
	}
	// P3-M9: pack variadic type-param method args.
	args, err := e.exportCallArgsPacked(c, sig)
	if err != nil {
		return nil, err
	}
	// P3-M9 (WS H + WS D): for multi-result, the dispatch yields the
	// pair (not the multi-valued call result — callType scope-errors on
	// tuples). Mirror exportIfaceMethodCall's pairPlan path.
	var typ any
	if pairPlan != nil {
		tid, err := e.typeID(pairPlan.tmpType)
		if err != nil {
			return nil, err
		}
		typ = tid
	} else {
		typ, err = e.callType(c)
		if err != nil {
			return nil, err
		}
	}
	return e.node("ifacemethod", c, map[string]any{
		"recv": recv, "method": mname, "iface": ifaceID,
		"args": args, "sig": sigID, "type": typ,
	}), nil
}

// exportSyncOp exports a method call on a synthetic sync primitive
// (P3-M2b, WS D, ADR-0002) as an additive "syncop" wire node. The receiver
// evaluates to the Java primitive object itself (ADR-0002 D1: the Go value
// IS the object), so the pointer/value receiver distinction is a no-op at
// the value level — this deliberately bypasses exportMethodCall's WS D
// no-implicit-& rule, which would reject m.Lock() on an addressable value
// despite every real sync method having a pointer receiver. Out-of-scope
// methods (TryLock/TryRLock/RLocker/WaitGroup.Go) fail fast here with a
// named M2 scope error (ADR-0002 D6).
func (e *exporter) exportSyncOp(c *ast.CallExpr, sel *ast.SelectorExpr, nt *types.Named) (*Node, error) {
	// P3-M4 (WS C, ADR-0004): the sync/atomic overlay shares this path
	// (package-aware scope check via syntheticOpInScope).
	if err := syntheticOpInScope(nt, sel.Sel.Name); err != nil {
		return nil, err
	}
	// fix-linkname: sync.Map's multi-result methods (Load, LoadOrStore,
	// LoadAndDelete, Swap) only desugar in N-target assignment position
	// (the mrSyncMap plan, via exportMultiResultDesugar). Any other
	// position (return, argument, single-value) fails fast with a named
	// scope error — mirroring the runtime.* multi-result precedent —
	// instead of callType's generic WS D message.
	if nt.Obj().Pkg() != nil && nt.Obj().Pkg().Path() == "sync" && nt.Obj().Name() == "Map" {
		switch sel.Sel.Name {
		case "Load", "LoadOrStore", "LoadAndDelete", "Swap":
			return nil, scopeErrorf("sync.Map.%s is multi-result: use an N-target assignment (v, ok := m.Load(k)) in frontend/v1 scope (WS D)", sel.Sel.Name)
		}
	}
	recv, err := e.exportExpr(sel.X)
	if err != nil {
		return nil, err
	}
	args, err := e.exportCallArgs(c)
	if err != nil {
		return nil, err
	}
	return e.exportSyncOpNodes(c, sel, nt, recv, args)
}

// exportSyncOpNodes builds the "syncop" wire node from pre-exported
// receiver/argument nodes (P3-M2b, WS D, ADR-0002). exportSyncOp is the
// thin wrapper for the direct-call path; exportSyncGoStmt pre-exports
// hoisted go-time temporaries and calls this directly so the syncop body
// references the temporaries instead of the original expressions.
func (e *exporter) exportSyncOpNodes(c *ast.CallExpr, sel *ast.SelectorExpr, nt *types.Named, recv *Node, args []*Node) (*Node, error) {
	op := sel.Sel.Name
	typ, err := e.callType(c)
	if err != nil {
		return nil, err
	}
	return e.node("syncop", c, map[string]any{
		"op": op, "synctyp": syntheticOpTypeName(nt), "recv": recv, "args": args, "type": typ,
	}), nil
}

// exportTypeConversion exports a Go conversion T(x) as a "convert" node
// (M3, WS D). Factored so qualified conversions (time.Duration(5)) share
// the unqualified path.
func (e *exporter) exportTypeConversion(c *ast.CallExpr, tn *types.TypeName) (*Node, error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("gofrontend: conversion to %s has %d arguments (want 1)",
			tn.Name(), len(c.Args))
	}
	// `T(nil)` — the nil must be typed as T, not as the empty interface
	// (the default nil export).
	var x *Node
	if id, ok := c.Args[0].(*ast.Ident); ok && id.Name == "nil" {
		to, err := e.typeID(tn.Type())
		if err != nil {
			return nil, err
		}
		x = e.node("nil", id, map[string]any{"type": to})
	} else {
		var err error
		x, err = e.exportExpr(c.Args[0])
		if err != nil {
			return nil, err
		}
	}
	to, err := e.typeID(tn.Type())
	if err != nil {
		return nil, err
	}
	// P3-M8: unsafe.Pointer conversions (synth_unsafe.go). handled=false
	// keeps the existing path (notably unsafe.Pointer→uintptr, the D5 trap).
	if n, handled, err := e.exportUnsafeConversion(c, tn.Type(), x, to); handled {
		return n, err
	}
	// M7: "from" records the source type id (needed for string<->
	// slice conversions to get the element kind).
	var from any
	if tv, ok := e.info.Types[c.Args[0]]; ok && tv.Type != nil {
		fid, err := e.typeID(tv.Type)
		if err != nil {
			return nil, err
		}
		from = fid
	}
	// "type" records the conversion target: NodeFactory stamps it on
	// the built node (M4: interface boxing needs the target type id).
	fields := map[string]any{"x": x, "to": to, "type": to}
	if from != nil {
		fields["from"] = from
	}
	return e.node("convert", c, fields), nil
}

// exportCallArgs exports a call's argument expressions.
// BUG-MODPROBE-010: a user-defined multi-result call in argument position
// (f(g())) spreads g's results as f's arguments per Go spec (g() must be
// the sole argument; the type checker guarantees this for valid Go). The
// inner call is hoisted to a temp var (emitted to e.mrHoistPre, flushed by
// exportStmt), and the pair fields are spread as separate arguments.
// Nested MR calls (h(f(g()))) are handled recursively.
// Only user-defined MR kinds (mrUser, mrFuncField) are hoisted here; the
// synthetic M5/M7 kinds keep their pinned fail-fast outside desugar
// positions (TestMultiResultFailFastKept, GG-0017).
func (e *exporter) exportCallArgs(c *ast.CallExpr) ([]*Node, error) {
	args := make([]*Node, 0, len(c.Args))
	for _, a := range c.Args {
		if inner, ok := unparen(a).(*ast.CallExpr); ok {
			if innerPlan := e.classifyMultiResultCall(inner); innerPlan != nil &&
				(innerPlan.kind == mrUser || innerPlan.kind == mrFuncField) {
				// User-defined multi-result call as argument: hoist to
				// temp, unpack.
				unpacked, pre, err := e.hoistMRCallArg(inner, innerPlan)
				if err != nil {
					return nil, err
				}
				e.mrHoistPre = append(e.mrHoistPre, pre...)
				args = append(args, unpacked...)
				continue
			}
		}
		x, err := e.exportExpr(a)
		if err != nil {
			return nil, err
		}
		args = append(args, x)
	}
	return args, nil
}

// callType returns the type-table id of a call's result type (nil when the
// call has no result, e.g. a builtin like panic or a void function).
// go/types reports a no-result call as an empty *types.Tuple.
func (e *exporter) callType(c *ast.CallExpr) (any, error) {
	var typ any
	if tv, ok := e.info.Types[c]; ok && tv.Type != nil {
		if t, ok := tv.Type.(*types.Tuple); ok {
			if t.Len() == 0 {
				return nil, nil
			}
			// P3-M9 (WS D): user-defined multi-result functions transformed
			// to pair-returning form. The go/types type is still the
			// original tuple, but the emitted function returns the pair.
			if fn := e.calledFunc(c); fn != nil {
				if mrInfo, ok := e.userMRFuncs[fn]; ok {
					tid, err := e.typeID(mrInfo.pairType)
					if err != nil {
						return nil, err
					}
					return tid, nil
				}
				// P3-M9: synthetic multi-result methods (bytes.Buffer.Read,
				// etc.) fail fast with the named milestone scope error, not
				// the generic WS D message — the test pins the attribution.
				if e.isMultiResultSyntheticFailFast(fn) {
					return nil, multiResultSyntheticError(fn)
				}
			}
			return nil, scopeErrorf("multi-valued call result is not supported in frontend/v1 scope (WS D)")
		}
		tid, err := e.typeID(tv.Type)
		if err != nil {
			return nil, err
		}
		typ = tid
	}
	return typ, nil
}

// calledFunc returns the *types.Func denoted by a call expression's Fun,
// or nil if it doesn't denote a function.
func (e *exporter) calledFunc(c *ast.CallExpr) *types.Func {
	// Ident: package-level function foo().
	if id, ok := c.Fun.(*ast.Ident); ok {
		if fn, ok := e.info.Uses[id].(*types.Func); ok {
			return fn
		}
		return nil
	}
	// SelectorExpr: pkg.Func() or recv.Method().
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if selection, ok := e.info.Selections[sel]; ok {
			if fn, ok := selection.Obj().(*types.Func); ok {
				return fn
			}
		}
		// Package-qualified: pkg.Func().
		if id, ok := sel.X.(*ast.Ident); ok {
			if _, ok := e.info.Uses[id].(*types.PkgName); ok {
				if fn, ok := e.info.Uses[sel.Sel].(*types.Func); ok {
					return fn
				}
			}
		}
	}
	return nil
}

// exportFuncLit exports a function literal (M5, WS F). Params and results
// are emitted as kind:"param" nodes (the funcdecl param schema:
// name/obj/type), from the go/types signature tuple so unnamed
// parameters/results are covered too.
func (e *exporter) exportFuncLit(l *ast.FuncLit) (*Node, error) {
	tv, ok := e.info.Types[l.Type]
	if !ok || tv.Type == nil {
		return nil, fmt.Errorf("gofrontend: no type info for function literal")
	}
	sig, ok := tv.Type.(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("gofrontend: function literal has non-signature type %s", tv.Type)
	}
	params, err := e.exportSignatureVars(l.Type, sig.Params())
	if err != nil {
		return nil, err
	}
	results, err := e.exportSignatureVars(l.Type, sig.Results())
	if err != nil {
		return nil, err
	}
	if l.Body == nil {
		return nil, scopeErrorf("function literal without body")
	}
	body, err := e.exportBlock(l.Body)
	if err != nil {
		return nil, err
	}
	// M7: the literal's signature type id, so the factory can box the
	// function value into an interface (boxInterface needs the dynamic
	// type descriptor).
	tid, err := e.typeID(tv.Type)
	if err != nil {
		return nil, err
	}
	return e.node("funclit", l, map[string]any{
		"params": params, "results": results, "body": body, "type": tid,
	}), nil
}

// exportSignatureVars emits one kind:"param" node per *types.Var in a
// signature tuple (defining each in the object table as a param). Positions
// are best-effort (the enclosing func type); per-var positions don't exist
// for unnamed parameters.
func (e *exporter) exportSignatureVars(pos ast.Node, t *types.Tuple) ([]*Node, error) {
	out := make([]*Node, 0, t.Len())
	for i := 0; i < t.Len(); i++ {
		p, err := e.exportParamVar(pos, t.At(i))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Composite-type expressions (M4, WS E).
// ---------------------------------------------------------------------------

// exportCompositeLit exports a composite literal. The literal's type comes
// from go/types (info.Types), so syntactically elided element literals
// (`[][]int{{1,2}}`, `[]*Point{{1,2}}`) arrive with their full explicit
// type — the wire never carries elision.
func (e *exporter) exportCompositeLit(l *ast.CompositeLit) (*Node, error) {
	t, err := e.exprType(l)
	if err != nil {
		return nil, err
	}
	return e.exportCompositeLitAs(l, t)
}

// poolNewFieldValue reports whether l is exactly `sync.Pool{New: f}`
// (P3-M11) and returns f's expression.
func poolNewFieldValue(l *ast.CompositeLit) (ast.Expr, bool) {
	if len(l.Elts) != 1 {
		return nil, false
	}
	kv, ok := l.Elts[0].(*ast.KeyValueExpr)
	if !ok {
		return nil, false
	}
	id, ok := kv.Key.(*ast.Ident)
	if !ok || id.Name != "New" {
		return nil, false
	}
	return kv.Value, true
}

// exportCompositeLitAs is exportCompositeLit with an explicit type id
// (used for the &T{...} normalization, where the pointer type comes from
// the enclosing UnaryExpr).
func (e *exporter) exportCompositeLitAs(l *ast.CompositeLit, t int) (*Node, error) {
	// P3-M2b (ADR-0002 D3): a composite literal of a sync primitive
	// constructs the Java object directly — gc's ready-to-use zero value.
	// (Elements are impossible: the fields are unexported, so go/types
	// rejects any; the guard is defense in depth.)
	if tv, ok := e.info.Types[l]; ok && tv.Type != nil {
		if nt := syntheticPrimitiveNamed(tv.Type); nt != nil {
			if len(l.Elts) > 0 {
				// P3-M11: sync.Pool{New: f} — the only composite literal
				// with elements a sync primitive accepts (gc: every other
				// Pool field is unexported, so go/types already rejected
				// any other key or an unkeyed literal). Lowers to syncnew
				// carrying the New func, evaluated before construction
				// (gc's literal evaluation order).
				if nt.Obj().Name() == "Pool" {
					if nf, ok := poolNewFieldValue(l); ok {
						newFn, err := e.exportExpr(nf)
						if err != nil {
							return nil, err
						}
						return e.node("syncnew", l, map[string]any{
							"synctyp": syntheticOpTypeName(nt), "type": t,
							"newfunc": newFn,
						}), nil
					}
				}
				if atomicPrimitiveNamed(nt) != nil {
					return nil, scopeErrorf("sync/atomic.%s literal with elements is not in M4 scope", nt.Obj().Name())
				}
				return nil, scopeErrorf("sync.%s literal with elements is not in M2 scope", nt.Obj().Name())
			}
			return e.node("syncnew", l, map[string]any{
				"synctyp": syntheticOpTypeName(nt), "type": t,
			}), nil
		}
		// P3-M5 (WS E, ADR-0005 D1): time.Timer{}/time.Ticker{} literals
		// are not in M5 scope (gc's zero Timer is uninitialized; use
		// time.NewTimer/time.NewTicker).
		if nt := timeTimerTickerNamedExact(tv.Type); nt != nil {
			return nil, timerScopeErrorf(nt, "composite literal")
		}
	}
	keyed := false
	var keys []*Node
	var fieldKeys []string
	// Non-nil so an empty literal marshals as [] rather than null
	// (the factory requires values to be an array).
	values := []*Node{}
	for _, el := range l.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			keyed = true
			v, err := e.exportExpr(kv.Value)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
			// Struct literal keys are field names (not expressions);
			// map/array/slice literal keys are index expressions.
			if id, ok := kv.Key.(*ast.Ident); ok && e.isStructLitKey(l, id) {
				fieldKeys = append(fieldKeys, id.Name)
			} else {
				k, err := e.exportExpr(kv.Key)
				if err != nil {
					return nil, err
				}
				keys = append(keys, k)
			}
		} else {
			v, err := e.exportExpr(el)
			if err != nil {
				return nil, err
			}
			values = append(values, v)
		}
	}
	if keyed && len(keys)+len(fieldKeys) != len(values) {
		return nil, fmt.Errorf("gofrontend: keyed literal key/value count mismatch")
	}
	fields := map[string]any{"type": t, "keyed": keyed, "values": values}
	// P3-M9 (x509): compact encoding for large byte arrays. A [...]byte{...}
	// with 88k elements serializes as 17MB of JSON nodes (one per byte).
	// If the literal has >100 elements and all are byte-sized integer
	// literals, encode as a hex string in "hexdata" instead. The Java
	// NodeFactory.buildComplit decodes it directly. This is a general
	// frontend optimization, not x509-specific. (The earlier "truncated"
	// workaround is removed: it silently zeroed literals >100 scalars,
	// breaking valid certificates. Compact encoding preserves correctness.)
	if !keyed && len(l.Elts) > 100 {
		if hex, ok := e.byteLiteralHex(l.Elts); ok {
			fields["values"] = []*Node{}
			fields["hexdata"] = hex
		}
	}
	if len(fieldKeys) > 0 {
		fields["fieldkeys"] = fieldKeys
	}
	if len(keys) > 0 {
		fields["keys"] = keys
	}
	return e.node("complit", l, fields), nil
}

// byteLiteralHex extracts byte values from a list of AST expressions.
// Returns the hex-encoded string and true if all elements are integer
// literals in [0,255]. Used for compact encoding of large byte arrays.
func (e *exporter) byteLiteralHex(elts []ast.Expr) (string, bool) {
	var buf []byte
	for _, el := range elts {
		// Handle *ast.BasicLit (e.g., 0x3c, 255)
		bl, ok := el.(*ast.BasicLit)
		if !ok {
			return "", false
		}
		// Parse the integer value using go/types info
		tv, ok := e.info.Types[bl]
		if !ok || tv.Value == nil {
			return "", false
		}
		// Get the constant value as int64. Int64Val panics on non-Int
		// constants, so check the kind first.
		if tv.Value.Kind() != constant.Int {
			return "", false
		}
		v, ok := constant.Int64Val(tv.Value)
		if !ok || v < 0 || v > 255 {
			return "", false
		}
		buf = append(buf, byte(v))
	}
	// Hex encode
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(buf)*2)
	for i, b := range buf {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0xf]
	}
	return string(out), true
}

// countScalars recursively counts scalar (non-composite) values in a
// composite literal. Used to detect pathological tables.
func countScalars(l *ast.CompositeLit) int {
	count := 0
	for _, el := range l.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			el = kv.Value
		}
		if inner, ok := el.(*ast.CompositeLit); ok {
			count += countScalars(inner)
		} else {
			count++
		}
		// Early exit to avoid counting huge tables fully
		if count > 60000 {
			return count
		}
	}
	return count
}

// isStructLitKey reports whether id is a struct field name in a keyed
// composite literal (as opposed to a map index expression).
func (e *exporter) isStructLitKey(l *ast.CompositeLit, id *ast.Ident) bool {
	if tv, ok := e.info.Types[l]; ok && tv.Type != nil {
		t := tv.Type
		// Dereference pointers: []*T{{field: v}} has inner literals of
		// type *T, whose Underlying() is the pointer itself, not the struct.
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if _, ok := t.Underlying().(*types.Struct); ok {
			return true
		}
	}
	return false
}

// exportIndex exports x[index]. commaOk marks the two-valued map form
// (set by exportAssign/exportDefine from the assignment shape); the
// value type is the map's element type (go/types records a tuple for the
// two-valued form).
func (e *exporter) exportIndex(x *ast.IndexExpr, commaOk bool) (*Node, error) {
	var t int
	var err error
	if commaOk {
		mt, ok := e.info.Types[x.X].Type.Underlying().(*types.Map)
		if !ok {
			return nil, fmt.Errorf("gofrontend: comma-ok index on non-map type %v", e.info.Types[x.X].Type)
		}
		if t, err = e.typeID(mt.Elem()); err != nil {
			return nil, err
		}
	} else {
		if t, err = e.exprType(x); err != nil {
			return nil, err
		}
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	// P3-M7: an index operand may be an untyped string constant
	// (e.g. smalls[x0] where smalls is a const string) — materialize
	// it to string like exportSlice does.
	xe, err = e.materializeStringConst(x.X, xe)
	if err != nil {
		return nil, err
	}
	ie, err := e.exportExpr(x.Index)
	if err != nil {
		return nil, err
	}
	return e.node("index", x, map[string]any{
		"x": xe, "index": ie, "type": t, "commaOk": commaOk,
	}), nil
}

// materializeStringConst wraps xe in a no-op string conversion when x
// has untyped string type (a string constant used as a slice/index
// operand). The factory only accepts typed containers.
func (e *exporter) materializeStringConst(x ast.Expr, xe *Node) (*Node, error) {
	if tv, ok := e.info.Types[x]; ok && tv.Type != nil {
		if b, ok := tv.Type.(*types.Basic); ok && b.Kind() == types.UntypedString {
			to, err := e.typeID(types.Typ[types.String])
			if err != nil {
				return nil, err
			}
			from, err := e.typeID(tv.Type)
			if err != nil {
				return nil, err
			}
			return e.node("convert", x, map[string]any{
				"x": xe, "to": to, "type": to, "from": from,
			}), nil
		}
	}
	return xe, nil
}

// exportSlice exports x[lo:hi] and x[lo:hi:max] (absent bounds are null).
func (e *exporter) exportSlice(x *ast.SliceExpr) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	// P3-M7: the sliced operand may be an untyped string constant
	// (e.g. digits[i:i+1] where digits is a const string, as in
	// strconv's small()). The factory's slice node only accepts typed
	// containers, so materialize the constant to string first — a
	// no-op conversion the factory already supports (UNTYPED_STRING
	// is transparent to STRING).
	xe, err = e.materializeStringConst(x.X, xe)
	if err != nil {
		return nil, err
	}
	var lo, hi, max *Node
	if x.Low != nil {
		if lo, err = e.exportExpr(x.Low); err != nil {
			return nil, err
		}
	}
	if x.High != nil {
		if hi, err = e.exportExpr(x.High); err != nil {
			return nil, err
		}
	}
	if x.Max != nil {
		if max, err = e.exportExpr(x.Max); err != nil {
			return nil, err
		}
	}
	return e.node("sliceexpr", x, map[string]any{
		"x": xe, "lo": lo, "hi": hi, "max": max, "type": t,
	}), nil
}

// exportSelector exports x.Field. selpath is go/types' field-index path
// through (possibly embedded) structs, so the factory needs no promotion
// logic of its own.
func (e *exporter) exportSelector(x *ast.SelectorExpr) (*Node, error) {
	t, err := e.exprType(x)
	if err != nil {
		return nil, err
	}
	// P3-M5 (WS E, ADR-0005 D1): t.C on *time.Timer / *time.Ticker lowers
	// to the additive "timec" wire node (the Go values are opaque Java
	// objects; the struct field exists only for type-checking).
	if x.Sel.Name == "C" {
		if sel, ok := e.info.Selections[x]; ok && sel.Kind() == types.FieldVal {
			if nt := timeTimerTickerNamed(sel.Recv()); nt != nil {
				recv, err := e.exportExpr(x.X)
				if err != nil {
					return nil, err
				}
				return e.node("timec", x, map[string]any{"recv": recv, "type": t}), nil
			}
		}
	}
	// P3-M5 (WS E, ADR-0005): qualified identifiers time.X / context.X in
	// expression position (time.Second consts, context.Canceled vars).
	// P3-M7 (wave 1): errors/sort/bytes/math/strings consts/vars likewise,
	// plus the 8 encoding/* primitives' consts/vars (base64.StdEncoding,
	// binary.LittleEndian, hex.ErrLength, asn1.TagNull, ...).
	// P3-M8 (wave 2): syscall/os/io/fs/reflect/hash/crypto/crypto/*
	// consts/vars likewise (syscall.EINVAL, reflect.Int, ...).
	// P3-M9 (wave 3, tier-1a worker): cmp/slices/maps/unicode consts/vars
	// likewise (utf8.RuneError, unicode.Categories, ...).
	// Method expressions (time.Duration.String) have a SelectorExpr X and
	// fall through to the normal path.
	if ident, ok := x.X.(*ast.Ident); ok {
		if pkgName, ok := e.info.Uses[ident].(*types.PkgName); ok {
			if p := pkgName.Imported().Path(); p == "time" || p == "context" || p == "errors" || p == "sort" || p == "bytes" || p == "math" || p == "math/bits" || p == "math/big" || p == "math/cmplx" || p == "math/rand" || p == "math/rand/v2" || p == "bufio" || p == "path" || p == "text/scanner" || p == "regexp/syntax" || p == "strings" ||
				p == "encoding/base32" || p == "encoding/base64" || p == "encoding/hex" || p == "encoding/binary" || p == "encoding/ascii85" || p == "encoding/pem" || p == "encoding/asn1" || p == "encoding" || p == "encoding/xml" || p == "encoding/csv" || p == "encoding/gob" || p == "strconv" || p == "encoding/json" || p == "io" || p == "syscall" || p == "os" || p == "io/fs" || p == "reflect" || p == "hash" || p == "crypto" ||
				p == "crypto/aes" || p == "crypto/cipher" || p == "crypto/des" || p == "crypto/hmac" || p == "crypto/md5" || p == "crypto/rand" || p == "crypto/rc4" || p == "crypto/sha1" || p == "crypto/sha256" || p == "crypto/sha3" || p == "crypto/sha512" || p == "crypto/subtle" ||
				p == "cmp" || p == "slices" || p == "maps" || p == "unicode" || p == "unicode/utf8" || p == "unicode/utf16" ||
				p == "flag" || p == "testing" || p == "testing/quick" || p == "testing/iotest" ||
				// P3-M9 (tier-1d): unique/structs consts/vars (none today;
				// future-proof) and synthetic-only internal/bytealg
				// (MaxBruteForce/MaxLen, referenced from synthetic sources).
				// real-reflect (Phase 3): synthetic internal/cpu vars
				// (cpu.X86, cpu.ARM64, ... feature structs, referenced
				// from internal/runtime/maps AlgInit).
				p == "unique" || p == "structs" || p == "internal/bytealg" ||
				p == "internal/cpu" ||
				// P3-M9 (wave 3, tier-1b): container/* + hash/* consts/vars
				// (crc32.IEEE, crc64.ECMA, fnv offsets, ...).
				// P3-M9 (wave 3, tier-5b): compress/* consts/vars
				// (lzw.LSB/MSB, flate/gzip/zlib level consts).
				// P3-M9 (tier-5): asymmetric-crypto consts/vars/funcs
				// (elliptic, ecdh, dsa, ecdsa, ed25519, rsa, hkdf, pbkdf2
				// plus the internal fips140/* deps and cryptobyte that the
				// synthetic sources reference as qualified idents —
				// without this, e.g. nistec.NewP256Point falls through to
				// exportExpr(x.X) and fails `identifier "nistec" denotes
				// *types.PkgName`).
				p == "container/heap" || p == "container/list" || p == "container/ring" ||
				p == "hash/adler32" || p == "hash/crc32" || p == "hash/crc64" ||
				p == "hash/fnv" || p == "hash/maphash" ||
				p == "compress/flate" || p == "compress/gzip" || p == "compress/zlib" ||
				p == "compress/lzw" || p == "compress/bzip2" ||
				p == "crypto/elliptic" || p == "crypto/ecdh" || p == "crypto/dsa" ||
				p == "crypto/ecdsa" || p == "crypto/ed25519" || p == "crypto/rsa" ||
				p == "crypto/hkdf" || p == "crypto/pbkdf2" ||
				// P3-M9 (tier-5): synthetic crypto/x509 + crypto/tls
				// qualified idents in expression position (x509.ExtKeyUsageServerAuth,
				// tls.VersionTLS13, ...); the call path (exportSyntheticPkgCall)
				// already routes these packages.
				p == "crypto/x509" || p == "crypto/x509/pkix" || p == "crypto/tls" ||
				p == "golang.org/x/crypto/cryptobyte" ||
				p == "golang.org/x/crypto/cryptobyte/asn1" ||
				p == "crypto/internal/fips140" || p == "crypto/internal/fips140/check" ||
				p == "crypto/internal/fips140/alias" || p == "crypto/internal/fips140only" ||
				p == "crypto/internal/fips140cache" || p == "crypto/internal/fips140hash" ||
				p == "crypto/internal/fips140/drbg" || p == "crypto/internal/rand" ||
				p == "crypto/internal/boring/bbig" || p == "crypto/internal/boring" ||
				p == "crypto/internal/constanttime" ||
				p == "crypto/internal/fips140deps/byteorder" ||
				p == "crypto/internal/fips140/nistec/fiat" ||
				p == "crypto/internal/fips140/nistec" ||
				p == "crypto/internal/fips140/edwards25519/field" ||
				p == "crypto/internal/fips140/edwards25519" ||
				p == "crypto/internal/fips140/bigmod" ||
				p == "crypto/internal/fips140/subtle" ||
				p == "crypto/internal/fips140/sha256" ||
				p == "crypto/internal/fips140/sha512" ||
				p == "crypto/internal/fips140/sha3" ||
				p == "crypto/internal/fips140/hmac" ||
				p == "crypto/internal/fips140/hkdf" ||
				p == "crypto/internal/fips140/pbkdf2" ||
				p == "crypto/internal/fips140/ecdh" ||
				p == "crypto/internal/fips140/ecdsa" ||
				p == "crypto/internal/fips140/ed25519" ||
				p == "crypto/internal/fips140/rsa" {
				if _, isSel := e.info.Selections[x]; !isSel {
					return e.exportSyntheticQualified(x, pkgName)
				}
			}
			// WS G (P3-M9, BUG-MODPROBE-007): disk-package qualified
			// const/var access (e.g., uuid.RFC4122). General: any
			// disk-built package resolves here; the synthetic list
			// above is for overlay packages.
			if _, derr := getDiskPackage(pkgName.Imported().Path()); derr == nil {
				if _, isSel := e.info.Selections[x]; !isSel {
					return e.exportSyntheticQualified(x, pkgName)
				}
			}
		}
	}
	// P3-M3 (WS D, ADR-0003): a method expression on an interface type
	// (T.M in value position) fails fast here, before x.X is exported —
	// x.X is a type name, not an exportable expression (exporting it
	// yields the confusing `identifier "T" denotes *types.TypeName`
	// internal error). Named P3-M3 scope error, greppable re-test
	// trigger.
	if sel, ok := e.info.Selections[x]; ok && sel.Kind() == types.MethodExpr &&
		recvIface(sel.Recv()) != nil {
		return nil, scopeErrorf("method expression %s is not supported: method expression via interface (P3-M3 scope)", x.Sel.Name)
	}
	// P3-M11 (sync.Pool): the New field of the synthetic Pool type. The
	// Go value is the opaque GoPool Java object (ADR-0002 D1), so the
	// generic struct-field "selector" node does not apply: lower the read
	// to the GetNew syncop. Writes (`p.New = f`) are intercepted in
	// exportAssign (SetNew); calls (`p.New()`) at the FieldVal call site
	// (CallNew).
	if nt := e.poolNewSelector(x); nt != nil {
		recv, err := e.exportExpr(x.X)
		if err != nil {
			return nil, err
		}
		return e.syncFieldOp(x, "GetNew", nt, recv, []*Node{}, t), nil
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	sel, ok := e.info.Selections[x]
	if !ok {
		return nil, scopeErrorf("selector %q is not a field selection (WS D)", x.Sel.Name)
	}
	// M6 (WS H): a selector denoting a method in value position is a
	// method value (bound receiver) — exported as a "methodvalue" node
	// (P3-M9, WS H follow-up). Method CALLS are handled by
	// exportMethodCall.
	// P3-M3 (WS D, ADR-0003): method values and method expressions on
	// interface receivers keep failing with a named P3-M3 scope error
	// (greppable re-test trigger): dynamic dispatch deferred to call
	// time is out of scope.
	if _, ok := sel.Obj().(*types.Func); ok {
		var rt types.Type
		if sel.Kind() == types.MethodExpr {
			rt = sel.Recv()
		} else if tv, ok := e.info.Types[x.X]; ok {
			rt = tv.Type
		}
		if recvIface(rt) != nil {
			what := "method value"
			if sel.Kind() == types.MethodExpr {
				what = "method expression"
			}
			return nil, scopeErrorf("%s %s is not supported: %s via interface (P3-M3 scope)",
				what, x.Sel.Name, what)
		}
		if sel.Kind() == types.MethodExpr {
			return nil, scopeErrorf("method expression %s is not supported: method expression on concrete type (P3-M3 scope)", x.Sel.Name)
		}
		return e.methodValueNode(x, sel, xe, t)
	}
	idx := sel.Index()
	path := make([]any, len(idx))
	for i, v := range idx {
		path[i] = v
	}
	return e.node("selector", x, map[string]any{
		"x": xe, "field": x.Sel.Name, "selpath": path, "type": t,
	}), nil
}

// exportTypeAssert exports x.(T). commaOk marks the two-valued form; the
// value type is the asserted type (go/types records a tuple for the
// two-valued form).
func (e *exporter) exportTypeAssert(x *ast.TypeAssertExpr, commaOk bool) (*Node, error) {
	if x.Type == nil {
		return nil, scopeErrorf("x.(type) outside a type switch is not supported in frontend/v1 scope")
	}
	var t int
	var err error
	if commaOk {
		if t, err = e.exprType(x.Type); err != nil {
			return nil, err
		}
	} else {
		if t, err = e.exprType(x); err != nil {
			return nil, err
		}
	}
	xe, err := e.exportExpr(x.X)
	if err != nil {
		return nil, err
	}
	return e.node("typeassert", x, map[string]any{
		"x": xe, "type": t, "commaOk": commaOk,
	}), nil
}

// unparen strips parenthesized wrappers (used for comma-ok shape detection;
// the exporter normalizes the paren away in that position).
func unparen(x ast.Expr) ast.Expr {
	for {
		p, ok := x.(*ast.ParenExpr)
		if !ok {
			return x
		}
		x = p.X
	}
}

// isCommaOkRhs reports whether r is a two-valued map-index,
// type-assertion, or channel-receive expression (the `v, ok := m[k]` /
// `v, ok := x.(T)` / `v, ok := <-ch` shapes).
func isCommaOkRhs(r ast.Expr) bool {
	switch r := unparen(r).(type) {
	case *ast.IndexExpr, *ast.TypeAssertExpr:
		return true
	case *ast.UnaryExpr:
		// P3-M5 (WS E, ADR-0005 App C): v, ok := <-ch.
		return r.Op == token.ARROW
	}
	return false
}
