# T4 Wave-3: comment-depth mining — golang/go #60879, #77893, #51553, #15397

**Worker:** W5 (T4 comment-depth), wave 3. **Date:** 2026-10-04.
**Mission:** read FULL comment threads of the four issues; mine design/objection arcs,
status of CLs, Go-team positions; verdict per issue (candidate / ATTEMPTED lesson /
negative); cross-reference any thread that sharpens an existing SURV-### item.
**Rule:** all numbers below are the commenters', never ours.

**New full candidates from this wave: none.** The four threads produced zero items
passing the full candidate bar on their own; they instead sharpen SURV-041, SURV-045,
SURV-058 (recorded as §3-style complementary angles below) and correct one stale
"why gc doesn't do it" claim (SURV-041). No new §2 ATTEMPTED lessons: nothing in
these threads was attempted-and-abandoned with a failure reason (details per issue).

---

## 1. golang/go#77893 — runtime: better adaptive stack sizing

**State:** open, labels `Performance, NeedsInvestigation, compiler/runtime, Implementation`.
**31 comments** (2026-03-02 → 2026-09-27). Issue body (fleet data, reporter = Uber's
rabbbit): `runtime.newstack`/`copystack` costs **3.9% core-weighted** vs GC at 7.3%;
per-service stack distributions tabled (service 1: mean 14.2 KB, P50 12.1 KB vs
4 KB starting stack). Note: the issue says "Starting stack size: 4kb" — Go 1.26.

### Design/objection arc

1. **randall77 (Go team)** immediately reframes: gc *already has* an adaptive
   stack starting size on master (points to `src/runtime/stack.go` adaptive
   heuristic); a 14.2 KB mean with a 4 KB start implies a **bimodal distribution**
   — most goroutines small, the grown ones much larger. Concession from the
   reporter (cdvr1993): the numbers only profile goroutines that *needed*
   expansion, so the mean is conditional on exceeding the start size.
2. **randall77** proposes the design direction: start at the 75th/90th percentile
   of existing stack sizes, bounded by "total stack space < 1% of total heap".
   Also: "we could always use more optimization of the stack copying itself. I
   don't think anyone has looked at it in quite a while."
3. **thepudds** contributes the "dormant goroutines skew the average" diagnosis
   (from Uber's #57175 data): long-dormant goroutines pull the global average down
   even though they never contribute to spawn+growth cost. Mails **CL 758141**
   (`runtime: exclude inactive goroutines from starting stack size heuristic`,
   2026-03-23) + benchmark/trace CL 758140. Synthetic benchmark: −5.9% to −12.9%.
   Then a percentile variant **CL 764220** (p90 of scanned sizes; GODEBUG
   `adaptivestackstart`), then the "more aggressive" **CL 793920** (stack-size
   watermark tracked per goroutine: peak size, creation GC-cycle, peak-at-death;
   ~10× on synthetic benchmarks vs ~15% for the earlier CLs).
4. **mpx** floats per-`go`-statement tracking: a uint64 per `go` site tracking the
   preferred initial size, merged like a load average; notes PGO could seed it.
   **thepudds** defers to #18138 (per-start-PC discussion). **CAFxX (the program
   owner)** revives his own old experiment (CL 341990 on #18138) and adds the
   load-bearing refinement: track **not just the PC but the address of the
   function/method/closure passed to the `go` statement** (to handle the
   `workgroup.Go`/`errgroup.Go` pattern), falling back to PC-only on miss.
5. **Uber (rabbbit)** tests a radically simpler alternative: a ~35-line patch to
   `gfput`/`gfget` that keeps grown goroutine stacks (>128 KB threshold) in the
   free cache instead of freeing non-standard sizes — "bias towards recently dead
   goroutines as they are likely similar to the incoming one; stack shrinking
   will fix down any oversizing". Fleet rollout: 30+ services, stack-copy cost
   **~4% → ~0.2%**, no noticeable RSS impact. Mails **CL 793780** (`runtime: cache
   grown goroutine stacks to reduce stack-growth cost`).
6. **randall77 pushes back** on the stack-cache approach (the thread's key
   objection): "I'm not a big fan of just putting free goroutines with >min
   stacks in the cache. That could cause problems with other goroutine
   population patterns. For instance, if we have short-lived goroutines that
   are mostly small, but rarely need to call some subroutine that uses lots of
   stack, then eventually the rare thing happens on all the stacks in the cache,
   and then everything is running with a large stack, mostly unnecessarily. And
   stack shrinking wouldn't ameliorate that problem much." Suggests statistics
   at goroutine exit instead (allocated size at death is within 2× of the
   high-water mark when born+died in the same GC cycle).
7. **rabbbit** counters with three population patterns that make prediction hard
   without start-location knowledge: (a) thousands of tiny background goroutines;
   (b) GC runs 10–100× less frequently than a typical RPC duration; (c) libraries
   (uber-go/cff, safego) spawning many small goroutines. Conclusion: "it just
   seems very hard to predict exactly how big a given goroutine should be
   without knowing more about its starting location."
8. **geekswaroop (Uber)** production-tests the watermark patch on 3 real services:
   copystack 10%→0.7%, 4.12%→0%, 5%→0.4%; watermark selects 32/64/32 KB starts vs
   Go 1.26's 4 KB. Caveat recorded: **one-GC-cycle feedback lag** — bursty
   populations make the global p90 oscillate (32→16→4 KB); planned mitigation:
   rate-limit downward adjustments to one size class per GC.

### What landed vs dropped

**Nothing landed.** All five CLs are status NEW (unmerged) as of 2026-10-04:
758140, 758141, 764220, 793780, 793920 (verified via Gerrit REST). The issue is
still open; the Go team has not endorsed any design. The stack-cache direction
(793780) drew explicit Go-team skepticism (randall77, above).

### Verdict: no new candidate; sharpens SURV-041; corrects a stale claim

**§3-style complementary angle on SURV-041 (exact ID):** the thread is the richest
available obstacle/failure catalog for per-spawn-site stack sizing —
(a) bimodal distributions are *universal* across Uber services (geekswaroop),
so a single global estimate is structurally wrong; (b) dormant goroutines poison
GC-sampled statistics (thepudds) — our always-on profiler must track
creation/destruction events, not snapshot existing goroutines; (c) one-GC-cycle
feedback lag on bursty populations (geekswaroop) — rate-limit downward
adjustments; (d) randall77's rare-large-stack contamination objection is the
exact guard the design needs: cap cached/pooled sizes and shrink aggressively,
or size by *spawn site* (which is what SURV-041 does — per-site histograms are
immune to this objection by construction); (e) rabbbit's reuse-by-recency data
(~4% → ~0.2% with a trivial patch) is the strongest feasibility evidence for
SURV-041's "cached pool of sized stacks" — recency alone works, per-site
histograms should work better; (f) CAFxX's PC + closure-address refinement
(2026-05-07) is a concrete design input for the histogram key: key by
(spawn-site PC, callee address), fall back to PC-only — handles
`workgroup.Go`/`errgroup.Go` fan-out where the PC alone is multi-modal.

**Correction for SURV-041's "Why gc doesn't do it" (coordinator action):** the
entry currently says "the 8 KB start is a static policy." Per randall77's
2026-03-02 comment, gc on master *does* have an adaptive global starting-stack
heuristic (Go 1.26 starts at 4 KB per the issue data). The accurate statement:
gc's heuristic is **global, mean-based, sampled at GC time** — exactly the three
properties this thread shows failing (dormant skew, bimodality, GC-frequency
lag). What gc still lacks — and no Go-team CL proposes — is per-spawn-site
sizing (only floated by mpx/CAFxX in comments, never mailed as a design). The
gap SURV-041 claims is real, but narrower than the entry states.

**No ATTEMPTED lesson:** no design was abandoned with a failure reason; the
work is in-flight.

## 2. golang/go#51553 — cmd/compile,runtime: optimize channel filling idiom

**State:** open, labels `Performance, NeedsDecision, compiler/runtime`.
**12 comments** (2022-03-09 → 2022-04-17). Filed by **CAFxX (the program owner)**.

### Design/objection arc

1. **Frequency skepticism.** ianlancetaylor: "Is this really a common idiom?"
   robpike: "No. Also, why?" CAFxX's defense: it appears where work created
   upfront is distributed to a bounded worker pool, and in iterator patterns.
   mvdan adds a sighting: buffered-channel-as-semaphore of size N (fill N, then
   acquire = receive, release = send), plus a lockout–tagout reframing (bcmills).
2. **bcmills' tier split (the comment SURV-045 records):** the *unshared*-channel
   case "is a special case of inlining and escape analysis" — inline the send,
   the lock/unlock fast paths cancel out via ordinary optimizations. CAFxX
   concedes it would be ideal but doubts the fit with gc's compiler; supplies
   the benchmark: channel fill **272 ns/op** vs mutex-guarded 161 ns/op vs
   plain 30.1 ns/op (reporter's numbers).
3. **mpx** asks for bottleneck evidence; adds the key scoping edit: "Enabling
   **ongoing batch transfers** into/out of a channel likely has performance
   advantages (less so for just setup)." — i.e. the win is in hot loops, not
   one-time fill setup.
4. **josharian's language-level alternative:** extend `copy` — `copy(chan T,
   []T)` blocks until all elements are sent; `copy([]T, chan T)` drains. robpike:
   "This has come up before. The performance variability with data type is
   concerning, but maybe it's OK."
5. **CAFxX's prototype (comment 12, 2022-04-17):** "I recently prototyped a
   limited version of the runtime bits to support bulk sends and it's **up to
   20× faster in the uncontended case**, depending on the number of elements."
   Never submitted as a CL. Also raises the semantic design question josharian's
   form must answer: **blocking vs non-blocking** — non-blocking can be wrapped
   into blocking, but not vice versa; and whether `copy` should work in a
   `select` case.

### Verdict: no new candidate; ATTEMPTED-adjacent prototype evidence; sharpens SURV-045

**No Go-team objection to the mechanism itself** — the skepticism was about
idiom frequency (ianlancetaylor/robpike) and EA-sufficiency for the unshared
case (bcmills), both already in SURV-045's entry. Nothing was declined or built.

**§3-style complementary angle on SURV-045 (exact ID)** — three additions from
the thread's second half, which wave-2 did not extract:
- (a) **Prototype evidence:** CAFxX's bulk-send runtime prototype hit **up to
  20×** in the uncontended case (reporter's number) — the mechanism is
  demonstrated, the item is "prototyped, never submitted," not merely wished
  for. Feasibility anchor for SURV-045 tier 2.
- (b) **Scope narrowing (mpx):** target *ongoing* batch transfers in hot loops,
  not fill-setup — aligns with SURV-045's hot-loop gating (E4) and answers the
  frequency objection by construction (the JIT only versions loops the profiler
  proves hot).
- (c) **Guard-story sharpening (robpike + CAFxX):** robpike's "performance
  variability with data type" concern names a design constraint the bulk path
  must satisfy — the batched transfer's cost model must be type-uniform, or
  per-type slow paths become observable performance cliffs; and the
  blocking-vs-non-blocking question is exactly the observability boundary the
  guard must define: under SURV-045's exclusive-access guard, no other goroutine
  can observe partial batch progress, which is the load-bearing soundness
  argument — batch boundaries must never be observable to a concurrent
  receiver, so batch only while exclusive access holds (already in SURV-045's
  open risks; this thread supplies the adversarial framing).

**Language-level alternative (not a candidate):** josharian's `copy`
extension is a language feature, not a compiler optimization — no transfer,
except as the *exposed-primitive* variant of SURV-045's runtime mechanism.
Noted for completeness, not proposed.

**No §2 ATTEMPTED lesson:** the prototype was never submitted, so there is no
abandonment reason to record.

## 3. golang/go#60879 — cmd/compile,runtime: speed up heap allocations in loops

**State:** open, labels `Performance, NeedsInvestigation, FeatureRequest,
compiler/runtime`. Filed by **CAFxX (the program owner)**, 2023-06-20.
**1 comment.**

The issue body (the design discussion) proposes loop-scoped batch allocation:
for `for … { e := new(T); … }`, allocate a stack-held `[]*T` of capacity C
(known trip count) / 64 (unknown), fill it with one runtime call allocating N
individual elements of T (each still individually garbage-collectable —
explicitly *not* converting N allocs into one `[]T`), and bump-carve per
iteration. Reporter's benchmark: N individual `new(T)` vs one slice alloc,
−25.6% at N=2 … **−83.93% at N=32** … −91.8% at N=4096 (reporter's numbers,
go1.20.5 darwin/amd64). The body also names two alternatives: (a) generic fast
per-P bump allocators specialized on {size class, has pointers} ("more invasive
runtime changes"); (b) sync.Pool overuse as a symptom of allocator deficiency.

The single comment (rip-create-your-account, 2023-06-20): batch-allocation
opportunities "come up regularly" in real code; links a real-world instance
(finnishtable `map.go`); admits doing the manual
`batch := make([]T, n); obj1 := &batch[0]` rewrite "for the reasons already
mentioned in the OP" (kills individual GC of elements) but "it improves
performance on all metrics too much to not do it."

### Design/objection arc

There is none: **zero Go-team engagement** in three years. No objections, no
concessions, no design proposed by the team, no part built. The issue sits at
NeedsInvestigation with the reporter's design sketch as the only content.

### Verdict: negative (no new material); strengthens SURV-058's frequency claim

**§3-style complementary angle on SURV-058 (exact ID):** the single comment is a
real-world frequency sighting — the pattern appears in production code
(finnishtable), not just unmarshaling microbenchmarks, and programmers are
already hand-applying the unsound variant (losing per-element GC) because the
win is too large to ignore. This corroborates SURV-058's benchmark sketch
(tree/graph builders) and answers any "is this a real pattern" skepticism with
a concrete sighting. Nothing in the thread changes SURV-058's design (the
load-bearing work remains the slab's interior GC pointer maps, already flagged
as prototype-gated).

**Raw, unvetted variant (not a candidate):** the issue body's alternative (a) —
generic fast per-P bump allocators for *all* code, not just loops. Not vetted:
no frequency or win evidence beyond the loop case; gc's mcache already provides
a per-P fast path, so the delta is unclear; fails bar leg (b) (no named
benchmark where it beats gc+PGO) as stated. Recorded only so the synthesis
doesn't re-mine it as new.

**No §2 ATTEMPTED lesson:** nothing was attempted.

## 4. golang/go#15397 — cmd/compile: ephemeral slicing doesn't need protection against next object pointers

**State:** open, labels `Performance, compiler/runtime`. Filed by **randall77
(Go team)**, 2016-04-21. **6 comments** (all 2018-03-10/12).

### Design/objection arc

The issue: `b = b[3:]; return b[4]` compiles the reslice to a branch
(`if b.cap == 3 { inc = 0 }`) solely to avoid *manufacturing a pointer to the
next heap object*. The pointer is never exposed to the GC, so the guard is
unnecessary — manufacturing the out-of-bounds pointer is fine if it is never
spilled at a safe point (bounds checks prevent actual use). randall77: "I don't
see an easy way to do this optimization in the current compiler. Marked as
unplanned." References #14849.

- **rasky** (2018): asks randall77 for any current attack plan; notes the guard
  is "already branch-free, but it's still expensive" (SSA lowers it to a
  select) — "people will always have to remove slicing from
  performance-critical code, unless we agree on a way forward."
- **josharian**: notes aclements is working on "safe points everywhere," which
  "might make this infeasible"; wonders about a branch-free `inc` computation.
- **navytux**: adjacent reference — the write barrier at `b = b[3:]` may also be
  omittable; points to **#24314** (still open, verified 2026-10-04).
- **randall77**: "No, I'm still not sure how you would do this. And @josharian
  is right, if we get safe points everywhere this gets significantly harder."
- **aclements** (the thread's design lead): "If you can ensure the out-of-bounds
  pointer won't be live for more than an instruction or two, it might be okay to
  **mark those as unsafe points**. We're absolutely going to need unsafe points
  for the write barrier, but I've been working on removing them from other
  compiler constructs (like range loops)."

### Verdict: negative — stays parked; GC design note, not a candidate

The Go team's position is explicit: unplanned, no known implementation in gc,
and "safe points everywhere" actively closed off the easy routes. Nothing was
built or declined on merit — it is a **parked negative**. It fails the survey's
candidate shape: it is backend codegen + GC-contract engineering (the class
wave-2 filed under "gc-internals / backend codegen Graal already does"), with
no speculative or profile-driven dimension and no named benchmark where a
graalgo-specific mechanism would beat gc+PGO beyond shaving a select per
reslice.

**Design note for the coordinator (not a §3 angle — no matching backlog item):**
aclements' comment names the actual mechanism that would unlock this:
compiler-marked **unsafe points** (1–2-instruction windows where a
manufactured pointer may be live but the GC promises not to look). Whoever
designs graalgo's GC must decide whether the GC contract supports marked unsafe
points; with "safe points everywhere" (the direction gc took), the reslicing
guard cost is permanent. This is a GC-subsystem design input, recorded here so
it isn't lost; it is not a Phase-5 optimization candidate. Adjacent #24314
(write-barrier omission on self-subslice) remains open and unowned — same
verdict class, no action.

**No §2 ATTEMPTED lesson:** nothing was attempted ("marked as unplanned").

---

## Summary for the coordinator

| Issue | Verdict | New candidate? | §2 ATTEMPTED? | Sharpening |
|---|---|---|---|---|
| #77893 stack sizing | In-flight, nothing landed (5 CLs all NEW; Go-team skepticism of stack-cache) | No | No | **SURV-041**: obstacle catalog (bimodality, dormant skew, GC-lag, randall77's contamination objection), rabbbit's recency-reuse feasibility data, CAFxX's PC+closure-address histogram-key refinement; **correction**: SURV-041's "why gc doesn't do it" is stale — gc has an adaptive *global* heuristic; the remaining gap is per-site sizing, which no Go-team CL proposes |
| #51553 channel fill | Frequency/EA skepticism only; prototype existed, never submitted | No | No (prototype never submitted → no abandonment reason) | **SURV-045**: (a) CAFxX's 20× bulk-send prototype as feasibility anchor; (b) mpx's scope narrowing to ongoing hot-loop transfers; (c) robpike's type-variability constraint + blocking/non-blocking observability framing for the guard story |
| #60879 loop batch alloc | No Go-team engagement in 3 years | No | No | **SURV-058**: real-world frequency sighting (finnishtable); per-P bump allocator variant noted raw/unvetted |
| #15397 ephemeral slicing | Parked since 2018, Go team: no known implementation; "safe points everywhere" hardened it | No (negative) | No | GC design note (unsafe-point marking per aclements); adjacent #24314 still open |

**New SURV IDs needed: none.** All findings are sharpenings of SURV-041, SURV-045,
SURV-058, or negatives/design notes.
