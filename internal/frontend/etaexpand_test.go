package main

// L06 (eta-expand-fix): a `var x = pkg.F` declaration where F is a
// multi-result function must be KEPT, not removed. The general PKG-035
// value-position mechanism lowers the initializer to the pair-returning
// __-helper (exportSelector via mrPlanForFuncValue) and links call sites
// through the variable at its definition (userMRVars). An earlier
// revision of etaExpandMRFuncValues recorded a Var->Func alias, REMOVED
// the declaration, and left the ident→selector rewrite as a no-op stub
// (`_ = pkgIdent`) while discarding the alias maps on the main-package
// and synthetic-package paths — so every use of the variable was
// orphaned: the main-package case failed export with `variable "lstat"
// used before declaration`, and the PKG-035 tests
// (TestMRFuncValueSyntheticAccept, TestMRFuncValueFailFastKept) failed
// because their declarations vanished.
//
//  1. TestEtaExpandKeepsPackageVarAlias pins the reported construct:
//     `var lstat = os.Lstat` in the main package plus a call through
//     the var. The global is kept with a `__lstat` initializer, and the
//     call desugars through a `__p5mr_` pair temp.
//  2. TestEtaExpandDiskPackageVarAlias pins the disk path (the one
//     working path before the fix): the real path/filepath's
//     `var lstat = os.Lstat` is kept with a `__lstat` initializer, and
//     the `lstat(filename)` call inside Glob desugars.

import (
	"strings"
	"testing"
)

// etaExpandExport exports src, failing the test on export error, and
// returns the decoded wire doc.
func etaExpandExport(t *testing.T, src string) map[string]any {
	t.Helper()
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	return decodeExport(t, out)
}

// etaExpandGlobalInitByName returns the init node of the package-level
// global whose object-table name matches.
func etaExpandGlobalInitByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	objects, _ := doc["objects"].([]any)
	globals, _ := doc["globals"].([]any)
	for _, g := range globals {
		gm, _ := g.(map[string]any)
		oidF, _ := gm["obj"].(float64)
		oid := int(oidF)
		if oid < 0 || oid >= len(objects) {
			continue
		}
		om, _ := objects[oid].(map[string]any)
		if om["name"] == name {
			init, _ := gm["init"].(map[string]any)
			if init == nil {
				t.Fatalf("global %q has no init", name)
			}
			return init
		}
	}
	t.Fatalf("no global named %q", name)
	return nil
}

// etaExpandPairTempCallFun returns the fun node of the first desugared
// __p5mr_N pair-temp call, or nil when no such temp exists.
func etaExpandPairTempCallFun(doc map[string]any) map[string]any {
	return etaExpandPairTempCallFunWithName(doc, "")
}

// etaExpandPairTempCallFunWithName returns the fun node of the desugared
// __p5mr_N pair-temp call whose fun is an ident with the given name, or
// nil when no such temp exists. Empty wantName matches any pair temp.
func etaExpandPairTempCallFunWithName(doc map[string]any, wantName string) map[string]any {
	for _, n := range findAllKind(doc, "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if !strings.HasPrefix(name, "__p5mr_") {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if wantName != "" {
			if got, _ := fun["fields"].(map[string]any)["name"].(string); got != wantName {
				continue
			}
		}
		return fun
	}
	return nil
}

func TestEtaExpandKeepsPackageVarAlias(t *testing.T) {
	doc := etaExpandExport(t, `package main

import "os"

var lstat = os.Lstat

func main() {
	fi, err := lstat("main.go")
	println(fi == nil, err == nil)
}
`)
	// The declaration is kept: the global's initializer is the
	// pair-returning __lstat helper (PKG-035 value-position lowering).
	init := etaExpandGlobalInitByName(t, doc, "lstat")
	if init["kind"] != "ident" {
		t.Fatalf("lstat initializer kind = %q, want ident", init["kind"])
	}
	if name := init["fields"].(map[string]any)["name"]; name != "__lstat" {
		t.Fatalf("lstat initializer ident name = %v, want __lstat", name)
	}
	// The call through the var desugars via the userMRVars link: a
	// __p5mr_ temp holding a call through the lstat variable.
	fun := etaExpandPairTempCallFun(doc)
	if fun == nil {
		t.Fatal("no desugared __p5mr_ pair-temp vardecl found")
	}
	if fun["kind"] != "ident" {
		t.Fatalf("pair-temp call fun kind = %q, want ident", fun["kind"])
	}
	if name := fun["fields"].(map[string]any)["name"]; name != "lstat" {
		t.Fatalf("pair-temp call fun name = %v, want lstat", name)
	}
}

func TestEtaExpandDiskPackageVarAlias(t *testing.T) {
	doc := etaExpandExport(t, `package main

import "path/filepath"

func main() {
	ms, _ := filepath.Glob("*.go")
	println(len(ms))
}
`)
	// The disk path/filepath's `var lstat = os.Lstat` is kept and its
	// initializer lowered to the pair-returning __lstat helper.
	found := false
	for _, g := range doc["globals"].([]any) {
		gm, _ := g.(map[string]any)
		init, _ := gm["init"].(map[string]any)
		if init == nil || init["kind"] != "ident" {
			continue
		}
		if name := init["fields"].(map[string]any)["name"]; name == "__lstat" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no global with __lstat initializer found (disk path/filepath lstat)")
	}
	// The lstat(filename) call *inside path/filepath's Glob* (the
	// in-disk-package call site) desugared to a pair temp whose fun is
	// the lstat variable ident. Matching any __p5mr_ temp would be weak:
	// main's own filepath.Glob call also produces one, so this pins the
	// disk-package desugar specifically.
	fun := etaExpandPairTempCallFunWithName(doc, "lstat")
	if fun == nil {
		t.Fatal("no desugared __p5mr_ pair-temp vardecl with fun ident lstat found (disk path)")
	}
}
