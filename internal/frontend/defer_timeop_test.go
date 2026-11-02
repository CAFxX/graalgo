package main

// Regression tests for DEFER-TIMEOP (review 2026-11-02 D0): `defer`/`go`
// of a call that lowers to a non-call wire node.
//
// Timer/Ticker Stop/Reset lower to the "timeop" wire node, which has no
// "fun" field, so buildDefer/GoStatementNode.build failed with
// "missing required key fun". The fix wraps timeop in a funclit in
// exportDeferStmt/exportGoStmt: the defer form passes the timeop
// operands as funclit arguments (evaluated at defer-statement time by
// DeferNode), and the go form hoists them to temps evaluated at
// go-statement time (the exportCloseDeferGo precedents). Every other
// non-call lowering keeps failing loudly, now with a named frontend
// error instead of the backend's opaque "missing fun".
//
// These tests pin the exported wire shape; the runtime semantics
// (defer/go-time operand evaluation, observable Stop/Reset effects) are
// pinned by test/regression/pkg049 under `graalgo test`.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exportSrcErr is exportSrc without the fatal-on-error: it returns the
// exporter error so tests can assert loud rejections.
func exportSrcErr(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return ExportFile(p)
}

// timeopInFunclitBody reports whether a funclit node's body contains an
// exprstmt running a timeop with the given op.
func timeopInFunclitBody(t *testing.T, fun map[string]any, wantOp string) bool {
	t.Helper()
	body, ok := nodeFields(t, fun)["body"].(map[string]any)
	if !ok {
		return false
	}
	stmts, _ := nodeFields(t, body)["stmts"].([]any)
	for _, s := range stmts {
		m, _ := s.(map[string]any)
		if m["kind"] != "exprstmt" {
			continue
		}
		x, _ := nodeFields(t, m)["x"].(map[string]any)
		if x["kind"] == "timeop" {
			if op, _ := nodeFields(t, x)["op"].(string); op == wantOp {
				return true
			}
		}
	}
	return false
}

// TestDeferTimeOpStop: `defer tmr.Stop()` must export as a defer of a
// call to a funclit (not a bare timeop node, which buildDefer rejects),
// with the receiver passed as the funclit call's argument so DeferNode
// evaluates it at defer-statement time.
func TestDeferTimeOpStop(t *testing.T) {
	src := `package main

import "time"

func main() {
	tmr := time.NewTimer(time.Second)
	defer tmr.Stop()
}
`
	doc := exportSrc(t, src)
	defers := findNodes(t, doc, "defer")
	found := false
	for _, d := range defers {
		call, ok := nodeFields(t, d)["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			continue
		}
		fun, ok := nodeFields(t, call)["fun"].(map[string]any)
		if !ok || fun["kind"] != "funclit" {
			continue
		}
		if !timeopInFunclitBody(t, fun, "Stop") {
			continue
		}
		args, _ := nodeFields(t, call)["args"].([]any)
		if len(args) != 1 {
			t.Fatalf("defer timeop Stop call has %d args, want 1 (receiver)", len(args))
		}
		found = true
	}
	if !found {
		t.Fatal("no defer of a funclit running a timeop Stop")
	}
}

// TestDeferTimeOpReset: `defer tmr.Reset(d)` must export the same way,
// with receiver AND duration as funclit call arguments (both evaluated
// at defer-statement time).
func TestDeferTimeOpReset(t *testing.T) {
	src := `package main

import "time"

func main() {
	tmr := time.NewTimer(time.Second)
	defer tmr.Reset(2 * time.Second)
}
`
	doc := exportSrc(t, src)
	defers := findNodes(t, doc, "defer")
	found := false
	for _, d := range defers {
		call, ok := nodeFields(t, d)["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			continue
		}
		fun, ok := nodeFields(t, call)["fun"].(map[string]any)
		if !ok || fun["kind"] != "funclit" {
			continue
		}
		if !timeopInFunclitBody(t, fun, "Reset") {
			continue
		}
		args, _ := nodeFields(t, call)["args"].([]any)
		if len(args) != 2 {
			t.Fatalf("defer timeop Reset call has %d args, want 2 (receiver, duration)", len(args))
		}
		found = true
	}
	if !found {
		t.Fatal("no defer of a funclit running a timeop Reset")
	}
}

// TestGoTimeOpStop: `go tmr.Stop()` must export as a block hoisting the
// receiver to a temp and then a gostmt spawning a zero-arg funclit that
// runs the timeop (the exportCloseDeferGo go-branch precedent).
func TestGoTimeOpStop(t *testing.T) {
	src := `package main

import "time"

func main() {
	tmr := time.NewTimer(time.Second)
	go tmr.Stop()
}
`
	doc := exportSrc(t, src)
	// The go desugar emits a block: hoisted temp vardecl(s) + gostmt.
	blocks := findNodes(t, doc, "block")
	found := false
	for _, b := range blocks {
		stmts, _ := nodeFields(t, b)["stmts"].([]any)
		hasVardecl := false
		var gostmt map[string]any
		for _, s := range stmts {
			m, _ := s.(map[string]any)
			if m["kind"] == "vardecl" {
				hasVardecl = true
			}
			if m["kind"] == "gostmt" {
				gostmt = m
			}
		}
		if !hasVardecl || gostmt == nil {
			continue
		}
		call, ok := nodeFields(t, gostmt)["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			continue
		}
		fun, ok := nodeFields(t, call)["fun"].(map[string]any)
		if !ok || fun["kind"] != "funclit" {
			continue
		}
		args, _ := nodeFields(t, call)["args"].([]any)
		if len(args) != 0 {
			continue
		}
		if timeopInFunclitBody(t, fun, "Stop") {
			found = true
		}
	}
	if !found {
		t.Fatal("go timeop Stop did not export as hoisted temp + zero-arg funclit gostmt")
	}
}

// TestDeferOtherNonCallLoweringLoud: deferring a different non-call
// lowering (runtime.NumCPU -> runtimenumcpu) must fail at the frontend
// with a named error, not reach the backend's "missing required key
// fun". This pins the "loudly rejected" half of the DEFER-TIMEOP audit:
// timeop is wrapped; the rest are named rejections, not silent drops
// and not opaque backend failures.
func TestDeferOtherNonCallLoweringLoud(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	defer runtime.NumCPU()
}
`
	_, err := exportSrcErr(t, src)
	if err == nil {
		t.Fatal("defer runtime.NumCPU() exported without error, want a named frontend rejection")
	}
	if !strings.Contains(err.Error(), "runtimenumcpu") {
		t.Fatalf("defer rejection = %v, want it to name the runtimenumcpu lowering", err)
	}
}

// TestGoOtherNonCallLoweringLoud mirrors the defer rejection for the
// go statement.
func TestGoOtherNonCallLoweringLoud(t *testing.T) {
	src := `package main

import "runtime"

func main() {
	go runtime.NumCPU()
}
`
	_, err := exportSrcErr(t, src)
	if err == nil {
		t.Fatal("go runtime.NumCPU() exported without error, want a named frontend rejection")
	}
	if !strings.Contains(err.Error(), "runtimenumcpu") {
		t.Fatalf("go rejection = %v, want it to name the runtimenumcpu lowering", err)
	}
}

// TestDeferTimeOpDocParses: the timeop defer document must still decode
// as JSON (guard against malformed wire nodes from the wrap).
func TestDeferTimeOpDocParses(t *testing.T) {
	src := `package main

import "time"

func main() {
	tk := time.NewTicker(time.Second)
	defer tk.Stop()
}
`
	doc := exportSrc(t, src)
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
}
