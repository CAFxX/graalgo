package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSyntheticContextM5 verifies: GG-0017 — P3-M5 (WS E, ADR-0005
// Appendix M) synthetic package shape: the single-result desugar helpers
// (__withCancel/__withCancelCause/__withDeadline/__withTimeout/
// __deadlineOf/__parseDuration, cancelFunc/cancelCauseFunc methods) are
// exported as real funcdecls; the 6 multi-result operations keep exact gc
// signatures but are NOT exported (their call sites desugar or fail
// fast); the retired ctxChanClose intrinsic is gone.
func TestSyntheticContextM5(t *testing.T) {
	src := `package main

import (
	"context"
	"time"
)

func main() {
	ctx := context.WithValue(context.Background(), "k", 1)
	_ = ctx
	d := 5 * time.Second
	_ = d
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "synctx.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						names[n] = true
					}
				}
			}
			// The retired ctxChanClose intrinsic lowered to a "call"
			// node with a "chanclose" marker; that marker must be gone
			// (user close(ch) now lowers to the "chanclose" statement).
			if v["kind"] == "call" {
				if f, ok := v["fields"].(map[string]any); ok {
					if _, ok := f["chanclose"]; ok {
						t.Errorf("found retired chanclose-marker call node")
					}
				}
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(raw)
	for _, want := range []string{
		"__withCancel", "__withCancelCause", "__withDeadline", "__withTimeout",
		"__deadlineOf", "__parseDuration", "cancelFunc", "cancelCauseFunc",
	} {
		if !names[want] {
			t.Errorf("synthetic helper %q not exported as funcdecl", want)
		}
	}
	for _, banned := range []string{
		"WithCancel", "WithCancelCause", "WithDeadline", "WithTimeout",
		"ParseDuration", "ctxChanClose",
	} {
		if names[banned] {
			t.Errorf("multi-result/intrinsic %q must not be exported as funcdecl", banned)
		}
	}
}

// TestMultiResultFailFastKept verifies: GG-0017 — outside the 2-target
// desugar position, the 5 reachable multi-result operations still fail
// fast with the named M5 scope error. (Single-target
// `d := time.ParseDuration(s)` is a go/types type error before the
// exporter runs; only type-valid non-2-target positions are tested here:
// statement and multi-arg spread. `ctx.Deadline()` is not covered here —
// it is callable since P4 (general interface MR) and desugars through
// the mrDeadline path instead of failing fast; see
// TestDeadlineCallableP4.)
func TestMultiResultFailFastKept(t *testing.T) {
	progs := map[string]string{
		"statement": `package main
import "context"
func main() { context.WithCancel(context.Background()) }
`,
		"spread": `package main
import "context"
func use(a context.Context, b context.CancelFunc) {}
func main() { use(context.WithCancel(context.Background())) }
`,
		"parseduration-statement": `package main
import "time"
func main() { time.ParseDuration("1s") }
`,
	}
	for name, src := range progs {
		dir := t.TempDir()
		p := filepath.Join(dir, "mrfail.go")
		if err := os.WriteFile(p, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := ExportFile(p)
		if err == nil {
			t.Errorf("%s: expected multi-result fail-fast, got success", name)
			continue
		}
		if !strings.Contains(err.Error(), "not in M5 scope") {
			t.Errorf("%s: error %q does not name the M5 scope", name, err)
		}
	}
}

// TestDeadlineCallableP4 verifies the P4 restoration of GG-0017 —
// P3-M5 (afc0dfb amendment, ADR-0005 Appendix M) deliberately omitted
// `Deadline` from the synthetic Context interface, so an interface-typed
// call was a go/types type error. The general interface multi-result
// mechanism (046afc86) removed that divergence: `Deadline()` is declared
// on the synthetic Context interface again (gc 1.27.1 method set
// identical), and call sites desugar through the live `mrDeadline`
// path. Statement position emits an ifacemethod node (results discarded,
// legal in gc); 2-target position lowers through a `__deadlineOf`
// pair temp exactly like the other Appendix M operations.
func TestDeadlineCallableP4(t *testing.T) {
	// mainBody returns the main funcdecl's body node, so the checks
	// below pin the user call site, not synthetic-internal nodes.
	mainBody := func(doc map[string]any) map[string]any {
		t.Helper()
		for _, n := range findAllKind(doc, "funcdecl") {
			if n["fields"].(map[string]any)["name"] == "main" {
				body, _ := n["fields"].(map[string]any)["body"].(map[string]any)
				return body
			}
		}
		t.Fatal("no main funcdecl in wire doc")
		return nil
	}
	stmtDoc := exportFuncValueProg(t, `package main
import "context"
func main() {
	ctx := context.Background()
	ctx.Deadline()
}
`)
	foundIface := false
	for _, n := range findAllKind(mainBody(stmtDoc), "ifacemethod") {
		if n["fields"].(map[string]any)["method"] == "Deadline" {
			foundIface = true
		}
	}
	if !foundIface {
		t.Fatal("statement-position ctx.Deadline() did not emit an ifacemethod node")
	}

	twoDoc := exportFuncValueProg(t, `package main
import "context"
func main() {
	ctx := context.Background()
	d, ok := ctx.Deadline()
	println(d.IsZero(), ok)
}
`)
	foundPair := false
	for _, n := range findAllKind(mainBody(twoDoc), "vardecl") {
		names, _ := n["fields"].(map[string]any)["names"].([]any)
		if len(names) != 1 {
			continue
		}
		name, _ := names[0].(string)
		if len(name) < 7 || name[:7] != "__p5mr_" {
			continue
		}
		vals, _ := n["fields"].(map[string]any)["values"].([]any)
		if len(vals) != 1 {
			continue
		}
		call, _ := vals[0].(map[string]any)
		if call["kind"] != "call" {
			continue
		}
		fun, _ := call["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if fun["fields"].(map[string]any)["name"] == "__deadlineOf" {
			foundPair = true
		}
	}
	if !foundPair {
		t.Fatal("2-target ctx.Deadline() did not desugar through a __deadlineOf pair temp")
	}
}

// TestMultiResultDesugarM5 verifies: GG-0017 — P3-M5 (WS E, ADR-0005
// Appendix M) call-site desugar: the 5 reachable multi-result operations
// (WithCancel, WithCancelCause, WithDeadline, WithTimeout,
// ParseDuration) in 2-target position lower to single-result helper
// calls + extraction (no fail-fast), for both := and = forms.
// (`ctx.Deadline()` is the 6th Appendix M operation; it was not callable
// in M5 but is callable since P4 via the mrDeadline path — see
// TestDeadlineCallableP4 — so this test covers the other 5.)
func TestMultiResultDesugarM5(t *testing.T) {
	src := `package main

import (
	"context"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctxcc, cancelcc := context.WithCancelCause(context.Background())
	cancelcc(context.Canceled)
	du, err := time.ParseDuration("5s")
	_, _ = du, err
	var ctx2 context.Context
	var cancel2 context.CancelFunc
	ctx2, cancel2 = context.WithTimeout(context.Background(), time.Second)
	_, _ = ctx2, cancel2
	ctx3, cancel3 := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	_, _ = ctx3, cancel3
	_, _, _ = ctx, ctxcc, ctx3
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "mrdesugar.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	// Collect ident names called, and check the desugar shape. Scope the
	// walk to main's body: the synthetic package funcdecls are compiled
	// whole into the same document and would pollute the counts (their
	// bodies contain converts, and __withTimeout's body calls
	// __withDeadline).
	var mainBody any
	var findMain func(v any)
	findMain = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "funcdecl" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok && n == "main" {
						mainBody = f["body"]
						return
					}
				}
			}
			for _, c := range v {
				if mainBody == nil {
					findMain(c)
				}
			}
		case []any:
			for _, c := range v {
				if mainBody == nil {
					findMain(c)
				}
			}
		}
	}
	findMain(raw)
	if mainBody == nil {
		t.Fatal("main funcdecl not found in export")
	}
	calledIdents := map[string]bool{}
	var temps []string
	var converts, methodrefs, selectors int
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "ident" {
				if f, ok := v["fields"].(map[string]any); ok {
					if n, ok := f["name"].(string); ok {
						calledIdents[n] = true
						if strings.HasPrefix(n, "__p5mr_") {
							temps = append(temps, n)
						}
					}
				}
			}
			if v["kind"] == "convert" {
				converts++
			}
			if v["kind"] == "methodref" {
				methodrefs++
			}
			if v["kind"] == "selector" {
				selectors++
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(mainBody)
	for _, want := range []string{"__withCancel", "__withCancelCause", "__withTimeout", "__withDeadline", "__parseDuration"} {
		if !calledIdents[want] {
			t.Errorf("desugar helper %q not called", want)
		}
	}
	if len(temps) == 0 {
		t.Errorf("no __p5mr_ temps emitted")
	}
	// Post-afc0dfb amendment: the desugar emits NO Context() interface
	// conversion (the extracted ctx keeps its concrete *cancelCtx /
	// *timerCtx type); pin that no convert node appears for it.
	if converts != 0 {
		t.Errorf("want 0 convert nodes (no Context boxing post-amendment), got %d", converts)
	}
	// Four cancelFunc-family methodrefs (WithCancel, WithCancelCause,
	// WithTimeout, WithDeadline).
	if methodrefs < 4 {
		t.Errorf("want >=4 methodref nodes (cancelFunc/cancelCauseFunc), got %d", methodrefs)
	}
	// Pair field selectors: d+err (ParseDuration) = 2. (Deadline's
	// d+ok pair is not desugared — not callable in M5.)
	if selectors < 2 {
		t.Errorf("want >=2 pair-field selectors, got %d", selectors)
	}
	// The 5 multi-result declarations are never called directly: every
	// 2-target call site desugared to a helper.
	for _, banned := range []string{"WithCancel", "WithCancelCause", "WithDeadline", "WithTimeout", "ParseDuration"} {
		if calledIdents[banned] {
			t.Errorf("direct call to %q survived desugaring", banned)
		}
	}
}
