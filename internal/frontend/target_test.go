package main

// Tests for the compile target (target.go; REVIEW-2026-11-03-16, P0-4).
// The defect was a silent lie: hard-coded linux/amd64 constants and
// sizes on any host. These tests pin the three properties that fix it:
// (1) the reported target is the host target, (2) the exported document
// carries the host target's constants, and (3) anything else fails
// loudly instead of compiling. Property (1) is host-sensitive by
// design: on a linux/amd64 host it passes for the correct values, and
// on an arm64/darwin host it fails for hard-coded amd64 values — that
// is the test doing its job, not flakiness.

import (
	"go/build"
	"go/constant"
	"go/types"
	"runtime"
	"strings"
	"testing"
)

// TestHostTargetMatchesHost: the frontend's target must be the machine
// it runs on, from the same build context disk imports use, not a
// literal. Fails if HostTarget ever diverges from the host again.
func TestHostTargetMatchesHost(t *testing.T) {
	host := HostTarget()
	if host.GOOS != build.Default.GOOS || host.GOARCH != build.Default.GOARCH {
		t.Errorf("HostTarget() = %s, want build.Default %s/%s", host, build.Default.GOOS, build.Default.GOARCH)
	}
	if host.GOOS != runtime.GOOS || host.GOARCH != runtime.GOARCH {
		t.Errorf("HostTarget() = %s, want runtime %s/%s", host, runtime.GOOS, runtime.GOARCH)
	}
	if host.String() != host.GOOS+"/"+host.GOARCH {
		t.Errorf("Target.String() = %q, want %q", host.String(), host.GOOS+"/"+host.GOARCH)
	}
}

// TestParseTarget: form checking only — support is Validate's job, so
// a well-formed but unsupported target must parse and then fail
// validation, not fail parsing (the two failures have different exit
// codes in main.go: usage vs frontend error).
func TestParseTarget(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Target
		wantErr bool
	}{
		{"linux/amd64", Target{GOOS: "linux", GOARCH: "amd64"}, false},
		{"darwin/arm64", Target{GOOS: "darwin", GOARCH: "arm64"}, false},
		{"linux", Target{}, true},
		{"", Target{}, true},
		{"/amd64", Target{}, true},
		{"linux/", Target{}, true},
		{"linux/amd64/extra", Target{}, true},
	} {
		got, err := ParseTarget(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q): got %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseTarget(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestTargetValidate: only linux/amd64 is supported (target.go header:
// the synthetic syscall/os are linux/amd64-only). Every other
// well-formed target must fail validation — including linux/arm64,
// which go/types sizes tables know but graalgo's synthetics do not
// support, so Sizes must fail for it too (see TestTargetSizes).
func TestTargetValidate(t *testing.T) {
	if err := (Target{GOOS: "linux", GOARCH: "amd64"}).Validate(); err != nil {
		t.Errorf("linux/amd64 must validate: %v", err)
	}
	for _, unsupported := range []Target{
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "darwin", GOARCH: "amd64"},
		{GOOS: "linux", GOARCH: "arm64"},
		{GOOS: "windows", GOARCH: "amd64"},
		{GOOS: "", GOARCH: ""},
	} {
		if err := unsupported.Validate(); err == nil {
			t.Errorf("Validate(%s): got nil, want unsupported-target error", unsupported)
		} else if !strings.Contains(err.Error(), unsupported.String()) {
			t.Errorf("Validate(%s) error %q must name the target", unsupported, err)
		}
	}
}

// TestTargetValidateForHost: an explicit target must equal the host
// (cross-compilation is rejected, not approximated), and a host that
// is itself unsupported fails even when the target equals it. The
// host argument is a parameter precisely so both rules are testable
// without changing the machine the test runs on.
func TestTargetValidateForHost(t *testing.T) {
	host := Target{GOOS: "linux", GOARCH: "amd64"}
	if err := host.validateForHost(host); err != nil {
		t.Errorf("matching supported target must validate: %v", err)
	}
	if err := (Target{GOOS: "linux", GOARCH: "arm64"}).validateForHost(host); err == nil {
		t.Errorf("cross target must fail validateForHost")
	} else if !strings.Contains(err.Error(), "cross-compilation") {
		t.Errorf("cross-target error %q must say cross-compilation", err)
	}
	otherHost := Target{GOOS: "darwin", GOARCH: "arm64"}
	if err := otherHost.validateForHost(otherHost); err == nil {
		t.Errorf("unsupported host must fail validateForHost even when target == host")
	}
}

// TestTargetSizes: the supported target yields real gc sizes (int is
// 8 bytes on amd64); an unsupported target yields an error even though
// go/types knows its architecture — a sizes table alone is not support.
func TestTargetSizes(t *testing.T) {
	sizes, err := (Target{GOOS: "linux", GOARCH: "amd64"}).Sizes()
	if err != nil {
		t.Fatalf("linux/amd64 Sizes: %v", err)
	}
	if got := sizes.Sizeof(types.Typ[types.Int]); got != 8 {
		t.Errorf("amd64 Sizeof(int) = %d, want 8", got)
	}
	if _, err := (Target{GOOS: "linux", GOARCH: "arm64"}).Sizes(); err == nil {
		t.Errorf("linux/arm64 Sizes: got a table, want unsupported-target error")
	}
}

// runtimeConstString reads a string constant out of the synthetic
// runtime view exactly as importing code sees it.
func runtimeConstString(t *testing.T, imp overlayImporter, name string) string {
	t.Helper()
	pkg, err := imp.Import("runtime")
	if err != nil {
		t.Fatalf("overlayImporter.Import(runtime): %v", err)
	}
	obj, ok := pkg.Scope().Lookup(name).(*types.Const)
	if !ok {
		t.Fatalf("runtime.%s is not a *types.Const", name)
	}
	return constant.StringVal(obj.Val())
}

// TestRuntimeViewTargetConstants: the synthetic runtime view's
// GOARCH/GOOS must equal the host target — the exact property the
// hard-coded literals violated on any other host. Covers both the
// zero-value importer (pre-existing callers, tests, nested builds) and
// an explicit target.
func TestRuntimeViewTargetConstants(t *testing.T) {
	host := HostTarget()
	for _, imp := range []overlayImporter{{}, {target: host}} {
		if got := runtimeConstString(t, imp, "GOARCH"); got != host.GOARCH {
			t.Errorf("runtime.GOARCH = %q, want host %q", got, host.GOARCH)
		}
		if got := runtimeConstString(t, imp, "GOOS"); got != host.GOOS {
			t.Errorf("runtime.GOOS = %q, want host %q", got, host.GOOS)
		}
	}
}

// TestRuntimeViewRejectsUnsupportedTarget: even a direct overlay
// import must fail on an unsupported target, so no path can obtain
// runtime constants without the target gate.
func TestRuntimeViewRejectsUnsupportedTarget(t *testing.T) {
	if _, err := (overlayImporter{target: Target{GOOS: "darwin", GOARCH: "arm64"}}).Import("runtime"); err == nil {
		t.Errorf("overlay import with darwin/arm64 target: got a package, want an error")
	}
}

// TestExportFilesWithTarget: end-to-end through ExportFiles — the
// supported target exports a document carrying the host constants, and
// unsupported/cross targets fail before any file is read (a missing
// file with a bad target must report the target, not the file).
func TestExportFilesWithTarget(t *testing.T) {
	host := HostTarget()
	src := "package main\n\nimport \"runtime\"\n\nvar X = runtime.GOARCH\nvar Y = runtime.GOOS\n\nfunc main() { _, _ = X, Y }\n"
	out, err := ExportFilesWithTarget([]string{writeSyncProg(t, src)}, "", host)
	if err != nil {
		t.Fatalf("ExportFilesWithTarget(host %s): %v", host, err)
	}
	// The document JSON-encodes the constant's value with escaped
	// quotes (a "const" field of `\"amd64\"`), so assert that exact
	// encoding rather than a bare quoted substring.
	if !strings.Contains(out, `\"`+host.GOARCH+`\"`) || !strings.Contains(out, `\"`+host.GOOS+`\"`) {
		t.Errorf("exported document does not carry host target constants %s", host)
	}
	for _, bad := range []Target{
		{GOOS: "darwin", GOARCH: "arm64"},
		{GOOS: "linux", GOARCH: "arm64"},
	} {
		if _, err := ExportFilesWithTarget([]string{writeSyncProg(t, src)}, "", bad); err == nil {
			t.Errorf("ExportFilesWithTarget(%s): got success, want failure", bad)
		} else if !strings.Contains(err.Error(), bad.String()) {
			t.Errorf("ExportFilesWithTarget(%s) error %q must name the target", bad, err)
		}
	}
	if _, err := ExportFilesWithTarget([]string{"/nonexistent/prog.go"}, "", Target{GOOS: "darwin", GOARCH: "arm64"}); err == nil || !strings.Contains(err.Error(), "darwin/arm64") {
		t.Errorf("bad target + missing file: got err %v, want the target error first", err)
	}
}

// TestExportFilesUsesHostTarget: the no-target entry point compiles on
// the host (it must not fail merely because no --target was given).
func TestExportFilesUsesHostTarget(t *testing.T) {
	src := "package main\n\nfunc main() {}\n"
	if _, err := ExportFiles([]string{writeSyncProg(t, src)}, ""); err != nil {
		t.Fatalf("ExportFiles on host %s: %v", HostTarget(), err)
	}
}
