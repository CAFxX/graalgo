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
// desugar position, the 6 multi-result operations still fail fast with
// the named M5 scope error. (Single-target `d := time.ParseDuration(s)`
// is a go/types type error before the exporter runs; only type-valid
// non-2-target positions are tested here: statement and multi-arg
// spread.)
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
		"deadline-statement": `package main
import "context"
func main() {
	ctx := context.Background()
	ctx.Deadline()
}
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
