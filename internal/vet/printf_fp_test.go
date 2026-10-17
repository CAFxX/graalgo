package vet

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// QN-1: %s/%q must accept fmt.Stringer and fmt.Formatter implementers,
// mirroring go vet. Regression test asserting NO diagnostic for
// Stringer/Formatter args (previously flagged "expects string or []byte").
//
// The Stringer type deliberately has a non-string field: without the
// String() method exemption it would still be flagged, so the test
// proves the exemption works (not struct-field luck).
func TestPrintfStringerFormatterNoFP(t *testing.T) {
	src := `package main

import "fmt"

type myStringer struct{ n int }

func (m myStringer) String() string { return "x" }

type myFormatter struct{}

func (myFormatter) Format(f fmt.State, verb rune) {}

type myError struct{}

func (myError) Error() string { return "boom" }

type byteSlice []byte

func main() {
	var ms myStringer
	var mf myFormatter
	var me myError
	var bs byteSlice
	var err error
	var b4 [4]byte
	_ = fmt.Sprintf("%s", ms)
	_ = fmt.Sprintf("%q", ms)
	_ = fmt.Sprintf("%s", &ms)
	_ = fmt.Sprintf("%s", mf)
	_ = fmt.Sprintf("%q", mf)
	_ = fmt.Sprintf("%s", &mf)
	_ = fmt.Sprintf("%d", mf)
	_ = fmt.Sprintf("%s", me)
	_ = fmt.Sprintf("%s", err)
	_ = fmt.Sprintf("%s", bs)
	_ = fmt.Sprintf("%s", []byte("x"))
	_ = fmt.Sprintf("%s", b4)
	_ = fmt.Sprintf("%s", "lit")
	_ = fmt.Sprintf("%v", ms)
}
`
	pass := testPass(t, src)
	if ds := runPrintf(pass); len(ds) != 0 {
		for _, d := range ds {
			t.Errorf("false positive: %s: %s", d.Pos, d.Message)
		}
	}
}

// QN-1 negative controls: genuinely wrong %s/%q args must still be
// flagged (all of these are also flagged by go vet).
func TestPrintfBadStringArgsStillFlagged(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	_ = fmt.Sprintf("%s", 42)
	_ = fmt.Sprintf("%q", true)
	_ = fmt.Sprintf("%s", []int{1})
}
`
	pass := testPass(t, src)
	ds := runPrintf(pass)
	if len(ds) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %v", len(ds), ds)
	}
	for _, d := range ds {
		if !strings.Contains(d.Message, "expects string or []byte") {
			t.Errorf("unexpected diagnostic: %s: %s", d.Pos, d.Message)
		}
	}
}

// QN-2: %f must accept UntypedFloat and %t must accept UntypedBool,
// mirroring go vet's verb/type matrix (which accepts each untyped kind
// for its natural verb).
//
// This is a unit test on checkVerbArg with synthetic untyped types,
// because the scenario cannot arise end-to-end: arguments to a resolved
// printf-like call (fmt.Sprintf etc., ...any) always receive their
// default types, while arguments to an unresolved call get no type info
// at all (so the analyzer bails before checking). What is pinned to go
// vet's matrix here is the kind handling itself.
func TestPrintfUntypedKinds(t *testing.T) {
	cases := []struct {
		verb rune
		kind types.BasicKind
		want string // "" means accepted
	}{
		// Untyped kinds accepted for their natural verbs (the QN-2 fix).
		{'f', types.UntypedFloat, ""},
		{'F', types.UntypedFloat, ""},
		{'g', types.UntypedFloat, ""},
		{'e', types.UntypedFloat, ""},
		{'t', types.UntypedBool, ""},
		{'d', types.UntypedInt, ""},
		{'d', types.UntypedRune, ""},
		{'c', types.UntypedRune, ""},
		{'s', types.UntypedString, ""},
		{'q', types.UntypedString, ""},
		// Untyped kinds of the wrong kind stay flagged, mirroring go vet
		// (verified: go vet flags %d with 2.0, %f with 42, %t with "x").
		{'d', types.UntypedFloat, "expects integer"},
		{'f', types.UntypedInt, "expects floating point"},
		{'f', types.UntypedString, "expects floating point"},
		{'t', types.UntypedString, "expects boolean"},
		{'t', types.UntypedFloat, "expects boolean"},
		{'t', types.UntypedInt, "expects boolean"},
		{'s', types.UntypedInt, "expects string or []byte"},
		{'s', types.UntypedBool, "expects string or []byte"},
		{'s', types.UntypedFloat, "expects string or []byte"},
	}
	for _, c := range cases {
		arg := &ast.BasicLit{Kind: token.FLOAT, Value: "0"}
		pass := &Pass{
			Fset: token.NewFileSet(),
			Info: &types.Info{
				Types: map[ast.Expr]types.TypeAndValue{
					arg: {Type: types.Typ[c.kind]},
				},
			},
		}
		verb := printfVerb{verb: c.verb, text: "%" + string(c.verb)}
		if msg := checkVerbArg(pass, verb, arg); msg != c.want {
			t.Errorf("%%%c with kind %d: got %q, want %q", c.verb, int(c.kind), msg, c.want)
		}
	}
}

// Resolved-world guard: with imports resolving, float/bool literals take
// their default types and must keep working for %f/%t (no behavior change
// on the common path).
func TestPrintfScalarLiteralsNoFP(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	_ = fmt.Sprintf("%f", 2.5)
	_ = fmt.Sprintf("%t", true)
	_ = fmt.Sprintf("%d", 42)
	_ = fmt.Sprintf("%s", "x")
	_ = fmt.Sprintf("%c", 'a')
}
`
	pass := testPass(t, src)
	if ds := runPrintf(pass); len(ds) != 0 {
		for _, d := range ds {
			t.Errorf("false positive: %s: %s", d.Pos, d.Message)
		}
	}
}
