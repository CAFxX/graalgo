// Package main implements stdlib-vendor, the graalgo Phase-3 stdlib
// vendoring pipeline (P3-M6, WS F).
//
// The Go stdlib source tree (~157M for go1.27.1) is too large to commit
// into the git repo. stdlib-vendor copies $GOROOT/src to a pinned
// EXTERNAL directory (default ~/workspace/graalgo-stdlib/<version>/) and
// writes a byte-deterministic file manifest (relative path + SHA-256 +
// size + mode) plus the pinned Go version into the repo at
// tools/stdlib-fork/ (as `vendor-manifest.json` + `pinned-version.txt`;
// the old `phases/phase3/stdlib-fork/` location was retired when the
// tooling moved to `tools/` on 2026-10-14).
//
// Subcommands:
//
//	stdlib-vendor vendor [--goroot DIR] [--target DIR] [--manifest PATH]
//	                    [--repo DIR] [--force] [--allow-version-drift]
//	stdlib-vendor verify [--target DIR] [--manifest PATH] [--repo DIR]
//
// Exit codes: 0 ok, 1 refused / verification failed, 2 usage error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usageText = `stdlib-vendor — graalgo Phase-3 stdlib vendoring pipeline (P3-M6, WS F)

Usage:
  stdlib-vendor vendor [--goroot DIR] [--target DIR] [--manifest PATH]
                      [--repo DIR] [--force] [--allow-version-drift]
  stdlib-vendor verify [--target DIR] [--manifest PATH] [--repo DIR]

vendor copies $GOROOT/src into --target (default
$HOME/workspace/graalgo-stdlib/<goVersion>/) and writes a deterministic
manifest at --manifest (default <repo>/phases/phase3/stdlib-fork/vendor-manifest.json)
plus the pinned version at <repo>/phases/phase3/stdlib-fork/pinned-version.txt.

  --force                vendor into an existing non-empty target directory
  --allow-version-drift  proceed when the GOROOT version differs from the pin

verify re-hashes --target against --manifest and reports missing, extra,
or modified files.

Exit codes: 0 ok, 1 refused/verification failed, 2 usage error.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "usage: %v\n\n%s", ue, usageText)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "stdlib-vendor: %v\n", err)
		os.Exit(1)
	}
}

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func run(argv []string, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		return &usageError{"missing subcommand"}
	}
	switch argv[0] {
	case "vendor":
		return runVendorCmd(argv[1:], stdout, stderr)
	case "verify":
		return runVerifyCmd(argv[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usageText)
		return nil
	default:
		return &usageError{fmt.Sprintf("unknown subcommand %q", argv[0])}
	}
}

// repoRoot walks up from dir looking for a .git entry; it is used only to
// locate the default in-repo manifest/pin paths.
func repoRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		fi, err := os.Stat(filepath.Join(abs, ".git"))
		if err == nil && (fi.IsDir() || !fi.IsDir()) {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no git repository found walking up from %q", dir)
		}
		abs = parent
	}
}

// stdlibForkDir is the in-repo directory owned by this workstream.
func stdlibForkDir(repo string) string {
	return filepath.Join(repo, "phases", "phase3", "stdlib-fork")
}

func defaultManifestPath(repo string) string {
	return filepath.Join(stdlibForkDir(repo), "vendor-manifest.json")
}

func defaultPinPath(repo string) string {
	return filepath.Join(stdlibForkDir(repo), "pinned-version.txt")
}

// parseRepoFlag resolves the repo root from --repo, or by discovery from
// the current working directory.
func parseRepoFlag(fs *flag.FlagSet, repoFlag *string) (string, error) {
	if *repoFlag != "" {
		return filepath.Abs(*repoFlag)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return repoRoot(cwd)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
