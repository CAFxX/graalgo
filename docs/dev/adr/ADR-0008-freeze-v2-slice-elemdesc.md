# ADR-0008 — Freeze v1→v2: `elemDesc` parameter on frozen `makeslice`/`growslice`

**Date:** 2026-10-13
**Status:** accepted 2026-10-13 — retroactive recording of a signature
change that landed without the freeze process (finding F-1, m10-doc-audit
lane closeout §4.1/§5 — on that lane's unmerged branch). The v2 delta is
exactly the two slice-allocation signatures below; nothing else in the
freeze changes.
**Scope:** [phases/phase1/INTERFACE_FREEZE.md](../../phase1/INTERFACE_FREEZE.md) v1 → v2;
[phases/phase1/docs/freeze-stub.md](../../phase1/docs/freeze-stub.md) GG-0020 rows;
[FreezeSignatureTest](../../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java) frozen list → v2 signatures.
**Normative basis:** [phases/phase1/INTERFACE_FREEZE.md](../../phase1/INTERFACE_FREEZE.md) (v1, in force)
§§1, 3.4, 5; [phases/phase1/docs/freeze-stub.md](../../phase1/docs/freeze-stub.md) §"GG-0020 intrinsic
surface"; P3-M9 compress commit `f4915c2`
(`f4915c24cff912b577c998f153968d639284ab8a`, 2026-10-11).

## Context

1. **The change.** The P3-M9 compress lane changed two frozen
   `Intrinsics` signatures to thread the element
   `GoStructDescriptor` into slice allocation:
   - `makeslice(GoKind,int,int)` →
     `makeslice(GoKind,GoStructDescriptor,int,int)`
   - `growslice(GoSlice,GoKind,int,long,boolean)` →
     `growslice(GoSlice,GoKind,GoStructDescriptor,int,long,boolean)`
2. **Why it is needed (not gratuitous).** Root cause
   (`f4915c2` message): `Intrinsics.newTypedArray` allocated
   `Object[len]` for struct elements without initializing slots. Go's
   `make([]T, n)` zero-initializes; a null slot NPE'd on `s[i].field`
   access (bzip2's `pairs[i].value = uint16(i)` in `newHuffmanTree` →
   nil-pointer-dereference panic under graalgo, gc fine). The fix
   threads the descriptor `MakeNode`/`AppendNode` already hold (from
   `buildMakeBuiltin`/`buildAppendBuiltin`) into
   `makeslice`/`growslice`/`newTypedArray`; struct slots are pre-filled
   with `GoStruct.zero(desc)`. Verified: the minimal struct-slice-field
   reproducer and bzip2 hello-block decompression match gc. The
   descriptor is an inherent allocation input for struct kinds —
   alongside the already-frozen `elemKind`, `elemSizeBytes`,
   `elemHasPointers` — not a convenience parameter.
3. **The process violation.** The lane changed FROZEN signatures with
   no ADR and no freeze version bump (freeze policy §1 violation).
   `FreezeSignatureTest.testFrozenSignatures` is RED on master
   `855f8b03`: empirically verified 2026-10-13 (compiled master's
   `Intrinsics.class`, ran the test's exact reflection checks —
   `makeslice(GoKind,int,int)` MISSING, actual
   `makeslice(GoKind,GoStructDescriptor,int,int)`;
   `growslice(GoSlice,GoKind,int,long,boolean)` MISSING, actual
   `growslice(GoSlice,GoKind,GoStructDescriptor,int,long,boolean)`).
   The `INTERFACE_FREEZE.md` v1 doc is correct as written — the code
   drifted, not the doc.

## Decision

### D1. Record the change through the freeze process: freeze v1 → v2

- `INTERFACE_FREEZE.md`: version v1 → v2; §3.4 names the two v2
  signatures explicitly; §5 version table updated.
- [docs/freeze-stub.md](../../phase1/docs/freeze-stub.md): the two GG-0020 rows updated to the v2
  signatures (historical "freeze v1 (frozen 2026-10-04)" line kept;
  v2 amendment noted).
- [FreezeSignatureTest](../../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java): the two `FROZEN` entries updated to the v2
  parameter lists; version references v1 → v2.
- Unchanged and still frozen: the 3-arg `growslice(GoSlice,GoKind,int)`
  convenience overload and every other frozen signature.

### D2. Semantics of `elemDesc` (part of the v2 contract)

`elemDesc` may be null for non-struct kinds. For `STRUCT` element kinds
the caller MUST supply the element type's `GoStructDescriptor`; the
allocator zero-initializes every allocated slot with
`GoStruct.zero(desc)` (both the `makeslice` array and `growslice`'s
fresh growth region). In-tree callers: `MakeNode` (from
`buildMakeBuiltin`) and `AppendNode` (from `buildAppendBuiltin`).

### D3. Honest §1(c) accounting

Freeze §1 requires the §6 machine check to fail *before* a frozen
change lands. That ordering was violated here and cannot be recovered
post-hoc ("No mechanism records (c) post-hoc — the ADR is the
record"). The record is: the change landed in `f4915c2` without the
process; [FreezeSignatureTest](../../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java) caught it — red on master `855f8b03`
(F-1). The red test on master IS the evidence that the check guards
the surface. This ADR closes the process gap; it does not rewrite the
ordering.

## Alternatives considered (and rejected)

- **(a) Revert the signatures, drop `elemDesc`.** Rejected —
  reintroduces the struct-slot NPE (the bzip2 reproducer fails again).
  A real correctness fix, verified vs gc, is not reverted to satisfy
  paperwork.
- **(b) Keep the frozen signatures as null-delegating wrappers and add
  new overloads for the struct path.** Rejected — the frozen 3-param
  `makeslice` / 5-param `growslice` would then silently produce corrupt
  struct slices (null slots, later NPE on field access): a correctness
  trap sitting on a FROZEN cross-component contract. Worse than the
  drift it would paper over.
- **(c) Revert the signatures and zero-fill at the call sites.**
  Rejected — splits the allocator invariant ("a struct-kind slice has
  zero-valued slots") across the boundary: every present and future
  caller must remember, and `growslice`'s internal growth region cannot
  be filled by callers without exposing the backing-array layout.
  Design regression; the intrinsic exists to encapsulate exactly this.
- **(d) A `GoKind`→descriptor registry inside `Intrinsics`.**
  Rejected — `GoKind` is an enum shared across all struct types;
  descriptors are per-type. Global mutable state as a smuggled
  allocation input is the wrong abstraction.

## Verification

- [FreezeSignatureTest](../../../internal/backend/truffle/src/test/java/graalgo/runtime/FreezeSignatureTest.java) updated to the v2 list; red-green: the v1 list
  fails against the v2 code (observed on master `855f8b03`), the v2
  list passes.
- Full Phase-1 suite green (`make -C phases/phase1 test`).
- [requirements/GG-0020.yaml](../../../requirements/GG-0020.yaml) history entry appended.
