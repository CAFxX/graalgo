package main

// H2 lane (independent codebase review 2026-11-01): embedded synthetic
// packages must not silently drop package-level multi-result func-var
// aliases.
//
// etaExpandMRFuncValues with removePkgSpecs=true DELETES the
// package-level `var x = pkg.F` declaration from the AST and records the
// alias x->F in its returned maps. That deletion is safe only when the
// caller stores the maps (buildDiskPackage does); buildSyntheticPackage
// used to discard both maps, so the declaration vanished with no alias
// surviving, and call sites through x had nothing to desugar against.
// These tests pin the invariant at both levels: the alias map stored on
// the built synthetic package, and the end-to-end desugar of a call
// through the alias to the aliased function's pair helper.

import (
	"encoding/json"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// h2TestSyntheticPath is a test-only synthetic-source key: it is not a
// real stdlib path, and no shipped synthetic source uses it. It is
// injected into syntheticSources for the duration of each test and
// removed afterwards (including from the synthCache, so no stale entry
// survives the test).
const h2TestSyntheticPath = "h2mralias"

const h2TestSyntheticSource = `package h2mralias

import "os"

// myStat aliases a multi-result function (os.Stat). With
// removePkgSpecs=true this declaration is removed from the AST and the
// alias recorded; the alias survives only if the builder stores it.
var myStat = os.Stat

// CallStat calls through the alias; its pair desugar must resolve
// through the stored alias to os.Stat's table entry (__stat).
func CallStat(name string) (os.FileInfo, error) {
	return myStat(name)
}
`

func injectH2TestSynthetic(t *testing.T) {
	t.Helper()
	synthMu.Lock()
	delete(synthCache, h2TestSyntheticPath)
	synthMu.Unlock()
	syntheticSources[h2TestSyntheticPath] = h2TestSyntheticSource
	t.Cleanup(func() {
		delete(syntheticSources, h2TestSyntheticPath)
		synthMu.Lock()
		delete(synthCache, h2TestSyntheticPath)
		synthMu.Unlock()
	})
}

func TestBuildSyntheticPackageKeepsMRFuncVarAlias(t *testing.T) {
	injectH2TestSynthetic(t)
	sp := buildSyntheticPackage(h2TestSyntheticPath)
	obj := sp.pkg.Scope().Lookup("myStat")
	vr, ok := obj.(*types.Var)
	if !ok {
		t.Fatalf("myStat is %T, want *types.Var (scope object %v)", obj, obj)
	}
	fn, ok := sp.varFuncAliases[vr]
	if !ok {
		t.Fatalf("varFuncAliases has no entry for myStat: alias dropped with the declaration (maps=%v)", sp.varFuncAliases)
	}
	if fn.Name() != "Stat" || fn.Pkg() == nil || fn.Pkg().Path() != "os" {
		t.Fatalf("myStat alias = %v, want os.Stat", fn)
	}
	// Pin the removePkgSpecs=true side of the contract: the declaration
	// itself is gone from the AST, so the stored alias is the ONLY record
	// of the variable. If this ever stops removing, the test name and
	// comment must be revisited, not silently weakened.
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
					if n.Name == "myStat" {
						t.Fatalf("myStat ValueSpec still present in AST; alias storage alone does not pin the removal contract")
					}
				}
			}
		}
	}
}

func TestSyntheticMRFuncVarAliasCallDesugars(t *testing.T) {
	injectH2TestSynthetic(t)
	const src = `package main

import h2alias "h2mralias"

func main() {
	fi, err := h2alias.CallStat("x")
	_, _ = fi, err
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	// The call through myStat must desugar to os.Stat's pair helper,
	// not to the fail-fast Stat stub and not to a dropped call. Match
	// the exact object name: "__statFollow" (os's own helper) must not
	// satisfy this check.
	var doc struct {
		Objects []struct {
			Name string `json:"name"`
		} `json:"objects"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	for _, o := range doc.Objects {
		if o.Name == "__stat" {
			return
		}
	}
	names := make([]string, 0, len(doc.Objects))
	for _, o := range doc.Objects {
		names = append(names, o.Name)
	}
	t.Fatalf("no __stat object in exported doc; call through myStat did not desugar to os.Stat's pair helper (objects=%s)", strings.Join(names, ", "))
}
