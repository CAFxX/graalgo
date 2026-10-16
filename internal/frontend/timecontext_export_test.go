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
// post-P4 (cc0a78f4) it desugars via __deadlineOf like the other
// operations (see TestDeadlineDesugarsM5), so there is no fail-fast
// position left to pin for it.)
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

// TestDeadlineDesugarsM5 verifies: P4 (cc0a78f4) restored Deadline() to
// the synthetic Context interface, removing the P3-M5 deliberate
// divergence (afc0dfb amendment: the interface omitted Deadline so calls
// were go/types type errors). A ctx.Deadline() call now exports cleanly
// and desugars to the __deadlineOf helper — the Appendix M mrDeadline
// path, the same mechanism serving the other multi-result context
// operations. This test replaces TestDeadlineNotCallableM5, which pinned
// the pre-P4 divergence; the old expectation (type error) contradicts
// the now-intended behavior.
func TestDeadlineDesugarsM5(t *testing.T) {
	src := `package main
import "context"
func main() {
	ctx := context.Background()
	d, ok := ctx.Deadline()
	println(d.Unix(), ok)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "deadline.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ctx.Deadline() should export cleanly post-P4, got: %v", err)
	}
	doc := decodeExport(t, out)
	found := false
	for _, n := range findAllKind(doc, "call") {
		fun, _ := n["fields"].(map[string]any)["fun"].(map[string]any)
		if fun == nil || fun["kind"] != "ident" {
			continue
		}
		if name, _ := fun["fields"].(map[string]any)["name"].(string); name == "__deadlineOf" {
			found = true
		}
	}
	if !found {
		t.Error("ctx.Deadline() did not desugar to a __deadlineOf helper call")
	}
}

// TestMultiResultDesugarM5 verifies: GG-0017 — P3-M5 (WS E, ADR-0005
// Appendix M) call-site desugar: the 5 reachable multi-result operations
// (WithCancel, WithCancelCause, WithDeadline, WithTimeout,
// ParseDuration) in 2-target position lower to single-result helper
// calls + extraction (no fail-fast), for both := and = forms.
// (`ctx.Deadline()` is the 6th Appendix M operation; post-P4 (cc0a78f4)
// it desugars via __deadlineOf — see TestDeadlineDesugarsM5.)
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
	// d+ok pair is desugared via __deadlineOf — see
	// TestDeadlineDesugarsM5 — but this test's program never calls it.)
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
