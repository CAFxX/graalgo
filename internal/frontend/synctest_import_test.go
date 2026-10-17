package main

// Compile-time scope exclusion for testing/synctest (RV-P1-01).
// testing/synctest's entire public API requires bubble semantics
// (fake-time scheduling with quiescence-gated clock advancement), which
// graalgo does not implement; any importing program would abort at
// runtime in synctest.Run. The frontend therefore rejects the import at
// the type-check boundary via the unsupportedImports table, with a
// diagnostic naming the missing feature — never a compilable program
// that panics.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSynctestProg writes src to a temp .go file and returns its path.
func writeSynctestProg(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestExportRejectsTestingSynctest: importing testing/synctest fails at
// export time; the error names the import path, the missing feature, and
// the work-registry tracking item.
func TestExportRejectsTestingSynctest(t *testing.T) {
	src := `package main

import "testing/synctest"

func main() {
	synctest.Test(nil, func() {})
}
`
	_, err := ExportFile(writeSynctestProg(t, src))
	if err == nil {
		t.Fatal("ExportFile admitted import \"testing/synctest\"")
	}
	msg := err.Error()
	for _, want := range []string{
		`import "testing/synctest"`,
		"not supported by graalgo",
		"fake-time scheduling",
		"quiescence-gated clock advancement",
		"synctest-bubbles",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("export error %q does not name %q", msg, want)
		}
	}
}

// TestExportAcceptsSyncOnly: the exclusion is exactly scoped to
// testing/synctest — sync (whose real implementation reaches
// internal/synctest only via linknames, not imports) still compiles.
func TestExportAcceptsSyncOnly(t *testing.T) {
	src := `package main

import "sync"

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Wait()
}
`
	out, err := ExportFile(writeSynctestProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with import \"sync\" (no testing/synctest): %v", err)
	}
	for _, want := range []string{"Add", "Done", "Wait"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing method %s", want)
		}
	}
}

// TestUnsupportedImportsTableScoped: the exclusion table rejects exactly
// testing/synctest — every other checked path is left to the normal
// import machinery.
func TestUnsupportedImportsTableScoped(t *testing.T) {
	for _, path := range []string{"testing/synctest", "sync", "time", "testing", "runtime", "example.com/x"} {
		_, excluded := unsupportedImports[path]
		if excluded != (path == "testing/synctest") {
			t.Errorf("unsupportedImports[%q] excluded=%v, want %v", path, excluded, path == "testing/synctest")
		}
	}
}
