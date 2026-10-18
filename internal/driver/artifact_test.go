package driver

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEmitProducesBinary verifies that Emit produces a real executable
// binary (not a shell script) embedding the frontend output.
func TestEmitProducesBinary(t *testing.T) {
	// Minimal fake frontend JSON. Emit does not validate the content;
	// it only packages it.
	wireJSON := []byte(`{"frontend":"v1","functions":[]}`)

	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/opt/graalgo",
		WireJSON:    wireJSON,
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("artifact %s is not executable (mode %v)", out, fi.Mode())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// Must be an ELF binary, not a shell script.
	if !bytes.HasPrefix(data, []byte("\x7fELF")) {
		t.Errorf("artifact is not an ELF binary (first bytes: %q)", data[:min(32, len(data))])
	}
	if bytes.HasPrefix(data, []byte("#!")) {
		t.Errorf("artifact is a script (shebang), not a binary")
	}
	// The graalgo root must be baked into the binary.
	if !bytes.Contains(data, []byte("/opt/graalgo")) {
		t.Errorf("artifact does not bake in the graalgo root")
	}
	// The launcher must honor GRAALGO_HOME (check the string is present
	// in the binary; the actual env-var logic is exercised at run time).
	if !bytes.Contains(data, []byte("GRAALGO_HOME")) {
		t.Errorf("artifact does not reference GRAALGO_HOME")
	}
	// The launcher must use --wire-input (not re-run the frontend).
	if !bytes.Contains(data, []byte("--wire-input")) {
		t.Errorf("artifact does not use --wire-input")
	}
}

func TestEmitNoOutput(t *testing.T) {
	err := Emit(Artifact{GraalgoRoot: "/x", WireJSON: []byte("{}")})
	if err == nil || !strings.Contains(err.Error(), "output") {
		t.Errorf("Emit with no output path = %v, want output error", err)
	}
}

func TestEmitNoWireJSON(t *testing.T) {
	err := Emit(Artifact{OutputPath: filepath.Join(t.TempDir(), "prog"), GraalgoRoot: "/x"})
	if err == nil || !strings.Contains(err.Error(), "frontend output") {
		t.Errorf("Emit with no wire JSON = %v, want frontend output error", err)
	}
}

func TestEmitNoRoot(t *testing.T) {
	err := Emit(Artifact{OutputPath: filepath.Join(t.TempDir(), "prog"), WireJSON: []byte("{}")})
	if err == nil || !strings.Contains(err.Error(), "graalgo root") {
		t.Errorf("Emit with no graalgo root = %v, want root error", err)
	}
}

func TestDefaultOutputName(t *testing.T) {
	// Explicit -o wins.
	if got, err := DefaultOutputName("custom", nil); err != nil || got != "custom" {
		t.Errorf("explicit -o: got %q, %v", got, err)
	}
	// First .go file: base name minus extension.
	if got, err := DefaultOutputName("", []string{"hello.go"}); err != nil || got != "hello" {
		t.Errorf("file operand: got %q, %v", got, err)
	}
	if got, err := DefaultOutputName("", []string{"/tmp/x/myprog.go"}); err != nil || got != "myprog" {
		t.Errorf("file path operand: got %q, %v", got, err)
	}
	// Directory operand: base of the directory.
	if got, err := DefaultOutputName("", []string{"./mydir/"}); err != nil || got != "mydir" {
		t.Errorf("dir operand: got %q, %v", got, err)
	}
	// No operands: base of the current directory.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "coolproj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if got, err := DefaultOutputName("", nil); err != nil || got != "coolproj" {
		t.Errorf("no operands: got %q, %v", got, err)
	}
}

// TestEmitBinaryRuns is a smoke test that the produced binary actually
// executes. It uses a fake backend script that records its arguments,
// verifying the launcher passes --wire-input correctly. This does not
// require a real graalgo backend.
func TestEmitBinaryRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary execution test in short mode")
	}

	tmpDir := t.TempDir()

	// Fake backend: a script that writes its args to a file, copies the
	// wire-input file aside (the launcher deletes it on exit), and exits 0.
	// We fake GRAALGO_HOME to point at a dir containing
	// internal/backend/graalgo-sync-run.
	fakeRoot := filepath.Join(tmpDir, "fake-root")
	backendDir := filepath.Join(fakeRoot, "internal", "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(tmpDir, "args.txt")
	wireCopy := filepath.Join(tmpDir, "wire-copy.json")
	backendScript := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"cp \"$2\" " + wireCopy + "\n" + // $2 is the --wire-input path
		"exit 0\n"
	backendPath := filepath.Join(backendDir, "graalgo-sync-run")
	if err := os.WriteFile(backendPath, []byte(backendScript), 0o755); err != nil {
		t.Fatal(err)
	}

	wireJSON := []byte(`{"frontend":"v1"}`)
	out := filepath.Join(tmpDir, "prog")
	if err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/nonexistent-baked-in",
		WireJSON:    wireJSON,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Run the artifact with GRAALGO_HOME pointing at the fake root.
	cmd := exec.Command(out, "hello", "world")
	cmd.Env = append(os.Environ(), "GRAALGO_HOME="+fakeRoot)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("artifact run failed: %v\n%s", err, out)
	}

	// Verify the backend received --wire-input with a real JSON file.
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("backend was not invoked (no args file): %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	if len(args) < 4 {
		t.Fatalf("backend args = %v, want at least [--wire-input FILE -- argv0 ...]", args)
	}
	if args[0] != "--wire-input" {
		t.Errorf("args[0] = %q, want --wire-input", args[0])
	}
	// The backend copied the wire file before the launcher cleaned up.
	wireData, err := os.ReadFile(wireCopy)
	if err != nil {
		t.Errorf("cannot read preserved wire-input copy: %v", err)
	} else if string(wireData) != string(wireJSON) {
		t.Errorf("wire-input content mismatch: got %q, want %q", wireData, wireJSON)
	}
	if args[2] != "--" {
		t.Errorf("args[2] = %q, want --", args[2])
	}
	// argv[0] for the program should be the artifact's absolute path.
	if !filepath.IsAbs(args[3]) {
		t.Errorf("program argv[0] = %q, want absolute path", args[3])
	}
	// User args forwarded.
	if len(args) < 6 || args[4] != "hello" || args[5] != "world" {
		t.Errorf("user args not forwarded: %v", args)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestLauncherSourceHostileRoot is a regression test for the
// format-string injection in launcher source generation. The graalgo
// root is user-controlled (GRAALGO_HOME), and the old "%s" interpolation
// let a crafted root break out of the string literal and inject arbitrary
// Go code that `go build` then compiled. Every hostile root must still
// produce source that parses, with the graalgoRoot constant holding
// exactly the input root.
func TestLauncherSourceHostileRoot(t *testing.T) {
	roots := []string{
		`/tmp/graal"root`,     // embedded double quote
		"/tmp/line1\nline2",   // newline
		"/tmp/line1\r\nline2", // CRLF
		`${GRAALGO_HOME}`,     // shell expansion syntax
		`C:\graal\root`,       // backslashes
		`/tmp/root\`,          // trailing backslash (escapes the closing quote under "%s")
		"/tmp/100%s/root",     // literal percent sequence (must not be re-scanned)
		"/tmp/100%q/root",     // literal %q (must not be treated as a verb)
		`x"; import("os"); func init(){ os.Exit(1) } //`, // code-injection attempt
		"/tmp/gråälgo ✓",                                 // printable unicode
		"/tmp/root\x00bin",                               // NUL byte
		"",                                               // empty (Emit rejects it, but the generator must stay parseable)
	}
	for _, root := range roots {
		src := launcherSource(root)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "launcher.go", src, 0)
		if err != nil {
			t.Errorf("launcherSource(%q): generated source does not parse: %v", root, err)
			continue
		}
		got, ok := graalgoRootConst(f)
		if !ok {
			t.Errorf("launcherSource(%q): graalgoRoot const not found in generated source", root)
			continue
		}
		if got != root {
			t.Errorf("launcherSource(%q): graalgoRoot = %q, want exact input", root, got)
		}
	}
}

// graalgoRootConst finds `const graalgoRoot = <string literal>` in the
// parsed launcher source and returns the literal's value.
func graalgoRootConst(f *ast.File) (string, bool) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "graalgoRoot" || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return "", false
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return "", false
			}
			return v, true
		}
	}
	return "", false
}
