# BUG-MODPROBE-009 — package-init NPE: var-init dependency analysis blind to disk multi-result stubs

**Date:** 2026-10-11
**Lane:** m9-uuidnpe (worktree `wt/f7467b85-7da6-4da0-b9ec-94631a60dafe/m9-uuidnpe`)
**Status:** FIXED (general) — commit 25-line fix in `phases/phase1/frontend/synth_machinery.go`
**Base:** m9-arrdispatch @ efc88ee + m9-modprobe @ f5a0e82 + m9-mathbig @ 07c2731 (merged)

## Symptom

Importing `github.com/google/uuid` v1.6.0 NPEs during package init, before main runs:

```go
package main
import "github.com/google/uuid"
var _ = uuid.Nil
func main() {}
```

```
graalgo-run: fatal internal error in goroutine: java.lang.NullPointerException
```

No output printed. Minimal and deterministic.

## Root cause

`orderVarInits` (phases/phase1/frontend/export.go) topologically sorts package-level
var initializers so each runs after the vars it depends on, following dependencies
transitively through called package-level functions (`varDepAnalyzer.scanFunc`).

The disk multi-result transformation (`transformDiskMultiResult`, mr_disk.go) splits
each multi-result func `F` into a `__F` helper (real body) + `F` stub
(`{ panic("unreachable") }`). The stub is skipped from `g.fis`, so `depFuncs`
contains only the helper. But var initializers reference the STUB name — the
analyzer resolved `Parse` to the stub's `*types.Func`, found no `depFuncs` entry,
and concluded it had no dependencies.

Concrete chain in uuid: `NameSpaceDNS = Must(Parse("6ba7b810-..."))` (hash.go) must
run after `xvalues` (util.go), which `Parse` reaches via `xtob`. The analyzer saw
only the stub's panic body, ordered `NameSpaceDNS` before `xvalues`, and at runtime
`xvalues` was still null when `Parse` indexed it → NPE in `IndexNode.read`
(ARRAY case, `Array.getLength(null)`).

## Fix (general, not uuid-specific)

In `importSyntheticPackage` (phases/phase1/frontend/synth_machinery.go), after
building `depFuncs`, map each disk-MR stub's `*types.Func` (via
`e.info.Defs[spec.stub.Name]`) to its helper's `*ast.FuncDecl` (found in `g.fis`
by helper name). The dependency analyzer then transparently follows the real
implementation body. Interface-method specs (no helper/stub) are skipped.

This is general: any disk-imported package whose var initializers call
multi-result functions with transitive var dependencies was misordered.

## Verification

- Minimal reproducer: exit 0, no NPE (was: fatal NPE). ✅
- Extended smoke (`uuid.Nil`, `uuid.Max`, `uuid.NameSpaceDNS`, `MustParse`): all correct. ✅
- uuid 8-case battery vs gc 1.27.1: **4/8 PASS** (cases 01, 02, 05, 08). The 4 failures
  are THREE SEPARATE BUGS exposed by the NPE fix (not regressions):
  
### BUG-MODPROBE-010 (case06): multi-result call as function argument not unpacked
`uuid.Must(mustText(...))` where `mustText` returns `(UUID, error)`. The exporter
does not unpack the pair into separate args → `ArrayIndexOutOfBoundsException:
Index 2 out of bounds for length 2` at `GoFuncRootNode.execute:119`.
Root cause: `exportCallArgs` does not handle the Go special case where a
multi-result call is the sole argument (results bind to params in order).
Requires new desugar logic with temp-var hoisting.

### BUG-MODPROBE-011 (case04, case07): fmt.Stringer dispatch fails for disk named types
`v1.Version()` prints as `1` instead of `VERSION_1`. The method table IS in the
export JSON (type 4663 `github.com/google/uuid.Version` has `String` method obj 2236,
funcdecl exists), but `IfaceMethodTable.lookup(descName, "String")` in
`FmtHandleMethods` fails. Likely a runtime descriptor name mismatch or boxing issue.
Local named types work; disk-specific.

### BUG-MODPROBE-012 (case03): hang in UnmarshalText/String path
case03 (`UnmarshalText` → `String()` → `UnmarshalText`) produces no output and hits
the 300s timeout on both gmp legs. No error, no output — infinite loop or deadlock.
Needs thread-dump analysis. Not yet root-caused.

## Evidence

- Fix: `phases/phase1/frontend/synth_machinery.go` (25 lines, depFuncs stub→helper mapping)
- Reproducer: `/tmp/uuidnpe/min.go`
- Battery log: `/tmp/uuidnpe/battery.log` (4/8)
- Battery outputs: `/tmp/uuidnpe/battery/case0{1..8}.{gc,gr1,gr4}.{out,err}`
- case06 stack trace: `/tmp/uuidnpe/probe-case06.log` (GoFuncRootNode:119)

## Regression checks (pending)
- Phase1 unit tests: 0 failures expected (not yet run)
- arrdispatch 89-line battery: byte-identity expected (not yet run)
