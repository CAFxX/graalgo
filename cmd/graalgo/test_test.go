package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveTestTargetsFileMode is the regression test for Q1: a file
// operand used to be reduced to its directory, so `graalgo test
// a_test.go` ran every test in the directory. File operands must
// survive as file-mode targets carrying exactly the listed files.
func TestResolveTestTargetsFileMode(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/targets\n\ngo 1.21\n")
	writeFile(t, root, "p.go", "package targets\n")
	writeFile(t, root, "a_test.go", "package targets\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	writeFile(t, root, "b_test.go", "package targets\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")
	a := filepath.Join(root, "a_test.go")
	b := filepath.Join(root, "b_test.go")
	p := filepath.Join(root, "p.go")

	// A single test file: one target, carrying only that file.
	targets, err := resolveTestTargets([]string{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].dir != root || len(targets[0].files) != 1 || targets[0].files[0] != a {
		t.Fatalf("resolveTestTargets([a]) = %+v, want one target for %s with files [%s]", targets, root, a)
	}

	// Two files in one directory coalesce into one target, in order —
	// `go test` file mode builds one package from exactly those files.
	targets, err = resolveTestTargets([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || len(targets[0].files) != 2 || targets[0].files[0] != a || targets[0].files[1] != b {
		t.Fatalf("resolveTestTargets([a b]) = %+v, want one target with files [%s %s]", targets, a, b)
	}

	// A listed non-test file travels with the test files: it is the
	// package source for white-box tests, like `go test p.go a_test.go`.
	targets, err = resolveTestTargets([]string{p, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || len(targets[0].files) != 2 || targets[0].files[0] != p || targets[0].files[1] != a {
		t.Fatalf("resolveTestTargets([p a]) = %+v, want one target with files [%s %s]", targets, p, a)
	}

	// Listing the same file twice runs it once.
	targets, err = resolveTestTargets([]string{a, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || len(targets[0].files) != 1 {
		t.Fatalf("resolveTestTargets([a a]) = %+v, want one target with one file", targets)
	}

	// A directory operand keeps whole-directory behaviour (files nil).
	targets, err = resolveTestTargets([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].dir != root || targets[0].files != nil {
		t.Fatalf("resolveTestTargets([dir]) = %+v, want one whole-directory target", targets)
	}

	// A missing operand is an error, not an empty target.
	if _, err := resolveTestTargets([]string{filepath.Join(root, "nope_test.go")}); err == nil {
		t.Errorf("resolveTestTargets([missing]) = nil error, want an error")
	}
}

// TestResolveTestTargetsNoArgs pins the no-operand case: the current
// directory, whole-directory mode.
func TestResolveTestTargetsNoArgs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	targets, err := resolveTestTargets(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].dir != cwd || targets[0].files != nil {
		t.Fatalf("resolveTestTargets(nil) = %+v, want [{dir: %s, files: nil}]", targets, cwd)
	}
}

// TestSplitRunPattern pins `go test`'s split rules: '/' and '|' split
// only outside [...] and (...), and a backslash escapes the next
// character.
func TestSplitRunPattern(t *testing.T) {
	cases := []struct {
		pattern string
		want    [][]string
	}{
		{"TestX", [][]string{{"TestX"}}},
		{"TestX/failing", [][]string{{"TestX", "failing"}}},
		{"TestX/", [][]string{{"TestX", ""}}},
		{"A|B", [][]string{{"A"}, {"B"}}},
		{"A|B/c", [][]string{{"A"}, {"B", "c"}}},
		{"a[/]b", [][]string{{"a[/]b"}}},
		{"a(b/c)", [][]string{{"a(b/c)"}}},
	}
	// The escaped-slash case is checked explicitly below (a backslash
	// in a table literal would obscure what is being tested).
	for _, tc := range cases {
		got := splitRunPattern(tc.pattern)
		if len(got) != len(tc.want) {
			t.Fatalf("splitRunPattern(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
		for i := range got {
			if len(got[i]) != len(tc.want[i]) {
				t.Fatalf("splitRunPattern(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
			for j := range got[i] {
				if got[i][j] != tc.want[i][j] {
					t.Fatalf("splitRunPattern(%q) = %q, want %q", tc.pattern, got, tc.want)
				}
			}
		}
	}
	if got := splitRunPattern(`a\/b`); len(got) != 1 || len(got[0]) != 1 || got[0][0] != `a\/b` {
		t.Fatalf("splitRunPattern(`a\\/b`) = %q, want [[`a\\/b`]] (escaped slash is not a separator)", got)
	}
}

// TestParseRunFilterRejectsSubtests is the regression test for Q2: a
// slash-separated pattern used to compile as one regex, match no
// top-level test, and report `ok [no tests to run]` with exit 0 — a
// silent false pass. It must now fail loudly, naming subtest
// selection as unsupported.
func TestParseRunFilterRejectsSubtests(t *testing.T) {
	for _, pattern := range []string{"TestX/failing", "A|B/c", "/failing", "TestX/a/b"} {
		if _, err := parseRunFilter(pattern); err == nil {
			t.Errorf("parseRunFilter(%q) = nil error, want a subtest-selection error", pattern)
		} else if !strings.Contains(err.Error(), "subtest") {
			t.Errorf("parseRunFilter(%q) error %q does not name subtest selection", pattern, err)
		}
	}
}

// TestParseRunFilterAccepted pins what still works: plain patterns,
// alternation, and a trailing slash (which selects the top-level test
// whole — its empty trailing element matches every subtest level).
func TestParseRunFilterAccepted(t *testing.T) {
	// Plain pattern: the old single-regex behaviour, unanchored.
	res, err := parseRunFilter("TestX")
	if err != nil {
		t.Fatal(err)
	}
	if !matchRunFilter(res, "TestX") || !matchRunFilter(res, "TestXExtra") || matchRunFilter(res, "Other") {
		t.Errorf("parseRunFilter(TestX) matches wrong set")
	}

	// Trailing slash: no subtest filter is imposed.
	res, err = parseRunFilter("TestX/")
	if err != nil {
		t.Fatal(err)
	}
	if !matchRunFilter(res, "TestX") || matchRunFilter(res, "Other") {
		t.Errorf("parseRunFilter(TestX/) matches wrong set")
	}

	// Top-level alternation without any slash: either branch matches.
	res, err = parseRunFilter("TestA|TestB")
	if err != nil {
		t.Fatal(err)
	}
	if !matchRunFilter(res, "TestA") || !matchRunFilter(res, "TestB") || matchRunFilter(res, "TestC") {
		t.Errorf("parseRunFilter(TestA|TestB) matches wrong set")
	}

	// A slash inside brackets is not a separator: single element.
	res, err = parseRunFilter("a[/]b")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !matchRunFilter(res, "a/b") {
		t.Errorf("parseRunFilter(a[/]b) = %v, want one regex matching a/b", res)
	}

	// An invalid element regex is a hard error, like `go test`.
	if _, err := parseRunFilter("["); err == nil || !strings.Contains(err.Error(), "invalid -run regexp") {
		t.Errorf("parseRunFilter([) = %v, want an invalid-regexp error", err)
	}

	// A nil filter selects everything.
	if !matchRunFilter(nil, "Anything") {
		t.Errorf("matchRunFilter(nil, ...) = false, want true")
	}
}

// TestRewriteRunElement pins the space-to-underscore rewrite `go test`
// applies to pattern elements (and subtest names): a pattern written
// with the underscore form must match the space form's compiled regex.
func TestRewriteRunElement(t *testing.T) {
	if got := rewriteRunElement("my sub test"); got != "my_sub_test" {
		t.Errorf("rewriteRunElement(my sub test) = %q, want my_sub_test", got)
	}
	if got := rewriteRunElement("plain"); got != "plain" {
		t.Errorf("rewriteRunElement(plain) = %q, want plain", got)
	}
}
