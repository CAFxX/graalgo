package main

// Compile target for gofrontend (REVIEW-2026-11-03-16, P0-4).
//
// Before this file, the frontend hard-coded linux/amd64 in three places
// (synthetic runtime.GOARCH/GOOS constants, the exporter's type sizes, and
// p3frontend's type-check sizes) while disk imports selected files with
// the host build context. On any other host the frontend therefore
// silently emitted the wrong target: runtime.GOARCH printed "amd64" and
// unsafe.Sizeof/layout came from the amd64 oracle, with no fail-fast.
//
// The target is now a value, derived from the host, and validated:
//
//   - HostTarget is the machine gofrontend runs on, taken from
//     build.Default — the same source diskimport.go already uses for
//     file selection, so the frontend's constants, type sizes, and disk
//     file selection agree by construction. (build.Default's GOOS/GOARCH
//     are the host values for a normally-built gofrontend binary; they
//     are not read from the environment, so a user-exported GOOS/GOARCH
//     cannot redirect the target.)
//   - Only linux/amd64 is supported today, because the synthetic stdlib
//     is linux/amd64-only: synth_syscall.go ports
//     ztypes_linux_amd64.go/zsysnum_linux_amd64.go and synth_os.go
//     documents the linux/amd64 surface. Deriving GOARCH/GOOS from an
//     arm64/darwin host without porting those packages would trade one
//     lie (amd64 constants) for another (darwin constants over linux
//     syscall numbers). Widening the supported set therefore requires
//     porting the target-dependent synthetics first, then extending
//     supportedTargets here — never just adding a row to the table.
//   - An explicit --target flag (main.go) pins the target, but it must
//     equal the host: cross-compilation is rejected loudly, because the
//     synthetic/disk package caches are keyed by import path, not by
//     target, and disk file selection follows the host build context.
//     A cross target would type-check host-selected files against
//     target sizes. Until the caches and disk selection are target-keyed,
//     the honest behaviour is to fail, not to emit a mixed-target
//     document.
//
// Every compile entry point validates the target before doing any work:
// ExportFilesWithTarget (and ExportFiles via the host target), and
// main.go's --check-synthetic path. The overlay importer re-validates
// when it builds the synthetic runtime package, so even direct
// getSyntheticPackage callers cannot obtain amd64 constants on an
// unsupported host without an error.

import (
	"fmt"
	"go/build"
	"go/types"
	"strings"
)

// Target is a GOOS/GOARCH compile target.
type Target struct {
	GOOS   string
	GOARCH string
}

// String renders the target in GOOS/GOARCH form ("linux/amd64").
func (t Target) String() string {
	return t.GOOS + "/" + t.GOARCH
}

// IsZero reports whether the target carries no values. The zero value is
// used only as "not specified" by overlayImporter; it resolves to the
// host target (see overlayImporter.resolveTarget).
func (t Target) IsZero() bool {
	return t.GOOS == "" && t.GOARCH == ""
}

// HostTarget returns the compile target of the machine gofrontend runs
// on, from the same build context disk imports use for file selection.
func HostTarget() Target {
	return Target{GOOS: build.Default.GOOS, GOARCH: build.Default.GOARCH}
}

// supportedTargets is the full set of targets whose synthetic stdlib,
// type sizes, and disk file selection are all known to agree. See the
// file header for why this is a singleton today and what widening it
// requires. A target outside this set is rejected, never approximated.
var supportedTargets = map[Target]bool{
	{GOOS: "linux", GOARCH: "amd64"}: true,
}

// Validate reports whether the target is supported. It does not compare
// against the host; use validateForHost when a target must also equal
// the host (explicit --target, ExportFilesWithTarget).
func (t Target) Validate() error {
	if !supportedTargets[t] {
		return fmt.Errorf("gofrontend: unsupported target %s: only linux/amd64 is supported (synthetic syscall/os are linux/amd64-only); refusing to compile rather than emitting wrong GOARCH/GOOS or sizes", t)
	}
	return nil
}

// validateForHost rejects any target that is not the host target, and
// any host target that is not supported. Both failures are loud: the
// first is a cross-compilation request the frontend cannot honour
// honestly (see the file header), the second is an unsupported host.
func (t Target) validateForHost(host Target) error {
	if t != host {
		return fmt.Errorf("gofrontend: target %s does not match host %s: cross-compilation is not supported (synthetic/disk package caches are not target-keyed and disk file selection follows the host); pass --target=%s or run on the target host", t, host, host)
	}
	return t.Validate()
}

// ParseTarget parses a --target value in GOOS/GOARCH form. Parsing only
// checks the form; support is checked by Validate/validateForHost, so a
// well-formed but unsupported target (e.g. darwin/arm64) parses and then
// fails with the unsupported-target error, not a usage error.
func ParseTarget(s string) (Target, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Target{}, fmt.Errorf("gofrontend: --target must be GOOS/GOARCH (e.g. linux/amd64), got %q", s)
	}
	return Target{GOOS: parts[0], GOARCH: parts[1]}, nil
}

// Sizes returns the go/types sizes for the target's architecture, or an
// error for an unsupported target. Validate runs first on purpose:
// types.SizesFor knows many architectures (e.g. arm64) that graalgo's
// synthetic stdlib does not support, so a non-nil sizes table alone
// must never be read as "this target works".
func (t Target) Sizes() (types.Sizes, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	sizes := types.SizesFor("gc", t.GOARCH)
	if sizes == nil {
		return nil, fmt.Errorf("gofrontend: no gc type sizes for target %s", t)
	}
	return sizes, nil
}
