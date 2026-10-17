package main

import (
	"fmt"
	"go/types"
	"sort"
)

// RV-P1-02 (option (b)): assembly-backed functions are rejected at compile
// time, at the reference site — never as a runtime panic on a compilable
// program.
//
// A bodyless function declaration (fd.Body == nil) is classified by
// classifyBodyless into exactly one of:
//   - linkname: it carries //go:linkname, or the GOROOT runtime provides it
//     via a provider-side //go:linkname directive (generated table
//     runtimeProvidedLinknames). The declaration exports on the linkname
//     path; calls dispatch to the backend intrinsic (or a compiled Go
//     provider) by symbol name.
//   - unimplemented: true assembly (or otherwise bodyless) with no provider.
//     The declaration keeps its type identity in the frontend but emits no
//     wire funcdecl (exportLinknameDecl returns (nil, nil)); any reference —
//     call, function value, go/defer — fails at export via
//     checkBodylessReference with [RV-P1-02].
//
// The classification is computed from the declaration alone, identically at
// the declaration site and at every reference site, so the two can never
// disagree regardless of export order.

// classifyBodyless returns the linkname symbol for a bodyless declaration:
// the //go:linkname directive's symbol, the runtime-provider table's symbol,
// or "" when the function is truly unimplemented. Single source of truth
// for exportLinknameDecl and checkBodylessReference.
func classifyBodyless(fi *funcDeclInfo, pkgPath string) (string, error) {
	symbol, err := linknameDirective(fi.fd, pkgPath)
	if err != nil {
		return "", err
	}
	if symbol == "" {
		symbol = bodylessProviderSymbol(fi, pkgPath)
	}
	return symbol, nil
}

// bodylessProviderSymbol returns the linkname symbol for a bodyless function
// provided by the GOROOT runtime package (generated table
// runtimeProvidedLinknames, a mechanical scrape of the runtime's
// provider-side //go:linkname directives), or "" if it has no runtime
// provider. These are runtime services, not assembly. Methods are excluded:
// provider directives name package-level functions.
func bodylessProviderSymbol(fi *funcDeclInfo, pkgPath string) string {
	if fi == nil || fi.obj == nil || fi.recv != nil || pkgPath == "runtime" {
		return ""
	}
	if sym, ok := runtimeProvidedLinknames[pkgPath+"."+fi.obj.Name()]; ok {
		return sym
	}
	return ""
}

// bodylessDisplayName formats the "pkgpath.Func" (or "pkgpath.Recv.Method")
// identity used in [RV-P1-02] diagnostics.
func bodylessDisplayName(fi *funcDeclInfo, pkgPath string) string {
	name := ""
	if fi.obj != nil {
		name = fi.obj.Name()
	}
	if fi.recv != nil {
		if rt := fi.recv.Type(); rt != nil {
			if pt, ok := rt.(*types.Pointer); ok {
				rt = pt.Elem()
			}
			if nt, ok := rt.(*types.Named); ok && nt.Obj() != nil {
				name = nt.Obj().Name() + "." + name
			}
		}
	}
	if pkgPath == "" {
		return name
	}
	return pkgPath + "." + name
}

// checkBodylessReference implements RV-P1-02 reference-site rejection. fi
// is the callee's declaration (already resolved by the caller). A bodyless
// declaration with no backend implementation fails here, at export time:
//   - no linkname symbol: the function is unimplemented;
//   - linkname symbol with no registered backend intrinsic and no compiled
//     Go provider: the call could never dispatch.
//
// Bodied declarations (and anything that is not a bodyless FuncDecl) pass
// through. Lowering interceptors (atomics, time/math/reflect intrinsics,
// ...) run before this check at each reference site, so functions they
// implement are never rejected.
func (e *exporter) checkBodylessReference(fi *funcDeclInfo) error {
	if fi == nil || fi.fd == nil || fi.fd.Body != nil {
		return nil
	}
	pkgPath := funcPkgPath(e, fi)
	symbol, err := classifyBodyless(fi, pkgPath)
	if err != nil {
		return err
	}
	name := bodylessDisplayName(fi, pkgPath)
	if symbol == "" {
		return fmt.Errorf("gofrontend: cannot compile reference to %s: "+
			"assembly function not implemented in graalgo [RV-P1-02]", name)
	}

	if linknameIntrinsicSymbols[symbol] {
		return nil
	}
	if e.linknameGoProviderKnown(symbol) {
		return nil
	}
	return fmt.Errorf("gofrontend: cannot compile reference to %s: "+
		"linkname symbol %q has no registered backend intrinsic and no "+
		"compiled Go provider [RV-P1-02]", name, symbol)
}

// demandBody records a demand for fi's body under RV-P1-02 narrowing
// (Mechanism A: demand-driven body emission for imported packages). The
// exportSyntheticBodies drain emits exactly the demanded bodies, so
// checkBodylessReference only ever fires for references the compiled
// program contains.
//
// It is a no-op for:
//   - user-package functions (fi.synthPkg == nil): emitted wholesale;
//   - generic templates: emitted only via monomorphized instantiations;
//   - already-demanded or already-emitted functions.
//
// Bodyless functions with a linkname symbol ARE demanded: they have no
// body to emit, but their DECLARATION must be emitted (exportFuncDecl
// routes them to exportLinknameDecl, and the backend builds the
// LinknameRootNode from the funcdecl). Truly unimplemented bodyless
// functions never reach here — the reference-site check fails first —
// and would be skipped by exportFuncDecl anyway.
func (e *exporter) demandBody(fi *funcDeclInfo) {
	if fi == nil || fi.synthPkg == nil || fi.isTemplate {
		return
	}
	if e.demanded[fi] || e.emittedBodies[fi] {
		return
	}
	if e.demanded == nil {
		e.demanded = make(map[*funcDeclInfo]bool)
	}
	e.demanded[fi] = true
}

// demandIfaceCandidates implements the §4.5 conservative rule: a method
// referenced through an interface value demands the bodies of ALL
// candidate implementations registered in methodIndex under that method
// name. Dynamic dispatch targets are not statically knowable, so this is
// a deliberate over-approximation. Every candidate passes through
// checkBodylessReference: a dynamically reachable unimplemented function
// is "knowable at compile time" as a candidate and is rejected rather
// than deferred to a runtime panic (§5). Candidates are visited in
// deterministic (funcID, package path, name) order so rejections are
// reproducible.
func (e *exporter) demandIfaceCandidates(mname string) error {
	var cands []*funcDeclInfo
	for key, fi := range e.methodIndex {
		if key.name != mname || fi == nil {
			continue
		}
		cands = append(cands, fi)
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].funcID != cands[j].funcID {
			return cands[i].funcID < cands[j].funcID
		}
		pi, pj := funcPkgPath(e, cands[i]), funcPkgPath(e, cands[j])
		if pi != pj {
			return pi < pj
		}
		ni, nj := "", ""
		if cands[i].obj != nil {
			ni = cands[i].obj.Name()
		}
		if cands[j].obj != nil {
			nj = cands[j].obj.Name()
		}
		return ni < nj
	})
	for _, fi := range cands {
		if err := e.checkBodylessReference(fi); err != nil {
			return err
		}
		e.demandBody(fi)
	}
	return nil
}

// linknameGoProviderKnown reports whether a compiled Go function is known
// under symbol ("pkgpath.name"). This is the compile-time approximation of
// the backend's bindGo namespace: the launcher binds every compiled program
// function under its qualified Go name, and Linknames.dispatch falls back
// to it when no Java intrinsic is registered. Bodyless declarations are
// excluded — linkname stubs are consumers, never providers (cf. bindGoFuncs
// in LinknameIntrinsics.java, which skips funcdecls carrying "linkname").
func (e *exporter) linknameGoProviderKnown(symbol string) bool {
	if e.synthFuncIDs != nil {
		if fi, ok := e.synthFuncIDs[symbol]; ok && fi != nil &&
			(fi.fd == nil || fi.fd.Body != nil) {
			return true
		}
	}
	return e.compiledGoSymbols()[symbol]
}

// compiledGoSymbols lazily builds the qualified-name set of bodied,
// non-linkname functions known to this export. Rebuilt when funcByObj or
// synthFuncIDs grows (imported packages join during body export).
func (e *exporter) compiledGoSymbols() map[string]bool {
	if e.goSyms != nil && e.goSymsNFunc == len(e.funcByObj) &&
		e.goSymsNSynth == len(e.synthFuncIDs) {
		return e.goSyms
	}
	m := make(map[string]bool)
	for _, fi := range e.funcByObj {
		if fi == nil || fi.obj == nil || fi.obj.Pkg() == nil ||
			fi.fd == nil || fi.fd.Body == nil {
			continue
		}
		m[fi.obj.Pkg().Path()+"."+fi.obj.Name()] = true
	}
	e.goSyms = m
	e.goSymsNFunc = len(e.funcByObj)
	e.goSymsNSynth = len(e.synthFuncIDs)
	return m
}
