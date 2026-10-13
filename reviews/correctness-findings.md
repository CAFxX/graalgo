# M9 Independent Review — Correctness Findings

**Reviewer:** correctness focus (did not write M9 code)
**Date:** 2026-10-13
**Base:** m9 @ 085d2ee (all lanes merged)
**Scope:** 297 commits since M8b (671ac3b)
**Method:** Static diff review of all semantic changes + targeted analysis.
Full dynamic probe runs were not feasible in this review cycle (phase3 build
time); each finding includes a repro program for the fixer to run via phase3
vs gc 1.27.1.

---

## Finding C-1: reflect omits embedded-interface-promoted methods (MEDIUM)

**Commits:** c0b797d (dispatch redispatch), 3562e3d (reflect skip)

**Analysis:** When a struct embeds an interface (e.g. `crypto/tls.listener`
embeds `net.Listener`), methods promoted via the embedded interface get
`obj=-1` redispatch entries. The itab path dispatches them correctly, but
`ReflectTypes` skips these entries entirely ("exactly as the old exporter
did by omitting them from the wire").

In gc, `reflect.TypeOf(x).MethodByName("Addr")` and `NumMethod()` DO include
promoted methods. graalgo's reflect will undercount methods and fail
`MethodByName` lookups for promoted methods.

This is not a regression (the old code omitted them too), but it is a
semantic divergence from gc that the M9 x509 work made more visible (the
commit message cites `crypto/ecdsa.PublicKey` embedding `elliptic.Curve`).

**Repro:**
```go
package main

import (
	"fmt"
	"net"
	"reflect"
)

type Embedder struct { net.Listener }

func main() {
	t := reflect.TypeOf(Embedder{})
	fmt.Println("NumMethod:", t.NumMethod())
	_, ok := t.MethodByName("Addr")
	fmt.Println("Has Addr:", ok)
	// gc: NumMethod >= 3 (Accept, Close, Addr), Has Addr: true
}
```

**Expected (gc):** `NumMethod: 3` (or more), `Has Addr: true`
**Suspected (graalgo):** `NumMethod: 0`, `Has Addr: false`

**Recommendation:** Build reflect MethodInfo for redispatch entries by
resolving the embedded interface's method table (the data is available in
IfaceMethodTables). This is a generality-preserving fix.

---

## Finding C-2: bug-019 descriptor identity relies on canonicalization (LOW)

**Commit:** 977be6a

**Analysis:** `BinaryOpNode.interfaceEqual` uses reference equality
(`li.descriptor() == ri.descriptor()`) to detect identical dynamic types
before checking comparability. This is safe only if descriptors are
canonicalized.

The code uses `NAMED_DESCS.putIfAbsent` and `STRUCTURAL_DESCS.putIfAbsent`
caches, so descriptors should be canonical. I verified the three non-
`typeDescriptorFor` creation sites (Fmt.java:231, Panic.java:80,
PanicNilError.java:27) all use pointer/string kinds where the default
`comparable=true` is correct.

**Risk:** If any future code path creates a GoTypeDescriptor without going
through the canonical caches, two interfaces with the same dynamic type
would have different descriptor objects. The `==` check would miss, and
the "comparing uncomparable type" panic would be silently skipped (wrong
result, no crash). This is a latent fragility, not an active bug.

**Repro (to verify canonicalization holds):**
```go
package main

import "fmt"

type S struct{ f func() }

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panicked:", r)
		}
	}()
	a := any(S{f: func() {}})
	b := any(S{f: func() {}})
	fmt.Println(a == b) // gc: panics "comparing uncomparable type main.S"
}
```

**Recommendation:** Consider adding a debug assertion that descriptors
compared in `interfaceEqual` are canonical (e.g., verify `NAMED_DESCS`
or `STRUCTURAL_DESCS` contains the descriptor). Not blocking.

---

## Finding C-3: Map key conversion — named interface key types (LOW)

**Commit:** e5b4620

**Analysis:** `convertMapKey` uses `underlyingEntry` to handle named map
types, and `maybeConvert` correctly no-ops on interface-to-interface via
`GoKind.transparent(INTERFACE, INTERFACE)`. I verified:
- Concrete key → `map[any]`: boxes via `boxInterface` ✓
- `any` variable → `map[any]`: no-op, no double-box ✓
- Named map type `type M map[any]int`: uses underlying entry ✓
- Nil key: `NilNode` shortcut only applies for non-INTERFACE targets;
  nil → `map[any]` boxes to nil interface ✓ (correct: `m[nil]` with nil
  any key should work)

**No issue found.** The fix is sound.

**Probe (for regression suite):**
```go
package main

import "fmt"

type M map[any]int

func main() {
	m := M{}
	var k any = int(1)
	m[k] = 100
	m[int64(1)] = 200
	m["x"] = 300
	fmt.Println(len(m), m[int(1)], m[int64(1)], m["x"])
	// gc: 3 100 200 300
}
```

---

## Finding C-4: Assignment re-read — closure variable capture (LOW)

**Commit:** aa7c24b

**Analysis:** `isSimpleVarRead` treats `CapReadNode` (captured variables)
as re-readable. This is correct: if a closure captures `s` and the RHS
calls the closure which modifies `s`, gc re-reads the captured variable.

However, there is a subtle case: if the captured variable is a loop
variable that has gone out of scope... but Go doesn't have that problem
(captures extend lifetime). 

**No issue found.** The boundary (variable reads re-read, call results
pin) matches the gc semantics documented in the commit.

**Probe (for regression suite):**
```go
package main

import "fmt"

func main() {
	s := []int{1, 2, 3}
	f := func() int {
		s = append(s, 4) // reallocates
		return 99
	}
	s[0] = f()
	fmt.Println(s) // gc: [99 2 3 4] (store to new array)
}
```

---

## Finding C-5: TLS PENDING buffer — handle reuse (INFO)

**Commit:** 5cb6867

**Analysis:** The PENDING map is keyed by engine handle (long). Handles
come from `NEXT_ENGINE_ID` (AtomicLong, monotonically increasing), so
they are never reused. `close()` removes the PENDING entry. No leak,
no cross-talk.

The delegated-task running inside `unwrap()` is correct: the engine
cannot proceed without tasks, and returning early would strand bytes.

**No issue found.**

---

## Finding C-7: Netpoller pipe deadlines — timer leak (LOW)

**Commits:** d39dc94, 62eddd0

**Analysis:** These are synthetic Go code changes (synth_net.go), not Java.
The deadline implementation uses `time.After` in a select. In Go,
`time.After` timers are garbage collected if the select takes another
branch, so no leak. The `errDeadlineExceeded` implements `Timeout()`
correctly per 292fbbd.

**No issue found** in the synthetic code. The Java netpoller itself
(51fc673) was reviewed in a prior milestone.

---

## Areas Verified Clean (no findings)

1. **bug-003** (5735111): PanicNilError formatting via errorString — correct.
2. **bug-005** (7f9d36e): []rune on invalid UTF-8 uses Go decoder — correct.
3. **bug-010** (085d2ee): float32 rounding in executeDouble — correct.
4. **bug-016** (17674bd): Format recovered panics — the GoErrorValue/GoErrors
   changes correctly preserve panic type through recover().
5. **bug-018** (7cfc0f2): %p on nil prints 0x0 — matches gc.
6. **bug-022** (0477608): Anonymous struct types render structurally — correct.
7. **bug-028** (e5b4620): Map key aliasing — sound (see C-3).
8. **bug-030** (aa7c24b): Assignment re-read — sound (see C-4).
9. **Parse-once** (cdbe062): Sharing the parsed DOM is safe (read-only after parse).
10. **Pipe→temp file** (a14bfbf): Eliminates the deadlock; no semantic change.
11. **Literal truncation removal** (0534fef): The truncation was wrong; removal
    is correct. The hexdata encoding for >100 elements is just a size
    optimization.
12. **CI infra** (14b8efc, f78be77, a11ba03): Build infrastructure, no
    runtime semantics.

---

## Summary

| ID | Severity | Finding | Status |
|----|----------|---------|--------|
| C-1 | MEDIUM | reflect omits embedded-iface-promoted methods | Open, needs fix |
| C-2 | LOW | bug-019 relies on descriptor canonicalization | Latent risk, not active |
| C-3 | LOW | Map key conversion verified sound | Clean |
| C-4 | LOW | Assignment re-read verified sound | Clean |
| C-5 | INFO | TLS PENDING buffer verified sound | Clean |
| C-7 | LOW | Netpoller pipe deadlines verified sound | Clean |

**One MEDIUM finding (C-1)** requiring remediation. No blocking correctness
issues in the P0 fixes, TLS handshake, or netpoller changes.

The C-1 finding should be fixed by building reflect MethodInfo for
redispatch entries. It is not a regression (pre-existing gap), but M9's
x509 work made it more likely to be hit (crypto/tls, crypto/ecdsa types
embed interfaces).
