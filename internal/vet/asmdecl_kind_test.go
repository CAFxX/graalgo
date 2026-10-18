package vet

import (
	"go/types"
	"strings"
	"testing"
)

// RV18-P1-21(7): asmKindForType ended in panic("unreachable"), reachable on
// *types.Union from generic code — and go/types panics measuring type
// parameters/unions at all (Sizeof/Alignof), so a package with a generic
// function plus a .s file crashed graalgo vet in asmParseDecl before even
// reaching asmKindForType. Both now degrade gracefully: functions without a
// concrete assembly layout are skipped, and a union degrades to an opaque
// kind of its declared size.
func asmKindForTypeNoPanic(t *testing.T, typ types.Type, size int) asmKind {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("asmKindForType panicked on %T: %v", typ, r)
		}
	}()
	return asmKindForType(typ, size)
}

func TestAsmKindForTypeUnion(t *testing.T) {
	union := types.NewUnion([]*types.Term{
		types.NewTerm(true, types.Typ[types.Int]),
		types.NewTerm(true, types.Typ[types.String]),
	})
	if k := asmKindForTypeNoPanic(t, union, 8); k != asmKind(8) {
		t.Errorf("expected opaque kind asmKind(8) for union, got %v", k)
	}
}

func TestAsmKindForTypeTypeParam(t *testing.T) {
	// A type parameter normalizes via Underlying() to its constraint
	// interface; it must never panic. (Verified: this held even before
	// the fix; the test pins the no-crash contract.)
	bound := types.NewUnion([]*types.Term{
		types.NewTerm(true, types.Typ[types.Int]),
	})
	tp := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), bound)
	if k := asmKindForTypeNoPanic(t, tp, 8); k != asmInterface {
		t.Errorf("expected asmInterface for type parameter, got %v", k)
	}
}

func TestAsmDeclGenericNoCrash(t *testing.T) {
	// A bodyless generic declaration is a type error ("generic function is
	// missing function body"), but vet tolerates type errors and must not
	// crash on them: measuring the type parameter panicked inside go/types.
	// The function has no single assembly layout, so it is skipped — and
	// the declaration exists, so it must not be reported as missing.
	// (Before the fix this panicked in go/types' Sizeof/Alignof.)
	goSrc := `package p

func generic[T any](x T)
`
	// Note: · is U+00B7 MIDDLE DOT, as emitted by the Go assembler.
	asmSrc := "TEXT \u00b7generic(SB),$0-8\n" +
		"\tRET\n"
	joined := runVetOnModule(t, "", map[string]string{
		"p.go": goSrc, "p_amd64.s": asmSrc,
	})
	if strings.Contains(joined, "missing Go declaration") {
		t.Errorf("false positive: the generic declaration exists:\n%s", joined)
	}
}
