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
	// R15: never package unvalidated wire JSON into a shipped binary.
	// A truncated or corrupt frontend output must fail the build here,
	// not produce an artifact that fails only at run time.
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

	// Generate the launcher source (root quoted, never interpolated raw).
	src := launcherSource(a.GraalgoRoot)
	srcPath := filepath.Join(tmpDir, "launcher.go")
	if err := os.WriteFile(srcPath, []byte(src), 0o644); err != nil {
		return fmt.Errorf("cannot write launcher source: %v", err)
	}

	// Minimal go.mod so `go build` works in module mode. The launcher has
	// no external dependencies (stdlib only). The language version
	// tracks the pinned toolchain (go1.27.1); Emit version-checks the
	// selected toolchain against it below instead of letting `go build`
	// fail with a confusing go.mod error.
	goMod := "module graalgo-artifact\n\ngo 1.27\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return fmt.Errorf("cannot write go.mod: %v", err)
	}

	// Compile the launcher to a real binary. The toolchain is pinned,
	// not taken unchecked from PATH: a go older than the launcher
	// go.mod's language version fails here with a clear error.
	goBin := findGoBinary()
	major, minor, version, err := goToolchainVersion(goBin)
	if err != nil {
		return err
	}
	if major < 1 || (major == 1 && minor < 27) {
		return fmt.Errorf("artifact toolchain %s is go %s, need go 1.27 or later", goBin, version)
	}
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

// wireHeader is the minimal frontend/v1 shape Emit requires before it
// will package wire JSON into a binary: the protocol version, the
// generator that produced it, and the package it describes. This is a
// shape check, not a full validation (the full contract lives in
// docs/frontend-v1.md and tools/validate_frontend_v1.py); it exists to
// reject truncated or corrupt output, which previously sailed through
// on an emptiness check alone.
type wireHeader struct {
	Frontend  string `json:"frontend"`
	Generator struct {
		Version string `json:"version"`
	} `json:"generator"`
	Package struct {
		Name  string   `json:"name"`
		Files []string `json:"files"`
	} `json:"package"`
}

// validateWireJSON rejects wire JSON that is not a well-formed
// frontend/v1 document. A truncated write (e.g. a backend killed
// mid-emit) fails json.Valid; a valid JSON value of the wrong shape
// (empty object, a different protocol version, a document with no
// package) fails the shape checks.
func validateWireJSON(wire []byte) error {
	if !json.Valid(wire) {
		return fmt.Errorf("invalid frontend output: not valid JSON")
	}
	var h wireHeader
	if err := json.Unmarshal(wire, &h); err != nil {
		return fmt.Errorf("invalid frontend output: %v", err)
	}
	if h.Frontend != "v1" {
		return fmt.Errorf("invalid frontend output: unsupported frontend version %q (want \"v1\")", h.Frontend)
	}
	if h.Generator.Version == "" {
		return fmt.Errorf("invalid frontend output: missing generator version")
	}
	if h.Package.Name == "" {
		return fmt.Errorf("invalid frontend output: missing package name")
	}
	if len(h.Package.Files) == 0 {
		return fmt.Errorf("invalid frontend output: package has no files")
	}
	return nil
}

// launcherSource renders the launcher Go source with the graalgo root
// baked in. The root is emitted with %q (a quoted Go string literal),
// never %s: a root containing a quote, backslash, or newline must
// produce a valid literal, never injected source in a file that is
// then compiled and executed.
func launcherSource(graalgoRoot string) string {
	return fmt.Sprintf(launcherTemplate, graalgoRoot)
}

// goToolchainVersion runs `go version` for the given binary and parses
// its version. The returned version string is the raw "1.27.1" part
// (without the "go" prefix) for error messages.
func goToolchainVersion(goBin string) (major, minor int, version string, err error) {
	out, err := exec.Command(goBin, "version").Output()
	if err != nil {
		return 0, 0, "", fmt.Errorf("cannot run %s version: %v", goBin, err)
	}
	return parseGoVersionOutput(string(out))
}

// parseGoVersionOutput parses `go version` output, e.g.
// "go version go1.27.1 linux/amd64".
func parseGoVersionOutput(out string) (major, minor int, version string, err error) {
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return 0, 0, "", fmt.Errorf("cannot parse go version output %q", out)
	}
	version = strings.TrimPrefix(fields[2], "go")
	// Sscanf on "1.27rc1" yields major=1, minor=27; that is fine —
	// a release candidate of 1.27 satisfies a 1.27 floor.
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return 0, 0, "", fmt.Errorf("cannot parse go version %q", version)
	}
	return major, minor, version, nil
}

// launcherTemplate is the Go source for the artifact binary. The
// frontend output is embedded via //go:embed from wire.json.gz (written
// alongside this file at build time). %q is the graalgo root baked in
// at build time, as a quoted string literal (see launcherSource).
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
const graalgoRoot = %q

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
	// matching ` + "`go build`" + ` binaries.
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
