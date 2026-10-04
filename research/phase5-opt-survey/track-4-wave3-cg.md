# Wave-3 item 1 — C-G soundness re-verification + SURV-044/012 boundary

Coordinator pass, 2026-10-04 ~10:00 CEST.

## C-G: copy/clone elision for provably read-only destinations

Raw sources: golang/go#19817 (2017, `a := *p` on `[1000][1000]int` → borrow),
golang/go#75620 (2025, `slices.Clone` → noop when provably non-mutated).

### Soundness verdict: the issue's argument is insufficient — but a guarded
variant is sound. C-G survives, re-scoped.

#19817's claim: concurrent mutation is a data race anyway, and the copy isn't
atomic either, so borrowing changes nothing observable. This covers only the
*racy* case. It misses the *race-free* counterexample:

> Write W to the source is happens-before-ordered AFTER the copy point but
> BEFORE the last load of the borrowed destination. All accesses are
> race-free (HB-ordered). The eager copy snapshots the pre-W value; the borrow
> observes the post-W value — a behavior the original program can never
> exhibit.

So "read-only-by-convention" with only an intra-goroutine "all uses are loads"
check is **unsound**: the write-free window must hold across all
happens-before paths, including other goroutines. That proof is
interprocedural + cross-goroutine — undecidable in general, which is the real
reason gc never built #19817, not the atomicity hand-wave.

#75620's framing is the honest version: it lists the actual proof obligations
(input never mutated, output never mutated, capacity unobservable, element
addresses unobserved) — i.e. the full closed-world/escape proof. Statically,
that proof is only dischargeable in narrow cases (e.g. `slices.Clone` of a
fresh literal feeding an inlined no-mutate callee).

### The graalgo routing: speculate + guard with write-barrier invalidation

Our architecture routes around the obstacle (E1+E3): make the borrow a
*guarded* borrow. At elision, the JIT records (object, borrow-epoch); the
write barrier bumps the epoch on any write to the object; loads of the
borrowed destination check the epoch (or a safepoint does) and deopt if the
object was mutated. This converts the undecidable static proof into a cheap
speculative guard — the same class as SURV-052…055's speculation discipline.

This also explains why SURV-031 (`[]byte(s)` borrow) stays separate: a Go
string is *statically immutable*, so borrowing its buffer is sound with only a
read-only-use proof on the destination — the easy special case of C-G with no
guard needed. C-G is the general case (mutable source → guarded borrow).

### Bar check (re-scoped): (a) sound via write-barrier guard + deopt ✓;
(b) #19817's `[1000][1000]int` sum and reflect.Select-shaped call sites —
plausible large wins vs gc+PGO ✓; (c) E1 (representation≠semantics: a "value"
may be a guarded borrow) + E3 ✓; (d) not in SURV-001…059: distinct from
SURV-007 (field projection), SURV-024 (dead-source move elision), SURV-031
(immutable-source special case), SURV-056 (dead-object *slot reuse* — the
write analogue; C-G is the read analogue) ✓; (e) gc does not do it ✓.
**Bucket:** WOULD-LIKE (precedents: WOULD-LIKE — #19817 open since 2017,
unbuilt; #75620 proposal, unbuilt). Ranked as SURV-060 (Tier 2).

### Also recorded
- #53643 (`Clone` — C-G family) folded into the same candidate, not separate.
- The static-only variant (no guard) is struck to §5 with the race-free
  counterexample as the reason, so nobody re-surveys it.

## SURV-044 vs SURV-012 boundary review

- SURV-012 = const-*value* cloning (IPA-CP family): needs constant argument
  values; mechanism = fold constants, delete dead branches; guard = value
  equality (JIT) or whole-program constness (AOT).
- SURV-044 = concrete-*type* cloning of interface *parameters*: needs a closed
  observed type set; mechanism = change the calling convention at the boundary
  (clone takes T directly, no interface materialization); guard = type-set
  membership (JIT) or closed-world type enumeration (AOT, composes with
  SURV-001).

They are NOT the same candidate: different trigger (const value vs type set),
different mechanism (const-fold vs unboxed boundary), different guard story,
different AOT proof (whole-program constness vs closed-world type
enumeration). A clone can be *both* type-specialized and const-specialized —
they compose. Merging would lose the unboxed-boundary distinction, which is
SURV-044's whole point (and what #19165 asks for).

**Verdict: keep separate.** SURV-044's text updated to state the boundary
crisply (was: "Boundary with SURV-012 kept separate pending wave-3 review").
