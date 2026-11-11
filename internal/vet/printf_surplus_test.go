package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVetSrc runs the analyzers over src (a complete single-file program)
// and returns the joined diagnostic messages.
//
// Self-contained helper: F-P1-4's printf_test.go owns the shared
// runVetSnippet helper for the same package, but that file is still
// uncommitted in-flight work owned by another lane, so this file must
// not depend on it. The two helpers can be consolidated at integration
// time.
func runVetSrc(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "snippet.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range ds {
		msgs = append(msgs, d.Message)
	}
	return strings.Join(msgs, "\n")
}

func vetCall(t *testing.T, imports, call string) string {
	t.Helper()
	src := "package main\n\n" + imports + "\n\nfunc main() {\n\t_ = " + call + "\n}\n"
	return runVetSrc(t, src)
}

// TestPrintfExtraArgs: surplus arguments are reported, one diagnostic
// per surplus argument; calls with exactly the consumed count stay
// silent. (P1-25a: the verb loop previously only checked missing args.)
func TestPrintfExtraArgs(t *testing.T) {
	bad := []string{
		`fmt.Sprintf("%d", 1, 2)`,
		`fmt.Sprintf("%d %d", 1, 2, 3)`,
		`fmt.Sprintf("hello", 1)`,
		`fmt.Sprintf("%s", "a", "b", "c")`,
		`fmt.Sprintf("%*d", 5, 42, 7)`,
		`fmt.Sprintf("%.*f", 2, 3.14, 9)`,
		`fmt.Sprintf("%*.*f", 3, 2, 3.14, 9)`,
	}
	for _, call := range bad {
		if got := vetCall(t, "import \"fmt\"", call); !strings.Contains(got, "extra argument") {
			t.Errorf("%s: expected diagnostic containing %q, got:\n%s", call, "extra argument", got)
		}
	}

	good := []string{
		`fmt.Sprintf("%d", 1)`,
		`fmt.Sprintf("%d %d", 1, 2)`,
		`fmt.Sprintf("hello")`,
		`fmt.Sprintf("%*d", 5, 42)`,
		`fmt.Sprintf("%.*f", 2, 3.14)`,
		`fmt.Sprintf("%*.*f", 3, 2, 3.14)`,
	}
	for _, call := range good {
		// Assert only the absence of a surplus diagnostic: on the
		// pre-F-P1-4 base the %.*f cases still (correctly, for this
		// lane) report "expects floating point" for the misaligned
		// verb arg — that is F-P1-4's owned bug (b), not this lane's.
		if got := vetCall(t, "import \"fmt\"", call); strings.Contains(got, "extra argument") {
			t.Errorf("%s: expected no surplus diagnostic, got:\n%s", call, got)
		}
	}
}

// TestPrintfDotImport: dot-imported bare Printf/Sprintf/Errorf/Fprintf
// calls are checked exactly like their qualified forms. (P1-25c:
// printfFormatIndex previously required a *ast.SelectorExpr callee.)
func TestPrintfDotImport(t *testing.T) {
	bad := []struct {
		call string
		want string
	}{
		{`Printf("%d", "hello")`, "expects integer"},
		{`Sprintf("%d %d", 1)`, "missing argument"},
		{`Sprintf("%d", 1, 2)`, "extra argument"},
		{`Errorf("%d", "hello")`, "expects integer"},
	}
	for _, tc := range bad {
		if got := vetCall(t, "import . \"fmt\"", tc.call); !strings.Contains(got, tc.want) {
			t.Errorf("%s: expected diagnostic containing %q, got:\n%s", tc.call, tc.want, got)
		}
	}

	// Correct dot-imported calls stay silent.
	for _, call := range []string{`Printf("%d", 1)`, `Sprintf("%d", 1)`} {
		if got := vetCall(t, "import . \"fmt\"", call); strings.Contains(got, "expects") || strings.Contains(got, "missing argument") || strings.Contains(got, "extra argument") {
			t.Errorf("%s: expected no printf diagnostic, got:\n%s", call, got)
		}
	}

	// Dot-imported log.Printf works too.
	src := "package main\n\nimport . \"log\"\n\nfunc main() {\n\tPrintf(\"%d\", \"hello\")\n}\n"
	if got := runVetSrc(t, src); !strings.Contains(got, "expects integer") {
		t.Errorf("dot-imported log.Printf: expected diagnostic containing %q, got:\n%s", "expects integer", got)
	}

	// Dot-imported Fprintf: the format is argument index 1 (the writer
	// is argument 0), so both the type check and the surplus check must
	// use the shifted index. (Fprintf returns two values, so the calls
	// need `_, _ =`; a bare `_ =` would be a type error and go/types
	// best-effort info then drops the later call's format literal.)
	src = "package main\n\nimport (\n\t. \"fmt\"\n\t\"io\"\n)\n\nfunc main() {\n\tvar w io.Writer\n\t_, _ = Fprintf(w, \"%d\", \"hello\")\n\t_, _ = Fprintf(w, \"%d\", 1, 2)\n}\n"
	if got := runVetSrc(t, src); !strings.Contains(got, "expects integer") || !strings.Contains(got, "extra argument") {
		t.Errorf("dot-imported Fprintf: expected %q and %q diagnostics, got:\n%s", "expects integer", "extra argument", got)
	}

	// A local function named Printf must NOT be treated as fmt.Printf.
	src = "package main\n\nfunc Printf(format string, args ...any) string { return format }\n\nfunc main() {\n\t_ = Printf(\"%d\", \"hello\")\n}\n"
	if got := runVetSrc(t, src); strings.Contains(got, "expects integer") {
		t.Errorf("local Printf misidentified as fmt.Printf, got:\n%s", got)
	}
}
