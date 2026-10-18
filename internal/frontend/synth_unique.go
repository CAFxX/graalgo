// SYNTHETIC (Principle 4) — unique: real source interns via the runtime with weak references; synthetic uses a mutex-guarded table that never shrinks (documented D1 — unobservable except memory use).
// RETIRES WHEN: graalgo has runtime weak-reference interning for the real unique source.
// RE-TEST TRIGGER: weakref interning lands (B-4 phased removal: docs/arch/bridge-inventory.md).
// P3-M9 (tier-1d): synthetic `unique` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (see synth_ascii85.go for the pattern). The resulting
// *types.Package is what overlayImporter.Import("unique") returns, so the
// user's type-check and the synthetic source's own type-check share
// identical *types.Func/*types.TypeName objects; the exporter then emits
// the synthetic declarations' bodies into the same wire document via
// exportSyntheticPackages.
//
// Exact gc 1.27.1 public API: Handle[T comparable], Make[T comparable],
// (Handle[T]).Value. The canonicalization table is a package-level
// mutex-guarded map (real unique interns via the runtime with weak
// references; the table here never shrinks — unobservable to any program,
// only to memory usage).
//
// Key design: the map key is any(struct{ v T }{value}). The dynamic type
// of the key differs per instantiation of T, so values of different types
// never collide; within one T, key equality IS value equality (T is
// constrained to comparable). Equal values therefore map to the same
// canonical *T, and Handle equality (pointer comparison) matches gc.
//
// Deviations from gc (all unobservable except via deliberate abuse):
//   D1. gc's table uses weak references: entries for unreachable values
//       are reclaimed. Ours are retained forever (documented above).
//   D2. gc's Make for an uncomparable dynamic value (e.g. an interface
//       holding a slice) panics in the runtime map; ours panics in the
//       Go map insert with the same observable outcome (a panic).
//   D3. The mutex is *sync.Mutex (pointer), not a value field — the P3-M2
//       surface forbids value sync.Mutex in synthetic sources (synth_io.go
//       deviation D1/D2).
package main

// syntheticUniqueSource is the P3-M9 synthetic `unique` package: exact gc
// 1.27.1 API; real-Go canonicalization.
const syntheticUniqueSource = `package unique

import "sync"

// Handle is a canonicalized ("interned") handle for a comparable value.
// Two Handles compare equal iff they were made from equal values.
type Handle[T comparable] struct {
	// canonical points at the single interned copy. It is a pointer to a
	// one-field struct (rather than *T) because the exporter supports &
	// only on composite literals (WS D), not &x on an identifier.
	canonical *struct{ v T }
}

// uniqueMu guards uniqueTable (deviation D3: pointer, per the M2 rule).
var uniqueMu = &sync.Mutex{}

// uniqueTable maps the per-type key any(struct{ v T }{value}) to the
// canonical Handle[T]. Never shrinks (deviation D1).
var uniqueTable = map[any]any{}

// Make returns the canonical Handle for value: equal values yield equal
// Handles.
func Make[T comparable](value T) Handle[T] {
	uniqueMu.Lock()
	defer uniqueMu.Unlock()
	k := any(struct{ v T }{value})
	if h, ok := uniqueTable[k]; ok {
		return h.(Handle[T])
	}
	h := Handle[T]{canonical: &struct{ v T }{value}}
	uniqueTable[k] = h
	return h
}

// Value returns the canonical value the Handle was made from.
func (h Handle[T]) Value() T {
	return h.canonical.v
}
`
