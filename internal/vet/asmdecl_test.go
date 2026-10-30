package vet

import (
	"go/types"
	"testing"
)

// asmKindForType must never panic on an underlying type it does not
// model (review23 T-P2-7: the old code ended the type switch with
// panic("unreachable"), crashing the whole vet run). A union — the
// underlying type of a type parameter with a union constraint — is
// such a type: it must yield the asmUnknown sentinel instead.
func TestAsmKindForTypeUnknown(t *testing.T) {
	union := types.NewUnion([]*types.Term{
		types.NewTerm(false, types.Typ[types.Int]),
		types.NewTerm(false, types.Typ[types.String]),
	})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("asmKindForType panicked on union type: %v", r)
		}
	}()
	if got := asmKindForType(union, 8); got != asmUnknown {
		t.Errorf("asmKindForType(union) = %v, want asmUnknown (%v)", got, asmUnknown)
	}
}

func TestAsmKindForTypeKnown(t *testing.T) {
	if got := asmKindForType(types.Typ[types.Int], 8); got != asmKind(8) {
		t.Errorf("asmKindForType(int) = %v, want %v", got, asmKind(8))
	}
	if got := asmKindForType(types.Typ[types.String], 16); got != asmString {
		t.Errorf("asmKindForType(string) = %v, want asmString", got)
	}
}
