# P3-M4 Red-Green Evidence (GG-0020)

Every new semantic landed red-green-first vs gc 1.27.1. Each entry:
the RED (failing output before the fix), the fix, the GREEN (passing
output after). Mutations were verified to genuinely fail — no
tautologies.

## 0. Remediation red-greens (2026-10-06 review findings)

### R1. Atomic pointer aliasing (P0-1)

**RED** — `p:=new(atomic.Int64); setIt(p); p.Load()` where
`func setIt(p *atomic.Int64){p.Store(9)}`:

```
gc:       9
graalgo:  0
```

and `p:=new(atomic.Int64); q:=p; q.Store(5); p.Load()`:

```
gc:       5
graalgo:  0
```

`Boxing.copyForAssign` and `GoFuncRootNode` cloned `AtomicLong`/
`AtomicInteger` unconditionally, severing the aliasing: the D5 carrier
of a `*atomic.Int64` IS the same cell object, and a store through one
pointer must be visible through every alias.

Fix: gate both clone sites on `kind == GoKind.STRUCT` (mirroring the
`GoStruct` branch directly above): struct-kind (by-value) copies still
clone — `b:=a; b.Store(7)` leaves `a` alone, like gc — while
POINTER-kind carriers pass the cell through by reference.

### R2. `fastrandn(0)` (P1-1)

**RED** — `fastrandn(0)` via `//go:linkname fastrandn runtime.fastrandn`:

```
gc:       fastrandn(0) = 0, exit 0
graalgo:  graalgo-run: fatal internal error in goroutine:
          java.lang.IllegalArgumentException: fastrandn: n must be positive, got 0, exit 1
```

gc 1.27's `randn` has no positivity check; Lemire's multiply-shift
with n=0 yields 0. The guard turned a defined gc result into a fatal
exception.

Fix: drop the `n <= 0` guard (body change only); the multiply-shift
already returns 0 for n=0.

**GREEN** — new corpus `fr03_zero`: `fastrandn(0) = 0`, exit 0,
gc-identical.

### R3. Innermost runtime panic frame line (P1-2)

**RED** — nil-map write inside `helper()` (write at `main.go:6`,
`helper` defined at `main.go:5`):

```
gc:       helper() at main.go:6   (the panicking statement)
graalgo:  helper() at main.go:5   (the function definition line)
```

`IndexWriteNode`/`IndexNode` (and every other runtime panic site)
called `Panic.throwPanic(goValue)` with no throw-site node, so
`renderGoStack` fell back to the frame's funcdecl section.

Fix: pass `this` to the additive
`Panic.throwPanic(Object, Object, String, Node)` overload at every
runtime panic site in the frontend nodes (`BinaryOpNode`,
`CompositeLitNode`, `IndexNode`, `IndexWriteNode`, `SliceExprNode`
(`checkBound` made an instance method), `SelectorNode`/`SelectorWriteNode`
(`deref` takes the site), `PanicNode`, `DeferNode`, `TypeAssertNode`,
`MakeNode`, `FuncValueCallNode`) and the phase-3 nodes
(`SyncOpNode` — `panicOnMisuse` made an instance method,
`AtomicOpNode`, `IfaceDispatchNode` — `requireReceiver` made an
instance method). Deferred `panic(x)` also carries the site now:
`DeferNode` attaches itself via `DeferredCall.setCallSite` (gc
attributes the deferred panic to the defer statement line — verified).
Sites with no node available (phase-2 channel/timer runtime,
deferred-builtin dispatch internals) keep the old behavior; recorded
as known gaps.

**GREEN** — new corpus `mp04_nil_map_inner`: graalgo attributes
`helper()` to the panicking statement line, gc-identical. The map
harness now compares the FULL normalized stderr for panic programs
(P2-4), so a regression to the definition line fails the corpus.

## 1. Linkname arg/return boxing (`Linknames.callGo`)

**RED** — calling a linknamed Go helper with a concrete arg, then
returning a concrete value:

```
Exception in thread "main" java.lang.ClassCastException:
    class graalgo.values.GoInterface cannot be cast to class java.lang.Long
    at graalgo.frontend.LinknameRootNode.execute(...)
```

The call-site adaptation boxes concrete args as `GoInterface`, but
`LinknameRootNode` passed them through to a function expecting a raw
`long`. Fixed arg unwrapping; then the *return* failed the other way:

```
Exception in thread "main" java.lang.ClassCastException:
    class java.lang.Long cannot be cast to class graalgo.values.GoInterface
```

The declared `any` result must be boxed. Fix: `LinknameRootNode`
`boxResult` flag (computed by `NodeFactory` from the declared result
type); `GoFuncRootNode.wrapInterface` made package-visible.

**GREEN** — `callGo("main.helper", int64(21)).(int64)` prints `42`.

## 2. Delete on nil map (`DeleteNode`)

**RED**:

```
Exception in thread "main" java.lang.NullPointerException:
    Cannot invoke "graalgo.values.GoMap.remove(Object)" because "map" is null
    at graalgo.frontend.DeleteNode.executeVoid(...)
```

gc: `delete(nilMap, k)` is a no-op. Fix: nil check → no-op.

**GREEN** — `mp03_nil_map`: read/delete on nil map print `0 0`,
`false`, `read-delete ok`; write panics
`panic: assignment to entry in nil map` (message-identical to gc).

## 3. Struct slice append growth (`Intrinsics.structSizeBytes`)

**RED** — appending to `[]Point` (a struct slice):

```
Exception in thread "main" java.lang.UnsupportedOperationException:
    struct element size not implemented
    at graalgo.runtime.Intrinsics.structSizeBytes(...)
```

`AppendNode` had no element size for struct types, so growth
crashed. Fix: flat-struct ABI layout computation; nested
struct/array element sizes fail fast (documented M4 boundary).

**GREEN** — `sl01_elem_kinds`: struct append/copy values verified,
growth caps match gc.

## 4. Append growth caps vs gc (`AppendNode` / `Intrinsics`)

**RED** — naive doubling gave `1 2 4 8 16 …` for int appends; gc's
`growslice` gives `1 2 4 4 8 8 8 16 16 32 …` (the 2×→1.25× transition
at 256 elements, plus size-class rounding). Byte-identical mismatch on
`sl00_growth`.

Fix: replicate gc's `growslice` formula (size classes, 2× below 256
elements, 1.25× above, then round up to size class).

**GREEN** — `sl00_growth` byte-identical caps for ints and bytes,
appends 1..32.

## 5. Traceback line numbers (`GoStackFrame` / `FuncValueCallNode`)

**RED** — graalgo reported the callee's *definition* line; gc reports
the *call site*:

```
gc:       tb01_nested.go:8    (middle's frame)
graalgo:  tb01_nested.go:7    (middle's def line)
```

Off by one on every caller frame. Fix: `FuncValueCallNode` updates
the caller's top frame section to the call site before invoking;
`GoStackFrame.section` mutable; Phase-2 `GoroutineState` overlay gets
the same method (P2-D8 shadowing — without it, `NoSuchMethodError`).

**GREEN** — `tb01_nested`: `inner:4, middle:8, outer:12, main:16`,
exactly gc's lines. Traceback corpus 4/4 vs gc.

## 6. Atomic cell value semantics (`Boxing.copyForAssign`)

**RED** — `b := a; b.Store(7)` then `a.Load()` printed `7` (shared
cell); gc prints `0` (Go value copy). Similarly, by-value atomic
params shared the caller's cell.

Fix: `copyForAssign` clones `AtomicLong`/`AtomicInteger`;
`GoFuncRootNode` clones by-value atomic params (interface boxing
already clones via `copyForAssign`).

**GREEN** — `at02_types`: gc prints `0 7` for the copy case;
graalgo matches.
