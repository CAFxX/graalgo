// Package driver implements build orchestration for the graalgo command:
// parsing build requests, resolving package files, and emitting runnable
// artifacts from validated programs.
package driver

import (
	"archive/tar"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProgramFile is one Go source file of the program being built.
// Name is the base name the backend sees; Path is where the source is read
// from at build time. The backend compiles the files as one unit, exactly
// like `graalgo run` does, so every Name must be unique.
type ProgramFile struct {
	Name string
	Path string
}

// Artifact describes the executable artifact `graalgo build` emits.
type Artifact struct {
	// OutputPath is where the artifact script is written.
	OutputPath string
	// GraalgoRoot is the graalgo installation the artifact runs against.
	// It is baked into the artifact at build time; GRAALGO_HOME set at
	// run time takes precedence.
	GraalgoRoot string
	// Files are the program's Go source files, embedded into the artifact
	// so it stays runnable if the original sources move or disappear.
	Files []ProgramFile
}

// Emit writes the executable artifact described by a. The artifact is a
// self-contained shell launcher: it unpacks the embedded program sources
// into a temporary directory and execs the graalgo backend on them,
// forwarding its own arguments to the program. It is executable and can be
// run directly, like a binary produced by `go build`.
//
// The build step validates the program first (the caller runs the frontend
// gate before Emit); Emit itself only packages. The backend re-runs the
// frontend at execution time — the Truffle backend JIT-compiles, so there
// is no ahead-of-time machine code to embed. A future optimization is to
// embed the frontend wire output and teach the backend a wire-input mode,
// skipping the frontend re-run per execution.
func Emit(a Artifact) error {
	if a.OutputPath == "" {
		return fmt.Errorf("no output path")
	}
	if len(a.Files) == 0 {
		return fmt.Errorf("no program files")
	}
	seen := make(map[string]string, len(a.Files))
	for _, f := range a.Files {
		if f.Name == "" || f.Path == "" {
			return fmt.Errorf("program file with empty name or path")
		}
		if prev, dup := seen[f.Name]; dup {
			return fmt.Errorf("duplicate file name %q (%q and %q)", f.Name, prev, f.Path)
		}
		seen[f.Name] = f.Path
		if _, err := os.Stat(f.Path); err != nil {
			return fmt.Errorf("cannot read %s: %v", f.Path, err)
		}
	}

	tarBytes, err := buildTar(a.Files)
	if err != nil {
		return err
	}
	script := renderLauncher(filepath.Base(a.OutputPath), a.GraalgoRoot, a.Files, tarBytes)

	if err := os.WriteFile(a.OutputPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("cannot write %s: %v", a.OutputPath, err)
	}
	// WriteFile's mode is subject to umask; ensure the artifact is runnable.
	if err := os.Chmod(a.OutputPath, 0o755); err != nil {
		return fmt.Errorf("cannot chmod %s: %v", a.OutputPath, err)
	}
	return nil
}

// buildTar packs the program files into a tar archive keyed by base name.
// ModTime is fixed so repeated builds of the same sources are byte-identical.
func buildTar(files []ProgramFile) ([]byte, error) {
	var sb strings.Builder
	tw := tar.NewWriter(&sb)
	for _, f := range files {
		src, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %v", f.Path, err)
		}
		hdr := &tar.Header{
			Name:    f.Name,
			Mode:    0o644,
			Size:    int64(len(src)),
			ModTime: time.Unix(0, 0).UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("cannot pack %s: %v", f.Name, err)
		}
		if _, err := tw.Write(src); err != nil {
			return nil, fmt.Errorf("cannot pack %s: %v", f.Name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("cannot finish archive: %v", err)
	}
	return []byte(sb.String()), nil
}

// renderLauncher fills the artifact script template. payload is the
// base64-encoded tar of the program sources.
func renderLauncher(name, graalgoRoot string, files []ProgramFile, tarBytes []byte) string {
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = shQuote(f.Name)
	}
	return fmt.Sprintf(`#!/usr/bin/env bash
# %s — executable artifact produced by `+"`graalgo build`"+`.
#
# Self-contained launcher: unpacks the embedded Go program sources into a
# temporary directory and runs them through the graalgo backend, forwarding
# any arguments to the program.
#
# The backend is located via GRAALGO_HOME, falling back to the graalgo
# installation this artifact was built against.
set -euo pipefail
GRAALGO_ROOT="${GRAALGO_HOME:-%s}"
RUNNER="$GRAALGO_ROOT/internal/backend/graalgo-sync-run"
if [ ! -x "$RUNNER" ]; then
    echo "%s: cannot find graalgo backend at $RUNNER (set GRAALGO_HOME)" >&2
    exit 1
fi
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
base64 -d > "$WORK/program.tar" <<'GRAALGO_PAYLOAD_EOF'
%s
GRAALGO_PAYLOAD_EOF
tar -xf "$WORK/program.tar" -C "$WORK"
ARGS=()
for f in %s; do
    ARGS+=("$WORK/$f")
done
exec "$RUNNER" "${ARGS[@]}" -- "$@"
`, name, shDQuote(graalgoRoot), name, wrapBase64(tarBytes), strings.Join(names, " "))
}

// wrapBase64 encodes b in the base64 alphabet, wrapped at 76 columns. The
// alphabet never contains '_', so no encoded line can match or contain the
// heredoc delimiter GRAALGO_PAYLOAD_EOF.
func wrapBase64(b []byte) string {
	enc := base64.StdEncoding.EncodeToString(b)
	var sb strings.Builder
	for len(enc) > 76 {
		sb.WriteString(enc[:76])
		sb.WriteByte('\n')
		enc = enc[76:]
	}
	sb.WriteString(enc)
	return sb.String()
}

// shQuote single-quotes s for shell.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shDQuote escapes s for use inside double quotes in shell.
func shDQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return r.Replace(s)
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
