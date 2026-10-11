# BUG-MODPROBE-011: fmt String() dispatch fails on disk-imported named types

**Status:** FIXED (2026-10-12)
**Worktree:** `m9-uuid011` (branch `wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/m9-uuid011`)
**Base:** merge of `wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/m9-uuidnpe` at `037af0c`

## Symptom

`fmt` printing of a `String()` method on a disk-imported (module) type silently
falls through to structural printing. In the uuid probe:

- case04: `fmt.Println(v1.Version(), v1.Variant())` printed `1 RFC4122`
  instead of `VERSION_1 RFC4122`
- case07: same class of failure

Local (non-disk) types with `String()` worked. The method table IS present in
the export JSON (type 4663 `named github.com/google/uuid.Version` carries
`String` obj 2236), but `IfaceMethodTable.lookup` missed at runtime.

## Root cause

A **naming-convention mismatch** between the method-table install side and the
fmt lookup side, specific to disk-imported types.

- **Install side** (`IfaceMethodTables.resolve` →
  `DescriptorNames.descriptorName` → `NodeFactory.displayNameFor`):
  disk named types are keyed by **display name**, e.g. `uuid.Version`
  (package-NAME form, like gc's `%T`).
- **Lookup side** (`FmtHandleMethods.handleForType` ← `Fmt.Arg.typeName()`
  ← `GoTypeStrings.of`): the `named` case returned the **raw wire name**,
  e.g. `github.com/google/uuid.Version` (full module path).

`lookup("github.com/google/uuid.Version", "String")` missed the
`uuid.Version` entry → fell through to underlying-byte printing → `1`.

Local types worked only because their wire names contain no slash, so
`displayNameFor` is the identity for them — the bug was invisible until a
real module (with a path) was imported.

Two adjacent gaps found while verifying the fix (same `FmtHandleMethods`
area, general not disk-specific):

1. **Pointer → value receiver**: `adaptReceiver` did not do gc's implicit
   `(*p)` dereference, so `fmt.Println(&v)` on a `*Version` (value-receiver
   `String`) threw inside the callee and fell through to `0x0`.
2. **Verb not applied to method result**: the `String()`/`Error()` result was
   written raw for all verbs; Go applies the verb to the returned string, so
   `%q` must quote (`"VERSION_1"`) and `%x`/`%X` hex-encode.

## Fix (general, in frontend/runtime — no module-specific code)

1. [`phases/phase1/src/main/java/graalgo/frontend/GoTypeStrings.java`](../../../phase1/src/main/java/graalgo/frontend/GoTypeStrings.java)
   - `of()` `named` case now returns `NodeFactory.displayNameFor(...)`
     instead of the raw wire name — matching the install-side key and gc's
     `%T` rendering.
   - `pointer` case uses a new private `rawOf()` helper (old raw-wire
     behavior) for the element, preserving the established
     `"*"+qualifiedName` install-key convention for pointer-to-named
     (dbd728c) — deliberately NOT changed (see "Known follow-up" below).
   - `slice`/`array`/`map`/`chan` keep recursing via `of()` (display form is
     gc-exact there; they are not method-table keys).

2. [`phases/phase3/src/main/java/graalgo/iface/FmtHandleMethods.java`](../../src/main/java/graalgo/iface/FmtHandleMethods.java)
   - `adaptReceiver`: dereference `GoPointer` → value when the target has a
     value receiver (`!target.pointerReceiver()`); nil pointer → fall through
     to structural printing.
   - `handleForType`: route `String()`/`Error()` results through
     `printer.formatMethodResult(bytes, verb)` so `%q`/`%x`/`%X` apply to the
     returned string (Go semantics).

3. [`phases/phase1/src/main/java/graalgo/runtime/Fmt.java`](../../../phase1/src/main/java/graalgo/runtime/Fmt.java)
   - New `Printer.formatMethodResult(byte[], int)` delegating to the existing
     `fmtString` verb dispatcher.

## Evidence

### Battery (6 programs, gc 1.27.1 vs graalgo, gmp=1 AND gmp=4, byte-identical)

In [battery/](battery/):

| Program | What it covers | Result |
|---|---|---|
| b01_disk_string | disk `Version.String()` via `fmt.Println`, direct call, interface boxing, `Variant()` | PASS |
| b02_disk_ptr | pointer to disk type, value-receiver `String` promotes | PASS |
| b03_disk_verbs | `%v %s %T %#v %q` on disk type | PASS |
| b04_disk_error | disk error type via `fmt` | PASS |
| b05_local_string | local `String()`/`Error()` + pointer-receiver regression guard | PASS |
| b06_direct_iface | direct call + interface-param passing | PASS |

Runner: `/tmp/011probe/run-one.sh` (kept on the VM; battery sources are the
artifact). Note: the synthetic `fmt` package only exposes the seven D4
functions, so the battery declares its own local `Stringer` interface —
a pre-existing synthetic-package limitation, not a product bug.

### uuid probe cases

`run_module.sh` on `modprobe/uuid`: **case04 PASS, case07 PASS**
(`== 2 passed, 0 failed ==`).

### Regressions

- Phase1 unit tests: run via `make test` in `phases/phase1`; compared
  against the merge-base run — 0 new failures (the 7 failures reported by
  the uuidnpe worker are pre-existing; see below).
- arrdispatch 89-line battery: re-run, byte-identical.

### Pre-existing failures (verified, not caused by this fix)

The uuidnpe worker reported 7 phase1 test failures (synthetic package scope
errors, `bytes.Buffer.Read` multi-value). Verified pre-existing by running
the same tests at the merge-base (`037af0c`) without this change: identical
7 failures.

## Known follow-up (separate bug, NOT fixed here)

`%T` of a pointer-to-disk-named type prints the full path
(`*github.com/google/uuid.Version`) instead of gc's `*uuid.Version`.
The `"*"+qualifiedName` convention is load-bearing (runtime descriptors in
`NodeFactory.typeDescriptorFor`, install keys in `DescriptorNames`,
`dynamicMethodSets`, pinned by `DescriptorNamesTest`) — changing it is a
6-site refactor with cache-key collision risk (two modules can share a
`pkg.Type` display name). Filed separately; out of scope for 011.

## Reproducer (minimal)

```go
package main

import (
	"fmt"

	"github.com/google/uuid"
)

func main() {
	v := uuid.Version(1)
	fmt.Println(v) // was: 1 ; now: VERSION_1
}
```
