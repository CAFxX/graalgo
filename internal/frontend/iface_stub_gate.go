package main

// P1-1 (review-2026-11-04): compile-time rejection of interface dispatch
// to stub-only synthetic methods.
//
// The bug: a multi-result interface call (e.g. `w.Write(p)` on an
// io.Writer) was accepted by the frontend even when the concrete method
// behind the dispatch is a fail-fast panic stub. The call node was
// classified (whitelist or general user-interface pair synthesis) and the
// method table wired the method to the stub funcdecl, so the accepted
// program could only panic (or fail to load) at run time. Direct calls
// to the same out-of-scope methods fail loud at compile time; the
// interface path must agree.
//
// What counts as stub-only (verified against the wire, not the panic
// count): a synthetic method whose body is a single panic statement AND
// which has no pair-returning __-helper (multiResultHelperFunc). A
// single-panic body alone is NOT the criterion: most streaming methods
// (base64 encoder.Write, gzip/zlib Reader.Read/Writer.Write, tar
// Reader.Read/Writer.Write, zip fileWriter.Write, multipart Part.Read,
// hash digest Write/MarshalBinary/AppendBinary, tls Conn.Read/Write)
// carry a panic body but are table-mapped to a real __-helper, and their
// interface dispatch works. Gating on panic bodies alone would regress
// all of those. The helper check is what separates the genuinely
// stub-only methods (encoding/base32 encoder.Write/decoder.Read,
// io SectionReader.Read, teeReader.Read, multiReader.Read,
// multiWriter.Write, OffsetWriter.Write) from the helper-wired ones.
//
// Reachability (the over-approximation guard): the gate fires only when
// the stub type can actually flow into user code — i.e. user code
// references a function whose result is the stub type itself, or an
// interface the stub type implements (e.g. base32.NewEncoder returns
// io.WriteCloser, which *encoder implements; io.TeeReader returns
// io.Reader, which teeReader implements). A program that merely imports
// a stub package (e.g. base32 for EncodeToString) but never obtains a
// stub value keeps compiling, and its io.Writer.Write calls on real
// implementers (bytes.Buffer) are unaffected. Residual over-approximation:
// a program that constructs a stub value anywhere AND calls the same
// interface method on a different, real implementer elsewhere is still
// rejected, because the frontend has no per-variable flow analysis.
// That residue is documented here so the follow-up (flow-sensitive
// implementer tracking, WS D work) knows exactly what to narrow.
//
// Scope of the gate: multi-result interface calls only (the pair path).
// Single-result stub dispatch (e.g. encoder.Close via io.Closer) has the
// same shape but a much wider blast radius (Close is ubiquitous); it is
// a known residual gap, not silently fixed here. The gate applies to
// user-code call sites only (e.pkg == e.userPkg): synthetic bodies are
// exempt, because they legitimately call interface methods on parameters
// whose dynamic types the exporter cannot know, and their internal calls
// must not be rejected for stubs in packages they happen to import.
// Library-mediated dispatch (e.g. io.Copy(enc, src), whose Write call
// lives in the synthetic io body) is likewise a residual gap: the call
// site is synthetic, so the gate cannot see the stub flowing through it.

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// ifaceStubMethod is one stub-only synthetic method: a fail-fast panic
// body with no pair-returning helper, so interface dispatch to it cannot
// work. named is the stub type's *types.Named (nil when the receiver is
// not a named type, in which case the stub can never be reached and is
// dropped at registry build time).
type ifaceStubMethod struct {
	pkgPath  string
	recvType string
	method   string
	panicMsg string
	named    *types.Named
	// ptrRecv is true when the method's receiver is a pointer: only *T
	// (not T) carries the method in its method set.
	ptrRecv bool
}

// ifaceReachability is the set of value sources user code references:
// concrete types returned by referenced functions, and interface types
// returned by referenced functions. Built once per export from the
// user package's types.Info (complete before export starts, so it is
// independent of body-export order).
type ifaceReachability struct {
	concrete map[string]bool
	ifaces   []types.Type
}

// isSinglePanicBody reports whether fd's body is exactly one panic
// statement (any message). This generalizes isFailFastStub (mr_disk.go),
// whose "not in M* scope" message pattern misses other stub spellings
// (e.g. "multi-result call needs the mrM8 desugar", "unreachable
// (Appendix-M desugar did not fire)"). A single panic is the codebase's
// fail-fast stub convention; real method bodies are never a lone panic.
func isSinglePanicBody(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return false
	}
	stmt, ok := fd.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	return ok && fn.Name == "panic"
}

// stubPanicMsg extracts the panic message literal of a single-panic body,
// or "" when the panic argument is not a string literal.
func stubPanicMsg(fd *ast.FuncDecl) string {
	if fd.Body == nil || len(fd.Body.List) != 1 {
		return ""
	}
	stmt, ok := fd.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return ""
	}
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	msg, err := strconv.Unquote(lit.Value)
	if err != nil {
		return lit.Value
	}
	return msg
}

// stubSigKey keys a method by name + parameter/result types. Types are
// printed with full package paths so identically-named types from
// different packages never collide. Parameter names are irrelevant
// (p vs b vs d) and are not part of the key.
func stubSigKey(mname string, sig *types.Signature) string {
	var b strings.Builder
	b.WriteString(mname)
	b.WriteString("\x00")
	qual := func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Path()
	}
	for i := 0; i < sig.Params().Len(); i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(types.TypeString(sig.Params().At(i).Type(), qual))
	}
	b.WriteString("\x00")
	for i := 0; i < sig.Results().Len(); i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(types.TypeString(sig.Results().At(i).Type(), qual))
	}
	return b.String()
}

// ifaceStubFingerprint returns a fingerprint of the imported-package sets
// the registry depends on. The registry is rebuilt whenever it changes,
// so lazily-imported packages (and their helpers) are never missed and
// helper lookups never go stale.
func (e *exporter) ifaceStubFingerprint() string {
	synth := make([]string, 0, len(e.importedSynthetic))
	for p := range e.importedSynthetic {
		synth = append(synth, p)
	}
	sort.Strings(synth)
	tree := make([]string, 0, len(e.importedPkgTree))
	for p := range e.importedPkgTree {
		tree = append(tree, p)
	}
	sort.Strings(tree)
	return strings.Join(synth, ",") + "|" + strings.Join(tree, ",")
}

// refreshIfaceStubRegistry rebuilds the stub-only registry when the
// imported-package fingerprint changed since the last build.
func (e *exporter) refreshIfaceStubRegistry() {
	fp := e.ifaceStubFingerprint()
	if e.ifaceStubMethods != nil && fp == e.ifaceStubFP {
		return
	}
	reg := make(map[string][]*ifaceStubMethod)
	for path := range e.importedSynthetic {
		// importedSynthetic holds disk-built packages too (WS G);
		// importedPackage resolves both kinds. getSyntheticPackage
		// alone would panic on disk paths (e.g. internal/goarch).
		sp, err := e.importedPackage(path)
		if err != nil || sp == nil || sp.info == nil {
			continue
		}
		for _, file := range sp.files {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Recv == nil || !isSinglePanicBody(fd) {
					continue
				}
				obj, ok := sp.info.Defs[fd.Name].(*types.Func)
				if !ok {
					continue
				}
				sig, ok := obj.Type().(*types.Signature)
				if !ok || sig.Recv() == nil || sig.Results().Len() < 2 {
					continue
				}
				if mentionsTypeParam(sig) {
					continue
				}
				// Helper-mapped methods work through interface
				// dispatch even though their declared body panics
				// (the desugar routes call sites to the helper).
				// Only methods with no helper are stub-only. Two
				// independent helper predicates, both required:
				// multiResultHelperFunc (the MR table/disk helper
				// lookup) and hasStaticMREntry (the M5/M7/io/os
				// static tables with dedicated exporter machinery —
				// e.g. context.Deadline, whose mrDeadline path
				// lowers through __deadlineOf instead of dispatching
				// to the stub; TestDeadlineCallableP4 pins it live).
				// The import pass uses hasStaticMREntry the same way
				// (table entries skip funcByObj registration), so a
				// table entry is helper-mapped by construction.
				if _, ok := e.multiResultHelperFunc(obj); ok {
					continue
				}
				rt := sig.Recv().Type()
				ptrRecv := false
				if pt, ok := rt.(*types.Pointer); ok {
					rt = pt.Elem()
					ptrRecv = true
				}
				nt, ok := rt.(*types.Named)
				if !ok || nt.Obj() == nil || nt.Obj().Pkg() == nil {
					continue
				}
				if nt.Origin() != nt {
					continue
				}
				if tp := nt.TypeParams(); tp != nil && tp.Len() > 0 {
					continue
				}
				if hasStaticMREntry(sp.path, obj.Name(), nt.Obj().Name()) {
					continue
				}
				stub := &ifaceStubMethod{
					pkgPath:  sp.path,
					recvType: nt.Obj().Name(),
					method:   obj.Name(),
					panicMsg: stubPanicMsg(fd),
					named:    nt,
					ptrRecv:  ptrRecv,
				}
				key := stubSigKey(obj.Name(), sig)
				reg[key] = append(reg[key], stub)
			}
		}
	}
	e.ifaceStubMethods = reg
	e.ifaceStubFP = fp
}

// ifaceReachabilityOf builds (once per export) the value-source set from
// the user package's types.Info: every function the user code references
// contributes its result types. Concrete results name their type
// directly; interface results are recorded for the Implements check.
func (e *exporter) ifaceReachabilityOf() *ifaceReachability {
	if e.ifaceReach != nil {
		return e.ifaceReach
	}
	r := &ifaceReachability{concrete: make(map[string]bool)}
	if e.info != nil {
		for _, obj := range e.info.Uses {
			fn, ok := obj.(*types.Func)
			if !ok {
				continue
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok {
				continue
			}
			for i := 0; i < sig.Results().Len(); i++ {
				rt := sig.Results().At(i).Type()
				if mentionsTypeParam(rt) {
					continue
				}
				// Interface results first: a named interface (e.g.
				// io.WriteCloser, the result of base32.NewEncoder)
				// is also a *types.Named, so derefNamed alone would
				// misfile it as a concrete type and the Implements
				// check would never see it.
				if _, ok := rt.Underlying().(*types.Interface); ok {
					r.ifaces = append(r.ifaces, rt)
					continue
				}
				if nt := derefNamed(rt); nt != nil && nt.Obj() != nil && nt.Obj().Pkg() != nil {
					r.concrete[nt.Obj().Pkg().Path()+"."+nt.Obj().Name()] = true
				}
			}
		}
	}
	e.ifaceReach = r
	return r
}

// stubImplements reports whether the stub type (T or *T, per receiver)
// implements iface. Both forms are tried: a value-receiver method is in
// both method sets, a pointer-receiver method only in *T's.
func stubImplements(stub *ifaceStubMethod, iface *types.Interface) bool {
	if stub.named == nil {
		return false
	}
	if types.Implements(stub.named, iface) {
		return true
	}
	return types.Implements(types.NewPointer(stub.named), iface)
}

// unwrapIface returns the *types.Interface underlying t, or nil.
func unwrapIface(t types.Type) *types.Interface {
	t = types.Unalias(t)
	if nt, ok := t.(*types.Named); ok {
		t = nt.Underlying()
	}
	iface, ok := t.(*types.Interface)
	if !ok {
		return nil
	}
	return iface
}

// ifaceStubsFor returns the reachable stub-only synthetic methods
// matching the interface call (method name + parameter/result types),
// or nil. It returns nil for non-user-code call sites (synthetic/disk
// bodies), for single-result calls, and for generic signatures it
// cannot match reliably. The caller turns a non-empty result into a
// scope error.
//
// All reachable matches are returned, not just the first: several stub
// types can share one method+signature (e.g. io.SectionReader.Read,
// io.teeReader.Read, io.multiReader.Read all match Read([]byte)
// (int, error)), and interface dispatch cannot tell which one a given
// call site holds. Naming only the first would misattribute the call
// (a TeeReader probe reported as SectionReader). The error therefore
// lists the whole reachable stub set the call could dispatch to.
func (e *exporter) ifaceStubsFor(mname string, sig *types.Signature, iface types.Type) []*ifaceStubMethod {
	if sig == nil || sig.Results().Len() < 2 {
		return nil
	}
	// User-code call sites only: synthetic bodies swap e.pkg to the
	// synthetic package (and restore it after), so e.pkg == e.userPkg
	// exactly during user package export. A nil userPkg (defensive: only
	// ExportFiles constructs exporters, and it always sets it) falls
	// through to gating rather than silently skipping.
	if e.userPkg != nil && e.pkg != e.userPkg {
		return nil
	}
	if mentionsTypeParam(sig) {
		return nil
	}
	ifaceType := unwrapIface(iface)
	if ifaceType == nil {
		return nil
	}
	e.refreshIfaceStubRegistry()
	stubs := e.ifaceStubMethods[stubSigKey(mname, sig)]
	if len(stubs) == 0 {
		return nil
	}
	reach := e.ifaceReachabilityOf()
	var out []*ifaceStubMethod
	for _, stub := range stubs {
		// The stub type must actually implement the called interface:
		// a signature match alone does not imply method-set inclusion.
		if !stubImplements(stub, ifaceType) {
			continue
		}
		// And it must be reachable from user code: returned directly
		// as a concrete type, or through an interface it implements.
		if reach.concrete[stub.pkgPath+"."+stub.recvType] {
			out = append(out, stub)
			continue
		}
		for _, rif := range reach.ifaces {
			if rifIface := unwrapIface(rif); rifIface != nil && stubImplements(stub, rifIface) {
				out = append(out, stub)
				break
			}
		}
	}
	return out
}

// ifaceStubError formats the P1-1 scope error for a reachable stub set:
// the interface method, every stub it could dispatch to, and the first
// stub's panic literal (representative; all listed stubs carry the same
// fail-fast shape).
func ifaceStubError(mname string, stubs []*ifaceStubMethod) error {
	names := make([]string, 0, len(stubs))
	for _, s := range stubs {
		names = append(names, s.pkgPath+"."+s.recvType+"."+s.method)
	}
	return scopeErrorf("interface method %s is not supported: stub-only synthetic method(s) in the implementer set (%s); interface dispatch to a stub-only synthetic method cannot work (P1-1: re-test when a real implementation lands; first stub's panic: %s)",
		mname, strings.Join(names, ", "), stubs[0].panicMsg)
}
