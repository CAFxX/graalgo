package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
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
// (UPDATE_GOLDEN=1) and update docs/arch/frontend-v1.md in the same change.
//
// A golden whose uncompressed export exceeds the GitHub API blob size limit
// (10MB) is stored gzip-compressed as <base>.json.gz instead of <base>.json
// (PKG-059: mr_return_named's encoding/json import pulls ~45MB of stdlib
// type info into the export). The reader decompresses transparently, and
// UPDATE_GOLDEN=1 re-writes the already-registered form, so the raw blob is
// never reintroduced.
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
			golden, compressed := goldenFile(base)
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				data := []byte(out + "\n")
				if compressed {
					data = gzipBytes(t, data)
				}
				if err := os.WriteFile(golden, data, 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("updated %s", golden)
				return
			}
			want, err := readGolden(t, golden, compressed)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 to create)", golden, err)
			}
			if got := out + "\n"; got != string(want) {
				t.Fatalf("golden mismatch for %s:\nfirst diff at line %d", f, firstDiffLine(got, string(want)))
			}
		})
	}
}

// goldenFile resolves the golden for base: testdata/<base>.json, falling
// back to testdata/<base>.json.gz when a compressed golden is registered.
// The second return is true iff the .gz form exists; UPDATE_GOLDEN=1
// re-writes whichever form is registered (new programs default to .json).
func goldenFile(base string) (path string, compressed bool) {
	plain := filepath.Join("testdata", base+".json")
	if _, err := os.Stat(plain); err == nil {
		return plain, false
	}
	gz := filepath.Join("testdata", base+".json.gz")
	if _, err := os.Stat(gz); err == nil {
		return gz, true
	}
	return plain, false
}

// readGolden reads the golden at path, decompressing when it is the .gz form.
func readGolden(t *testing.T, path string, compressed bool) ([]byte, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !compressed {
		return raw, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// gzipBytes compresses data for UPDATE_GOLDEN=1 re-writes of a .json.gz
// golden. Deterministic for identical input (no filename or mtime in the
// header), so re-running the update is a byte-identical no-op.
func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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
		// P3-M9 (WS D, graalgo-ordinverse-generic): function-field calls
		// (t.f() where f is func-typed) are now supported; testdata/bad_call.go
		// retired (was WS D scope error, now exports successfully).
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

// TestPackageLevelMRArgHoist (BUG-MODPROBE-010): a multi-result call nested
// in argument position at package scope (var x = f(g())) must export: the
// inner call hoists to a synthesized package-level temp global emitted
// immediately before the consuming variable's initializer, whose init
// selects the temp's pair fields. General mechanism — any package, any
// user-defined multi-result function — never module-specific.
func TestPackageLevelMRArgHoist(t *testing.T) {
	const src = `package main

func mkPair(s string) (int, error) {
	if s == "" {
		return 0, errNo
	}
	return len(s), nil
}

func mustInt(v int, err error) int {
	if err != nil {
		panic(err)
	}
	return v
}

var errNo = errorf("no")

func errorf(s string) error {
	return errWrap{s}
}

type errWrap struct{ s string }

func (e errWrap) Error() string { return e.s }

var hoisted = mustInt(mkPair("abcd"))

func main() {
	println("hoisted =", hoisted)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "pkgvar.go")
	if err := os.WriteFile(p, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := ExportFile(p)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	var doc struct {
		Objects []struct {
			Name string `json:"name"`
		} `json:"objects"`
		Globals []struct {
			Obj  int `json:"obj"`
			Init *struct {
				Kind   string         `json:"kind"`
				Fields map[string]any `json:"fields"`
			} `json:"init"`
		} `json:"globals"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	names := make([]string, 0, len(doc.Globals))
	for _, g := range doc.Globals {
		if g.Obj < 0 {
			continue
		}
		names = append(names, doc.Objects[g.Obj].Name)
	}
	tempIdx, consIdx := -1, -1
	for i, n := range names {
		if strings.HasPrefix(n, "__p5mr_") {
			tempIdx = i
		}
		if n == "hoisted" {
			consIdx = i
		}
	}
	if tempIdx < 0 {
		t.Fatalf("no hoisted temp global emitted; globals=%v", names)
	}
	if consIdx < 0 {
		t.Fatalf("no 'hoisted' global emitted; globals=%v", names)
	}
	if tempIdx > consIdx {
		t.Fatalf("temp global emitted after its consumer (tempIdx=%d, consIdx=%d)", tempIdx, consIdx)
	}
	// The consumer's init must reference the temp's pair fields.
	init := doc.Globals[consIdx].Init
	if init == nil || init.Kind != "call" {
		t.Fatalf("consumer init is not a call node: %+v", init)
	}
	args, _ := init.Fields["args"].([]any)
	if len(args) != 2 {
		t.Fatalf("consumer init has %d args, want 2 (pair fields)", len(args))
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
	var nodes []nodeKind
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if k, ok := v["kind"].(string); ok {
				f, _ := v["fields"].(map[string]any)
				nodes = append(nodes, nodeKind{k, f})
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
	// P1-1 shape pin: every gostmt must be a zero-arg funclit call (the
	// Java GoStatementNode rejects args>0 at build time). `go close(a)`
	// desugars to hoisted-temp + go func() { close(tmp) }().
	assertGostmtZeroArg(t, nodes)
}

// nodeKind is a walked (kind, fields) pair from exported wire JSON.
type nodeKind struct {
	kind   string
	fields map[string]any
}

// assertGostmtZeroArg pins the GoStatementNode contract: every "gostmt"
// node's call has zero args and its fun is a funclit with zero params.
// (Reviewer1 P1-1 test gap: TestChannelOpsM5 only asserted a gostmt
// node exists.)
func assertGostmtZeroArg(t *testing.T, nodes []nodeKind) {
	t.Helper()
	var count int
	for _, n := range nodes {
		if n.kind != "gostmt" {
			continue
		}
		count++
		call, ok := n.fields["call"].(map[string]any)
		if !ok || call["kind"] != "call" {
			t.Errorf("gostmt call field is not a call node: %v", n.fields["call"])
			continue
		}
		cf, _ := call["fields"].(map[string]any)
		if args, ok := cf["args"].([]any); !ok || len(args) != 0 {
			t.Errorf("gostmt call has %d args, want 0 (GoStatementNode rejects args>0)", len(args))
		}
		fun, ok := cf["fun"].(map[string]any)
		if !ok || fun["kind"] != "funclit" {
			t.Errorf("gostmt call fun is not a funclit: %v", cf["fun"])
			continue
		}
		ff, _ := fun["fields"].(map[string]any)
		if params, ok := ff["params"].([]any); !ok || len(params) != 0 {
			t.Errorf("gostmt funclit has %d params, want 0", len(params))
		}
	}
	if count == 0 {
		t.Errorf("no gostmt nodes found for shape assertion")
	}
}

// TestGoWithArgsInLoop verifies: GG-0017 — P3-M5 (reviewer1 P2-4) the
// `go f(args)` desugar inside a loop. The Appendix N desugar emits one
// __go_arg_* temp per go *statement*; in a loop the temp vardecls must
// sit inside the loop body (so the factory's loopcap pre-scan gives
// them per-iteration box semantics when the funclit captures them) and
// the spawn must keep the zero-arg funclit shape. Per-iteration VALUES
// are asserted end to end (see the P2-4 commit message for the
// P3SyncMain run: sum 0+10+20=30, not 3×30).
func TestGoWithArgsInLoop(t *testing.T) {
	src := `package main

func f(a int, ch chan int) { ch <- a * 10 }

func main() {
	ch := make(chan int, 3)
	for i := 0; i < 3; i++ {
		go f(i, ch)
	}
	s := 0
	for i := 0; i < 3; i++ {
		s += <-ch
	}
	println(s)
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "goargsloop.go")
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
	// Find the first for-loop body block.
	var loopBody map[string]any
	var findLoop func(v any) bool
	findLoop = func(v any) bool {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "for" {
				if f, ok := v["fields"].(map[string]any); ok {
					if b, ok := f["body"].(map[string]any); ok {
						loopBody = b
						return true
					}
				}
			}
			for _, c := range v {
				if findLoop(c) {
					return true
				}
			}
		case []any:
			for _, c := range v {
				if findLoop(c) {
					return true
				}
			}
		}
		return false
	}
	if !findLoop(raw) {
		t.Fatal("no for loop found in export")
	}
	// The loop body must contain the desugar block: __go_arg_* vardecls
	// dominating the gostmt, all inside the loop (per-iteration boxes).
	bodyFields, _ := loopBody["fields"].(map[string]any)
	stmts, _ := bodyFields["stmts"].([]any)
	var sawArgHoist, sawGostmt bool
	var gostmtNode map[string]any
	for _, st := range stmts {
		sm, ok := st.(map[string]any)
		if !ok {
			continue
		}
		if sm["kind"] == "block" {
			bf, _ := sm["fields"].(map[string]any)
			inner, _ := bf["stmts"].([]any)
			for _, in := range inner {
				im, ok := in.(map[string]any)
				if !ok {
					continue
				}
				if im["kind"] == "vardecl" {
					iff, _ := im["fields"].(map[string]any)
					if names, ok := iff["names"].([]any); ok {
						for _, nm := range names {
							if s, ok := nm.(string); ok && strings.HasPrefix(s, "__go_arg_") {
								sawArgHoist = true
							}
						}
					}
				}
				if im["kind"] == "gostmt" {
					sawGostmt = true
					gostmtNode = im
				}
			}
		}
	}
	if !sawArgHoist {
		t.Errorf("loop body missing __go_arg_* hoist vardecls (per-iteration capture precondition)")
	}
	if !sawGostmt {
		t.Errorf("loop body missing gostmt")
	}
	// Zero-arg funclit shape on the loop's gostmt.
	if gostmtNode != nil {
		gf, _ := gostmtNode["fields"].(map[string]any)
		call, _ := gf["call"].(map[string]any)
		cf, _ := call["fields"].(map[string]any)
		if args, ok := cf["args"].([]any); !ok || len(args) != 0 {
			t.Errorf("loop gostmt call has args, want 0")
		}
		fun, _ := cf["fun"].(map[string]any)
		ff, _ := fun["fields"].(map[string]any)
		if params, ok := ff["params"].([]any); !ok || len(params) != 0 {
			t.Errorf("loop gostmt funclit has params, want 0")
		}
		// The funclit's inner call must reference the hoisted temps
		// (i.e. capture them — the loopcap precondition for per-iteration
		// values).
		var refsTemp bool
		var findIdent func(v any)
		findIdent = func(v any) {
			switch v := v.(type) {
			case map[string]any:
				if v["kind"] == "ident" {
					if f, ok := v["fields"].(map[string]any); ok {
						if s, ok := f["name"].(string); ok && strings.HasPrefix(s, "__go_arg_") {
							refsTemp = true
						}
					}
				}
				for _, c := range v {
					findIdent(c)
				}
			case []any:
				for _, c := range v {
					findIdent(c)
				}
			}
		}
		findIdent(fun)
		if !refsTemp {
			t.Errorf("loop gostmt funclit does not reference the __go_arg_* temps")
		}
	}
}

// TestGoBuiltinCalls verifies FE-11: `go <builtin>(args)` — go
// println("hello"), go print(...), go panic("x") — is legal Go (accepted
// by the gc toolchain) and must export. Pre-fix, ExportFile failed in
// hoistGoFunValue with `gofrontend: go func "println" not declared`.
//
// The desugar must reuse the shared builtin-call lowering
// (builtinCallNode — the same path exportCall uses for direct builtin
// calls): the builtin marker rides on the inner call node (that is what
// the factory's buildBuiltinCall dispatches on), the args are hoisted to
// __go_arg_* temps evaluated at go time in the calling goroutine, and no
// __go_fun_ temp is hoisted — builtins have no function value (go/types
// types them Invalid), so a fun temp would mint a bogus Invalid-typed
// var and lose the marker.
//
// `go panic("x")` is asserted at export level only: running it would
// (correctly, matching gc) crash the process with an unrecovered panic.
func TestGoBuiltinCalls(t *testing.T) {
	src := `package main

func main() {
	go println("hello")
	go print("a", 1, true)
	go panic("x")
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "gobuiltin.go")
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
	desugars := findGoDesugars(t, raw)
	if len(desugars) != 3 {
		t.Fatalf("found %d go-desugar blocks, want 3 (println, print, panic)", len(desugars))
	}
	want := []struct {
		builtin string
		nargs   int
	}{
		{"println", 1},
		{"print", 3},
		{"panic", 1},
	}
	for i, w := range want {
		d := desugars[i]
		if d.hoists != w.nargs {
			t.Errorf("go %s: %d __go_arg_ hoists, want %d", w.builtin, d.hoists, w.nargs)
		}
		inner := d.inner
		if inner["kind"] != "call" {
			t.Errorf("go %s: inner node kind %v, want call", w.builtin, inner["kind"])
			continue
		}
		f, _ := inner["fields"].(map[string]any)
		if f["builtin"] != w.builtin {
			t.Errorf("go %s: inner call builtin marker %v, want %q", w.builtin, f["builtin"], w.builtin)
		}
		fun, _ := f["fun"].(map[string]any)
		ff, _ := fun["fields"].(map[string]any)
		if fun["kind"] != "ident" || ff["builtin"] != w.builtin {
			t.Errorf("go %s: inner call fun is not the builtin ident form: %v", w.builtin, f["fun"])
		}
		args, _ := f["args"].([]any)
		if len(args) != w.nargs {
			t.Errorf("go %s: inner call has %d args, want %d", w.builtin, len(args), w.nargs)
			continue
		}
		for j, a := range args {
			am, _ := a.(map[string]any)
			af, _ := am["fields"].(map[string]any)
			name, _ := af["name"].(string)
			if am["kind"] != "ident" || !strings.HasPrefix(name, "__go_arg_") {
				t.Errorf("go %s: inner call arg %d is not a __go_arg_ temp: %v", w.builtin, j, a)
			}
		}
	}
}

// TestGoBuiltinClearCopyDelete covers the remaining statement-legal
// builtins in `go <builtin>(args)` position (FE-11): go clear(m) must
// lower to the dedicated "clear" wire node — the frozen v1 builtin path
// rejects builtin-marked clear calls (WS G), exactly as exportCall does
// for direct clear(m) — while go copy and go delete reuse the
// builtin-marked call lowering.
func TestGoBuiltinClearCopyDelete(t *testing.T) {
	src := `package main

func main() {
	m := map[string]int{"k": 1}
	s := []int{1, 2, 3}
	d := []int{0, 0, 0}
	clear(m)
	go clear(m)
	go copy(d, s)
	go delete(m, "k")
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "gobuiltinclear.go")
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
	desugars := findGoDesugars(t, raw)
	if len(desugars) != 3 {
		t.Fatalf("found %d go-desugar blocks, want 3 (clear, copy, delete)", len(desugars))
	}
	// go clear(m): dedicated "clear" wire node, same kind as the direct
	// clear(m) — not a builtin-marked call.
	clearInner := desugars[0].inner
	if clearInner["kind"] != "clear" {
		t.Fatalf("go clear: inner node kind %v, want clear", clearInner["kind"])
	}
	cf, _ := clearInner["fields"].(map[string]any)
	x, _ := cf["x"].(map[string]any)
	xf, _ := x["fields"].(map[string]any)
	xname, _ := xf["name"].(string)
	if x["kind"] != "ident" || !strings.HasPrefix(xname, "__go_arg_") {
		t.Errorf("go clear: x is not the hoisted __go_arg_ temp: %v", cf["x"])
	}
	if _, ok := cf["builtin"]; ok {
		t.Errorf("go clear: clear node must not carry a builtin marker: %v", cf)
	}
	// The direct clear(m) in the same program must lower identically
	// (same wire kind) — the go desugar reuses exportCall's lowering.
	var directClear int
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "clear" {
				directClear++
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
	if directClear != 2 {
		t.Errorf("found %d clear nodes, want 2 (direct clear(m) + go clear(m) via the same lowering)", directClear)
	}
	// go copy / go delete: builtin-marked inner calls.
	for i, name := range []string{"copy", "delete"} {
		inner := desugars[i+1].inner
		if inner["kind"] != "call" {
			t.Errorf("go %s: inner node kind %v, want call", name, inner["kind"])
			continue
		}
		f, _ := inner["fields"].(map[string]any)
		if f["builtin"] != name {
			t.Errorf("go %s: inner call builtin marker %v, want %q", name, f["builtin"], name)
		}
	}
}

// goDesugar is one `go f(args)` desugar block: the __go_arg_* hoist
// count and the funclit's inner call node.
type goDesugar struct {
	hoists int
	inner  map[string]any
}

// findGoDesugars collects every go-desugar block in the export, in
// source order: a block whose statements are __go_arg_* vardecls
// followed by a gostmt whose call is the zero-arg funclit spawn. A
// __go_fun_ temp is reported as an error — builtins have no function
// value to hoist.
func findGoDesugars(t *testing.T, raw map[string]any) []goDesugar {
	t.Helper()
	var out []goDesugar
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["kind"] == "block" {
				if d, ok := asGoDesugar(t, v); ok {
					out = append(out, d)
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
	return out
}

func asGoDesugar(t *testing.T, blk map[string]any) (goDesugar, bool) {
	t.Helper()
	f, _ := blk["fields"].(map[string]any)
	stmts, _ := f["stmts"].([]any)
	if len(stmts) == 0 {
		return goDesugar{}, false
	}
	last, _ := stmts[len(stmts)-1].(map[string]any)
	if last["kind"] != "gostmt" {
		return goDesugar{}, false
	}
	d := goDesugar{}
	for _, st := range stmts[:len(stmts)-1] {
		sm, ok := st.(map[string]any)
		if !ok || sm["kind"] != "vardecl" {
			return goDesugar{}, false
		}
		sf, _ := sm["fields"].(map[string]any)
		names, _ := sf["names"].([]any)
		for _, nm := range names {
			s, _ := nm.(string)
			switch {
			case strings.HasPrefix(s, "__go_fun_"):
				t.Errorf("go desugar hoisted a __go_fun_ temp %q: builtins have no function value to hoist", s)
			case strings.HasPrefix(s, "__go_arg_"):
				d.hoists++
			default:
				return goDesugar{}, false
			}
		}
	}
	// Drill gostmt -> call(fun=funclit) -> body block -> exprstmt -> inner.
	gf, _ := last["fields"].(map[string]any)
	call, _ := gf["call"].(map[string]any)
	cf, _ := call["fields"].(map[string]any)
	fun, _ := cf["fun"].(map[string]any)
	if fun["kind"] != "funclit" {
		return goDesugar{}, false
	}
	ff, _ := fun["fields"].(map[string]any)
	body, _ := ff["body"].(map[string]any)
	bf, _ := body["fields"].(map[string]any)
	bstmts, _ := bf["stmts"].([]any)
	if len(bstmts) != 1 {
		return goDesugar{}, false
	}
	es, _ := bstmts[0].(map[string]any)
	if es["kind"] != "exprstmt" {
		return goDesugar{}, false
	}
	ef, _ := es["fields"].(map[string]any)
	inner, _ := ef["x"].(map[string]any)
	if inner == nil {
		return goDesugar{}, false
	}
	d.inner = inner
	return d, true
}
