# Wave 2 — gc's liveness-driven allocation reuse (`runtime.freegc`)

**Track:** 4 (Go-adjacent), wave 2, targeted investigation. **Date:** 2026-10-04.
**Question (program owner):** gc is reportedly "playing with" reusing heap objects proven
dead — pin down the EXACT status in golang/go.
**Answer in one line:** It is real, merged, and active — `GOEXPERIMENT=runtimefreegc`,
tracking issue **golang/go#74299**, driven by PJ Malloy (@thepudds) — but it is
**off-by-default in Go 1.26+**, static-proof-only, keeps the allocator round-trip, and
already has filed soundness bugs. It races our backlog item
("Dead-object slot reuse (allocation hoisting)", PHASES.md §7) **head-on in the
growslice-intermediate sub-case only**; our stronger forms (allocator-bypass slot
reuse, speculative JIT reuse with guard/deopt) are ones gc structurally cannot do.

**Verdict placement:** RACING head-on in one sub-case; our edges survive everywhere
else. Details in §5.

**Discipline notes:** every issue/CL number below was fetched and verified on
2026-10-04 (wave-1 rule: recalled numbers proved unreliable). No SURV- IDs assigned
(coordinator owns that). No edits to SPEC.md / PHASES.md / registry.

---

## §1 Investigation trail (sources fetched)

| # | Source | Fetched | Key content |
|---|--------|---------|-------------|
| 1 | golang/go#74299 "runtime, cmd/compile: add runtime.freegc and runtime.freegcTracked to reduce GC work (GOEXPERIMENT=runtimefreegc)" | 2026-10-04 (issue body, incl. the Nov 27 2025 status UPDATE) | Tracking issue: motivation ("chicken and egg" with #72036), prototype numbers, merged-CL list |
| 2 | golang/proposal `design/74299-runtime-freegc.md` (PJ Malloy, Jan 2025; via go.dev/design/74299-runtime-freegc) | 2026-10-04, all 1152 lines | Full design: runtime APIs, compiler alias analysis, makeslice tracking, obstacles, benchmarks |
| 3 | CL 673695 "runtime: add runtime.freegc to reduce GC work" (merged; GitHub commit fecfcaa4) | 2026-10-04 | Runtime primitive + commit message with json/v2 numbers |
| 4 | CL 710015 "cmd/compile: introduce alias analysis and automatically free non-aliased memory after growslice" (merged; GitHub commit 4879151d) | 2026-10-04 | `growsliceNoAlias`, `src/cmd/compile/internal/escape/alias.go`, append benchmarks (~2x, ~3x memory) |
| 5 | CL 720120 "runtime: set GOEXPERIMENT=runtimefreegc to disabled by default" (merged 2025-11-12; GitHub commit 5a347b77) | 2026-10-04 | "The plan has been for GOEXPERIMENT=runtimefreegc to be disabled by default for Go 1.26, so here we disable it." — **verified** |
| 6 | CL 700235 "runtime: add GOEXPERIMENT=runtimefree" (merged) | via #74299 UPDATE | Original experiment name; renamed to `runtimefreegc` by CL 720120 |
| 7 | golang/go#80697 "cmd/compile: runtimefreegc misclassifies append in a multi-assignment as no-alias" | 2026-10-04 (body only; no comments rendered) | **Soundness bug**: premature NoAlias commit in `r, old = append(r, n), r` → use-after-free class |
| 8 | golang/go#79909 "cmd/compile: slice allocation reuses stack backing store incorrectly" (josharian) | 2026-10-04 | **Soundness bug**: range-over-slice retains backing store; prints 1924 not 1234 in 1.26/tip |
| 9 | golang/go#79972 (watchflakes auto-file: `Test/codegen/append_freegc.go` codegen failure) | 2026-10-04 | Test fallout from the #79909 fix; closed by CL 789980 |
| 10 | golang/go commit 61202f57 "test/codegen: update runtimefreegc test for slice backing store fix" (CL 789980, auto-submit thepudds, 2026-06-11) | 2026-10-04 | "CL 789060 fixed a case in the backing store analysis for range over slice statements." "Updates #79909 / Fixes #79972" |
| 11 | golang/go#79631 "cmd/compile: GOEXPERIMENT=runtimefreegc go build does not work if ./make.bash was built without" (community report, ~2026-05) | via search snippet | Build-integration bug; reporter showed `growsliceNoAlias` in `-S` output with the experiment on |
| 12 | golang/go#76930 "x/build: add a builder for GOEXPERIMENT=runtimefreegc" (thepudds) | via search snippet | @mknyszek suggested a CI builder "to help with coverage"; merged into x/build |
| 13 | golang/go#72036 "cmd/compile: teach escape analysis to conditionally stack alloc interface method call parameters like a slice in w.Write(b)" (thepudds, "dynamic escapes", WIP) | via search snippet | The motivating sibling: static EA improvements stall because dynamic-size slices heap-allocate anyway — the "chicken and egg" freegc is meant to crack |
| 14 | Alex Edwards, "Go Experiments Explained" (~2026-06) | via search snippet | `RuntimeFreegc`: "Allows immediate reuse of memory without waiting for a GC cycle when safe to do so" — off-by-default, under evaluation since Go 1.26, "see #74299 for status" |
| 15 | GoFree (Peng, Zhang, Ernst, Chen, Ding), CGO 2025 | abstract fetched 2026-10-04 | Independent academic precedent: compiler-inserted explicit freeing; 14% of heap deallocated, −7% GC frequency, −13% GC time, −2% wall-clock on 6 programs (**their** numbers, not ours) |
| 16 | PHASES.md §7 backlog item "Dead-object slot reuse (allocation hoisting)" | read 2026-10-04 | Our own item — the head-on overlap, quoted in §5 |

**Searches run (negative results):** no GopherCon talks, no blog posts, no golang-nuts/dev
threads on freegc found — public discussion lives in the issue + CLs + one community
bug report (#79631). Attempted Gerrit JSON API query for open freegc CLs: throttled by
the fetch service (transient); not retried per tool policy — status below is from the
merged-CL list + dated commits + issues, which is sufficient.

**Status summary:**
- **Driver:** PJ Malloy (@thepudds) — design-doc author, issue author, CL-stack author;
  early review feedback from Michael Knyszek (Go runtime team), who also suggested the
  x/build coverage builder (#76930). (Employment/affiliation not verified beyond
  "well-known Go contributor"; not needed for the verdict.)
- **Shipped state:** merged behind `GOEXPERIMENT=runtimefreegc`, **disabled by default
  since Go 1.26** (CL 720120, 2025-11-12, verified). Still off-by-default in tip: the
  #80697 reproducer (Jul 2026) builds with `X:runtimefreegc` explicitly enabled on
  go1.28-devel.
- **Activity:** ACTIVE. Merged-CL update Nov 27 2025; backing-store fix CL 789060 +
  test update CL 789980 (auto-submit thepudds) 2026-06-11; soundness bug #80697 filed
  2026-07-29 against go1.28-devel. Not stalled, not declined.

---

## §2 Exact mechanism and current scope

### 2.1 What the compiler proves (CL 710015, `src/cmd/compile/internal/escape/alias.go`)

A new **alias analysis** walks the IR from slice declarations and proves whether a
slice is aliased. Design rules, quoted from the CL 710015 commit message:

> "The high-level approach here is we step through the IR starting from a slice
> declaration and look for any operations that either alias the slice or might do
> so, and **treat any IR construct we don't specifically handle as a potential
> alias (and therefore conservatively fall back to treating the slice as aliased**
> when encountering something not understood)."

Loop handling is explicit and delicate: "an alias in the body of a loop causes all
the appends in that same loop body to be marked aliased, even if the aliasing
occurs after the append in the IR" (with correct inner/outer-loop nesting).
The analysis tracks aliasing/renamings across inlining and recognizes some
unstructured-loop gotos. When the slice is proven unaliased at an `append`, the
compiler substitutes `runtime.growsliceNoAlias` for `runtime.growslice`.

### 2.2 What the runtime does (CL 673695)

Four entry points (design doc §"Runtime API overview"):

- `freegc(ptr unsafe.Pointer, size uintptr, noscan bool) bool` — "records that a
  heap object is reusable and available for immediate reuse in a subsequent
  mallocgc allocation, without needing to wait for the GC cycle to progress."
  Freed objects go onto **free lists in the current P's mcache** and are
  "immediately reusable for the next allocation in the same span class"
  (CL 673695 commit message). It is engineered to "coexist and cooperate with the
  concurrently running garbage collector" — the design doc requires robustness
  for sequences like "first alloc of a given pointer, GC observes and queues the
  pointer, runtime.free, GC dequeues pointer for scanning, runtime re-uses the
  same pointer, all within a single GC phase."
- `growsliceNoAlias(oldPtr, ...)` — like `growslice`, but frees the old backing
  array after growth completes, when the old storage is "logically dead." Works
  even if the final slice **escapes** (e.g. returned from the function) — only
  the *intermediate* arrays are freed.
- `makeslicetracked64(et, len, cap, trackedObjs)` + `freegcTracked(trackedObjs)`
  — for `make` with non-constant size (>32 bytes → heap) where escape analysis
  proves function-scoped lifetime. The compiler statically counts `make` sites,
  reserves a stack array of `trackedObj`, and inserts one `freegcTracked` at
  function exit (currently an open-coded defer; plan is to drop the defer).
  Crucially, "tracking does not keep the tracked memory alive" (GC zeroes
  tracked pointers as cycles progress — a Keith Randall suggestion, weak-pointer-like),
  avoiding the liveness pessimization that a plain `freegc(ptr)` call would
  introduce by keeping its argument alive.
- A separate CL (stated in the design doc; merge status **not** verified) frees
  logically-dead bucket storage during builtin map growth/split.

### 2.3 Allocation patterns covered today vs. not

| Pattern | Status |
|---|---|
| `append` in a loop building a slice (`s = append(s, g(x))`), intermediate backing arrays, final slice may escape | **Covered** (CL 710015, merged). Their benchmark claim: up to ~2x faster, up to ~3x memory reduction (their numbers) |
| `strings.Builder`/`bytes.Buffer`-style repeated growth | Covered via the same mechanism (the ~2x `strings.Builder` prototype was the motivating demo; the doc hopes the compiler will do it automatically, "roughly 2 of 3 steps" done) |
| `make([]T, n)` with dynamic `n`, function-scoped lifetime | Covered by `makeslicetracked64`/`freegcTracked` (freed at scope exit), **except** inside `for` loops or unstructured-loop gotos — currently skipped |
| Same allocation site inside a loop allocating per iteration, reused across iterations | **NOT covered — explicitly future work.** Design doc: "a modest extension (and the current plan) is to have the compiler instead use a different runtime entry point for freeing in loops… to allow heap-allocated slice memory to be reclaimed on each loop iteration when it returns to the same allocation site… (The compiler can already prove it is safe and the runtime entry point is prototyped.)" |
| More than slices (arbitrary heap objects) | Future ("Slices are a natural first step, but the large majority of the work implemented is independent of whether the heap object represents a slice") |
| Objects with finalizers / `AddCleanup` ("specials") | **Excluded**: "the caller must know that ptr's object has no specials, such as might have been created by a call to SetFinalizer or AddCleanup." freegcTracked support for specials is a TODO (bitmap of objIndexes, not implemented) |
| Objects < 16 bytes or > 32 KiB − MallocHeaderSize | `freegc` is a no-op (size threshold) |
| Stdlib hand-insertion / user API | Explicitly refused: "there is no intent to modify the standard library to have explicit calls to runtime.freegc, and of course such an ability would never be exposed to end-user code" |

---

## §3 Obstacle / failure list (from the issue tracker + design doc)

### 3.1 Filed bugs (fetched)

**B1 — #80697 (OPEN, filed 2026-07-29, go1.28-devel + `X:runtimefreegc`): "runtimefreegc
misclassifies append in a multi-assignment as no-alias."** A *soundness* bug, not a
perf nit. Reproducer:

```go
func f(n int) ([]int, []int) {
	r := make([]int, n, n)
	var old []int
	r, old = append(r, n), r   // len==cap, so append must grow
	return r, old
}
```

The compiler emits `CALL runtime.growsliceNoAlias(SB)`. The reporter's diagnosis,
quoted: "The right-hand-side expressions of the multi-assignment are evaluated
before the assignments are performed. Therefore, the second RHS retains the old
slice header in `old`. The alias analysis currently **records the first
self-append as a NoAlias append immediately. When the later RHS `r` invalidates
the candidate, the already-recorded decision cannot be revoked.** … growsliceNoAlias
may release the old pointer-free backing array even though `old` still refers to
it." Expected: "The NoAlias decision should only be committed after all operands
of the assignment have been analyzed and the slice is still a valid candidate."
This is a premature-commit design flaw in the analysis (use-after-free class). No
comments were visible on the fetched page — resolution status **unknown**.

**B2 — #79909 (josharian; reproduces in 1.26 and tip): "slice allocation reuses
stack backing store incorrectly."** `for idx, v := range s` keeps a pointer to the
backing store of `s`, making `s` non-exclusive at that point; the backing-store
analysis (CL 707755) missed the range's retention and the program prints `1924`
instead of `1234`. Fixed in-tree by CL 789060 ("fixed a case in the backing store
analysis for range over slice statements"), per CL 789980's message ("Updates
#79909"). This bug is in the *shared* backing-store analysis that freegc's alias
analysis builds on — same root class as B1 (retention the analysis didn't model).

**B3 — #79972 (watchflakes auto-file; fixed):** `Test/codegen/append_freegc.go`
codegen test failed after the B2 fix changed emitted code ("opcode not found:
`^.*moveSliceNoCapNoScan\b`"). Closed by the test update in CL 789980
(2026-06-11). Process signal, not a soundness issue — but evidence the experiment
has a dedicated builder + watchflakes coverage (see #76930).

**B4 — #79631 (community, ~2026-05):** `GOEXPERIMENT=runtimefreegc go build` silently
does nothing unless the toolchain itself was built with the experiment
(`goexperiment.RuntimeFreegc` is always false for the compiler; should use
`buildcfg.Experiment.RuntimeFreegc` in ssa.go/escape.go). The reporter's `-S`
output shows `growsliceNoAlias` emitted once fixed. Integration roughness;
resolution status not verified from the fetched snippet.

### 3.2 Stated obstacles (design doc + issue, quoted)

1. **Concurrent-GC cooperation.** "Safety, including in the face of the concurrent
   GC, conservative scanning, and time delays between an object being observed by
   the GC to when it is processed." The runtime must tolerate alloc → GC observes
   → free → GC scans → re-allocates the *same* pointer inside one GC phase.
2. **Finalizers/specials.** freegc requires "no specials, such as might have been
   created by a call to SetFinalizer or AddCleanup"; tracked-object support is an
   unimplemented TODO.
3. **Liveness pessimization.** "freegc accepts an unsafe.Pointer and hence keeps
   the pointer alive. It therefore could be a pessimization in some cases (such
   as a long-lived function…) if the caller does not call freegc before or
   roughly when the liveness analysis of the compiler would otherwise have
   determined ptr's object is reclaimable by the GC." — the reason
   `freegcTracked` exists (weak-pointer-like tracking).
4. **Normal-path allocation cost.** "without materially slowing down the normal
   allocation path when there is no reuse" — measured at geomean −0.05% on
   malloc microbenchmarks (their numbers); the bar for default-on is ~zero
   regression when reuse doesn't fire.
5. **Threshold economics.** 16-byte floor ("the free-related overhead [must be]
   small enough" vs. zeroing/useful work); PGO-gated insertion is a possible
   future cost control, "though I'm hopeful a first version could be viable
   without requiring PGO."
6. **Analysis conservatism as a permanent ceiling.** Unknown IR ⇒ treated as
   alias ⇒ no free. B1/B2 show the analysis is simultaneously *too aggressive*
   (premature commit) and the fix direction (commit later, model more
   retentions) pushes toward *more* conservatism — i.e. the static design's
   coverage can only grow by careful, bug-prone casework.

---

## §4 What gc's mechanism does NOT cover that our backlog item does

Our backlog item (PHASES.md §7, "Dead-object slot reuse (allocation hoisting)",
quoted verbatim where marked):

> "When liveness proves an object dead at a later allocation point with
> compatible size/layout, reuse the slot instead of allocating — the heap
> analogue of register coloring, and the complement to escape analysis (EA:
> non-escaping → stack; reuse: escaping-but-dead → recycled slot). The canonical
> win is per-iteration temporaries that escape just enough to defeat EA: reuse
> turns N medium-lived objects into one long-lived slot and, crucially, avoids
> premature tenuring… Sharp edges: finalizers (bail, or clear+re-arm with care);
> pointer-identity observation (`%p`, `unsafe`) — …no reuse when the address can
> leak; partial initialization needs a zero-the-delta step (fresh zeroed-pool
> allocation is free, reuse is not); different-type reuse needs full reinit or
> zeroed pointer fields or the collector mis-scans; deadness proof must be
> race-aware. **Static proof is the practical path; speculation covers
> profile-dependent retention branches.**"

Mapping gc's design against this, point by point:

**(a) Allocator round-trip retained vs. true slot reuse.** gc's mechanism is
*free-early + re-allocate-via-freelist*: every iteration still executes an
allocation call (mallocgc path into the per-P mcache free list) and a free call.
What drops is **GC work** (marking, scan, GC-trigger frequency) plus cache
locality from LIFO reuse — not the alloc/free traffic itself. Our item's strong
form is *hoist + reuse in place*: N per-iteration allocations become **one
long-lived slot**, skipping the allocator entirely each iteration and avoiding
premature tenuring. Mechanically strictly stronger; also the only form that
attacks the allocator-traffic cost at all. (Honest caveat: our item's own sharp
edges admit "fresh zeroed-pool allocation is free, reuse is not" — the
zero-the-delta cost is real on both sides.)

**(b) Static-only analysis vs. speculative JIT reuse (E3).** gc's alias analysis
is AOT and conservative by construction ("treat any IR construct we don't
specifically handle as a potential alias"). It cannot touch any case where
deadness depends on a *dynamic* condition — e.g. conditional retention branches,
profile-dependent non-retention, or values whose aliasing is only decidable at
run time. Our item explicitly reserves "speculation covers profile-dependent
retention branches": with the always-on profiler (E4) we know the hot pattern,
compile a reuse fast path guarded on it, and deopt to the allocating path on
violation. gc structurally cannot do this — AOT has no deopt. B1/B2 also show
that gc's static casework is already hitting soundness bugs; each new pattern
they add is more audit surface, while our speculation route converts "can't
prove" from a hard bail into a guarded fast path.

**(c) Descriptor value model (E1): cross-type slot reuse.** gc reuses within a
span class via mcache free lists and must preserve GC shape (noscan vs scan is
a parameter of the free call). Our descriptor model decouples representation
from semantics per allocation site: a dead slot can be re-purposed for a
*different type* by rewriting the descriptor (with the item's own rules: full
reinit or zeroed pointer fields), which gc's span-class mechanism cannot
express. Note the design doc does claim an advantage over *user* free lists
("reuse memory across types more safely and easily") — but that is still
within the freelist/span-class frame, not representation change.

**(d) Scope not yet built by gc.** The per-iteration same-site `make` case —
"allow heap-allocated slice memory to be reclaimed on each loop iteration when
it returns to the same allocation site" — is **planned, runtime-prototyped,
compiler-not-done**. Non-slice objects, finalizer-bearing objects, and map
growth/split freeing are likewise future or unverified. Our item can target
exactly these while gc's static version is still future work — but the window
is time-limited, not structural.

**(e) Whole-program deadness (E5, pure-AOT mode).** gc's alias analysis is
per-package and relies on inlining to see across functions; separate
compilation bounds what it can prove dead. A closed-world whole-program pass
can prove deadness across package boundaries that gc's analysis never sees.

**What is genuinely raced head-on (honest subsumption):** the *static*
growslice-intermediate case — `s = append(s, x)` in a loop with the final
slice escaping. That is our item's "append-growth temporaries" candidate
benchmark, and CL 710015 already ships it (behind the experiment flag). If
`runtimefreegc` graduates to on-by-default, that sub-case is gone and must not
be claimed as a graalgo win. The surviving differentiators are (a)–(e) above.

---

## §5 Verdict: racing head-on vs. filling the gap

**It is both — split by sub-case:**

1. **RACING head-on (one sub-case, concede it):** static, compiler-proven freeing
   of append-growth intermediate backing arrays. gc has a merged implementation,
   measured wins (their claim: up to ~2x, ~3x memory reduction on append
   microbenchmarks), CI coverage, and a design doc. When the experiment
   graduates, our "append-growth temporaries" benchmark entry is subsumed.
   Do not pitch the backlog item on that pattern.

2. **GAP — allocator-bypass slot reuse (a).** gc keeps the malloc/free round
   trip; we can skip it. This is the larger mechanical win and gc's design
   cannot reach it without a second, different mechanism.

3. **GAP — speculative reuse with guard/deopt (b), the structural one.** This
   is the form gc *cannot* do: AOT-only, no deopt, conservative-by-construction
   analysis. "Speculative reuse with deopt on 'not actually dead'" is exactly
   the E3 edge, and our backlog item already names it ("speculation covers
   profile-dependent retention branches"). gc's B1/B2 soundness bugs are
   evidence that their static route gets *harder* as coverage grows — our
   speculation route gets *easier* (bail → guard).

4. **GAP — descriptor-driven cross-type reuse (c) and whole-program deadness
   (e).** Representation≠semantics and closed-world AOT are outside gc's
   design frame.

5. **Timing note, not a gap:** the per-iteration same-site `make` case is gc's
   stated next step. If we want that pattern, the window is "before their
   compiler CL lands" — treat as a race with a clock, and prefer leading with
   (2)/(3) which have no clock.

**Net for the program owner:** gc's freegc **weakens** our backlog item's
*static* half (one candidate benchmark subsumed) but **does not touch** its
speculative half or its allocator-bypass half — and those are precisely the
halves that need our architectural edges (E1 descriptor model, E3 speculative
JIT + deopt, E4 always-on profiler, E5 pure-AOT closed world). Recommendation:
re-scope the backlog item to lead with (a)+(b) — "speculative dead-object slot
reuse" — and demote the static growslice-intermediate pattern to
"already raced; do not claim." (Re-scoping is the coordinator/owner's call;
no backlog edits made here.)

## §6 Open uncertainties

- #80697 resolution status unknown (no comments rendered on fetch; filed
  2026-07-29 against go1.28-devel). If still open, it is a live soundness hole
  in the experiment.
- Map growth/split freeing CL: stated in the design doc, merge status not
  verified.
- #79631 (experiment build integration) resolution not verified.
- Whether the proposal committee must approve default-on, and any timeline for
  graduation from experiment — not found in fetched sources.
- PJ Malloy's affiliation not verified (immaterial to the technical verdict).
- All performance numbers above are the authors' claims (issue/design-doc/CL
  text), never our measurements.
