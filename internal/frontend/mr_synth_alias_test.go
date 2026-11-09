package main

// R10-03 (+ R10-P2-02): synthetic package-level MR func-var aliases.
//
// buildSyntheticPackage used to discard the alias map
// etaExpandMRFuncValues returns while removing the matching
// `var x = pkg.F` declarations, so a synthetic package's package-level
// MR func-var alias vanished with no record: call sites through the var
// could not desugar as the aliased Func. The disk path
// (buildDiskPackage, e.g. path/filepath's `var lstat = os.Lstat`) wired
// the map; the synthetic path did not.
//
//  1. TestMRFuncValueSyntheticAliasMap pins the wiring: a synthetic
//     package containing `var MyLstat = os.Lstat` yields a
//     varFuncAliases entry MyLstat -> os.Lstat, and the declaration is
//     gone from the AST.
//  2. TestMRFuncValueSyntheticAliasExport pins the consumption: a user
//     program calling through the synthetic package's wrapper exports
//     successfully, with the call desugared to the os table's __lstat
//     pair helper instead of failing with an M5/WS-D scope error.
//
// Both tests inject a synthetic source under a path no other test uses
// and remove it (plus the cached package) when done.

import (
	"go/ast"
	"go/types"
	"testing"
)

const mrSynthAliasPath = "example.com/mralias"

const mrSynthAliasSource = `package mralias

import "os"

var MyLstat = os.Lstat

func CallIt(name string) (os.FileInfo, error) {
	return MyLstat(name)
}

// CallItAssign exercises the same alias in assignment position
// (fi, err := MyLstat(name)), which desugars via exportMultiResultDesugar
// rather than the return-position hoist.
func CallItAssign(name string) (os.FileInfo, error) {
	fi, err := MyLstat(name)
	return fi, err
}
`

// injectMRTestSyntheticSource registers the test synthetic package;
// the returned cleanup removes it and drops the cached build.
func injectMRTestSyntheticSource(t *testing.T) func() {
	t.Helper()
	if _, dup := syntheticSources[mrSynthAliasPath]; dup {
		t.Fatalf("test synthetic source %q already registered", mrSynthAliasPath)
	}
	syntheticSources[mrSynthAliasPath] = mrSynthAliasSource
	return func() {
		synthMu.Lock()
		delete(syntheticSources, mrSynthAliasPath)
		delete(synthCache, mrSynthAliasPath)
		synthMu.Unlock()
	}
}

func TestMRFuncValueSyntheticAliasMap(t *testing.T) {
	defer injectMRTestSyntheticSource(t)()
	sp := buildSyntheticPackage(mrSynthAliasPath)
	if len(sp.varFuncAliases) != 1 {
		t.Fatalf("varFuncAliases has %d entries, want 1", len(sp.varFuncAliases))
	}
	obj := sp.pkg.Scope().Lookup("MyLstat")
	v, ok := obj.(*types.Var)
	if !ok {
		t.Fatalf("MyLstat in package scope is %T, want *types.Var", obj)
	}
	fn, ok := sp.varFuncAliases[v]
	if !ok {
		t.Fatal("varFuncAliases has no entry for MyLstat")
	}
	if fn.Pkg() == nil || fn.Pkg().Path() != "os" || fn.Name() != "Lstat" {
		t.Fatalf("MyLstat aliases %v, want os.Lstat", fn)
	}
	// The declaration was removed from the AST (removePkgSpecs=true):
	// no ValueSpec anywhere in the package still declares MyLstat.
	for _, f := range sp.files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, n := range vs.Names {
					if n.Name == "MyLstat" {
						t.Fatal("MyLstat ValueSpec still present in synthetic AST")
					}
				}
			}
		}
	}
}

func TestMRFuncValueSyntheticAliasExport(t *testing.T) {
	defer injectMRTestSyntheticSource(t)()
	doc := exportFuncValueProg(t, `package main

import "example.com/mralias"

func main() {
	_, _ = mralias.CallIt("x")
	_, _ = mralias.CallItAssign("x")
}
`)
	// The call through the alias desugars to the os table's __lstat
	// pair helper: some call node in the doc has fun ident __lstat,
	// and the helper itself is emitted as a funcdecl.
	foundCall := false
	for _, n := range findAllKind(doc, "call") {
		fun, _ := n["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] == "__lstat" {
			foundCall = true
		}
	}
	if !foundCall {
		t.Fatal("no call to __lstat in wire doc (alias call did not desugar to the os table helper)")
	}
	if !funcNames(doc)["__lstat"] {
		t.Fatal("no __lstat funcdecl in wire doc")
	}
	// The alias var itself is not emitted as a global: its declaration
	// was removed and the alias map is the only record of it.
	globals, _ := doc["globals"].([]any)
	objects, _ := doc["objects"].([]any)
	for _, g := range globals {
		gm, _ := g.(map[string]any)
		oidF, _ := gm["obj"].(float64)
		oid := int(oidF)
		if oid < 0 || oid >= len(objects) {
			continue
		}
		om, _ := objects[oid].(map[string]any)
		if om["name"] == "MyLstat" {
			t.Fatal("MyLstat emitted as a global despite its declaration being removed")
		}
	}
}
