package main

// PKG-041 regression: type aliases must canonicalize to the aliased type's
// descriptor. go/types represents every use of an alias as *types.Alias; in
// gc an alias IS the aliased type (one type, one identity): interface ==
// compares them equal and reflect.TypeOf reports a single name. The exporter
// used to mint a separate "alias" wire entry per spelling (e.g. os.PathError
// — declared `type PathError = fs.PathError` — vs io/fs.PathError), giving
// one Go type two wire ids and breaking both.
//
// The fix canonicalizes aliases in typeID (the single choke point for wire
// type ids) via types.Unalias, so no alias entries are ever minted and every
// spelling maps to the aliased type's id. General: all aliases, all
// packages — not PathError-specific, not name-keyed.
//
// Each subtest exports a small program and asserts on the wire types table:
// (a) no entry has kind "alias"; (b) the aliased type appears exactly once,
// under its canonical qualified name; (c) no entry carries the alias's own
// name. All assertions fail pre-fix (alias entries present) and pass
// post-fix (verified via patch-revert, never git stash — the stash ref is
// shared across worktrees).

import (
	"encoding/json"
	"strings"
	"testing"
)

// aliasWireTypes exports src and returns the decoded wire types table.
func aliasWireTypes(t *testing.T, src string) []map[string]any {
	t.Helper()
	doc := exportSrc(t, src)
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(doc), &raw); err != nil {
		t.Fatalf("unmarshal doc: %v", err)
	}
	typesRaw, _ := raw["types"].([]any)
	types := make([]map[string]any, len(typesRaw))
	for i, tr := range typesRaw {
		m, ok := tr.(map[string]any)
		if !ok {
			t.Fatalf("types[%d] not an object: %T", i, tr)
		}
		types[i] = m
	}
	return types
}

func countKind(types []map[string]any, kind string) int {
	n := 0
	for _, e := range types {
		if e["kind"] == kind {
			n++
		}
	}
	return n
}

// namedEntries returns the "name" of every entry with kind "named".
func namedEntries(types []map[string]any) []string {
	var out []string
	for _, e := range types {
		if e["kind"] != "named" {
			continue
		}
		d, ok := e["detail"].(map[string]any)
		if !ok {
			continue
		}
		if name, ok := d["name"].(string); ok {
			out = append(out, name)
		}
	}
	return out
}

func assertNoAliasEntries(t *testing.T, types []map[string]any) {
	t.Helper()
	if n := countKind(types, "alias"); n != 0 {
		t.Errorf("wire types table has %d \"alias\" entries; aliases must canonicalize to the aliased type (PKG-041)", n)
	}
}

func TestAliasCanonicalization(t *testing.T) {
	t.Run("universe alias any", func(t *testing.T) {
		types := aliasWireTypes(t, `package main

var a any
var b interface{}

func use(x any, y interface{}) (any, interface{}) { return x, y }

func main() {
	a = 1
	b = 2
	_, _ = use(a, b)
}
`)
		assertNoAliasEntries(t, types)
		// The empty interface appears exactly once; no entry is named "any".
		emptyIfaces := 0
		for _, e := range types {
			if e["kind"] == "interface" {
				if d, ok := e["detail"].(map[string]any); ok {
					if empty, _ := d["empty"].(bool); empty {
						emptyIfaces++
					}
				}
			}
			if e["kind"] == "named" {
				if d, ok := e["detail"].(map[string]any); ok {
					if name, _ := d["name"].(string); name == "any" {
						t.Errorf("found named entry %q; \"any\" is an alias and must not mint its own entry", name)
					}
				}
			}
		}
		if emptyIfaces != 1 {
			t.Errorf("want exactly 1 empty-interface entry, got %d", emptyIfaces)
		}
	})

	t.Run("local alias to imported named type", func(t *testing.T) {
		types := aliasWireTypes(t, `package main

import "strings"

type SB = strings.Builder

var a SB
var b strings.Builder

func main() {
	a.WriteString("x")
	b.WriteString("y")
	_, _ = a, b
}
`)
		assertNoAliasEntries(t, types)
		names := namedEntries(types)
		count := 0
		for _, n := range names {
			if n == "strings.Builder" {
				count++
			}
			if strings.Contains(n, "SB") {
				t.Errorf("found named entry %q; alias SB must not mint its own entry", n)
			}
		}
		if count != 1 {
			t.Errorf("want exactly 1 named entry for strings.Builder, got %d (all: %v)", count, names)
		}
	})

	t.Run("cross-package alias os.PathError", func(t *testing.T) {
		// The reported PKG-041 shape: synthetic os declares
		// `type PathError = fs.PathError`; both spellings must share one
		// descriptor.
		types := aliasWireTypes(t, `package main

import (
	"io/fs"
	"os"
)

func conv(pe *os.PathError) *fs.PathError { return pe }

func main() {
	e := &os.PathError{Op: "open", Path: "x"}
	var _ *fs.PathError = conv(e)
}
`)
		assertNoAliasEntries(t, types)
		names := namedEntries(types)
		count := 0
		for _, n := range names {
			if n == "io/fs.PathError" {
				count++
			}
			if n == "os.PathError" {
				t.Errorf("found named entry %q; os.PathError is an alias of fs.PathError and must not mint its own entry", n)
			}
		}
		if count != 1 {
			t.Errorf("want exactly 1 named entry for io/fs.PathError, got %d (all: %v)", count, names)
		}
	})
}
