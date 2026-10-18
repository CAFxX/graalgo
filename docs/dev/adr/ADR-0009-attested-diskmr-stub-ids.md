# ADR-0009 — Attested disk-MR stub obj ids: fail fast on unattested unknown method-table ids

**Date:** 2026-10-18
**Status:** accepted (design) — implementation in the `review3-p1-iface-silentdrop` lane
**Scope:** review finding P1-8 (independent codebase review 2026-10-18):
[`IfaceMethodTables.java`](../../../internal/backend/interop/src/main/java/graalgo/iface/IfaceMethodTables.java)
`resolve()` silently dropped any method-table entry whose `obj` id was
absent from `Program.functions()`, logging `P3Log.warn("... (skipped;
likely a disk-MR stub)")`.
**Normative basis:** the class javadoc's fail-fast contract ("a method
entry without a name/obj, or an obj id absent from
`Program.functions()`, is a `FrontendException` naming the descriptor,
method, and obj id") and
[`P3SyncMain.java`](../../../internal/backend/interop/src/main/java/graalgo/p3sync/launcher/P3SyncMain.java)
("a missing objID fails fast here") — both already document fail-fast as
the intended behavior; the warn+skip was the deviation.

## Context

1. **The legitimate case is real.** WS G (P3-M9) rewrites each
   multi-result `FuncDecl` in a disk package into a `__`-helper (real
   body) + a stub (`panic("unreachable")`, kept for type-checking and
   method resolution); see
   [`mr_disk.go`](../../../internal/frontend/mr_disk.go)
   (`transformDiskMultiResult`) and
   [`diskimport.go`](../../../internal/frontend/diskimport.go)
   (`buildDiskPackage`: "stubs are never exported"). The stub IS
   registered in `funcByObj` with a real obj id (assigned by `defFunc`),
   so
   [`export.go`](../../../internal/frontend/export.go)
   (`methodTableEntries`) writes the *stub's* id into the wire
   `"methods"` entry — but
   [`export.go`](../../../internal/frontend/export.go)
   (`exportFuncDecl`, the single emission choke point) deliberately
   emits no `funcdecl` for it. The id is therefore absent from
   `Program.functions()`: a *vestigial* entry.
2. **The masking.** The old code could not distinguish a vestigial stub
   id from a genuinely bogus id produced by an exporter bug — both got
   warn+skip. The method then vanished from the itab and the failure
   surfaced much later at dispatch as the misleading
   `method missing "..." (unreachable: go/types verified assignability)`
   ([`IfaceMethodTable.java`](../../../internal/backend/interop/src/main/java/graalgo/iface/IfaceMethodTable.java):223).
3. **Only the frontend knows.** Whether an id is vestigial is decided at
   exactly one place: `exportFuncDecl`'s disk-MR-stub skip branch. The
   backend was guessing ("likely a disk-MR stub"); guessing is the bug.

## Decision

### D1. The frontend attests the vestigial ids; the backend fails fast on anything else

- `exporter` gains `diskMRStubObjIDs map[int]bool`, populated **only**
  in `exportFuncDecl`'s disk-MR-stub skip branch (the single decision
  point that withholds emission; `fi.funcID` is assigned by then on both
  the main-package and synthetic/disk emission paths).
- [`Doc`](../../../internal/frontend/export.go) gains
  `DiskMRStubObjIDs []int \`json:"diskMRStubObjIDs"\``, filled at
  document assembly from the sorted set (sorted for determinism;
  normalized to `[]`, never `null`, to keep the wire shape stable).
- [`docs/arch/frontend-v1.md`](../../arch/frontend-v1.md) §2 documents the
  new top-level field (docs are part of the change).
- `IfaceMethodTables.resolve()` reads the optional `diskMRStubObjIDs`
  array (absent/null → empty set):
  - unknown id **in** the set → skip the entry silently (attested; the
    warn was the noise that masked real bugs — attested skips are
    expected, not warnings);
  - unknown id **not** in the set → `FrontendException` naming the
    descriptor, method, and obj id (the contract the javadoc already
    states).
- The class javadoc's fail-fast paragraph is updated to describe the
  attested-set behavior.

### D2. Alternatives rejected

- **Fail fast on every unknown id.** Breaks legitimate programs: any
  disk package with a multi-result method on an interface-boxed type
  (the entire WS G third-party-module path) emits vestigial entries.
  Rejected: the legitimate case is real and frequent.
- **Frontend stops emitting vestigial entries** (helper-map disk stubs
  in `methodTableEntries` like synthetic ones). That changes dispatch
  behavior — the itab would target the `__`-helper and interface
  dispatch of disk multi-result methods would start working instead of
  dropping the method. A larger behavioral change needing its own
  differential verification; out of scope. Recorded as a follow-up.

### D3. Explicitly out of scope

- [`ReflectTypes.java`](../../../internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectTypes.java):1077
  carries the identical warn+skip bodge for the reflect path. It can
  consume the same `diskMRStubObjIDs` wire field. Flagged as a
  follow-up remediation lane (P1-8 names `IfaceMethodTables` only).
- `removeDiskMRStubs`
  ([`mr_disk.go`](../../../internal/frontend/mr_disk.go):1977) is
  dead code (defined, never called outside tests — the stubs stay in
  the AST per `buildDiskPackage`'s "kept for method resolution"
  comment). Flagged as a P2 cleanup, not touched here.

## Verification

- Java
  ([`IfaceMethodTablesTest.java`](../../../internal/backend/interop/src/test/java/graalgo/iface/IfaceMethodTablesTest.java)):
  `testMissingObjIdFailsFast` (pre-existing; red against the old
  warn+skip code — the positive fail-fast proof), new
  `testAttestedDiskMRStubIdSkips` (stub-set id skips silently, no
  exception), new `testUnattestedIdFailsFastDespiteStubSet` (the set is
  not a blanket pass: any other unknown id still fails fast naming
  method and id). Run via `make test` in `internal/backend/interop`.
- Go (`internal/frontend`): `TestExportFuncDeclRecordsDiskMRStubObjID`
  — drives `exportFuncDecl` on a fabricated disk-MR stub `funcDeclInfo`
  and asserts the skip fires (nil node, nil error) and the obj id is
  recorded. Run via `go test` in `internal/frontend`.
- Wire gate: `tools/validate_frontend_v1.py` tolerates the new field
  (it only checks known fields); extended with a shape check (absent or
  array of non-negative ints) plus selftest cases.
- No behavior change for documents without disk-MR stubs
  (`diskMRStubObjIDs: []`); launch-time only (`resolve()` runs once at
  startup), so no steady-state performance impact.
