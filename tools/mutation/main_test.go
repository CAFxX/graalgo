package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot returns the repo root from the package dir (tools/mutation).
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// fakeBin creates an executable file named name in dir (for PATH tests).
func fakeBin(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGoBinDiscovery pins the toolchain-discovery order (same shape as
// tools/stdlib-excl/validate.go:goBinary): GRAALGO_GO, then $GO, then
// PATH, then $HOME/tools/go/bin/go.
func TestGoBinDiscovery(t *testing.T) {
	binDir := t.TempDir()
	fakeGo := fakeBin(t, binDir, "go")

	t.Setenv("GRAALGO_GO", "/opt/go/bin/go")
	t.Setenv("GO", "/usr/local/go/bin/go")
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if got := goBin(); got != "/opt/go/bin/go" {
		t.Fatalf("GRAALGO_GO must win: got %q", got)
	}

	t.Setenv("GRAALGO_GO", "")
	if got := goBin(); got != "/usr/local/go/bin/go" {
		t.Fatalf("$GO must win over PATH: got %q", got)
	}

	t.Setenv("GO", "")
	if got := goBin(); got != fakeGo {
		t.Fatalf("PATH lookup must win over fallback: got %q", got)
	}

	// Nothing on PATH, no env: the pinned local install.
	noGoDir := t.TempDir()
	t.Setenv("PATH", noGoDir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "tools", "go", "bin", "go")
	if got := goBin(); got != want {
		t.Fatalf("fallback: got %q, want %q", got, want)
	}
}

// TestJavaBinDiscovery pins: GRAALGO_JAVA, then $JAVA_HOME, then PATH,
// then $HOME/tools/graalvm-jdk-25/bin/javac.
func TestJavaBinDiscovery(t *testing.T) {
	binDir := t.TempDir()
	fakeJavac := fakeBin(t, binDir, "javac")

	t.Setenv("GRAALGO_JAVA", "/opt/graalvm/bin/javac")
	t.Setenv("JAVA_HOME", "/opt/other-jdk")
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if got := javaBin(); got != "/opt/graalvm/bin/javac" {
		t.Fatalf("GRAALGO_JAVA must win: got %q", got)
	}

	t.Setenv("GRAALGO_JAVA", "")
	if got := javaBin(); got != "/opt/other-jdk/bin/javac" {
		t.Fatalf("$JAVA_HOME must win over PATH: got %q", got)
	}

	t.Setenv("JAVA_HOME", "")
	if got := javaBin(); got != fakeJavac {
		t.Fatalf("PATH lookup must win over fallback: got %q", got)
	}

	noJavaDir := t.TempDir()
	t.Setenv("PATH", noJavaDir)
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "tools", "graalvm-jdk-25", "bin", "javac")
	if got := javaBin(); got != want {
		t.Fatalf("fallback: got %q, want %q", got, want)
	}
}

// TestJavaRunBinDiscovery pins: GRAALGO_JAVA_RUN, then the sibling of
// GRAALGO_JAVA, then $JAVA_HOME, then PATH, then the local fallback.
func TestJavaRunBinDiscovery(t *testing.T) {
	binDir := t.TempDir()
	fakeJava := fakeBin(t, binDir, "java")

	t.Setenv("GRAALGO_JAVA_RUN", "/opt/graalvm/bin/java")
	t.Setenv("GRAALGO_JAVA", "/opt/other/bin/javac")
	t.Setenv("JAVA_HOME", "/opt/third-jdk")
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	if got := javaRunBin(); got != "/opt/graalvm/bin/java" {
		t.Fatalf("GRAALGO_JAVA_RUN must win: got %q", got)
	}

	t.Setenv("GRAALGO_JAVA_RUN", "")
	if got := javaRunBin(); got != "/opt/other/bin/java" {
		t.Fatalf("java must sit alongside GRAALGO_JAVA: got %q", got)
	}

	t.Setenv("GRAALGO_JAVA", "")
	if got := javaRunBin(); got != "/opt/third-jdk/bin/java" {
		t.Fatalf("$JAVA_HOME must win over PATH: got %q", got)
	}

	t.Setenv("JAVA_HOME", "")
	if got := javaRunBin(); got != fakeJava {
		t.Fatalf("PATH lookup must win over fallback: got %q", got)
	}
}

// TestClasspaths pins the backend layout against the real tree: the
// test classpath mirrors the truffle Makefile `test` target
// (test-classes : runtime/classes : truffle/classes : jars, overlay
// first), and the javac classpath is the built classes + jars.
func TestClasspaths(t *testing.T) {
	repo := repoRoot(t)

	cp := jarCP(repo)
	if cp == "" {
		t.Fatal("jarCP is empty: truffle/lib/*.jar missing?")
	}
	for _, jar := range strings.Split(cp, ":") {
		if _, err := os.Stat(jar); err != nil {
			t.Fatalf("jarCP entry missing: %s: %v", jar, err)
		}
	}

	overlay := filepath.Join(t.TempDir(), "classes")
	tc := testClasspath(repo, overlay)
	parts := strings.Split(tc, ":")
	if parts[0] != overlay {
		t.Fatalf("overlay must be first on the test classpath: %q", tc)
	}
	want := []string{
		filepath.Join(repo, "internal", "backend", "truffle", "out", "test-classes"),
		filepath.Join(repo, "internal", "backend", "runtime", "out", "classes"),
		filepath.Join(repo, "internal", "backend", "truffle", "out", "classes"),
	}
	for _, w := range want {
		found := false
		for _, p := range parts {
			if p == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("test classpath %q lacks %q", tc, w)
		}
	}
	if strings.Contains(tc, "phases/phase1") {
		t.Fatalf("test classpath still references the deleted tree: %q", tc)
	}

	jc := javacClasspath(repo)
	if strings.Contains(jc, "test-classes") {
		t.Fatalf("javac classpath must not include test classes: %q", jc)
	}
	if !strings.Contains(jc, filepath.Join(repo, "internal", "backend", "runtime", "out", "classes")) {
		t.Fatalf("javac classpath lacks runtime classes: %q", jc)
	}
}

// TestMutantsSpecCurrent pins mutants.json against the current tree:
// every listed file must exist and contain its `old` string, or the
// tool can only ERROR on that mutant ("old string not found" / read
// failure). This is the test that would have caught the deleted-tree
// spec (all four Java mutants pointed at phases/phase1/...).
func TestMutantsSpecCurrent(t *testing.T) {
	repo := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(repo, "tools", "mutation", "mutants.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Mutants []Mutant `json:"mutants"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Mutants) == 0 {
		t.Fatal("mutants.json lists no mutants")
	}
	for _, m := range spec.Mutants {
		src, err := os.ReadFile(filepath.Join(repo, m.File))
		if err != nil {
			t.Errorf("mutant %s: file %s unreadable: %v", m.ID, m.File, err)
			continue
		}
		if !strings.Contains(string(src), m.Old) {
			t.Errorf("mutant %s: old string not found in %s", m.ID, m.File)
		}
		if strings.Contains(m.File, "phases/") {
			t.Errorf("mutant %s: file still points at the deleted tree: %s", m.ID, m.File)
		}
	}
}

// TestPersistReport pins the report second copy: MUTATION_REPORT_DIR
// set writes a timestamped report there; unset writes nothing.
func TestPersistReport(t *testing.T) {
	report := []byte(`[{"id":"M-T01"}]`)

	if p, err := persistReport(report); err != nil || p != "" {
		t.Fatalf("unset MUTATION_REPORT_DIR: got path %q err %v", p, err)
	}

	dir := t.TempDir()
	t.Setenv("MUTATION_REPORT_DIR", dir)
	p, err := persistReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Fatal("expected a persisted path")
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(report) {
		t.Fatalf("persisted report mismatch: %q", got)
	}
	if base := filepath.Base(p); !strings.HasPrefix(base, "report-") || !strings.HasSuffix(base, ".json") {
		t.Fatalf("persisted filename must be report-<ts>.json: %q", base)
	}
}
