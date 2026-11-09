// Regression tests for the R-1108-171 remediation lane
// (independent codebase review, 2026-11-08-22).
//
// describeType used to emit literal empty method/embedded lists for every
// named and interface type, and a hardcoded offset 0 for every struct
// field, so no method set ever reached the wire. For interface types the
// loss was unrecoverable: interface methods appear nowhere else in the
// document.
//
// The tests below are demonstrably capable of failing: each one fails on
// the pre-fix emitter (methods [] / embedded [] / offsets all 0), verified
// by running them against master@7ca48356 before the fix.
//
// Ground truth for the expected values is go/types itself, probed
// independently of the emitter:
//   - types.NewMethodSet(T) lists a named type's methods name-sorted,
//     value-receiver methods only (pointer-receiver methods belong to the
//     method set of *T, not T).
//   - Interface.NumMethods()/Method(i) is the FULL flattened method set
//     (explicit + promoted from embedded interfaces), name-sorted;
//     NumExplicitMethods()/ExplicitMethod(i) is the explicit subset.
//   - Interface.EmbeddedTypes() yields the embedded types.
//   - types.SizesFor("gc", "amd64").Offsetsof gives real field offsets
//     (v1 uses the same sizes: internal/frontend/export.go).
package emit

import (
	"strings"
	"testing"
)

// methodsSources is a package exercising every describeType case this lane
// fixes: a non-empty interface, an interface embedding another interface,
// a named struct type with value- and pointer-receiver methods, and an
// empty interface. Use references each type so it lands in the type table
// (types are emitted lazily, on reference).
const methodsSources = `package demo

type Reader interface { Read([]byte) (int, error) }

type ReadWriter interface {
	Reader
	Write([]byte) (int, error)
	Close() error
}

type Empty interface{}

type Counter struct {
	a int8
	b int64
	c int8
}

func (c Counter) Value() int { return int(c.b) }
func (c *Counter) Inc()      { c.b++ }
func (c Counter) String() string { return "" }

func Use(r Reader, w ReadWriter, e Empty, c Counter) int {
	return c.Value()
}
`

// findNamedType returns the type-table id and detail map of the named or
// interface type whose detail name ends with "."+name (namedName prefixes
// the package path, e.g. "demo.Counter").
func findNamedType(t *testing.T, doc *Document, name string) (int, map[string]any) {
	t.Helper()
	for i, tv := range doc.Types {
		m, ok := tv.(map[string]any)
		if !ok {
			continue
		}
		if m["kind"] != "named" && m["kind"] != "interface" {
			continue
		}
		d, ok := m["detail"].(map[string]any)
		if !ok {
			continue
		}
		if n, _ := d["name"].(string); n == name || strings.HasSuffix(n, "."+name) {
			return i, d
		}
	}
	t.Fatalf("type %q not found in type table (%d entries)", name, len(doc.Types))
	return -1, nil
}

// methodNames returns the method names in wire order from a type detail's
// "methods" list, failing if the list or any entry is malformed. Each
// entry must carry a "name" and a "type" that is a valid type-table id of
// a signature type.
func methodNames(t *testing.T, doc *Document, detail map[string]any) []string {
	t.Helper()
	raw, ok := detail["methods"].([]any)
	if !ok {
		t.Fatalf("methods is %T, want []any", detail["methods"])
	}
	var names []string
	for _, mv := range raw {
		m, ok := mv.(map[string]any)
		if !ok {
			t.Fatalf("method entry is %T, want map[string]any", mv)
		}
		name, _ := m["name"].(string)
		if name == "" {
			t.Fatalf("method entry has no name: %v", m)
		}
		sigID, ok := m["type"].(int)
		if !ok {
			t.Fatalf("method %q has type %T, want int type id", name, m["type"])
		}
		if sigID < 0 || sigID >= len(doc.Types) {
			t.Fatalf("method %q has out-of-range signature type id %d", name, sigID)
		}
		sig, ok := doc.Types[sigID].(map[string]any)
		if !ok || sig["kind"] != "signature" {
			t.Fatalf("method %q signature type id %d is not a signature entry: %v", name, sigID, doc.Types[sigID])
		}
		names = append(names, name)
	}
	return names
}

func TestEmitInterfaceMethods(t *testing.T) {
	doc := emitTestSrc(t, methodsSources)

	// Reader: one method, no embedded types.
	_, reader := findNamedType(t, doc, "Reader")
	if got := methodNames(t, doc, reader); !equalStrings(got, []string{"Read"}) {
		t.Fatalf("Reader methods = %v, want [Read]", got)
	}
	if emb, _ := reader["embedded"].([]any); len(emb) != 0 {
		t.Fatalf("Reader embedded = %v, want []", emb)
	}
	// The Read signature must be a real signature type: 1 param, 2 results.
	sig := signatureOf(t, doc, reader, "Read")
	if n := len(sigParams(sig)); n != 1 {
		t.Fatalf("Read signature params = %d, want 1 ([]byte)", n)
	}
	if n := len(sigResults(sig)); n != 2 {
		t.Fatalf("Read signature results = %d, want 2 (int, error)", n)
	}
}

func TestEmitEmbeddedInterfaceMethods(t *testing.T) {
	doc := emitTestSrc(t, methodsSources)

	// ReadWriter: the FULL flattened method set (explicit Close/Write
	// plus Read promoted from embedded Reader), in method-set order.
	_, rw := findNamedType(t, doc, "ReadWriter")
	if got := methodNames(t, doc, rw); !equalStrings(got, []string{"Close", "Read", "Write"}) {
		t.Fatalf("ReadWriter methods = %v, want [Close Read Write]", got)
	}
	// Embedded types: exactly the Reader interface, as a type id.
	emb, ok := rw["embedded"].([]any)
	if !ok {
		t.Fatalf("ReadWriter embedded is %T, want []any", rw["embedded"])
	}
	if len(emb) != 1 {
		t.Fatalf("ReadWriter embedded = %v, want [Reader type id]", emb)
	}
	embID, ok := emb[0].(int)
	if !ok {
		t.Fatalf("ReadWriter embedded[0] is %T, want int type id", emb[0])
	}
	if embID < 0 || embID >= len(doc.Types) {
		t.Fatalf("ReadWriter embedded[0] = %d, out of range", embID)
	}
	// The embedded type is the *types.Named Reader — the same canonical
	// type-table id every other reference to Reader uses (the type table
	// is keyed by go/types identity, so embedded must not point at a
	// different, unwrapped entry). Its underlying interface entry is what
	// carries the method set.
	embEntry, ok := doc.Types[embID].(map[string]any)
	if !ok || embEntry["kind"] != "named" {
		t.Fatalf("ReadWriter embedded[0] type %d is not the named Reader entry: %v", embID, doc.Types[embID])
	}
	embDetail, _ := embEntry["detail"].(map[string]any)
	if n, _ := embDetail["name"].(string); n != "Reader" && !strings.HasSuffix(n, ".Reader") {
		t.Fatalf("ReadWriter embedded[0] is %q, want Reader", n)
	}
	underID, ok := embDetail["underlying"].(int)
	if !ok {
		t.Fatalf("Reader entry has no underlying type id: %v", embDetail)
	}
	under, ok := doc.Types[underID].(map[string]any)
	if !ok || under["kind"] != "interface" {
		t.Fatalf("Reader underlying type %d is not an interface entry: %v", underID, doc.Types[underID])
	}
	underDetail, _ := under["detail"].(map[string]any)
	if got := methodNames(t, doc, underDetail); !equalStrings(got, []string{"Read"}) {
		t.Fatalf("Reader underlying interface methods = %v, want [Read]", got)
	}

	// Empty: no methods, no embedded types, and no crash.
	_, empty := findNamedType(t, doc, "Empty")
	if got := methodNames(t, doc, empty); len(got) != 0 {
		t.Fatalf("Empty methods = %v, want []", got)
	}
	if emb, _ := empty["embedded"].([]any); len(emb) != 0 {
		t.Fatalf("Empty embedded = %v, want []", emb)
	}
}

func TestEmitNamedTypeMethods(t *testing.T) {
	doc := emitTestSrc(t, methodsSources)

	// Counter: the method set of the named type itself, in method-set
	// order (name-sorted per go/types). Value-receiver methods only:
	// Inc has a pointer receiver, so it belongs to *Counter's method
	// set, not Counter's, and must not be listed here.
	_, counter := findNamedType(t, doc, "Counter")
	if got := methodNames(t, doc, counter); !equalStrings(got, []string{"String", "Value"}) {
		t.Fatalf("Counter methods = %v, want [String Value]", got)
	}
}

func TestEmitStructFieldOffsets(t *testing.T) {
	doc := emitTestSrc(t, methodsSources)

	// Counter is struct { a int8; b int64; c int8 }: real gc/amd64
	// offsets are [0 8 16] (b is 8-aligned, c follows at 16) — not the
	// hardcoded 0 the emitter used to write for every field.
	_, counter := findNamedType(t, doc, "Counter")
	fields, ok := counter["fields"].([]any)
	// Named types carry the struct under "underlying"; the struct entry
	// itself holds the fields. Resolve it.
	if !ok {
		underID, ok := counter["underlying"].(int)
		if !ok {
			t.Fatalf("Counter detail has no fields and no underlying type id: %v", counter)
		}
		under, ok := doc.Types[underID].(map[string]any)
		if !ok || under["kind"] != "struct" {
			t.Fatalf("Counter underlying type %d is not a struct entry: %v", underID, doc.Types[underID])
		}
		underDetail, _ := under["detail"].(map[string]any)
		fields, ok = underDetail["fields"].([]any)
		if !ok {
			t.Fatalf("Counter underlying struct has no fields list: %v", underDetail)
		}
	}
	if len(fields) != 3 {
		t.Fatalf("Counter fields = %d, want 3", len(fields))
	}
	want := []int{0, 8, 16}
	for i, fv := range fields {
		f, ok := fv.(map[string]any)
		if !ok {
			t.Fatalf("field %d is %T, want map[string]any", i, fv)
		}
		off, ok := f["offset"].(int)
		if !ok {
			t.Fatalf("field %d (%v) has offset %T, want int", i, f["name"], f["offset"])
		}
		if off != want[i] {
			t.Fatalf("field %d offset = %d, want %d", i, off, want[i])
		}
	}
}

// signatureOf resolves a method's signature type entry from a type detail.
func signatureOf(t *testing.T, doc *Document, detail map[string]any, method string) map[string]any {
	t.Helper()
	raw, _ := detail["methods"].([]any)
	for _, mv := range raw {
		m, ok := mv.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == method {
			sigID, _ := m["type"].(int)
			sig, _ := doc.Types[sigID].(map[string]any)
			return sig
		}
	}
	t.Fatalf("method %q not found in %v", method, detail["methods"])
	return nil
}

func sigParams(sig map[string]any) []any {
	d, _ := sig["detail"].(map[string]any)
	p, _ := d["params"].([]any)
	return p
}

func sigResults(sig map[string]any) []any {
	d, _ := sig["detail"].(map[string]any)
	r, _ := d["results"].([]any)
	return r
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
