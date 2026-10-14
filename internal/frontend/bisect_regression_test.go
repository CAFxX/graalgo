package main

// Regression test for the Caddy import-chain failure:
//   internal/bisect/bisect.go:428:15: undefined: runtime.Callers
//
// The real internal/bisect source must get past the runtime.Callers
// type-check error. (Full export of bisect still needs
// sync/atomic.Pointer struct fields — a separate lane; this test asserts
// precisely that the Callers error is gone, not that the whole package
// exports.)

import (
	"os"
	"strings"
	"testing"

	"path/filepath"
)

func toolchainGoRoot(t *testing.T) string {
	t.Helper()
	if gr := os.Getenv("GOROOT"); gr != "" {
		return gr
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	for _, cand := range []string{
		filepath.Join(home, "tools", "go"),
		filepath.Join(home, "tools", "go-1.27.1"),
	} {
		if st, err := os.Stat(filepath.Join(cand, "VERSION")); err == nil && !st.IsDir() {
			return cand
		}
	}
	t.Skip("no Go toolchain found for the bisect regression test")
	return ""
}

// TestBisectNoCallersError: exporting the real internal/bisect source must
// not fail with "undefined: runtime.Callers".
func TestBisectNoCallersError(t *testing.T) {
	goRoot := toolchainGoRoot(t)
	path := filepath.Join(goRoot, "src", "internal", "bisect", "bisect.go")
	out, err := ExportFile(path)
	if err != nil {
		if strings.Contains(err.Error(), "undefined: runtime.Callers") {
			t.Fatalf("regression: %v", err)
		}
		t.Logf("bisect export blocked by unrelated feature: %v", err)
		return
	}
	if !strings.Contains(out, `"kind": "runtimecallers"`) {
		t.Errorf("exported bisect JSON missing the runtimecallers wire node")
	}
}
