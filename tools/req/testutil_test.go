// Shared fixtures for the req CLI test suite.
//
// Hermeticity contract: every test builds its registry and scanned tree
// inside t.TempDir(). No test may touch the real ~/workspace/momo/requirements
// tree or rely on the process working directory, except the findRoot tests
// which chdir into a temp dir and restore afterwards (no t.Parallel anywhere
// in this package: several tests swap os.Stdin/os.Stdout/os.Stderr and cwd).
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRoot creates a temp directory populated with files: keys are
// slash-separated paths relative to the root, values are file contents.
// It returns the absolute root path.
func testRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// reqFile renders a registry YAML for one requirement. Empty parent and
// supersededBy serialize as null (loaded back as "").
func reqFile(id, status, parent, supersededBy string, history ...string) string {
	h := "  - \"2026-10-01: created (draft)\"\n"
	for _, e := range history {
		h += "  - \"" + e + "\"\n"
	}
	return fmt.Sprintf(`id: %s
title: Title %s
statement: Statement %s.
rationale: Rationale %s.
parent: %s
status: %s
superseded_by: %s
verification_plan: Plan %s.
history:
%s`, id, id, id, id, parent, status, supersededBy, id, h)
}

// ctxFlags builds the standard flag set pointing at a temp tree.
func ctxFlags(root, reqDir, scanDir string) []string {
	return []string{"--root", root, "--req-dir", reqDir, "--scan-dir", scanDir}
}

// stdLayout creates root/requirements (registry dir) and returns
// (root, reqDir). The scan dir defaults to root unless overridden.
func stdLayout(t *testing.T, files map[string]string) (root, reqDir string) {
	t.Helper()
	root = testRoot(t, files)
	reqDir = filepath.Join(root, "requirements")
	if err := os.MkdirAll(reqDir, 0755); err != nil {
		t.Fatal(err)
	}
	return root, reqDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// requireErrContains fails unless err is non-nil and mentions sub.
func requireErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Fatalf("error %q does not contain %q", err.Error(), sub)
	}
}

// captureOutput redirects os.Stdout/os.Stderr for the duration of fn and
// returns what was written.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	ro, wo, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wo, we
	fn()
	_ = wo.Close()
	_ = we.Close()
	bout, _ := io.ReadAll(ro)
	berr, _ := io.ReadAll(re)
	return string(bout), string(berr)
}

// withStdin replaces os.Stdin with a pipe preloaded with input ("" gives an
// immediate EOF) for the rest of the test.
func withStdin(t *testing.T, input string) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if input != "" {
		if _, err := w.Write([]byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = old
		_ = r.Close()
	})
}

// stubTerminal replaces the isTerminal gate for the rest of the test.
func stubTerminal(t *testing.T, term bool) {
	t.Helper()
	old := isTerminal
	isTerminal = func() bool { return term }
	t.Cleanup(func() { isTerminal = old })
}

// withCwd chdirs for the rest of the test and restores afterwards.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Fatal(err)
		}
	})
}

// snapshotTree records every regular file under root: rel path -> content.
func snapshotTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		snap[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// assertTreeIdentical fails if the file set or any file content under root
// differs from the snapshot.
func assertTreeIdentical(t *testing.T, root string, snap map[string][]byte) {
	t.Helper()
	cur := snapshotTree(t, root)
	if len(cur) != len(snap) {
		t.Fatalf("file set changed: before %d files, after %d files", len(snap), len(cur))
	}
	for rel, want := range snap {
		got, ok := cur[rel]
		if !ok {
			t.Fatalf("file %s disappeared", rel)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("file %s changed:\nbefore: %q\nafter:  %q", rel, want, got)
		}
	}
}
