package driver

import (
	"bytes"
	"fmt"
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

// TestEmitRelativeOutputPath is a regression test for the flagship
// command: `graalgo build` without -o derives a BARE relative output
// name (DefaultOutputName returns e.g. "prog"). Emit compiles the
// launcher in its own temp dir, so it must resolve a relative output
// path against the CALLER's working directory — otherwise the binary
// lands in the temp dir (deleted on return) and Emit fails with a
// confusing "cannot chmod prog: no such file or directory".
func TestEmitRelativeOutputPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode: compiles a launcher binary")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	// Bare relative name, exactly what DefaultOutputName returns when
	// -o is not given.
	if err := Emit(Artifact{
		OutputPath:  "prog",
		GraalgoRoot: "/opt/graalgo",
		WireJSON:    []byte(`{"frontend":"v1"}`),
	}); err != nil {
		t.Fatalf("Emit with relative output path: %v", err)
	}

	out := filepath.Join(tmp, "prog")
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("binary not in caller cwd: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Errorf("artifact %s is not executable", out)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("\x7fELF")) {
		t.Errorf("artifact is not an ELF binary (first bytes: %q)", data[:min(32, len(data))])
	}
}

// TestEmitEscapesHostileRoot verifies the graalgo root is baked into
// the generated launcher as a properly quoted Go string literal. A
// root containing quotes or backslashes must neither break the
// generated source nor inject code into it. Pre-fix, Emit failed here
// because the raw interpolation produced uncompilable launcher source.
func TestEmitEscapesHostileRoot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode: compiles a launcher binary")
	}

	hostile := `/opt/graal"go\dir"; rm -rf /`
	out := filepath.Join(t.TempDir(), "prog")
	if err := Emit(Artifact{
		OutputPath:  out,
		GraalgoRoot: hostile,
		WireJSON:    []byte(`{"frontend":"v1"}`),
	}); err != nil {
		t.Fatalf("Emit with hostile root: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The exact hostile value must round-trip into the binary.
	if !bytes.Contains(data, []byte(hostile)) {
		t.Errorf("artifact does not bake in the exact root %q", hostile)
	}

	// The generated source itself must parse: the template's graalgoRoot
	// line takes a pre-quoted %s, so any root round-trips syntactically.
	src := fmt.Sprintf(launcherTemplate, strconv.Quote(hostile))
	if _, err := parser.ParseFile(token.NewFileSet(), "launcher.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated launcher source does not parse: %v", err)
	}
	if want := "const graalgoRoot = " + strconv.Quote(hostile); !strings.Contains(src, want) {
		t.Errorf("generated source does not contain %q", want)
	}
}
