package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Unit tests for the `graalgo run` exit-code machinery: the --exit-report
// verdict parsing and the `go run` exit-contract decision function. These
// run without a built backend.

func TestParseExitReport(t *testing.T) {
	cases := []struct {
		name    string
		content *string // nil = file does not exist
		want    exitKind
	}{
		{"program exit 0", strp("program-exit 0\n"), exitProgram},
		{"program exit 3", strp("program-exit 3\n"), exitProgram},
		{"program exit no trailing newline", strp("program-exit 3"), exitProgram},
		{"program exit 255", strp("program-exit 255\n"), exitProgram},
		{"backend failure 2", strp("backend-failure 2\n"), exitBackendFailure},
		{"backend failure 1", strp("backend-failure 1\n"), exitBackendFailure},
		{"extra whitespace tolerated", strp("  backend-failure\t2  \n"), exitBackendFailure},
		{"missing file", nil, exitUnknown},
		{"empty file", strp(""), exitUnknown},
		{"unknown verdict", strp("bogus 1\n"), exitUnknown},
		{"missing code", strp("program-exit\n"), exitUnknown},
		{"non-numeric code", strp("program-exit x\n"), exitUnknown},
		{"extra field", strp("program-exit 1 2\n"), exitUnknown},
		{"empty verdict word", strp(" 1\n"), exitUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "report.txt")
			if c.content != nil {
				if err := os.WriteFile(path, []byte(*c.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := parseExitReport(path); got != c.want {
				t.Fatalf("parseExitReport(%q) = %v, want %v", deref(c.content), got, c.want)
			}
		})
	}
}

func strp(s string) *string { return &s }

func deref(s *string) string {
	if s == nil {
		return "<missing>"
	}
	return *s
}

func TestDecideRunExit(t *testing.T) {
	cases := []struct {
		name        string
		outcome     runOutcome
		wantTrailer string
		wantCode    int
	}{
		{
			"program success",
			runOutcome{waitCode: 0, kind: exitProgram},
			"", 0,
		},
		{
			"program success, no report",
			runOutcome{waitCode: 0, kind: exitUnknown},
			"", 0,
		},
		{
			"program os.Exit(1)",
			runOutcome{waitCode: 1, kind: exitProgram},
			"exit status 1\n", 1,
		},
		{
			"program os.Exit(2)",
			runOutcome{waitCode: 2, kind: exitProgram},
			"exit status 2\n", 1,
		},
		{
			"program os.Exit(3)",
			runOutcome{waitCode: 3, kind: exitProgram},
			"exit status 3\n", 1,
		},
		{
			"program os.Exit(42)",
			runOutcome{waitCode: 42, kind: exitProgram},
			"exit status 42\n", 1,
		},
		{
			"program os.Exit(255)",
			runOutcome{waitCode: 255, kind: exitProgram},
			"exit status 255\n", 1,
		},
		{
			"backend input error",
			runOutcome{waitCode: 1, kind: exitBackendFailure},
			"", 1,
		},
		{
			"backend build failure",
			runOutcome{waitCode: 2, kind: exitBackendFailure},
			"", 1,
		},
		{
			"backend crash, no report",
			runOutcome{waitCode: 2, kind: exitUnknown},
			"", 1,
		},
		{
			"backend crash, no report, code 1",
			runOutcome{waitCode: 1, kind: exitUnknown},
			"", 1,
		},
		{
			"backend killed by SIGTERM",
			runOutcome{waitCode: -1, signaled: true, sigText: "signal: terminated", kind: exitUnknown},
			"signal: terminated\n", 1,
		},
		{
			"backend killed by SIGKILL",
			runOutcome{waitCode: -1, signaled: true, sigText: "signal: killed", kind: exitUnknown},
			"signal: killed\n", 1,
		},
		{
			"backend never started",
			runOutcome{waitCode: -1, startErr: errors.New("exec: not found")},
			"graalgo: exec: not found\n", 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trailer, code := decideRunExit(c.outcome)
			if trailer != c.wantTrailer || code != c.wantCode {
				t.Fatalf("decideRunExit(%+v) = (%q, %d), want (%q, %d)",
					c.outcome, trailer, code, c.wantTrailer, c.wantCode)
			}
		})
	}
}
