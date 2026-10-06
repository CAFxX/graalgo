package main

// P3-M2b (WS D, ADR-0002): exporter tests for the synthetic `sync` package.
// Red-green: these fail before the exporter change (import "sync" is a
// WS G scope error) and pass after.

import (
	"encoding/json"
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

// Defer of a sync method desugars to defer of a funclit call (the frozen
// buildDefer only understands funclit/ident/builtin). A value-typed ident
// receiver is CAPTURED (address semantics: `defer m.Unlock()` uses &m, so a
// reassigned m still unlocks the current object — verified against gc).
func TestSyncDeferUnlockDesugars(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex

func main() {
	defer gMu.Unlock()
	gMu.Lock()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	deferNode := findKind(doc, "defer")
	if deferNode == nil {
		t.Fatal("no defer node in export")
	}
	call := fieldsOf(deferNode)["call"].(map[string]any)
	if call["kind"] != "call" {
		t.Fatalf("defer call kind = %v, want call", call["kind"])
	}
	cf := fieldsOf(call)
	fun := cf["fun"].(map[string]any)
	if fun["kind"] != "funclit" {
		t.Fatalf("defer fun kind = %v, want funclit", fun["kind"])
	}
	ff := fieldsOf(fun)
	// Value-typed ident receiver is captured: no params, no outer args.
	if params, _ := ff["params"].([]any); len(params) != 0 {
		t.Fatalf("funclit params = %d, want 0 (receiver captured)", len(params))
	}
	if args, _ := cf["args"].([]any); len(args) != 0 {
		t.Fatalf("defer call args = %d, want 0 (receiver captured)", len(args))
	}
	body := ff["body"].(map[string]any)
	stmts, _ := fieldsOf(body)["stmts"].([]any)
	x := fieldsOf(stmts[0].(map[string]any))["x"].(map[string]any)
	if x["kind"] != "syncop" || fieldsOf(x)["op"] != "Unlock" {
		t.Fatalf("funclit body = %v %v, want syncop Unlock", x["kind"], fieldsOf(x)["op"])
	}
	recv := fieldsOf(x)["recv"].(map[string]any)
	if recv["kind"] != "ident" || fieldsOf(recv)["name"] != "gMu" {
		t.Fatalf("syncop recv = %v %v, want captured ident gMu", recv["kind"], fieldsOf(recv)["name"])
	}
}

// Defer of a sync method with arguments: args are params (evaluated at
// defer time); a pointer-typed receiver is also a param.
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
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	deferNode := findKind(doc, "defer")
	if deferNode == nil {
		t.Fatal("no defer node in export")
	}
	call := fieldsOf(deferNode)["call"].(map[string]any)
	cf := fieldsOf(call)
	fun := cf["fun"].(map[string]any)
	ff := fieldsOf(fun)
	params, _ := ff["params"].([]any)
	// wg is value-typed ident -> captured; n -> one param.
	if len(params) != 1 {
		t.Fatalf("funclit params = %d, want 1 (the arg n)", len(params))
	}
	if args, _ := cf["args"].([]any); len(args) != 1 {
		t.Fatalf("defer call args = %d, want 1 (n evaluated at defer time)", len(args))
	}
}

// go m.Unlock() desugars to a zero-arg funclit spawn (GoStatementNode's
// P2-M1 contract).
func TestSyncGoUnlockDesugars(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex

func main() {
	gMu.Lock()
	go gMu.Unlock()
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	goNode := findKind(doc, "gostmt")
	if goNode == nil {
		t.Fatal("no gostmt node in export")
	}
	call := fieldsOf(goNode)["call"].(map[string]any)
	cf := fieldsOf(call)
	fun := cf["fun"].(map[string]any)
	if fun["kind"] != "funclit" {
		t.Fatalf("gostmt fun kind = %v, want funclit", fun["kind"])
	}
	if args, _ := cf["args"].([]any); len(args) != 0 {
		t.Fatalf("gostmt call args = %d, want 0", len(args))
	}
	body := fieldsOf(fun)["body"].(map[string]any)
	stmts, _ := fieldsOf(body)["stmts"].([]any)
	x := fieldsOf(stmts[0].(map[string]any))["x"].(map[string]any)
	if x["kind"] != "syncop" || fieldsOf(x)["op"] != "Unlock" {
		t.Fatalf("spawned body = %v %v, want syncop Unlock", x["kind"], fieldsOf(x)["op"])
	}
}

// Package-level `var m sync.Mutex` (no init) must carry the implicit
// syncnew in the globals entry — otherwise the frozen factory installs
// its struct zero (GoStruct) and every method call CCEs at runtime.
func TestSyncPackageVarImplicitSyncnew(t *testing.T) {
	src := `package main

import "sync"

var gMu sync.Mutex
var gWg sync.WaitGroup

func main() { gMu.Lock(); gMu.Unlock() }
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	globals, _ := doc.(map[string]any)["globals"].([]any)
	if len(globals) != 2 {
		t.Fatalf("globals = %d entries, want 2", len(globals))
	}
	for i, g := range globals {
		init := g.(map[string]any)["init"]
		if init == nil {
			t.Fatalf("globals[%d] init is null, want implicit syncnew", i)
		}
		if init.(map[string]any)["kind"] != "syncnew" {
			t.Fatalf("globals[%d] init kind = %v, want syncnew", i, init.(map[string]any)["kind"])
		}
	}
}
