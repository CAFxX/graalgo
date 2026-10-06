package main

// P3-M2b (WS D, ADR-0002): exporter tests for the synthetic `sync` package.
// Red-green: these fail before the exporter change (import "sync" is a
// WS G scope error) and pass after.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSyncProg writes src to a temp .go file and returns its path.
func writeSyncProg(t *testing.T, src string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSyncImportAccepted: import "sync" exports; construction and method
// calls lower to the additive syncnew/syncop wire nodes.
func TestSyncImportAccepted(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex

func main() {
	var m sync.Mutex
	m.Lock()
	m.Unlock()
	m2 := sync.Mutex{}
	m2.Lock()
	m2.Unlock()
	pm := new(sync.Mutex)
	pm.Lock()
	pm.Unlock()
	pm2 := &sync.RWMutex{}
	pm2.RLock()
	pm2.RUnlock()
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	wg.Wait()
	var o sync.Once
	o.Do(func() {})
	gMu.Lock()
	gMu.Unlock()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with import \"sync\": %v", err)
	}
	for _, want := range []string{
		`"kind": "syncnew"`,
		`"kind": "syncop"`,
		`"op": "Lock"`,
		`"op": "Unlock"`,
		`"op": "RLock"`,
		`"op": "RUnlock"`,
		`"op": "Add"`,
		`"op": "Done"`,
		`"op": "Wait"`,
		`"op": "Do"`,
		`"synctyp": "Mutex"`,
		`"synctyp": "RWMutex"`,
		`"synctyp": "WaitGroup"`,
		`"synctyp": "Once"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

// TestSyncScopeErrors: out-of-M2-scope sync surface fails fast with a
// named "not in M2 scope" error — never silently.
func TestSyncScopeErrors(t *testing.T) {
	cases := map[string]string{
		// TryLock/TryRLock/RLocker/WaitGroup.Go: defined, out of scope.
		"m.TryLock()":     "not in M2 scope",
		"rw.TryLock()":    "not in M2 scope",
		"rw.TryRLock()":   "not in M2 scope",
		"rw.RLocker()":    "not in M2 scope",
		"wg.Go(func(){})": "not in M2 scope",
	}
	for body, want := range cases {
		src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tvar rw sync.RWMutex\n\tvar wg sync.WaitGroup\n\t_ = m\n\t_ = rw\n\t_ = wg\n\t" + body + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%s: expected scope error, got success", body)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q does not contain %q", body, err, want)
		}
	}
}

// TestSyncCompositeScopeErrors: sync primitives in factory-zero positions
// (struct fields, array/slice elements, map keys/elements) fail fast.
func TestSyncCompositeScopeErrors(t *testing.T) {
	cases := map[string]string{
		"type T struct { mu sync.Mutex }":   "not in M2 scope",
		"var s []sync.Mutex":                "not in M2 scope",
		"var a [2]sync.WaitGroup":           "not in M2 scope",
		"var mm map[string]sync.Mutex":      "not in M2 scope",
		"var mk map[sync.Mutex]int":         "not in M2 scope",
		"var st struct { mu sync.RWMutex }": "not in M2 scope",
	}
	for decl, want := range cases {
		src := "package main\n\nimport \"sync\"\n\n" + decl + "\n\nfunc main() {}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%s: expected scope error, got success", decl)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q does not contain %q", decl, err, want)
		}
	}
}

// TestSyncMethodValueScopeError: a method value on a sync primitive
// (f := m.Lock) fails fast with the existing WS H scope error.
func TestSyncMethodValueScopeError(t *testing.T) {
	src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tf := m.Lock\n\t_ = f\n}\n"
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatal("expected method-value scope error, got success")
	}
	if !strings.Contains(err.Error(), "method value") {
		t.Fatalf("expected method-value scope error, got: %v", err)
	}
}

// TestSyncPointerFieldAllowed: *sync.Mutex in composite positions is fine
// (nil is the correct zero) — only the bare type is out of scope.
func TestSyncPointerFieldAllowed(t *testing.T) {
	src := `package main

import "sync"

type T struct {
	mu *sync.Mutex
}

func main() {
	t := T{}
	if t.mu != nil {
		panic("want nil")
	}
	t.mu = &sync.Mutex{}
	t.mu.Lock()
	t.mu.Unlock()
	var s []*sync.WaitGroup
	_ = s
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("pointer-to-sync in composite positions must be allowed: %v", err)
	}
}
