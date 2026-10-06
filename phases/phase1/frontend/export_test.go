package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestGolden: every non-bad_ testdata program exports byte-identical JSON to
// its checked-in golden file. The goldens pin the frontend/v1 wire format;
// any intentional format change must regenerate them explicitly
// (UPDATE_GOLDEN=1) and update docs/frontend-v1.md in the same change.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no testdata programs found")
	}
	for _, f := range files {
		base := strings.TrimSuffix(filepath.Base(f), ".go")
		if strings.HasPrefix(base, "bad_") {
			continue
		}
		t.Run(base, func(t *testing.T) {
			out, err := ExportFile(f)
			if err != nil {
				t.Fatalf("ExportFile(%s): %v", f, err)
			}
			golden := filepath.Join("testdata", base+".json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, []byte(out+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", golden)
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to create)", golden, err)
			}
			if got := out + "\n"; got != string(want) {
				t.Fatalf("golden mismatch for %s:\nfirst diff at line %d", f, firstDiffLine(got, string(want)))
			}
		})
	}
}

func firstDiffLine(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	if len(al) != len(bl) {
		return min(len(al), len(bl)) + 1
	}
	return 0
}

// TestDeterministic: two exports of the same program must be byte-identical
// (no map-iteration nondeterminism may leak into the wire format).
func TestDeterministic(t *testing.T) {
	a, err := ExportFile("testdata/straight02.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ExportFile("testdata/straight02.go")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("ExportFile is not deterministic")
	}
}

// TestScopeErrors: out-of-scope programs fail fast with an explicit error
// naming the workstream that owns the extension.
func TestScopeErrors(t *testing.T) {
	cases := map[string]string{
		"testdata/bad_import.go": "WS G",
		"testdata/bad_call.go":   "WS D",
		// P3-M5 (WS E, ADR-0005 App C): select/send/close are now lowered;
		// testdata/bad_select.go (select-with-default scope error) retired.
	}
	for f, want := range cases {
		_, err := ExportFile(f)
		if err == nil {
			t.Fatalf("%s: expected scope error, got success", f)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error %q does not attribute %s", f, err, want)
		}
	}
}

// TestTypeError: ill-typed programs are rejected by the vendored go/types
// before any JSON is emitted.
func TestTypeError(t *testing.T) {
	_, err := ExportFile("testdata/bad_type.go")
	if err == nil {
		t.Fatal("expected type error, got success")
	}
	if !strings.Contains(err.Error(), "type error") {
		t.Fatalf("expected a type error, got: %v", err)
	}
}

// TestNoMain: a file without func main is rejected explicitly.
func TestNoMain(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nomain.go")
	if err := os.WriteFile(p, []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := ExportFile(p)
	if err == nil {
		t.Fatal("expected no-func-main error, got success")
	}
	if !strings.Contains(err.Error(), "no func main") {
		t.Fatalf("expected no-func-main error, got: %v", err)
	}
}

// TestDeepNesting (M10 P1-4): 40-deep value-nested structs must export fast.
// go/types' gcSizes has no memoization (Sizeof is Theta(2^k)); the exporter
// depth-caps offset computation at maxStructDepth and emits -1 offsets
// instead of hanging. Pre-fix, 30-deep hung >60s.
func TestDeepNesting(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := 40; i >= 1; i-- {
		if i == 40 {
			sb.WriteString("type B40 struct{ v int }\n")
		} else {
			sb.WriteString("type B" + strconv.Itoa(i) + " struct{ v B" + strconv.Itoa(i+1) + " }\n")
		}
	}
	sb.WriteString("\nfunc main() { var x B1; println(x.v) }\n")
	p := filepath.Join(dir, "deep.go")
	if err := os.WriteFile(p, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		out, err := ExportFile(p)
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		done <- out
	}()
	select {
	case out := <-done:
		if strings.HasPrefix(out, "error: ") {
			t.Fatalf("ExportFile: %s", out)
		}
		if !strings.Contains(out, "\"offset\": -1") {
			t.Fatal("expected -1 offsets for over-cap structs")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("ExportFile hung on 40-deep nesting (P1-4 regression)")
	}
}

// TestChannelOpsM5 verifies: GG-0017 — P3-M5 (WS E, ADR-0005 Appendix C)
// channel-operations lowering: make(chan)/send/recv/close/select lower to
// the additive wire nodes (makechan/send/recv/chanclose/select), and
// `for v := range ch` desugars to a for+select loop.
func TestChannelOpsM5(t *testing.T) {
	src := `package main

func main() {
	a := make(chan int)
	b := make(chan int, 3)
	a <- 1
	x := <-b
	y, ok := <-b
	_ = x
	_ = y
	_ = ok
	close(a)
	select {
	case v := <-a:
		_ = v
	case b <- 2:
	default:
	}
	for v := range b {
		_ = v
	}
	defer close(b)
	go close(a)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "chanops.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	// Walk the raw JSON collecting (kind, fields) pairs.
	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	type nk struct {
		kind   string
		fields map[string]any
	}
	var nodes []nk
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if k, ok := v["kind"].(string); ok {
				f, _ := v["fields"].(map[string]any)
				nodes = append(nodes, nk{k, f})
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
	kindCount := map[string]int{}
	for _, n := range nodes {
		kindCount[n.kind]++
	}
	for _, want := range []string{"makechan", "send", "recv", "chanclose", "select", "selectcase", "defer", "gostmt", "labeled"} {
		if kindCount[want] == 0 {
			t.Errorf("expected at least one %q node, got kinds %v", want, kindCount)
		}
	}
	// No "unary" node may carry op "<-" anymore (frozen UnaryOpNode never
	// implemented it; recv replaced it).
	for _, n := range nodes {
		if n.kind == "unary" && n.fields["op"] == "<-" {
			t.Errorf("found legacy unary <- node; want recv")
		}
	}
	// makechan: unbuffered has no "size"; buffered does; both carry elem+type.
	var sawBare, sawSized bool
	for _, n := range nodes {
		if n.kind != "makechan" {
			continue
		}
		if _, ok := n.fields["elem"]; !ok {
			t.Errorf("makechan missing elem")
		}
		if _, ok := n.fields["type"]; !ok {
			t.Errorf("makechan missing type")
		}
		if _, ok := n.fields["size"]; ok {
			sawSized = true
		} else {
			sawBare = true
		}
	}
	if !sawBare || !sawSized {
		t.Errorf("want one bare and one sized makechan (bare=%v sized=%v)", sawBare, sawSized)
	}
	// recv: plain `<-ch` emits recv nodes; the v, ok := <-ch shape
	// desugars to a single-case select (ADR-0005 App C, reconciled
	// 2026-10-06) — no recv node may carry commaOk.
	var plain int
	for _, n := range nodes {
		if n.kind != "recv" {
			continue
		}
		if _, hasCommaOk := n.fields["commaOk"]; hasCommaOk {
			t.Errorf("recv node carries commaOk; want select desugar")
		} else {
			plain++
		}
	}
	if plain == 0 {
		t.Errorf("want plain recv nodes (plain=%d)", plain)
	}
	// The comma-ok desugar emits a single-case, no-default select with
	// two targets.
	var sawCommaOkSelect bool
	for _, n := range nodes {
		if n.kind != "select" {
			continue
		}
		cases, ok := n.fields["cases"].([]any)
		if !ok || len(cases) != 1 {
			continue
		}
		c, ok := cases[0].(map[string]any)
		if !ok {
			continue
		}
		cf, ok := c["fields"].(map[string]any)
		if !ok {
			continue
		}
		if cf["kind"] == "recv" {
			if tgts, ok := cf["targets"].([]any); ok && len(tgts) == 2 {
				sawCommaOkSelect = true
			}
		}
	}
	if !sawCommaOkSelect {
		t.Errorf("want single-case select desugar for v, ok := <-ch")
	}
	// chanclose nodes are statements: none may appear under an "x" field
	// of an exprstmt (they must be direct statement children).
	for _, n := range nodes {
		if n.kind == "exprstmt" {
			if x, ok := n.fields["x"].(map[string]any); ok && x["kind"] == "chanclose" {
				t.Errorf("chanclose wrapped in exprstmt; want bare statement")
			}
		}
	}
	// select cases: one recv with targets, one send, one default.
	var recvCase, sendCase, defaultCase bool
	for _, n := range nodes {
		if n.kind != "selectcase" {
			continue
		}
		switch n.fields["kind"] {
		case "recv":
			if _, ok := n.fields["targets"]; ok {
				recvCase = true
			}
		case "send":
			if _, ok := n.fields["chan"]; ok {
				if _, ok := n.fields["value"]; ok {
					sendCase = true
				}
			}
		case "default":
			defaultCase = true
		}
	}
	if !recvCase || !sendCase || !defaultCase {
		t.Errorf("want recv/send/default selectcases (recv=%v send=%v default=%v)", recvCase, sendCase, defaultCase)
	}
}
