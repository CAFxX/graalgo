// Guard tests for the review-1106-p1-v2-directives remediation lane
// (independent codebase review, 2026-11-06, V12-P1-7).
//
// The emitter rejects //go:embed and //go:linkname loudly
// (directives.go); these tests pin the rejection's narrowness — it
// must fire only on real directives of those two names, not on other
// //go: directives, not on prose mentioning them, and not on directive
// text inside a block comment. Each guard is demonstrably capable of
// failing: broadening the rejection (e.g. to every //go: directive,
// or to a substring match) breaks it.
package emit

import (
	"testing"
)

// TestOtherGoDirectivesStillEmit pins that the rejection is specific
// to embed/linkname: a program carrying //go:noinline (a real
// directive with no program semantics the emitter drops) and
// //go:generate (a tool directive) still emits.
func TestOtherGoDirectivesStillEmit(t *testing.T) {
	doc := emitTestSrc(t, `package main
//go:generate echo hi
//go:noinline
func f() { println("f") }
func main() { f() }
`)
	if len(doc.Funcs) != 2 {
		t.Fatalf("other //go: directives: expected 2 funcs, got %d", len(doc.Funcs))
	}
}

// TestDirectiveProseStillEmits pins that the rejection keys on the
// directive grammar (ast.ParseDirective), not on a substring match:
// a doc comment whose prose mentions go:embed, and a line that merely
// starts with "//go:" followed by prose, must not be rejected.
func TestDirectiveProseStillEmits(t *testing.T) {
	doc := emitTestSrc(t, `package main
// The go:embed directive embeds files; see the docs.
//go: this line is not a directive, it is prose.
func f() { println("f") }
func main() { f() }
`)
	if len(doc.Funcs) != 2 {
		t.Fatalf("directive prose: expected 2 funcs, got %d", len(doc.Funcs))
	}
}

// TestEmbedInBlockCommentStillEmits pins that a directive inside a
// block comment is not a directive: ast.ParseDirective only
// recognizes //-comments, matching gc, which also ignores it.
func TestEmbedInBlockCommentStillEmits(t *testing.T) {
	doc := emitTestSrc(t, `package main
/* //go:embed hello.txt */
var s string
func main() { println(s) }
`)
	if len(doc.Globals) != 1 {
		t.Fatalf("block-comment embed: expected 1 global, got %d", len(doc.Globals))
	}
}
