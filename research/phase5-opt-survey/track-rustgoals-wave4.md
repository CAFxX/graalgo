# Wave-4 track note: Rust Project Goals 2026 follow-up — MIR move elimination

**Worker:** track worker (wave-4 queue item 6 — SURV-024 follow-up).
**Date:** 2026-10-04. **Fetch date for all status claims below:** 2026-10-04.
**Purpose:** follow up the §3 complementary angle on SURV-024 ("Rust Project Goals
2026 include MIR move elimination — prototype-gated spike design") and check whether
the Rust work is vapor or real. I do NOT assign SURV- IDs; I do NOT edit SURVEY.md
or CHECKPOINT.md. Coordinator disposes.

## §1 Status of MIR move elimination (as of 2026-10-04)

**Verdict first: not vapor. Accepted 2026 goal, RFC under active review, a real
prototype with measured numbers, first pieces landing in rustc.**

### 1.1 The goal itself

- **2025h2:** "MIR move elimination", contact @Amanieu (Amanieu d'Antras),
  status **Proposed**, tracking issue rust-lang/goals#396.
  URL: https://goals.rust-lang.org/2025h2/mir-move-elimination.html
- **2026:** "Continue MIR Move Elimination in 2026", status **Accepted**,
  tracking issue **rust-lang/goals#396** (same issue, now pointing at the 2026
  goal document). Champions: @Amanieu ([lang] champion), @RalfJung ([opsem]),
  @dianqk ([compiler] and [wg-mir-opt]).
  URL: https://goals.rust-lang.org/2026/mir-move-elimination.html
  (PR proposing the 2026 continuation: https://github.com/rust-lang/rust-project-goals/pull/481)

### 1.2 What the goal proposes (mechanism)

Summary (quoting the goal doc): "Add a MIR optimization which eliminates move
operations. This will require changes to the MIR semantics of `move` to enable
the optimization to cases where a value has had its address taken (LLVM already
eliminates moves when this is not the case)."

The core problem: Rust constructs values in one place and moves them into their
final address; current MIR semantics forbid the source and destination of a
`move` from sharing an address once the address has been taken (the storage
lifetime of `a` extends to end of scope and overlaps `b`, so under the tree
borrows model a previously-taken raw pointer may still legally access the old
allocation — goal doc `example1`). Same problem for partial moves (`example2`:
`a.0` moved out, `a.1` still address-observable at its current address).

The proposed semantic change: **when moving a place, the portion of the
allocation covered by that place is freed, which allows another allocation to
take its place**; this enables a MIR optimization that **unifies two
move-connected locals whose live ranges don't overlap into a single local**
(goal doc, with the caveat that design axioms are Teachable / Checkable by
Miri / Efficient).

Sequencing per the goal: (1) nail down the semantics + write the RFC (lang/opsem
work); (2) implement the new semantics in Miri for checking; (3) implement the
MIR optimization pass in rustc. Expected effects: eliminated copies; shorter
allocation lifetimes (smaller futures — fewer locals live across async yield
points); resolves long-standing `move`-semantics issues (rust-lang/rust#71117,
rust-lang/rust#68364, rust-unsafe-code-guidelines #416, #188); possible compile-
time win from less LLVM IR (not guaranteed).

### 1.3 The RFC

**rust-lang/rfcs#3943 "MIR move elimination"** — URL:
https://github.com/rust-lang/rfcs/pull/3943. **Status: Open, under active review**
(fetched 2026-10-04). Review activity visible: threads with @Amanieu, @scottmcm,
@RalfJung (co-author on semantic changes), @tmiasko, @Jules-Bertholet,
@JonathanBrouwer; at least one reviewer suggestion merged. The RFC proposes the
new `move` semantics; it is NOT yet accepted — this matters because (per
@Amanieu on rust-lang/rust#157943, 2026-06-22) the `PreciseLiveness` analysis is
built on the RFC's new semantics, "which technically makes this MIR optimization
unsound at the moment."

### 1.4 The prototype (measured, not paper)

There is a real prototype, with three generations:

1. **[WIP] MIR move elimination pass, rust-lang/rust#156046** (opened by
   @Amanieu 2026-05-01). URL: https://github.com/rust-lang/rust/pull/156046.
   **Closed 2026-06-15 by its author, explicitly in favor of #157943**
   ("Closed in favor of #157943 so reviewers don't need to wade through 100+
   CI/bors/perf messages"). It had real perf data: the project all-hands 2026
   session ("Landing the MIR move elimination optimization",
   https://github.com/rust-lang/all-hands-2026/issues/63) describes the
   prototype as showing "**2–7% speedup on the compiler itself**" — that is the
   session description's claim, not our measurement. The try-perf runs on the PR
   are noisier: instruction-count (rust's "most reliable metric") mean −0.7%
   to −0.3% overall on primary benchmarks with a long tail of improvements
   (e.g. 192 improvements vs 36 regressions on the 2026-08-29 run) against a few
   sharp regressions (up to +7% on primary, +700% on secondary in one run);
   cycles mean −2.2% to −2.5% primary; bootstrap −3.2%; artifact size −0.8%.
   I.e. net-improving on aggregate, with per-benchmark regression risk —
   consistent with a real but young optimization pass, not a finished product.

2. **Implement RFC 3943: MIR move elimination, rust-lang/rust#157943**
   (successor, opened 2026-06-15). URL:
   https://github.com/rust-lang/rust/pull/157943. Design, from the PR body:
   gated behind `-Z mir-move-elimination`; **disables `DestinationPropagation`,
   which it completely supersedes**; three parts:
   - **PreciseLiveness** — analysis computing, at sub-statement granularity,
     where a local requires allocated storage; finer than `MaybeStorageLive`,
     borrow-aware.
   - **TailCopyToMove** — pre-pass turning `Copy` into `Move` immediately
     before a `Return` terminator (valid even for borrowed places: the return
     invalidates borrows); needed so the copy source can be unified with the
     return place.
   - **MoveElimination** — the main pass: optimizes away moves by **place
     unification** (merge move-connected locals with non-overlapping live
     ranges into one local), including an alias fixup, and deletes all storage
     statements then **reconstructs them from scratch from liveness
     information**.
   
   Review was substantive (cjgillot, dianqk on analysis precision and soundness;
   saethlin found a miscompile-shaped case via rustlantis 2026-07-03 — moving
   `addr_of!`-observed locals — which Amanieu traced to new-semantics/storage
   reconstruction and fixed by 2026-08-29). **2026-09-17/25: split into 7
   smaller PRs** at reviewers' request:
   - [1/6] #163335 MIR preparation
   - [1.5/6] #163359 ensure ZSTs always initialized
   - **[2/6] #163336 TailCopyToMove — MERGED** (rollup merge 2026-09-30; bors
     log on the RFC page confirms it landed)
   - [3/6] #163337 PreciseLiveness
   - [4/6] #163338 MoveElimination (incl. alias fixup)
   - [5/6] #163339 storage reconstruction
   - [6/6] #163340 Miri support
   
   **Blockers stated by the author** (2026-09-19): Miri support work "uncovered
   several issues with the semantics" — he is fixing those first. dianqk
   available for review from first week of October 2026.

3. **rustlantis differential testing** is already running against the branch
   (saethlin's 2026-07-03 fuzz finding) — a healthy sign for soundness work.

**Status classification:** **THINKING → ABLE (in progress)**. This is not a
paper goal: the 2026 goal is Accepted, the RFC is Open and actively reviewed,
the prototype is a real compiler branch with perf measurements, the first
landing (TailCopyToMove) is in rustc main behind the pre-existing pipeline, and
the remaining 5–6 PRs are under review gated on the RFC's semantics. The
honest caveat: the pass is **not yet sound/accepted upstream** — it depends on
an unaccepted RFC and Miri adaptation (rust-lang/rust#162048), and the
"2–7%" number is the author's/proponent's characterization of try-perf results
on rustc self-hosting, not an independent measurement.

### 1.5 What changed vs the wave-3 note

Wave 3 recorded MIR move elimination as "THINKING ... not a ranked candidate —
prototype-gated spike" based on the 2025h2 Proposed doc. The 2026-10-04 follow-up
upgrades the disposition: **Accepted goal + open RFC + measured prototype + one
landed sub-pass**. The prototype gate is now *arming*, not aspirational — the
spike design below can be informed by rustc's actual mechanism, not just the
goal sketch.

## §2 Transfer analysis: what MIR move elimination means for a Go compiler on GraalVM/Truffle

**Starting position (from SURV-024):** Go copies on assignment, always. The
SURV-024 hypothesis: transfer the *shape* of the Rust/Swift move optimization as
a runtime-level optimization with no language change — when liveness analysis
proves (or profiling + a guard observes) that a copied value's source is dead
after the copy, convert copy→move; for heap values that is pointer swizzling
instead of memcpy. E1: in the descriptor model a "move" is a representation
change. E3: speculative variant with a uniqueness guard + deopt.

The wave-4 follow-up lets me sharpen this into a precise transfer map against
rustc's *actual* mechanism (RFC 3943 + the #157943 split). Three mechanisms
transfer, one transfers partially, and the soundness core does **not** transfer
in its static form.

### 2.1 TRANSFERS: precise sub-statement liveness as the driver

rustc's `PreciseLiveness` computes, at sub-statement granularity and
borrow-aware, the points where a local needs allocated storage. That is exactly
the analysis a Go JIT needs to answer "is this copy's source dead after the
copy?" — finer than gc's SSA-level liveness, and it is the missing analysis in
SURV-024's original sketch (which said "liveness analysis proves" without
naming the precision required).

**Go mechanism analog:** our JIT's liveness must be defined over the descriptor
model's value slots, at the granularity of the lowered copy sites. The borrow-
awareness in `PreciseLiveness` (a borrow of a place extends the storage need)
maps to Go's **escape analysis facts**: an address taken via `&` and escaping
into an interface, a closure, a channel, `unsafe`, or `reflect` is the Go
equivalent of a Rust borrow — it extends the source's lifetime past the copy
site and vetoes elision unless statically contained. The always-on profiler
(E4) contributes the dynamic half: sites where the profiler observes the source
never re-observed-alive after the copy are speculation candidates.

This transfers cleanly: it is the same dataflow problem with a different
"keeps-alive" relation (borrow → escape). No language change required either
way.

### 2.2 TRANSFERS: move-connected local unification (the general case)

The `MoveElimination` core — **unify two move-connected locals with
non-overlapping live ranges into a single local** — is strictly more general
than "convert this copy into a pointer swizzle". It says: `b = a; use(b)` where
`a` dies at the copy becomes a single storage location, and the "copy" becomes
a nop; the downstream passes then construct into/forward from one place.

**Go mechanism analogs** (each is a concrete site shape in Go lowering):

1. **Value-copy sites:** `b = a` for struct/array values, and by-value
   argument/return passing of large structs, where the source local is dead
   after the copy. In the descriptor model this is unification of the source
   and destination descriptors — the emitted machine code never materializes
   the memcpy. This generalizes gc's existing NRVO-ish behavior only in that
   gc has no move concept at all: unification works for mid-function locals,
   not just return places.
2. **`TailCopyToMove` analog — return-place unification:** rustc's pre-pass
   turns `Copy` into `Move` immediately before `Return` because the return
   invalidates borrows, enabling unification with the return place. The Go
   analog: `return bigStructValue` where the returned local is a fresh local
   dead everywhere after — construct directly into the caller's return slot
   (NRVO generalized to any provably-dead local, not just named returns). The
   "return invalidates borrows" justification maps to Go's "after return the
   callee frame is gone" — but note the Go caveat in §2.4: a Go callee can
   stash `&local` into a heap-escaping location before returning, which is
   exactly what escape analysis must rule out.
3. **`copy(dst, src)` / append-growth sites:** SURV-024's original targets.
   rustc's design confirms the shape: the win is not "avoid the loop" but
   "delete the source's storage lifetime". For `copy(dst, src)` where `src`
   dies after and is uniquely referenced, the memmove is replaced by aliasing
   (descriptor retargeting). For append growth, the old backing array's storage
   is dead after the grow → the grow becomes an in-place realloc when the
   array is uniquely referenced. **This is SURV-024's core, now with rustc's
   analysis as the mechanism to port.**

### 2.3 TRANSFERS (partially): storage-statement reconstruction

rustc deletes all storage statements and rebuilds them from the liveness
analysis — the bold part of the design, and the source of the rustlantis
miscompile (storage-lifetime semantics interacting with `addr_of!`).

**Partial transfer:** the *reconstruction-from-liveness* discipline transfers
as a pipeline-scheduling principle (cf. wave-3 R5 / T5): our JIT's storage
allocation (stack-slot assignment, GC-root liveness for the collector) should
be a *derived view* of one canonical liveness analysis, not a parallel
bookkeeping that can drift. The full delete-and-rebuild of `StorageLive/Dead`
is Rust-specific (Rust has explicit storage statements; Go SSA does not) —
the concrete code motion does not transfer, the invariant does: **one
liveness analysis, all downstream storage decisions derived from it.**

### 2.4 DOES NOT TRANSFER (static form): the borrow-checker soundness core — §5 negative

This is the critical honest boundary. Rust's move elimination is sound
*statically* because the borrow checker proves the source cannot be observed
after the move; the RFC's semantic change only needed to extend that proof to
address-taken places. **Go has no such static proof and cannot get one:**
`unsafe`, `reflect`, interface smuggling, closures capturing the source
variable, and method values can resurrect an "dead" source at any time, and
no type-system analysis rules them out.

Consequence: **the static-proof variant of MIR move elimination is a §5
negative for Go — there is no Go analog of the borrow checker's
single-ownership guarantee, so the technique cannot transfer as a sound
static optimization.** What transfers is only:

- (a) the **analysis machinery** (§2.1) to find candidate sites, plus
- (b) the **speculative variant** (SURV-024's E3 arm): guard on uniqueness /
  source-death (descriptor-level reference tracking or a cheap runtime
  uniqueness check at the copy site), deopt to the copying path on violation.

The guard placement is the new design work the Rust project doesn't have to
do: candidates are (i) allocation-site-anchored guards (descriptor carries a
"uniquely referenced" bit maintained by the GC/allocator — cheap to test at
the copy site); (ii) code-version guards (the JIT compiles a move-eliding
version guarded on a per-site assumption, invalidated by the first observed
aliasing — mirrors the assumption-dependency framework, SURVEY.md §4).

**Related non-transfer:** Rust's win is partly driven by "Rust objects are
typically constructed by a method and then moved into their final address" —
a language-idiom artifact (builder patterns, `Box::new`, futures). Go's copy
idioms are different: by-value struct passing is culturally discouraged at
large sizes, so the *frequency* of large move-connected locals in idiomatic
Go is lower than in Rust. This is why SURV-024's benchmark sketch targets
`copy`/`append` builtins rather than general struct assignment — the
frequency argument transfers only at the builtin sites. Honest scope:
general move-connected local unification may be thin in Go; the builtin
sites are the fat part.

### 2.5 What Graal already gives us (ABLE precedent, constrains the design)

Graal's **Partial Escape Analysis** (HotSpot/Graal — ABLE, shipped) already
performs allocation elimination and scalar replacement driven by escape
facts. Our JIT inherits this. The transfer question is therefore not "build
a move-elimination pass from scratch" but: **can the JIT's PEA + descriptor
model express copy→move conversion at Go builtin sites that PEA misses?**
PEA eliminates allocations that don't escape; it does *not* generally turn a
surviving `copy(dst, src)` memmove into an alias transfer when `src` dies —
that is the gap the spike probes. If PEA already kills the benchmark's
copies, the spike dies at the headroom step (§3.1), which is a cheap and
honest outcome.

**Transfer summary:** analysis (§2.1) and unification shape (§2.2) transfer;
the reconstruction invariant transfers as pipeline discipline (§2.3); the
static soundness core is a §5 negative — the Go version is necessarily the
guarded speculative variant, which Rust never needed to design.

## §3 Prototype-gated spike design (implementable by a later Phase-5 worker)

**Standing rule honored:** this item is prototype-gated. It is promoted only on
a measured prototype, never on paper numbers. The spike is staged so the cheap
killing experiment runs first.

### 3.0 Hypothesis (falsifiable)

> In the graalgo speculative JIT, converting dead-source copies into
> representation-level moves — guarded on uniqueness, deopt on alias
> violation — at `copy(dst, src)` builtin sites and large-struct value-copy
> sites beats stock gc **with PGO** on a copy-heavy benchmark, because gc
> always emits the memmove (it has no move concept and no speculative
> uniqueness guard), while the JIT can delete the source's storage lifetime
> and alias the destination instead.

### 3.1 Stage 0 — headroom measurement (cheap, runs first; no JIT changes)

**Builds on:** existing always-on profiler intent + a standalone
instrumentation harness (a gc-compiled binary with `runtime.memmove`/
`duffcopy` call-site attribution, or a Pin/DynamoRIO-style dynamic counter —
worker's choice, but the method must be documented).

**What it measures:** for each dynamic `copy(dst, src)` and struct-value-copy
site in the benchmark (§3.3): bytes copied, and the fraction of those bytes
whose source is never read again after the copy (dead-source fraction),
counting only sites where the source is heap/stack-unique (no observed second
reference). Output: an upper-bound table — "X% of copied bytes are
dead-source-unique".

**Kill trigger (Stage 0):** if the dead-source-unique fraction is **< 10% of
total copied bytes** in the benchmark, or the absolute headroom is **< 1% of
benchmark wall time** (copy cost × fraction), **kill the spike here** — the
optimization cannot beat gc+PGO by a margin worth its guard machinery.
Record the numbers as the negative result (this is the §5-style honest
outcome; PEA or gc may already be covering the space).

### 3.2 Stage 1 — guarded prototype (only if Stage 0 survives)

**Builds on:** the JIT's escape-analysis/descriptor facts (E1) + the
assumption-dependency/guard framework (SURVEY.md §4, E3). Concretely:

1. **Candidate selection:** `PreciseLiveness`-shaped analysis over the JIT IR
   at function scope: for each `copy(dst, src)` and struct-copy site, compute
   whether the source place is dead after the site under the borrow-aware
   liveness analog (§2.1: escape facts = the Go "borrow"). Static-proof sites
   (source provably dead + provably unique, e.g. fresh local never
   address-taken) get the **static** conversion. All other Stage-0-positive
   sites get the **guarded** conversion.
2. **The conversion:** replace the memmove emission with a descriptor
   retarget (destination aliases the source storage; source descriptor marked
   dead/moved). For heap values this is pointer swizzling; no write barrier
   is skipped unless the source slot is dead *and* cleared — the proof bar
   from SURV-024's open risks stands.
3. **The guard:** per-site uniqueness assumption. Cheap form: descriptor
   carries a uniqueness bit maintained at allocation/escape points; the copy
   site tests it and falls back to the memmove path on violation (no deopt
   needed — a local branch, not a full deopt). Expensive/rare form: full
   code-version guard with deopt for sites where the uniqueness bit is
   unstable. The worker must justify the chosen form with measured
   guard-overhead numbers.
4. **Scope discipline:** straight-line, same-function regions first
   (SURV-024's legality work item); cross-block and interprocedural (return-
   place unification, the `TailCopyToMove` analog) only after Stage 1 shows
   a win — each is a separately killable sub-experiment.

**Must NOT do:** no SPEC.md/PHASES.md/registry changes (survey proposes only);
no changes to the language or to gc's pipeline; no claiming the static-proof
variant is sound for arbitrary Go (it is not — §2.4).

### 3.3 Benchmark and metric

- **Benchmark (named):** append-growth loop over large element types
  (e.g. `[64]byte`-element slices grown to 10k+ elements) + a `copy(dst, src)`
  microbenchmark where `src` dies (both from SURV-024's sketch), **plus one
  real workload**: a JSON/codec encode-decode pipeline or `bytes.Buffer`
  growth path — the worker picks the real workload but must name it in the
  spike report. All three run under **gc with PGO** as the baseline
  (Phase-5 rule), same machine, same conditions.
- **Measured metric:** wall-clock time (primary), bytes memmove'd (secondary,
  from Stage-0 instrumentation), guard-branch overhead in cycles (secondary).
  All numbers are **measured** — no modeled/projected figures in the verdict.
- **Comparison discipline:** graalgo-JIT (with the prototype) vs gc+PGO; the
  prototype must also be run with the conversion *disabled but guards
  enabled* to isolate guard overhead from conversion win.

### 3.4 Kill triggers (any one kills)

1. **Stage-0 headroom kill** (§3.1): dead-source-unique fraction < 10% of
   copied bytes, or headroom < 1% of wall time.
2. **No-win kill:** prototype (Stage 1) does not beat gc+PGO by **> 2%**
   wall-clock on at least one of the three benchmarks, with the win
   reproducible across 5 runs (report median + IQR; the 2% bar is above the
   noise floor the worker must establish for the machine).
3. **Guard-cost kill:** guard overhead alone (conversion disabled) costs
   **> 50% of the conversion win** — the mechanism is not worth its
   machinery even where it works.
4. **Soundness kill:** any miscompile or aliasing violation observed in the
   prototype's test battery (differential against gc on the benchmark corpus
   + a targeted unsafe/reflect/closure-capture adversarial suite) that cannot
   be closed by tightening the guard within the spike's timebox — the item
   returns to WOULD-LIKE with the failure documented, not to the backlog.

### 3.5 Deliverables of the spike (bounded experiment, not a product)

- The Stage-0 headroom table (measured).
- The prototype diff (JIT-side only) + the adversarial test battery and its
  results.
- The three-benchmark comparison table: gc+PGO vs graalgo-JIT
  (conversion on) vs graalgo-JIT (guards only) — medians, IQRs, machine
  description.
- A verdict: PROMOTE (meets the >2% bar with guard economics sane),
  KILL (with the trigger number), or NARROW (win confined to a sub-site
  class — e.g. only `copy()` builtins — with the narrowed claim stated).
- Estimated bound: the spike is a **bounded experiment** — if Stage 0 is
  negative it ends in days; Stage 1 is scoped to same-function regions
  first.

### 3.6 What the Rust work gives the spike worker (concrete, citable)

- The **mechanism to port**: `PreciseLiveness` (sub-statement,
  borrow-aware liveness) + move-connected local unification + the
  delete-and-rebuild-storage invariant — rust-lang/rust#157943 (PR body and
  the split PRs #163335–#163340) is the design document, more useful than
  the goal text.
- The **cautionary tale**: the rustlantis miscompile (saethlin, 2026-07-03
  on #157943) — storage reconstruction interacting with address-taken
  places is where the soundness bugs live; the Go spike's adversarial suite
  must include address-taken-source cases from day one.
- The **perf honesty reference**: try-perf showed net-improving aggregates
  with sharp per-benchmark regressions — the spike's benchmark suite must
  be wide enough to catch the same pattern, not just the three named
  benchmarks.
- The **non-portable part**, restated: Rust's soundness comes from the
  borrow checker; ours must come from the guard. Any spike design that
  assumes static single-ownership for arbitrary Go values is unsound by
  construction — reviewer should reject it at design review.

---

*Note on sources: all Rust status claims above were fetched 2026-10-04 from
the URLs cited inline. The "2–7%" figure is the all-hands session
description's characterization of the prototype's try-perf results, not an
independent measurement. No performance numbers in this note are our own
measurements — there is no graalgo JIT prototype yet; that is exactly what
§3 stages.*
