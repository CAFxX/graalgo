package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticTimeDifferential extracts the synthetic time source and
// differential-tests it against gc's time package: time.Parse (via the
// real __parseTime) and the time.Time accessors. The table lives in
// testdata/synthtime_diff/ (package synthtime, so it can call the
// unexported __parseTime); this test copies it into a scratch module
// with the extracted source and runs `go test` there.
func TestSyntheticTimeDifferential(t *testing.T) {
	src := extractSyntheticTimeSource(t)
	// The embedded source must stay gofmt-clean.
	if out, err := gofmtCheck(t, src); err != nil || out != "" {
		t.Fatalf("synthetic time source is not gofmt-clean: %v\n%s", err, out)
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module synthtimetest\n\ngo 1.24\n")
	// Rename the package so it does not clash with the real time package
	// under test.
	renamed := strings.Replace(src, "package time\n", "package synthtime\n", 1)
	if renamed == src {
		t.Fatal("synthetic time source does not start with 'package time'")
	}
	writeFile(t, filepath.Join(dir, "synthtime.go"), renamed)
	for _, name := range []string{"diff_test.go", "roundtrip_test.go"} {
		data, err := os.ReadFile(filepath.Join("testdata", "synthtime_diff", name))
		if err != nil {
			t.Fatalf("read testdata %s: %v", name, err)
		}
		writeFile(t, filepath.Join(dir, name), string(data))
	}

	goBin := goToolchain(t)
	cmd := exec.Command(goBin, "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("differential test failed: %v\n%s", err, out)
	}
	t.Logf("differential suite:\n%s", out)
}

// extractSyntheticTimeSource returns the raw Go source of the synthetic
// time package (the body of the syntheticTimeSource const).
func extractSyntheticTimeSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("synth_time_context.go")
	if err != nil {
		t.Fatalf("read synth_time_context.go: %v", err)
	}
	s := string(data)
	const marker = "const syntheticTimeSource = `"
	start := strings.Index(s, marker)
	if start < 0 {
		t.Fatal("syntheticTimeSource const not found")
	}
	start += len(marker)
	end := strings.Index(s[start:], "`\n\n// syntheticContextSource")
	if end < 0 {
		t.Fatal("end of syntheticTimeSource const not found")
	}
	return s[start : start+end]
}

func gofmtCheck(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "src.go")
	writeFile(t, p, src)
	// gofmt -l lists files whose formatting differs.
	gofmtBin := filepath.Join(filepath.Dir(goToolchain(t)), "gofmt")
	cmd := exec.Command(gofmtBin, "-l", p)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func goToolchain(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("GO"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if _, err := os.Stat(filepath.Join(home, "tools", "go", "bin", "go")); err == nil {
			return filepath.Join(home, "tools", "go", "bin", "go")
		}
	}
	if v, err := exec.LookPath("go"); err == nil {
		return v
	}
	t.Fatal("no Go toolchain found (set $GO)")
	return ""
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
