package main

// Regression tests for RV22-P1-9: the runtime-intrinsic interception block
// in exportCall gated on the qualifier's identifier spelling
// (pkgIdent.Name == "runtime"), so `import rt "runtime"` fell through to
// the general path where the synthetic runtime's bodyless members died
// with "call to unknown function". The gate now resolves the qualifier
// to its package object and compares the import path. Red-green: the
// positive test fails before the fix (export error on rt.GC()); the
// negative test fails before the fix (silent miscompilation of the user
// package's GC into a gcinterop wire node).

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// renamedRuntimeSrc exercises the intrinsic call shapes from the lane
// brief through a renamed runtime import.
const renamedRuntimeSrc = `package main

import rt "runtime"

func main() {
	rt.GC()
	_ = rt.NumCPU()
	_ = rt.GOMAXPROCS(0)
	_ = rt.NumGoroutine()
	rt.Gosched()
}
`

// TestRenamedRuntimeImportIntrinsics: every intrinsic in the block must
// be intercepted through a renamed import exactly as through the plain
// "runtime" qualifier.
func TestRenamedRuntimeImportIntrinsics(t *testing.T) {
	out, err := ExportFile(writeSyncProg(t, renamedRuntimeSrc))
	if err != nil {
		t.Fatalf("ExportFile with import rt \"runtime\": %v", err)
	}
	for _, want := range []string{
		`"gcinterop": "GC"`,
		`"kind": "runtimenumcpu"`,
		`"kind": "runtimegomaxprocs"`,
		`"kind": "runtimenumgoroutine"`,
		`"kind": "runtimegosched"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

var kindFieldRe = regexp.MustCompile(`"kind": "([^"]+)"`)

func nodeKindSeq(t *testing.T, src string) []string {
	t.Helper()
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var seq []string
	for _, m := range kindFieldRe.FindAllStringSubmatch(out, -1) {
		seq = append(seq, m[1])
	}
	if len(seq) == 0 {
		t.Fatal("exported JSON has no kind fields")
	}
	return seq
}

// TestRenamedRuntimeImportMatchesPlainForm: the renamed import must lower
// to the identical wire-node kind sequence as the plain "runtime" form —
// the identifier spelling must not change the compilation.
func TestRenamedRuntimeImportMatchesPlainForm(t *testing.T) {
	plainSrc := strings.Replace(renamedRuntimeSrc, `import rt "runtime"`, `import "runtime"`, 1)
	plainSrc = strings.ReplaceAll(plainSrc, "rt.", "runtime.")
	renamed := nodeKindSeq(t, renamedRuntimeSrc)
	plain := nodeKindSeq(t, plainSrc)
	if !reflect.DeepEqual(renamed, plain) {
		t.Errorf("renamed import kind sequence differs from plain form:\nrenamed: %v\nplain:   %v", renamed, plain)
	}
}

// TestRenamedRuntimeGoschedGoDeferMatchesPlainForm: `go rt.Gosched()` /
// `defer rt.Gosched()` must take the same funclit-desugar path as the
// plain form (isGoschedCall resolves the qualifier by import path), never
// a bare statement node wrapped directly in gostmt/defer (which the
// backend cannot decode — it expects fields.call.fields.fun).
func TestRenamedRuntimeGoschedGoDeferMatchesPlainForm(t *testing.T) {
	renamedSrc := `package main

import rt "runtime"

func main() {
	go rt.Gosched()
	defer rt.Gosched()
}
`
	plainSrc := `package main

import "runtime"

func main() {
	go runtime.Gosched()
	defer runtime.Gosched()
}
`
	renamed := nodeKindSeq(t, renamedSrc)
	plain := nodeKindSeq(t, plainSrc)
	if !reflect.DeepEqual(renamed, plain) {
		t.Errorf("renamed go/defer Gosched kind sequence differs from plain form:\nrenamed: %v\nplain:   %v", renamed, plain)
	}
}

// TestUserPackageNamedRuntimeNotIntercepted: a user package whose package
// clause is "runtime" but whose import path is NOT "runtime" must never be
// intercepted as an intrinsic. Uses the plain "runtime" qualifier spelling
// (the case the old name-keyed inner check miscompiled silently).
func TestUserPackageNamedRuntimeNotIntercepted(t *testing.T) {
	gp := t.TempDir()
	pkgDir := filepath.Join(gp, "src", "example.com", "rv22p19", "runtime")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	userSrc := `package runtime

func GC() int { return 42 }
`
	if err := os.WriteFile(filepath.Join(pkgDir, "runtime.go"), []byte(userSrc), 0644); err != nil {
		t.Fatal(err)
	}
	mainSrc := `package main

import "example.com/rv22p19/runtime"

func main() {
	_ = runtime.GC()
}
`
	t.Setenv("GOPATH", gp)
	out, err := ExportFile(writeSyncProg(t, mainSrc))
	if err != nil {
		t.Fatalf("ExportFile with user runtime package: %v", err)
	}
	for _, forbidden := range []string{
		`"gcinterop"`,
		`"kind": "runtimenumcpu"`,
		`"kind": "runtimegomaxprocs"`,
		`"kind": "runtimenumgoroutine"`,
		`"kind": "runtimegosched"`,
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("user package's GC miscompiled as intrinsic: exported JSON contains %s", forbidden)
		}
	}
	if !strings.Contains(out, `"kind": "call"`) {
		t.Errorf("user package's GC did not compile to a normal call node")
	}
}
