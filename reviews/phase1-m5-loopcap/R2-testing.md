# R2 — Test adequacy & evidence honesty: M5 (defer/panic/recover/Goexit) + loop-capture remediation

Reviewer lens: do the corpus tests cover the claimed semantics, and do the evidence logs prove the claims?
Read-only from the fixed snapshot `reviews/phase1-m5-loopcap/snapshot/`; the live tree was never
touched and nothing was built. gc oracle: `~/tools/go/bin/go` (go1.27.1). All adversarial programs below
were run under the oracle; outputs are pinned.

## 0. Integrity checks

1. `sha256sum -c ../SHA256SUMS.txt` inside `snapshot/` → every file OK, exit 0.
2. `evidence/loopcap/pristine/MD5SUMS` → 11/11 pristine copies verify (NodeFactory, GoBox, GoFunc,
   FuncLitNode, BoxWriteNode, 4 Range nodes, pristine pn13, ArithmeticCorpusTest).
3. Snapshot contains: `corpus/` (7 dirs), `evidence/` (m5 logs, loopcap dir, ws-g dir, CHECKPOINT.md,
   freeze-stub.md, frontend-v1.md), `java/frontend/` (node sources incl. NodeFactory). It does NOT
   contain: test sources, runtime sources (`DeferChain`, `Fmt`, `Panic`, …), `hidden_files/`,
   `Makefile`, or `/tmp` logs.

## 1. Evidence audit

### 1.1 `evidence/m5_green.log` — P0-1 (mislabeled; does not evidence the claim)

1. The file contains 70 `PASS` lines, then
   `FAIL PanicCorpusTest.testPn06PanicNil: java.lang.AssertionError: ./corpus/panic/pn06_panic_nil.go: stock go run failed (exit 143): stdout=<> stderr=<>`,
   then `make: *** [Makefile:66: test] Terminated`. No summary line; the run never reached pn07.
   Exit 143 = SIGTERM on the gc oracle itself.
2. The M5 acceptance record (`evidence/CHECKPOINT.md:717`) does not cite this file. It cites
   `/tmp/m5_verify.log` — "coordinator re-run: 102 tests, 0 failures, EXIT=0" — which is absent
   from the snapshot (ephemeral `/tmp`, not preserved).
3. Finding: the file named `m5_green.log` documents a run killed mid-suite, not 102/102 green.
   The M5 "102/102 tests green" claim **cannot be re-derived from the snapshot**. The single
   artifact cited for the acceptance gate is not in the review basis at all.

### 1.2 `evidence/m5_mutant_red.log` — the one fully checkable mutation claim (with one anomaly)

1. Runs to completion: `102 tests, 3 failures`, every failure with exact assertion text.
2. `pn07_recover_stops.stage (INT64): gc=<7123> graalgo=<712>` — hand-derived: defers registered
   d1=`stage*10+3`, d2=recover→`+2`, d3=`stage*10+1`; LIFO on `panic("x")`: 7→71 (d3), →712
   (d2 recovers), →7123 (d1). Reverting the DeferChain fix (drop defers remaining after recovery)
   loses d1 → exactly 712. **Matches the log.**
3. `pn09_nested_panic.log (INT64): gc=<321> graalgo=<32>` — hand-derived: `panic("first")` →
   d3 runs (log=3) then `panic("second")` → d2 recovers "second" (final="second", log=32) →
   d1 dropped by the reverted bug → exactly 32. **Matches the log.**
4. The mutation itself (what was reverted) is not recorded in the log — no diff, no marker;
   "reverting the DeferChain fix" comes from checkpoint prose only. P2-1.
5. **P1-1 — unexplained third failure:** `pn14_oob_index: // panic: trailer <runtime error: index
   out of range [5] with length 3> does not match gc's first stderr line <panic: runtime error:
   index out of range [5] with length 3>`. pn14 has no defers and no recover; the reverted bug
   (drop post-recovery defers) cannot affect it, and pn15–pn20 (same `// panic:` machinery) all
   pass in this run. No in-snapshot baseline exists for pn14 (the green log died at pn06), so this
   failure is unaccounted for: either the mutation's blast radius is wider than documented, or the
   trailer comparison is flaky/environmental. Either way the "fails exactly as predicted" story is
   incomplete — the prediction covered 2 failures, the log shows 3.
6. Incidental positive: the mutant log is the best in-snapshot evidence of overall health —
   99/102 pass *under the mutation*, and the fmt matrix (`122 rows, all match gc`) plus freeze
   (`37 signatures verified`) lines pass there too.

### 1.3 Loop-capture red-green + 114/114 — P0-2 (no logs; prose only)

1. The snapshot contains **no red-green log and no 114/114 log**. The claims
   ("8 corpus programs fail on pre-fix tree with exactly the shared-slot values, all pass with
   fix", "Full suite: 114 tests, 0 failures", "self-attack 4/4") rest on prose in
   `evidence/CHECKPOINT.md` (~12:25 entry) and `evidence/loopcap/COMPLETE.md`.
2. I independently derived gc values and shared-slot (pre-fix) predictions for all 8 programs
   by running gc equivalents (§3, table below). Results:

   | program | gc (oracle) | pre-fix shared-slot prediction | red-green? |
   |---|---|---|---|
   | cf12 body var | 100 | 200 (5×40) | yes |
   | cf13 header var | 10 | 25 (5×5) | yes |
   | cf14 nested | 99 | 198 (9×22) | yes |
   | cf15 range | 60 | 90 (3×30) | yes |
   | cf16 writethrough | 3 | 3 (no closure → no divergence) | no — regression guard |
   | cf17 goto | 3, k=3 | 6, k=3 | yes |
   | cf18 defer | 10 | 25 (5×5) | yes |
   | cf19 break | 20, k=5 | 40, k=5 | yes |
   | pn13 defer storm | 49995000 | 100000000 (10000×10000) | yes |

3. Two claimed pre-fix values are corroborated in-prose: pn13=100000000 (CHECKPOINT.md 12:00
   entry) and cf15=90 (same entry: "gc=60, graalgo=90" as a mid-fix failure — the shared-slot
   value). The other six are derivable but unlogged.
4. Finding: the red-green *values* are exactly right, but the red-green *runs* are unevidenced.
   A reviewer cannot check "fail on pristine with exactly the shared-slot values" from the
   snapshot — the brief asked me to audit those logs and they do not exist.
5. P2-2 — inconsistent pre-fix pn13 values across docs: the M5 known-limitation note records
   `gc=49995000 vs graalgo=99990000`, the loopcap note records `graalgo=100000000`. Only the
   latter is derivable from the snapshotted corpus (10000 iterations × final i=10000). 99990000
   (= 9999×10000) matches no program in the snapshot; presumably an ad-hoc M5 probe with a
   different bound, but it is not identified as such.

### 1.4 WS-G evidence

1. `evidence/ws-g/mutant1_append_tailcopy.log` — prose-only ("KILLED (bi01_append_growth logic)"),
   no run output. Plausible (dropping the tail copy breaks growth), but asserted, not shown. P2-3.
2. `evidence/ws-g/mutant2_fmt_x_upper.log` — **P1-2, false kill attribution.** It claims
   `Sprintf("%x","é")` → `"C3A9"` is "KILLED (matrix row %x/string)". The matrix's hex-path
   string rows are `%x`, `%#x`, `% x` — all on `string:hi` → `6869`/`0x6869`/`68 69`
   (matrix.tsv:59-61). "hi" = 0x68 0x69 contains no a-f hex digits, so LDIGITS→UDIGITS is
   unobservable through every one of them; the mutant (in `Fmt.fmtString`, case 'x') almost
   certainly **survives** the matrix. The matrix has no non-ASCII `%x` string case at all —
   the exact case the mutant needs. The kill is not re-derivable and, by construction of the
   matrix, likely false.
3. fmt matrix methodology — 122 lines confirmed (`wc -l`), but 2 are exact duplicates
   (`% d`/`int:42`, `%v`/`bytes:6869` — `sort | uniq -d`), so 120 unique cases. Verb/type/flag
   spread is genuinely good (%v×16, %d×13, %T×7, %x×6, %s×6, %q×6, %#v×4, flags/width/precision/
   indexed args covered; types int×37, float64×25, string×17, error×9, bytes×7, nil×5,
   complex128×5, slices, maps, bool, ptr). I spot-checked 8 tricky rows against live gc — all match, including
   `%d` on error → `&{%!d(string=oops)}`, `%x` on "é" → `c3a9`, `%#v` on `[]byte("hi")` →
   `[]byte{0x68, 0x69}`, `%w` → `%!w(*errors.errorString=&{oops 7})`, `%U` on error →
   `&{%!U(string=oops)}`, `%x`/`%d` on `[]int{1,2,3}` → `[1 2 3]`. P2-4: the test sources are
   not in the snapshot, so I cannot determine whether the test compares against live gc per row
   or against the static tsv's expected column (the log line "(fmt matrix: 122 rows, all match
   gc)" is ambiguous). If tsv-only, "122/122 vs gc" is indirect — though the tsv itself is
   gc-accurate per my spot-checks.
4. Freeze "37/37" — **P1-3, unverifiable denominator.** The PASS is evidenced (mutant log:
   "(freeze v1: 37 signatures verified)"). But the signature list being verified is not in the
   snapshot, and the doc does not reconcile: prose says "58 functions + 11 builtin nodes",
   the tables actually contain 65 `` `public`` signature rows + 11 node rows. No combination I
   can construct from the doc (REAL-only = 44, minus D4-fmt = 37 is one speculation) is stated.
   37/37 is true relative to the test's own list; whether that list covers the frozen surface
   cannot be checked from the snapshot.

### 1.5 Self-attack 4/4 (cf20–cf23)

1. The four programs exist and are well-designed: cf20 (inner init refs outer header + capture
   of both — phase-stack resolution), cf21 (per-iteration cell mutation isolation), cf22
   (shadowed header oid), cf23 (multi-header capture). Their PASS is prose-claimed only, no log.
2. Coverage verdict on the attacks themselves: they exercise the phase stack, oid-distinct
   shadowing, and capture propagation — the three mechanisms the design docs call load-bearing.
   They do not exercise per-iteration-vs-per-closure (see P1-4).

## 2. Findings

### P0 (false or uncheckable green claim)

- **P0-1** — `evidence/m5_green.log` does not evidence "102/102 green"; it is a 70-pass run
  terminated mid-suite (pn06 oracle SIGTERM, `make Terminated`). The cited green artifact
  (`/tmp/m5_verify.log`) is absent from the snapshot. M5 acceptance's headline claim is
  uncheckable from the review basis. (The mutant log incidentally shows 99/102 under mutation —
  good secondary signal, not the claimed artifact.)
- **P0-2** — Loop-capture "8 red-green programs fail on pristine / pass with fix" and "114/114"
  have no log evidence in the snapshot. The predicted pre-fix values are exactly derivable and
  two are corroborated in-prose, but the runs themselves cannot be audited.

### P1 (coverage gap that could hide a bug, or misleading evidence)

- **P1-1** — Third, unexplained failure (pn14) in `m5_mutant_red.log`; no in-snapshot baseline.
  (§1.2.5)
- **P1-2** — WS-G mutant2 kill claim is not credible; the mutant likely survives the matrix.
  (§1.4.2) This also exposes a real matrix gap: no non-ASCII `%x` string row.
- **P1-3** — Freeze "37/37" denominator undefined in snapshot; doc counts (58+11 prose,
  65+11 tables) don't reconcile with 37. (§1.4.4)
- **P1-4** — The 8 loopcap tests pin per-iteration-vs-shared but **not per-iteration-vs-per-closure**.
  A subtly wrong implementation that allocates a fresh box per funclit creation (seeded from the
  current value) passes all 8 red-green tests yet breaks same-iteration aliasing. Distinguishing
  case: adv02 (§3) — gc `0 1 1 10 11 11`; per-closure impl gives `0 1 0 10 11 10`.
- **P1-5** — Struct-typed for-header var: post must read AND write S_outer (DESIGN_WIRING.md
  correction #1 — the first draft's "post reads S_iter" was wrong). No corpus test pins this;
  the draft-1 bug would infinite-loop (cond reads stale S_outer). adv16. (§3)
- **P1-6** — Struct field write-through (`p.n = 10` in body must reach S_outer via GoBox.outer):
  cf16 covers only the int case; the struct path is distinct. adv17. (§3)
- **P1-7** — Range-over-map capture untested (cf15 is slice-only; RangeMapNode is a separate
  class — I verified its per-iteration `new GoBox` in code, but no test guards it). adv04. (§3)
- **P1-8** — While-style loop capture untested (`for cond {}` takes the same buildFor path —
  verified `f.opt()` handling — but zero coverage). adv05. (§3)
- **P1-9** — Closure invoked *during* its iteration untested (pins BoxRefreshNode seeding order:
  S_iter must be seeded before the body runs). adv03. (§3)
- **P1-10** — Inner-loop closure capturing the *outer body* var untested (cf14: inner body;
  cf20: outer header via init + headers). adv01. (§3)
- **P1-11** — Cross-frame panic unwind with per-frame defers untested (pn12 unwinds 10000
  frames but defers exist only in main). adv07. (§3)
- **P1-12** — recover-then-set-named-result (the canonical panic-to-value translation) untested:
  pn03 has no panic, pn07 has no named write after recover. adv08. (§3)
- **P1-13** — Defer registered *during* unwinding (+ re-panic replacing the in-flight panic)
  untested. adv09. Runtime not in snapshot; likely handled by stack discipline, but unpinned. (§3)
- **P1-14** — Panic/recover per loop iteration combined with loop-var capture (M5×loopcap
  interplay; shared-slot bug would give 95959 instead of 91939). adv06. (§3)
- **P1-15** — `runtime.Goexit` in a nested call untested (pn10/pn11 are Goexit-in-main only).
  adv11: gc prints `g-defer`, `main-defer`, then fatal deadlock, exit 2. (§3)
- **P1-16** — fmt verbs on boxed loop vars untested. Code reasoning says handled (BoxReadNode
  unboxes by kind — verified; FmtCallNode.readArg dispatches on `child.kind().slotKind()` —
  verified), but the fmt/callable boundary is unpinned. adv10. (§3)

### P2 (minor / process)

- **P2-1** — Mutant logs record no mutation description/diff; WS-G mutant logs are prose-only.
- **P2-2** — 99990000 vs 100000000 pre-fix pn13 values across docs; only the latter derivable.
- **P2-3** — mutant1 kill asserted without run output (plausible but unshown).
- **P2-4** — fmt test methodology (live gc vs static tsv) not determinable from snapshot.
- **P2-5** — `corpus/controlflow/README.md` program table stops at cf11; cf12–cf23 undocumented,
  and its mutation story cites `hidden_files/m4_red/` logs absent from the snapshot.
- **P2-6** — Smaller semantic gaps, each with an adv program in §3: body var shadowing the
  header var (adv13), continue+capture (adv14), range-over-string capture (adv15),
  recover() in a nested call inside a deferred func → nil (adv12), panic(nil) *fatal*
  rendering (only the recover path is pinned by pn06), deferring a nil func value
  (DeferNode.java documents a defer-time panic; no corpus test).
- **P2-7** — Oracle robustness: `m5_green.log` shows the gc oracle itself SIGTERMed (exit 143)
  and the failure attributed to the test. If the harness kills the oracle on timeout, slow
  machines can flake any differential test; worth confirming the timeout/kill policy.

## 3. New adversarial programs (gc-pinned, recommended corpus additions)

All run under stock go1.27.1 (`GOTOOLCHAIN=local` default env). Presented as-runnable with
prints (what I verified); on landing, adapt to `// observe:` trailers + `// probe` anchors per
`corpus/panic/README.md` / `corpus/controlflow/README.md` conventions. "Covered?" = present in
the snapshot corpus. "Design handles?" = my code reasoning from snapshot sources (no execution).

### adv01 — inner-loop closure over OUTER BODY var (P1-10)
Not covered (cf14: inner body; cf20: headers). Design: prescan collects outer bodyDecls and the
funclit refs; phase stack resolves the oid to the outer loop's BODY phase → S_iter. Likely handled.
```go
package main

import "fmt"

// Inner-loop closure captures OUTER body var: propagation must cross two
// loop levels for body vars (cf14 only covers inner body var).
func main() {
	var fns [6]func() int
	k := 0
	for i := 0; i < 3; i++ {
		y := i * 100
		for j := 0; j < 2; j++ {
			fns[k] = func() int { return y + j }
			k++
		}
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	fmt.Println(sum, k)
}
```
gc: `603 6`.

### adv02 — two closures, one iteration cell (P1-4 — the per-closure distinguisher)
Not covered. This is the case the 8 red-green tests cannot distinguish: a fresh-box-per-funclit
implementation passes cf12–cf19 but fails here (`0 1 0 10 11 10`). Design (decl allocates one
GoBox per iteration execution) handles it.
```go
package main

import "fmt"

// Two closures created in the SAME iteration must share one per-iteration
// cell: distinguishes per-iteration cells from per-closure cells.
func main() {
	var fns [6]func() int
	for i := 0; i < 3; i++ {
		x := i * 10
		fns[2*i] = func() int { return x }
		fns[2*i+1] = func() int { x++; return x }
	}
	a := fns[0]()
	b := fns[1]()
	c := fns[0]()
	d := fns[2]()
	e := fns[3]()
	f := fns[2]()
	fmt.Println(a, b, c, d, e, f)
}
```
gc: `0 1 1 10 11 11`.

### adv03 — closure invoked during its iteration (P1-9)
Not covered. Pins that the BoxRefreshNode prologue seeds S_iter before the body executes.
```go
package main

import "fmt"

// Closure invoked DURING its iteration: pins BoxRefreshNode seeding order
// (S_iter must be seeded before body executes).
func main() {
	sum := 0
	bsum := 0
	for i := 0; i < 5; i++ {
		f := func() int { return i }
		sum += f()
		x := i * 3
		g := func() int { return x }
		bsum += g()
	}
	fmt.Println(sum, bsum)
}
```
gc: `10 30`.

### adv04 — range-over-map capture (P1-7)
Not covered (cf15 slice-only; cf08 map without capture). I verified RangeMapNode allocates
`new GoBox` per iteration under keyDefine/valDefine and writes into the existing cell on `=`
(snapshot `java/frontend/RangeMapNode.java:94,108`), and prescanRangeBoxes feeds it — likely
handled, but a separate node class with zero capture coverage.
```go
package main

import "fmt"

// Range-over-map capture: RangeMapNode has a separate boxing path from
// RangeSliceNode (cf15 covers slice only). Sums keep it order-independent.
func main() {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	var fns []func() int
	var keys []func() string
	for k, v := range m {
		fns = append(fns, func() int { return v })
		keys = append(keys, func() string { return k })
	}
	sum := 0
	ksum := 0
	for _, f := range fns {
		sum += f()
	}
	for _, kf := range keys {
		ksum += len(kf())
	}
	fmt.Println(sum, ksum, len(fns))
}
```
gc: `6 3 3`.

### adv05 — while-style loop capture (P1-8)
Not covered. buildFor handles missing init/post (verified `f.opt()` guards), body var `x` is
boxed via bodyDecls while outer `i` stays a plain shared capture — both behaviors in one program.
```go
package main

import "fmt"

// While-style loop: body var x gets per-iteration cells; outer var i is a
// plain captured var (one shared cell). Both in one program.
func main() {
	var fns [3]func() int
	var gns [3]func() int
	k := 0
	i := 0
	for i < 3 {
		x := i * 7
		fns[k] = func() int { return x }
		gns[k] = func() int { return i }
		k++
		i++
	}
	xs, is := 0, 0
	for _, f := range fns {
		xs += f()
	}
	for _, g := range gns {
		is += g()
	}
	fmt.Println(xs, is)
}
```
gc: `21 9`.

### adv06 — panic/recover per iteration + capture (P1-14)
Not covered. M5×loopcap interplay: the deferred closure captures header `i`; shared-slot bug
gives 95959. Exercises per-iteration defer chains, recover-in-defer, and S_iter snapshots.
```go
package main

import "fmt"

// Panic/recover across loop iterations; the deferred closure captures the
// loop var (M5 x loopcap interplay). Buggy shared-slot impl observes i=5.
func main() {
	log := 0
	for i := 0; i < 5; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log = log*10 + i
				}
			}()
			if i%2 == 1 {
				panic("odd")
			}
			log = log*10 + 9
		}()
	}
	fmt.Println(log)
}
```
gc: `91939`.

### adv07 — cross-frame unwind with per-frame defers (P1-11)
Not covered (pn12 unwinds 10000 frames but only main has defers). Runtime (DeferChain) not in
snapshot — cannot verify by code; likely handled.
```go
package main

import "fmt"

// Cross-frame panic unwind: defers at every frame run LIFO during unwind,
// recover in main observes the original panic value.
func main() {
	fmt.Println(wrap())
}

var order int

func g(n int) {
	defer func() { order = order*10 + n }()
	if n > 0 {
		g(n - 1)
	} else {
		panic("boom")
	}
}

func wrap() (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = fmt.Sprintf("%d %v", order, r)
		}
	}()
	g(3)
	return "unreached"
}
```
gc: `123 boom`.

### adv08 — recover then set named result (P1-12)
Not covered (pn03: no panic; pn07: no named write after recover). The canonical panic-to-value
translation.
```go
package main

import "fmt"

// Recover in a defer, then set the named result: the classic
// panic-to-error-value translation. Not covered by pn03 (no panic) or
// pn07 (no named-result write after recover).
func f() (r int) {
	defer func() {
		if recover() != nil {
			r = 99
		}
	}()
	panic("x")
}

func main() {
	fmt.Println(f())
}
```
gc: `99`.

### adv09 — defer registered during unwinding (P1-13)
Not covered. Trace: p1 → d3 ("A") → d2 ("B", panic p2) → inner defer ("C") → d1 recovers ("!").
Runtime not in snapshot; stack discipline likely handles it, but unpinned.
```go
package main

import "fmt"

// Defer registered DURING unwinding runs before the remaining earlier
// defers; re-panic inside replaces the in-flight panic.
func wrap() (s string) {
	defer func() {
		if recover() != nil {
			s += "!"
		}
	}()
	defer func() {
		defer func() { s += "C" }()
		s += "B"
		panic("p2")
	}()
	defer func() { s += "A" }()
	panic("p1")
}

func main() {
	fmt.Println(wrap())
}
```
gc: `ABC!`.

### adv10 — fmt on boxed loop vars (P1-16)
Not covered. Code reasoning says handled: BoxReadNode unboxes by the variable's kind
(verified `java/frontend/BoxReadNode.java`), and FmtCallNode.readArg dispatches on
`child.kind().slotKind()` (verified `java/frontend/FmtCallNode.java:98-108`) — but the
fmt/callable boundary has zero coverage.
```go
package main

import "fmt"

// fmt verbs applied to boxed loop vars: in-body reads and captured reads
// must both see through the GoBox.
func main() {
	var fns [3]func() string
	for i := 0; i < 3; i++ {
		x := i * 11
		fns[i] = func() string { return fmt.Sprintf("%d/%x/%q", x, x, x) }
		fmt.Printf("iter %d: x=%d\n", i, x)
	}
	for _, f := range fns {
		fmt.Println(f())
	}
}
```
gc:
```
iter 0: x=0
iter 1: x=11
iter 2: x=22
0/0/'\x00'
11/b/'\v'
22/16/'\x16'
```

### adv11 — Goexit in a nested call (P1-15)
Not covered (pn10/pn11: Goexit-in-main only). `// panic:`-style fatal program: exit code 2.
```go
package main

import (
	"fmt"
	"runtime"
)

// Goexit in a NESTED call: defers in g run, then defers in main run, then
// the fatal deadlock error. pn10/pn11 cover Goexit-in-main only.
func g() {
	defer fmt.Println("g-defer")
	runtime.Goexit()
	fmt.Println("unreachable-g")
}

func main() {
	defer fmt.Println("main-defer")
	g()
	fmt.Println("unreachable-main")
}
```
gc stdout: `g-defer`, `main-defer`; stderr: `fatal error: no goroutines (main called runtime.Goexit) - deadlock!`; exit 2.

### adv12 — recover in a nested call inside a deferred func (P2-6)
Not covered (pn08 covers top-level and defer-arg positions). recover must be called directly
by the deferred function; the nested call observes nil.
```go
package main

import "fmt"

// recover() must be called DIRECTLY by a deferred function: a nested call
// inside the deferred function observes no panic in flight (nil).
func wrap() (s string) {
	defer func() { recover() }()
	defer func() {
		func() {
			if recover() == nil {
				s += "nil"
			} else {
				s += "val"
			}
		}()
	}()
	panic("x")
}

func main() {
	fmt.Println(wrap())
}
```
gc: `nil`.

### adv13 — body var shadows the header var (P2-6)
Not covered (cf22 covers header shadowing across nested loops). go/types oids keep them
distinct (per DESIGN_WIRING.md); the closure must capture the inner per-iteration cell.
```go
package main

import "fmt"

// Body var shadows the for-header var: the closure must capture the inner
// oid (per-iteration), not the outer header oid.
func main() {
	var fns [3]func() int
	k := 0
	for i := 0; i < 3; i++ {
		i := 100 + i
		fns[k] = func() int { return i }
		k++
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	fmt.Println(sum)
}
```
gc: `303` (capturing the outer oid would give 9).

### adv14 — continue with capture (P2-6)
Not covered (cf19 covers break). Closures keep cells only from iterations that ran the capture.
```go
package main

import "fmt"

// continue with capture: closures keep cells only from iterations that
// ran the capture point (cf19 covers break; continue untested).
func main() {
	var fns [5]func() int
	k := 0
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			continue
		}
		x := i * 2
		fns[k] = func() int { return x }
		k++
		if i == 7 {
			break
		}
	}
	sum := 0
	for n := 0; n < k; n++ {
		sum += fns[n]()
	}
	fmt.Println(sum, k)
}
```
gc: `32 4` (x = 2,6,10,14).

### adv15 — range-over-string capture (P2-6)
Not covered (cf07 ranges a string without capture). RangeStringNode is a third boxing path.
```go
package main

import "fmt"

// Range-over-string capture: RangeStringNode boxing path (cf07 ranges a
// string without capture; cf15 captures a slice var).
func main() {
	var fns []func() string
	for _, r := range "héllo" {
		fns = append(fns, func() string { return string(r) })
	}
	s := ""
	for _, f := range fns {
		s += f()
	}
	fmt.Println(s, len(fns))
}
```
gc: `héllo 5` (shared-slot bug would give `ooooo`).

### adv16 — struct-typed for-header var: post writes S_outer (P1-5)
Not covered. Pins DESIGN_WIRING.md correction #1: the first draft had post read S_iter, which
is wrong when the body cell holds a mutated copy. With the draft-1 bug, cond reads a stale
S_outer forever → hang; with a milder variant (post bumps S_iter only), closures would observe
corruption. (Note: `for p := (P{0}); …` needs the parens — composite literal in for-init.)
```go
package main

import "fmt"

type P struct{ n int }

// Struct-typed for-header var: post (p.n++) must read AND write S_outer.
// DESIGN_WIRING.md correction #1: the first draft had post read S_iter,
// wrong when the body cell holds a mutated copy.
func main() {
	var fns [3]func() int
	k := 0
	for p := (P{0}); p.n < 3; p.n++ {
		fns[k] = func() int { return p.n }
		k++
	}
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	fmt.Println(sum, k)
}
```
gc: `3 3`.

### adv17 — struct field write-through to S_outer (P1-6)
Not covered (cf16 is the int case; struct fields take the write-through path via GoBox.outer).
Without write-through: post sees p.n=2 → count=5, last=4.
```go
package main

import "fmt"

type P struct{ n int }

// Body struct-field write to a for-header var must write through to S_outer
// (cf16 covers the int case; struct fields take the write-through path via
// GoBox.outer).
func main() {
	count := 0
	last := -1
	for p := (P{0}); p.n < 5; p.n++ {
		if p.n == 2 {
			p.n = 10
		}
		count++
		last = p.n
	}
	fmt.Println(count, last)
}
```
gc: `3 10`.

## 4. What I could NOT verify (snapshot limits — did not build the live tree)

1. Anything requiring execution of the interpreter (M6 owns the live tree; also out of scope
   for a read-only review).
2. Test sources (absent): fmt matrix live-gc-vs-tsv methodology; FreezeSignatureTest's 37-list;
   the `// panic:` trailer comparison behind the pn14 anomaly (§1.2.5).
3. Runtime sources (absent): DeferChain unwind ordering (adv09), Goexit across frames (adv11),
   RecoverBuiltinNode's caller check (adv12).
4. `/tmp/m5_verify.log` — the cited 102/102 evidence; absent.
5. Red-green and 114/114 run logs — absent (§1.3).
6. Whether the pre-loopcap tree truly had zero loop-capture corpus programs: the pristine set
   preserves only pn13 + java files, so the "zero before" claim rests on worker testimony, not
   on snapshot artifacts. (Consistent with the current corpus, where loop captures appear only
   in cf12–cf23 — verified by grep.)

## 5. Verdict — do the tests + evidence prove what they claim?

1. **M5 "102/102 green": NOT proven from the snapshot.** The filed `m5_green.log` is a
   terminated 70-pass run; the cited `/tmp/m5_verify.log` is missing. This is an evidence
   filing failure, not necessarily a code failure — the mutant log's 99/102-under-mutation is
   strong secondary signal — but the headline acceptance claim is uncheckable as filed.
2. **M5 mutation "pn07/pn09 fail as predicted": PROVEN checkable.** Both predicted values
   (712, 32) are exactly derivable from the corpus + the described bug, and the log matches
   them. This is the strongest evidence in the snapshot. Downgraded only by the unexplained
   pn14 third failure (P1-1) and the unrecorded mutation diff (P2-1).
3. **Loop-capture "8 red-green programs": values check out, runs unevidenced.** Every claimed
   pre-fix value is exactly derivable; two are corroborated in-prose. But no red-green log
   exists to audit, and — more importantly for adequacy — the 8 tests cannot distinguish
   per-iteration cells from per-closure cells (P1-4, adv02). The fix is pinned against the bug
   it had, not against its plausible over-eager neighbor.
4. **"114/114" and "self-attack 4/4": prose-only.** The attack programs (cf20–cf23) are
   well-chosen for the phase-stack/oid mechanisms, but their runs are unlogged.
5. **fmt "122/122": matrix is sound and gc-accurate** (spot-checked), but the "vs gc"
   methodology is ambiguous from the snapshot, and the WS-G mutant2 kill claim is not credible
   (P1-2) — the mutation evidence for the fmt overlay is weaker than stated.
6. **Freeze "37/37": pass evidenced, coverage undefined** (P1-3).
7. **Adequacy overall:** the corpus is genuinely adversarial in style (the pn07/pn09/pn13
   designs are excellent traps), and the loopcap set closes the documented M5 gap. But §2
   lists 16 P1 gaps — notably cross-frame per-frame defers, recover→named-result, defer-during-
   unwind, nested Goexit, per-iteration/per-closure aliasing, struct-header post semantics, and
   the untested RangeMap/RangeString/while-loop boxing paths. None is exotic; all are within
   the claimed Phase-1 language surface. Recommend landing the §3 programs (adapted to
   `// observe:`) before M10 closeout, and re-filing the missing green/red-green logs — an
   acceptance claim whose cited log lives in `/tmp` is not an archived claim.
