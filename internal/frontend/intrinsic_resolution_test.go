package main

// Build-time intrinsic / linkname resolution gate (RV1107-10, R-17/R-18).
//
// The frontend emits three kinds of call targets whose bodies never
// execute as Go, so a missing backend target is invisible until a
// program happens to call one — the recent red-master cause 215cd8e1
// was exactly this (unbound runtime linknames). This file is the
// build-time gate: it enumerates every emitted intrinsic and linkname
// and asserts each resolves to a registered backend target. It runs
// under `go test .` in internal/frontend, hence under `build.sh
// check`, `make check`, and CI (`make all`).
//
// What is checked (all from disk truth at test time, no hardcoded
// symbol lists except the two explicit classifications below):
//
//  1. Every __reflect_* name in reflectIntrinsicNames maps to a
//     `case "op"` in the backend ReflectOpNode, and has a matching
//     func definition in the synthetic reflect source.
//  2. Every intrinsic family (time, math, reflect, alias, abi)
//     lowers to a wire kind registered in P3KindRegistrations /
//     NodeFactory, and every name in each frontend intrinsic table
//     is covered by the test's kind map (a new intrinsic name with
//     no mapped kind fails here).
//  3. Every panic-only `func __*` helper in the embedded synthetic
//     sources is either an intercepted intrinsic or a classified
//     scope stub (scopeStubHelpers). A new panic-only __ helper
//     fails until classified; a stale classification fails too.
//  4. Every //go:linkname (and //go:linknamestd) directive in the
//     embedded synthetic sources resolves: Java-registered
//     (Linknames.register literal, pkg-loop expansion, or *.SYMBOL
//     constant) or provided by a Go function compiled under that
//     qualified name.
//  5. Every linkname symbol CALL-REACHABLE from the probe program's
//     entries (main + package inits, through call-fun and
//     method-value edges) resolves the same way. Symbols that are
//     reachable but unbound today are listed in knownUnboundLinknames
//     with the reason and the fix shape — the ratchet: any NEW
//     unbound symbol fails, and a listed symbol that becomes
//     resolvable must be removed from the list (stale entries fail).
//  6. The reflectop ops actually emitted by the probe program are
//     exactly the live set (8 ops). The other 82 __reflect_*
//     intrinsics are emitted only via exportReflectIntrinsic from
//     within the synthetic reflect package (reflect_m7), which
//     contributes no funcdecls to a disk-reflect program — they are
//     bound in the backend (check 1) but unreachable from public
//     API at HEAD. If the reachable set changes, this fails with
//     instructions to update the classification.
//
// Known limits (documented, not hidden):
//   - Call reachability is flow-insensitive at function granularity
//     and does not see through func values (a linkname func stored
//     in a variable and called later is not an edge) or constant
//     conditions (race.Enabled / GOOS == "aix" guards still count
//     as edges; the affected symbols are documented in
//     knownUnboundLinknames).
//   - A reference that is never a call (e.g. methodValueCall taken
//     as a FuncPCABI0 argument) is not a dispatch and is not an
//     edge; such symbols are excluded by the call-edge rule.
//   - Go-provider resolution is by qualified name only
//     (pkgpath.name, mirroring Linknames.bindGo); it proves a
//     provider exists in the compiled sources, not that a
//     particular program binds it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// gateRepoRoot returns the graalgo checkout root (the directory
// containing internal/frontend), derived from this file's location
// so the test works regardless of the test runner's cwd.
func gateRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is <root>/internal/frontend/intrinsic_resolution_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// gateReadFile reads path relative to the repo root, failing the test.
func gateReadFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(gateRepoRoot(t), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// gateJavaFiles returns all .java files under internal/backend.
func gateJavaFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	root := filepath.Join(gateRepoRoot(t), "internal", "backend")
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".java") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk backend java: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no backend .java files found")
	}
	return files
}

var (
	gateCaseRe       = regexp.MustCompile(`case "([^"]+)"`)
	gateKindRegRe    = regexp.MustCompile(`register(?:Expr|Stmt)Kind\("([^"]+)"`)
	gateRegisterRe   = regexp.MustCompile(`Linknames\.register\(\s*([^,]+?),`)
	gateRegisterLit  = regexp.MustCompile(`^"([^"]+)"$`)
	gateRegisterLoop = regexp.MustCompile(`^(\w+)\s*\+\s*"([^"]+)"$`)
	gateSymbolConst  = regexp.MustCompile(`^([A-Za-z0-9_.]+)\.SYMBOL$`)
	gateStringArrRe  = regexp.MustCompile(`for\s*\(\s*String\s+(\w+)\s*:\s*new String\[\]\{([^}]*)\}\s*\)`)
	gateStrLitRe     = regexp.MustCompile(`"([^"]+)"`)
)

// gateRegisteredLinknames returns the set of symbols with a Java
// intrinsic registration, expanding the ReflectLinknames pkg loop
// (`for (String pkg : new String[]{...})` + `pkg + ".suffix"`) and
// *.SYMBOL constants. Any other non-literal register expression
// fails the test (a new dynamic registration shape must be handled
// explicitly here, not silently skipped).
func gateRegisteredLinknames(t *testing.T) map[string]bool {
	t.Helper()
	registered := map[string]bool{}
	for _, f := range gateJavaFiles(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		text := string(src)
		// pkg-loop expansions declared in this file.
		loops := map[string][]string{}
		for _, m := range gateStringArrRe.FindAllStringSubmatch(text, -1) {
			var pkgs []string
			for _, s := range gateStrLitRe.FindAllStringSubmatch(m[2], -1) {
				pkgs = append(pkgs, s[1])
			}
			loops[m[1]] = pkgs
		}
		for _, m := range gateRegisterRe.FindAllStringSubmatch(text, -1) {
			arg := strings.TrimSpace(m[1])
			switch {
			case gateRegisterLit.MatchString(arg):
				registered[gateRegisterLit.FindStringSubmatch(arg)[1]] = true
			case gateRegisterLoop.MatchString(arg):
				parts := gateRegisterLoop.FindStringSubmatch(arg)
				pkgs, ok := loops[parts[1]]
				if !ok {
					t.Errorf("%s: Linknames.register uses loop var %q with no String-array loop in file",
						f, parts[1])
					continue
				}
				for _, p := range pkgs {
					registered[p+parts[2]] = true
				}
			case gateSymbolConst.MatchString(arg):
				class := gateSymbolConst.FindStringSubmatch(arg)[1]
				simple := class[strings.LastIndex(class, ".")+1:]
				sym := gateSymbolConstant(t, simple)
				if sym == "" {
					t.Errorf("%s: cannot resolve %s.SYMBOL", f, class)
					continue
				}
				registered[sym] = true
			default:
				t.Errorf("%s: unhandled Linknames.register form %q (extend gateRegisteredLinknames)",
					f, arg)
			}
		}
	}
	return registered
}

// gateSymbolConstant resolves `public static final String SYMBOL =
// "..."` for the Java class named simple (searched under
// internal/backend). Returns "" when not found.
func gateSymbolConstant(t *testing.T, simple string) string {
	t.Helper()
	for _, f := range gateJavaFiles(t) {
		if filepath.Base(f) != simple+".java" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if m := regexp.MustCompile(`SYMBOL\s*=\s*"([^"]+)"`).FindStringSubmatch(string(b)); m != nil {
			return m[1]
		}
	}
	return ""
}

// gateRegisteredKinds returns the wire kinds registered in
// P3KindRegistrations.java and NodeFactory.java.
func gateRegisteredKinds(t *testing.T) map[string]bool {
	t.Helper()
	kinds := map[string]bool{}
	for _, rel := range []string{
		"internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3KindRegistrations.java",
		"internal/backend/truffle/src/main/java/graalgo/frontend/NodeFactory.java",
	} {
		for _, m := range gateKindRegRe.FindAllStringSubmatch(gateReadFile(t, rel), -1) {
			kinds[m[1]] = true
		}
	}
	return kinds
}

// gateSyntheticFuncs parses every embedded synthetic source
// (syntheticSources map values) and returns, per package path, the
// parsed file.
func gateSyntheticFuncs(t *testing.T) map[string]*ast.File {
	t.Helper()
	files := map[string]*ast.File{}
	fset := token.NewFileSet()
	for path, src := range syntheticSources {
		// Mirror buildSyntheticPackage's parse input exactly: the
		// generator-damage repairs run before parsing (concatenated
		// gc sources scatter imports; raw strings may be damaged).
		src = restoreRawStrings(src)
		src = hoistSyntheticImports(src)
		f, err := parser.ParseFile(fset, path+".go", src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse synthetic source %q: %v", path, err)
		}
		files[path] = f
	}
	return files
}

// gateIsPanicOnly reports whether fd's body is exactly one
// `panic(...)` statement.
func gateIsPanicOnly(fd *ast.FuncDecl) bool {
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
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "panic"
}

// TestReflectIntrinsicOpsHaveBackendCases: every name in
// reflectIntrinsicNames must have a `case "op"` in ReflectOpNode
// (both the type and value switches) and a matching func
// definition in the synthetic reflect source.
func TestReflectIntrinsicOpsHaveBackendCases(t *testing.T) {
	java := gateReadFile(t, "internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectOpNode.java")
	cases := map[string]bool{}
	for _, m := range gateCaseRe.FindAllStringSubmatch(java, -1) {
		cases[m[1]] = true
	}
	if len(cases) == 0 {
		t.Fatal("no case strings found in ReflectOpNode.java")
	}
	// Func definitions in the synthetic reflect source.
	defs := map[string]bool{}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "reflect.go", syntheticReflectSource, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse syntheticReflectSource: %v", err)
	}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && strings.HasPrefix(fd.Name.Name, "__reflect_") {
			defs[fd.Name.Name] = true
		}
	}
	var missing []string
	var undef []string
	for name := range reflectIntrinsicNames {
		op := strings.TrimPrefix(name, "__reflect_")
		if !cases[op] {
			missing = append(missing, name+" (op "+op+")")
		}
		if !defs[name] {
			undef = append(undef, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(undef)
	if len(missing) > 0 {
		t.Errorf("reflect intrinsics with no ReflectOpNode case (%d):\n%s",
			len(missing), strings.Join(missing, "\n"))
	}
	if len(undef) > 0 {
		t.Errorf("reflect intrinsics with no func definition in syntheticReflectSource (%d):\n%s",
			len(undef), strings.Join(undef, "\n"))
	}
	// Definitions that are NOT intercepted intrinsics must be exactly
	// the documented real-Go helpers.
	var extra []string
	for name := range defs {
		if !reflectIntrinsicNames[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	wantExtra := []string{"__reflect_taglookup", "__reflect_visibleWalk"}
	if strings.Join(extra, ",") != strings.Join(wantExtra, ",") {
		t.Errorf("non-intrinsic __reflect_* definitions = %v; want exactly %v "+
			"(a new real-Go helper must be documented here, a new intrinsic must join reflectIntrinsicNames)",
			extra, wantExtra)
	}
	t.Logf("reflectIntrinsicNames: %d entries, %d backend cases, %d func defs",
		len(reflectIntrinsicNames), len(cases), len(defs))
}

// intrinsicKindMap maps each frontend intrinsic name to the wire
// kind its call lowers to. The lowering functions are named in the
// comment per family; a name missing from this map fails the
// coverage assertion in TestIntrinsicWireKindsRegistered.
var intrinsicKindMap = map[string]map[string]string{
	// timeIntrinsicNames -> exportTimeIntrinsic/timeIntrinsicNode
	// (synth_machinery.go); Timer/Ticker Stop/Reset -> "timeop"
	// via exportTimeOp.
	"time": {
		"Now": "timenow", "Sleep": "timesleep", "After": "timeafter",
		"Tick": "timetick", "NewTimer": "timenew", "NewTicker": "timenew",
		"AfterFunc": "timeafterfunc",
	},
	// mathIntrinsicNames -> exportMathIntrinsic (synth_machinery.go).
	"math": {
		"Float64bits": "mathf64bits", "Float64frombits": "mathf64frombits",
		"Float32bits": "mathf32bits", "Float32frombits": "mathf32frombits",
		"Sqrt": "mathsqrt", "FMA": "mathfma",
	},
	// aliasIntrinsicNames -> exportAliasIntrinsic (synth_machinery.go).
	"alias": {
		"AnyOverlap": "sliceoverlap", "InexactOverlap": "sliceoverlap",
	},
	// abiFuncPCIntrinsicNames -> exportAbiFuncPCIntrinsic
	// (synth_machinery.go).
	"abi": {
		"FuncPCABI0": "abifuncpc", "FuncPCABIInternal": "abifuncpc",
	},
}

// TestIntrinsicWireKindsRegistered: every intrinsic family's wire
// kinds must be registered in the backend, and every name in each
// frontend intrinsic table must be covered by intrinsicKindMap
// (reflect uses the single "reflectop" kind for all 90 names).
func TestIntrinsicWireKindsRegistered(t *testing.T) {
	registered := gateRegisteredKinds(t)
	tables := map[string]map[string]bool{
		"time":  timeIntrinsicNames,
		"math":  mathIntrinsicNames,
		"alias": aliasIntrinsicNames,
		"abi":   abiFuncPCIntrinsicNames,
	}
	for family, table := range tables {
		kinds, ok := intrinsicKindMap[family]
		if !ok {
			t.Errorf("no kind map for intrinsic family %q", family)
			continue
		}
		for name := range table {
			kind, ok := kinds[name]
			if !ok {
				t.Errorf("%s intrinsic %q has no mapped wire kind in intrinsicKindMap "+
					"(add it with its lowering function)", family, name)
				continue
			}
			if !registered[kind] {
				t.Errorf("%s intrinsic %q lowers to wire kind %q, which is not registered "+
					"in P3KindRegistrations/NodeFactory", family, name, kind)
			}
		}
	}
	// reflect: all 90 names lower to the single "reflectop" kind.
	if !registered["reflectop"] {
		t.Error(`reflect intrinsics lower to wire kind "reflectop", which is not registered`)
	}
	// timeop: Timer/Ticker Stop/Reset methods lower to the "timeop"
	// node; TimeOpNode must handle both ops.
	if !registered["timeop"] {
		t.Error(`time Timer/Ticker methods lower to wire kind "timeop", which is not registered`)
	}
	timeOp := gateReadFile(t, "internal/backend/interop/src/main/java/graalgo/p3time/nodes/TimeOpNode.java")
	for _, op := range []string{"Stop", "Reset"} {
		if !strings.Contains(timeOp, `case "`+op+`"`) {
			t.Errorf("TimeOpNode.java has no case %q (time.%s.%s has no backend target)", op, "Timer/Ticker", op)
		}
	}
}

// scopeStubHelpers classifies panic-only `func __*` helpers that are
// NOT intercepted intrinsics: Appendix-M single-result helpers for
// multi-result fail-fast public APIs, whose bodies panic honestly
// with a "not in scope" message. Keyed by synthetic package path and
// helper name. A helper that gains a real body drops out of the
// panic-only set and its entry goes stale (failing the test until
// removed); a new panic-only helper fails until classified here.
var scopeStubHelpers = map[string]string{
	// crypto/internal/boring: boringcrypto is not available under
	// graalgo; every entry point fails fast with that message.
	"crypto/internal/boring.__mrDecryptRSANoPadding":      "boringcrypto: not available",
	"crypto/internal/boring.__mrDecryptRSAOAEP":           "boringcrypto: not available",
	"crypto/internal/boring.__mrDecryptRSAPKCS1":          "boringcrypto: not available",
	"crypto/internal/boring.__mrECDH":                     "boringcrypto: not available",
	"crypto/internal/boring.__mrEncryptRSANoPadding":      "boringcrypto: not available",
	"crypto/internal/boring.__mrEncryptRSAOAEP":           "boringcrypto: not available",
	"crypto/internal/boring.__mrEncryptRSAPKCS1":          "boringcrypto: not available",
	"crypto/internal/boring.__mrGenerateKeyECDH":          "boringcrypto: not available",
	"crypto/internal/boring.__mrGenerateKeyECDSA":         "boringcrypto: not available",
	"crypto/internal/boring.__mrGenerateKeyRSA":           "boringcrypto: not available",
	"crypto/internal/boring.__mrNewAESCipher":             "boringcrypto: not available",
	"crypto/internal/boring.__mrNewGCMTLS":                "boringcrypto: not available",
	"crypto/internal/boring.__mrNewGCMTLS13":              "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPrivateKeyECDH":        "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPrivateKeyECDSA":       "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPrivateKeyRSA":         "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPublicKeyECDH":         "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPublicKeyECDSA":        "boringcrypto: not available",
	"crypto/internal/boring.__mrNewPublicKeyRSA":          "boringcrypto: not available",
	"crypto/internal/boring.__mrPrivateKeyECDH_PublicKey": "boringcrypto: not available",
	"crypto/internal/boring.__mrSignMarshalECDSA":         "boringcrypto: not available",
	"crypto/internal/boring.__mrSignRSAPKCS1v15":          "boringcrypto: not available",
	"crypto/internal/boring.__mrSignRSAPSS":               "boringcrypto: not available",
	"crypto/internal/boring.__mrrandReader_Read":          "boringcrypto: not available",
	// crypto/ecdsa, crypto/rsa: same boringcrypto fail-fast.
	"crypto/ecdsa.__mrboringPrivateKey": "boringcrypto: not available",
	"crypto/ecdsa.__mrboringPublicKey":  "boringcrypto: not available",
	"crypto/rsa.__mrboringPrivateKey":   "boringcrypto: not available",
	"crypto/rsa.__mrboringPublicKey":    "boringcrypto: not available",
	// io/fs: Appendix-M helpers for multi-result fail-fast members
	// ("not in M8 scope (multi-result interface dispatch needs WS D)"
	// or "unreachable (Appendix-M desugar did not fire)").
	"io/fs.__fsGlobWithLimit": "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsLstat":         "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsReadDir":       "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsReadFile":      "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsReadLink":      "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsStat":          "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__fsSub":           "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__subFSGlob":       "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__subFSOpen":       "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__subFSReadDir":    "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__subFSReadFile":   "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	"io/fs.__subFSSub":        "io/fs Appendix-M helper: not in M8 scope / WS D desugar pending",
	// math/big: Appendix-M helper for the byteReader ReadByte
	// multi-result member ("Scan not supported in frontend/v1
	// scope (WS D)").
	"math/big.__mrbyteReader_ReadByte": "math/big Appendix-M helper: Scan not in frontend/v1 scope (WS D)",
	// os: Appendix-M helpers for multi-result fail-fast members
	// (OpenRoot, Executable, StartProcess, Process.Wait,
	// File.SyscallConn — "not in M8 scope").
	"os.__executable":      "os Appendix-M helper: not in M8 scope",
	"os.__fileSyscallConn": "os Appendix-M helper: not in M8 scope",
	"os.__openRoot":        "os Appendix-M helper: not in M8 scope",
	"os.__processWait":     "os Appendix-M helper: not in M8 scope",
	"os.__startProcess":    "os Appendix-M helper: not in M8 scope",
	// os File.ReadFrom/WriteTo: named trap until the mrM8 desugar
	// covers the D4 streaming implementation (P0-2).
	"os.__fileReadFrom": "os Appendix-M helper: named trap until mrM8 desugar covers it (P0-2)",
	"os.__fileWriteTo":  "os Appendix-M helper: named trap until mrM8 desugar covers it (P0-2)",
	// syscall: Appendix-M helpers for multi-result fail-fast members
	// (I/O wrappers, Syscall/RawSyscall, sockets, epoll/inotify,
	// Mmap, ForkExec/Wait4 — "not in scope" per the synthetic
	// syscall package header).
	"syscall.__accept":          "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__epollcreate":     "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__epollcreate1":    "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__epollwait":       "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__forkexec":        "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__getsockoptint":   "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__inotifyaddwatch": "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__inotifyinit":     "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__inotifyrmwatch":  "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__mmap":            "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__open":            "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__pread":           "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__pwrite":          "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__rawsyscall":      "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__rawsyscall6":     "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__read":            "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__readlink":        "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__recvmsg":         "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__seek":            "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__socket":          "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__startprocess":    "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__syscall":         "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__syscall6":        "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__wait4":           "syscall Appendix-M helper: fail-fast, not in scope",
	"syscall.__write":           "syscall Appendix-M helper: fail-fast, not in scope",
}

// TestPanicOnlyDoubleUnderscoreHelpersClassified: every panic-only
// `func __*` in the embedded synthetic sources must be an
// intercepted intrinsic or a classified scope stub (exact set:
// stale entries fail too).
func TestPanicOnlyDoubleUnderscoreHelpersClassified(t *testing.T) {
	found := map[string]bool{}
	intercepted := 0
	for path, f := range gateSyntheticFuncs(t) {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(fd.Name.Name, "__") || !gateIsPanicOnly(fd) {
				continue
			}
			key := path + "." + fd.Name.Name
			found[key] = true
			if reflectIntrinsicNames[fd.Name.Name] {
				intercepted++
				continue
			}
			if _, ok := scopeStubHelpers[key]; !ok {
				t.Errorf("panic-only __ helper %s is neither an intercepted intrinsic "+
					"nor a classified scope stub (add it to scopeStubHelpers with its reason, "+
					"or intercept it with a backend target)", key)
			}
		}
	}
	for key := range scopeStubHelpers {
		if !found[key] {
			t.Errorf("scopeStubHelpers entry %q is stale (no panic-only __ helper with that name "+
				"in the synthetic sources anymore — remove it)", key)
		}
	}
	t.Logf("panic-only __ helpers: %d total (%d intercepted reflect intrinsics, %d scope stubs)",
		len(found), intercepted, len(found)-intercepted)
}

// gateLinknameDirectives returns the linkname symbols declared in
// one synthetic source, keyed by symbol, with the declaring func
// name as the value. Mirrors the exporter's rules: a //go:linkname
// directive wins; otherwise a //go:linknamestd NAME line yields
// pkgPath + "." + NAME (mr_disk.go mrLinknameSymbol); a 1-field
// //go:linkname defaults to pkgPath + "." + local.
func gateLinknameDirectives(t *testing.T, pkgPath string, f *ast.File) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Doc == nil {
			continue
		}
		var linkname, linknamestd string
		for _, c := range fd.Doc.List {
			text := strings.TrimSpace(c.Text)
			if strings.HasPrefix(text, "//go:linkname ") {
				fields := strings.Fields(text[len("//go:linkname "):])
				if len(fields) != 1 && len(fields) != 2 {
					t.Errorf("%s.%s: //go:linkname must have one or two fields, got %q",
						pkgPath, fd.Name.Name, text)
					continue
				}
				if fields[0] != fd.Name.Name {
					t.Errorf("%s.%s: //go:linkname local name %q does not match func %s",
						pkgPath, fd.Name.Name, fields[0], fd.Name.Name)
					continue
				}
				if len(fields) == 2 {
					linkname = fields[1]
				} else {
					linkname = pkgPath + "." + fields[0]
				}
			}
			if strings.HasPrefix(text, "//go:linknamestd ") {
				fields := strings.Fields(text[len("//go:linknamestd "):])
				if len(fields) != 1 || fields[0] != fd.Name.Name {
					t.Errorf("%s.%s: //go:linknamestd must name the func itself, got %q",
						pkgPath, fd.Name.Name, text)
					continue
				}
				linknamestd = pkgPath + "." + fields[0]
			}
		}
		sym := linkname
		if sym == "" {
			sym = linknamestd
		}
		if sym != "" {
			out[sym] = fd.Name.Name
		}
	}
	return out
}

// gateGoProviders returns the qualified names of Go functions with
// bodies in the embedded synthetic sources (pkgPath + "." + name),
// mirroring Linknames.bindGo (which binds every compiled program
// function under pkgPath.name, skipping linkname stubs).
func gateGoProviders(t *testing.T) map[string]bool {
	t.Helper()
	providers := map[string]bool{}
	for path, f := range gateSyntheticFuncs(t) {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			providers[path+"."+fd.Name.Name] = true
		}
	}
	return providers
}

// TestLinknameDirectivesResolve: every //go:linkname /
// //go:linknamestd directive in the embedded synthetic sources must
// resolve to a Java-registered intrinsic or a Go provider.
func TestLinknameDirectivesResolve(t *testing.T) {
	registered := gateRegisteredLinknames(t)
	providers := gateGoProviders(t)
	var unresolved []string
	emitted, viaProvider := 0, 0
	for path, f := range gateSyntheticFuncs(t) {
		for sym, fn := range gateLinknameDirectives(t, path, f) {
			emitted++
			if registered[sym] {
				continue
			}
			if providers[sym] {
				viaProvider++
				continue
			}
			unresolved = append(unresolved, fmt.Sprintf("%s (declared by %s.%s)", sym, path, fn))
		}
	}
	sort.Strings(unresolved)
	if len(unresolved) > 0 {
		t.Errorf("synthetic //go:linkname directives with no backend target (%d):\n%s\n"+
			"Register the symbol in the backend (Linknames.register) or provide a Go function "+
			"under that qualified name; a linkname that resolves to nothing fails at call time.",
			len(unresolved), strings.Join(unresolved, "\n"))
	}
	t.Logf("synthetic linkname directives: %d emitted (%d via Go provider, %d Java-registered symbols known)",
		emitted, viaProvider, len(registered))
}

// knownUnboundLinknames is the ratchet for linkname symbols that
// are CALL-REACHABLE from the probe program but have no backend
// target today. Each entry names the reason and the fix shape.
// Any reachable symbol NOT listed here fails the gate; a listed
// symbol that stops being reachable (or gains a target) is stale
// and fails until removed. This list must only shrink, and only by
// registering the target or providing the Go function — never by
// narrowing the probe.
//
// Evidence (all verified at HEAD 2d5a1dfa from the probe export's
// call graph and the GOROOT sources): Value.MapIndex calls
// mapaccess/mapaccess_faststr unconditionally (reflect/map.go);
// MapKeys/MapRange reach maps.Init via mapIterStart, and Init calls
// rand() unconditionally (internal/runtime/maps/table.go);
// SetMapIndex reaches mapassign*/mapdelete*; MapOf calls typehash;
// Value.Elem reaches verifyNotInHeapPtr on not-in-heap pointers.
//
// Shrink (fix-rv1107-10-mapaccess): the mapaccess family
// (reflect.mapaccess, mapaccess_faststr, mapassign0,
// mapassign_faststr0, mapdelete, mapdelete_faststr, typehash) and
// internal/runtime/maps.rand are now bound to real backend targets
// (ReflectLinknames + MapsView), so their entries are removed here.
// The 5 remaining entries are the separate small items, untouched.
var knownUnboundLinknames = map[string]string{
	// Called by maps.Next on concurrent map iteration+write (gc
	// panics). Fix: register a fatal-throwing target mirroring
	// sync.fatal / internal/sync.fatal.
	"internal/runtime/maps.fatal": "error path only (concurrent map iteration+write)",
	// Called only under race.Enabled / msan.Enabled guards.
	// Fix: register a 0-returning stub (graalgo has no PCs; the
	// guarded paths are never taken).
	"internal/runtime/sys.GetCallerPC": "race/msan-guarded only",
	// Called only when runtime.GOOS == "aix" (reflect/type.go).
	// Fix: register the identity function (gc's non-AIX behavior).
	"reflect.adjustAIXGCDataForRuntime": "AIX-only (GOOS guard)",
	// Called by Value.Elem / UnsafePointer on not-in-heap
	// pointers. No Java target; needs a backend decision (graalgo
	// has no Go heap — the faithful value is likely always true).
	"reflect.verifyNotInHeapPtr": "Value.Elem/UnsafePointer not-in-heap path",
	// Called only under race.Enabled (sync/pool.go:105).
	// Fix: register fastrandn (runtime.fastrandn precedent).
	"runtime.randn": "race-guarded only (sync.Pool)",
}

// liveReflectOps are the reflectop ops actually emitted by the
// probe program: the directly-intercepted public APIs
// (Value.Call/CallSlice/Interface/IsValid, Type.Method/
// MethodByName/NumMethod on interfaces, MakeFunc). The other 82
// __reflect_* intrinsics are emitted only via exportReflectIntrinsic
// from within the synthetic reflect package (reflect_m7), which
// contributes no funcdecls to a disk-reflect program.
var liveReflectOps = map[string]bool{
	"call": true, "callslice": true, "interface": true, "isvalid": true,
	"makefuncval": true, "typeMethod": true, "typeMethodByName": true,
	"typeNumMethod": true,
}

// gateProbeSrc is the probe program for the exported checks: it
// exercises the public reflect API surface (type + value ops, map
// ops, MakeFunc, Select, Swapper), fmt over maps (fmtsort/MapIter),
// sync.Pool, and the time/math intrinsics, so the export's call
// graph covers the linkname surface a real program reaches.
const gateProbeSrc = `package main

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"
)

type gateS struct {
	X int
	Y string ` + "`json:\"y\"`" + `
}

func (s gateS) Greet() string { return s.Y }
func (s *gateS) SetX(v int)   { s.X = v }

func main() {
	// --- reflect: type surface ---
	s := gateS{X: 3, Y: "hi"}
	t := reflect.TypeOf(s)
	_ = t.Kind()
	_ = t.String()
	_ = t.Name()
	_ = t.PkgPath()
	_ = t.Size()
	_ = t.Align()
	_ = t.FieldAlign()
	_ = t.Bits()
	_ = t.Len()
	_ = t.NumField()
	_ = t.NumMethod()
	_ = t.NumIn()
	_ = t.NumOut()
	_ = t.IsVariadic()
	_ = t.Comparable()
	_ = t.Field(0)
	_ = t.Method(0)
	_ = t.Elem()
	_, _ = t.FieldByName("X")
	_, _ = t.MethodByName("Greet")
	_ = t.Implements(reflect.TypeOf((*fmt.Stringer)(nil)).Elem())
	_ = t.AssignableTo(reflect.TypeOf(s))
	_ = t.ConvertibleTo(reflect.TypeOf("x"))
	_ = reflect.PointerTo(t)
	_ = reflect.ArrayOf(2, reflect.TypeOf(0))
	_ = reflect.SliceOf(reflect.TypeOf(0))
	_ = reflect.MapOf(reflect.TypeOf(""), reflect.TypeOf(0))
	_ = reflect.ChanOf(reflect.BothDir, reflect.TypeOf(0))
	_ = reflect.FuncOf([]reflect.Type{reflect.TypeOf(0)}, []reflect.Type{reflect.TypeOf(0)}, false)
	_ = reflect.StructOf([]reflect.StructField{{Name: "A", Type: reflect.TypeOf(0)}})
	_ = reflect.TypeOf([3]int{}).Len()
	_ = reflect.TypeOf(make(chan int)).ChanDir()
	_ = reflect.TypeOf(map[string]int{}).Key()
	_ = reflect.TypeOf(func(int) string { return "" }).In(0)
	_ = reflect.TypeOf(func(int) string { return "" }).Out(0)

	// --- reflect: value surface ---
	v := reflect.ValueOf(&s).Elem()
	_ = v.Field(0)
	_ = v.Index(0)
	_ = v.Elem()
	_ = v.Len()
	_ = v.Cap()
	_ = v.IsNil()
	_ = v.IsZero()
	_ = v.Int()
	_ = v.Uint()
	_ = v.Float()
	_ = v.Complex()
	_ = v.Bool()
	_ = v.String()
	_ = v.Bytes()
	_ = v.Interface()
	_ = v.Convert(reflect.TypeOf(0))
	_ = v.Slice(0, 1)
	_ = v.Slice3(0, 1, 1)
	_ = v.Method(0)
	_ = v.MethodByName("Greet")
	_ = reflect.ValueOf(1)
	_ = reflect.New(t)
	_ = reflect.NewAt(t, nil)
	_ = reflect.Append(reflect.ValueOf([]int{}), reflect.ValueOf(1))
	_ = reflect.MakeSlice(reflect.TypeOf([]int{}), 1, 2)
	_ = reflect.MakeMap(reflect.TypeOf(map[string]int{}))
	_ = reflect.MakeChan(reflect.TypeOf(make(chan int)), 1)
	_ = reflect.MakeFunc(reflect.TypeOf(func(int) int { return 0 }), func(args []reflect.Value) []reflect.Value { return args })
	_ = reflect.Swapper([]int{1, 2})
	_, _, _ = reflect.Select([]reflect.SelectCase{{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(make(chan int))}})
	nv := reflect.New(t).Elem()
	nv.SetInt(7)
	nv.SetUint(8)
	nv.SetFloat(1.5)
	nv.SetComplex(1 + 2i)
	nv.SetBool(true)
	nv.SetString("s")
	nv.SetBytes([]byte("b"))
	nv.Set(reflect.ValueOf(s))
	nv.SetZero()
	nv.SetLen(0)
	nv.SetCap(0)
	nv.Grow(1)
	nv.Clear()
	mv := reflect.ValueOf(map[string]int{"a": 1})
	_ = mv.MapIndex(reflect.ValueOf("a"))
	_ = mv.MapKeys()
	mv.SetMapIndex(reflect.ValueOf("b"), reflect.ValueOf(2))
	it := mv.MapRange()
	for it.Next() {
		_ = it.Key()
		_ = it.Value()
	}
	ch := reflect.ValueOf(make(chan int, 1))
	ch.Send(reflect.ValueOf(5))
	_, _ = ch.Recv()
	_ = ch.TrySend(reflect.ValueOf(6))
	ch.Close()
	_ = reflect.ValueOf(func() {}).Call(nil)
	_ = reflect.ValueOf(func(a ...int) {}).CallSlice([]reflect.Value{reflect.ValueOf(1)})

	// --- fmt over maps/slices/structs (fmtsort, MapIter, Pool paths) ---
	m := map[string]int{"b": 2, "a": 1}
	fmt.Println(m)
	fmt.Println([]int{1, 2}, s, t)
	var p sync.Pool
	p.Put(42)
	_ = p.Get()

	// --- time + math intrinsics ---
	_ = time.Now()
	time.Sleep(time.Millisecond)
	_ = time.After(time.Millisecond)
	_ = time.Tick(time.Millisecond)
	_ = time.NewTimer(time.Millisecond)
	_ = time.NewTicker(time.Millisecond)
	_ = math.Float64bits(1.5)
	_ = math.Float64frombits(0)
	_ = math.Float32bits(1)
	_ = math.Float32frombits(0)
	_ = math.Sqrt(4)
	_ = math.FMA(1, 2, 3)
	_ = time.Since(time.Now())
	_ = math.Abs(-1)
}
`

// gateFuncDecl is one funcdecl entry of a decoded export.
type gateFuncDecl struct {
	obj      int
	pkgPath  string
	name     string
	linkname string
	hasBody  bool
	body     any
}

// gateExportFuncs indexes the export's funcdecls by obj id.
func gateExportFuncs(t *testing.T, doc map[string]any) map[int]*gateFuncDecl {
	t.Helper()
	funcs, ok := doc["funcs"].([]any)
	if !ok {
		t.Fatal("export has no funcs array")
	}
	byObj := map[int]*gateFuncDecl{}
	for _, f := range funcs {
		fm, ok := f.(map[string]any)
		if !ok || fm["kind"] != "funcdecl" {
			continue
		}
		fields, ok := fm["fields"].(map[string]any)
		if !ok {
			continue
		}
		objF, ok := fields["obj"].(float64)
		if !ok {
			continue
		}
		d := &gateFuncDecl{obj: int(objF)}
		d.pkgPath, _ = fields["pkgpath"].(string)
		d.name, _ = fields["name"].(string)
		d.linkname, _ = fields["linkname"].(string)
		if body, ok := fields["body"]; ok {
			d.hasBody = true
			d.body = body
		}
		byObj[d.obj] = d
	}
	return byObj
}

// gateCallTargets collects the funcdecl obj ids a node tree may
// invoke: "call" nodes' fun targets plus "methodvalue"/"methodref"
// values (which may be invoked later). Bare references (e.g. a
// func passed to FuncPCABI0) are not edges.
func gateCallTargets(n any, acc map[int]bool) {
	switch v := n.(type) {
	case map[string]any:
		kind, _ := v["kind"].(string)
		fields, _ := v["fields"].(map[string]any)
		switch kind {
		case "call":
			if fun, ok := fields["fun"].(map[string]any); ok {
				if ff, ok := fun["fields"].(map[string]any); ok {
					if obj, ok := ff["obj"].(float64); ok {
						acc[int(obj)] = true
					}
				}
			}
		case "methodvalue", "methodref":
			if obj, ok := fields["obj"].(float64); ok {
				acc[int(obj)] = true
			}
		}
		for _, c := range v {
			gateCallTargets(c, acc)
		}
	case []any:
		for _, c := range v {
			gateCallTargets(c, acc)
		}
	}
}

// gateReachable returns the obj ids reachable from main + package
// inits through call edges.
func gateReachable(byObj map[int]*gateFuncDecl) map[int]bool {
	seen := map[int]bool{}
	var stack []int
	for obj, d := range byObj {
		if d.name == "main" || d.name == "init" {
			seen[obj] = true
			stack = append(stack, obj)
		}
	}
	for len(stack) > 0 {
		obj := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		d := byObj[obj]
		if d == nil || !d.hasBody {
			continue
		}
		targets := map[int]bool{}
		gateCallTargets(d.body, targets)
		for tgt := range targets {
			if _, ok := byObj[tgt]; !ok || seen[tgt] {
				continue
			}
			seen[tgt] = true
			stack = append(stack, tgt)
		}
	}
	return seen
}

// gateReflectOps collects every reflectop "op" in the export.
func gateReflectOps(doc map[string]any) map[string]bool {
	ops := map[string]bool{}
	var walk func(n any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "reflectop" {
				if f, ok := v["fields"].(map[string]any); ok {
					if op, ok := f["op"].(string); ok {
						ops[op] = true
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(doc)
	return ops
}

// TestExportedResolutionGate exports the probe program once and
// runs the export-level checks as subtests: reflectop liveness
// (per-op) and call-reachable linkname resolution (ratchet).
func TestExportedResolutionGate(t *testing.T) {
	out, err := ExportFile(writeSyncProg(t, gateProbeSrc))
	if err != nil {
		t.Fatalf("ExportFile(probe): %v", err)
	}
	doc := decodeExport(t, out)
	byObj := gateExportFuncs(t, doc)

	t.Run("reflectops", func(t *testing.T) {
		ops := gateReflectOps(doc)
		for op := range liveReflectOps {
			op := op
			t.Run(op, func(t *testing.T) {
				if !ops[op] {
					t.Errorf("live reflectop %q was not emitted by the probe program "+
						"(the public API behind it stopped lowering to reflectop — "+
						"check the interception in export.go)", op)
				}
			})
		}
		var extra []string
		for op := range ops {
			if !liveReflectOps[op] {
				extra = append(extra, op)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			t.Errorf("newly emitted reflectop ops (not in liveReflectOps): %v — "+
				"a new intrinsic became reachable from public API; classify it in "+
				"liveReflectOps (with its backend case already covered by "+
				"TestReflectIntrinsicOpsHaveBackendCases) or fix the interception", extra)
		}
		t.Logf("reflectop ops emitted by probe: %d (live set size %d)", len(ops), len(liveReflectOps))
	})

	t.Run("linknames", func(t *testing.T) {
		registered := gateRegisteredLinknames(t)
		providers := map[string]bool{}
		for _, d := range byObj {
			if d.hasBody && d.linkname == "" {
				providers[d.pkgPath+"."+d.name] = true
			}
		}
		reachable := gateReachable(byObj)
		unbound := map[string]string{} // symbol -> declaring func
		reached := 0
		for obj := range reachable {
			d := byObj[obj]
			if d == nil || d.linkname == "" {
				continue
			}
			reached++
			if registered[d.linkname] || providers[d.linkname] {
				continue
			}
			unbound[d.linkname] = d.pkgPath + "." + d.name
		}
		var unlisted []string
		for sym, decl := range unbound {
			if _, ok := knownUnboundLinknames[sym]; !ok {
				unlisted = append(unlisted, fmt.Sprintf("%s (declared by %s)", sym, decl))
			}
		}
		sort.Strings(unlisted)
		if len(unlisted) > 0 {
			t.Errorf("call-reachable linkname symbols with no backend target (%d):\n%s\n"+
				"Register the symbol in the backend or provide a Go function under that "+
				"qualified name. If the symbol is knowingly unbound for now, add it to "+
				"knownUnboundLinknames with its reason and fix shape — never by narrowing "+
				"the probe.", len(unlisted), strings.Join(unlisted, "\n"))
		}
		var stale []string
		for sym := range knownUnboundLinknames {
			if _, ok := unbound[sym]; !ok {
				stale = append(stale, sym)
			}
		}
		sort.Strings(stale)
		if len(stale) > 0 {
			t.Errorf("knownUnboundLinknames entries no longer call-reachable/unbound (%d): %v — "+
				"the target was registered (or the probe stopped reaching it); remove them "+
				"from the list.", len(stale), stale)
		}
		t.Logf("call-reachable linkname funcdecls: %d (%d reachable funcdecls, %d known-unbound)",
			reached, len(reachable), len(knownUnboundLinknames))
	})
}
