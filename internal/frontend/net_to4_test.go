package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIPTo4MappedPrefixOnly pins gc parity for the synthetic net.IP.To4
// (review-1705 P1-B): a 16-byte IP converts only with the IPv4-mapped
// prefix ::ffff:0:0/96. The all-zero 4-in-6 form ::a.b.c.d (and ::, ::1)
// is NOT an IPv4 address and must return nil, matching gc net/ip.go.
// The len-4 fast path is preserved. Expectations verified against gc
// go1.27.1 (net.ParseIP("::1.2.3.4").To4() == nil;
// net.ParseIP("::ffff:1.2.3.4").To4() == 1.2.3.4).
//
// The synthetic net package is source text (syntheticNetSource), not Go
// code in this package, so the test extracts the exact shipped To4 and
// its dependencies from that text and executes them with gc: a regression
// to the all-zero acceptance fails this test.
func TestIPTo4MappedPrefixOnly(t *testing.T) {
	src := extractTo4Program(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module to4check\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	cmd := exec.Command(goBin, "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run of extracted To4 program failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ALL PASS") {
		t.Fatalf("extracted To4 program disagrees with gc:\n%s", out)
	}
}

// to4Decls are the synthetic net declarations the extracted program
// needs: To4 itself, the IP type and its String form (used to report
// results), the mapped-prefix helper, and the ParseIP family so the
// parsed-string cases exercise the shipped parser too.
var to4Decls = []string{
	"IP", "To4", "String", "ipToString", "itoa",
	"__isV4InV6Prefix",
	"ParseIP", "parseIPv4", "parseIPv6",
	"__parseIPv6Groups", "parseIPv6GroupsPair",
}

// extractTo4Program slices the named declarations out of
// syntheticNetSource verbatim and wraps them in a main package that
// runs the gc-verified case table.
func extractTo4Program(t *testing.T) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "net.go", syntheticNetSource, 0)
	if err != nil {
		t.Fatalf("syntheticNetSource does not parse: %v", err)
	}
	want := map[string]bool{}
	for _, name := range to4Decls {
		want[name] = true
	}
	var decls []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && want[d.Name.Name] {
				decls = append(decls, sliceDecl(fset, d))
				delete(want, d.Name.Name)
			}
			if d.Recv != nil && want[d.Name.Name] {
				decls = append(decls, sliceDecl(fset, d))
				delete(want, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && want[ts.Name.Name] {
					decls = append(decls, sliceDecl(fset, d))
					delete(want, ts.Name.Name)
				}
			}
		}
	}
	for name := range want {
		t.Fatalf("synthetic net source is missing declaration %q (extraction incomplete)", name)
	}
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\n")
	for _, d := range decls {
		b.WriteString(d)
		b.WriteString("\n\n")
	}
	b.WriteString(to4Main)
	return b.String()
}

func sliceDecl(fset *token.FileSet, d ast.Decl) string {
	start := fset.Position(d.Pos()).Offset
	end := fset.Position(d.End()).Offset
	return syntheticNetSource[start:end]
}

// to4Main runs the gc-verified case table against the extracted To4.
const to4Main = `
func main() {
	failed := false
	check := func(label string, got IP, want string) {
		if want == "" {
			if got != nil {
				fmt.Printf("FAIL %s: got %v, want nil\n", label, got)
				failed = true
			}
			return
		}
		if got == nil || len(got) != 4 || got.String() != want {
			fmt.Printf("FAIL %s: got %v, want %s\n", label, got, want)
			failed = true
		}
	}
	parsed := []struct {
		in   string
		want string
	}{
		{"::1.2.3.4", ""},
		{"::ffff:1.2.3.4", "1.2.3.4"},
		{"::ffff:0.0.0.0", "0.0.0.0"},
		{"1.2.3.4", "1.2.3.4"},
		{"::", ""},
		{"::1", ""},
		{"2001:db8::1", ""},
		{"::ffff:0:1.2.3.4", ""},
	}
	for _, tc := range parsed {
		ip := ParseIP(tc.in)
		if ip == nil {
			fmt.Printf("FAIL ParseIP(%q) = nil\n", tc.in)
			failed = true
			continue
		}
		check("ParseIP("+tc.in+")", ip.To4(), tc.want)
	}
	raw := []struct {
		in   IP
		want string
	}{
		{IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4}, ""},
		{IP{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 1, 2, 3, 4}, "1.2.3.4"},
		{IP{0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0, 0, 1, 2, 3, 4}, ""},
		{IP{1, 2, 3, 4}, "1.2.3.4"},
		{IP{1, 2, 3}, ""},
		{IP{}, ""},
		{nil, ""},
	}
	for _, tc := range raw {
		check("raw "+tc.in.String(), tc.in.To4(), tc.want)
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("ALL PASS")
}
`
