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
	// Minimal fake frontend JSON carrying the expected wire version.
	// Emit validates it (see the Reject tests below).
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

// TestEmitRejectsMalformedWireJSON is the negative test for QN-6: a
// document that is not JSON must fail the build loudly, not embed.
func TestEmitRejectsMalformedWireJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/x",
		WireJSON:    []byte(`{"frontend": "v1", truncated`),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid frontend output") {
		t.Errorf("Emit with malformed JSON = %v, want invalid-output error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("Emit wrote an artifact despite the validation failure")
	}
}

// TestEmitRejectsVersionSkew is the negative test for QN-6: wire JSON
// from a frontend speaking a different protocol version must fail the
// build loudly — never produce an artifact that dies at run time.
func TestEmitRejectsVersionSkew(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/x",
		WireJSON:    []byte(`{"frontend":"v99"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "wire protocol version mismatch") {
		t.Errorf("Emit with skewed version = %v, want version-mismatch error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("Emit wrote an artifact despite the validation failure")
	}
}

// TestEmitRejectsMissingVersion pins the wrong-schema case: syntactically
// valid JSON with no "frontend" field is not a frontend document.
func TestEmitRejectsMissingVersion(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prog")
	err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: "/x",
		WireJSON:    []byte(`{"functions":[]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "missing \"frontend\"") {
		t.Errorf("Emit with no frontend field = %v, want missing-field error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("Emit wrote an artifact despite the validation failure")
	}
}

// TestEmitAcceptsExpectedWireVersion is the positive test for QN-6: a
// well-formed document at the driver's expected protocol version embeds
// cleanly.
func TestEmitAcceptsExpectedWireVersion(t *testing.T) {
	out := filepath.Join(t.TempDir(), "prog")
	wireJSON := []byte(`{"frontend":"v1","package":{"name":"main"}}`)
	if err := Emit(Artifact{OutputPath: out, GraalgoRoot: "/x", WireJSON: wireJSON}); err != nil {
		t.Fatalf("Emit with valid v1 JSON: %v", err)
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

// TestWireProtocolVersionMatchesFrontend pins the driver's expected wire
// protocol version to the version the frontend actually emits.
// internal/frontend is a separate Go module (graalgo-frontend), so the
// constant cannot be shared by import; instead this test extracts the
// frontendVersion literal from the frontend's source with go/ast and
// asserts equality with wireProtocolVersion. A version bump on either
// side without the other fails loudly here instead of drifting silently
// (docs/arch/frontend-v1.md §8 governs bumps).
func TestWireProtocolVersionMatchesFrontend(t *testing.T) {
	// Tests run with the package directory as the working directory.
	const frontendSrc = "../frontend/export.go"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, frontendSrc, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", frontendSrc, err)
	}
	var emitted string
	found := false
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "frontendVersion" || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("frontendVersion in %s is not a string literal", frontendSrc)
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote frontendVersion: %v", err)
				}
				emitted, found = v, true
			}
		}
	}
	if !found {
		t.Fatalf("const frontendVersion not found in %s", frontendSrc)
	}
	if emitted != wireProtocolVersion {
		t.Errorf("wire protocol version skew: frontend emits %q, driver expects %q",
			emitted, wireProtocolVersion)
	}
}
