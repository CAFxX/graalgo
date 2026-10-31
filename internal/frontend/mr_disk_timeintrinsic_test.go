package main

// Regression tests for R-03 (general intrinsic-value eta-expansion).
//
// The pre-type-check AST rewrite in transformDiskMultiResult used to
// eta-expand exactly `var x = time.Now` (name-keyed on the "time" ident and
// the "Now" selector) in disk packages. It was deleted: intrinsics in value
// position lower at export time through the general mechanism
// (exportIntrinsicValueFor via exportSyntheticQualified), which covers
// disk packages exactly like synthetic ones. The disk-fixture shape here
// adapts the tests from the unmerged RV22-P1-14 branch
// (wt/graalgo-review-20261018/p1-14-mr-disk, stale since 2026-10-19, never
// integrated); the math-intrinsic cases are new in this lane, because
// math intrinsics in value position previously failed fast with a named
// M7 scope error and now lower through the same wrapper mechanism.
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
//  3. TestDiskMathIntrinsicsValuePosition: math.Sqrt and math.Float64bits
//     in value position in a disk package lower to wrapper closures
//     holding the mathsqrt/mathf64bits wire nodes — the two non-time
//     intrinsics this lane generalises to. (Before this lane these were
//     a named M7 scope error.)
//  4. TestUserPackageIntrinsicValues: the same wrapper mechanism in a
//     user (non-disk) package, including the zero-result time.Sleep case,
//     whose wrapper body is an expression statement, not a return.

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
var FBits = math.Float64bits

func CallSqrt(x float64) float64 { return FSqrt(x) }
func CallBits(x float64) uint64  { return FBits(x) }
`

const diskMathIntrinsicMain = `package main

import (
	"tmprobe"
)

func main() {
	_ = tmprobe.CallSqrt(2)
	_ = tmprobe.CallBits(2)
}
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
	if err := os.WriteFile(filepath.Join(mainDir, "main.go"), []byte(mainSrc), 0o644); err != nil {
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

func TestDiskMathIntrinsicsValuePosition(t *testing.T) {
	doc, err := exportViaDiskFixture(t, "tmprobe", diskMathIntrinsicPkg, diskMathIntrinsicMain)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	for _, want := range []string{`"mathsqrt"`, `"mathf64bits"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("exported document missing wire node %s (math intrinsic value not wrapped)", want)
		}
	}
	if n := strings.Count(doc, `"funclit"`); n < 2 {
		t.Errorf("want >= 2 funclit nodes (one per math intrinsic wrapper), got %d", n)
	}
}

func TestUserPackageIntrinsicValues(t *testing.T) {
	src := `package main

import (
	"math"
	"time"
)

var FSqrt = math.Sqrt
var FSleep = time.Sleep

func main() {
	_ = FSqrt(2)
	FSleep(time.Second)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "main.go")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	for _, want := range []string{`"mathsqrt"`, `"timesleep"`, `"funclit"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("exported document missing %s (intrinsic value not wrapped)", want)
		}
	}
}
