package archive

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// compileArchive produces a real gc package archive for the tests, so the
// reader and appender are exercised against the format cmd/compile writes.
func compileArchive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "p.go")
	if err := os.WriteFile(src, []byte("package p\n\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "p.a")
	cmd := exec.Command("go", "tool", "compile", "-p", "p", "-pack", "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go tool compile: %v\n%s", err, b)
	}
	return out
}

func TestAppendReadRoundTrip(t *testing.T) {
	a := compileArchive(t)
	if _, err := Read(a, IRMember); !errors.Is(err, ErrNoMember) {
		t.Fatalf("Read before Append: err = %v, want ErrNoMember", err)
	}
	// Odd length exercises the padding rule on both the write and the
	// following append.
	first := []byte("odd")
	if err := Append(a, IRMember, first); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(a, IRMember); err != nil || !bytes.Equal(got, first) {
		t.Fatalf("Read = %q, %v; want %q", got, err, first)
	}
	if err := Append(a, "other", []byte("xy")); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(a, IRMember); err != nil || !bytes.Equal(got, first) {
		t.Fatalf("Read after second Append = %q, %v; want %q", got, err, first)
	}
	if got, err := Read(a, "other"); err != nil || string(got) != "xy" {
		t.Fatalf("Read(other) = %q, %v", got, err)
	}
}

// The extended archive must stay usable by the stock toolchain: go tool
// pack can list it and gc can still import the package from it.
func TestAppendKeepsArchiveStockReadable(t *testing.T) {
	a := compileArchive(t)
	if err := Append(a, IRMember, bytes.Repeat([]byte{0xAB}, 1001)); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "tool", "pack", "t", a).CombinedOutput()
	if err != nil {
		t.Fatalf("go tool pack t: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte(IRMember)) || !bytes.Contains(out, []byte("__.PKGDEF")) {
		t.Fatalf("pack listing lacks members:\n%s", out)
	}
	dir := t.TempDir()
	user := filepath.Join(dir, "q.go")
	if err := os.WriteFile(user, []byte("package q\n\nimport \"p\"\n\nvar V = p.F()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "importcfg")
	if err := os.WriteFile(cfg, []byte("packagefile p="+a+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "tool", "compile", "-p", "q", "-importcfg", cfg, "-o", filepath.Join(dir, "q.a"), user)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gc cannot import from the extended archive: %v\n%s", err, b)
	}
}

func TestRejectsNonArchive(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(p, []byte("not an archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Append(p, IRMember, []byte("x")); err == nil {
		t.Fatal("Append to a non-archive succeeded")
	}
	if _, err := Read(p, IRMember); err == nil {
		t.Fatal("Read of a non-archive succeeded")
	}
}
