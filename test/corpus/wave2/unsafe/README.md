# wave-2 `unsafe` differential corpus (P3-M8)

12 programs (9 differential, 2 trap pins, 2 scope-limited). Run with
`./run_differential.sh` (thin wrapper over `../run_wave.sh unsafe`).

Frontend design note: the synthetic `unsafe` package exposes go/types'
own `*types.Builtin` objects (shared from `types.Unsafe`), so operand
checking and Sizeof/Alignof/Offsetof constant-folding are gc's own — e.g.
`unsafe.Slice[byte](p,n)` is rejected and a constant negative len is a
compile error, exactly like gc. No AST pre-pass; no size tables.

## Differential programs (gc == graalgo, byte-compared)

1. `00_sizeof_table` — Sizeof/Alignof over padding structs, arrays,
   strings, slices, maps, chans, funcs, pointers, interfaces; const-position
   and array-length occurrences (folded by go/types, no pre-pass).
2. `01_offsetof` — field offsets incl. embedded structs, trailing fields,
   const/array-length positions, same-package unexported fields.
3. `02_pointer_roundtrip` — `*T→Pointer→*T`, interning `==` parity
   (`p1 == p2` for the same referent), nil round-trips.
3b. [`03b_ptr2uintptr_identity`](03b_ptr2uintptr_identity.go) —
   `unsafe.Pointer → uintptr` yields the handle as an integer identity
   token (PKG-007): nil → 0, stability (same referent → same token),
   distinctness, equality-as-identity, nil is the only zero token.
   Prints only portable properties (gc addresses and graalgo handles
   differ numerically).
4. `04_slicedata_roundtrip` — SliceData→Slice aliasing (writes visible via
   the original), sub-slice views differ as pointers but re-intern stably,
   nil slice → nil pointer.
5. `05_slice_shapes` — whole-array backing, single-T len-1, inferred
   element type (explicit `Slice[byte]` type args are rejected, as in gc),
   zero-length views.
6. `06_string_roundtrip` — StringData→String content equality, empty string
   → nil pointer (gc: `StringData("") == nil`), nil+0 → "".
7. `08_offsetof_deep` — Offsetof through 20 nested structs (go/types folds
   the constant; no depth cap in the type-checker).
8. `10_pointer_identity` — pointer identity (P3-M10 regression test):
   distinct `new(T)` allocations compare unequal, a pointer keeps its
   handle across `*p = v` writes, `*T→Pointer→*T` round trips preserve the
   referent for reads and writes, `&arr[i]` element pointers are distinct
   per index and stable per element, `new(map)` is a non-nil pointer.

## Intentional-divergence pins (NOT differential — see below)

8. [`03a_add_trap`](03a_add_trap.go) / [`03c_uintptr2ptr_trap`](03c_uintptr2ptr_trap.go) —
   the two remaining D5 traps, each with a `.panic` file pinning the exact first
   stderr line (`UnsafeEscapeException`, exit 1). gc implements both,
   so the gc leg exits 0: the exit code differs BY DESIGN. The `.panic`
   line and (empty) stdout are the assertions. Runner gap (see handoff):
   `run_wave.sh` compares exit codes unconditionally; these need a
   `.divergent` marker (skip exit comparison, still pin stdout+stderr).
   (`03b` was the third trap pin until PKG-007: `unsafe.Pointer → uintptr`
   now yields the handle as an integer identity token instead of trapping,
   so it moved to the differential list above.)
9. DIV-US-1 (`unsafe.String` copies): post-`String()` writes through the
   original pointer are invisible to the string on graalgo, visible under
   gc. Pinned by the Java unit checks (copy verified), not as a
   differential program — no differential formulation exists.
10. Over-long `unsafe.Slice` len and carrier-mismatched referents fail fast
    with Go panics (`unsafe.Slice: len out of range`); gc does not check.
    Pinned by the Java unit checks.

## Scope-limited programs

11. `07_sizeof_generic_scopelimited` — Sizeof over a type parameter is not
    a constant expression (go/types value mode); the frontend fails fast
    with a named M8 scope error instead of guessing a size.
    retest: sizes over type parameters.
12. `09_addrof_patterns_scopelimited` — `&s.field` / `&arr[i]` are WS D;
    the realistic `unsafe.Pointer(&x)` idiom cannot reach the exporter yet.
    retest: WS D (explicit address-of).

## Tmp discipline

Per `corpus/wave2/README.md`: never `/tmp`. The runner sets its own
`TMPDIR`/`GOCACHE`; ad-hoc go builds use
`TMPDIR=$HOME/workspace/tmp-wave2-unsafe`
`GOCACHE=$HOME/workspace/.gocache-wave2-unsafe`.
