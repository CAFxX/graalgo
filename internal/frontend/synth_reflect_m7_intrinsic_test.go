package main

// B-1: intrinsic panic-body funcdecls must not ship on the wire.
//
// The synthetic reflect_m7 package is built from syntheticReflectSource,
// which declares the __reflect_* intrinsics with panic bodies purely for
// type-checking; every call site is intercepted into the "reflectop" wire
// node. The funcdecls must never be emitted: bindGoFuncs would otherwise
// bind them under reflect_m7.__reflect_* and a //go:linkname to such a
// symbol would resolve to the panic body instead of failing fast.
//
// Red-green: before the fix, isSyntheticIntrinsic only recognized the
// retired "reflect" path, not the "reflect_m7" alias, so the wire doc
// contained ~100 __reflect_* funcdecls with panic bodies and the
// reflect_m7-internal calls to them were plain call nodes. After the
// fix, no intrinsic funcdecl is emitted and the internal calls lower to
// "reflectop".

import (
	"strings"
	"testing"
)

// reflectM7TriggerSrc is a program whose export pulls in the reflect_m7
// synthetic package: the M7 desugar for the concrete
// reflect.StructTag.Lookup two-value call needs its __reflect_taglookup
// helper (via m7HelperPkgOverride), and multiResultHelper imports the
// helper's package so the call can link.
const reflectM7TriggerSrc = `package main

import "reflect"

func main() {
	tag := reflect.StructTag("json:\"x\"")
	v, ok := tag.Lookup("json")
	println(v, ok)
}
`

// TestReflectM7NoIntrinsicFuncdecls: no name in reflectIntrinsicNames may
// appear as a funcdecl on the wire — those declarations are intercepted
// at every call site and their panic bodies must never be reachable,
// including via //go:linkname symbol resolution.
func TestReflectM7NoIntrinsicFuncdecls(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, reflectM7TriggerSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	leaked := []string{}
	for _, f := range findAllKind(doc, "funcdecl") {
		fields, _ := f["fields"].(map[string]any)
		name, _ := fields["name"].(string)
		if reflectIntrinsicNames[name] {
			pkgpath, _ := fields["pkgpath"].(string)
			leaked = append(leaked, pkgpath+"."+name)
		}
	}
	if len(leaked) > 0 {
		show := leaked
		if len(show) > 5 {
			show = show[:5]
		}
		t.Errorf("wire doc emits %d intrinsic panic-body funcdecls (linkname-reachable): %s",
			len(leaked), strings.Join(show, ", "))
	}
}

// TestReflectM7HelpersStillEmitted: the real-Go __reflect* helpers (not in
// reflectIntrinsicNames — e.g. __reflectTFieldByName, __reflect_taglookup)
// are ordinary synthetic functions and must still be emitted. This guards
// the intrinsic skip against over-skipping.
func TestReflectM7HelpersStillEmitted(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, reflectM7TriggerSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	foundRealHelper := false
	for _, f := range findAllKind(doc, "funcdecl") {
		fields, _ := f["fields"].(map[string]any)
		name, _ := fields["name"].(string)
		if strings.HasPrefix(name, "__reflect") && !reflectIntrinsicNames[name] {
			foundRealHelper = true
			break
		}
	}
	if !foundRealHelper {
		t.Error("wire doc lost the real-Go __reflect* helpers (over-skipped intrinsics?)")
	}
}

// TestReflectM7IntrinsicCallsLowerToReflectop: calls to intrinsics from
// inside reflect_m7 bodies (e.g. __reflect_tKind in rtype.Kind) must lower
// to "reflectop" wire nodes, not to direct calls of the panic-body
// funcdecls.
func TestReflectM7IntrinsicCallsLowerToReflectop(t *testing.T) {
	out, err := ExportFile(writeIfaceProg(t, reflectM7TriggerSrc))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	reflectops := findAllKind(doc, "reflectop")
	if len(reflectops) == 0 {
		t.Error("no reflectop wire nodes: intrinsic call sites were not intercepted")
	}
	// No direct call node may target an intrinsic: every such call must
	// have been intercepted before the funcByObj lookup. (The call
	// node's "fun" is an ident carrying the target name.)
	for _, f := range findAllKind(doc, "funcdecl") {
		fields, _ := f["fields"].(map[string]any)
		for _, c := range findAllKind(f, "call") {
			cf, _ := c["fields"].(map[string]any)
			fun, _ := cf["fun"].(map[string]any)
			funFields, _ := fun["fields"].(map[string]any)
			if target, _ := funFields["name"].(string); reflectIntrinsicNames[target] {
				name, _ := fields["name"].(string)
				t.Errorf("funcdecl %q calls intrinsic %q directly (panic body reachable)", name, target)
			}
		}
	}
}

// TestReflectM7LinknameToIntrinsicHasNoProvider: a //go:linkname stub
// naming a __reflect_* symbol exports as a consumer stub, but the wire
// doc must contain no provider funcdecl for it — so Linknames.dispatch
// fails fast on the unbound symbol instead of invoking a panic body.
// The StructTag.Lookup call pulls in the reflect_m7 package, so pre-fix
// this test sees the leaked provider funcdecls and fails.
func TestReflectM7LinknameToIntrinsicHasNoProvider(t *testing.T) {
	const src = `package main

import "unsafe"
import "reflect"

//go:linkname probe reflect_m7.__reflect_tKind
func probe(tok unsafe.Pointer) int

func main() {
	tag := reflect.StructTag("json:\"x\"")
	v, ok := tag.Lookup("json")
	println(v, ok)
	println(probe(nil))
}
`
	out, err := ExportFile(writeIfaceProg(t, src))
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	doc := decodeExport(t, out)
	stubFound := false
	for _, f := range findAllKind(doc, "funcdecl") {
		fields, _ := f["fields"].(map[string]any)
		name, _ := fields["name"].(string)
		if _, isStub := fields["linkname"]; isStub && name == "probe" {
			stubFound = true
			continue
		}
		if reflectIntrinsicNames[name] {
			t.Errorf("provider funcdecl %q on the wire: linkname would resolve to its panic body", name)
		}
	}
	if !stubFound {
		t.Error("expected the //go:linkname consumer stub for probe on the wire")
	}
}
