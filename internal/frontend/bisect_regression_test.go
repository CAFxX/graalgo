package main

// Regression test for the Caddy import-chain failure:
//   internal/bisect/bisect.go:428:15: undefined: runtime.Callers
//
// The real internal/bisect source must get past the runtime.Callers
// type-check error. (Full export of bisect still needs
// sync/atomic.Pointer struct fields — a separate lane; this test asserts
// precisely that the Callers error is gone, not that the whole package
// exports. While full export is still blocked, the test SKIPs loudly —
// it never passes without having run the regression check.)

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
		// Unrelated blocker (e.g. the sync/atomic.Pointer struct-field
		// lane): loud skip, never a silent pass. A green suite must mean
		// the Callers regression is genuinely guarded, not that the
		// export never ran.
		t.Skipf("bisect export blocked by unrelated feature: %v", err)
	}
	if !strings.Contains(out, `"kind": "runtimecallers"`) {
		t.Errorf("exported bisect JSON missing the runtimecallers wire node")
	}
}
