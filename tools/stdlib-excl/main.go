// Package main implements stdlib-excl, the graalgo Phase-3 stdlib
// exclusion-list validator and coupled-package measurement tool (P3-M6,
// workstreams 4+5; GG-0009 re-measurement, GG-0033 exclusion machinery).
//
// Subcommands:
//
//	stdlib-excl validate --list PATH [--src DIR] [--max N]
//	                    [--test-binaries N] [--baseline PATH]
//	stdlib-excl measure  --src DIR --out PATH [--go-version V]
//
// "validate" enforces the exclusion-list contract: versioned file, reason
// code mandatory and drawn from the file's own reason_codes vocabulary,
// entry cap (default 25 packages; additionally 5% of stdlib test binaries,
// whichever binds first), no change without a version bump (compared
// against the git HEAD baseline by default), and no stale entries (every
// package path must exist in the stdlib tree).
//
// "measure" walks a Go source tree and applies the strict pure-Go check
// per package dir (see measure.go), emitting a versioned JSON list of
// pure-Go vs coupled dirs with per-dir reasons.
//
// Exit codes: 0 ok, 1 validation/measurement failure, 2 usage error.
// No external dependencies; the YAML reader is a minimal line parser with
// the same semantics as test/differential LoadExclusions and the Java
// readExclusions in TypeparamDifferentialTest (GG-0033 precedent).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const usageText = `stdlib-excl — graalgo Phase-3 stdlib exclusion-list validator + coupled-package measurement (P3-M6)

Usage:
  stdlib-excl validate --list PATH [--src DIR] [--max N]
                      [--test-binaries N] [--baseline PATH]
  stdlib-excl measure  --src DIR --out PATH [--go-version V]

validate checks phases/phase3/stdlib-fork/exclusions.yaml:
  every entry carries a reason code from the file's reason_codes
  vocabulary; the entry count stays under the review cap (default 25,
  additionally 5% of stdlib test binaries, whichever binds first);
  a changed entry set without a version bump is rejected (baseline
  defaults to the git HEAD version of the list); every entry's package
  path must exist in the stdlib tree (--src, default: vendored tree
  when present, else GOROOT/src).

measure walks --src (default GOROOT/src) and applies the strict pure-Go
  check per package dir (GOOS=linux GOARCH=amd64), writing a versioned
  JSON list of pure-Go / coupled dirs with per-dir reasons to --out.

Exit codes: 0 ok, 1 validation/measurement failure, 2 usage error.
`

type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "usage: %v\n\n%s", ue, usageText)
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "stdlib-excl: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &usageError{"missing subcommand"}
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "measure":
		return runMeasure(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usageText)
		return nil
	default:
		return &usageError{fmt.Sprintf("unknown subcommand %q", args[0])}
	}
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
