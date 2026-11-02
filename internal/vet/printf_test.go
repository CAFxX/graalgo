package vet

import (
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// printfDiagnostics runs all analyzers on src and returns only the
// printf analyzer's messages, so these tests are not coupled to other
// analyzers' diagnostics on the same snippet.
func printfDiagnostics(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "p.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	ds, err := Run([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range ds {
		m := d.Message
		if strings.Contains(m, "expects ") ||
			strings.Contains(m, "missing argument") ||
			strings.Contains(m, "missing verb") ||
			strings.Contains(m, "too many arguments") ||
			strings.Contains(m, "invalid argument index") ||
			strings.Contains(m, "unknown verb") ||
			strings.Contains(m, "width argument") ||
			strings.Contains(m, "precision argument") ||
			strings.Contains(m, "%w") {
			out = append(out, m)
		}
	}
	return out
}

// mainSrc builds a package-main source with the given imports, top-level
// declarations, and main body.
func mainSrc(imports, decls, body string) string {
	return "package main\n\nimport (\n" + imports + ")\n\n" + decls + "\nfunc main() {\n" + body + "}\n"
}

func TestPrintfValidCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		// V1: scan family takes pointers; the pointed-to type is checked.
		{"scanf int+string", mainSrc("\t\"fmt\"\n", "", "\tvar n int\n\tvar s string\n\t_, _ = fmt.Sscanf(\"42 hi\", \"%d %s\", &n, &s)\n")},
		{"scanf float+bool", mainSrc("\t\"fmt\"\n", "", "\tvar f float64\n\tvar b bool\n\t_, _ = fmt.Sscanf(\"1.5 true\", \"%f %t\", &f, &b)\n")},
		{"fscanf", mainSrc("\t\"fmt\"\n\t\"strings\"\n", "", "\tvar n int\n\t_, _ = fmt.Fscanf(strings.NewReader(\"42\"), \"%d\", &n)\n")},
		{"scanf named int", mainSrc("\t\"fmt\"\n", "type N int\n", "\tvar n N\n\t_, _ = fmt.Sscanf(\"42\", \"%d\", &n)\n")},
		// Scan excess args are not reported (excess check is print-only).
		{"scanf excess ignored", mainSrc("\t\"fmt\"\n", "", "\tvar n, m int\n\t_, _ = fmt.Sscanf(\"42\", \"%d\", &n, &m)\n")},

		// V2: width-star and precision-star args are consumed before the value.
		{"width star", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%*d\", 5, 42)\n")},
		{"precision star", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%.*f\", 2, 1.5)\n")},
		{"width+precision stars", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%*.*f\", 5, 2, 1.5)\n")},
		{"width digits", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%5d\", 42)\n")},
		{"percent verb consumes nothing", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%% %d\", 42)\n")},

		// V3: untyped constants and complex values for float verbs.
		{"float literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%f\", 1.5)\n")},
		{"g literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%g\", 2.5)\n")},
		{"e literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%e\", 1e3)\n")},
		{"complex literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%f\", 1+2i)\n")},
		{"complex128 var", mainSrc("\t\"fmt\"\n", "", "\tvar c complex128\n\t_ = fmt.Sprintf(\"%f\", c)\n")},
		{"bool literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%t\", true)\n")},
		{"string literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%s\", \"hi\")\n")},

		// V4: %s/%q accept errors, Stringers, named []byte, %q on integers.
		{"s error value", mainSrc("\t\"errors\"\n\t\"fmt\"\n", "", "\terr := errors.New(\"boom\")\n\t_ = fmt.Sprintf(\"%s\", err)\n")},
		{"s stringer value", mainSrc("\t\"fmt\"\n", "type S struct{}\n\nfunc (S) String() string { return \"s\" }\n", "\t_ = fmt.Sprintf(\"%s\", S{})\n")},
		{"s stringer pointer", mainSrc("\t\"fmt\"\n", "type S struct{}\n\nfunc (S) String() string { return \"s\" }\n", "\t_ = fmt.Sprintf(\"%s\", &S{})\n")},
		{"s stringer interface", mainSrc("\t\"fmt\"\n", "", "\tvar s fmt.Stringer\n\t_ = fmt.Sprintf(\"%s\", s)\n")},
		{"s any value", mainSrc("\t\"fmt\"\n", "", "\tvar x any\n\t_ = fmt.Sprintf(\"%s\", x)\n")},
		{"s named byte slice", mainSrc("\t\"fmt\"\n", "type B []byte\n", "\tvar b B\n\t_ = fmt.Sprintf(\"%s\", b)\n")},
		{"q int", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", 65)\n")},
		{"q rune literal", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", 'a')\n")},
		{"q string", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", \"hi\")\n")},
		{"q byte slice", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", []byte(\"hi\"))\n")},
		{"errorf w", mainSrc("\t\"errors\"\n\t\"fmt\"\n", "", "\terr := errors.New(\"boom\")\n\t_ = fmt.Errorf(\"wrap: %w\", err)\n")},

		// P2-D2: indexed verbs, Appendf, testing helpers.
		{"indexed in order", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[1]s %[2]d\", \"a\", 3)\n")},
		{"indexed out of order", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[2]d %[1]s\", \"a\", 3)\n")},
		{"sequential after indexed uses next arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[2]d %d\", \"a\", 3, 4)\n")},
		// Upstream go vet skips the arity check for indexed formats,
		// so a trailing argument after an indexed verb is not excess.
		{"indexed trailing arg ignored", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[1]d\", 1, 2)\n")},
		{"appendf", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Appendf(nil, \"%d\", 42)\n")},
		{"testing logf", "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Logf(\"%d\", 42)\n}\n"},
		{"testing skipf", "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skipf(\"%d\", 42)\n}\n"},
		{"testing errorf", "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Errorf(\"%d\", 42)\n}\n"},
		// A fmt.Formatter handles every verb.
		{"formatter any verb", mainSrc("\t\"fmt\"\n", "type F struct{}\n\nfunc (F) Format(s fmt.State, v rune) {}\n", "\t_ = fmt.Sprintf(\"%d\", F{})\n")},
		// A fmt.Scanner handles every scan verb.
		{"scanner scan verb", mainSrc("\t\"fmt\"\n", "type SC struct{}\n\nfunc (*SC) Scan(s fmt.State, v rune) error { return nil }\n", "\tvar sc SC\n\t_, _ = fmt.Sscanf(\"42\", \"%d\", &sc)\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := printfDiagnostics(t, tc.src); len(got) != 0 {
				t.Errorf("expected no printf diagnostics, got %v", got)
			}
		})
	}
}

// TestPrintfKindHelpers pins the kind tables directly. This matters for
// V3: go/types defaults an untyped constant passed to a ...any parameter
// to its default type (1.5 arrives as float64, not UntypedFloat), so the
// end-to-end literal cases above cannot exercise the UntypedFloat branch
// on their own; this test can, and it failed before the fix.
func TestPrintfKindHelpers(t *testing.T) {
	for _, k := range []struct {
		kind types.BasicKind
		want bool
	}{
		{types.Float32, true},
		{types.Float64, true},
		{types.Complex64, true},
		{types.Complex128, true},
		{types.UntypedFloat, true},
		{types.UntypedComplex, true},
		{types.Int, false},
		{types.String, false},
	} {
		if got := isFloatKind(k.kind); got != k.want {
			t.Errorf("isFloatKind(%v) = %v, want %v", k.kind, got, k.want)
		}
	}
	if !isBoolKind(types.UntypedBool) {
		t.Errorf("isBoolKind(UntypedBool) = false, want true")
	}
}

func TestPrintfInvalidCases(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		// V1: dereferenced scan arg types are still checked.
		{"scanf int into string ptr", mainSrc("\t\"fmt\"\n", "", "\tvar s string\n\t_, _ = fmt.Sscanf(\"42\", \"%d\", &s)\n"), "expects integer"},
		{"scanf string into int ptr", mainSrc("\t\"fmt\"\n", "", "\tvar n int\n\t_, _ = fmt.Sscanf(\"hi\", \"%s\", &n)\n"), "expects string"},
		// V2: star args checked as integers, real value checked as value.
		{"width star value wrong", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%*d\", 5, \"x\")\n"), "expects integer"},
		{"width star arg wrong", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%*d\", \"x\", 42)\n"), "width argument expects integer"},
		{"precision star arg wrong", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%.*f\", \"x\", 1.5)\n"), "precision argument expects integer"},
		{"precision star value missing", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%.*f\", 2)\n"), "missing argument"},
		// V3 family: genuinely wrong float/bool/string args still flagged.
		{"float verb string arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%f\", \"x\")\n"), "expects floating point"},
		{"float verb int arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%f\", 42)\n"), "expects floating point"},
		{"bool verb int arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%t\", 1)\n"), "expects boolean"},
		// V4: genuinely wrong %s/%q args still flagged.
		{"s int arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%s\", 42)\n"), "expects string"},
		{"q float arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", 1.5)\n"), "expects string"},
		{"q bool arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%q\", true)\n"), "expects string"},
		// Pre-existing behaviour that must not regress.
		{"d string arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%d\", \"hello\")\n"), "expects integer"},
		{"missing arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%d %d\", 1)\n"), "missing argument"},
		// P2-D1: excess print-family args.
		{"excess arg", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%d\", 1, 2)\n"), "too many arguments"},
		{"excess args", mainSrc("\t\"fmt\"\n", "", "\t_, _ = fmt.Printf(\"%d\", 1, 2, 3)\n"), "too many arguments"},
		// P2-D2: indexed verbs, unknown verbs, misplaced %w, helpers.
		{"indexed wrong type", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[1]s\", 42)\n"), "expects string"},
		{"sequential after indexed wrong type", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[1]d %d\", 1, \"x\")\n"), "expects integer"},
		{"indexed out of range", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%[2]d\", 1)\n"), "invalid argument index"},
		{"unknown verb", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%y\", 1)\n"), "unknown verb"},
		{"missing verb", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Sprintf(\"%\")\n"), "missing verb"},
		{"w outside errorf", mainSrc("\t\"errors\"\n\t\"fmt\"\n", "", "\terr := errors.New(\"boom\")\n\t_ = fmt.Sprintf(\"oops %w\", err)\n"), "%w"},
		{"appendf wrong type", mainSrc("\t\"fmt\"\n", "", "\t_ = fmt.Appendf(nil, \"%d\", \"x\")\n"), "expects integer"},
		{"testing logf wrong type", "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Logf(\"%d\", \"x\")\n}\n", "expects integer"},
		{"testing skipf wrong type", "package main\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {\n\tt.Skipf(\"%d\", \"x\")\n}\n", "expects integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := printfDiagnostics(t, tc.src)
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("expected diagnostic containing %q, got %v", tc.want, got)
			}
		})
	}
}
