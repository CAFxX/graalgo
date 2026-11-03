package main

// Tests for gofrontend argv parsing and target resolution (main.go;
// REVIEW-2026-11-03-16, P0-4). The binary's exit-code contract is:
// malformed --target -> usage (1), well-formed but unsupported or
// host-mismatched --target -> frontend error (2), matching target -> 0
// (via ExportFilesWithTarget). These tests pin the parsing and the
// classification without spawning the binary.

import (
	"reflect"
	"testing"
)

// TestParseFrontendArgs: flags are stripped in any position, unknown
// flags and files pass through untouched, and a repeated flag's last
// value wins (the pre---target behaviour for --test-package-dir).
func TestParseFrontendArgs(t *testing.T) {
	dir, target, rest := parseFrontendArgs([]string{
		"a.go", "--target=linux/amd64", "--test-package-dir=/pkg", "b.go",
	})
	if dir != "/pkg" || target != "linux/amd64" {
		t.Errorf("parse = dir %q target %q, want /pkg linux/amd64", dir, target)
	}
	if !reflect.DeepEqual(rest, []string{"a.go", "b.go"}) {
		t.Errorf("rest = %v, want [a.go b.go]", rest)
	}

	dir, target, rest = parseFrontendArgs([]string{
		"--check-synthetic", "strings",
	})
	if dir != "" || target != "" || !reflect.DeepEqual(rest, []string{"--check-synthetic", "strings"}) {
		t.Errorf("--check-synthetic parse = dir %q target %q rest %v", dir, target, rest)
	}

	_, target, _ = parseFrontendArgs([]string{
		"--target=darwin/arm64", "--target=linux/amd64", "a.go",
	})
	if target != "linux/amd64" {
		t.Errorf("repeated --target: got %q, want last value linux/amd64", target)
	}
}

// TestResolveCompileTarget: no flag -> host target, validated; a
// matching flag -> that target; a malformed flag -> exit 1; a
// well-formed but unsupported or mismatched flag -> exit 2.
func TestResolveCompileTarget(t *testing.T) {
	host := HostTarget()
	target, explicit, code, err := resolveCompileTarget("")
	if err != nil || code != 0 || explicit || target != host {
		t.Errorf("no flag: got target %v explicit %v code %d err %v, want host %v", target, explicit, code, err, host)
	}
	target, explicit, code, err = resolveCompileTarget(host.String())
	if err != nil || code != 0 || !explicit || target != host {
		t.Errorf("matching flag: got target %v explicit %v code %d err %v", target, explicit, code, err)
	}
	_, _, code, err = resolveCompileTarget("linux")
	if err == nil || code != 1 {
		t.Errorf("malformed flag: got code %d err %v, want code 1 with error", code, err)
	}
	for _, bad := range []string{"darwin/arm64", "linux/arm64"} {
		_, _, code, err = resolveCompileTarget(bad)
		if err == nil || code != 2 {
			t.Errorf("flag %q: got code %d err %v, want code 2 with error", bad, code, err)
		}
	}
}
