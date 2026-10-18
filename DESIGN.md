# R23-P1-22 — Design: pointer-receiver adaptation in `methodVal`

**Issue:** `methodVal` skips the pointer-receiver adaptation its own comment promises (P1, correctness).
**Scope:** design only. The implementation lane builds exactly this.
**Base:** master tip `56a3dd68`; all line numbers below verified against this worktree.

---

## 1. Exact failure point

**File:** `internal/backend/interop/src/main/java/graalgo/p3reflect/ReflectOpNode.java`,
`methodVal` at lines 1285–1296:

```java
private static GoStruct methodVal(GoStruct v, int i) {
    ReflectValues.ValueParts p = ReflectValues.parts(v);
    TypeInfo t = ReflectTypes.table().info(p.typ());
    ReflectTypes.MethodInfo mi = t.methods.get(i);
    Object receiver = unboxValue(v);
    // Pointer-receiver adaptation: if the method needs a pointer but
    // the value is addressable, wrap it.
    long handle = ReflectRefs.intern(
            new ReflectCalls.BoundMethodVal(mi, receiver));
    ...
}
```

`mi.pointerReceiver` is populated from the wire (`"pointer".equals(recv)` at
`ReflectTypes.java:1098`, stored into `MethodInfo` at `ReflectTypes.java:1140`)
but is **never read** in `methodVal`. The raw D5 receiver is interned as-is.

**Current behavior:** for `reflect.ValueOf(&x).Elem().Method(i).Call(nil)` where the
method has a pointer receiver, the bound method value's receiver is a `GoStruct`.
At call time `ReflectCalls.invokeCallable` (`ReflectCalls.java:71-76`) prepends
that `GoStruct` as the receiver argument to the compiled method body, which
expects `*T` — a `GoPointer`. Any body that indexes/slices through the receiver
hits the hard `(GoPointer)` casts in `IndexNode.java:95`, `IndexWriteNode.java:71`,
`SliceExprNode.java:120` → `ClassCastException` host crash (not a Go panic).

**gc behavior:** `v.Method(i)` on an addressable `v` whose method needs a pointer
receiver captures `&v` — a pointer to `v`'s storage — as the bound receiver. The
call proceeds normally, and mutations through the pointer receiver are visible
through `v` afterwards. If `v` is **not** addressable, gc panics from
`reflect.Value.Method` instead of forming the method value.

**Non-reflect path (the parity target):** for a direct `x.M()` call on an
addressable `x`, the exporter inserts an implicit `addrof` node
(`NodeFactory.buildAddrOf`, `NodeFactory.java:3088`), which for a boxed local
produces `BoxAddrNode` → `new GoPointer(GoBox)` (`BoxAddrNode.java:57`) — a
**cell pointer addressing the variable's storage**, so reads see the current
value and writes through the pointer are observable through the variable.

---

## 2. Proposed adaptation mechanism

### 2.1 Where the wrap happens

In `methodVal` (`ReflectOpNode.java:1285`), replacing the dead comment with the
real adaptation, immediately after `Object receiver = unboxValue(v);`:

```java
Object receiver = unboxValue(v);
if (mi.pointerReceiver && receiver != null && !(receiver instanceof GoPointer)) {
    if ((p.flag() & ReflectValues.FLAG_ADDR) == 0) {
        throw new IllegalStateException(
            "reflect: reflect.Value.Method on non-addressable value");
    }
    receiver = adaptPointerReceiver(p.ptr(), receiver);
}
```

Notes on the condition:

- `mi.pointerReceiver` — the wire-provided bit the current code ignores.
- `!(receiver instanceof GoPointer)` — a `*T` Value (e.g. `reflect.ValueOf(&x)`
  without `.Elem()`, or a nil `*T`) already denotes a `GoPointer`; gc passes the
  pointer through unchanged, so we do too. This also preserves today's behavior
  for the already-working pointer case.
- `receiver != null` — a nil pointer receiver (Java null from `denotedValue`)
  keeps today's pass-through; the body's existing nil checks
  (`IndexNode.java:96`, etc.) then raise gc's nil-dereference panic as before.
- `FLAG_ADDR` (`ReflectValues.java:43`, `1L << 8`) is the addressability bit.
  `reflectElem` sets it on `Elem()` results of pointers (`ReflectOpNode.java:750`),
  mirroring gc's `flagAddr`. `reflect.ValueOf(x)` (non-pointer) has flag `0` →
  panic, matching gc. The panic uses this file's existing misuse convention
  (`IllegalStateException("reflect: ...")`, cf. `reflectElem`/`reflectLen`);
  the implementation lane must pin the exact text in the corpus `.panic` file
  (see §4) — gc parity of the *fact* of the panic is what matters here.

### 2.2 GoPointer construction — storage-aware, not snapshot

The naive `new GoPointer(receiver)` (snapshot of the denoted `GoStruct`) fixes the
CCE but only gives **field-level** write-back: field writes mutate the shared
`GoStruct` in place (`SelectorWriteNode.java:44-52` — in-place mutation is sound
because struct assignment deep-copies), so `p.x = v` in the body is visible
afterwards. But a whole-value replacement `*p = T{...}` (`DerefWriteNode.java:45`
→ `GoPointer.setReferent`) would write into the wrapper's private referent and
be lost — a gc deviation.

The proper adaptation addresses **v's storage**, exactly like the non-reflect
`addrof` path does. `methodVal` has the storage handle (`p.ptr()`); resolve it:

```java
/** Wrap the denoted receiver as *T for a pointer-receiver method value. */
private static Object adaptPointerReceiver(long handle, Object receiver) {
    Object storage = ReflectRefs.resolve(handle);
    if (storage instanceof graalgo.values.GoCell cell) {
        // &v semantics: address the variable's storage itself (cf. BoxAddrNode).
        // referent() reads the live value; setReferent() writes through —
        // full gc parity, including *p = v whole-value replacement.
        return new GoPointer(cell);
    }
    // Fallback: wrap the denoted value (precedent: makeStructEmbedWrapper,
    // ReflectTypes.java:1284). Field mutations are in-place on the shared
    // aggregate and stay visible; *p = v replacement is not written back
    // (documented residual gap, §3).
    return new GoPointer(receiver);
}
```

Why the `GoCell` branch is reachable in the primary repro:
`reflect.ValueOf(&x)` interns the D5 `GoPointer(GoBox)` that `&x` evaluates to
(`BoxAddrNode.java:57`); `.Elem()` on a cell pointer interns `gp.cell()`
(`ReflectOpNode.java:741`, the PKG-022 path), so `p.ptr()` resolves to the live
`GoBox`. `denotedValue` (`ReflectRefs.java:253-271`) derefs it to the current
`GoStruct`, and our `new GoPointer(cell)` re-addresses the same storage the
non-reflect path would have used. This is not a special case — it is the same
"pointer to the variable's storage" concept in both paths.

`GoPointer` and `GoCell` live in `graalgo.values` (already imported:
`GoPointer` at `ReflectOpNode.java:14`); add `import graalgo.values.GoCell;`.
`ReflectRefs.resolve` is public (`ReflectRefs.java:198`).

### 2.3 Addressability / write-back semantics and gc parity

| Aspect | gc | This design |
|---|---|---|
| Receiver captured | `&v` at `Method(i)` time | `GoPointer` to v's storage (cell) or to the denoted aggregate (fallback), captured at `methodVal` time |
| Later `v.Set` before `Call` | visible (address captured, not value) | **visible** in the cell branch (`referent()` re-reads); snapshot in the fallback branch (documented, §3) |
| Field mutation via receiver | visible through `v` | visible: in-place `GoStruct` mutation on the shared object (both branches) |
| `*p = T{...}` replacement | visible through `v` | visible in cell branch (`setReferent` → `cell.set`); **not** in fallback branch (residual gap) |
| Non-addressable `v`, pointer receiver | panic | `IllegalStateException("reflect: ...")` (file convention) |
| `*T` value (already pointer) | pass through | pass through (`instanceof GoPointer` guard) |
| Nil `*T` receiver | method value forms; call panics only on deref | unchanged pass-through; existing nil checks panic as before |

**Receiver-evaluation timing** matches gc and `MethodValueNode` (frontend):
the receiver is fixed when the method value is formed, not per call.

### 2.4 How parity is tested

- Differential corpus case (new `test/corpus/wave2/reflect/13_method_ptr_receiver.go`,
  see §4) run through `run_differential.sh` against real gc: output equality
  covers call results, write-back visibility, and the non-addressable panic.
- The panic case gets a `.panic` file pinning the expected text (corpus
  convention, cf. `10_failfast_methods.panic`); a `.divergent` file is added
  **only** for the documented residual gaps if the implementation lane can
  demonstrate gc divergence there — never pre-emptively.

---

## 3. Edge cases

1. **Non-addressable value with pointer-receiver method** — e.g.
   `reflect.ValueOf(T{...}).MethodByName("SetX")`. gc panics; the design panics
   via the `FLAG_ADDR` gate (§2.1). Must be a Go-visible panic per the file's
   `reflect:` error convention, not a host CCE.

2. **Nil pointer receiver** — `var p *T = nil; reflect.ValueOf(p).Method(i)`.
   The denoted value is a nil `GoPointer` (or Java null); the
   `instanceof GoPointer` / null guards pass it through unchanged. gc forms the
   method value and panics only if the body dereferences — preserved.

3. **Value-receiver methods** — untouched: `mi.pointerReceiver` is false, the
   guard is skipped, behavior identical to today.

4. **Embedded / promoted methods** — promoted `MethodInfo`s carry the inner
   method's `pointerReceiver` bit (`promoteOne`, `ReflectTypes.java:1916-1942`)
   and their `func` is the `makeStructEmbedWrapper` wrapper
   (`ReflectTypes.java:1275-1297`), which already unwraps an outer `GoPointer`
   (`recv instanceof GoPointer gp ? gp.referent() : recv`) before navigating
   the embedding path and applies its own inner wrap at line 1284. Our outer
   wrap therefore **composes** without double-wrapping. Write-back through
   promoted methods inherits the wrapper's documented limitation
   (`ReflectTypes.java:1275-1279`: mutations don't write back to the outer
   struct — pre-existing, accepted; the alternative was a crash).

5. **`reflect.New(T).Elem().Method(i)`** — handle resolves to a
   `ReflectRefs.Cell` (not a `GoCell`), so the fallback branch wraps the
   denoted `GoStruct`. Field mutations are visible afterwards (same object as
   `cell.value`); `*p = T{...}` whole-value replacement is the residual gap.
   The implementation lane must cover the field-mutation case in the corpus
   test and note the replacement gap in the test file if exercised.

6. **Methods on non-struct named types** — e.g. `type I int` with
   `func (p *I) Set(v int)`. The denoted receiver is a boxed scalar; the cell
   branch (addressable case) still gives full parity via `GoCell`; the fallback
   wraps the snapshot. Bodies operating through `(GoPointer)` casts work in
   both branches.

7. **Interface-kind values** — `methodVal` on an interface `v` is unchanged by
   this design; only the `mi.pointerReceiver && !(receiver instanceof GoPointer)`
   conjunction gains the wrap. If `unboxValue` yields a non-pointer for an
   interface holding `*T`, that is a separate (pre-existing) representation
   question, out of scope.

8. **Fallback-branch staleness** — in the fallback branch the `GoStruct` object
   is captured at `Method(i)` time, so a later `v.Set` before `Call` is not
   observed (gc would observe it). This is inherent to wrapping a denoted
   snapshot and is why the cell branch is preferred whenever the handle
   resolves to a `GoCell`. The primary repro (`ValueOf(&x).Elem()`) always
   takes the cell branch.

---

## 4. Regression test plan

New corpus case `test/corpus/wave2/reflect/13_method_ptr_receiver.go`
(differential via `run_differential.sh`, compared against real gc):

```go
package main

import (
	"fmt"
	"reflect"
)

type Counter struct {
	Vals [4]int
	N    int
}

func (c *Counter) Add(i, v int) { c.Vals[i] = v; c.N++ } // field write via *T
func (c *Counter) At(i int) int { return c.Vals[i] }      // index through *T (CCE repro shape)
func (c *Counter) Sum() int {
	s := 0
	for _, v := range c.Vals[:c.N] { s += v } // slice through *T (CCE repro shape)
	return s
}
func (c *Counter) Reset() { *c = Counter{} } // whole-value replacement

func main() {
	c := Counter{}
	m := reflect.ValueOf(&c).Elem().MethodByName("Add")
	m.Call([]reflect.Value{reflect.ValueOf(1), reflect.ValueOf(42)})
	fmt.Println(c.N, c.Vals[1]) // write-back visibility: 1 42

	at := reflect.ValueOf(&c).Elem().MethodByName("At")
	fmt.Println(at.Call([]reflect.Value{reflect.ValueOf(1)})[0].Int()) // 42

	sum := reflect.ValueOf(&c).Elem().MethodByName("Sum")
	fmt.Println(sum.Call(nil)[0].Int()) // 42

	r := reflect.ValueOf(&c).Elem().MethodByName("Reset")
	r.Call(nil)
	fmt.Println(c.N, c.Vals[1]) // 0 0 — exercises the cell branch's setReferent path
}
```

Companion panic case `14_method_ptr_receiver_nonaddr.go`:

```go
package main

import "reflect"

type T struct{ X int }

func (t *T) SetX(v int) { t.X = v }

func main() {
	// gc panics: reflect.Value.Method on non-addressable value.
	m := reflect.ValueOf(T{X: 1}).MethodByName("SetX")
	m.Call([]reflect.Value{reflect.ValueOf(2)})
}
```

with `14_method_ptr_receiver_nonaddr.panic` pinning the exact expected stderr
text (implementation lane to fill in from the `IllegalStateException` message
after confirming how `reflect:` errors surface through the synthetic-call
driver), and a `.divergent` file only if a genuine residual gap is demonstrated.

Additional assertions for the implementation lane:

1. **Existing corpus stays green:** `05_method_values.go` (value-receiver path
   must be byte-identical in behavior — the guard must not fire for it).
2. **Nil receiver:** `var p *Counter; reflect.ValueOf(p).MethodByName("Sum")`
   forms without wrapping; calling panics with nil dereference under both
   implementations (differential covers it).
3. **Pointer value without Elem:** `reflect.ValueOf(&c).MethodByName("Add")`
   — receiver already a `GoPointer`, no wrap, write-back visible (regression
   guard for the `instanceof` branch).
4. **Embedded method:** struct embedding `Counter` (value) with a promoted
   pointer-receiver method — call works (no double-wrap CCE); write-back
   limitation from `makeStructEmbedWrapper` documented, not asserted.

## 5. Open questions for the implementation lane

1. Exact panic surfacing: confirm that `IllegalStateException("reflect: ...")`
   thrown from `methodVal` surfaces to the corpus runner as a Go-visible panic
   with stable text (check how `10_failfast_methods`' text is produced) before
   writing the `.panic` file.
2. `GoCell` vs `GoBox`: `reflectElem` interns `gp.cell()` whose static type is
   `GoCell`; `BoxAddrNode` wraps a `GoBox` (a `GoCell` subtype — verify).
   The `instanceof GoCell` check covers both; confirm no other cell-like
   storage type needs inclusion.
3. Whether `p.flag()` on the *source* Value is the right addressability
   authority in all `methodVal` callers (only `__reflect_methodval` calls it —
   verify there is exactly one call site).

## 6. What this design deliberately does NOT do

- No change to `ReflectCalls.BoundMethodVal` / `invokeCallable`: the adaptation
  happens once at method-value formation, matching gc's capture semantics.
- No change to `boundMethodTok` or the packed `Value` flags.
- No new wrapper node types, no frontend changes: the compiled bodies already
  expect `GoPointer`; we now actually give them one.
- No snapshot-copy of the receiver for the value-receiver path (pre-existing
  semantics, out of scope).
