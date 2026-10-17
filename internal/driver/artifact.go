// Package driver implements build orchestration for the graalgo command:
// parsing build requests, resolving package files, and emitting runnable
// artifacts from validated programs.
package driver

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// wireProtocolVersion is the frontend wire protocol version this driver
// expects. It must track internal/frontend's frontendVersion
// (docs/frontend-v1.md §8 governs version bumps): a skew between the
// two means the embedded JSON is not what the backend understands.
const wireProtocolVersion = "v1"

// Artifact describes the executable artifact `graalgo build` emits.
type Artifact struct {
	// OutputPath is where the artifact binary is written.
	OutputPath string
	// GraalgoRoot is the graalgo installation the artifact runs against.
	// It is baked into the artifact at build time; GRAALGO_HOME set at
	// run time takes precedence.
	GraalgoRoot string
	// WireJSON is the frontend/v1 JSON output to embed in the artifact.
	// It is produced by running the frontend at build time (via
	// `graalgo-sync-run --emit-wire`), so the artifact does not re-run
	// the frontend on every execution.
	WireJSON []byte
}

// Emit writes the executable artifact described by a. The artifact is a
// real binary (not a shell script): a small Go launcher that embeds the
// frontend output and, at run time, feeds it to the graalgo backend via
// `--wire-input`, forwarding its own arguments to the program.
//
// Unlike `go build`, which produces a native binary via AOT compilation,
// graalgo's backend is a Truffle JIT on the JVM. A true native binary
// requires GraalVM native-image with Truffle runtime compilation
// (Phase 5 work; see docs/arch/research/T6-optimization-inventory.md).
// Until then, the artifact is a launcher binary that skips the frontend
// re-run by embedding its output — a proper executable, not a bash
// wrapper around unpacked sources.
func Emit(a Artifact) error {
	if a.OutputPath == "" {
		return fmt.Errorf("no output path")
	}
	if len(a.WireJSON) == 0 {
		return fmt.Errorf("no frontend output to embed")
	}
	if a.GraalgoRoot == "" {
		return fmt.Errorf("no graalgo root")
	}

	// Validate the frontend output BEFORE embedding it: a malformed,
	// wrong-schema, or version-skewed document must fail the build
	// here, not surface as a run-time failure of the artifact.
	if err := validateWireJSON(a.WireJSON); err != nil {
		return err
	}

	// Compress the JSON. Frontend output can be large (100MB+ for big
	// programs); JSON compresses well with gzip.
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(a.WireJSON); err != nil {
		return fmt.Errorf("cannot compress frontend output: %v", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("cannot compress frontend output: %v", err)
	}

	// Set up a temp build dir with the launcher source and the payload.
	tmpDir, err := os.MkdirTemp("", "graalgo-build-*")
	if err != nil {
		return fmt.Errorf("cannot create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write the compressed payload for //go:embed.
	payloadPath := filepath.Join(tmpDir, "wire.json.gz")
	if err := os.WriteFile(payloadPath, compressed.Bytes(), 0o644); err != nil {
		return fmt.Errorf("cannot write payload: %v", err)
	}

	// Generate the launcher source.
	src := fmt.Sprintf(launcherTemplate, a.GraalgoRoot)
	srcPath := filepath.Join(tmpDir, "launcher.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		return fmt.Errorf("cannot write launcher source: %v", err)
	}

	// Minimal go.mod so `go build` works in module mode. The launcher has
	// no external dependencies (stdlib only).
	goMod := "module graalgo-artifact\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("cannot write go.mod: %v", err)
	}

	// Compile the launcher to a real binary.
	goBin := findGoBinary()
	cmd := exec.Command(goBin, "build", "-o", a.OutputPath, ".")
	cmd.Dir = tmpDir
	// Don't inherit GOFLAGS that might break the build; keep it hermetic.
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot compile artifact binary: %v\n%s", err, stderr.String())
	}

	// go build sets the executable bit, but ensure it (umask-safe).
	if err := os.Chmod(a.OutputPath, 0o755); err != nil {
		return fmt.Errorf("cannot chmod %s: %v", a.OutputPath, err)
	}
	return nil
}

// validateWireJSON parses the frontend output and checks its wire
// protocol version against the driver's expected version. Failing the
// build here is the point: a silently embedded bad document would
// produce an artifact that exits 0 at build time and fails only when
// run.
func validateWireJSON(wire []byte) error {
	var doc struct {
		// The "frontend" field carries the protocol version (Doc.Frontend
		// in internal/frontend). Checking only this field is deliberate:
		// it is the field the version contract (docs/frontend-v1.md §8)
		// is anchored on; full schema validation belongs to the
		// frontend's own --check gate.
		Frontend string `json:"frontend"`
	}
	if err := json.Unmarshal(wire, &doc); err != nil {
		return fmt.Errorf("invalid frontend output: %v", err)
	}
	if doc.Frontend == "" {
		return fmt.Errorf("invalid frontend output: missing \"frontend\" version field")
	}
	if doc.Frontend != wireProtocolVersion {
		return fmt.Errorf("wire protocol version mismatch: frontend emitted %q, driver expects %q", doc.Frontend, wireProtocolVersion)
	}
	return nil
}

// launcherTemplate is the Go source for the artifact binary. The
// frontend output is embedded via //go:embed from wire.json.gz (written
// alongside this file at build time). %s is the graalgo root baked in
// at build time.
const launcherTemplate = `// Code generated by graalgo build. DO NOT EDIT.
package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed wire.json.gz
var wireGz []byte

// graalgoRoot is the installation this artifact was built against.
// GRAALGO_HOME at run time takes precedence.
const graalgoRoot = "%s"

func main() {
	os.Exit(run())
}

func run() int {
	// Decompress the embedded frontend output.
	gr, err := gzip.NewReader(bytes.NewReader(wireGz))
	if err != nil {
		fmt.Fprintf(os.Stderr, "artifact: cannot decompress embedded program: %%v\n", err)
		return 1
	}
	wireJSON, err := io.ReadAll(gr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "artifact: cannot decompress embedded program: %%v\n", err)
		return 1
	}
	gr.Close()

	// Write the JSON to a temp file for the backend's --wire-input.
	tmp, err := os.CreateTemp("", "graalgo-wire-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "artifact: cannot create temp file: %%v\n", err)
		return 1
	}
	tmpName := tmp.Name()
	// Remove the temp file when we exit; the backend reads it before we do.
	defer os.Remove(tmpName)
	if _, err := tmp.Write(wireJSON); err != nil {
		fmt.Fprintf(os.Stderr, "artifact: cannot write frontend output: %%v\n", err)
		return 1
	}
	if err := tmp.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "artifact: cannot write frontend output: %%v\n", err)
		return 1
	}
	// Clear the in-memory copy; it can be large.
	wireJSON = nil

	// Locate the backend. GRAALGO_HOME wins; fall back to the baked-in root.
	root := os.Getenv("GRAALGO_HOME")
	if root == "" {
		root = graalgoRoot
	}
	runner := filepath.Join(root, "internal", "backend", "graalgo-sync-run")
	if fi, err := os.Stat(runner); err != nil || fi.Mode()&0o111 == 0 {
		fmt.Fprintf(os.Stderr, "artifact: cannot find graalgo backend at %%s (set GRAALGO_HOME)\n", runner)
		return 1
	}

	// argv[0] for the Go program is the artifact's own absolute path,
	// matching `+"`go build`"+` binaries.
	argv0, err := filepath.Abs(os.Args[0])
	if err != nil {
		argv0 = os.Args[0]
	}
	args := []string{"--wire-input", tmpName, "--", argv0}
	args = append(args, os.Args[1:]...)

	cmd := exec.Command(runner, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "artifact: %%v\n", err)
		return 1
	}
	return 0
}
`

// findGoBinary locates the Go toolchain for compiling the artifact
// launcher. It prefers the toolchain that built the driver itself
// (via GOROOT), then PATH, then the well-known install location.
func findGoBinary() string {
	// If the driver was built with a specific GOROOT, use its go binary.
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		cand := filepath.Join(goroot, "bin", "go")
		if fi, err := os.Stat(cand); err == nil && fi.Mode()&0o111 != 0 {
			return cand
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	// Well-known install location used by this project's toolchain.
	cand := filepath.Join(os.Getenv("HOME"), "tools", "go", "bin", "go")
	if fi, err := os.Stat(cand); err == nil && fi.Mode()&0o111 != 0 {
		return cand
	}
	// Last resort: let exec fail with a clear message.
	return "go"
}

// DefaultOutputName derives the artifact path when -o is not given,
// mirroring `go build`: the -o value if set, otherwise the base name of
// the first .go file (minus the extension), the base name of the named
// directory, or the base name of the current directory when no operands
// are given.
func DefaultOutputName(output string, pkgs []string) (string, error) {
	if output != "" {
		return output, nil
	}
	if len(pkgs) > 0 {
		first := pkgs[0]
		if strings.HasSuffix(first, ".go") {
			return strings.TrimSuffix(filepath.Base(first), ".go"), nil
		}
		base := filepath.Base(filepath.Clean(first))
		if base != "." {
			return base, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine output name: %v", err)
	}
	return filepath.Base(cwd), nil
}
