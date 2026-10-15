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
	// Positions that still fail fast: slice elements and map keys/elements
	// zero through the factory to an unusable empty struct (export.go:
	// syntheticCompositeScopeError). Non-Pool array elements stay scoped
	// too — a conservative frontend gate even though the backend could
	// manufacture per-slot zeros there. [N]sync.Pool is the deliberate
	// exception: the factory manufactures a distinct GoPool per slot
	// (NodeFactory.zeroArrayFor), which regexp's matchPool needs.
	stillScoped := map[string]string{
		"var s []sync.Mutex":           "not in M2 scope",
		"var a [2]sync.WaitGroup":      "not in M2 scope",
		"var mm map[string]sync.Mutex": "not in M2 scope",
		"var mk map[sync.Mutex]int":    "not in M2 scope",
	}
	for decl, want := range stillScoped {
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

// TestSyncMethodValueSupported: a method value on a sync primitive
// (f := m.Lock) is supported (fix-linkname: exportPrimMethodValue lowers
// it to a capturing funclit). Previously this was a WS H scope error.
func TestSyncMethodValueSupported(t *testing.T) {
	src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar m sync.Mutex\n\tf := m.Lock\n\t_ = f\n}\n"
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("method value on sync.Mutex should be supported, got: %v", err)
	}
	if !strings.Contains(out, "__primrecv_") {
		t.Errorf("method value did not lower to a capturing funclit")
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
	call := mustNodeField(t, deferNode, "call")
	cf := fieldsOf(call)
	fun := mustNodeField(t, call, "fun")
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

// findGostmtBlock returns the first "block" node having a direct "gostmt"
// child statement (the exportSyncGoStmt desugar shape), or nil.
func findGostmtBlock(n any) map[string]any {
	var rec func(n any) map[string]any
	rec = func(n any) map[string]any {
		switch v := n.(type) {
		case map[string]any:
			if v["kind"] == "block" {
				if stmts, _ := fieldsOf(v)["stmts"].([]any); stmts != nil {
					for _, s := range stmts {
						if sm, ok := s.(map[string]any); ok && sm["kind"] == "gostmt" {
							return v
						}
					}
				}
			}
			for _, c := range v {
				if r := rec(c); r != nil {
					return r
				}
			}
		case []any:
			for _, c := range v {
				if r := rec(c); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return rec(n)
}

// go m.Unlock() desugars to a block wrapping a zero-arg funclit spawn
// (GoStatementNode's P2-M1 contract). With no args and a value-typed ident
// receiver there is nothing to hoist, so the block holds just the spawn;
// the receiver is captured by reference in the body (gc's &m address
// semantics).
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
	blk := findGostmtBlock(doc)
	if blk == nil {
		t.Fatal("no block wrapping a gostmt in export")
	}
	stmts, _ := fieldsOf(blk)["stmts"].([]any)
	if len(stmts) != 1 {
		t.Fatalf("block stmts = %d, want 1 (no hoists for zero-arg go)", len(stmts))
	}
	goNode, _ := stmts[0].(map[string]any)
	if goNode["kind"] != "gostmt" {
		t.Fatalf("block stmt kind = %v, want gostmt", goNode["kind"])
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
	bstmts, _ := fieldsOf(body)["stmts"].([]any)
	x := fieldsOf(bstmts[0].(map[string]any))["x"].(map[string]any)
	if x["kind"] != "syncop" || fieldsOf(x)["op"] != "Unlock" {
		t.Fatalf("spawned body = %v %v, want syncop Unlock", x["kind"], fieldsOf(x)["op"])
	}
	recv := fieldsOf(x)["recv"].(map[string]any)
	if recv["kind"] != "ident" || fieldsOf(recv)["name"] != "gMu" {
		t.Fatalf("syncop recv = %v %v, want captured ident gMu", recv["kind"], fieldsOf(recv)["name"])
	}
}

// go wg.Add(n): the argument is hoisted into a synthetic temporary at
// go-statement time (Go spec evaluation timing); the zero-arg spawn's body
// references the temporary, and the temporary's value expression is the
// original argument. The value-typed ident receiver stays captured.
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
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export is not JSON: %v", err)
	}
	blk := findGostmtBlock(doc)
	if blk == nil {
		t.Fatal("no block wrapping a gostmt in export")
	}
	stmts, _ := fieldsOf(blk)["stmts"].([]any)
	if len(stmts) != 2 {
		t.Fatalf("block stmts = %d, want 2 (vardecl hoist + gostmt)", len(stmts))
	}
	vd, _ := stmts[0].(map[string]any)
	if vd["kind"] != "vardecl" {
		t.Fatalf("block stmt 0 kind = %v, want vardecl", vd["kind"])
	}
	vf := fieldsOf(vd)
	names, _ := vf["names"].([]any)
	if len(names) != 1 || names[0] != "__sync_go_arg0_0" {
		t.Fatalf("vardecl names = %v, want [__sync_go_arg0_0]", names)
	}
	if vf["define"] != true {
		t.Fatalf("vardecl define = %v, want true", vf["define"])
	}
	values, _ := vf["values"].([]any)
	if len(values) != 1 {
		t.Fatalf("vardecl values = %d, want 1", len(values))
	}
	val0, _ := values[0].(map[string]any)
	if val0["kind"] != "ident" || fieldsOf(val0)["name"] != "n" {
		t.Fatalf("hoist value = %v %v, want ident n", val0["kind"], fieldsOf(val0)["name"])
	}
	objs, _ := vf["objs"].([]any)
	if len(objs) != 1 {
		t.Fatalf("vardecl objs = %d, want 1", len(objs))
	}
	goNode, _ := stmts[1].(map[string]any)
	call := fieldsOf(goNode)["call"].(map[string]any)
	cf := fieldsOf(call)
	if args, _ := cf["args"].([]any); len(args) != 0 {
		t.Fatalf("gostmt call args = %d, want 0 (zero-arg spawn contract)", len(args))
	}
	fun := cf["fun"].(map[string]any)
	body := fieldsOf(fun)["body"].(map[string]any)
	bstmts, _ := fieldsOf(body)["stmts"].([]any)
	x := fieldsOf(bstmts[0].(map[string]any))["x"].(map[string]any)
	if x["kind"] != "syncop" || fieldsOf(x)["op"] != "Add" {
		t.Fatalf("spawned body = %v %v, want syncop Add", x["kind"], fieldsOf(x)["op"])
	}
	xf := fieldsOf(x)
	bargs, _ := xf["args"].([]any)
	if len(bargs) != 1 {
		t.Fatalf("syncop args = %d, want 1", len(bargs))
	}
	barg0, _ := bargs[0].(map[string]any)
	bf := fieldsOf(barg0)
	if barg0["kind"] != "ident" || bf["name"] != "__sync_go_arg0_0" {
		t.Fatalf("syncop arg = %v %v, want ident __sync_go_arg0_0", barg0["kind"], bf["name"])
	}
	if bf["obj"] != objs[0] {
		t.Fatalf("syncop arg obj = %v, want the hoisted temporary %v", bf["obj"], objs[0])
	}
	recv := xf["recv"].(map[string]any)
	if recv["kind"] != "ident" || fieldsOf(recv)["name"] != "wg" {
		t.Fatalf("syncop recv = %v %v, want captured ident wg", recv["kind"], fieldsOf(recv)["name"])
	}
}

// go pm.Unlock() with a pointer-typed receiver: the receiver is hoisted
// into a __sync_go_recv temporary evaluated at go-statement time (same
// evaluation-timing rule as the args).
func TestSyncGoPointerRecvHoisted(t *testing.T) {
	src := `package main

import "sync"

func main() {
	pm := &sync.Mutex{}
	pm.Lock()
	go pm.Unlock()
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
	blk := findGostmtBlock(doc)
	if blk == nil {
		t.Fatal("no block wrapping a gostmt in export")
	}
	stmts, _ := fieldsOf(blk)["stmts"].([]any)
	if len(stmts) != 2 {
		t.Fatalf("block stmts = %d, want 2 (vardecl hoist + gostmt)", len(stmts))
	}
	vd, _ := stmts[0].(map[string]any)
	names, _ := fieldsOf(vd)["names"].([]any)
	if len(names) != 1 || names[0] != "__sync_go_recv0_0" {
		t.Fatalf("vardecl names = %v, want [__sync_go_recv0_0]", names)
	}
	goNode, _ := stmts[1].(map[string]any)
	call := fieldsOf(goNode)["call"].(map[string]any)
	fun := fieldsOf(call)["fun"].(map[string]any)
	body := fieldsOf(fun)["body"].(map[string]any)
	bstmts, _ := fieldsOf(body)["stmts"].([]any)
	x := fieldsOf(bstmts[0].(map[string]any))["x"].(map[string]any)
	recv := fieldsOf(x)["recv"].(map[string]any)
	if recv["kind"] != "ident" || fieldsOf(recv)["name"] != "__sync_go_recv0_0" {
		t.Fatalf("syncop recv = %v %v, want ident __sync_go_recv0_0", recv["kind"], fieldsOf(recv)["name"])
	}
	if fieldsOf(recv)["obj"] != fieldsOf(vd)["objs"].([]any)[0] {
		t.Fatal("syncop recv obj does not match the hoisted temporary")
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

// TestSyncOnceFunc: sync.OnceFunc(f) lowers to a funclit capturing a
// fresh sync.Once; the closure body is once.Do(f) via the syncop wire
// node. Red-green: fails before the OnceFunc intrinsic ("call to unknown
// function OnceFunc"), passes after.
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
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with sync.OnceFunc: %v", err)
	}
	for _, want := range []string{
		`"kind": "funclit"`,
		`"kind": "syncnew"`,
		`"kind": "syncop"`,
		`"op": "Do"`,
		`"synctyp": "Once"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
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
