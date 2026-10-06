# ADR-0004 — P3-M4 freeze exceptions: linkname by symbol name, `sync/atomic` typed wrappers, itabFor/traceback wiring

**Date:** 2026-10-06
**Status:** accepted 2026-10-06 — P3-M4 implementation brief. Spec-first
ordering honored: ADR committed before any exporter/factory change.
**Scope:** closes PLAN.md §4 P3-M4 (WS C) items 3–5 (linkname both
directions, atomics corpus); GG-0020 →active.
**Normative basis:** `phases/phase1/INTERFACE_FREEZE.md` (v1, in force)
§§1, 2, 5; PLAN.md §§2–5 (WS C); SPEC §2.8 ("Linkname compatibility is by
symbol name, not gc ABI (GG-0021)"); ADR-0002/ADR-0003 (precedent).

## Context

P3-M4 (WS C) completes the GG-0020 intrinsic surface. Three items need
frozen-file changes:

1. **Linkname by symbol name.** The M4 corpora must call intrinsics from
   Go source differentially vs gc (fastrand statistical properties;
   traceback rendering). gc's mechanism is `//go:linkname localName
   symbol` on bodyless functions (requires `import _ "unsafe"`). The
   frozen exporter scope-errors bodyless functions (`func %s without
   body`) and rejects the `unsafe` import; the frozen `NodeFactory`
   builds every funcdecl from a body.
2. **`sync/atomic` for the contention corpus.** Go programs cannot name
   an `*int32`/`*int64` (`&x`/`*p` are WS D scope errors), so the
   package-level `atomic.AddInt64(&x, …)` API is inexpressible. gc's
   typed wrappers (`atomic.Int64` etc., go1.19+) have pointer-receiver
   methods on addressable values — exactly the shape ADR-0002 already
   binds for `sync.Mutex`.
3. **`Intrinsics.itabFor` / `traceback`.** The two remaining phase-1
   stubs (audit `hidden_files/m4/intrinsic-audit.md` §2c). `itabFor`
   must delegate to the P3-M3 itab machinery without phase-1 depending
   on phase-3; `traceback` renders `GoroutineState` stacks.

## Decision

A single documented exception, following the ADR-0002/ADR-0003
precedent (additive wire data via public hooks; toolVersion bump per
freeze §5; red-green evidence per change):

### D1. `//go:linkname` — exporter + wire + factory

1. **Exporter** (`export.go`): for each top-level `*ast.FuncDecl` with no
   body, scan its doc comment for `//go:linkname <local> <symbol>`
   (exact gc spelling; the local name must equal the declared name —
   mismatch is a scope error). A bodyless func WITH a valid directive
   emits its funcdecl with the additive field `"linkname": "<symbol>"`;
   a bodyless func WITHOUT one keeps the existing scope error. Linkname
   on methods or generic functions is a named scope error (out of M4
   scope).
2. **Import gate**: `import _ "unsafe"` is allowed (blank only — a
   non-blank `unsafe` import is a named scope error, since selections on
   it cannot resolve). `overlayImporter` synthesizes an empty `unsafe`
   package (blank imports only need the package to exist for go/types).
3. **Wire**: `"linkname"` on funcdecl is additive (freeze §2.3); old
   consumers ignore unknown fields — a 0.5.0 document with linknames
   still parses, but the linked function has no body to build: the old
   factory's `buildStmt(fields.object("body", …))` fails fast on the
   missing field (loud, never silent).
4. **`NodeFactory`** (frozen — this ADR is the exception): in the
   `funcs` loop, a funcdecl carrying `"linkname"` builds a new
   `graalgo.frontend.LinknameRootNode(symbol, funcName)` instead of
   `buildNamedFunc`. The root's `execute` calls
   `Linknames.dispatch(symbol, args[1..])` (D5-boxed args, no adaptation;
   each registered target adapts explicitly). No Go stack frame is
   pushed (gc's assembly intrinsics are likewise invisible in traces).
   `Safepoint.poll()` runs on entry, matching `GoFuncRootNode`.
5. **`graalgo.runtime.Linknames`** (new phase-1 file, not previously
   frozen): the symbol registry —
   `register(symbol, Target, arity)`, `dispatch(symbol, args)` (unknown
   symbol or arity mismatch → `FrontendException`, fail-fast),
   `bindGo(qualifiedName, GoFunc)` / `resolveGo` /
   `callGo(qualifiedName, args)` (intrinsic→Go by symbol name, via the
   `GoCalls.invoke` protocol). `Target` is a plain
   `Object apply(Object[] args)` functional interface.
6. **Phase-3** (`graalgo.p3link`, new package): `LinknameIntrinsics.install(json,
   program)` called by `P3SyncMain` after `buildProgram`:
   - registers the M4 gc-symbol table —
     `runtime.fastrand`→`Intrinsics.fastrand`,
     `runtime.fastrandn`→`Intrinsics.fastrandn`,
     `runtime.nanotime`→`Timers.nanotime` — so the *same Go source*
     links against gc's runtime under gc and graalgo's intrinsics under
     graalgo ("by symbol name, not gc ABI");
   - registers `graalgo.runtime.Linknames.callGo` (the intrinsic→Go
     direction);
   - binds every program funcdecl under `package.name + "." + name`
     (from the doc) via `Linknames.bindGo`.
7. **Version:** `gofrontend` toolVersion **0.4.0 → 0.5.0** per freeze §5
   (bodyless+linkname previously a scope error; new wire field).

Out of scope (named scope errors, re-test triggers): linkname on
methods/generics; linkname *to* Go symbols from Go (`//go:linkname a
b` where both are Go — no intrinsic involved); `runtime.Stack`
(gc forbids the link: "invalid reference to runtime.Stack" — verified
2026-10-06) — traceback goes through the panic path instead (§D3).

### D2. Synthetic `sync/atomic` typed wrappers

Following the ADR-0002 pattern (synthetic package + exporter
interception + `syncop`/`syncnew` wire nodes + phase-3 nodes):

1. `overlayImporter` gains `case "sync/atomic"`:
   `syntheticAtomicPackage()` defines `Int64`, `Uint64`, `Int32`,
   `Uint32`, `Bool` (underlying `struct{}`, like the sync primitives)
   with **exact gc 1.27.1 signatures** (verified against
   `~/tools/go-1.27.1/src/sync/atomic/`): pointer-receiver
   `Load() T`, `Store(val T)`, `Add(delta T) T`, `Swap(new T) T`,
   `CompareAndSwap(old, new T) bool`. Package-level functions
   (`AddInt32`, …) and `Pointer[T]`/`Value` are *defined* with exact
   signatures and rejected with named `not in M4 scope` errors
   (package-level funcs need `&x` — WS D; `Pointer`/`Value` need
   interfaces/pointers).
2. Import gate allows `"sync/atomic"`.
3. `exportCall` interception generalizes: `syncPrimitiveRecv` OR
   `atomicPrimitiveRecv` (new helper, same shape); atomic method calls
   emit `syncop` with `synctyp: "atomic.Int64"` (etc.) via a new
   `exportAtomicOp` (arity/result checking against the atomic op table;
   out-of-scope methods → named M4 scope error). Defer/go desugars and
   the zero-value normalization (`var v atomic.Int64` → implicit
   `syncnew`) follow the sync paths; `SyncNewNode`/`SyncOpNode`
   (phase-3, not frozen) accept the `atomic.*` namespace.
4. **Java** (phase-3, additive): `AtomicOpNode` (new node with real
   result kinds — `SyncOpNode` returns null/`INTERFACE`) dispatching to
   `graalgo.runtime.Atomics` (the real phase-1 atomics:
   `AtomicLong`/`AtomicInteger` ops); the Go value IS the
   `AtomicLong`/`AtomicInteger` (ADR-0002 D1 value model). `SyncNewNode`
   allocates them.
5. **Differentiality:** the same Go source (`import "sync/atomic"`,
   typed wrappers only) runs under gc 1.27.1 against the real package
   and under graalgo against the overlay — CAS/Load/Store/Add/Swap +
   contention compared byte-for-byte.

### D3. itabFor + traceback

1. **`Intrinsics.itabFor`** (frozen signature kept; STUB bodies are
   INTERNAL per freeze §8 — the hook below is documented here as an
   additive exception in the ADR-0003 class): gains a private volatile
   provider
   `BiFunction<GoTypeDescriptor,GoTypeDescriptor,Object>` + public
   static `setItabProvider`. `itabFor` delegates; null provider →
   the old stub error (phase-1-only mode has no method tables).
   Phase-3: `IfaceMethodTable` gains `installInterfaces(Map<String,
   List<String>>)` (interface descriptor name → method names, populated
   by the launcher from the doc type table's interface entries) and
   public `itabForIntrinsic(descriptorName, ifaceName)` (stable iface id
   via an interned name→int map; delegates to the existing cached
   `itabFor`). `LinknameIntrinsics.install` registers the provider.
   Red-green: Java test asserting `Intrinsics.itabFor(d, i)` is
   *identical* to the dispatch path's itab for the same pair.
2. **`Intrinsics.traceback()`** (STUB body → real; INTERNAL, no
   signature change): renders the current goroutine's `GoStackFrame`
   chain via `GoroutineState.current()` (phase-2 ThreadLocal holder
   under phase-2/3 classpaths) in gc's single-goroutine shape
   (`goroutine 1 [running]:` + `name()` + `\tfile:line` per frame).
   The panic path (`Main.reportPanic`) is refactored to share the
   frame renderer (launcher-internal, not frozen). Differential vs gc
   panic tracebacks with normalization (goroutine IDs, `+0x` offsets,
   arg lists, file paths).

### D4. Write barrier

No code. Recorded in the audit (§4): no-op-by-design per P2 ADR-0002
D3; gc's `writeBarrier` is a linknamed *variable*, not a function.

## Alternatives considered (and rejected)

- **(a) Growing the synthetic `runtime` package with `Fastrand()` etc.:**
  rejected — invents non-gc API names, killing differentiality; the
  linkname symbol table keeps the Go source gc-identical.
- **(b) Atomics via linkname with `*int64` params:** rejected —
  `&x`/`*p` are WS D; no address vehicle exists. The typed wrappers are
  the only Go-expressible atomic surface today.
- **(c) Phase-3 shadowing of the whole `Intrinsics` class for itabFor:**
  rejected — would duplicate 17 real methods; the provider hook is
  one field + one setter.
- **(d) `//go:linkname` to `runtime.Stack` for the traceback corpus:**
  rejected — gc forbids the link ("invalid reference", verified);
  the panic path is the differential vehicle.

## Verification

- Red-green per semantic in `hidden_files/m4/redgreen.md`: linkname
  dispatch (unknown symbol fails pre/post), atomic CAS contention,
  itab identity, traceback normalization.
- New corpora `phases/phase3/corpus/{map,slice,atomic,fastrand,traceback}/`
  differential vs gc 1.27.1, 100%.
- Suites stay green: phase-1 208/208, phase-2 194/194, p3frontend,
  phase-3, iface 13/13, sync 25/25.
- `verifies: GG-0020` annotations; `requirements/GG-0020.yaml` history
  entry.
