package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Wire-shape tests for the `for v := range ch` desugar (ADR-0005 App C,
// exportRangeChan in chanops_m5.go): FE-4 (user label must land on the
// synthesized for loop) and FE-18 (unlabeled body breaks rewritten to the
// loop label). These assert on the exported wire JSON the way
// TestChannelOpsM5 does; behavioral coverage lives in the differential
// corpus (test/differential/corpus/49_range_chan_break_continue).

// exportSrcNodes exports a Go source string and returns every (kind,
// fields) node in the wire JSON.
func exportSrcNodes(t *testing.T, src string) []nodeKind {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prog.go")
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
	return nodes
}

// branchLabel returns the label of a "branch" node as a string, or "" when
// the branch is unlabeled (wire encodes it as JSON null).
func branchLabel(n nodeKind) string {
	if n.kind != "branch" {
		return ""
	}
	s, _ := n.fields["label"].(string)
	return s
}

// rangeChanLoopLabel returns the label of the synthesized for loop for the
// single `for v := range ch` desugar in nodes: the "labeled" node whose
// stmt is a "for".
func rangeChanLoopLabel(t *testing.T, nodes []nodeKind) string {
	t.Helper()
	var labels []string
	for _, n := range nodes {
		if n.kind != "labeled" {
			continue
		}
		stmt, _ := n.fields["stmt"].(map[string]any)
		if stmt == nil || stmt["kind"] != "for" {
			continue
		}
		if l, ok := n.fields["label"].(string); ok {
			labels = append(labels, l)
		}
	}
	if len(labels) != 1 {
		t.Fatalf("want exactly one labeled-for from range-chan desugar, got labels %q", labels)
	}
	return labels[0]
}

// TestRangeChanLabeledContinue (FE-4): `outer: for v := range ch` must put
// the user label on the synthesized for loop — the backend loop node
// consumes exactly one pending label, and a label on the desugared block
// leaves `continue outer` with no loop to target (uncaught ContinueSignal
// at runtime). Pre-fix this fails: the label lands on the block and the
// for keeps the synthetic __rng_N label.
func TestRangeChanLabeledContinue(t *testing.T) {
	src := `package main

func main() {
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)
	sum := 0
outer:
	for v := range ch {
		if v == 2 {
			continue outer
		}
		if v > 100 {
			break outer
		}
		sum += v
	}
	_ = sum
}
`
	nodes := exportSrcNodes(t, src)

	// The synthesized loop itself carries the user label.
	if got := rangeChanLoopLabel(t, nodes); got != "outer" {
		t.Errorf("range-chan loop label = %q, want %q (label must be on the for, not the block)", got, "outer")
	}
	// No synthetic __rng_ label may survive when the user labeled the loop:
	// the loop has exactly one label slot.
	for _, n := range nodes {
		if n.kind == "labeled" {
			if l, ok := n.fields["label"].(string); ok && strings.HasPrefix(l, "__rng_") {
				t.Errorf("found synthetic labeled node %q alongside user label; want the user label to replace it", l)
			}
		}
	}
	// continue outer / break outer target the loop by name.
	var sawContinueOuter, sawBreakOuter int
	for _, n := range nodes {
		if n.kind != "branch" {
			continue
		}
		op, _ := n.fields["op"].(string)
		switch {
		case op == "continue" && branchLabel(n) == "outer":
			sawContinueOuter++
		case op == "break" && branchLabel(n) == "outer":
			sawBreakOuter++
		}
	}
	if sawContinueOuter != 1 {
		t.Errorf("want 1 branch{continue outer}, got %d", sawContinueOuter)
	}
	// break outer: one from the user body, one from the desugared
	// `if !ok` terminator (which must use the loop's label too).
	if sawBreakOuter != 2 {
		t.Errorf("want 2 branch{break outer} (body + desugared if-!ok), got %d", sawBreakOuter)
	}
}

// TestRangeChanUnlabeledBreakRewrite (FE-18): an unlabeled `break` in the
// range-chan body binds to the range loop at source level, but the copied
// body sits inside a synthesized select — so it is rewritten to
// `break <loopLabel>`. Today it only works because the backend SelectNode
// does not catch BreakSignal; the rewrite makes the binding explicit and
// is behavior-preserving either way. Pre-fix this fails: the body break
// stays unlabeled.
func TestRangeChanUnlabeledBreakRewrite(t *testing.T) {
	src := `package main

func main() {
	ch := make(chan int, 3)
	ch <- 1
	close(ch)
	n := 0
	for v := range ch {
		n++
		if v > 10 {
			break // binds to the range loop: must be rewritten
		}
		select {
		case x := <-ch:
			_ = x
			break // binds to the inner select: must stay unlabeled
		default:
		}
		for i := 0; i < 1; i++ {
			break // binds to the inner for: must stay unlabeled
		}
		_ = v
	}
	_ = n
}
`
	nodes := exportSrcNodes(t, src)
	loopLabel := rangeChanLoopLabel(t, nodes)
	if !strings.HasPrefix(loopLabel, "__rng_") {
		t.Fatalf("unlabeled range-chan loop label = %q, want synthetic __rng_N", loopLabel)
	}

	var labeledBreaks, unlabeledBreaks int
	for _, n := range nodes {
		if n.kind != "branch" {
			continue
		}
		if op, _ := n.fields["op"].(string); op != "break" {
			continue
		}
		if branchLabel(n) == loopLabel {
			labeledBreaks++
		} else if branchLabel(n) == "" {
			unlabeledBreaks++
		} else {
			t.Errorf("break with unexpected label %q (want %q or unlabeled)", branchLabel(n), loopLabel)
		}
	}
	// Labeled: the desugared `if !ok` terminator + the rewritten body
	// break. Pre-fix the body break is unlabeled, so this is 1, not 2.
	if labeledBreaks != 2 {
		t.Errorf("want 2 branch{break %s} (if-!ok + rewritten body break), got %d", loopLabel, labeledBreaks)
	}
	// Unlabeled: the inner select's break and the inner for's break — the
	// rewrite must stop at those boundaries.
	if unlabeledBreaks != 2 {
		t.Errorf("want 2 unlabeled branch{break} (inner select + inner for), got %d", unlabeledBreaks)
	}
}

// TestRangeChanBreakInNestedIf (FE-18): the rewrite descends into plain
// control flow — a break inside an if nested in the body still binds to
// the range loop.
func TestRangeChanBreakInNestedIf(t *testing.T) {
	src := `package main

func main() {
	ch := make(chan int, 2)
	ch <- 1
	close(ch)
	for v := range ch {
		if v > 0 {
			if v > 10 {
				break
			}
		} else {
			_ = v
		}
	}
}
`
	nodes := exportSrcNodes(t, src)
	loopLabel := rangeChanLoopLabel(t, nodes)
	var labeledBreaks int
	for _, n := range nodes {
		if n.kind != "branch" {
			continue
		}
		if op, _ := n.fields["op"].(string); op != "break" {
			continue
		}
		if branchLabel(n) == loopLabel {
			labeledBreaks++
		}
	}
	// if-!ok terminator + the doubly-nested body break.
	if labeledBreaks != 2 {
		t.Errorf("want 2 branch{break %s}, got %d", loopLabel, labeledBreaks)
	}
}
