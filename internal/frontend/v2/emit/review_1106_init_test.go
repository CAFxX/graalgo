// Regression tests for the review-1106-p1-v2-init remediation lane
// (independent codebase review, 2026-11-06, R-P1-3).
//
// v2 silently emitted package-level `func init()` as an ordinary
// funcdecl that nothing ever calls, silently dropping package
// initialization. emitFuncDecl now rejects it loudly, matching the
// p3frontend contract (flatten.go rejectInitFunc).
//
// The guard is load-bearing: a *method* named init is an ordinary
// callable method, not package initialization, and must still emit.
// This test is demonstrably capable of failing: it fails on the
// pre-fix emitter only if the rejection is over-broad (e.g. keyed on
// the bare name without the receiver check).
package emit

import (
	"testing"
)

func TestMethodNamedInitStillEmits(t *testing.T) {
	doc := emitTestSrc(t, `package main
type T struct{}
func (T) init() { println("method init") }
func main() {
	var v T
	v.init()
}
`)
	// The method must be present in Funcs as a funcdecl with a receiver.
	found := false
	for _, fn := range doc.Funcs {
		m, ok := fn.(map[string]any)
		if !ok {
			continue
		}
		fields, ok := m["fields"].(map[string]any)
		if !ok {
			continue
		}
		if fields["name"] == "init" && fields["receiver"] != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("method named init: no funcdecl with a receiver in Funcs (got %d funcs)", len(doc.Funcs))
	}
}
