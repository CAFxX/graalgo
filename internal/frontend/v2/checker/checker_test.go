// Stage-2 tests (R23): the checker accepts valid programs, rejects
// type errors and bad imports loudly, and reports diagnostics in a
// deterministic file order (the checker sorts file names before
// checking, matching the emitter's order).
package checker

import (
	"go/token"
	"strings"
	"testing"

	"graalgo-frontend-v2/parser"
)

// checkSources parses and type-checks in-memory sources.
func checkSources(t *testing.T, sources map[string]string, conf *Config) (*Result, error) {
	t.Helper()
	fset := token.NewFileSet()
	pkg, err := parser.ParseSources(fset, sources)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Check(fset, pkg, conf)
}

func TestCheckAccept(t *testing.T) {
	res, err := checkSources(t, map[string]string{
		"a.go": `package demo

var Count = 42

type Point struct {
	X int
	Y int
}

func (p Point) Sum() int { return p.X + p.Y }

func Add(a, b int) int { return a + b }
`,
	}, &Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Pkg == nil || res.Pkg.Name() != "demo" {
		t.Fatalf("Pkg = %v, want demo", res.Pkg)
	}
	if res.Info == nil || len(res.Info.Defs) == 0 {
		t.Fatalf("Info is empty")
	}
	// The checked package knows its own declarations.
	if obj := res.Pkg.Scope().Lookup("Add"); obj == nil {
		t.Fatalf("package scope lacks Add")
	}
	if obj := res.Pkg.Scope().Lookup("Count"); obj == nil {
		t.Fatalf("package scope lacks Count")
	}
}

func TestCheckAcceptStdlibImport(t *testing.T) {
	// The importer resolves from the real toolchain: no synthetic
	// packages, no overlay. If the toolchain layout ever stops
	// providing export data, this test fails instead of silently
	// accepting an unresolved import.
	_, err := checkSources(t, map[string]string{
		"a.go": `package demo

import "fmt"

func Hello() { fmt.Println("hi") }
`,
	}, &Config{GoVersion: "go1.27"})
	if err != nil {
		t.Fatalf("Check with fmt import: %v", err)
	}
}

func TestCheckNilConfig(t *testing.T) {
	_, err := checkSources(t, map[string]string{
		"a.go": "package demo\n\nfunc F() {}\n",
	}, nil)
	if err != nil {
		t.Fatalf("Check with nil Config: %v", err)
	}
}

func TestCheckReject(t *testing.T) {
	t.Run("type error", func(t *testing.T) {
		_, err := checkSources(t, map[string]string{
			"a.go": "package demo\n\nvar X int = \"s\"\n",
		}, &Config{GoVersion: "go1.27"})
		if err == nil {
			t.Fatalf("expected a type error")
		} else if !strings.Contains(err.Error(), "cannot use") {
			t.Fatalf("error %q does not describe the type mismatch", err.Error())
		}
	})
	t.Run("undefined", func(t *testing.T) {
		_, err := checkSources(t, map[string]string{
			"a.go": "package demo\n\nfunc F() { nosuch() }\n",
		}, &Config{GoVersion: "go1.27"})
		if err == nil {
			t.Fatalf("expected an undefined-identifier error")
		} else if !strings.Contains(err.Error(), "undefined") {
			t.Fatalf("error %q does not say undefined", err.Error())
		}
	})
	t.Run("bad import", func(t *testing.T) {
		_, err := checkSources(t, map[string]string{
			"a.go": "package demo\n\nimport \"nonexistent-xyz-abc\"\n\nvar _ = 1\n",
		}, &Config{GoVersion: "go1.27"})
		if err == nil {
			t.Fatalf("expected an import error")
		} else if !strings.Contains(err.Error(), "could not import") {
			t.Fatalf("error %q does not say could not import", err.Error())
		}
	})
	t.Run("checker prefix", func(t *testing.T) {
		_, err := checkSources(t, map[string]string{
			"a.go": "package demo\n\nvar X int = \"s\"\n",
		}, &Config{GoVersion: "go1.27"})
		if err == nil || !strings.Contains(err.Error(), "checker:") {
			t.Fatalf("error %q lacks the checker: prefix", err)
		}
	})
}

// TestCheckDeterministicDiagnostics pins the file-name sort: with two
// files each holding a type error, the first reported diagnostic must
// come from a.go on every run. Before the sort, pkg.Files map order
// decided which file's error came first, so this failed about half the
// runs; 25 fresh parses make a pre-fix pass overwhelmingly unlikely.
func TestCheckDeterministicDiagnostics(t *testing.T) {
	sources := map[string]string{
		"a.go": "package demo\n\nvar A int = \"a\"\n",
		"b.go": "package demo\n\nvar B int = \"b\"\n",
	}
	for i := 0; i < 25; i++ {
		_, err := checkSources(t, sources, &Config{GoVersion: "go1.27"})
		if err == nil {
			t.Fatalf("run %d: expected a type error", i)
		}
		msg := err.Error()
		ia := strings.Index(msg, "a.go")
		ib := strings.Index(msg, "b.go")
		if ia < 0 {
			t.Fatalf("run %d: error %q mentions no a.go diagnostic", i, msg)
		}
		if ib >= 0 && ib < ia {
			t.Fatalf("run %d: b.go diagnostic precedes a.go: %q", i, msg)
		}
	}
}
