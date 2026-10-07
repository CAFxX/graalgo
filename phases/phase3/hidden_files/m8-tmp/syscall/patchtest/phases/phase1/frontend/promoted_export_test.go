package main

// GAP-6: promoted-method call-site resolution. Direct call sites x.M(args)
// where M is promoted through one or more embedded fields must resolve to
// the declaring method, with the receiver adapted along go/types'
// Selection.Index() path (a "selector" wire node, mirroring exportSelector
// and the P3-M3 method-table "index" path). Ambiguous selectors never
// resolve; implicit &/* adaptation stays a WS D scope error; direct-method
// calls are byte-identical to before (no selector on the receiver).
//
// Red-green: every success case fails before the GAP-6 change with
// "gofrontend: method M not found on Outer" (the receiver adaptation in
// methodRefNode additionally fails the suite if removed: the declared
// receiver check rejects the unadapted outer receiver).

import (
	"strings"
	"testing"
)

// singleMethodRef returns the only methodref node in the export.
func singleMethodRef(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	mrs := findAllKind(doc, "methodref")
	if len(mrs) != 1 {
		t.Fatalf("want exactly 1 methodref node, got %d", len(mrs))
	}
	return mrs[0]
}

// methodRefFuncName resolves the methodref's obj index to the object name.
func methodRefFuncName(t *testing.T, doc map[string]any, mr map[string]any) string {
	t.Helper()
	objs, ok := doc["objects"].([]any)
	if !ok {
		t.Fatal("export has no objects array")
	}
	f, ok := mr["fields"].(map[string]any)
	if !ok {
		t.Fatal("methodref has no fields")
	}
	idf, ok := f["obj"].(float64)
	if !ok {
		t.Fatalf("methodref obj is not a number: %v", f["obj"])
	}
	o, ok := objs[int(idf)].(map[string]any)
	if !ok {
		t.Fatalf("object %d is not an object", int(idf))
	}
	name, _ := o["name"].(string)
	return name
}

// methodRefRecv returns the methodref's receiver node.
func methodRefRecv(t *testing.T, mr map[string]any) map[string]any {
	t.Helper()
	f, ok := mr["fields"].(map[string]any)
	if !ok {
		t.Fatal("methodref has no fields")
	}
	r, ok := f["recv"].(map[string]any)
	if !ok {
		t.Fatal("methodref has no recv node")
	}
	return r
}

// requireRecvSelector asserts the receiver is a selector adaptation node
// with the expected selpath and final field name, over an ident receiver.
func requireRecvSelector(t *testing.T, mr map[string]any, wantPath []int, wantField string) {
	t.Helper()
	r := methodRefRecv(t, mr)
	if r["kind"] != "selector" {
		t.Fatalf("methodref recv kind = %v, want selector (promoted receiver adaptation)", r["kind"])
	}
	f, ok := r["fields"].(map[string]any)
	if !ok {
		t.Fatal("selector recv has no fields")
	}
	sp, ok := f["selpath"].([]any)
	if !ok {
		t.Fatal("selector recv has no selpath")
	}
	if len(sp) != len(wantPath) {
		t.Fatalf("selpath = %v, want %v", sp, wantPath)
	}
	for i, v := range sp {
		fv, ok := v.(float64)
		if !ok || int(fv) != wantPath[i] {
			t.Fatalf("selpath = %v, want %v", sp, wantPath)
		}
	}
	if fn, _ := f["field"].(string); fn != wantField {
		t.Fatalf("selector field = %q, want %q", fn, wantField)
	}
	x, ok := f["x"].(map[string]any)
	if !ok || x["kind"] != "ident" {
		t.Fatalf("selector x kind = %v, want ident (the unadapted receiver expression)", x["kind"])
	}
}

// TestPromotedCallDirectUnchanged: a direct method call keeps the receiver
// expression untouched — no selector adaptation node (GAP-6 must not change
// direct-method behavior).
func TestPromotedCallDirectUnchanged(t *testing.T) {
	const src = `package main

type Inner struct{ V int }

func (i Inner) Get() int { return i.V }

func main() {
	i := Inner{V: 7}
	println(i.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Get" {
		t.Fatalf("methodref resolves to %q, want Get", name)
	}
	if r := methodRefRecv(t, mr); r["kind"] != "ident" {
		t.Fatalf("direct call recv kind = %v, want ident (no receiver adaptation)", r["kind"])
	}
}

// TestPromotedCallSingleEmbed: o.Get() with Outer{Inner} resolves to
// Inner.Get; the receiver is adapted via selector selpath [0].
func TestPromotedCallSingleEmbed(t *testing.T) {
	const src = `package main

type Inner struct{ V int }

func (i Inner) Get() int { return i.V }

type Outer struct{ Inner }

func main() {
	o := Outer{Inner{V: 42}}
	println(o.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Get" {
		t.Fatalf("methodref resolves to %q, want the promoted Inner.Get", name)
	}
	requireRecvSelector(t, mr, []int{0}, "Inner")
}

// TestPromotedCallMultiLevel: o.Get() with Outer{Mid{Inner}} adapts along
// selpath [0 0].
func TestPromotedCallMultiLevel(t *testing.T) {
	const src = `package main

type Inner struct{ V int }

func (i Inner) Get() int { return i.V }

type Mid struct{ Inner }

type Outer struct{ Mid }

func main() {
	o := Outer{Mid{Inner{V: 42}}}
	println(o.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Get" {
		t.Fatalf("methodref resolves to %q, want the promoted Inner.Get", name)
	}
	requireRecvSelector(t, mr, []int{0, 0}, "Inner")
}

// TestPromotedCallPointerEmbed: po.Get() with POuter{*PInner} and Get on
// *PInner adapts along selpath [0] (the selector yields the pointer; the
// declared receiver *PInner matches, so no implicit &/* is invented).
func TestPromotedCallPointerEmbed(t *testing.T) {
	const src = `package main

type PInner struct{ V int }

func (p *PInner) Get() int { return p.V }

type POuter struct{ *PInner }

func main() {
	po := POuter{&PInner{V: 3}}
	println(po.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Get" {
		t.Fatalf("methodref resolves to %q, want the promoted (*PInner).Get", name)
	}
	requireRecvSelector(t, mr, []int{0}, "PInner")
}

// TestPromotedCallValueRecvViaPointerEmbedFailsFast: a value-receiver
// method promoted via a POINTER-embedded field needs gc's implicit (*p)
// deref; the adaptation must not invent it — WS D scope error, named.
func TestPromotedCallValueRecvViaPointerEmbedFailsFast(t *testing.T) {
	const src = `package main

type PInner struct{ V int }

func (p PInner) Get() int { return p.V }

type POuter struct{ *PInner }

func main() {
	po := POuter{&PInner{V: 3}}
	println(po.Get())
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected WS D scope error (implicit deref), got success")
	}
	if !strings.Contains(err.Error(), "implicit address-of or deref") {
		t.Fatalf("error %q does not name the WS D implicit &/* scope error", err)
	}
}

// TestPromotedCallPointerRecvViaValueEmbedFailsFast: a pointer-receiver
// method promoted via a VALUE-embedded field needs gc's implicit & on the
// field; the adaptation must not invent it — WS D scope error, named.
func TestPromotedCallPointerRecvViaValueEmbedFailsFast(t *testing.T) {
	const src = `package main

type Buf struct{ n int }

func (b *Buf) Write() int { return b.n }

type V struct{ Buf }

func main() {
	v := V{Buf{n: 1}}
	println(v.Write())
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected WS D scope error (implicit address-of), got success")
	}
	if !strings.Contains(err.Error(), "implicit address-of or deref") {
		t.Fatalf("error %q does not name the WS D implicit &/* scope error", err)
	}
}

// TestPromotedCallAmbiguousFailsFast: an ambiguous selector (M promoted
// from both A and B) never resolves — go/types rejects it at type-check
// time, so no candidate is silently picked.
func TestPromotedCallAmbiguousFailsFast(t *testing.T) {
	const src = `package main

type A struct{}

func (a A) M() int { return 1 }

type B struct{}

func (b B) M() int { return 2 }

type C struct {
	A
	B
}

func main() {
	c := C{}
	println(c.M())
}
`
	_, err := ExportFile(writeIfaceProg(t, src))
	if err == nil {
		t.Fatal("expected ambiguous-selector failure, got success")
	}
	if !strings.Contains(err.Error(), "ambiguous selector") {
		t.Fatalf("error %q does not report the ambiguous selector", err)
	}
}

// TestPromotedCallGenericSharedInstantiation: a generic method promoted
// through an embedding instantiates once, keyed by the declaring receiver
// (main.GInner[int].Get) — the same instantiation a direct call on
// GInner[int] would use.
func TestPromotedCallGenericSharedInstantiation(t *testing.T) {
	const src = `package main

type GInner[T any] struct{ x T }

func (g GInner[T]) Get() T { return g.x }

type GOuter[T any] struct{ GInner[T] }

func main() {
	o := GOuter[int]{GInner[int]{x: 42}}
	println(o.Get())
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "main.GInner[int].Get" {
		t.Fatalf("methodref resolves to %q, want the shared main.GInner[int].Get instantiation", name)
	}
	requireRecvSelector(t, mr, []int{0}, "GInner")
}

// TestPromotedCallGoStatement: go o.Get() hoists the receiver before the
// call (P3-M5); the hoisted ident is adapted the same way.
func TestPromotedCallGoStatement(t *testing.T) {
	const src = `package main

type Inner struct{ V int }

func (i Inner) Get() int { return i.V }

type Outer struct{ Inner }

func main() {
	o := Outer{Inner{V: 42}}
	go o.Get()
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	doc := decodeExport(t, out)
	mr := singleMethodRef(t, doc)
	if name := methodRefFuncName(t, doc, mr); name != "Get" {
		t.Fatalf("methodref resolves to %q, want the promoted Inner.Get", name)
	}
	requireRecvSelector(t, mr, []int{0}, "Inner")
}
