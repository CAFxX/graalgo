package main

// Upstream-Go signature oracle for the synthetic multi-result desugars
// (R2-P1-3).
//
// The wiring tests (synth_m7desugar_test.go, synth_compress_desugar_test.go)
// verify SELF-CONSISTENCY ONLY: they compare three artifacts the same lane
// authored — the desugar tables in mrdesugar_m5.go, the synthetic sources,
// and the pair structs. A mutation that changes all three consistently passes
// every step while the wire format disagrees with real gc: e.g. changing
// __atoiPub's pair field `v int` to `v int64` (with matching pair struct and
// stub updates) keeps all 5 wiring steps green but breaks strconv.Atoi's
// wire format.
//
// This file closes that hole. For every entry of the five desugar tables
// (m7MultiResultFuncs, m8MultiResultFuncs, osMultiResultFuncs,
// m9MultiResultFuncs, ioMultiResultFuncs) plus the xml.Decoder
// Token/RawToken desugar (mrXmlToken, matched structurally by
// isXmlTokenMethod), TestSyntheticMultiResultOracle type-checks the REAL
// stdlib package from the pinned toolchain's GOROOT/src with go/types and
// asserts the synthetic multi-result function's signature matches the real
// one — arity, parameter types, result types — ignoring the desugar pair
// machinery entirely. (The receiver is pinned by the table-driven lookups,
// not compared; see oracleSigIdentical.)
//
// types.Identical cannot be used directly: the synthetic and real packages
// are checked by two separate types.Config runs, and named-type identity is
// per-check (synthetic os.File and real os.File are distinct *types.Named
// values). oracleIdentical compares structurally instead: named types are
// identified by (package path, type name, type arguments); everything else
// recurses. Parameter/result/variable names are ignored throughout — only
// types are compared. The receiver is not compared at all (see
// oracleSigIdentical): its identity is pinned by the table-driven lookups on
// each side, and ports re-declare receiver types.
//
// TestSyntheticIfacePairOracle covers the structural interface-dispatch
// table m8IfaceMultiResultMethods: the synthetic pair struct's field types
// (the wire format for interface dispatch) must equal the real interface
// method's result types positionally.
//
// Entries whose synthetic declaration has no real-stdlib counterpart by
// design (synthetic-only inventions) are listed in oracleSyntheticOnly with
// a reason each. A table entry missing from real stdlib that is NOT listed
// there fails loudly — the list is the complete inventory of deliberate
// synthetic inventions, not a place to park drift.
//
// The oracle resolves GOROOT via GRAALGO_TEST_GOROOT (override) or
// runtime.GOROOT() and requires exactly go1.27.1 (VERSION file); a version
// mismatch fails loudly instead of oracling against the wrong stdlib.

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// GOROOT resolution
// ---------------------------------------------------------------------------

// oracleGOROOT resolves the stdlib source tree the oracle type-checks and
// pins it to the toolchain this repo builds against.
func oracleGOROOT(t *testing.T) string {
	t.Helper()
	if g := os.Getenv("GRAALGO_TEST_GOROOT"); g != "" {
		return oracleCheckGoVersion(t, g)
	}
	return oracleCheckGoVersion(t, runtime.GOROOT())
}

func oracleCheckGoVersion(t *testing.T, goroot string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(goroot, "VERSION"))
	if err != nil {
		t.Fatalf("oracle: cannot read %s/VERSION: %v (override with GRAALGO_TEST_GOROOT)", goroot, err)
	}
	// VERSION has two lines: "go1.27.1" and a build timestamp.
	v := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	if v != "go1.27.1" {
		t.Fatalf("oracle: requires go1.27.1 stdlib, GOROOT %s reports %q (override with GRAALGO_TEST_GOROOT)", goroot, v)
	}
	if fi, err := os.Stat(filepath.Join(goroot, "src")); err != nil || !fi.IsDir() {
		t.Fatalf("oracle: %s/src is not a directory", goroot)
	}
	return goroot
}

// ---------------------------------------------------------------------------
// Source importer: type-checks real stdlib packages from GOROOT/src
// ---------------------------------------------------------------------------

// oracleImporter type-checks real stdlib packages from GOROOT/src with
// go/types, using go/build to select files for linux/amd64 with cgo
// disabled (every package the tables need has a cgo-free build; this keeps
// `import "C"` out of the type-check). Imported packages are cached; type
// errors are recorded per package instead of aborting, so lookups can still
// be attempted and diagnosed.
type oracleImporter struct {
	mu     sync.Mutex
	goroot string
	ctxt   build.Context
	fset   *token.FileSet
	pkgs   map[string]*types.Package
	busy   map[string]bool
	errs   map[string][]error
}

func newOracleImporter(goroot string) *oracleImporter {
	ctxt := build.Default
	ctxt.GOROOT = goroot
	ctxt.GOOS = "linux"
	ctxt.GOARCH = "amd64"
	ctxt.CgoEnabled = false
	return &oracleImporter{
		goroot: goroot,
		ctxt:   ctxt,
		fset:   token.NewFileSet(),
		pkgs:   make(map[string]*types.Package),
		busy:   make(map[string]bool),
		errs:   make(map[string][]error),
	}
}

// sourceDir resolves an import path to a source directory:
// GOROOT/src/<path>, falling back to GOROOT/src/vendor/<path> for vendored
// modules (e.g. golang.org/x/crypto/cryptobyte, imported by crypto/x509).
func (o *oracleImporter) sourceDir(path string) (string, error) {
	for _, dir := range []string{
		filepath.Join(o.goroot, "src", path),
		filepath.Join(o.goroot, "src", "vendor", path),
	} {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no source directory for import %q", path)
}

func (o *oracleImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	o.mu.Lock()
	if p, ok := o.pkgs[path]; ok {
		o.mu.Unlock()
		return p, nil
	}
	if o.busy[path] {
		o.mu.Unlock()
		return nil, fmt.Errorf("import cycle involving %q", path)
	}
	o.busy[path] = true
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		delete(o.busy, path)
		o.mu.Unlock()
	}()

	dir, err := o.sourceDir(path)
	if err != nil {
		return nil, err
	}
	bp, err := o.ctxt.ImportDir(dir, 0)
	if err != nil {
		return nil, fmt.Errorf("go/build ImportDir %s: %v", dir, err)
	}
	files := make([]*ast.File, 0, len(bp.GoFiles))
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(o.fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s/%s: %v", path, name, err)
		}
		files = append(files, f)
	}
	var errs []error
	conf := types.Config{
		Importer: o,
		Error:    func(e error) { errs = append(errs, e) },
	}
	pkg, _ := conf.Check(path, o.fset, files, nil)
	o.mu.Lock()
	o.pkgs[path] = pkg
	if len(errs) > 0 {
		o.errs[path] = errs
	}
	o.mu.Unlock()
	return pkg, nil
}

// ImportFrom implements types.ImporterFrom. Stdlib packages have no
// relative-vendoring subtleties beyond sourceDir's vendor fallback, so the
// importing directory is irrelevant.
func (o *oracleImporter) ImportFrom(path, _ string, _ types.ImportMode) (*types.Package, error) {
	return o.Import(path)
}

// typeErrors returns the recorded type-check errors for path (nil if clean).
func (o *oracleImporter) typeErrors(path string) []error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.errs[path]
}

// ---------------------------------------------------------------------------
// Cross-world type identity
// ---------------------------------------------------------------------------

// oraclePkgPath renders a package's identity for cross-world named-type
// comparison. nil (universe scope) renders as "".
func oraclePkgPath(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Path()
}

// oracleString renders a type with named types qualified by full import path,
// so synthetic-world and real-world types print comparably in diagnostics.
func oracleString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Path()
	})
}

// oracleIdentical reports whether synthetic-world type a denotes the same
// type as real-world type b. types.Identical is unusable across two separate
// types.Config.Check runs (named-type identity is per-check), so named types
// are identified by (package path, type name, type arguments) and everything
// else is compared structurally. Aliases are resolved first: `type
// RawMessage = jsontext.Value` (encoding/json under the toolchain-default
// jsonv2 experiment) denotes the same type as jsontext.Value.
// oracleTypePair is a visited (synthetic, real) type pair for the coinductive
// equality check.
type oracleTypePair struct {
	a, b types.Type
}

func oracleIdentical(a, b types.Type) bool {
	return oracleIdenticalSeen(a, b, make(map[oracleTypePair]bool))
}

// oracleIdenticalSeen is the recursive worker. Type graphs can be cyclic
// (e.g. a generic type parameter P constrained by an interface whose methods
// mention P), so pairs already under comparison are treated as equal — the
// standard coinductive rule for equirecursive type equality. Any genuine
// difference manifests at finite depth and is reported before a pair
// repeats.
func oracleIdenticalSeen(a, b types.Type, seen map[oracleTypePair]bool) bool {
	a = types.Unalias(a)
	b = types.Unalias(b)
	if a == b {
		return true
	}
	p := oracleTypePair{a, b}
	if seen[p] {
		return true
	}
	seen[p] = true
	eq := func(x, y types.Type) bool { return oracleIdenticalSeen(x, y, seen) }
	switch at := a.(type) {
	case *types.Basic:
		bt, ok := b.(*types.Basic)
		return ok && at.Kind() == bt.Kind()
	case *types.Named:
		bt, ok := b.(*types.Named)
		if !ok {
			return false
		}
		if at.Obj().Name() != bt.Obj().Name() ||
			oraclePkgPath(at.Obj().Pkg()) != oraclePkgPath(bt.Obj().Pkg()) {
			return false
		}
		aa, ba := at.TypeArgs(), bt.TypeArgs()
		if aa.Len() != ba.Len() {
			return false
		}
		for i := 0; i < aa.Len(); i++ {
			if !eq(aa.At(i), ba.At(i)) {
				return false
			}
		}
		return true
	case *types.Pointer:
		bt, ok := b.(*types.Pointer)
		return ok && eq(at.Elem(), bt.Elem())
	case *types.Slice:
		bt, ok := b.(*types.Slice)
		return ok && eq(at.Elem(), bt.Elem())
	case *types.Array:
		bt, ok := b.(*types.Array)
		return ok && at.Len() == bt.Len() && eq(at.Elem(), bt.Elem())
	case *types.Map:
		bt, ok := b.(*types.Map)
		return ok && eq(at.Key(), bt.Key()) && eq(at.Elem(), bt.Elem())
	case *types.Chan:
		bt, ok := b.(*types.Chan)
		return ok && at.Dir() == bt.Dir() && eq(at.Elem(), bt.Elem())
	case *types.Signature:
		bt, ok := b.(*types.Signature)
		return ok && oracleSigMismatchSeen(at, bt, seen) == ""
	case *types.Struct:
		bt, ok := b.(*types.Struct)
		if !ok || at.NumFields() != bt.NumFields() {
			return false
		}
		for i := 0; i < at.NumFields(); i++ {
			af, bf := at.Field(i), bt.Field(i)
			if af.Name() != bf.Name() || af.Embedded() != bf.Embedded() ||
				at.Tag(i) != bt.Tag(i) || !eq(af.Type(), bf.Type()) {
				return false
			}
		}
		return true
	case *types.Interface:
		bt, ok := b.(*types.Interface)
		if !ok || at.NumMethods() != bt.NumMethods() || at.NumEmbeddeds() != bt.NumEmbeddeds() {
			return false
		}
		for i := 0; i < at.NumEmbeddeds(); i++ {
			if !eq(at.EmbeddedType(i), bt.EmbeddedType(i)) {
				return false
			}
		}
		for i := 0; i < at.NumMethods(); i++ {
			am := at.Method(i)
			var bm *types.Func
			for j := 0; j < bt.NumMethods(); j++ {
				if bt.Method(j).Name() == am.Name() {
					bm = bt.Method(j)
					break
				}
			}
			if bm == nil {
				return false
			}
			as, aok := am.Type().(*types.Signature)
			bs, bok := bm.Type().(*types.Signature)
			if !aok || !bok || oracleSigMismatchSeen(as, bs, seen) != "" {
				return false
			}
		}
		return true
	case *types.TypeParam:
		bt, ok := b.(*types.TypeParam)
		return ok && at.Obj().Name() == bt.Obj().Name() &&
			eq(at.Constraint(), bt.Constraint())
	case *types.Union:
		bt, ok := b.(*types.Union)
		if !ok || at.Len() != bt.Len() {
			return false
		}
		for i := 0; i < at.Len(); i++ {
			if !eq(at.Term(i).Type(), bt.Term(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// oracleSigIdentical compares two signatures structurally: parameter types,
// result types, variadic flag, and type parameters. Names are ignored — only
// types are compared. The receiver is deliberately NOT compared: the issue
// scopes the oracle to (arity, param types, result types), the receiver's
// identity is pinned by the table-driven lookups on each side (a lookup
// failure there is already loud), and synthetic ports re-declare receiver
// types (e.g. encoding/json's own RawMessage vs the toolchain-default alias
// to jsontext.Value) — comparing cross-world receiver identity would drown
// the wire-format signal in port-declaration noise.
func oracleSigIdentical(a, b *types.Signature) bool {
	return oracleSigMismatch(a, b) == ""
}

// oracleSigMismatch compares synthetic-world signature a against real-world
// signature b and returns "" on match, else a human-readable description of
// the first difference found.
func oracleSigMismatch(a, b *types.Signature) string {
	return oracleSigMismatchSeen(a, b, make(map[oracleTypePair]bool))
}

// oracleSigMismatchSeen is the recursive worker sharing the visited-pair set
// with oracleIdenticalSeen (see the coinduction note there).
func oracleSigMismatchSeen(a, b *types.Signature, seen map[oracleTypePair]bool) string {
	eq := func(x, y types.Type) bool { return oracleIdenticalSeen(x, y, seen) }
	if (a.Recv() == nil) != (b.Recv() == nil) {
		return "receiver presence differs"
	}
	if a.Variadic() != b.Variadic() {
		return "variadic flag differs"
	}
	if a.Params().Len() != b.Params().Len() {
		return fmt.Sprintf("param count differs: synthetic %d vs real %d",
			a.Params().Len(), b.Params().Len())
	}
	for i := 0; i < a.Params().Len(); i++ {
		if !eq(a.Params().At(i).Type(), b.Params().At(i).Type()) {
			return fmt.Sprintf("param %d differs: synthetic %s vs real %s", i,
				oracleString(a.Params().At(i).Type()), oracleString(b.Params().At(i).Type()))
		}
	}
	if a.Results().Len() != b.Results().Len() {
		return fmt.Sprintf("result count differs: synthetic %d vs real %d",
			a.Results().Len(), b.Results().Len())
	}
	for i := 0; i < a.Results().Len(); i++ {
		if !eq(a.Results().At(i).Type(), b.Results().At(i).Type()) {
			return fmt.Sprintf("result %d differs: synthetic %s vs real %s", i,
				oracleString(a.Results().At(i).Type()), oracleString(b.Results().At(i).Type()))
		}
	}
	ta, tb := a.TypeParams(), b.TypeParams()
	if ta.Len() != tb.Len() {
		return fmt.Sprintf("type param count differs: synthetic %d vs real %d", ta.Len(), tb.Len())
	}
	for i := 0; i < ta.Len(); i++ {
		if !eq(ta.At(i), tb.At(i)) {
			return fmt.Sprintf("type param %d differs: synthetic %s vs real %s", i,
				oracleString(ta.At(i)), oracleString(tb.At(i)))
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Entry enumeration and lookup
// ---------------------------------------------------------------------------

// oracleEntry is one synthetic multi-result function/method to oracle against
// real stdlib: the (pkgPath, name, recvType) triple from a desugar table.
type oracleEntry struct {
	table    string // desugar table the entry came from: m7/m8/os/m9/io/xml
	pkgPath  string
	name     string
	recvType string // "" for package-level functions
}

func (e oracleEntry) key() string {
	if e.recvType == "" {
		return e.table + "/" + e.pkgPath + "." + e.name
	}
	return e.table + "/" + e.pkgPath + ".(" + e.recvType + ")." + e.name
}

// collectOracleEntries flattens the five desugar tables plus the xml
// Token/RawToken desugar (mrXmlToken, matched structurally by
// isXmlTokenMethod on (*encoding/xml.Decoder).Token/RawToken) into one list.
func collectOracleEntries() []oracleEntry {
	var out []oracleEntry
	for _, mf := range m7MultiResultFuncs {
		out = append(out, oracleEntry{"m7", mf.pkgPath, mf.name, mf.recvType})
	}
	for _, mf := range ioMultiResultFuncs {
		out = append(out, oracleEntry{"io", mf.pkgPath, mf.name, mf.recvType})
	}
	for _, mf := range m8MultiResultFuncs {
		out = append(out, oracleEntry{"m8", mf.pkgPath, mf.name, mf.recvType})
	}
	for _, mf := range osMultiResultFuncs {
		out = append(out, oracleEntry{"os", mf.pkgPath, mf.name, mf.recvType})
	}
	for _, mf := range m9MultiResultFuncs {
		out = append(out, oracleEntry{"m9", mf.pkgPath, mf.name, mf.recvType})
	}
	out = append(out,
		oracleEntry{"xml", "encoding/xml", "Token", "Decoder"},
		oracleEntry{"xml", "encoding/xml", "RawToken", "Decoder"},
	)
	return out
}

// lookupSyntheticFunc finds the synthetic multi-result function/method in an
// already type-checked synthetic package. It mirrors
// lookupM7MultiResultFunc's method-set logic but returns errors instead of
// failing the test, so the oracle can report precisely.
func lookupSyntheticFunc(sp *syntheticPackage, name, recvType string) (*types.Func, error) {
	if recvType == "" {
		obj := sp.pkg.Scope().Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok || fn == nil {
			return nil, fmt.Errorf("synthetic %s.%s missing or not a func (got %T)", sp.path, name, obj)
		}
		return fn, nil
	}
	return lookupOracleMethod(sp.pkg, sp.path, recvType, name)
}

// lookupOracleMethod finds name in the method set of the named type recvType
// in pkg. The pointer method set covers both pointer and value receivers,
// mirroring lookupM7MultiResultFunc. Type aliases are resolved: under the
// toolchain-default jsonv2 experiment, real encoding/json declares
// `type RawMessage = jsontext.Value`, and MarshalJSON lives on jsontext.Value.
func lookupOracleMethod(pkg *types.Package, pkgPath, recvType, name string) (*types.Func, error) {
	obj := pkg.Scope().Lookup(recvType)
	if obj == nil {
		return nil, fmt.Errorf("%s: type %q not in package scope", pkgPath, recvType)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s: %q is %T, want *types.TypeName", pkgPath, recvType, obj)
	}
	nt, ok := types.Unalias(tn.Type()).(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s: %q has type %T, want named", pkgPath, recvType, tn.Type())
	}
	// The pointer method set covers both pointer and value receivers for
	// concrete types. For interfaces, the pointer method set is empty in
	// go/types — use the interface's method set directly.
	var ms *types.MethodSet
	if _, ok := nt.Underlying().(*types.Interface); ok {
		ms = types.NewMethodSet(nt)
	} else {
		ms = types.NewMethodSet(types.NewPointer(nt))
	}
	sel := ms.Lookup(pkg, name)
	if sel == nil {
		return nil, fmt.Errorf("%s: method (%s).%s not in method set", pkgPath, recvType, name)
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return nil, fmt.Errorf("%s: (%s).%s is %T, want *types.Func", pkgPath, recvType, name, sel.Obj())
	}
	return fn, nil
}

// lookupRealFunc finds the real-stdlib function/method for an oracle entry.
func lookupRealFunc(imp *oracleImporter, e oracleEntry) (*types.Func, error) {
	pkg, err := imp.Import(e.pkgPath)
	if err != nil {
		return nil, fmt.Errorf("import real %q: %v", e.pkgPath, err)
	}
	if e.recvType == "" {
		obj := pkg.Scope().Lookup(e.name)
		fn, ok := obj.(*types.Func)
		if !ok || fn == nil {
			return nil, fmt.Errorf("real %s.%s missing or not a func (got %T)", e.pkgPath, e.name, obj)
		}
		return fn, nil
	}
	return lookupOracleMethod(pkg, e.pkgPath, e.recvType, e.name)
}

// oracleSyntheticOnly lists table entries whose synthetic multi-result
// declaration has NO real-stdlib counterpart by design: the synthetic
// package invents the function or its receiver type. Each entry carries its
// reason. A table entry missing from real stdlib that is NOT listed here
// fails the oracle loudly.
var oracleSyntheticOnly = map[string]string{
	// crypto/cipher gcmFallback is invented by the synthetic cipher package
	// (P3-M8b fallback implementation); real crypto/cipher has no such type.
	"m8/crypto/cipher.(gcmFallback).Open": "synthetic-only receiver type gcmFallback",
	// reader is invented by the synthetic crypto/rand package; real
	// crypto/rand exposes only package-level Read (Reader is an io.Reader
	// supplied by crypto/internal/rand).
	"m8/crypto/rand.(reader).Read": "synthetic-only receiver type reader",
	// The synthetic net/http package is a curated port whose unexported
	// helpers do not mirror real net/http internals: real net/http has no
	// readLine, chunkedReader, limitedReadCloser or responseWriter, and its
	// readMIMEHeader takes (*textproto.Reader, int64, int64), not
	// (*bufio.Reader).
	"m7/net/http.readLine":                 "synthetic-only helper: no real net/http.readLine",
	"m7/net/http.readMIMEHeader":           "synthetic-only helper: real readMIMEHeader has a different signature",
	"m7/net/http.(chunkedReader).Read":     "synthetic-only type: no real net/http.chunkedReader",
	"m7/net/http.(limitedReadCloser).Read": "synthetic-only type: no real net/http.limitedReadCloser",
	"m7/net/http.(responseWriter).Write":   "synthetic-only type: no real net/http.responseWriter",
	"m7/net/http.(responseWriter).Hijack":  "synthetic-only type: no real net/http.responseWriter",
	// Software-reimplementation digests: the synthetic crypto/sha256,
	// crypto/sha512 and crypto/hmac packages define their own digest/hmac
	// types ("Software implementations only", synth_machinery.go); real
	// crypto/sha256, crypto/sha512 and crypto/hmac delegate to
	// crypto/internal/fips140/*, whose table entries are oracled separately.
	"m8/crypto/sha256.(digest).Write":         "synthetic-only type: real crypto/sha256 delegates to fips140",
	"m8/crypto/sha256.(digest).Clone":         "synthetic-only type: real crypto/sha256 delegates to fips140",
	"m8/crypto/sha256.(digest).MarshalBinary": "synthetic-only type: real crypto/sha256 delegates to fips140",
	"m8/crypto/sha256.(digest).AppendBinary":  "synthetic-only type: real crypto/sha256 delegates to fips140",
	"m8/crypto/sha512.(digest).Write":         "synthetic-only type: real crypto/sha512 delegates to fips140",
	"m8/crypto/sha512.(digest).Clone":         "synthetic-only type: real crypto/sha512 delegates to fips140",
	"m8/crypto/sha512.(digest).MarshalBinary": "synthetic-only type: real crypto/sha512 delegates to fips140",
	"m8/crypto/sha512.(digest).AppendBinary":  "synthetic-only type: real crypto/sha512 delegates to fips140",
	"m8/crypto/hmac.(hmac).Write":             "synthetic-only type: real crypto/hmac delegates to fips140",
	"m8/crypto/hmac.(hmac).Clone":             "synthetic-only type: real crypto/hmac delegates to fips140",
	// The synthetic nistec package ports the pure-Go implementation
	// (p256.go); real linux/amd64 uses the assembly stubs (p256_asm.go),
	// whose internal helper signatures differ (boothW5/boothW6 take uint
	// and return (int, int); the pure-Go versions take uint64 and return
	// (uint8, int), which the synthetic matches exactly).
	"m8/crypto/internal/fips140/nistec.boothW5":                   "synthetic ports pure-Go nistec; real amd64 uses asm stubs",
	"m8/crypto/internal/fips140/nistec.boothW6":                   "synthetic ports pure-Go nistec; real amd64 uses asm stubs",
	"m8/crypto/internal/fips140/nistec.(p256OrdElement).SetBytes": "synthetic ports pure-Go nistec; real amd64 asm has no such method",
	// The synthetic crypto/x509 package delegates to graalgo.x509.*
	// intrinsics ("Java delegation", synth_machinery.go) with its own
	// helpers; real crypto/x509 has no parsePrivateKeyDescriptor or
	// hashFromSigAlg.
	"m8/crypto/x509.parsePrivateKeyDescriptor": "synthetic-only helper: no real crypto/x509.parsePrivateKeyDescriptor",
	"m8/crypto/x509.hashFromSigAlg":            "synthetic-only helper: no real crypto/x509.hashFromSigAlg",
	// The synthetic net package defines its own pipeConn type; real
	// net.Pipe returns the Conn interface with no concrete pipeConn type.
	"m9/net.(pipeConn).Read":  "synthetic-only type: no real net.pipeConn",
	"m9/net.(pipeConn).Write": "synthetic-only type: no real net.pipeConn",
	// The synthetic os package defines its own dirEntry type; real os has
	// dirEntry only on plan9/windows (not linux).
	"os/os.(dirEntry).Info": "synthetic-only type: no real os.dirEntry on linux",
}

// ---------------------------------------------------------------------------
// Test 1: multi-result function signatures vs real stdlib
// ---------------------------------------------------------------------------

// loadSyntheticPackage loads a synthetic package, converting
// buildSyntheticPackage's panic (parse/type error in the synthetic source)
// into an error. A synthetic package that does not type-check is itself a
// P0 — the desugar tables reference it and user imports resolve through it
// (diskimport.go) — so the oracle reports it loudly per entry instead of
// crashing the test binary.
func loadSyntheticPackage(path string) (sp *syntheticPackage, err error) {
	defer func() {
		if r := recover(); r != nil {
			sp, err = nil, fmt.Errorf("synthetic package %q does not type-check: %v", path, r)
		}
	}()
	sp = getSyntheticPackage(path)
	if sp == nil || sp.pkg == nil {
		return nil, fmt.Errorf("synthetic package %q not loaded", path)
	}
	return sp, nil
}

func TestSyntheticMultiResultOracle(t *testing.T) {
	goroot := oracleGOROOT(t)
	imp := newOracleImporter(goroot)
	for _, e := range collectOracleEntries() {
		t.Run(e.key(), func(t *testing.T) {
			if _, ok := syntheticSources[e.pkgPath]; !ok {
				t.Fatalf("no synthetic source registered for %q", e.pkgPath)
			}
			sp, err := loadSyntheticPackage(e.pkgPath)
			if err != nil {
				t.Fatalf("synthetic package load: %v", err)
			}
			synthFn, err := lookupSyntheticFunc(sp, e.name, e.recvType)
			if err != nil {
				t.Fatalf("synthetic lookup: %v", err)
			}
			synthSig, ok := synthFn.Type().(*types.Signature)
			if !ok {
				t.Fatalf("synthetic %s has non-signature type %v", e.key(), synthFn.Type())
			}
			realFn, realErr := lookupRealFunc(imp, e)
			if reason, ok := oracleSyntheticOnly[e.key()]; ok {
				// Synthetic-only by design: no real comparison applies.
				// Still attempt the real lookup: if the real declaration
				// now exists and matches, the exemption is stale and
				// should be removed.
				if realErr == nil {
					realSig, ok := realFn.Type().(*types.Signature)
					if !ok {
						t.Logf("exempt (synthetic-only): %s", reason)
					} else if msg := oracleSigMismatch(synthSig, realSig); msg == "" {
						t.Logf("note: allow-listed entry %s now matches real stdlib; consider removing it from oracleSyntheticOnly (%s)", e.key(), reason)
					} else {
						t.Logf("exempt (synthetic-only): %s; real differs as expected: %s", reason, msg)
					}
				} else {
					t.Logf("exempt (synthetic-only): %s", reason)
				}
				return
			}
			if realErr != nil {
				t.Fatalf("real-stdlib lookup for table entry %s failed: %v\ntype errors for %s: %v\n"+
					"(if this declaration is a deliberate synthetic invention, add it to oracleSyntheticOnly with its reason)",
					e.key(), realErr, e.pkgPath, imp.typeErrors(e.pkgPath))
			}
			realSig, ok := realFn.Type().(*types.Signature)
			if !ok {
				t.Fatalf("real %s has non-signature type %v", e.key(), realFn.Type())
			}
			if msg := oracleSigMismatch(synthSig, realSig); msg != "" {
				t.Errorf("signature drift vs go1.27.1 stdlib: %s\n  synthetic: %s\n  real:      %s",
					msg, oracleString(synthSig), oracleString(realSig))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 2: interface-dispatch pair structs vs real interface methods
// ---------------------------------------------------------------------------

// oracleIfaceEntry maps one m8IfaceMultiResultMethods entry to the real
// interface method it must wire-match. The synthetic pair struct's field
// types (the wire format for interface dispatch) must equal the real
// method's result types positionally.
type oracleIfaceEntry struct {
	synthPkg string // synthetic package holding the pair type (table's pkgPath)
	pairType string
	method   string
	realPkg  string // real package holding the interface ("" when skipped)
	iface    string // real interface type name ("" when skipped)
	skip     string // non-empty: documented reason to skip the comparison
}

var oracleIfaceEntries = []oracleIfaceEntry{
	{"io", "ioReadPair", "Read", "io", "Reader", ""},
	{"io", "ioWritePair", "Write", "io", "Writer", ""},
	{"io", "ioWritePair", "WriteString", "io", "StringWriter", ""},
	{"io", "ioByteReaderPair", "ReadByte", "io", "ByteReader", ""},
	{"io", "ioWriteToPair", "WriteTo", "io", "WriterTo", ""},
	{"io", "ioReadFromPair", "ReadFrom", "io", "ReaderFrom", ""},
	{"reflect", "reflectMethodPair", "MethodByName", "reflect", "Type", ""},
	{"reflect", "reflectStructFieldPair", "FieldByName", "reflect", "Type", ""},
	{"reflect", "reflectStructFieldPair", "FieldByNameFunc", "reflect", "Type", ""},
	{"hash", "ClonePair", "Clone", "hash", "Cloner", ""},
	{"hash", "binaryMarshalPair", "MarshalBinary", "encoding", "BinaryMarshaler", ""},
	{"hash", "binaryAppendPair", "AppendBinary", "encoding", "BinaryAppender", ""},
	{"crypto/elliptic", "__mrnistCurve_AddPair", "Add", "crypto/elliptic", "Curve", ""},
	{"crypto/elliptic", "__mrnistCurve_DoublePair", "Double", "crypto/elliptic", "Curve", ""},
	{"crypto/elliptic", "__mrnistCurve_ScalarMultPair", "ScalarMult", "crypto/elliptic", "Curve", ""},
	{"crypto/elliptic", "__mrnistCurve_ScalarBaseMultPair", "ScalarBaseMult", "crypto/elliptic", "Curve", ""},
	{"crypto/ecdh", "__mrnistCurve_GenerateKeyPair", "GenerateKey", "crypto/ecdh", "Curve", ""},
	{"crypto/ecdh", "__mrnistCurve_NewPrivateKeyPair", "NewPrivateKey", "crypto/ecdh", "Curve", ""},
	{"crypto/ecdh", "__mrnistCurve_NewPublicKeyPair", "NewPublicKey", "crypto/ecdh", "Curve", ""},
	{"crypto/ecdh", "__mrnistCurve_ecdhPair", "ecdh", "crypto/ecdh", "Curve", ""},
	// fips140/ecdsa Point[P] interface methods: the pair types deliberately
	// erase the generic point type to (any, ...) "for wire simplicity" (table
	// comment); the concrete per-curve signatures are pinned by the m8
	// entries. The real method must still exist (stale-skip detection).
	{"crypto/internal/fips140/ecdsa", "__mrPointIfaceSetBytesPair", "SetBytes", "crypto/internal/fips140/ecdsa", "Point", "deliberate (any,...) erasure; concrete sigs pinned by m8 entries"},
	{"crypto/internal/fips140/ecdsa", "__mrPointIfaceBytesXPair", "BytesX", "crypto/internal/fips140/ecdsa", "Point", "deliberate (any,...) erasure; concrete sigs pinned by m8 entries"},
	{"crypto/internal/fips140/ecdsa", "__mrPointIfaceScalarMultPair", "ScalarMult", "crypto/internal/fips140/ecdsa", "Point", "deliberate (any,...) erasure; concrete sigs pinned by m8 entries"},
	{"crypto/internal/fips140/ecdsa", "__mrPointIfaceScalarBaseMultPair", "ScalarBaseMult", "crypto/internal/fips140/ecdsa", "Point", "deliberate (any,...) erasure; concrete sigs pinned by m8 entries"},
	{"math/big", "fmtPrecisionPair", "Precision", "fmt", "State", ""},
	{"math/big", "fmtWidthPair", "Width", "fmt", "State", ""},
	// crypto/elliptic.unmarshaler is a real (unexported) stdlib interface.
	{"crypto/elliptic", "__mrUnmarshalPair", "Unmarshal", "crypto/elliptic", "unmarshaler", ""},
	{"crypto/elliptic", "__mrUnmarshalCompressedPair", "UnmarshalCompressed", "crypto/elliptic", "unmarshaler", ""},
	{"net", "acceptPair", "Accept", "net", "Listener", ""},
	{"net", "writePair", "WriteTo", "net", "PacketConn", ""},
	{"net", "readFromPair", "ReadFrom", "net", "PacketConn", ""},
	{"net/http", "__clientDoPair", "RoundTrip", "net/http", "RoundTripper", ""},
}

// lookupPairStruct finds the synthetic pair struct's fields.
func lookupPairStruct(sp *syntheticPackage, pairType string) (*types.Struct, error) {
	obj := sp.pkg.Scope().Lookup(pairType)
	if obj == nil {
		return nil, fmt.Errorf("synthetic pair type %s.%s missing from package scope", sp.path, pairType)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("synthetic %s.%s is %T, want *types.TypeName", sp.path, pairType, obj)
	}
	nt, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("synthetic %s.%s has type %T, want named", sp.path, pairType, tn.Type())
	}
	st, ok := nt.Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("synthetic %s.%s underlying is %T, want struct", sp.path, pairType, nt.Underlying())
	}
	return st, nil
}

// lookupIfaceMethod finds method on the (possibly unexported) interface type
// iface in the real package pkgPath.
func lookupIfaceMethod(imp *oracleImporter, pkgPath, iface, method string) (*types.Signature, error) {
	pkg, err := imp.Import(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("import real %q: %v", pkgPath, err)
	}
	obj := pkg.Scope().Lookup(iface)
	if obj == nil {
		return nil, fmt.Errorf("real %s: interface %q not in package scope", pkgPath, iface)
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("real %s: %q is %T, want *types.TypeName", pkgPath, iface, obj)
	}
	nt, ok := tn.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("real %s: %q has type %T, want named", pkgPath, iface, tn.Type())
	}
	var found *types.Func
	ms := types.NewMethodSet(nt)
	for i := 0; i < ms.Len(); i++ {
		if sel := ms.At(i); sel.Obj().Name() == method {
			fn, ok := sel.Obj().(*types.Func)
			if !ok {
				return nil, fmt.Errorf("real %s.%s(%s) is %T, want *types.Func", pkgPath, iface, method, sel.Obj())
			}
			found = fn
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("real %s: method %s(%s) not in method set", pkgPath, iface, method)
	}
	sig, ok := found.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("real %s.%s(%s) has non-signature type %v", pkgPath, iface, method, found.Type())
	}
	return sig, nil
}

func TestSyntheticIfacePairOracle(t *testing.T) {
	goroot := oracleGOROOT(t)
	imp := newOracleImporter(goroot)

	// Coverage: every m8IfaceMultiResultMethods entry must have a mapping
	// above, so a newly added entry cannot silently escape the oracle.
	mapped := make(map[string]bool, len(oracleIfaceEntries))
	for _, me := range oracleIfaceEntries {
		mapped[me.synthPkg+"/"+me.pairType+"/"+me.method] = true
	}
	for _, te := range m8IfaceMultiResultMethods {
		if k := te.pkgPath + "/" + te.pairType + "/" + te.method; !mapped[k] {
			t.Errorf("m8IfaceMultiResultMethods entry %s has no mapping in oracleIfaceEntries", k)
		}
	}

	for _, me := range oracleIfaceEntries {
		t.Run(me.synthPkg+"/"+me.pairType+"/"+me.method, func(t *testing.T) {
			if _, ok := syntheticSources[me.synthPkg]; !ok {
				t.Fatalf("no synthetic source registered for %q", me.synthPkg)
			}
			sp, err := loadSyntheticPackage(me.synthPkg)
			if err != nil {
				t.Fatalf("synthetic package load: %v", err)
			}
			st, err := lookupPairStruct(sp, me.pairType)
			if err != nil {
				t.Fatalf("synthetic pair lookup: %v", err)
			}
			sig, err := lookupIfaceMethod(imp, me.realPkg, me.iface, me.method)
			if err != nil {
				t.Fatalf("real interface method lookup: %v\ntype errors for %s: %v",
					err, me.realPkg, imp.typeErrors(me.realPkg))
			}
			if me.skip != "" {
				t.Logf("comparison skipped: %s", me.skip)
				return
			}
			if st.NumFields() != sig.Results().Len() {
				t.Fatalf("pair %s has %d fields, real %s.%s(%s) has %d results",
					me.pairType, st.NumFields(), me.realPkg, me.iface, me.method, sig.Results().Len())
			}
			for i := 0; i < st.NumFields(); i++ {
				f, r := st.Field(i), sig.Results().At(i).Type()
				if !oracleIdentical(f.Type(), r) {
					t.Errorf("pair %s field %d (%s) type %s != real result type %s",
						me.pairType, i, f.Name(), oracleString(f.Type()), oracleString(r))
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 3: negative control — the oracle must be able to fail
// ---------------------------------------------------------------------------

// oracleTypeParamSlice converts a *types.TypeParamList to the slice form
// types.NewSignatureType takes.
func oracleTypeParamSlice(tps *types.TypeParamList) []*types.TypeParam {
	if tps == nil {
		return nil
	}
	out := make([]*types.TypeParam, tps.Len())
	for i := range out {
		out[i] = tps.At(i)
	}
	return out
}

// oracleMutateResult returns a copy of sig with result idx replaced by nt.
func oracleMutateResult(sig *types.Signature, idx int, nt types.Type) *types.Signature {
	res := make([]*types.Var, sig.Results().Len())
	for i := range res {
		v := sig.Results().At(i)
		typ := v.Type()
		if i == idx {
			typ = nt
		}
		res[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), typ)
	}
	return types.NewSignatureType(sig.Recv(), oracleTypeParamSlice(sig.TypeParams()), nil,
		sig.Params(), types.NewTuple(res...), sig.Variadic())
}

// oracleDropParam returns a copy of sig with param idx removed.
func oracleDropParam(sig *types.Signature, idx int) *types.Signature {
	ps := make([]*types.Var, 0, sig.Params().Len()-1)
	for i := 0; i < sig.Params().Len(); i++ {
		if i != idx {
			ps = append(ps, sig.Params().At(i))
		}
	}
	return types.NewSignatureType(sig.Recv(), oracleTypeParamSlice(sig.TypeParams()), nil,
		types.NewTuple(ps...), sig.Results(), sig.Variadic())
}

// TestSyntheticOracle_NegativeControl proves the oracle is capable of
// failing: it replays the R2-P1-3 concrete uncaught mutation
// (strconv.Atoi's `int` result becoming `int64`) plus arity and parameter
// mutations through the same oracleSigMismatch the table tests use, and
// requires each to be detected. The positive control (unmutated Atoi)
// requires a match, guarding against a comparator that always mismatches.
func TestSyntheticOracle_NegativeControl(t *testing.T) {
	goroot := oracleGOROOT(t)
	imp := newOracleImporter(goroot)

	sp, err := loadSyntheticPackage("strconv")
	if err != nil {
		t.Fatalf("synthetic package load: %v", err)
	}
	synthFn, err := lookupSyntheticFunc(sp, "Atoi", "")
	if err != nil {
		t.Fatalf("synthetic lookup: %v", err)
	}
	realFn, err := lookupRealFunc(imp, oracleEntry{"m7", "strconv", "Atoi", ""})
	if err != nil {
		t.Fatalf("real lookup: %v", err)
	}
	synthSig := synthFn.Type().(*types.Signature)
	realSig := realFn.Type().(*types.Signature)

	// Positive control: the unmutated pair must match.
	if msg := oracleSigMismatch(synthSig, realSig); msg != "" {
		t.Fatalf("positive control failed (unmutated strconv.Atoi must match): %s", msg)
	}

	// Mutation 1 (the R2-P1-3 concrete example): `v int` -> `v int64` in the
	// multi-result signature. Must be detected.
	mut := oracleMutateResult(synthSig, 0, types.Typ[types.Int64])
	if msg := oracleSigMismatch(mut, realSig); msg == "" {
		t.Errorf("negative control FAILED: int->int64 result mutation not detected")
	} else {
		t.Logf("mutation 1 detected: %s", msg)
	}

	// Mutation 2: dropped parameter (arity). Must be detected.
	mut = oracleDropParam(synthSig, 0)
	if msg := oracleSigMismatch(mut, realSig); msg == "" {
		t.Errorf("negative control FAILED: dropped-param mutation not detected")
	} else {
		t.Logf("mutation 2 detected: %s", msg)
	}

	// Mutation 3: string param -> []byte param. Must be detected.
	ps := make([]*types.Var, synthSig.Params().Len())
	for i := range ps {
		v := synthSig.Params().At(i)
		typ := v.Type()
		if i == 0 {
			typ = types.NewSlice(types.Typ[types.Byte])
		}
		ps[i] = types.NewVar(v.Pos(), v.Pkg(), v.Name(), typ)
	}
	mut = types.NewSignatureType(synthSig.Recv(), oracleTypeParamSlice(synthSig.TypeParams()), nil,
		types.NewTuple(ps...), synthSig.Results(), synthSig.Variadic())
	if msg := oracleSigMismatch(mut, realSig); msg == "" {
		t.Errorf("negative control FAILED: param-type mutation not detected")
	} else {
		t.Logf("mutation 3 detected: %s", msg)
	}
}
