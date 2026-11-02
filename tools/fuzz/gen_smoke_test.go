package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Smoke test (review 2026-11-02, P2-20): the generator is the tool's
// backend-free path, so it is gated without a JVM: a seed must always
// yield a parseable program, and generation must be deterministic per
// seed. (Parse only, not typecheck: the differential legs, not the
// generator, judge semantic validity.)
func TestGenerateParsesAndIsDeterministic(t *testing.T) {
	src := generate(42)
	if !strings.HasPrefix(src, "package main") {
		t.Fatalf("generate(42) does not start with 'package main': %q", src[:min(64, len(src))])
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0); err != nil {
		t.Fatalf("generate(42) does not parse: %v\n%s", err, src)
	}
	if again := generate(42); again != src {
		t.Fatalf("generate(42) is not deterministic")
	}
	if empty := generate(7); empty == "" {
		t.Fatalf("generate(7) is empty")
	}
}
