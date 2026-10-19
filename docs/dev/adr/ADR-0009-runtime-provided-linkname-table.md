# ADR-0009 — Replace the `runtime_` prefix heuristic with an exact runtime-provided-symbol table (F-1)

**Date:** 2026-10-19
**Status:** full-table design proposed (not yet implemented); interim package-gate implemented with tests (this ADR §5)
**Scope:** [`internal/frontend/export.go`](../../../internal/frontend/export.go) `exportLinknameDecl`; future `tools/stdlib-fork/gen-runtime-provided-symbols.py` + generated `internal/frontend/zz_runtime_provided_gen.go`
**Finding:** independent codebase review REVIEW-2026-10-19-05 §1 F-1

## Context

1. **The defect.** `exportLinknameDecl`
   ([`internal/frontend/export.go`](../../../internal/frontend/export.go))
   emits `pkgpath.runtime_X` as a linkname symbol for *any* bodyless
   `func runtime_*` in *any* non-`runtime` package, on the uninventoried
   assumption that package runtime provides it:
   ```go
   if len(funcName) > 8 && funcName[:8] == "runtime_" && pkgPath != "runtime" {
   ```
   Name-keyed, untested. A user bodyless `func runtime_foo()` is silently
   misrouted: instead of the honest `unimplemented` stub (fail-loud at
   call time), it becomes a linkname stub for a fabricated symbol.
2. **Ground truth (go1.27.1 GOROOT, verified 2026-10-19).** The real Go
   pattern has two halves:
   - *Consumers* declare directive-less bodyless `func runtime_X(...)`
     ("defined in package runtime") — e.g. `src/sync/runtime.go`,
     `src/sync/atomic/value.go`, `src/internal/sync/runtime.go`.
   - *The provider inventory* is package runtime's **own** 2-field
     `//go:linkname <local> <P.runtime_X>` directives (e.g.
     `//go:linkname sync_runtime_Semacquire sync.runtime_Semacquire` in
     `src/runtime/sema.go`). This is the authoritative table of
     runtime-provided symbols — the same table gc links against.
     The full `runtime_*` consumer-symbol list is the appendix below
     (~60 symbols, `*.go` files only).
3. **graalgo-specific facts (verified).**
   - `sync`, `sync/atomic`, `internal/sync` compile from real GOROOT
     sources (absent from the synthetic registry in
     [`internal/frontend/synth_machinery.go`](../../../internal/frontend/synth_machinery.go))
     — the only packages where the heuristic is load-bearing.
   - `arena`, `weak` declare their `runtime_*` functions with their own
     1-field `//go:linkname` — already handled by `linknameDirective`;
     the heuristic never fires for them.
   - `os`, `syscall`, `internal/poll`, `unique`, `math/rand`,
     `math/rand/v2`, `net`, `hash/maphash`, `internal/bytealg`,
     `crypto/internal/boring` are synthetic with explicit `//go:linkname`
     directives in their synthetic sources.
   - Backend:
     [`internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java`](../../../internal/backend/interop/src/main/java/graalgo/p3link/LinknameIntrinsics.java)
     registers Java intrinsics under exact symbols;
     `Linknames.dispatch` fails fast on unknown symbols.
   - Known pre-existing backend coverage gaps (out of scope for this
     ADR; fail-fast at dispatch, never silent): `sync.runtime_canSpin`,
     `sync.runtime_doSpin`, `internal/sync.runtime_rand` have provider
     table entries but no registered intrinsic.

## Decision — full structural fix (PROPOSED)

1. **Generator** `tools/stdlib-fork/gen-runtime-provided-symbols.py`
   (next to
   [`tools/stdlib-fork/gen-asm-inventory.py`](../../../tools/stdlib-fork/gen-asm-inventory.py)):
   scans `$GOROOT/src/runtime/**/*.go` for `//go:linkname <local>
   <symbol>`, keeps consumer symbols matching
   `\.runtime_[A-Za-z0-9_]+$`, and emits a Go map literal into
   `internal/frontend/zz_runtime_provided_gen.go`
   (`// Code generated … DO NOT EDIT`, pinned Go version recorded).
2. **`exportLinknameDecl`** replaces the prefix check *and* any package
   check with one exact lookup:
   `if _, ok := runtimeProvidedSymbols[pkgPath+"."+funcName]; ok`.
   No name-keying remains: the table *is* the inventory, derived from
   the same GOROOT sources gc uses, so linkage matches gc by
   construction.
3. **Regeneration** on Go version bumps, alongside the asm inventory;
   the generator asserts the GOROOT version matches the pinned version.
4. **Optional hardening:** generator-time cross-check that every table
   symbol has a backend `Linknames.register` entry, or is recorded as a
   known fail-fast gap (linkage vs. implementation stay separate
   concerns; a gap fails loudly at dispatch, never silently).

## Interim step (IMPLEMENTED)

Until the generator lands, the prefix heuristic is gated to the three
stdlib packages actually known to need it (see §3 Context):

```go
var runtimeProvidedPackages = map[string]bool{
	"sync":          true,
	"sync/atomic":   true,
	"internal/sync": true,
}
```

A bodyless `runtime_*` anywhere else falls through to `unimplemented`.
Tests in
[`internal/frontend/linkname_export_test.go`](../../../internal/frontend/linkname_export_test.go):
- positive: real `sync` package's `runtime_Semacquire` routes to
  `"linkname": "sync.runtime_Semacquire"`;
- negative: user-package bodyless `func runtime_foo()` is
  `unimplemented`, never `main.runtime_foo`.

The interim gate is strictly narrower than the old heuristic (3 known
stdlib packages vs. every package on earth) and is deleted when the
generated table lands.

## Consequences

- User bodyless `runtime_*` → honest `unimplemented` stub, never a
  fabricated linkname.
- The full table removes the last name-keyed dispatch in this path;
  linkage decisions become exact-symbol matches against a
  version-pinned, regenerable inventory.
- Backend coverage gaps (§3.4) become visible at generator time instead
  of first call.

## Appendix — go1.27.1 runtime-provided `runtime_*` consumer symbols

From `//go:linkname` directives in `$GOROOT/src/runtime/*.go`
(`*.go` files only; `HACKING.md` documentation examples excluded):

```
arena.runtime_arena_arena_Free
arena.runtime_arena_arena_New
arena.runtime_arena_arena_Slice
arena.runtime_arena_heapify
arena.runtime_arena_newArena
crypto/internal/boring.runtime_arg0
internal/poll.runtime_Semacquire
internal/poll.runtime_Semrelease
internal/poll.runtime_ignoreHangup
internal/poll.runtime_isPollServerDescriptor
internal/poll.runtime_pollClose
internal/poll.runtime_pollOpen
internal/poll.runtime_pollReset
internal/poll.runtime_pollServerInit
internal/poll.runtime_pollSetDeadline
internal/poll.runtime_pollUnblock
internal/poll.runtime_pollWait
internal/poll.runtime_pollWaitCanceled
internal/poll.runtime_unignoreHangup
internal/sync.runtime_SemacquireMutex
internal/sync.runtime_Semrelease
internal/sync.runtime_canSpin
internal/sync.runtime_doSpin
internal/sync.runtime_nanotime
os.runtime_args
os.runtime_beforeExit
runtime/metrics.runtime_readMetrics
runtime/pprof.runtime_FrameStartLine
runtime/pprof.runtime_FrameSymbolName
runtime/pprof.runtime_cyclesPerSecond
runtime/pprof.runtime_expandFinalInlineFrame
runtime/pprof.runtime_getProfLabel
runtime/pprof.runtime_goroutineLeakGC
runtime/pprof.runtime_goroutineleakcount
runtime/pprof.runtime_setProfLabel
runtime/trace.runtime_traceClockUnitsPerSecond
sync.runtime_Semacquire
sync.runtime_SemacquireRWMutex
sync.runtime_SemacquireRWMutexR
sync.runtime_SemacquireWaitGroup
sync.runtime_Semrelease
sync.runtime_canSpin
sync.runtime_doSpin
sync.runtime_notifyListAdd
sync.runtime_notifyListCheck
sync.runtime_notifyListNotifyAll
sync.runtime_notifyListNotifyOne
sync.runtime_notifyListWait
sync.runtime_procPin
sync.runtime_procUnpin
sync.runtime_registerPoolCleanup
sync/atomic.runtime_procPin
sync/atomic.runtime_procUnpin
syscall.runtime_AfterExec
syscall.runtime_AfterFork
syscall.runtime_AfterForkInChild
syscall.runtime_BeforeExec
syscall.runtime_BeforeFork
syscall.runtime_doAllThreadsSyscall
syscall.runtime_envs
unique.runtime_blockUntilEmptyCleanupQueue
weak.runtime_makeStrongFromWeak
weak.runtime_registerWeakPointer
```

(`runtime/metrics_test.*` and `sync_test.*` test-only symbols omitted;
they reappear automatically if the generator includes test files.)
