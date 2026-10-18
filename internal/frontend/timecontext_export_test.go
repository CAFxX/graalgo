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
// __withDeadlineCause/__withTimeoutCause/__deadlineOf/__parseDuration,
// __parseTime, cancelFunc/cancelCauseFunc methods) are exported as real
// funcdecls; the 8 multi-result operations keep exact gc signatures but
// are NOT exported (their call sites desugar or fail fast); the retired
// ctxChanClose intrinsic is gone.
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
		"__withDeadlineCause", "__withTimeoutCause",
		"__deadlineOf", "__parseDuration", "__parseTime",
		"cancelFunc", "cancelCauseFunc",
	} {
		if !names[want] {
			t.Errorf("synthetic helper %q not exported as funcdecl", want)
		}
	}
	for _, banned := range []string{
		"WithCancel", "WithCancelCause", "WithDeadline", "WithTimeout",
		"WithDeadlineCause", "WithTimeoutCause",
		"ParseDuration", "Parse", "ctxChanClose",
	} {
		if names[banned] {
			t.Errorf("multi-result/intrinsic %q must not be exported as funcdecl", banned)
		}
	}
}

// TestMultiResultFailFastKept verifies: GG-0017 — outside the 2-target
// desugar position, the 8 reachable multi-result operations still fail
// fast with the named M5 scope error. (Single-target
// `d := time.ParseDuration(s)` is a go/types type error before the
// exporter runs; only type-valid non-2-target positions are tested here:
// statement and multi-arg spread. `ctx.Deadline()` is not covered here —
// post-afc0dfb the synthetic Context interface omits Deadline, so an
// interface-typed call is a type error, never a scope error; see
// TestDeadlineNotCallableM5.)
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
		// The Cause variants are M5-kind (mrWithDeadline), so they keep
		// the pinned statement-position fail-fast like their plain
		// siblings — unlike time.Parse, which is M7 and desugars in
		// statement position (see TestParseStatementDesugarsM7).
		"withdeadlinecause-statement": `package main
import (
	"context"
	"time"
)
func main() { context.WithDeadlineCause(context.Background(), time.Now(), context.Canceled) }
`,
		"withtimeoutcause-statement": `package main
import (
	"context"
	"time"
)
func main() { context.WithTimeoutCause(context.Background(), time.Second, context.Canceled) }
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

// TestParseStatementDesugarsM7 verifies: time.Parse (M7 table entry,
// review-p1-time lane) as a bare statement desugars to the __parseTime
// helper call with the pair dropped — the P3-M8 statement-position rule
// for mrM7 plans (export.go ExprStmt case). This is deliberate: M7 ops
// have real helpers, so discarding results is honest, unlike the M5
// kinds which keep the pinned fail-fast (TestMultiResultFailFastKept).
func TestParseStatementDesugarsM7(t *testing.T) {
	src := `package main
import "time"
func main() { time.Parse(time.RFC3339, "2021-03-14T15:09:26Z") }
`
	dir := t.TempDir()
	p := filepath.Join(dir, "parsestmt.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("statement-position time.Parse should desugar, got error: %v", err)
	}
	if !strings.Contains(out, "__parseTime") {
		t.Errorf("statement-position time.Parse did not desugar to __parseTime")
	}
}

// TestDeadlineNotCallableM5 verifies: GG-0017 — P3-M5 (afc0dfb
// amendment, ADR-0005 Appendix M) `Context.Deadline()` is NOT callable
// in M5. The synthetic Context interface deliberately omits Deadline
// (v1 cannot emit multi-result methods, WS D), so a call on an
// interface-typed value is a go/types type error — it never reaches the
// exporter and never produces the M5 scope error. The exporter-side
// `mrDeadline` desugar path is retained defense-in-depth for WS D (see
// mrdesugar_m5.go); `__deadlineOf` remains for synthetic-internal use.
func TestDeadlineNotCallableM5(t *testing.T) {
	src := `package main
import "context"
func main() {
	ctx := context.Background()
	ctx.Deadline()
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "deadline.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatal("expected type error for ctx.Deadline(), got success")
	}
	if strings.Contains(err.Error(), "not in M5 scope") {
		t.Errorf("ctx.Deadline() must be a type error, not a scope error: %q", err)
	}
	if !strings.Contains(err.Error(), "Deadline") {
		t.Errorf("expected the type error to name Deadline, got %q", err)
	}
}

// TestMultiResultDesugarM5 verifies: GG-0017 — P3-M5 (WS E, ADR-0005
// Appendix M) call-site desugar: the 7 reachable multi-result operations
// (WithCancel, WithCancelCause, WithDeadline, WithTimeout,
// WithDeadlineCause, WithTimeoutCause, ParseDuration) in 2-target
// position lower to single-result helper calls + extraction (no
// fail-fast), for both := and = forms, plus time.Parse via the M7 table.
// (`ctx.Deadline()` is the 8th Appendix M operation but is not callable
// in M5 — the synthetic Context interface omits it post-afc0dfb, so no
// user call site can type-check; see TestDeadlineNotCallableM5.)
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
	ctx4, cancel4 := context.WithDeadlineCause(context.Background(), time.Now().Add(time.Second), context.Canceled)
	cancel4()
	var ctx5 context.Context
	var cancel5 context.CancelFunc
	ctx5, cancel5 = context.WithTimeoutCause(context.Background(), time.Second, context.Canceled)
	_, _ = ctx5, cancel5
	tm, terr := time.Parse(time.RFC3339, "2021-03-14T15:09:26Z")
	_, _ = tm, terr
	_, _, _, _ = ctx, ctxcc, ctx3, ctx4
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
	for _, want := range []string{"__withCancel", "__withCancelCause", "__withTimeout", "__withDeadline",
		"__withDeadlineCause", "__withTimeoutCause", "__parseDuration", "__parseTime"} {
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
	// Six cancelFunc-family methodrefs (WithCancel, WithCancelCause,
	// WithTimeout, WithDeadline, WithDeadlineCause, WithTimeoutCause).
	if methodrefs < 6 {
		t.Errorf("want >=6 methodref nodes (cancelFunc/cancelCauseFunc), got %d", methodrefs)
	}
	// Pair field selectors: d+err (ParseDuration) + t+err (Parse) = 4.
	// (Deadline's d+ok pair is not desugared — not callable in M5.)
	if selectors < 4 {
		t.Errorf("want >=4 pair-field selectors, got %d", selectors)
	}
	// The 8 multi-result declarations are never called directly: every
	// 2-target call site desugared to a helper.
	for _, banned := range []string{"WithCancel", "WithCancelCause", "WithDeadline", "WithTimeout",
		"WithDeadlineCause", "WithTimeoutCause", "ParseDuration", "Parse"} {
		if calledIdents[banned] {
			t.Errorf("direct call to %q survived desugaring", banned)
		}
	}
}
