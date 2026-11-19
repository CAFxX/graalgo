package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSyntheticNetTo4GcParity is the differential test for REVIEW-1723
// P1 net-to4: the synthetic net.IP.To4 (internal/frontend/synth_net.go,
// embedded as syntheticNetSource) must match gc's net.IP.To4 exactly.
//
// gc (go1.27.1 src/net/ip.go) maps ONLY the ::ffff:0:0/96 prefix: a 16-byte
// address whose first 10 bytes are zero and bytes 10,11 are 0xff,0xff.
// The pre-fix synthetic To4 ALSO accepted the all-zero prefix (::/96),
// so ::1.2.3.4, :: and ::1 mapped to bogus IPv4 addresses (0.0.0.1 for
// ::1) and broke tcp6 resolution / AF selection.
//
// The test extracts the LIVE To4 + __isV4InV6Prefix declarations from
// syntheticNetSource (never a hand copy — a copy could pass while the
// shipped source is broken), emits them into a temp program, runs it
// under the gc toolchain, and compares every line against the oracle:
// real gc net.IP.To4 called in-process on the same inputs.
//
// Falsifiability: against the pre-fix To4 the all-zero-prefix cases
// (::1.2.3.4, ::, ::1) fail; the mapped cases still pass, so the test
// discriminates the fix rather than the harness.
//
// Filename note: this file MUST NOT be named synth_net_to4_test.go —
// .github/scripts/check_no_bridges.py Rule 1 flags ANY new file whose
// path contains "synth_" and ends .go as a new synthetic file. That name
// already failed the gate for sibling fix acd4ad8e (CI 44028225362).
func TestSyntheticNetTo4GcParity(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "net.go", syntheticNetSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("synthetic net source does not parse: %v", err)
	}
	var to4, prefix *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fd.Recv != nil && fd.Name.Name == "To4" {
			to4 = fd
		}
		if fd.Recv == nil && fd.Name.Name == "__isV4InV6Prefix" {
			prefix = fd
		}
	}
	if to4 == nil {
		t.Fatal("synthetic net source has no (IP) To4 method — test cannot pin the fix")
	}
	if prefix == nil {
		t.Fatal("synthetic net source has no __isV4InV6Prefix — To4 fix dependency missing")
	}
	var prog bytes.Buffer
	prog.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"net\"\n)\n\ntype IP []byte\n\n")
	for _, fd := range []*ast.FuncDecl{prefix, to4} {
		if err := format.Node(&prog, fset, fd); err != nil {
			t.Fatalf("format %s: %v", fd.Name.Name, err)
		}
		prog.WriteString("\n\n")
	}
	prog.WriteString(`func show(ip IP) string {
	if ip == nil {
		return "nil"
	}
	return net.IP(ip).String()
}

func main() {
	cases := []IP{
		{1, 2, 3, 4},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 1, 2, 3, 4},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xfe, 1, 2, 3, 4},
		nil,
		{1, 2, 3},
		{1, 2, 3, 4, 5},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 1, 2, 3},
	}
	for _, ip := range cases {
		fmt.Println(show(ip.To4()))
	}
}
`)

	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, prog.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
		if _, err := os.Stat(goBin); err != nil {
			t.Fatalf("no go toolchain for the differential run: %v", err)
		}
	}
	cmd := exec.Command(goBin, "run", mainPath)
	cmd.Env = append(os.Environ(),
		"GO111MODULE=off",
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(dir, "gocache"),
		"GOTMPDIR="+dir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run of extracted To4 failed: %v\n%s", err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")

	inputs := []net.IP{
		{1, 2, 3, 4},
		net.ParseIP("::ffff:1.2.3.4").To16(),
		net.ParseIP("::ffff:0.0.0.0").To16(),
		net.ParseIP("::1.2.3.4").To16(),
		net.ParseIP("::").To16(),
		net.ParseIP("::1").To16(),
		net.ParseIP("2001:db8::1").To16(),
		net.ParseIP("::fffe:1.2.3.4").To16(),
		nil,
		{1, 2, 3},
		{1, 2, 3, 4, 5},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 1, 2, 3},
	}
	if len(got) != len(inputs) {
		t.Fatalf("expected %d output lines, got %d:\n%s", len(inputs), len(got), out)
	}
	for i, in := range inputs {
		want := "nil"
		if v := in.To4(); v != nil {
			want = v.String()
		}
		if got[i] != want {
			t.Errorf("case %d (%v): synthetic To4 = %q, gc To4 = %q", i, net.IP(in), got[i], want)
		}
	}
}

// TestSyntheticNetTo4UsesV4InV6Prefix pins the fix's mechanism: To4 must
// route the 16-byte case through __isV4InV6Prefix (the single shared
// definition of the ::ffff:0:0/96 prefix, also used by Mask) instead of
// re-implementing prefix matching inline, where the all-zero-prefix
// acceptance bug lived.
func TestSyntheticNetTo4UsesV4InV6Prefix(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "net.go", syntheticNetSource, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("synthetic net source does not parse: %v", err)
	}
	var to4 *ast.FuncDecl
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv != nil && fd.Name.Name == "To4" {
			to4 = fd
		}
	}
	if to4 == nil {
		t.Fatal("synthetic net source has no (IP) To4 method")
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, to4); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "__isV4InV6Prefix") {
		t.Errorf("To4 does not route through __isV4InV6Prefix:\n%s", body)
	}
	if strings.Contains(body, "ip[10] == 0") {
		t.Errorf("To4 still contains an all-zero-prefix acceptance branch:\n%s", body)
	}
}
