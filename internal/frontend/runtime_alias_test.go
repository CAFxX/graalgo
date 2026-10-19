package main

// Regression tests for fix-p0-runtime-alias (independent review P0):
// the runtime intrinsic interception in exportCall and isGoschedCall must
// discriminate on the resolved package object (the R2-P2-9 pattern), not
// the identifier spelling, and on the package's import PATH, not its
// declared name.
//
// Red-green: the alias cases fail before the fix — rt.GC() etc. fall
// through to the general path, where the bodyless synthetic decl hits the
// "unimplemented" marker — and pass after. The user-package-named-runtime
// case fails before the fix (old name-keying over-matches when the import
// is unaliased, so the qualifier is spelled "runtime"); the shadowed-local
// case guards against over-matching after the fix.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestRuntimeAliasIntrinsicsExport: every intercepted runtime intrinsic,
// spelled through an import alias (rt "runtime"), must lower to its wire
// node exactly like the unaliased form.
func TestRuntimeAliasIntrinsicsExport(t *testing.T) {
	cases := []struct {
		name  string
		stmts string
		want  string
	}{
		{"GC", `rt.GC()`, `"gcinterop": "GC"`},
		{"SetFinalizer", "var x int\n\trt.SetFinalizer(&x, nil)", `"gcinterop": "SetFinalizer"`},
		{"KeepAlive", "var x int\n\trt.KeepAlive(&x)", `"gcinterop": "KeepAlive"`},
		{"NumCPU", `_ = rt.NumCPU()`, `"kind": "runtimenumcpu"`},
		{"GOMAXPROCS", `_ = rt.GOMAXPROCS(2)`, `"kind": "runtimegomaxprocs"`},
		{"NumGoroutine", `_ = rt.NumGoroutine()`, `"kind": "runtimenumgoroutine"`},
		{"Gosched", `rt.Gosched()`, `"kind": "runtimegosched"`},
		{"ReadMemStats", "var m rt.MemStats\n\trt.ReadMemStats(&m)", `"kind": "runtimereadmemstats"`},
		{"GOROOT", `_ = rt.GOROOT()`, `"kind": "runtimegoroot"`},
		{"Version", `_ = rt.Version()`, `"kind": "runtimeversion"`},
		{"Stack", "var buf [64]byte\n\t_ = rt.Stack(buf[:], true)", `"kind": "runtimestack"`},
		{"Callers", "var pcs [8]uintptr\n\t_ = rt.Callers(1, pcs[:])", `"kind": "runtimecallers"`},
		{"CallersFrames", "var pcs [8]uintptr\n\t_ = rt.CallersFrames(pcs[:])", `"kind": "runtimecallersframes"`},
		{"FuncForPC", "var pc uintptr\n\t_ = rt.FuncForPC(pc)", `"kind": "runtimefuncforpc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package main\n\nimport rt \"runtime\"\n\nfunc main() {\n\t" +
				tc.stmts + "\n}\n"
			out, err := ExportFile(writeSyncProg(t, src))
			if err != nil {
				t.Fatalf("ExportFile with rt.%s: %v", tc.name, err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("aliased rt.%s: exported JSON missing %s", tc.name, tc.want)
			}
		})
	}
}

// TestRuntimeAliasGoexitExport: rt.Goexit() through an import alias keeps
// lowering to the goexit wire node (R2-P2-9 behavior, unchanged by the
// Path()-based second gate).
func TestRuntimeAliasGoexitExport(t *testing.T) {
	src := `package main

import rt "runtime"

func main() {
	rt.Goexit()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with rt.Goexit: %v", err)
	}
	if !strings.Contains(out, `"goexit": true`) {
		t.Errorf("exported JSON missing \"goexit\": true")
	}
}

// TestRuntimeAliasDeferGoGoschedExport: `defer rt.Gosched()` and
// `go rt.Gosched()` through an import alias desugar through the funclit
// exactly like the unaliased form (isGoschedCall must use the same
// resolved-object discrimination as exportCall).
func TestRuntimeAliasDeferGoGoschedExport(t *testing.T) {
	src := `package main

import rt "runtime"

func main() {
	defer rt.Gosched()
	go rt.Gosched()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with defer/go rt.Gosched: %v", err)
	}
	if n := strings.Count(out, `"kind": "runtimegosched"`); n != 2 {
		t.Errorf("exported JSON has %d runtimegosched nodes, want 2 (defer + go)", n)
	}
}

// TestRuntimeShadowedLocalNotIntercepted: a local variable named runtime
// (not a package) must not trigger intrinsic interception.
func TestRuntimeShadowedLocalNotIntercepted(t *testing.T) {
	src := `package main

type sh struct{}

func (sh) GC() string { return "shadow" }

func main() {
	var runtime sh
	s := runtime.GC()
	println(s)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with shadowed runtime local: %v", err)
	}
	if strings.Contains(out, "gcinterop") {
		t.Errorf("shadowed local runtime.GC() wrongly intercepted as a runtime intrinsic")
	}
}

// TestRuntimeUserPackageNamedRuntimeNotIntercepted: a user package whose
// DECLARED name is "runtime" but whose import path is not ("fakert/runtime",
// resolved from a temp GOPATH) must not have its functions rewritten to
// runtime intrinsics. The import is deliberately unaliased so the qualifier
// is spelled "runtime" — the old spelling gate over-matched this case.
func TestRuntimeUserPackageNamedRuntimeNotIntercepted(t *testing.T) {
	gp := t.TempDir()
	pkgDir := filepath.Join(gp, "src", "fakert", "runtime")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeSrc := `package runtime

// GC shares its name with the runtime intrinsic but is ordinary user code.
func GC() string { return "user GC" }

func NumCPU() int { return 42 }

func Gosched() string { return "user Gosched" }
`
	if err := os.WriteFile(filepath.Join(pkgDir, "runtime.go"), []byte(fakeSrc), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPATH", gp)
	src := `package main

import "fakert/runtime"

func main() {
	s := runtime.GC()
	n := runtime.NumCPU()
	runtime.Gosched()
	println(s, n)
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with user runtime package: %v", err)
	}
	for _, bad := range []string{`"gcinterop"`, `"kind": "runtimenumcpu"`, `"kind": "runtimegosched"`} {
		if strings.Contains(out, bad) {
			t.Errorf("user package (fakert/runtime).%s wrongly intercepted as a runtime intrinsic (%s present)", bad, bad)
		}
	}
}

// wireKinds extracts the sorted multiset of "kind" values from an exported
// JSON document (source positions aside).
func wireKinds(t *testing.T, out string) []string {
	t.Helper()
	re := regexp.MustCompile(`"kind":\s*"([a-z0-9]+)"`)
	m := re.FindAllStringSubmatch(out, -1)
	kinds := make([]string, 0, len(m))
	for _, s := range m {
		kinds = append(kinds, s[1])
	}
	sort.Strings(kinds)
	return kinds
}

// TestRuntimeAliasMatchesUnaliasedExport: a program exercising the runtime
// intrinsics through an import alias must export the identical multiset of
// wire-node kinds as the same program with the plain "runtime" qualifier —
// the alias changes nothing downstream of the frontend.
func TestRuntimeAliasMatchesUnaliasedExport(t *testing.T) {
	body := `	var x int
	var m %s.MemStats
	var buf [64]byte
	var pcs [8]uintptr
	var pc uintptr
	%s.GC()
	%s.SetFinalizer(&x, nil)
	%s.KeepAlive(&x)
	_ = %s.NumCPU()
	_ = %s.GOMAXPROCS(2)
	_ = %s.NumGoroutine()
	%s.Gosched()
	%s.ReadMemStats(&m)
	_ = %s.GOROOT()
	_ = %s.Version()
	_ = %s.Stack(buf[:], true)
	_ = %s.Callers(1, pcs[:])
	_ = %s.CallersFrames(pcs[:])
	_ = %s.FuncForPC(pc)
`
	mkprog := func(qual, imp string) string {
		n := strings.Count(body, "%s")
		args := make([]any, n)
		for i := range args {
			args[i] = qual
		}
		return "package main\n\n" + imp + "\n\nfunc main() {\n" +
			fmt.Sprintf(body, args...) + "}\n"
	}
	plain := mkprog("runtime", `import "runtime"`)
	aliased := mkprog("rt", `import rt "runtime"`)
	outPlain, err := ExportFile(writeSyncProg(t, plain))
	if err != nil {
		t.Fatalf("ExportFile unaliased: %v", err)
	}
	outAlias, err := ExportFile(writeSyncProg(t, aliased))
	if err != nil {
		t.Fatalf("ExportFile aliased: %v", err)
	}
	kp, ka := wireKinds(t, outPlain), wireKinds(t, outAlias)
	if strings.Join(kp, ",") != strings.Join(ka, ",") {
		t.Errorf("wire-node kinds differ:\n unaliased: %v\n aliased:   %v", kp, ka)
	}
}
