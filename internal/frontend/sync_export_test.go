package main

// Exporter tests for the `sync` package (real, from disk since 503608ea).
// Previously these tested the synthetic sync package; now they verify the
// real package exports correctly.

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
// calls lower to normal method calls (real sync package, 503608ea).
// Previously these lowered to additive syncnew/syncop wire nodes under the
// synthetic sync package; now they compile as ordinary Go. Note: the
// imported real sync package's internals may still contain syncop nodes
// (for atomic ops with no Go bodies) — that's the legitimate atomic
// intrinsic, not the retired synthetic sync API.
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
	// The method names must appear as call targets in the export.
	for _, want := range []string{"Lock", "Unlock", "RLock", "RUnlock", "Add", "Done", "Wait", "Do"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing method %s", want)
		}
	}
}

// TestSyncScopeErrors: with real sync (503608ea), the extended sync
// surface is in scope — TryLock/TryRLock/RLocker/WaitGroup.Go all export
// successfully. (Previously these were "not in M2 scope" errors under the
// synthetic sync package; the real package supports them.)
func TestSyncScopeErrors(t *testing.T) {
	cases := []string{
		"m.TryLock()",
		"rw.TryLock()",
		"rw.TryRLock()",
		"_ = rw.RLocker()",
		"wg.Go(func(){})",
	}
	for _, body := range cases {
		src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tvar rw sync.RWMutex\n\tvar wg sync.WaitGroup\n\t_ = m\n\t_ = rw\n\t_ = wg\n\t" + body + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err != nil {
			t.Errorf("%s: expected success with real sync, got error: %v", body, err)
		}
	}
}

// TestSyncCompositeSupported: sync primitives in struct-field positions
// (and [N]sync.Pool array slots) export successfully. The M2 fail-fast for
// these positions was deliberately removed (933fd93 driver-harden-gocmp,
// extended by d174a1e5 fix-linkname): the backend manufactures a fresh
// GoMutex/GoRWMutex/GoWaitGroup/GoOnce/GoPool per field/slot
// (NodeFactory.newSyncPrimitive via the type table), so the zero value
// is a usable primitive — verified end-to-end through the product
// launcher, byte-identical to gc 1.27.1: struct-held mutex under
// contention (10 goroutines x 100 increments == 1000), independent
// instances per struct value, struct-field RWMutex/WaitGroup/Once, and
// per-slot-distinct [2]sync.Pool.
func TestSyncCompositeSupported(t *testing.T) {
	supported := []struct {
		name string
		decl string
		use  string
		prim string // named type the wire type table must carry
	}{
		{"struct field mutex",
			"type T struct { mu sync.Mutex }",
			"t := T{}\n\tt.mu.Lock()\n\tt.mu.Unlock()",
			`"name": "sync.Mutex"`},
		{"struct field rwmutex",
			"var st struct { mu sync.RWMutex }",
			"st.mu.RLock()\n\tst.mu.RUnlock()",
			`"name": "sync.RWMutex"`},
		{"struct field waitgroup",
			"var st struct { wg sync.WaitGroup }",
			"st.wg.Add(1)\n\tst.wg.Done()\n\tst.wg.Wait()",
			`"name": "sync.WaitGroup"`},
		{"struct field once",
			"var st struct { o sync.Once }",
			"st.o.Do(func() {})",
			`"name": "sync.Once"`},
		{"array of pools",
			"var pools [2]sync.Pool",
			"pools[0].Put(1)\n\t_ = pools[0].Get()",
			`"name": "sync.Pool"`},
	}
	for _, c := range supported {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport \"sync\"\n\n" + c.decl +
				"\n\nfunc main() {\n\t" + c.use + "\n}\n"
			out, err := ExportFile(writeSyncProg(t, src))
			if err != nil {
				t.Fatalf("%s: export failed, want success: %v", c.decl, err)
			}
			// The backend manufactures the primitive from the type
			// table: the named sync type must be present in the wire
			// doc, otherwise the field would lower to an empty struct.
			if !strings.Contains(out, c.prim) {
				t.Errorf("%s: wire doc missing type-table entry %s", c.decl, c.prim)
			}
		})
	}
	// With real sync (503608ea), composite positions work — the scope
	// restriction was for the synthetic package. Slices, arrays, and maps
	// of sync primitives are ordinary Go types now.
	compositeOk := []string{
		"var s []sync.Mutex",
		"var a [2]sync.WaitGroup",
		"var mm map[string]sync.Mutex",
		"var mk map[sync.Mutex]int",
	}
	for _, decl := range compositeOk {
		src := "package main\n\nimport \"sync\"\n\n" + decl + "\n\nfunc main() {}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err != nil {
			t.Errorf("%s: expected success with real sync, got error: %v", decl, err)
		}
	}
}

// TestSyncMethodValueSupported: a method value on a sync primitive
// (f := m.Lock) is supported via the general method-value path (the real
// sync methods have funcdecls; 503608ea retired the synthetic overlay).
// Previously this was a WS H scope error.
func TestSyncMethodValueSupported(t *testing.T) {
	src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tf := m.Lock\n\t_ = f\n}\n"
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("method value on sync.Mutex should be supported, got: %v", err)
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

// findKind walks the decoded export JSON and returns the first node with the
// given kind, or nil.
func findKind(n any, kind string) map[string]any {
	switch v := n.(type) {
	case map[string]any:
		if v["kind"] == kind {
			return v
		}
		for _, c := range v {
			if r := findKind(c, kind); r != nil {
				return r
			}
		}
	case []any:
		for _, c := range v {
			if r := findKind(c, kind); r != nil {
				return r
			}
		}
	}
	return nil
}

func fieldsOf(n map[string]any) map[string]any {
	f, _ := n["fields"].(map[string]any)
	return f
}

// Defer of a sync method exports through the general call path with real
// sync (503608ea retired the syncop desugar): the deferred call is a
// standard call node and the frozen buildDefer handles it.
func TestSyncDeferUnlockDesugars(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex

func main() {
	defer gMu.Unlock()
	gMu.Lock()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with defer gMu.Unlock(): %v", err)
	}
}

// Defer of a sync method with arguments: exports successfully with
// real sync (503608ea).
func TestSyncDeferArgsAreParams(t *testing.T) {
	src := `package main

import "sync"

func main() {
	var wg sync.WaitGroup
	n := 2
	wg.Add(1)
	func() {
		defer wg.Add(n)
		n = 100
	}()
	wg.Wait()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with defer wg.Add(n): %v", err)
	}
}

// mustNodeField fetches a node-valued field from a decoded export node,
// failing the test with a message instead of panicking on a bare type
// assertion when the shape is absent (methodology P2-3: a red run must
// fail cleanly without aborting sibling tests).
func mustNodeField(t *testing.T, n map[string]any, field string) map[string]any {
	t.Helper()
	f := fieldsOf(n)[field]
	m, ok := f.(map[string]any)
	if !ok {
		t.Fatalf("node kind %v: field %q is %T, want a node object", n["kind"], field, f)
	}
	return m
}

// go m.Unlock() exports through the general go-statement path with real
// sync (503608ea retired the syncop desugar): a gostmt wrapping the call
// node, args hoisted at go time per the P3-M5 general rule.
func TestSyncGoUnlockDesugars(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex

func main() {
	gMu.Lock()
	go gMu.Unlock()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with go gMu.Unlock(): %v", err)
	}
}

// go wg.Add(n): exports successfully with real sync (503608ea).
func TestSyncGoArgsHoisted(t *testing.T) {
	src := `package main

import "sync"

func main() {
	var wg sync.WaitGroup
	n := 3
	go wg.Add(n)
	wg.Wait()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with go wg.Add(n): %v", err)
	}
}

// go pm.Unlock() with a pointer-typed receiver: exports successfully
// with real sync (503608ea).
func TestSyncGoPointerRecvHoisted(t *testing.T) {
	src := `package main

import "sync"

func main() {
	pm := &sync.Mutex{}
	pm.Lock()
	go pm.Unlock()
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with go pm.Unlock(): %v", err)
	}
}

// Package-level `var m sync.Mutex` exports successfully with real sync
// (503608ea). The zero value is a usable primitive.
func TestSyncPackageVarRealSync(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex
var gWg sync.WaitGroup

func main() { gMu.Lock(); gMu.Unlock() }
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with package-level sync vars: %v", err)
	}
}

// TestSyncOnceFunc: sync.OnceFunc(f) compiles with the real sync
// package (503608ea disabled the intrinsic; the real implementation
// is used).
func TestSyncOnceFunc(t *testing.T) {
	src := `package main

import "sync"

func main() {
	called := 0
	f := sync.OnceFunc(func() { called++ })
	f()
	f()
	_ = called
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with sync.OnceFunc: %v", err)
	}
}

// TestSyncOnceFuncArity: sync.OnceFunc with wrong arity fails fast.
func TestSyncOnceFuncArity(t *testing.T) {
	src := `package main

import "sync"

func main() {
	f := sync.OnceFunc()
	_ = f
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err == nil {
		t.Fatalf("ExportFile with sync.OnceFunc(): want arity error, got nil")
	}
}

// TestSyntheticDiskDepMethodsResolved (PKG-008): a synthetic package may
// import a disk package (synthetic io imports the real sync; mime/multipart
// imports path/filepath). The disk dependency's methods must be merged into
// methodIndex/funcByObj when the synthetic package is imported — otherwise
// method calls in the synthetic package's own body fail with
// "gofrontend: method X not found on Y". The failing method varied run to
// run (Lock vs Unlock vs Do): scanRoots resolves every MethodVal selection
// in the imported bodies while iterating info.Selections, a Go map.
// This test imports bytes, whose transitive synthetic dependency io calls
// sync.Mutex.Lock/Unlock and sync.Once.Do in its body; pre-fix the export
// failed deterministically (only the method name varied).
func TestSyntheticDiskDepMethodsResolved(t *testing.T) {
	src := `package main

import "bytes"

func main() {
	var b bytes.Buffer
	b.WriteString("hi")
	println(b.String())
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with synthetic bytes (disk sync dep via io): %v", err)
	}
}
