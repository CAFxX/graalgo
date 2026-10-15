package main

// P3-M11: exporter tests for the synthetic `sync.Pool` binding.
// Red-green: these fail before the fix (import "sync" has no Pool —
// "undefined: sync.Pool") and pass after.

import (
	"strings"
	"testing"
)

// TestSyncPoolAccepted: Pool construction and Put/Get export as normal
// Go with the real sync package (503608ea).
func TestSyncPoolAccepted(t *testing.T) {
	src := `package main

import "sync"

var gPool sync.Pool

func main() {
	var p sync.Pool
	p.Put(1)
	p.Put("x")
	_ = p.Get()
	p2 := sync.Pool{}
	p2.Put(2)
	_ = p2.Get()
	pp := new(sync.Pool)
	pp.Put(3)
	_ = pp.Get()
	pp2 := &sync.Pool{}
	pp2.Put(4)
	_ = pp2.Get()
	gPool.Put(5)
	_ = gPool.Get()
	_ = p
}
`
	out, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with sync.Pool: %v", err)
	}
	for _, want := range []string{"Put", "Get"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported JSON missing %s", want)
		}
	}
}

// TestSyncPoolNewField: the New func field — keyed literal, read,
// write, and direct invocation — exports with the real sync package
// (503608ea).
func TestSyncPoolNewField(t *testing.T) {
	src := `package main

import "sync"

func mk() func() any { return func() any { return 42 } }

func main() {
	p := sync.Pool{New: func() any { return 1 }}
	p.New = func() any { return 2 }
	f := p.New
	_ = f
	_ = p.New()
	q := &sync.Pool{New: mk()}
	q.New = mk()
	_ = q.New()
	_ = p
	_ = q
}
`
	_, err := ExportFile(writeSyncProg(t, src))
	if err != nil {
		t.Fatalf("ExportFile with sync.Pool New field: %v", err)
	}
}

// TestSyncPoolLiteralScopeErrors: Pool literals that gc rejects are
// compile errors here too — the unexported fields are declared in the
// synthetic struct exactly so go/types enforces this.
func TestSyncPoolLiteralScopeErrors(t *testing.T) {
	cases := []string{
		// Unkeyed: gc reports "implicit assignment of unexported field".
		"p := sync.Pool{func() any { return 1 }}\n\t_ = p",
		// Non-New key: unexported field, not nameable from main.
		"p := sync.Pool{localSize: 1}\n\t_ = p",
		// Two elements: only the single New key is accepted.
		"p := sync.Pool{New: func() any { return 1 }, New: func() any { return 2 }}\n\t_ = p",
	}
	for _, body := range cases {
		src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\t" + body + "\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err == nil {
			t.Errorf("%q: expected type error, got success", body)
		}
	}
}

// TestSyncPoolNewAssignScopeError: with real sync (503608ea), exotic
// assignment shapes touching Pool.New are ordinary Go and succeed.
func TestSyncPoolNewAssignScopeError(t *testing.T) {
	cases := []string{
		"var a any\n\ta, p.New = 1, func() any { return 2 }\n\t_ = a",
	}
	for _, body := range cases {
		src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar p sync.Pool\n\t" + body + "\n\t_ = p\n}\n"
		_, err := ExportFile(writeSyncProg(t, src))
		if err != nil {
			t.Errorf("%q: expected success with real sync, got error: %v", body, err)
		}
	}
}

// TestSyncPoolGetResultUsable: p.Get() results flow into interface
// positions — assignment, type assertion, and type switch.
func TestSyncPoolGetResultUsable(t *testing.T) {
	src := `package main

import "sync"

func main() {
	var p sync.Pool
	p.Put("hello")
	v := p.Get()
	s := v.(string)
	_ = s
	switch x := p.Get().(type) {
	case string:
		println("str", x)
	default:
		println("other")
	}
}
`
	if _, err := ExportFile(writeSyncProg(t, src)); err != nil {
		t.Fatalf("ExportFile with p.Get() results: %v", err)
	}
}
