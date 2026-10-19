package main

// Regression tests for RV22-P1-14 (mr_disk.go time.Now eta-expansion).
//
// A pre-type-check AST rewrite in transformDiskMultiResult used to
// eta-expand exactly `var x = time.Now` (name-keyed on the "time" ident and
// the "Now" selector) in disk packages. It was deleted as redundant: time
// intrinsics in value position lower at export time through the general
// mechanism (exportTimeIntrinsicValue via exportSyntheticQualified), which
// covers disk packages exactly like synthetic ones.
//
// These tests pin the general path through the real disk pipeline
// (getDiskPackage/buildDiskPackage + export of an importing program):
//  1. TestDiskTimeIntrinsicsValuePosition: time.Now/time.After/time.Tick
//     in value position in a disk package lower to wrapper closures
//     holding the timenow/timeafter/timetick wire nodes. time.After is
//     the load-bearing case: it used to be silently dropped (nil global
//     init) because the package-var initializer skip in
//     importSyntheticPackage keyed synthFuncIDs by (path, name), colliding
//     with the (*Time).After method registered under the same key. The
//     skip now matches by object identity.
//  2. TestDiskSyntheticFuncValueSkipIntact: the skip the identity fix
//     preserves — `var S = strings.Clone` (a synthetic package-level func
//     in value position) still leaves the var nil. Guards against the
//     identity check breaking the intended carve-out.
//  3. TestDiskMathIntrinsicValuePositionM7: math intrinsics in value
//     position stay a named M7 scope error (kept deliberately, f4f2b95a:
//     "the math-intrinsic value fail-fast is kept as a named limitation").
//     This locks the three-way split: time intrinsics wrap, math
//     intrinsics fail fast, ordinary funcs take the func-value path.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture disk package: time intrinsics in value position plus the
// preserved-skip case. Built from a temp GOPATH so the test is hermetic.
const diskTimeIntrinsicPkg = `package tivprobe

import (
	"strings"
	"time"
)

var FNow = time.Now
var FAfter = time.After
var FTick = time.Tick

// The carve-out the identity fix must preserve: a synthetic package-level
// function in value position keeps the var nil (call sites desugar via the
// alias map). Single-result, so etaExpandMRFuncValues leaves the decl in
// place and the importSyntheticPackage skip handles it.
var S = strings.Clone

func CallNow() time.Time                      { return FNow() }
func CallAfter(d time.Duration) <-chan time.Time { return FAfter(d) }
func CallTick(d time.Duration) <-chan time.Time  { return FTick(d) }
`

const diskTimeIntrinsicMain = `package main

import (
	"tivprobe"
	"time"
)

func main() {
	_ = tivprobe.CallNow()
	_ = tivprobe.CallAfter(time.Second)
	_ = tivprobe.CallTick(time.Second)
}
`

const diskMathIntrinsicPkg = `package tmprobe

import "math"

var FSqrt = math.Sqrt
`

const diskMathIntrinsicMain = `package main

import _ "tmprobe"

func main() {}
`

// exportViaDiskFixture writes pkgSrc as a disk package importable under
// pkgPath in a temp GOPATH, writes mainSrc importing it, and exports the
// main program, returning the JSON document.
func exportViaDiskFixture(t *testing.T, pkgPath, pkgSrc, mainSrc string) (string, error) {
	t.Helper()
	gp := t.TempDir()
	pkgDir := filepath.Join(gp, "src", filepath.FromSlash(pkgPath))
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "p.go"), []byte(pkgSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	mainDir := filepath.Join(gp, "src", "mainprog")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(mainDir, "main.go")
	if err := os.WriteFile(main, []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gp)
	t.Setenv("GOMODCACHE", filepath.Join(gp, "pkg", "mod"))
	return ExportFiles([]string{main}, "")
}

func TestDiskTimeIntrinsicsValuePosition(t *testing.T) {
	doc, err := exportViaDiskFixture(t, "tivprobe", diskTimeIntrinsicPkg, diskTimeIntrinsicMain)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	// Each intrinsic's wire node appears inside a synthesized wrapper
	// closure in the exported document.
	for _, want := range []string{`"timenow"`, `"timeafter"`, `"timetick"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("exported document missing wire node %s (intrinsic value not wrapped)", want)
		}
	}
	// The wrappers are closures: funclit nodes must be present for the
	// three globals' initializers.
	if n := strings.Count(doc, `"funclit"`); n < 3 {
		t.Errorf("want >= 3 funclit nodes (one per intrinsic wrapper), got %d", n)
	}
}

func TestDiskSyntheticFuncValueSkipIntact(t *testing.T) {
	doc, err := exportViaDiskFixture(t, "tivprobe", diskTimeIntrinsicPkg, diskTimeIntrinsicMain)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	// strings.Clone in value position: the var keeps a nil initializer
	// (the skip fired). Assert on the decoded document: the global named
	// S has a null init entry.
	var v struct {
		Objects []struct {
			Name string `json:"name"`
		} `json:"objects"`
		Globals []struct {
			Obj  int             `json:"obj"`
			Init json.RawMessage `json:"init"`
		} `json:"globals"`
	}
	if err := json.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("cannot decode exported document: %v", err)
	}
	found := false
	for _, g := range v.Globals {
		if g.Obj < 0 || g.Obj >= len(v.Objects) {
			continue
		}
		if v.Objects[g.Obj].Name == "S" {
			found = true
			if string(g.Init) != "null" {
				t.Errorf("global S init = %s, want null (skip did not fire)", g.Init)
			}
		}
	}
	if !found {
		t.Fatal("exported document has no global S")
	}
}

func TestDiskMathIntrinsicValuePositionM7(t *testing.T) {
	_, err := exportViaDiskFixture(t, "tmprobe", diskMathIntrinsicPkg, diskMathIntrinsicMain)
	if err == nil {
		t.Fatal("expected M7 scope error for math.Sqrt in value position, got success")
	}
	if !strings.Contains(err.Error(), "M7") {
		t.Fatalf("want M7 scope error, got: %v", err)
	}
}
