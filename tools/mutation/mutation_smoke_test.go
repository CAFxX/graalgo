package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Smoke tests (review 2026-11-02, P2-20): the tool's backend-free helpers,
// gated without a JVM. findFailingTests parses a suite log for FAIL lines;
// copyDir stages a mutant workdir.
func TestFindFailingTestsSmoke(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "run.log")
	if err := os.WriteFile(log, []byte("ok  \tgraalgo/x\t1.2s\nFAIL Foo.bar: boom\nFAIL Baz.qux\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := findFailingTests(log)
	if len(got) != 2 || got[0] != "Foo.bar" || got[1] != "Baz.qux" {
		t.Fatalf("findFailingTests = %v, want [Foo.bar Baz.qux]", got)
	}
	missing := findFailingTests(filepath.Join(dir, "nope.log"))
	if len(missing) != 1 || missing[0] != "(unreadable log)" {
		t.Fatalf("unreadable log = %v, want [(unreadable log)]", missing)
	}
}

func TestCopyDirSmoke(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "copy")
	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}
	for path, want := range map[string]string{"a.txt": "hello", "sub/b.txt": "world"} {
		got, err := os.ReadFile(filepath.Join(dst, path))
		if err != nil || string(got) != want {
			t.Fatalf("copy %s = %q, err %v; want %q", path, got, err, want)
		}
	}
}
