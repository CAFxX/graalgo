package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLauncher creates an executable file at dir/rel and returns its path.
func fakeLauncher(t *testing.T, dir, rel string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// isolatedEnv neuters the resolution inputs the test does not control:
// HOME (legacy fallback) and GRAALGO_RUN.
func isolatedEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRAALGO_RUN", "")
	return home
}

func TestResolveGraalgoRunExplicit(t *testing.T) {
	isolatedEnv(t)
	launcher := fakeLauncher(t, t.TempDir(), "my-graalgo-run")
	got, err := resolveGraalgoRun(launcher)
	if err != nil {
		t.Fatalf("explicit launcher: %v", err)
	}
	if got != launcher {
		t.Fatalf("got %q, want %q", got, launcher)
	}
}

func TestResolveGraalgoRunExplicitMissingFailsFast(t *testing.T) {
	isolatedEnv(t)
	if _, err := resolveGraalgoRun("/definitely/not/here/graalgo-run"); err == nil {
		t.Fatal("expected an error for a nonexistent --graalgo-run path, got nil")
	}
}

func TestResolveGraalgoRunExplicitNonExecutableFailsFast(t *testing.T) {
	isolatedEnv(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "graalgo-run")
	if err := os.WriteFile(p, []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveGraalgoRun(p); err == nil {
		t.Fatal("expected an error for a non-executable --graalgo-run path, got nil")
	}
}

func TestResolveGraalgoRunEnvVar(t *testing.T) {
	isolatedEnv(t)
	launcher := fakeLauncher(t, t.TempDir(), "env-graalgo-run")
	t.Setenv("GRAALGO_RUN", launcher)
	got, err := resolveGraalgoRun("")
	if err != nil {
		t.Fatalf("env launcher: %v", err)
	}
	if got != launcher {
		t.Fatalf("got %q, want %q", got, launcher)
	}
}

func TestResolveGraalgoRunEnvVarMissingFailsFast(t *testing.T) {
	isolatedEnv(t)
	t.Setenv("GRAALGO_RUN", "/definitely/not/here/graalgo-run")
	if _, err := resolveGraalgoRun(""); err == nil {
		t.Fatal("expected an error for a nonexistent $GRAALGO_RUN, got nil")
	}
}

func TestFindLauncherInAncestors(t *testing.T) {
	root := t.TempDir()
	want := fakeLauncher(t, root, filepath.Join("internal", "backend", "graalgo-run"))
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findLauncherInAncestors(deep, graalgoRunRel); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := findLauncherInAncestors(t.TempDir(), graalgoRunRel); got != "" {
		t.Fatalf("expected no launcher in an empty tree, got %q", got)
	}
}

func TestResolveGraalgoRunWalkUpFromCWD(t *testing.T) {
	isolatedEnv(t)
	root := t.TempDir()
	want := fakeLauncher(t, root, filepath.Join("internal", "backend", "graalgo-run"))
	deep := filepath.Join(root, "tools", "fuzz")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(deep)
	got, err := resolveGraalgoRun("")
	if err != nil {
		t.Fatalf("walk-up from CWD: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveGraalgoRunNothingFound(t *testing.T) {
	isolatedEnv(t)
	t.Chdir(t.TempDir())
	_, err := resolveGraalgoRun("")
	if err == nil {
		t.Fatal("expected an error when no launcher exists anywhere, got nil")
	}
	if !strings.Contains(err.Error(), "--graalgo-run") || !strings.Contains(err.Error(), "GRAALGO_RUN") {
		t.Fatalf("error should name the flag and env var, got: %v", err)
	}
}

func TestRunGraalWithoutResolvedBackend(t *testing.T) {
	old := graalgoRun
	graalgoRun = ""
	defer func() { graalgoRun = old }()
	r := runGraal(t.TempDir(), "main.go")
	if r.buildOK {
		t.Fatal("expected buildOK=false with an unresolved backend")
	}
	if !strings.Contains(r.buildErr, "not resolved") {
		t.Fatalf("expected a 'not resolved' buildErr, got %q", r.buildErr)
	}
}
