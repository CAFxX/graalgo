package mutate

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Each class test asserts: (a) the output differs from the input (the test
// fails if the mutator silently no-ops), (b) the output still parses
// (still-compiling requirement), (c) the expected textual change is present.

func apply(t *testing.T, class, src string) *Mutation {
	t.Helper()
	m, err := Apply(class, []byte(src), "test.go")
	if err != nil {
		t.Fatalf("Apply(%s): %v", class, err)
	}
	if m.Description == "" {
		t.Fatalf("Apply(%s): empty description", class)
	}
	if string(m.Src) == src {
		t.Fatalf("Apply(%s): source unchanged", class)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "mut.go", m.Src, 0); err != nil {
		t.Fatalf("Apply(%s): mutated source does not parse: %v", class, err)
	}
	return m
}

func TestOpSwap(t *testing.T) {
	m := apply(t, "OpSwap", "package main\nfunc f() int { a := 1\n b := 2\n return a + b }\n")
	if !strings.Contains(string(m.Src), "a - b") {
		t.Fatalf("expected 'a - b', got:\n%s", m.Src)
	}
}

func TestOpSwap_MulDiv(t *testing.T) {
	m := apply(t, "OpSwap", "package main\nfunc f() int { return 6 * 7 }\n")
	if !strings.Contains(string(m.Src), "6 / 7") {
		t.Fatalf("expected '6 / 7', got:\n%s", m.Src)
	}
}

func TestCmpSwap(t *testing.T) {
	m := apply(t, "CmpSwap", "package main\nfunc f(x int) bool { if x < 10 { return true }\n return false }\n")
	if !strings.Contains(string(m.Src), "x <= 10") {
		t.Fatalf("expected 'x <= 10', got:\n%s", m.Src)
	}
}

func TestCmpSwap_Equality(t *testing.T) {
	m := apply(t, "CmpSwap", "package main\nfunc f(x int) bool { return x == 3 }\n")
	if !strings.Contains(string(m.Src), "x != 3") {
		t.Fatalf("expected 'x != 3', got:\n%s", m.Src)
	}
}

func TestOffByOne(t *testing.T) {
	m := apply(t, "OffByOne", "package main\nfunc f() { for i := 0; i < 5; i++ { _ = i } }\n")
	if !strings.Contains(string(m.Src), "i < 6") {
		t.Fatalf("expected 'i < 6', got:\n%s", m.Src)
	}
}

func TestDropDefer(t *testing.T) {
	m := apply(t, "DropDefer", "package main\nimport \"fmt\"\nfunc f() { defer fmt.Println(1)\n fmt.Println(2) }\n")
	if strings.Contains(string(m.Src), "defer") {
		t.Fatalf("defer not removed:\n%s", m.Src)
	}
	if !strings.Contains(string(m.Src), "fmt.Println(2)") {
		t.Fatalf("body damaged:\n%s", m.Src)
	}
}

func TestCondNegate(t *testing.T) {
	m := apply(t, "CondNegate", "package main\nfunc f(x int) { if x > 0 { println(1) } }\n")
	if !strings.Contains(string(m.Src), "!(x > 0)") {
		t.Fatalf("expected '!(x > 0)', got:\n%s", m.Src)
	}
}

func TestConstChange_String(t *testing.T) {
	m := apply(t, "ConstChange", "package main\nconst greeting = \"hello\"\nfunc f() string { return greeting }\n")
	if !strings.Contains(string(m.Src), `"hello!"`) {
		t.Fatalf("expected \"hello!\", got:\n%s", m.Src)
	}
}

func TestConstChange_Int(t *testing.T) {
	m := apply(t, "ConstChange", "package main\nconst n = 5\nfunc f() int { return n }\n")
	if !strings.Contains(string(m.Src), "105") {
		t.Fatalf("expected 105, got:\n%s", m.Src)
	}
}

func TestReturnSwap(t *testing.T) {
	m := apply(t, "ReturnSwap", "package main\nfunc f() (int, int) { return 1, 2 }\n")
	if !strings.Contains(string(m.Src), "return 2, 1") {
		t.Fatalf("expected 'return 2, 1', got:\n%s", m.Src)
	}
}

func TestChanOp(t *testing.T) {
	m := apply(t, "ChanOp", "package main\nfunc f(ch chan int) { for v := range ch { ch <- v } }\n")
	if strings.Contains(string(m.Src), "ch <- v") {
		t.Fatalf("send not replaced:\n%s", m.Src)
	}
	if !strings.Contains(string(m.Src), "<-ch") {
		t.Fatalf("expected '<-ch', got:\n%s", m.Src)
	}
	// the sent value must stay used, otherwise the mutant does not compile
	if !strings.Contains(string(m.Src), "_ = v") {
		t.Fatalf("expected '_ = v' to keep v used, got:\n%s", m.Src)
	}
}

func TestSliceBound(t *testing.T) {
	m := apply(t, "SliceBound", "package main\nfunc f(s []int) []int { return s[1:4] }\n")
	if !strings.Contains(string(m.Src), "4-1") {
		t.Fatalf("expected high bound '4-1', got:\n%s", m.Src)
	}
}

func TestApply_UnknownClass(t *testing.T) {
	if _, err := Apply("Nope", []byte("package main\n"), "t.go"); err == nil {
		t.Fatal("expected error for unknown class")
	}
}

func TestApply_NoEligibleSite(t *testing.T) {
	// No binary operator present: OpSwap must report "no eligible site"
	// rather than silently returning the input unchanged.
	src := "package main\nfunc f() int { return 42 }\n"
	if _, err := Apply("OpSwap", []byte(src), "t.go"); err == nil {
		t.Fatal("expected no-eligible-site error")
	}
}

func TestApply_BadSource(t *testing.T) {
	if _, err := Apply("OpSwap", []byte("not go at all"), "t.go"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestClasses_Documented(t *testing.T) {
	if len(Classes) == 0 {
		t.Fatal("no mutation classes")
	}
	seen := map[string]bool{}
	for _, c := range Classes {
		if seen[c] {
			t.Fatalf("duplicate class %q", c)
		}
		seen[c] = true
	}
}
