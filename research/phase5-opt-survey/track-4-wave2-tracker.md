# T4 Wave-2 Tracker: golang/go issue tracker + community blogs

**Worker:** wave-2 survey worker (T4). **Started:** 2026-10-04.
**Rule in force:** every citation verified by fetch — no recalled issue numbers.
**Status markers:** OPEN / ACCEPTED-UNBUILT / DECLINED / THINKING / ABLE / ATTEMPTED / WOULD-LIKE.

Bar (all must hold): (a) semantics-preserving or sound speculative story; (b) plausible
win vs gc **with PGO** on a named benchmark; (c) fits ≥1 graalgo architectural edge;
(d) not in backlog and not in SURV-001…040; (e) not something gc already does well.

Already covered (do not re-propose): SURV-001…040 (see [SURVEY.md](SURVEY.md) §1) + backlog:
devirtualization cascade; profile-guided struct field reorder/pack; speculative AoS→SoA;
type narrowing + bit-packing; speculative data representation (map specialization,
unboxing); small string optimization; dynamic string interning; inferred store-once;
idle-time repack/defrag/shrink; adaptive concurrency primitives; aggressive optimistic
escape analysis incl. cross-goroutine.

---

## 1. API sweep log (pages cached under `.t4-wave2-cache/`)

| # | Query | Date | Cached as | Result |
|---|-------|------|-----------|--------|
| S1 | `repo:golang/go label:Performance label:"compiler/runtime" state:open`, sort=created asc, 5×100 | 2026-10-04 | `perf-compruntime-open-p{1..5}.json`, `perf-compruntime-open-titles.txt` | 447 issues; all titles triaged (see §2/§3/§4) |
| S2 | `repo:golang/go label:Proposal state:closed -label:Proposal-Accepted`, sort=updated desc, p1 | 2026-10-04 | `prop-closed-p1.json` | 3976 total; keyword-filtered p1 only — full paging not economical; targeted fetches below |
| S3 | `repo:golang/go label:Proposal-Declined` | 2026-10-04 | — | label does not exist; declined proposals are closed `Proposal` without `Proposal-Accepted` |

Individual issues fetched+verified (body + labels; comments where noted): 16798, 21536,
24582, 24926, 25828, 26680, 26734, 26951, 27345, 27801, 29276, 29716, 30509, 41948, 42999,
46079, 51553, 53643, 55930, 57060, 57070, 60879, 62488, 64541, 64673, 64675, 64900, 69015,
71349, 75620, 77090, 77893, 81887, 16869, 17133, 18004, 18138, 19165, 19817, 20351, 23929,
54622, 67520, 75273, 81004, 81230, 26891, 15397.

## 2. Mined candidates (unnumbered — coordinator assigns SURV-IDs)

### C-A — Append-chain fusion (incl. cross-inline-boundary in the JIT)
**Source:** https://github.com/golang/go/issues/25828 (open, 2018). Supporting:
https://github.com/golang/go/issues/46079 (append-make→make+copy idiom),
https://github.com/golang/go/issues/26734 (append 24% slower than make+assign, small N).
**Bucket:** WOULD-LIKE (gc: nobody has built it; issue includes a prototype gogrep
rewrite sketch by the reporter).
**Proposal:** Fuse `xs=append(xs,v1); xs=append(xs,v2)` → `xs=append(xs,v1,v2)`:
one growth check, one call sequence. In our JIT, do it *after inlining* so chains
formed across function boundaries fuse too — the case gc's textual form can never see.
**Status:** open, NeedsInvestigation-equivalent; no Go-team owner.
**Bar check:** (a) semantics-preserving — pure reassociation of a pure sequence;
(b) issue reporter's benchmark: 164 ns/op + 3 allocs → 61.7 ns/op + 1 alloc
(synthetic BenchmarkAppend; reporter's numbers, not ours); real sites in net/http,
cmd/vet; (c) edge E3 (JIT-only cross-boundary fusion) + E4 (profile can rank chains
by hotness); (d) not in SURV-001…040 (SURV-017 is capacity pre-sizing, complementary:
fuse first, then pre-size once); (e) gc does not do it.
**Benchmark sketch:** reporter's BenchmarkAppend shape + `go build ./...` (vet/http sites).

### C-B — Speculative batched channel send in fill/drain loops
**Source:** https://github.com/golang/go/issues/51553 (open, NeedsDecision; filed by
CAFxX — the user). Key discussion: bcmills notes the *unshared*-channel case reduces
to inlining+EA (already in our aggressive-EA backlog); the issue's second variant —
batch N sends under one lock/unlock when the loop performs no other synchronization —
is not reduced by that argument.
**Bucket:** WOULD-LIKE.
**Proposal:** At hot `for … { ch <- e }` / `<-ch` loop sites, JIT-version the loop to
transfer elements in batches under a single hchan lock acquisition. Soundness is
the load-bearing subtlety, in two tiers: (1) the *unshared*-channel case (channel
provably non-escaped) reduces to inlining+EA per bcmills' comment — already in
our aggressive-EA backlog, not new; (2) the *novel* tier is speculative batching
under an observed-exclusive-access guard: profile says single sender/receiver,
guard on a per-channel epoch/sender-count at batch-chunk entry, slow path on
violation. The caveat that must survive into design: batching changes
multi-sender interleaving granularity, so the guard is semantic load-bearing, not
an optimization hint — batch only while exclusive access holds.
**Status:** open, NeedsDecision; Go team skeptical of the idiom's frequency
(ianlancetaylor: "Is this really a common idiom?").
**Bar check:** (a) sound only under the exclusive-access guard (tier 2) or the
EA-proven unshared case (tier 1, already covered) — the issue's own "no other
synchronization" condition is necessary but not sufficient; (b) issue reporter's
numbers: channel fill 272 ns/op vs plain 30.1 ns/op (reporter's, not ours) — ~9×
gap is the ceiling; benchmark: worker-pool work-distribution handoff; (c) E3
(per-site versioning + guard) + E4 (hot-loop detection); (d) not in SURV-001…040
(SURV-032 is *select* lowering — different op); (e) gc does not do it.
**Open risk:** the frequency question the Go team raised — gate on E4 hotness data.

### C-C — Interface equality fast path: reflexive-type bit + data-pointer equality
**Source:** https://github.com/golang/go/issues/62488 (open, NeedsInvestigation).
randall77 (Go team) proposes the mechanism: "detect that interfaces are equal if
their data pointers are equal… add a bit to types to mark them as reflexive (we
already do that for map keys), and then data pointer equality implies interface
equality" (NaN is the caveat the bit handles).
**Bucket:** THINKING (Go-team-endorsed direction, unbuilt).
**Proposal:** In the type descriptor, carry a reflexive bit (non-NaN-containing,
pointer-identity-implies-equality types — gc already computes this for map keys).
Interface `==`: if both types reflexive and data pointers equal → equal, skipping
the recursive type-driven comparison. Covers the issue's exponential-blowup case
when sharing is pointer-identical, and the common fast path generally.
**Status:** open, NeedsInvestigation; no implementation.
**Bar check:** (a) semantics-preserving — reflexive bit is exactly the soundness
condition; (b) plausible win vs gc+PGO on interface-keyed map lookups and
`any`-heavy equality (e.g. reflect.DeepEqual-style traversals, testify-style
assertions); benchmark sketch: map[any]V lookup/insert with interned keys;
(c) E1 — the bit lives in the descriptor, representation≠semantics; (d) not in
SURV-001…040 (covers wave-1's "interface comparison" gap); (e) gc does not do it.

### C-D — Map-copy range idiom → bulk clone with presizing
**Source:** https://github.com/golang/go/issues/26951 (open). gogrep census: the
`m2:=make(map[K]V); for k,v := range m1 { m2[k]=v }` pattern occurs *more* often in
std than the map-clear idiom (#20138, which gc did optimize).
**Bucket:** WOULD-LIKE.
**Proposal:** Recognize the copy idiom (post-inlining, so caller/callee splits count)
and lower to: presized `makemap` (hint = len(m1)) + tight assign loop using the
inlined fast-path assign, skipping per-element mapassign call overhead and growth
repetitions. (Full memcpy is unsound without seed coupling — issue notes the seed
leak concern; presize+fast-loop avoids it.)
**Status:** open; no owner.
**Bar check:** (a) semantics-preserving — iteration order already unspecified;
(b) plausible win on maps.Clone-shaped workloads; (c) E3 (idiom recognition across
inline boundaries in the JIT); (d) not in SURV-001…040 (SURV-015/018 are access-path
opts, not bulk copy); (e) gc does not do it.

### C-E — Assign-side map string-key allocation elision
**Source:** https://github.com/golang/go/issues/55930 (open): `m[string(k)]=1` in a
loop costs 1 alloc/iter (29.83 ns/op) although the key already exists after the
first iteration. Related: #45045 made the naive fix harder (cited in issue).
**Bucket:** WOULD-LIKE.
**Proposal:** Two-layer: (1) static — at `mapassign` with a string key, probe with a
non-allocating key (the lookup path already avoids allocation via scratch buffer);
allocate the key string *only on insert*, reusing the resident key on update.
(2) speculative — per-site inline cache on "key already present" (ties to SURV-018's
slot-caching family, but for dynamic keys).
**Status:** open; no owner.
**Bar check:** (a) semantics-preserving — key bytes are identical; (b) issue
benchmark: 29.83 ns/op 1 alloc → expected 0 alloc on update path (reporter's
numbers); (c) E1 (key representation) + E3 (speculative variant); (d) not in
SURV-001…040 (SURV-018 is constant-key slot caching — different); (e) gc does not
do it.

### C-F — Callee-side concrete-type specialization of interface parameters
**Source:** https://github.com/golang/go/issues/19165 (open, 2017): when every call
site passes the same concrete type set for a function's interface params, rewrite
the callee to take the concrete types — removes conversion overhead, interface
dispatch, *and* lets EA keep the argument on the stack.
**Bucket:** WOULD-LIKE.
**Proposal:** PGO-driven (or closed-world-AOT-proven) cloning of hot functions on
the observed concrete types of interface parameters: the clone takes `T` directly,
no interface materialization at the boundary; guard on the type set (E3) or prove
closed-world (E5). Composes with the devirtualization cascade (which works the
call-site side) and SURV-012 (which is const-*value* specialization).
**Status:** open since 2017; the issue itself notes the code-bloat guard needed
(specialize only when methods inline / body is small).
**Bar check:** (a) sound via guard/deopt or closed-world proof; (b) plausible win vs
gc+PGO on `sort.Sort`-with-`sort.Interface`-shaped workloads (issue's own example)
and any hot interface-param function; (c) E3 (+E5 for the proof variant);
(d) not in SURV-001…040 — SURV-004 is func-*value* sites, SURV-011 is *value*
specialization, SURV-012 const-arg, SURV-016 generic type-args; this is
concrete-type specialization of *parameters*, uncovered; (e) gc does not do it.

### C-G — Copy/clone elision for provably read-only destinations
**Sources:** https://github.com/golang/go/issues/19817 (open, 2017): `a := *p`
(1M ints) where `a` is only read → substitute a borrowed pointer, no copy;
https://github.com/golang/go/issues/75620 (open): `slices.Clone` of a never-mutated
slice (motivated by `reflect.Select` defensive copy) → alias instead of copy.
**Bucket:** WOULD-LIKE.
**Proposal:** When the compiler/JIT can prove every use of a copy/clone destination
is a load (covering inlined callees; no escaping mutable reference), represent the
"value" as a borrow of the source instead of a copy. The issue itself argues the
data-race objection away: the copy isn't atomic either, so borrowing changes nothing
observable under the memory model (still record this as the soundness argument to
re-verify — it is the load-bearing claim).
**Status:** both open; no owners.
**Bar check:** (a) semantics-preserving modulo the data-race argument above
(verify, don't assume); (b) plausible win vs gc+PGO on large-struct copy loops
(#19817's `[1000][1000]int` sum) and reflect.Select-shaped call sites; (c) E1 —
representation≠semantics is exactly the enabler (a "value" may be a borrowed
pointer); (d) not in SURV-001…040 — SURV-007 is *field-projection*, SURV-024 is
*dead-source* move elision; this is *read-only-destination* borrow, distinct;
(e) gc does not do it.

### C-H — Concatenation-chain fusion: n-way string concat in one allocation
**Source:** https://github.com/golang/go/issues/27801 (open): CL 123256 discussion;
arity specialization N=2 measured −27.95% on Concat2 (reporter's numbers).
**Bucket:** THINKING (Go team discussed, did not land a general form).
**Proposal:** Fuse `s1+s2+…+sn` chains (post-inlining) into a single allocation of
the total length with unrolled part copies — the compile-time generalization of the
issue's arity specialization. Subsumes the N={2,3,4,5} runtime-specialization
debate: do it in the JIT with exact N known.
**Status:** open; partial work only.
**Bar check:** (a) semantics-preserving; (b) −28% on Concat2 reported; chains like
`a+b+c+d` in hot paths (logging, SQL building) are the benchmark shape;
(c) E3 (JIT arity knowledge); (d) not in SURV-001…040 (backlog has SSO + interning,
not concat fusion); (e) gc does not do it generally.

### C-I — Zeroing elision for provably fully-overwritten allocations
**Source:** https://github.com/golang/go/issues/24926 (open, 2019): `x:=new([1024]int)`
followed by a full init loop still pays `needszero=true`. Family:
https://github.com/golang/go/issues/57153 (avoid memclr for overwritten slices),
https://github.com/golang/go/issues/57000 (hybrid clearing strategy from prove results).
**Bucket:** WOULD-LIKE.
**Proposal:** At hot allocation sites (E4-gated), prove every byte is written before
any read (loop-aware: init loops count); allocate unzeroed (`mallocgc`-without-zero
/ fresh-page fast path) instead. Deopt-free: the proof is static per compiled
version; fall back to zeroing where unproven.
**Status:** open; the issue sketches the SSA pass shape.
**Bar check:** (a) semantics-preserving under the all-bytes-written proof;
(b) plausible win vs gc+PGO on init-heavy allocation benchmarks (the issue's
`[1024]int` init loop: saves a full 8 KB memset per alloc); (c) E4 (hot-site
gating — the analysis is only worth it where zeroing is a measured fraction) + E1
(zeroed vs unzeroed as representation choice); (d) not in SURV-001…040;
(e) gc does not do it.

### C-J — Profile-guided per-spawn-site initial goroutine stack size
**Source:** https://github.com/golang/go/issues/77893 (open): fleet-measured data —
`runtime.newstack`/`copystack` costs **3.9% core-weighted** vs GC at 7.3%; per-service
stack distributions tabled (P50 12 KB vs 4 KB start). Family:
https://github.com/golang/go/issues/18138 (per-spawn-site stack statistics, 2016),
https://github.com/golang/go/issues/27345 (parent-stack borrowing, see C-K).
**Bucket:** THINKING (industry data, no OSS-acceptable design yet — "we're happy to
contribute once you advise us on the best approach").
**Proposal:** Always-on profiler maintains per-`go`-statement max/typical stack-use
histograms; new goroutines start with a stack sized from the histogram (grown from
a cached pool of right-sized stacks) instead of 8 KB-then-copy-grow. Pure runtime
policy — no code change, no soundness risk (growth still works as fallback).
**Status:** open; data-rich, design-poor.
**Bar check:** (a) semantics-preserving — stack size is unobservable (growth is
transparent); (b) 3.9% fleet-wide is the plausibility anchor (reporter's fleet
data, not ours); benchmark sketch: fan-out/fan-in spawn-heavy server workload,
metric = copystack CPU + tail latency; (c) E4 (the profiler *is* the mechanism) +
E2 (scheduler owns spawn); (d) not in SURV-001…040; (e) gc does not do it.

### C-K — Speculative inline-stack goroutine spawn (parent-stack borrowing)
**Source:** https://github.com/golang/go/issues/27345 (open): "speculative idea from
discussion with @aclements" — run the child immediately on the free part of the
parent's stack; relocate (stack-copy, which Go already does) only if the parent
resumes first. Explicitly suggests "tracking some basic statistics on the fly to
decide whether to do this".
**Bucket:** THINKING.
**Proposal:** For spawn sites where the profiler says "child is short-lived and the
parent blocks/waits" (the overwhelmingly common fan-out shape), skip stack
allocation entirely: child runs on the parent's stack tail. Guard: if the parent is
rescheduled while the child still runs there, relocate the child (same machinery as
stack growth). Deopt-free at runtime — relocation is the fallback path.
**Status:** open, speculative, unbuilt.
**Bar check:** (a) sound via relocation fallback (stack copying is already
meaning-preserving in Go); (b) plausible win vs gc+PGO on spawn-heavy
microbenchmarks (ParallelizeUntil-shaped) and request-per-goroutine servers —
kills both the alloc and the first growth; (c) E2 (user-space scheduler: we know
exactly when the parent resumes — gc's scheduler could too, but our spawn path is
ours to redesign) + E4 (spawn-site lifetime stats are the arming signal);
(d) not in SURV-001…040; (e) gc does not do it. Compose with C-J (right-size the
borrowed region).

### C-L — Loop allocation batching: N same-type loop allocs → one slab
**Source:** https://github.com/golang/go/issues/60879 (open, FeatureRequest,
NeedsInvestigation): reporter's benchmark — N individual `new(T)` vs one
`make([]T,N)`: at N=32, **−83.93%** (reporter's numbers); community confirms it
"comes up regularly" in real code (finnishtable link in comments).
**Bucket:** WOULD-LIKE.
**Proposal:** At hot loop allocation sites allocating the same type (E4-gated),
allocate one slab and carve N objects (bump-pointer within the slab), instead of N
mallocgc calls. Needs GC cooperation: per-object pointer maps inside the slab or
slab-as-object-array with interior headers — the design work. Distinct from
SURV-025 (object inlining into parent) and SURV-003 (arenas): this batches
*sibling* allocations, preserving each object's identity.
**Status:** open; no design.
**Bar check:** (a) semantics-preserving — object identities/distinctness preserved,
only the backing store is shared (GC must still find each object's pointers —
the load-bearing design constraint); (b) −84% at N=32 in the reporter's
microbenchmark is the plausibility anchor; benchmark sketch: tree/graph builders
that `new(Node)` in a loop; (c) E1 (representation: N logical objects, one physical
slab) + E4 (hot-site gating); (d) not in SURV-001…040; (e) gc does not do it.

### C-M — Interface round-trip conversion elision (`any→T→any` reuses words)
**Sources:** https://github.com/golang/go/issues/64541 (open): `s,ok:=v.(string);
sink(s)` allocates in `convTstring` (67.47 ns/op, 1 alloc) although `s` came from
an `any` — vs `sink(v)` at 5.3 ns/op, 0 alloc; https://github.com/golang/go/issues/24582
(open, 2018): compiler avoids the alloc for `interface{}→string` but not when the
target interface has methods (`convT2Istring`).
**Bucket:** WOULD-LIKE.
**Proposal:** Dataflow-aware interface conversion: when a value is asserted out of
an interface and converted back to (a supertype of) the same interface with no
intervening mutation, reuse the original (type, data) words — no `convT*` call, no
alloc. The method-ful case (#24582) falls out of the same rule.
**Status:** both open; no owners.
**Bar check:** (a) semantics-preserving — identical type and data pointer;
(b) 67→5 ns/op, 1→0 allocs in the reporter's benchmark; `any`-heavy code
(serialization, `fmt`) is the benchmark shape; (c) E1 (interface word reuse is a
representation decision); (d) not in SURV-001…040; (e) gc does not do it.

### C-N — Non-work-conserving scheduler micro-policy (learned wake/wait/run costs)
**Source:** https://github.com/golang/go/issues/54622 (open, NeedsInvestigation):
Go's scheduler is work-conserving — always wakes a thread for new work. The issue
models the three costs (wake / wait-for-running-thread / run) and argues waking is
heavyweight enough that short work shouldn't always trigger it; cites #18237,
#21827, #32113 as contributors.
**Bucket:** WOULD-LIKE.
**Proposal:** In our user-space scheduler, implement the issue's cost model as
policy: don't wake a sleeping M/P for work predicted (E4) to be shorter than the
wake+sleep cycle — queue it for an already-running P instead, with hysteresis and
a starvation bound. This is scheduler *policy*, not a code transform — only
possible because we own the scheduler (E2).
**Status:** open; NeedsInvestigation; gc hasn't built it.
**Bar check:** (a) semantics-preserving — scheduling decisions are unobservable
except via timing; (b) plausible CPU-efficiency / tail-latency win on bursty
server workloads (the issue's profiling claim is the anchor; our benchmark:
bursty RPC-shaped load, metric = CPU per request at fixed latency);
(c) E2 (true-idleness + holder/waiter signals are exactly the inputs the policy
needs) + E4; (d) not in SURV-001…040 (backlog "adaptive concurrency primitives"
is about *locks*, not wake policy); (e) gc does not do it.

## 3. Declined / attempted / frozen proposals (obstacle knowledge, not candidates)

1. **#16798 — tail-call optimization for self-recursion — DECLINED (closed).**
   Proposal: TCO for self-recursion only (stack traces "roughly" preserved).
   Decline reason: Go's stack-trace fidelity is a design constraint — the Go team was
   "very clear that they wanted to keep the stack traces clean and accurate"
   (ghost comment, 2016-08-19); closed as dup of #15304 (full TCO, declined).
   **Routing:** none — TCO removes observable frames (runtime.Callers/debug.Stack),
   so it fails bar (a) for Go semantics. Do not resurrect.
   https://github.com/golang/go/issues/16798
2. **#21536 — `//go:inline` directive — DECLINED (closed, rsc).**
   Proposal: user directive forcing inlining. josharian: "This proposal has
   basically no chance of being accepted. Go eschews knobs. Compiler directives are
   widely disliked." rsc: "we're not going to do this."
   **Routing:** yes — the decline targets the *user-facing knob*, not inlining
   itself. Our route is PGO-driven inline policy (SURV-006 priority inlining): the
   profiler, not the programmer, decides. No directive needed.
   https://github.com/golang/go/issues/21536
3. **#41948 — selectgo: don't lock all channels — ATTEMPTED, abandoned.**
   Dmitry's attempt: https://go-review.googlesource.com/c/go/+/7570. randall77:
   "it is tricky to get right. We never convinced ourselves it was correct."
   Obstacle: lock-ordering / deadlock-avoidance proof across channels.
   **Routing:** do not resurrect as stated — the obstacle is a fundamental
   correctness proof, not a missing profile signal. The *motivation* (select
   overhead) is addressed by SURV-032 (JIT-specialized select lowering) instead.
   https://github.com/golang/go/issues/41948 (see also #8896)
4. **#47529 — `//go:noboundcheck` — DECLINED (+1:0/−1:11)** — already recorded in
   [SURVEY.md](SURVEY.md) §2.13; re-verified still closed/declined. Routing stands (SURV-033).
   https://github.com/golang/go/issues/47529
5. **#26680 — pack info into low bits of interface type pointers — THINKING
   (author-pessimistic).** Author (Go team): "We've generally avoided doing any bit
   packing, so I don't anticipate this happening, but wanted to record the idea."
   Data: convT2Estring dominates make.bash profiles (top entries 5–8% each, mostly
   len ≤ 11 strings).
   **Routing:** complementary mechanism to SURV-020 (speculative niche-filling):
   #26680 is the *static* variant (length-in-type-word for small strings/slices).
   The Go team's cultural obstacle (no bit packing) doesn't apply to us — our
   descriptor model (E1) expects representation experiments. Fold the static variant
   into SURV-020's design space.
   https://github.com/golang/go/issues/26680
6. **#67520 — proposal: simd: new package for intrinsics — FROZEN (FrozenDueToAge),
   not declined.** User-facing API proposal, not a compiler optimization; no
   transfer. https://github.com/golang/go/issues/67520
7. **#75273 — proposal: parameter-level `noescape` modifier — FROZEN
   (FrozenDueToAge).** User-asserted escape annotations. Complementary (not
   competing) with our speculative EA: annotations are facts, speculation is
   betting — the JIT can *trust-but-verify* annotations as an additional signal.
   https://github.com/golang/go/issues/75273
8. **#81004 — proposal: integrate gvisor/checkescape in the compiler — open
   Proposal.** Escape-*verification* tooling (tests that code doesn't escape).
   Process note for our testing doctrine (escape-behavior regression tests), not an
   optimization. https://github.com/golang/go/issues/81004

## 4. Swept, nothing applicable (negatives — first-class)

**Covered by backlog devirtualization cascade (not re-listed):**
#38992 (post-inlining devirtualization), #42569 (func vars never reassigned →
indirect call — == SURV-004), #51554 (missed devirtualization), #29276 (interface
dispatch, 2018 varint case), #64675 (PGO devirt of closures — cascade must handle
closure *context*; recorded as a cascade requirement, not a candidate),
#69015 (range-over-func iter inlining — iterator is a func value; cascade's job).

**Covered by backlog aggressive/optimistic escape analysis:**
#27625 (stack-alloc dynamic-size non-escaping slices — == SURV-003),
#20533 (EA of make([]T,n)), #77087 (EA induction through map values),
#77088 (conditional EA), #74364 (conditional escapes via splitting),
#74383 (devirtualized type-switch temp escapes), #74356 (func-in-interface escapes),
#72036 (conditionally stack-alloc w.Write(b)-style slice args),
#51462 (self-assigned append escape), #22081 (return-value allocs),
#48849 (generic arg escapes), #79592 (EA on generic impls),
#57060 (reflect.Value.Set short-lived Values — EA through reflect.Value),
#71349 (reflect.Value.Interface boxing — EA), #18300 (closure capture).

**Covered by SURV-015 (map read-modify-write fusion):** #17133 (multiple accesses
to same key — the issue IS the motivating example; add as precedent),
#70837 (merge map operations).

**Covered by SURV-007 (aggregate copy elision via field-use projection):**
#18004 (map iteration copies whole struct), #59706 (range over large elements),
#24386 (keeping a subset of struct fields).

**Covered by SURV-017 (allocation-site capacity speculation):** #14325 (append
loop recognition), #64595 (pre-allocate provable length), #64703 (online slice/map
pre-sizing — the issue's title is literally the SURV-017 idea; add as precedent).

**Covered by SURV-031 ([]byte(s) copy elision):** #18822 (Write([]byte(stringVal))
no-copy), #47454 (append([]byte(string),…)), #27148 (range over string([]byte)).

**Covered by SURV-032 (JIT-specialized select lowering):** #64900 (fast paths for
closed+nil select patterns — include its 126ns→0.9ns numbers as supporting
evidence), #20351 (select on shared channel — contention input for the design),
#57070 (single-channel contention consumes all Ps — design input for backlog
adaptive concurrency).

**Covered by SURV-005/SURV-040 (code layout):** #63678 (asm routine alignment),
#66420 (greedy block layout), #65068 (error blocks to end), #20356 (compact
loop-free layout).

**Covered by SURV-004 (monomorphic func-value sites):** #42569 (above).

**Covered by §2.5 placement-new routing:** #81649 (composite literals from inlined
constructors not initialized in place — descriptor+PEA is the answer).

**gc-internals / backend codegen Graal already does (no architectural delta):**
#30509 (growslice check elision — GVN), #42999 (nil-err branch after inlining —
const prop), #29716 (CAS loop codegen), #77090 (sliding-window reslice — loop
opts), #23929 (large-zero comparison), #15808 (LICM), #63670 (LICM),
#69889 (dead-loop elimination), #20934 (pure-call CSE), #26734 (append lowering
24% — codegen quality; folded as evidence into C-A), #81887 (BinarySearchFunc
inline budget), #16869 (reflect.TypeOf intrinsic — table stakes),
#12628/#24177/#17862/#25298/#46711/#37608 (branchelim family), prove-pass family
(#25086/#25087/#25115/#25167-69/#26299/#27572/#32515/#36889/#54544/#25862),
BCE family (#14808/#17370/#24686/#27585/#28942/#34816/#40987/#50365/#61333/#64272/
#70062/#73145/#75955/#76354/#78946/#28314), regalloc family
(#16122/#44670/#53599/#71868/#81131/#74365/#32255), arch-specific peepholes
(#14059/#15105/#15300/#17110/#19715/#23006/#25156/#37096/#47969/#59089/#59120/
#71780/#72741/#74489/#76066/#77541/#80400/#81609/#81755/#81839/#38547/#39710/
#27780/#25111/#41774), memmove/memclr family (#25317/#49058/#23306/#74171),
ABI/call-setup (#23377/#44827/#61984), const-folding minutiae
(#16108/#29095/#29166/#62049/#73601/#75201/#76372/#26498/#43403),
div/mod strength reduction (#16416/#25733/#50126/#59089), FP-specials likelihood
(#25125), switch codegen (#15164/#24650/#29292/#48220/#17622/#28045),
defer/panic minutiae (#38471 open-coded defer inlining — JIT inlines anyway),
#17216 (unused runtime calls — DCE), #20354/#37415/#16973/#53300/#49332/#79195
(CSE/CFG internals), #76073 (frame live-range packing), #80095/#80146/#80378
(compile-time), #75001 (line numbering), #18739 (order.go), #10292/#35196/#59708
(closure inlining — JIT), #34525 (single-caller inline), #71598 (indirect-import
inline), #20019 (linkname inline), #48192 (error-wrapper inline),
#30605 (wasm), #75056/#78370/#78379/#78808 (generics dict internals),
#59591/#57072 (generic type-switch opts), #81377 (generic dict devirt —
SURV-016's family), #29085 (variadic specialization — SURV-011/012 family),
#17566/#61502 (inliner overhaul), #19195 (scanobject), #23948 (zeroVal statics),
#29691/#29693/#28639 (static init), #30528 (hash/eq for global arrays),
#32554/#32969/#33012 (misc codegen), #65192 (mov-s), #49785 (vague),
#48637/#54612 (array init loops), #14723 (array cmp/hash size 1),
#64634 (crypto intrinsics proposal — policy), #31265/#39139/#71270 (intrinsics),
#70956 (rodata loads), #19042 (wrapper prologue), #36351 (CPU feature detection),
#30946 (popcount check), #76722 (signed/unsigned prove), #78390 (fallthrough BCE),
#77433 (check insertion), #42094/#42179 (shift checks), #76703 (large returns),
#80806 (struct param leaks), #78597 (struct literal temp), #39668 (range over
empty array), #57740 (Windows stdout escape), #57910 (Mutex escape),
#75620→C-G (listed as candidate), #53643 (Clone — C-G family),
#64541→C-M (listed), #24582→C-M (listed), #41174 (iface-iface assertions),
#51133 (convI2I/assert fast paths), #25130 (rematerialization),
#19165→C-F (listed), #17133→SURV-015 (above), #26951→C-D (listed),
#21639 (generic map strength reduction — runtime internals),
#26059 (map op wrappers), #68912 (tophash emptyRest), #80972 (fxhash),
#36915 (map hash tradeoffs), #19992 (evacuated buckets), #20135 (map shrink),
#70617 (map clear), #19495 (small maps vs nodejs — representation, backlog
speculative-data-representation's map axis), #71255 (swiss SIMD non-amd64 —
implementation), #78378 (mapaccess dict), #18004→SURV-007 (above).

**Runtime/GC/scheduler internals — no per-benchmark JIT optimization transfers:**
#14179 (GC wake idle Ps), #22654/#22693/#21056/#42430/#45894/#81657/#20307/
#61426/#61427/#61428/#79587/#24928/#20005 (GC/sparse-zeroing/locks),
#79931 (Green Tea GC), #54444 (wasm GC), #80389 (process_madvise),
#18020/#18236/#28701/#40229 (preemption/sysmon), #49026 (netpoll),
#53824 (epoll_pwait2), #80739 (mmap registry), #73055 (macOS wakeups),
#36603 (sysUsed), #15290 (time.Now/taskgated), #15110 (file-backed serve),
#75188 (AddCleanup), #65647/#74000/#74003/#64455/#37429/#16141/#16187/#16192/
#57505/#68815/#71400/#18977 (regressions/perf mysteries), #77332 (build speed),
#19574/#38325/#28454 (cgo), #19509 (32-bit div), #19650 (plan9),
#65440 (wasm perf), #15397 (ephemeral slicing/GC — needs deeper read; parked),
#14921/#15238/#48245 (write barriers — GC subsystem), #64634 (above),
#54622→C-N (listed as candidate), #20351/#57070 (design inputs, above),
#54622's cited #18237/#21827/#32113 (not individually swept — folded into C-N).

**PGO/tooling infra (enablers, not candidates):** #59612 (intra-line PGO),
#59616 (LBR PGO), #70393 (default PGO profiles), #64676 (PGO devirt export data).

### C-O — Profile-guided format-call specialization (`fmt.Sprintf` with constant format → direct builder)
**Source:** https://github.com/golang/go/issues/81230 (closed, unimplemented —
discussion drifted to partial evaluation; randall77's closing suggestion was a
`go fix`-style source rewrite, i.e. gc won't do it in the compiler).
**Bucket:** WOULD-LIKE.
**Proposal:** At hot `fmt.Sprintf/Errorf` sites where the format string is
profile-constant (very often source-constant), JIT-specialize: parse the format
once, emit a direct conversion chain (string concat / strconv.AppendInt / …)
with no reflect, no arg boxing, no format re-parsing. Guard on format-string
identity; deopt to the generic path otherwise. This is the JIT analogue of what
the issue asked gc to do statically.
**Status:** closed, unimplemented; no owner.
**Bar check:** (a) semantics-preserving for the guarded format (verbatim
`%v`/width/precision semantics must be reproduced — the load-bearing
correctness work); (b) plausible win vs gc+PGO on logging/serialization-shaped
benchmarks (fmt is famously reflect+alloc heavy); (c) E3 (per-site guard +
deopt) + E4 (hot-site gating); (d) not in SURV-001…040; (e) gc does not do it.

## 5. Blog / talk sweep (named authors + GopherCon + go101 + go-perfbook citations)

**Felix Geisendörfer** (blog.felixge.de; Datadog):
- "Blazingly Fast Shadow Stacks for Go" (2024-05-28,
  https://blog.felixge.de/blazingly-fast-shadow-stacks-for-go/): software shadow
  stack → up to 8× faster stack-trace capture vs frame-pointer unwinding
  (best case), up to 4× *slower* at shallow depths (worst case). Author's own
  verdict: "interesting, but not warranted at this point" — FP unwinding is
  already <a few % of the execution tracer's overhead; hardware shadow stacks
  would break the trick; external unwinders (perf) break. **Verdict:** profiling-
  infrastructure idea, author-declined. Not a program-optimization candidate.
  Note for E4 (always-on profiler): the technique exists if stack capture ever
  becomes our bottleneck.
- datadog-pgo (continuous PGO, "up to 14% CPU" on Datadog services, inlining +
  devirtualization): supports our E4 direction; it's a deployment story, not a
  new optimization. No transfer beyond "always-on production profiles work".
- Execution-tracing overhead work (1.21, <1%): profiling infra, no transfer.

**Jaana Dogan**: recent output is observability/AI-coding, not optimizations;
GopherCon 2020 "Debugging Code Generation in Go" is educational (how to read
codegen), no new optimization ideas. Swept, nothing.

**William Kennedy (Ardan Labs)**: training-focused ("Ultimate Go", mechanical
sympathy); no compiler-optimization proposals beyond known practice. Swept.

**Bryan Boreham**: "Blazing Fast Merge with Loser Trees" (Prometheus/Loki):
4–8× merge wins from less pointer chasing + generics devirtualization; talk notes
"devirtualization and PGO can further improve performance" — supporting evidence
for the devirt cascade + PGO, not a new idea. GC-settings talk is tuning. Swept.

**Phil Pearl** (philpearl.github.io, Ravelin):
- "Bad Go" series (slices of pointers, not sizing slices, interface costs):
  programmer guidance with benchmarks (e.g. []\*T vs []T: 2.50µs→117ns,
  100→0 allocs) — motivates our EA/copy-elision work but proposes no compiler
  mechanism. Swept.
- "Swissing a table" (2026-04-24, https://philpearl.github.io/post/swissing_a_table/):
  walkthrough of Swiss-table design behind Go's new map implementation
  (internal/runtime/maps). Implementation detail of gc's map; no missing-
  optimization proposal. Context for SURV-015/018 map work, not a candidate.
- "Go interfaces, but at what cost?" (2019): quantifies interface call overhead
  (4.63 vs 2.44 ns/op) — supporting evidence for the devirtualization cascade.

**Michael Stapelberg** (michael.stapelberg.ch):
- "Debian Code Search: Fast TurboPFor with Go SIMD" (2026-09-06,
  https://michael.stapelberg.ch/posts/2026-09-06-dcs-fast-turbopfor-go-simd/):
  replaced cgo TurboPFor with Go 1.26 `simd/archsimd`: 2× (popcount) / 3×
  (full-block decode) over scalar Go, parity with C. Key compiler note:
  gc re-derives loop indices (`POPCNTL; ADDQ; LEAQ (base)(CX*4)`, 3 insns) where
  clang uses one `lea` — a strength-reduction gap in gc's backend that "might be
  easy or prohibitively complex" to fix. **Verdict:** the SIMD story is the
  explicit-API alternative to auto-vectorization (already noted in SURVEY §3 for
  JEP 417); the index-rederivation gap is backend codegen our Graal backend
  handles. No new candidate; supports SURV-038's vector-plan framing.

**GopherCon compiler/performance talks (2024–2025 search)**: nothing proposing
unbuilt optimizations — Alan Donovan's 2025 talk is about analysis/refactoring
tooling (inlining algorithm reused for migration tooling — interesting reuse, not
an optimization). Swept.

**go101.org "Go Optimizations 101"** (https://go101.org/optimizations/101.html):
programmer-facing tricks (sizing, BCE hints, allocation placement); the meaty
chapters (escape analysis, structs, slices, strings, channels, functions,
interfaces) are paywalled. The free index shows no compiler-mechanism proposals.
Swept — nothing applicable beyond what's already in the issue tracker.

**dgryski/go-perfbook citations (outward only, not re-read)**: issue citations
are #18597 (register ABI — shipped in Go 1.17/1.18, done), #26891 (below),
#9704 (in TODO, unchecked — assembly-era). No live missing-optimization leads.

## 6. TinyGo escape-analysis gap vs gc

Sources: `tinygo/transform/allocs.go` (fetched via mirror;
"https://git.jwetzell.com/tinygo-org/tinygo/src/commit/adadbadec30b59f857ce48d7de547c0315d53356/transform/allocs.go"),
TinyGo v0.34.0 changelog
(https://github.com/tinygo-org/tinygo/blob/HEAD/CHANGELOG.md).

Findings:
- TinyGo's EA (`OptimizeAllocs`) is *simpler* than gc's, not better: it replaces
  `runtime.alloc` calls with stack allocs "if the allocated value does not
  escape", using **LLVM's `nocapture` flag for interprocedural EA** plus an
  intraprocedural check, with `maxStackAlloc = 256` bytes ("TODO: tune this,
  this is just a random value").
- Where TinyGo wins is **whole-program visibility** (closed world, small
  embedded programs): interprocedural `nocapture` across the entire program,
  blocking-function analysis, scheduler-needed analysis, type-based alias
  analysis for type switches/asserts, whole-program DCE. gc can't do
  whole-program EA (per-package compilation). **This is supporting evidence for
  SURV-001's closed-world whole-program mode** — whole-program EA is one of its
  concrete wins — not a new candidate.
- Targeted EA-precision fixes worth stealing as requirements on our aggressive
  EA (backlog): v0.34 "do not let the slice buffer escape when casting a
  `[]byte` or `[]rune` to a string, to help escape analysis"; "mark
  stringFromBytes/stringFromRunes as nocapture/readonly". I.e. conversion
  builtins must be modeled as non-capturing — file as EA precision requirements.
- v0.34 "transform: optimize range over `[]byte(string)`" is the same
  StringToBytes transform already recorded as SURV-031's ABLE precedent
  (tinygo PR #3923) — no double count.
- **Verdict:** no case found where TinyGo's EA is strictly more precise than
  gc's on the same program; its edge is whole-program scope. No new candidate;
  two EA-precision requirements noted above.

## 7. Declined-proposal additions from citation-following

9. **#26891 — proposal: unsafe: inline assembly with `unsafe.Asm` — DECLINED
   (closed).** Via go-perfbook citation. Language feature (user-written asm),
   not a compiler optimization; the Go team doesn't want inline asm in the
   language. No transfer (our story is a better backend, not user asm).
   https://github.com/golang/go/issues/26891

## 8. Resume notes

- Rate limit: unauthenticated 60/hr core; search 10/min. All issue fetches done;
  no further API needs except optional comment-depth on candidates at synthesis.
- Not yet fetched (optional, low priority): comments on C-B #51553 beyond p1
  (bcmills' EA-reduction noted); #60879 design discussion; #77893 follow-ups.
- Open question for coordinator: C-F vs SURV-012 boundary (const-arg vs
  concrete-type specialization) — kept separate; synthesis may merge.
- Pre-2020 coverage: #16798 (2016), #21536 (2017), #26891 (~2018), plus full
  title sweep back to 2015 (#10292…) — done.
