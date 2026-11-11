package vet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runVetSnippet runs the analyzers over a single-file snippet and returns
// the joined diagnostic messages.
func runVetSnippet(t *testing.T, src string) string {
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

func TestPrintfStarArgs(t *testing.T) {
	// Correct uses of '*' width / '.*' precision must produce no printf
	// diagnostic: the star args are consumed before the verb arg is checked.
	good := []string{
		`fmt.Sprintf("%.*f", 2, 3.14)`,
		`fmt.Sprintf("%*.*f", 3, 2, 3.14)`,
		`fmt.Sprintf("%*d", 5, 42)`,
		`fmt.Sprintf("%.*s", 2, "hello")`,
		`fmt.Sprintf("%*.*s", 3, 2, "hello")`,
	}
	for _, call := range good {
		src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\t_ = " + call + "\n}\n"
		if got := runVetSnippet(t, src); strings.Contains(got, "expects") || strings.Contains(got, "missing argument") {
			t.Errorf("%s: expected no printf diagnostic, got:\n%s", call, got)
		}
	}

	// The real verb arg (after the star args) must still be checked.
	bad := []struct {
		call string
		want string
	}{
		{`fmt.Sprintf("%.*f", 2, "hello")`, "expects floating point"},
		{`fmt.Sprintf("%*d", 5, "hello")`, "expects integer"},
		{`fmt.Sprintf("%*.*f", 3, 2, "hello")`, "expects floating point"},
		{`fmt.Sprintf("%.*f", 2)`, "missing argument"},
		{`fmt.Sprintf("%*.*f", 3, 2)`, "missing argument"},
	}
	for _, tc := range bad {
		src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\t_ = " + tc.call + "\n}\n"
		if got := runVetSnippet(t, src); !strings.Contains(got, tc.want) {
			t.Errorf("%s: expected diagnostic containing %q, got:\n%s", tc.call, tc.want, got)
		}
	}
}
