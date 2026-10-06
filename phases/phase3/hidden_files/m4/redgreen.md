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

**GREEN** — new corpus `at03_atomic_pointers`: `9`, `5 5`, `7`
(interface-held pointer), `3 0` (by-value still clones); gc-identical,
gmp=1 and gmp=4. Existing `at02_types` (value form) still passes.

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
