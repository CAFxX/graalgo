package main

// fix-9424f51a (PKG-073: resolve promoted methods through type aliases):
// when a method is called on a type alias (e.g. reflect's
// `type funcType = abi.FuncType`), the exporter failed with
// "gofrontend: method Kind not found on FuncType". Root cause:
// types.NewMethodSet does not reliably find promoted methods through
// alias types, and resolvePromotedMethodCall passed selection.Recv()
// (e.g. *funcType, pointer to alias) straight in; embeddedFieldType and
// embeddedFieldName likewise failed to unwrap aliases when walking the
// struct field path. The general fix adds unaliasType (recursive
// *types.Alias unwrapping, preserving pointer structure) and applies it
// in resolvePromotedMethodCall (before both NewMethodSet calls) and in
// the embedded-field walkers. It applies to all aliases in all
// packages, not just reflect/internal/abi.
//
// Red-green: before the fix, TestPromotedCallThroughAlias fails with
// the "gofrontend: method Kind not found on Outer" error (types.NewMethodSet
// does not find the promoted method through the alias); after, the
// methodref resolves to the promoted Inner.Kind with the selector adaptation.

import (
	"testing"
)

// TestPromotedCallThroughAlias pins promoted-method resolution through
// a type alias: a.Kind() where `type A = Outer` and Outer embeds Inner
// (which declares Kind). Mirrors TestPromotedCallSingleEmbed but routes
// the receiver type through an alias.
func TestPromotedCallThroughAlias(t *testing.T) {
	const src = `package main

type Inner struct{ V int }

func (i Inner) Kind() string { return "inner" }

type Outer struct{ Inner }

type A = Outer

func main() {
	var a A
	println(a.Kind())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Kind" {
		t.Fatalf("methodref resolves to %q, want the promoted Inner.Kind", name)
	}
	requireRecvSelector(t, mr, []int{0}, "Inner")
}
