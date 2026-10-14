# R1 peer review — M5 (defer/panic/recover/Goexit) + loop-capture remediation
**Lens:** semantics and design. **Reviewer:** R1 (independent; no coordination with other reviewers).
**Date:** 2026-10-04. **Snapshot:** [reviews/phase1-m5-loopcap/snapshot/](snapshot/)
**Report:** [reviews/phase1-m5-loopcap/R1-design.md](R1-design.md) (this file; my only write).

## 1. What I reviewed (and checksum verification)

I worked **only** from the fixed snapshot; I never read the live tree at [phases/phase1/](../../docs/arch/phases/phase1).

- Verified integrity: ran `sha256sum -c ../SHA256SUMS.txt` from inside [snapshot/](snapshot/) — **every file OK, zero failures**.
- Java frontend ([snapshot/java/frontend/](snapshot/java/frontend/), 77 files), principally:
  - `NodeFactory.java` (2977 lines) — `prescanForBoxes` (:1140), `prescanRangeBoxes`, `snapshotBoxedVar` (:893), `readBoxedVar` (:998), `boxWriteSlot` (:1022), `headerPhase` (:960), loop-phase stack (:203–211), `buildFor` (:2420), `buildRange` (:2521), `buildIncDec` (:2326), `assignTwoPhaseBoxed` (:1708), `classifyLhs`/`readLvalue`/`writeLvalue`
  - `Boxing.java` — `setBoxedCell` (:72), `copyForAssign` (:89)
  - `BoxWriteNode.java`, `BoxRefreshNode.java` (:30), `BoxDeclNode.java`, `BoxReadNode.java`
  - `FuncLitNode.java` — `(depth,slot,snapSlot)` capture (:56–80)
  - `ForRepeatingNode.java`, `GoLoopNode.java`, `ForStmtNode.java`
  - `RangeSliceNode.java` (:85–95), `RangeMapNode.java`
  - `DeferNode.java`, `PanicNode.java`, `PanicBuiltinNode.java`, `RecoverBuiltinNode.java`, `GoexitCallNode.java`
- Go exporter: [snapshot/export.go](snapshot/export.go) — `exportFor` (:1868), `exportRange` (:1915)
- Corpus: [snapshot/corpus/controlflow/](snapshot/corpus/controlflow/) (cf01–cf23), [snapshot/corpus/panic/](snapshot/corpus/panic/) (pn01–pn20)
- Design evidence: `[snapshot/evidence/loopcap/](snapshot/evidence/loopcap/){CHECKPOINT_LOOPCAP,DESIGN_WIRING,COMPLETE,PROGRESS}.md`, [snapshot/evidence/CHECKPOINT.md](snapshot/evidence/CHECKPOINT.md) (tail)

**Method.** I did not trust the prior reports' "gc-oracle verified" claims. I derived gc's true semantics two independent ways: (1) read gc's own loopvar transformation in the installed toolchain (`~/tools/go/src/cmd/compile/internal/loopvar/loopvar.go`, go1.27.1); (2) ran oracle probes with `~/tools/go/bin/go run` on scratch files in `/tmp/oracle` only. The two agree. The project's t1–t11 notes ("for-header vars = two cells; body writes write through to outer; post does NOT mutate body-captured instances") are a **mis-model of gc**, and every P0 below is a place where the implementation follows the mis-model instead of gc.

### gc's actual model (from gc source + oracle, not from project notes)

`cmd/compile/internal/loopvar/loopvar.go` transforms `for init(z); Cond; Post { Body }` (when z is captured) into:

```
z' := <init>            // carry variable (init rewritten z→z')
tmp_first := true
for {
    z := z'             // ONE fresh per-iteration variable
    if tmp_first { tmp_first=false } else { Post(z) }  // post on z, before cond/body
    if !Cond(z) { break }                              // cond on z
    Body(z)                                            // body + closures capture z
next:               // `continue` lands here
    z' = z             // write-back carry
}
```

Consequences I verified empirically: within an iteration, init-seed/post/cond/body/closures all operate on **one** variable `z_k`; iteration k+1's variable is initialized from `z_k`'s post-mutation value (write-back); a write to a *stale* iteration's variable affects only that dead variable. There is **no** separate "outer" variable for cond/post. Range `:=` becomes `for tk := range … { k := tk; body }` (fresh per-iteration copy); range `=` declares nothing and assigns the existing variable.

## 2. Findings

### P0-1 — Stale per-iteration cell writes corrupt the shared outer cell (wrong loop termination)
**Files:** `Boxing.java:72-83`, `BoxRefreshNode.java:37`, `FuncLitNode.java:62-80`, `NodeFactory.java:893-919`

The write-through (`cell.outer.value = …` in `setBoxedCell`) has no notion of "current iteration". A closure that captured iteration *j*'s body cell, when called during/after a later iteration, writes through `B_j → S_outer`, permanently polluting the cell that seeds all future iterations and drives `cond`.

```go
var g func()
func main() {
    for i := 0; i < 4; i++ {
        if i == 0 { g = func() { i += 100 } }  // captures iter-0 cell
        if i == 2 { g() }                       // called in iter 2: must touch only iter-0's var
        println(i)
    }
}
```
- **gc:** `0 1 2 3` (verified).
- **Snapshot (traced):** iter2's `g()` sets `B_0=100`, write-through sets `S_outer=100`; post makes it 101; iter3's `cond` reads `S_outer=101`, `101<4` false → loop exits. Prints **`0 1 2`** — silently drops an iteration.
- gc's write-back is `z' = z` where `z` is the *current* iteration's variable; a stale closure writes only its own dead `z_j`. The snapshot's shared `S_outer` has no such isolation.

### P0-2 — Struct/array field/element mutations in the body are invisible to post/cond
**Files:** `BoxRefreshNode.java:37` (`copyForAssign` deep-copies/clones into the body cell), `NodeFactory.java:998-1013` (post/cond read `S_outer`), `NodeFactory.java:2326+` (`s.x++` desugars to `SelectorNode`/`SelectorWriteNode` over `readBoxedVar`)

`BoxRefreshNode` seeds the body cell with an *independent copy* (struct deep-copy, array clone), but partial mutations (`s.x = 5`, `a[0] = 5`) mutate only that copy — `setBoxedCell`'s write-through fires only for whole-variable writes (`BoxWriteNode`). Post/cond read `S_outer`, which never sees the mutation. In gc, post/cond/body share the one per-iteration variable, so post sees the mutation.

```go
type T struct{ x int }
var sink func() int
func main() {
    n := 0
    for s := (T{x: 0}); s.x < 3; s.x++ {
        s.x = 5
        if sink == nil { sink = func() int { return s.x } }  // force boxing
        n++
    }
    println(n)  // gc: 1
}
```
- **gc:** `n=1` (verified): body sets `z.x=5`, post reads 5→6, cond fails.
- **Snapshot (traced):** body mutates `B_k`'s copy; post `s.x++` reads `S_outer={0}`→`{1}`; cond `1<3`… loop runs 3 times → **`n=3`**.
- Same class for arrays (`cloneArray`): `for a := ([3]int{0,0,0}); a[0] < 2; a[0]++ { a[0] = 5; … }` → gc `n=1`, snapshot traces to `n=3` (oracle `atkD2.go` confirmed gc side).
- Note the design doc (`DESIGN_WIRING.md` correction #1) claims "post reads AND writes S_outer" is the *fix* for this case — it is backwards. gc requires post to see the body's mutation; the doc's reasoning ("post must see outer") inverts the oracle.

### P0-3 — Closures created in cond/post capture `S_outer`, gc captures the per-iteration variable
**Files:** `NodeFactory.java:893-919` (`snapshotBoxedVar`: non-`LP_BODY` phase → snapshots `S_outer`)

In gc's desugar, cond/post sit *inside* the per-iteration region (steps 6–7 operate on `z` after `z := z'`), so a funclit there captures `z_k`. The snapshot captures `S_outer`, which keeps being mutated by later iterations' write-throughs and posts.

```go
var g func() int
func setG(f func() int) int { if g == nil { g = f }; return 0 }
func main() {
    for i := 0; i < 3; i, _ = i+1, setG(func() int { return i }) {
        if i == 2 { println(g()) }
    }
}
```
- **gc:** `1` (verified) — `g` captures iter-1's `z` (first post that runs), whose final value is 1.
- **Snapshot (traced):** `g`'s `boxSpec` triple is `(0, S_outer_slot, snap)`; called in iter-2's body it reads `S_outer=2` → prints **`2`**.
- Same class for cond-created closures (`for i := 0; i < 3 && check(func() int { return i }); i++`, oracle `atkH2.go`: gc `0`, snapshot traces to `2`).

### P0-4 — Range `=` into an enclosing for-header variable writes `S_outer` while the body reads `S_iter`
**Files:** `NodeFactory.java:2562` (`slotForOid` → `S_outer` slot for the `=` form), `RangeSliceNode.java:85-95` (`setBoxedCell` on that slot), `NodeFactory.java:998-1013` (body reads resolve to `S_iter` via the phase stack)

`for i = range …` (`=` form) declares nothing in gc — it assigns the existing variable, which inside the transformed loop body denotes the per-iteration `z_k`. The snapshot writes the `S_outer` slot directly, but every read in the (phase-`LP_BODY`) region resolves to `S_iter`, which the range never updates.

```go
func main() {
    sum := 0
    for i := 0; i < 2; i++ {
        f := func() int { return i }  // boxes outer i
        _ = f
        for i = range []int{7, 8} {   // `=` form: assigns existing i (indices 0,1)
            sum = sum*10 + i
        }
        sum = sum*10 + i
    }
    println(sum)  // gc: 11
}
```
- **gc:** `11` (verified).
- **Snapshot (traced):** range writes `S_outer` (0 then 1); inner body and the trailing read see stale `B_0=0` → **`sum=0`**.
- The `=`-into-boxed path must write the *phase-aware* slot (`boxWriteSlot`), i.e. `S_iter` in body phase, letting the existing write-through propagate to `S_outer` exactly as gc's write-back does. (The `=`-into-boxed path is correct only for single-cell body vardecls whose `outer` is null.)

### P0-5 — Transitive capture through a nested funclit does not propagate; the inner literal reads the live slot
**Files:** `NodeFactory.java:893-919` (triple added only to the *innermost* funclit's `boxSpec`), `FuncLitNode.java:62-80` (depth walk reads the *current* slot content)

When only the *inner* of two nested funclits references the loop variable, the outer funclit gets **no** `boxSpec` triple (its `boxSnapSlots` stays empty). The inner's triple `(depth=1, S_iter_slot, snap)` is resolved at the inner literal's *creation* time by walking the `$closure` chain to the **live** function frame and reading whatever cell the `S_iter` slot holds *then*.

```go
var fns [3]func() func() int
func main() {
    for i := 0; i < 3; i++ {
        fns[i] = func() func() int {
            return func() int { return i }  // only inner references i
        }
    }
    println(fns[1]()())  // gc: 1
}
```
- **gc:** `1` (verified) — the outer literal captures iter-1's variable (capture analysis is subtree-inclusive), the inner captures through it.
- **Snapshot (traced):** outer `FuncLitNode` has `boxSpec=null`; `fns[1]()` runs after the loop, the inner literal's walk reads the live `S_iter` slot = `B_2` → prints **`2`**.
- `COMPLETE.md` claims "FuncLitNode boxSpec (depth,slot,snapSlot) with capture propagation" — the propagation is not in the snapshot's `NodeFactory`; the doc overclaims. The depth walk is only correct when the intermediate literal is *called in the same iteration* it was created in.

### P1-1 — `cf16_loopcap_writethrough.go` is vacuous: it does not exercise boxing at all
**File:** `snapshot/corpus/controlflow/cf16_loopcap_writethrough.go`

The test's comment claims "body writes to the for-header variable write through to the outer cell, so cond/post observe them (gc-oracle: post sees the body write)". But the program contains **no function literal**, so `prescanForBoxes` never boxes `i` (`boxed = initDecls ∩ funclitRefs = ∅`) — the variable stays a plain slot and the write-through path is never executed. The differential test passes trivially and gives false confidence in the write-through design. (A real write-through test needs a capturing funclit, e.g. my P0-1/P0-2 programs.)

### P1-2 — `BoxRefreshNode` copy depth is unverifiable from the snapshot; must be Go-assignment (field-wise), not recursive
**File:** `snapshot/java/frontend/Boxing.java:89-110` (`copyForAssign` → `gs.deepCopy()`)

gc's per-iteration `z := z'` is a Go assignment: field-wise copy, pointer/slice/map fields **alias**. If `GoStruct.deepCopy()` is recursive (copies pointees), the refresh breaks pointer sharing:

```go
type T struct{ p *int }
var sink func() int
func main() {
    p0 := new(int); n := 0
    for s := (T{p: p0}); n < 2; n++ {
        *s.p = 7
        if sink == nil { sink = func() int { return *s.p } }
    }
    println(*p0)  // gc: 7
}
```
- **gc:** `*p0=7` (verified, oracle `atkP.go`) — iterations share the pointer.
- If `deepCopy` is recursive, the snapshot traces to `*p0=0` (each refresh re-copies the pristine pointee; the body's store hits a throwaway copy).
- The values package (`graalgo.values.GoStruct`) is **not in the snapshot** (only a pre-`outer` pristine `GoBox` copy exists under `evidence/`), and no corpus test pins struct-with-pointer-field assignment — so I could not verify. If recursive, this is a P0; either way the refresh must be audited against Go assignment semantics.

### P1-3 — Dead 4-arg `ForRepeatingNode` constructor; javadoc describes a design `buildFor` doesn't use
**File:** `snapshot/java/frontend/ForRepeatingNode.java:44-53`

The javadoc says the prologue "runs *before* the condition … giving each iteration its own variable", but the 4-arg constructor is **never called** (`grep new ForRepeatingNode` → only the 3-arg call at `NodeFactory.java:2492`); `buildFor` instead prepends `BoxRefreshNode`s to the body block (i.e. refresh runs *after* cond). The placement is currently unobservable (nothing can read `S_iter` before the first refresh, since cond/post/header-closures all use `S_outer`), so this is benign today — but the node's own documentation describes a different, more faithful design, evidencing design drift between the node layer and the factory wiring.

### P2-1 — Stale "(M6, …)" labels on loop-capture code
`Boxing.setBoxedCell` (:66–71), `BoxWriteNode` (:7), `NodeFactory` prescan (:1039: "M6 loop-capture pre-scan") label the loop-capture machinery as M6 (generics). The loop-capture remediation is a separate workstream from M6 generics; the mislabel will confuse the M6 worker editing these same files. Rename to the loop-capture workstream id.

### P2-2 — Redundant double copy on boxed writes
`BoxWriteNode.executeVoid` (:31–32) applies `copyForAssign` and then `setBoxedCell` applies it *again* to the outer cell (`Boxing.java:79-81`). Value-correct (both cells end independent), but a struct/array write pays two deep copies. Fold into one.

## 3. What I explicitly could NOT verify (and why)

1. **M5 runtime machinery** — `graalgo.runtime.{DeferChain, Panic, Goexit, GoroutineState, DeferredCall}`, `graalgo.values.GoBox` (with `outer`), `GoFunc`/`GoFuncRootNode` cell installation: **not present in the snapshot** (only `frontend/` + `export.go` + corpus + evidence were snapshotted). Therefore unverifiable from the snapshot: per-goroutine defer chains, LIFO drain on return/panic/goexit, `panic(nil)` → `*PanicNilError*` (GG-0045 stance), lazy stack traces, the recover-allowed gating (`RecoverBuiltinNode` → `Panic.recover()`), deferred-builtin `panic`/`recover` interplay, and the main-goroutine Goexit fatal-deadlock path. The *frontend* nodes (`DeferNode`, `PanicNode`, `PanicBuiltinNode`, `RecoverBuiltinNode`, `GoexitCallNode`) trace correctly: call-time arg evaluation, nil-func-value panic at defer time, exception transparency through `GoLoopNode`/`ForRepeatingNode`/`ForStmtNode` (only `BreakSignal`/`ContinueSignal` are caught, everything else propagates), and `GoexitCallNode` throwing via `Goexit.goexit()`.
2. **End-to-end execution of the snapshot** — the snapshot is not self-contained (missing `graalgo.values`, `graalgo.runtime`, Truffle jars, build files), and I am forbidden from touching the live tree the M6 worker owns. All snapshot-side behaviors above are **code traces**, cross-checked against the gc oracle. The traces are line-precise, but they are not runs.
3. **`GoStruct.deepCopy()` recursion depth** (P1-2) — values package absent; no corpus coverage.
4. **Generics-scoped code** (`typeParam`/`instantiate`/monomorphization in `export.go`) — out of scope per brief; I saw no interaction with M5/loopcap paths beyond shared files. One boundary note: `buildFuncLit`'s funclit path is shared; if M6 changes `boxSpec` draining, the P0-5 propagation fix must be coordinated.

## 4. Verdict

**The loop-capture design does not hold up adversarially. The M5 defer/panic/recover/Goexit frontend nodes do (within what the snapshot allows me to check).**

The five P0s share one root cause: the design models gc's loop variables as **two cells** (`S_outer` for init/cond/post + header closures, `S_iter` for the body, glued by write-through), but gc has **one variable per iteration** plus a carry with write-back (`z := z'` … `z' = z` at `next:`). The two-cell model is observationally equivalent only when (a) every body mutation is a whole-variable write, (b) no write ever reaches a stale iteration's cell, (c) no closure is created in cond/post, (d) no `=`-range targets a header variable, and (e) every capturing funclit directly references the variable. Each P0 breaks one of those assumptions with a short, legal Go program, confirmed against the gc oracle (go1.27.1) and against gc's own `loopvar` source.

Concretely: cross-iteration closure writes silently change loop termination (P0-1); struct/array header variables with field mutations run the wrong number of iterations (P0-2); post/cond-created escaping closures observe the wrong iteration (P0-3); `for i = range` into an enclosing header variable reads stale values (P0-4); nested-only captures bind the wrong iteration when the outer literal outlives its iteration (P0-5).

The 114 green differential tests do not cover any of these shapes — the corpus has no cross-iteration writes, no struct/array header variable with post-observed field mutation, no header-position escaping closures, no `=`-range into a header variable, and no nested-only transitive capture. (P1-1 shows one test even claims to cover the write-through without boxing the variable.)

**Recommended direction** (for the parent, not a prescription): make cond/post/init operate on the per-iteration cell like the body does (gc's `z_k`), replace the write-through with an explicit end-of-iteration write-back `S_outer := S_iter` (also on `continue` paths), move the refresh before cond (the dead 4-arg `ForRepeatingNode` constructor already describes this), make header-phase closures capture the per-iteration cell, fix range-`=` to use the phase-aware slot, and propagate captures through intermediate funclits. The phase stack itself (the one part explicitly called out as correct-for-nesting) is sound and should be kept.

The M5 frontend (defer evaluation-time semantics, panic-value boxing incl. the `*PanicNilError*` wiring point, recover gating call, Goexit-as-`ControlFlowException`, exception transparency of all three loop nodes, `break`/`continue`/label signal routing) is coherent as far as the snapshot reaches; the runtime half of M5 cannot be judged from this snapshot.
