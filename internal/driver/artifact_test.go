package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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

// writeStrictFakeBackend installs a fake graalgo-sync-run under fakeRoot
// that enforces the REAL backend arg contract from P3SyncMain (not a
// permissive mock): only `--wire-input=FILE` is accepted, a bare
// `--wire-input` is rejected as an unknown flag, `--` must follow, and
// the first post-`--` arg is the program's argv[0]. It records its args
// to argsFile and copies the wire file aside (the launcher deletes it
// on exit) to wireCopy, then exits 0. Any contract violation exits 1,
// so a launcher regression fails the artifact run loudly instead of
// passing against a mock that accepts anything.
func writeStrictFakeBackend(t *testing.T, fakeRoot, argsFile, wireCopy string) {
	t.Helper()
	backendDir := filepath.Join(fakeRoot, "internal", "backend")
	if err := os.MkdirAll(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	backendScript := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		// $1 must be exactly --wire-input=<non-empty path>.
		"wire=\"${1#--wire-input=}\"\n" +
		"if [ \"$wire\" = \"$1\" ] || [ -z \"$wire\" ]; then\n" +
		"  echo \"fake-backend: unknown flag or bad --wire-input: $1\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"if [ \"$2\" != \"--\" ]; then\n" +
		"  echo \"fake-backend: expected -- after --wire-input\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"cp \"$wire\" " + wireCopy + "\n" +
		"exit 0\n"
	backendPath := filepath.Join(backendDir, "graalgo-sync-run")
	if err := os.WriteFile(backendPath, []byte(backendScript), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestStrictFakeBackendRejectsBareWireInput pins the strict fake
// backend's fidelity to P3SyncMain: the separate `--wire-input FILE`
// form must be rejected, and the `--wire-input=FILE` form accepted.
// Without this, a future edit could silently weaken the mock back into
// accepting anything, and TestEmitBinaryRuns would stop testing the
// real contract.
func TestStrictFakeBackendRejectsBareWireInput(t *testing.T) {
	tmpDir := t.TempDir()
	fakeRoot := filepath.Join(tmpDir, "fake-root")
	argsFile := filepath.Join(tmpDir, "args.txt")
	wireCopy := filepath.Join(tmpDir, "wire-copy.json")
	writeStrictFakeBackend(t, fakeRoot, argsFile, wireCopy)
	backendPath := filepath.Join(fakeRoot, "internal", "backend", "graalgo-sync-run")

	wirePath := filepath.Join(tmpDir, "wire.json")
	if err := os.WriteFile(wirePath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bare separate form: must fail (this is the O5 bug shape).
	if out, err := exec.Command(backendPath, "--wire-input", wirePath, "--", "/prog").CombinedOutput(); err == nil {
		t.Errorf("bare --wire-input accepted, want rejection (output: %s)", out)
	}
	// Equals form: must succeed and copy the wire file.
	if out, err := exec.Command(backendPath, "--wire-input="+wirePath, "--", "/prog").CombinedOutput(); err != nil {
		t.Errorf("equals --wire-input rejected: %v\n%s", err, out)
	}
	if _, err := os.Stat(wireCopy); err != nil {
		t.Errorf("equals form did not copy wire file: %v", err)
	}
}

// TestEmitBinaryRuns is a smoke test that the produced binary actually
// executes. It uses a fake backend script that records its arguments,
// verifying the launcher passes --wire-input correctly. This does not
// require a real graalgo backend, but the fake enforces the real
// backend's arg contract (see writeStrictFakeBackend), so the launcher
// argv is verified against P3SyncMain's parsing rules, not a mock that
// accepts anything.
func TestEmitBinaryRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary execution test in short mode")
	}

	tmpDir := t.TempDir()

	// Fake backend: enforces the real P3SyncMain arg contract.
	// We fake GRAALGO_HOME to point at a dir containing
	// internal/backend/graalgo-sync-run.
	fakeRoot := filepath.Join(tmpDir, "fake-root")
	argsFile := filepath.Join(tmpDir, "args.txt")
	wireCopy := filepath.Join(tmpDir, "wire-copy.json")
	writeStrictFakeBackend(t, fakeRoot, argsFile, wireCopy)

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

	// Verify the backend received --wire-input=<file> with a real JSON
	// file, in the exact shape P3SyncMain accepts.
	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("backend was not invoked (no args file): %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	if len(args) < 5 {
		t.Fatalf("backend args = %v, want [--wire-input=FILE -- argv0 hello world]", args)
	}
	if !strings.HasPrefix(args[0], "--wire-input=") || len(args[0]) == len("--wire-input=") {
		t.Errorf("args[0] = %q, want --wire-input=<file>", args[0])
	}
	// The backend copied the wire file before the launcher cleaned up.
	wireData, err := os.ReadFile(wireCopy)
	if err != nil {
		t.Errorf("cannot read preserved wire-input copy: %v", err)
	} else if string(wireData) != string(wireJSON) {
		t.Errorf("wire-input content mismatch: got %q, want %q", wireData, wireJSON)
	}
	if args[1] != "--" {
		t.Errorf("args[1] = %q, want --", args[1])
	}
	// argv[0] for the program should be the artifact's absolute path.
	if !filepath.IsAbs(args[2]) {
		t.Errorf("program argv[0] = %q, want absolute path", args[2])
	}
	// User args forwarded.
	if args[3] != "hello" || args[4] != "world" {
		t.Errorf("user args not forwarded: %v", args)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
