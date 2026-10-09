# wave-3 `weak` corpus

Scope-limited corpus for `weak`. There is no synthetic adapter: a weak
pointer's contract ("once the object becomes unreachable, Pointer.Value
may return nil") is a runtime GC property. Emulating it with a strong
reference would be observably wrong (never nils), so the honest
representation is a scope limit, not an adapter.

- `00_weak.go`, `01_weak_types.go` are `.scopelimited`:
  `reason: needs weak-pointer runtime semantics in the GMP backend`,
  `retest: weak reference support lands in the backend`. The gc leg still
  builds and runs them, proving the programs are valid deterministic Go;
  the graalgo leg is skipped.
- Pins (for the retest): pointer-identity equality (not pointee equality),
  `Value` non-nil + correct while strongly reachable, nil-in/nil-out,
  field/element pointers, per-type behavior. Post-GC reclamation timing is
  deliberately NOT pinned (nondeterministic under any backend).
