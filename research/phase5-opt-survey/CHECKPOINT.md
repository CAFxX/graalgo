# Phase-5 optimization survey — coordinator checkpoint

**Registered:** 2026-10-04 (~08:31 CEST) as `graalgo-opt-survey` (kind: coordinator).
**Mission:** parallel literature + industry survey for Phase-5 optimization candidates beyond the existing backlog. Non-blocking vs Phase 1.
**Deliverable:** `~/workspace/graalgo/research/phase5-opt-survey/SURVEY.md` (ranked candidates + negative results + per-track notes). Do NOT edit SPEC.md / PHASES.md / registry — survey proposes only.

## Fan-out (2026-10-04 ~08:35 CEST)
- Track 1 — academic literature (PLDI/CGO/OOPSLA/ASPLOS/VEE/ISMM): spawned, writing `track-1-academic.md`
- Track 2 — industry JVMs (HotSpot/GraalVM/OpenJ9/Azul): spawned, writing `track-2-jvms.md`
- Track 3 — dynamic-language runtimes (V8/JSC/.NET/Cinder-Pyston/HHVM): spawned, writing `track-3-dynamic-runtimes.md`
- Track 4 — Go-adjacent (golang/go issue mining + community blogs/talks + TinyGo/llgo/gccgo), expanded per owner refinement: spawned, writing `track-4-go-adjacent.md`

## Next
Collect the track files on completion, dedupe against each other and the backlog, rank by (expected value × feasibility), write SURVEY.md, heartbeat, report to parent.

## Status 2026-10-04 ~08:36 CEST
- Track 1 (academic) COMPLETE: 8 candidates (C1–C8), 3 near-misses, negatives logged.
- Track 2 (industry JVMs) COMPLETE: `track-2-jvms.md` (513 lines). 6 candidates pass the bar: (1) cross-run speculation-profile cache (ReadyNow/OpenJ9-AOT/JEP 483–516 lineage); (2) per-allocation-site compact headers (Lilliput lesson via descriptors); (3) priority call-graph-simulating inlining + path duplication (Graal GR-77137); (4) optimistic aliasing speculation under guard+deopt; (5) lock coarsening across control flow (Graal GR-79526); (6) identity-freedom analysis → header-free flattened values (Valhalla JEP 401). 5 near-misses. Buckets: ABLE 14 / THINKING 4 / ATTEMPTED 3 / WOULD-LIKE 1. Top 3: cross-run speculation cache, optimistic aliasing speculation, priority inlining. Depth-steering arrived after completion — wave 2 will deepen as needed. Follow-up for wave 2: "Crema"/"Terminus" need verification — pointers: https://github.com/oracle/graal/issues/11327 (Crema), https://github.com/oracle/graal/issues/12236 (Terminus).
- Track 3 (dynamic runtimes) COMPLETE: `track-3-dynamic-runtimes.md` (195 lines, 9 candidates). 9 candidates, all WOULD-LIKE for Go (precedents 7× ABLE, 1× THINKING (C9 ← Swift SE-0474), 1× ABLE+ATTEMPTED (C6 ← HHVM repo-authoritative + HPHPc)): C1 monomorphic func-value call-site caching (`sort.Slice` comparator inlining); C2 div/mod by profile-constant → strength reduction; C3 allocation-site capacity pre-sizing; C4 inline-cached map slot lookup; C5 speculative loop unswitching w/ OSR exit; C6 closed-world whole-program AOT; C7 phase-adaptive rejit; C8 speculative generic-instantiation specialization; C9 map RMW fusion via borrowed pointer. 10 near-misses; 14 first-class negative results incl. 4 ATTEMPTED with failure reasons (TraceMonkey, HPHPc, Ruby MJIT, JSC LLVM→B3) + 1 THINKING (ZJIT → wave 2). Top 3: C6 closed-world AOT, C2 profile-constant division, C1 func-value call-site caching. Coverage notes include 13-item wave-2 backlog (V8/JSC/.NET sources, Cinder primaries, modern Pyston, TruffleRuby/GraalPy coordination with T2, PyPy, RaptorJIT).
- Track 4 (Go-adjacent) COMPLETE: `track-4-go-adjacent.md` (164 lines, 7 candidates C1–C7). New candidate C7: read-only `[]byte(s)` copy elision (alias string buffer at hot conversion sites with proven read-only use; precedent TinyGo PR #3923; gc lacks it — golang/go#26498 unbuilt). Sub-track (c) closed for wave 1, mostly negative: TinyGo (size/MCU-oriented, no PGO — its one transferable transform became C7), llgo (original dead; xgo-dev/llgo interop-focused, 1:1 OS-thread goroutines, no PGO), gccgo (stale frontend). Negatives + coverage notes recorded incl. wave-2 list (systematic label sweeps, declined-proposal enumeration, pre-2020 issues, named blog authors, TinyGo EA gap).
- Track 5 (native compilers) still running — last one.

## Standing directive 2026-10-04 ~08:35 CEST (program owner)
LONG-TERM background endeavor, not time-boxed. Operate in waves indefinitely: survey → synthesize SURVEY.md → gap analysis → next wave. No "done" until owner says stop or Phase 5 begins. SURVEY.md is living. Negative results first-class. Checkpoint everything; hourly heartbeats via `graalgo-opt-survey-heartbeat` cron (retire it when the survey stops).
Candidate pipeline: raw (track files, unvetted) → vetted (bar-checked, deduped vs backlog and other tracks) → ranked (expected value × feasibility, in SURVEY.md).

## Refinement 2026-10-04 ~08:33 CEST (program owner)
Industry scope = four buckets, filed per candidate in SURVEY.md:
1. ABLE — what production compilers/languages do today.
2. THINKING — roadmaps, RFCs, active proposals (Valhalla, Leyden/Crema/Terminus, Swift evolution, Rust RFCs, open JDK JEPs).
3. ATTEMPTED — tried and abandoned/reverted WITH reason (TSX lock elision, biased locking removal); failure reason names the obstacle; note when our architecture routes around it.
4. WOULD-LIKE — wishlists, "we wish the compiler did X", open RFCs nobody picked up.
Covers compilers AND languages (language-level features → runtime-level optimizations).
- Track 2 steered: + .NET Native/AOT, four buckets mandatory.
- Track 3 steered: + Swift (incl. evolution proposals), four buckets mandatory.
- Track 5 spawned: native compilers (rustc, Clang/LLVM, GCC) + language RFCs + wishlist mining, four buckets mandatory.

## Wave-1 synthesis COMPLETE 2026-10-04 ~10:30 CEST
- Track 5 COMPLETE: `track-5-native-compilers.md` (696 lines). LLVM 16 passes classified pass-by-pass vs gc 1.27.1 SSA passes (verified local source; gc PGO consumers = inlining budgets + devirtualize only); GCC IPA passes; rustc (niches, attributes, monomorphization); Rust RFCs (2000, 1210, 469/809/1228, 3204); Swift SE-0377/SE-0390; JDK JEPs (401, 417, drafts 8316779/8261529); wishlist mining (LLVM VPlan, GCC value-profiling, open Rust RFCs). 14 candidate sections + 3 ATTEMPTED (Graphite, placement-new saga, RFC 1210 specialization) + 12 negatives.
- **SURVEY.md written (1502 lines):** 40 ranked candidates SURV-001…SURV-040 in 4 tiers (expected value × feasibility); §2 = 13 ATTEMPTED failure-lessons with routing analysis; §3 = complementary angles on backlog items; §4 = enabling infrastructure (assumption-dependency framework, OSR-exit, deopt-budget discipline); §5 = consolidated negatives across all 5 tracks; §6 = per-track wave-1 coverage + prioritized wave-2 queue; §7 = candidate pipeline (raw → vetted → ranked, stable IDs, no renumbering).
- Tier 1 (build first): SURV-001 closed-world whole-program AOT devirtualization (T3-C6+T5 merged); SURV-002 speculative per-call-site escape analysis behind interface calls (T4-C4); SURV-003 scoped arenas/alloca (T4-C2); SURV-004 monomorphic func-value call sites (T3-C1); SURV-005 PGO-driven code layout (T5); SURV-006 priority inlining + path duplication (T2).
- Cross-track merges preserved with traceability: T3-C6+T5 ipa-devirt → SURV-001; T3-C8+T5 generic specialization → SURV-016; T1-C8+T5 loop multi-versioning+suite → SURV-010.
- Evidence honesty: all paper/vendor numbers labeled as source claims, never our measurements; prototype-gated items marked (SURV-020, 029, 030, 038); conditional items flagged (SURV-030 backend-choice, SURV-039 moving-GC).
- SPEC.md / PHASES.md / requirements registry NOT touched (survey proposes only).
- Next: wave-2 mining per SURVEY.md §6 queue (1. T4 label sweeps + named blogs; 2. T3 source-level passes; 3. T1 forward-citation sweep; 4. T5 pipeline-order walk; 5. T2 continuous re-sweep), then re-rank. No "done" until owner says stop or Phase 5 begins.

## Wave 2 — in progress (2026-10-04 ~09:15 UTC)
Coordinator: wave-2 subagent (depth 1). Wave-1 state: 40 ranked candidates SURV-001…040, §6 queue prioritized.
Wave-2 fan-out:
- Worker A (T4): golang/go label sweeps (`label:Performance` + `label:compiler/runtime`, paged API) + declined-proposal enumeration + named-author blogs/talks → `track-4-wave2-tracker.md`.
- Worker B (targeted, program-owner question): gc "reusing heap objects proven dead" — exact status in golang/go, form, stated obstacles, issue citations; verdict: races our backlog item or fills a gap → `track-4-wave2-deadobj-reuse.md`.
- Worker C (T3): source-level passes — V8 (`map.h`, `src/compiler/` IC/deopt), JSC (`dfg/`, `ftl/b3`), .NET RyuJIT (dynamic-PGO probes, GDV codegen), PyPy meta-tracing guard/deopt discipline → `track-3-wave2-sources.md`.
Next SURV- IDs: 041+. Synthesis: dedupe vs backlog + SURV-001…040, vet vs bar, rank, update §1/§6, mark complete.

## Wave 2 refinement (2026-10-04 ~09:12 UTC, program owner)
Worker B redirected: gc's experiment is NAMED — `runtimefree` / GOEXPERIMENT=runtimefreegc. Primary sources: golang/go#74299, https://go.dev/design/74299-runtime-freegc, CL 720120 (disabled by default for Go 1.26), CL 710015 (alias analysis + auto-free after growslice), CL 673695 (runtime.freegc/freegcTracked); known bugs #80697 (alias misclassification on multi-assign append), #79909 (range retaining backing store). Recording brief: (1) exact mechanism + scope, (2) obstacle/failure list, (3) what it does NOT cover vs our backlog item (direct slot reuse without allocator round-trip; JIT speculation + deopt where static analysis bails). Verdict: race head-on vs fill gap, with evidence.

## Wave 2 worker update (2026-10-04 ~09:20 UTC)
- Worker B (runtimefree investigation) COMPLETE → `track-4-wave2-deadobj-reuse.md` (344 lines). Verdict: SPLIT — (a) raced: static freeing of append-growth intermediates (CL 710015) subsumed when experiment graduates; (b) gaps: allocator-bypass slot reuse (gc does free-early + re-allocate, not true slot reuse), speculative reuse with guard/deopt (gc AOT-only, structurally cannot), descriptor-driven cross-type reuse. Recommendation for synthesis: re-scope backlog item to lead with speculative dead-object slot reuse; demote static growslice pattern to "already raced". Bugs verified: #80697 (open), #79909 (fixed via CL 789060).
- Workers A (tracker sweeps) and C (T3 sources) still running.

## Wave 2 worker update (2026-10-04 ~09:25 UTC)
- Worker C (T3 source-level passes) COMPLETE → `track-3-wave2-sources.md`. Findings pinned to commits (V8 7dce5a4, JSC 40508b4, .NET d90dbf4, PyPy 15f6096). Key policies mined: V8 poly-cap 4 → megamorphic cliff, miss-against-own-cache downgrade, no-evidence→generic with named reason; JSC spec-fail buckets, OSR-exit failure-address profiling, ArithProfile violation-only bitsets, reopt after 100 exits (5 from loops) with doubling thresholds, reoptimizationRetryCounterMax=0; .NET GDV diamond (no deopt), ladder cap 5 (CoreCLR)/3 (NativeAOT), 75% chain gate, 32-slot histogram sampled every 64th; PyPy lossy 5-way fractional timetable, bridge after 200 failures, retrace_limit=0, blackhole interpreter, counter decay every 32 minor GCs. Mined: 3 WOULD-LIKE policy candidates (P1/P2/P3/P5, bar-checked, no IDs yet); P4 → complementary angle on SURV-009/SURV-033; .NET ladder numbers → angle on devirt cascade. Negatives recorded. /tmp/wave2-src ephemeral — findings file is durable.
- Worker A (tracker sweeps) still running. Synthesis pending its completion.

## Wave 2 — complete (2026-10-04 ~12:30 UTC)
- Synthesized 19 new candidates: SURV-041…SURV-059 (1 Tier 1, 15 Tier 2, 3 Tier 3). 59 ranked total; no IDs reused or renumbered.
- T4: 15 mined (C-A…C-O, all issue-URL-verified), 14 ranked, 1 raw (C-G read-only borrow — pending soundness re-verification). Open boundary: C-F (SURV-044) vs SURV-012 — kept separate pending wave-3 review.
- T4 targeted (program owner's question): gc = GOEXPERIMENT=runtimefreegc, tracking golang/go#74299, design go.dev/design/74299-runtime-freegc; disabled by default in Go 1.26 (CL 720120); CL 710015 (alias analysis + growslice auto-free), CL 673695 (runtime.freegc); bugs #80697 (open), #79909 (fixed). Verdict: SPLIT — static free-early sub-case races gc (conceded in SURV-056); our open gap is allocator-bypass slot reuse + speculative guard/deopt.
- T3: 4 speculation-policy candidates ranked (SURV-052…055); P4 → §3 complementary angle; .NET ladder → §3 devirt-cascade angle.
- §2 +3 ATTEMPTED lessons (#41948 selectgo lock-all, #21536 //go:inline, runtimefreegc obstacle list). §3 complementary angles added (TinyGo EA reqs, #26680/#75273/#64900/#64703/#17133, JSC violation-bitset discipline, .NET GDV, Stapelberg/Boreham/Pearl). §4 +2 infra (invalidation registry, profiler liveness gating). §5 negatives (TCO, unsafe.Asm, simd pkg, shadow stacks, blog/talk sweeps, PyPy bridges, JSC lattice).
- §6 wave-3 queue written (C-G re-verification first; T1 citations, T5 pipeline order, T2 re-sweep, T3 remaining sources, T4 comment-depth, re-rank). T1/T5 not worked in wave 2 — carried forward.
- No successor spawned per task; supervisor cron drives wave scheduling.

## Wave 3 — in progress (2026-10-04 ~09:37 CEST)
Supervisor heartbeat cron: no STOP file; wave 2 marked complete; registry hb fresh; no wave in-progress → spawned wave-3 coordinator subagent (same bar/discipline as prior waves). Queue per SURVEY.md §6: C-G soundness re-verification first, then T1 citations, T5 pipeline order, T2 re-sweep, T3 remaining sources, T4 comment-depth, re-rank. Next SURV- IDs: 060+.

## Wave 3 — coordinator worklog (2026-10-04 ~09:40 CEST)
Coordinator (depth-1 wave-3 subagent) active. Wave-3 fan-out (staggered, ≤2 heavy pipelines):
- Coordinator handles queue item 1: C-G soundness re-verification + SURV-044/012 boundary review.
- W1 (T1): forward-citation sweep (Dolby, Lattner, Chilimbi, Shuf, Seidl&Zorn, Artigas; VEE/PACT/ASPLOS post-2015) → `track-1-wave3-citations.md`.
- W2 (T5): LLVM -O2/-O3 pipeline order + rustc MIR pass order + RFC full-text search → `track-5-wave3-pipeline.md`.
- W3 (T2): Graal changelog, Leyden JEP 544 / Valhalla JEP 401 status, TruffleRuby/GraalPy, Crema/Terminus re-check → `track-2-wave3-resweep.md`.
- W4 (T3): remaining source passes (Cinder, modern Pyston, HHVM region selector, Swift compiler, TruffleRuby/GraalPy, RaptorJIT, ZJIT re-check, Maglev docs, Sorbet) → `track-3-wave3-sources.md`.
- W5 (T4): comment-depth #60879, #77893, #51553, #15397 → `track-4-wave3-comments.md`.
Next SURV- IDs: 060+. Synthesis at end: dedupe vs backlog + SURV-001…059, vet vs bar, rank, re-rank pass, update §1/§2/§5/§6, mark complete.

## Wave-3 log 2026-10-04 ~10:05 CEST
- Item 1 DONE: C-G re-verified → static-only form STRUCK to §5 (race-free HB counterexample; #19817's atomicity argument covers only the racy case); guarded write-barrier-epoch variant ranked as SURV-060 (Tier 2, WOULD-LIKE); SURV-044/012 boundary review → kept separate (const-value vs type-set, different trigger/mechanism/guard), SURV-044 text updated. Notes: `track-4-wave3-cg.md`.
- W1 (T1 citations), W2 (T5 pipeline) spawned ~09:45; W3 (T2 resweep), W5 (T4 comments) spawned ~10:05; W4 (T3 sources) HELD until W1 or W2 completes (heavy repo work; polite concurrency ≤2 heavy pipelines).
- Next SURV- IDs: 061+.

## Wave-3 log 2026-10-04 ~10:20 CEST
- W3 (T2 resweep) COMPLETE → `track-2-wave3-resweep.md` (340 lines). 0 new ranked IDs (RC-W3-1 folded as §3 angle on SURV-006); §3 +6 angles (SURV-009/006/014/022/001), §4 +2 infra (training-data inspectability; deopt-safe frame-thaw), §5 updated (Crema/Terminus verified THINKING — re-check retired; Graal/Leyden/Valhalla/TruffleRuby/GraalPy negatives). SURVEY.md + CHECKPOINT.md updated.
- W4 (T3 sources) spawned ~10:15. Still running: W1 (T1 citations), W2 (T5 pipeline), W5 (T4 comments), W4 (T3 sources).
- Next SURV- IDs: 061+.

## Wave-3 log 2026-10-04 ~10:45 CEST
- W1 (T1 citations) COMPLETE → `track-1-wave3-citations.md` (408 lines): 4 citing papers mined (PLDI'21 value fields, ISMM'15 DSA-GC, CGO'25 GoFree, ISMM'14 allocation folding); 5 dead research lines → §5.
- W2 (T5 pipeline) COMPLETE → `track-5-wave3-pipeline.md` (551 lines): verbatim O2 dump, devirt<4>, IPSCCP-before-inline, rustc MIR order, RFC triage.
- W5 (T4 comments) COMPLETE → `track-4-wave3-comments.md` (309 lines): full threads #77893/#51553/#60879/#15397; SURV-041 corrected (gc HAS adaptive global heuristic — gap is per-site only); §3 angles on SURV-045/058; §4.13 unsafe-points note.
- SYNTHESIS: SURV-061 (value-field inlining, T2), SURV-062 (alloc folding, T2), SURV-063 (speculative tcfree, T2), SURV-064 (auto-DSA GC, T3), SURV-065 (devirt-triggered re-pipelining, T2). RC-W3-2 → §4 infra. §3 +18 angles total (waves: T2 +6, W5 +3, W2 +9); §4 +8 (7→13); §5 updated (T1/T5/T4 negatives); §6 wave-4 queue written; re-rank pass → tiers stable (7/30/16/12).
- W4 (T3 sources) STILL RUNNING → carried as wave-4 queue item 1; next SURV- IDs 066+.
- Wave 3 synthesis complete pending W4 integration; CHECKPOINT will be marked complete when W4's findings land.

## Wave-3 resume after VM reboot (2026-10-04 ~09:54 CEST)
- Reboot (boot-id a7287637 → new) killed the wave-3 coordinator subagent while W4 (T3 sources) was still running; no `track-3-wave3-sources.md` was written — W4's work is lost, everything else durable.
- Resume worker (registry `graalgo-opt-survey`, coordinator-level) picked up: re-dispatched W4 as a fresh worker with the same brief (next SURV- IDs 066+, do not touch SURVEY.md directly — synthesis on completion).
- On W4 completion: dedupe vs backlog + SURV-001…065, vet vs bar, assign IDs, update SURVEY.md §1/§5/§6, mark wave 3 complete; supervisor heartbeat cron then spawns wave-4 coordinator from §6.
- Registry item stays `active` (survey runs indefinitely until owner STOP or Phase 5 begins); heartbeating hourly.

## Wave-3 log 2026-10-04 ~11:05 CEST
- W4 (T3 sources) COMPLETE → `track-3-wave3-sources.md` (209 lines): source-level passes on Cinder `b731267e` · Pyston `fe8afded` · HHVM `a8fc771d` · Swift `6b8daddf` · RaptorJIT `1d83bb4e` · CRuby `b61936bc` · V8 Maglev `7dce5a4a` · Sorbet `15481bbc` · TruffleRuby `c734f265` · GraalPy `178ea261`.
- SYNTHESIS: SURV-066 (side-exit chain cap 4 + progress proof, T2), SURV-067 (bounded-tolerance global-state guards, T2), SURV-068 (fact-annotated runtime registry, T2), SURV-069 (convergent retranslation / guard widening, T2), SURV-070 (skewed-dispatch speculation at poly sites, T2), SURV-071 (borrow-scope copy elimination, T2), SURV-072 (idiom-aware region formation, T2). §2 +2 ATTEMPTED (Cinder HIR+Static Python, Pyston LLVM+nitrous); §3 +7 angles; §4 +1 (Maglev bailout) + item-1 bloom-filter edit; §5 T3 negatives w/ re-survey triggers.
- Final re-rank: tiers stable, 72 ranked (7/37/16/12). Wave-4 queue updated (T3 source passes → re-survey triggers only).
- **Wave 3 — COMPLETE.** Next SURV- IDs: 073+.

## Wave 3 — COMPLETE (2026-10-04 ~10:15 CEST)
- W4 re-run (T3 sources, post-reboot) COMPLETE → `track-3-wave3-sources.md` (349 lines, W4b–W9b sections disambiguate from the earlier run's W1–W9).
- **Correction of the resume log above:** the "no track-3-wave3-sources.md was written" claim was wrong — the file already held W1–W9 and SURV-066…072 were already synthesized. The re-run verified the filesystem first, kept the earlier record intact, and mined only the deeper passes. Lesson logged: verify filesystem state over checkpoint claims before declaring work lost (now in ~/workspace/system/lessons/LEDGER.md).
- Synthesized: **SURV-073** (patch-in-place guard self-downgrade, Tier 4 — ranked with explicit infrastructure-dependency framing; §4 item 1 cross-ref added), **SURV-074** (speculative contention-free lock fast path, Tier 2). §2 item 19 (Sorbet Compiler ATTEMPTED). 7 new §3 angles (SURV-009/033 narrowing policy, SURV-055 JIT_PARAMDEF, SURV-070 ladder ordering, SURV-067 static write-once, C1/C4/P1 per-kind IC limits, C7 Turbolev, SURV-001 module-granular closed-world). §5 T3 negatives extended (Maglev docs, Swift @_specialize, TruffleRuby IC defaults, Sorbet current tree). §6 updated (T3 coverage, re-run re-rank pass: 74 ranked total SURV-001…074, wave-4 queue item 1).
- Next SURV- IDs: 075+. No successor spawned — supervisor heartbeat cron (`graalgo-opt-survey-heartbeat`) drives wave scheduling: next tick sees wave 3 complete → spawns wave-4 coordinator from §6 queue.
- Registry `graalgo-opt-survey` stays `active` (survey runs indefinitely until owner STOP or Phase 5 begins); heartbeating hourly.

## Wave 4 — in progress (2026-10-04 ~10:40 CEST)
Coordinator: wave-4 subagent (depth 1). Wave-3 state: 74 ranked candidates SURV-001…074 (tiers 7/37/16/12). Next SURV- IDs: 075+.
Queue (SURVEY.md §6 "Wave-4 queue"):
1. T3 re-survey triggers — re-survey Cinder/Pyston/Swift/TruffleRuby/GraalPy/Sorbet only on named triggers (Meta new optimizing JIT; Pyston commits resume; move-only SIL pass; SE-0474 ships; TruffleRuby speculation options land; GraalPy JIT-internal changelog).
2. CGO'25 pair (T1 wave 3 spot): "GraalNN: Context-Sensitive Static Profiling with GNNs" + "Improving Native-Image Startup Performance" (T2/T5 lane; profiler-design and startup angles).
3. T2 continuous re-sweep — Graal changelog, Leyden JEP 544 / Valhalla JEP 401 status watch, TruffleRuby/GraalPy (with T3).
4. T5 verification gaps — verify O2/O3 pipeline deltas with local `opt` (arg-promotion-at-O3 claim carries upstream FIXME); re-verify current rustc MIR pass list pass-by-pass.
5. T4 watches — #24314 (open), #77893 CL status re-check, #16798/#21536 re-check.
6. Rust Project Goals 2026 follow-up — MIR move elimination; prototype-gated spike design for the §3 angle on SURV-024.
7. Re-rank pass after wave 4 (promote prototype-gated items only on measured prototypes, never paper numbers).
Fan-out plan: coordinator handles item 1 (trigger check, cheap); Worker A = item 2 (CGO'25 pair); Worker B = items 3+5 (T2 re-sweep + T4 watches); Worker C = item 4 (T5 verification, local opt); Worker D = item 6 (Rust Goals follow-up). ≤2 heavy pipelines concurrent; per-track notes `track-*-wave4-*.md`; synthesis: dedupe vs backlog + SURV-001…074, vet vs bar, rank from 075+, update SURVEY.md §1–§6, re-rank, wave-5 queue, mark complete.
- Registry `graalgo-opt-survey` stays `active`; coordinator heartbeats hourly until wave 4 marked complete.

## Wave-4 log 2026-10-04 ~10:45 CEST
- Fan-out dispatched (4 workers, ≤2 heavy pipelines; web-research lanes are light):
  - Worker A: CGO'25 pair (GraalNN + Native-Image Startup) → `track-cgo25-wave4.md`
  - Worker B: T2 re-sweep + T3 trigger check + T4 watches (#24314, #77893, #16798, #21536) → `track-resweep-wave4.md`
  - Worker C: T5 verification (O2/O3 deltas + arg-promotion FIXME; rustc MIR pass re-verify) → `track-5-wave4-verify.md`
  - Worker D: Rust Project Goals 2026 — MIR move elimination + prototype-gated spike design for SURV-024 angle → `track-rustgoals-wave4.md`
- Notes-only mandate: workers write per-track notes, no SURVEY.md/CHECKPOINT.md edits, no SURV- ID assignment (IDs from 075+ at synthesis). Synthesis pending: dedupe vs backlog + SURV-001…074, vet vs bar, rank, §1–§6 updates, re-rank, wave-5 queue, mark complete. No successor spawned — supervisor cron drives waves.

## Wave-4 log 2026-10-04 ~10:41 CEST
- Worker D (Rust Goals 2026) COMPLETE → `track-rustgoals-wave4.md` (25 KB). Key findings: 2026 MIR-move-elimination goal ACCEPTED (rust-lang/goals#396, champions Amanieu/lang, RalfJung/opsem, dianqk), RFC #3943 open under active review (pass "technically unsound" pending new `move` semantics), prototype #157943 (gated `-Z mir-move-elimination`, split into 7 PRs; #163336 TailCopyToMove merged 2026-09-30; rest pending), measured try-perf aggregates (instr −0.3..−0.7%, cycles −2.2..−2.5%, bootstrap −3.2%; their numbers, not ours). Transfer: precise sub-statement liveness via escape facts + move-connected local unification (value copies, TailCopyToMove→return-place, copy()/append-growth); static soundness core does NOT transfer (no borrow checker → §5 negative); only guarded speculative variant (SURV-024 E3 arm) viable. Spike design: Stage-0 headroom measurement first (kill if <10% dead-source-unique or <1% wall-time headroom), Stage-1 guarded prototype vs gc+PGO with kill triggers (no-win >2% bar, guard-cost >50% of win, adversarial soundness suite).
- Still running: Worker C (T5 verify).

## Wave-4 — COMPLETE (2026-10-04 ~11:20 CEST)
- All 4 workers done; synthesis landed in SURVEY.md:
  - **4 new ranked candidates (IDs 075–078, no reuse/renumbering):** SURV-075 (GraalNN ML-inferred static branch profiles as E5 AOT profile source, Tier 2); SURV-076 (profile-guided first-use-order binary layout for Go-AOT, Tier 4, cold-start-only); SURV-077 (speculative declared-type-exact array-store check removal for Go slices, Tier 2 — mechanism question on the concrete Go check stated as the gate); SURV-078 (LLVM O3-only pass set as JIT adoption targets, Tier 3, prototype-gated).
  - **78 ranked total (SURV-001…078), tiers 7/40/17/14** — verified: 78 `#### SURV-` headers, no dupes, sequence continuous.
  - §2: Pyston item strengthened (repo "(No longer maintained)", 0 commits past year).
  - §3: +12 angles — SURV-024 prototype-gated spike design + transfer ruling (static core struck), AOT profile consumers (SURV-075→005/006/010/013/014), SURV-023 convergence (GR-79526), SURV-008 adjacent (GR-79594/79585), SURV-009 mechanism (GR-79031), §4.10/§4.11 rustc corrections.
  - §4: items 15 (AOT profile-source duality) + 16 (cold-start binary layout); §4.10 R3 precedent marked stale (StateTransform lost its bracket); §4.11 mechanism corrected (per-pass policies).
  - §5: wave-4 re-sweep negatives (Graal 25.5 no new EA/inlining; JEP 544 still Candidate; zero T3 triggers fired; #24314 static; #16798/#21536 watches retired; #77893 CLs unmerged); CGO'25 negatives (Basso DOI unknown, Havlak regress, heap-path over-engineering, training-run dependency; GraalNN paywall/CE/predicts-branch-probs-only); static move-elimination strike.
  - §6: wave-4 coverage, re-rank pass (no movements, no prototype-gated promotions), wave-5 queue written.
  - Re-rank rule held: no promotions on paper numbers (GraalNN's 10.13%, Basso's 1.59×, try-perf −2.2% are all source claims).
- Open for wave 5: SURV-075's Go-idiom generalization measurement design; SURV-077's mechanism question (kill or confirm); #157943 PR-landing watch → SURV-024 spike priors; JEP 544 Proposed→Target at JDK 28 ramp-down.
- Next SURV- IDs: 079+. No successor spawned — supervisor heartbeat cron (`graalgo-opt-survey-heartbeat`) drives wave scheduling.
- Registry `graalgo-opt-survey` stays `active` (survey runs indefinitely until owner STOP or Phase 5 begins); heartbeating continues.

## Wave-4 log 2026-10-04 ~10:43 CEST
- Worker A (CGO'25 pair) COMPLETE → `track-cgo25-wave4.md` (275 lines). Both papers verified: GraalNN (Milikic/Cugurovic/Jovanovic, CGO'25, DOI 10.1145/3696443.3708958) — two-stage GNN context-sensitive static profiling, shipped in GraalVM production at -O3 (JDK 25), statically generalizable, predicts branch probabilities only (call counts → separate XGBoost); WOULD-LIKE for graalgo = day-0 static branch profiles for E5 pure-AOT (feeds SURV-005/006/010/013, underlays SURV-014's day-0 cache; paper claims 10.13% speedup, +2.5% over static profiling — their numbers); open measurement: Java-trained model generalization to Go idioms. Caveats: ACM paywall (mechanism beyond abstract unverified), not-in-Community-Edition per JDK 24 docs (non-blocking while Oracle LTS pinned). Basso et al. "Improving Native-Image Startup Performance" (CGO'25, 13-page authors' PDF read, DOI unverifiable → negative) — profile-guided binary reordering: CU ordering + heap-snapshot ordering (incremental/structural-hash/heap-path; heap-path over-engineering for deterministic Go); their numbers 1.65×/1.68× page-fault reduction, 1.59×/1.58× end-to-end startup speedup; heap-ordering REGRESSED steady-state Havlak 0.97–0.98× → cold-start-only, enabling infra not peak-throughput. Sharp synthesis contrast: static-generalizable (P1) vs training-run-dependent (P2) AOT profiling.

## Wave 5 — in progress (2026-10-04 ~11:36 CEST)
Coordinator: wave-5 subagent (spawned by graalgo-opt-survey-heartbeat supervisor). Wave-4 state: 78 ranked candidates SURV-001…078 (tiers 7/40/17/14). Next SURV- IDs: 079+.
Queue (SURVEY.md §6 "Wave-5 queue (prioritized)"): T3 re-survey triggers (+ #157943 watch) · T2 continuous re-sweep (Graal changelog, JEP 544, Valhalla JEP 401) · T4 watches (#24314, #77893 CLs) · SURV-075 Go-idiom generalization measurement design · SURV-077 mechanism question (kill or confirm) · SURV-024 spike priors refresh · re-rank pass.
- Registry `graalgo-opt-survey` stays `active`; coordinator heartbeats hourly until wave 5 marked complete. Supervisor cron drives waves (no successor spawn needed).

## Wave-5 log 2026-10-04 ~11:40 CEST
- Fan-out dispatched (4 workers, web-research lanes = light; no heavy pipelines this wave):
  - Worker A: T3 re-survey triggers (standing six) + #157943 PR watch → `track-t3-wave5-triggers.md` (queue items 1, 6)
  - Worker B: T2 continuous re-sweep (Graal 25.x, JEP 544, Valhalla JEP 401) + T4 watches (#24314, #77893 CLs) → `track-t2t4-wave5-resweep.md` (queue items 2, 3)
  - Worker C: SURV-075 load-bearing measurement protocol design + XGBoost call-count profiler extractability → `track-surv075-wave5.md` (queue item 4)
  - Worker D: SURV-077 mechanism question — enumerate candidate Go checks, vet vs gc BCE / SURV-070 / SURV-074, kill or confirm → `track-surv077-wave5.md` (queue item 5)
- Notes-only mandate: workers write per-track notes only; no SURVEY.md/CHECKPOINT.md edits, no SURV- ID assignment. Synthesis at end: dedupe vs backlog + SURV-001…078, vet vs bar, rank from 079+, update SURVEY.md §1–§6, re-rank pass, wave-6 queue, mark complete.

## Wave-5 log 2026-10-04 ~11:42 CEST
- Worker B (T2 re-sweep + T4 watches) COMPLETE → `track-t2t4-wave5-resweep.md`. Headline: JEP 544 (Leyden AOT code compilation) moved Candidate → **Targeted for JDK 28** (direct fetch of openjdk.org/jeps/544; transition within last ~10 days). Wave-4's Proposed→Target trigger satisfied; remaining watch = ramp-down survival to 28 GA. All other checks negative with exact-check records: Graal 25.x changelog unchanged (still tops at 25.5, 12 entries); JEP 401 unchanged (Integrated, Release 28, Value Objects Preview); #24314 open/no movement; #77893 all five CLs still NEW (trigger not fired; SURV-041's per-site edge intact); label-sweep capacity adequate, probe surfaced watch-notes only. No raw candidates clear the bar.
- Still running: Workers A (T3 triggers), C (SURV-075), D (SURV-077).

## Wave-5 log 2026-10-04 ~11:44 CEST
- Worker A (T3 triggers + #157943 watch) COMPLETE → `track-t3-wave5-triggers.md` (230 lines). All SIX standing triggers negative with exact-check records (incl. method notes: Pyston's real default branch is `pyston_main`, still "(No longer maintained)" since Aug 2024; TruffleRuby options now in generated Options.java; GraalPy CHANGELOG pinned at 178ea261 §25.4). #157943: #163336 TailCopyToMove stays merged; #163338 MoveElimination still OPEN (gated on #163337); new try-perf numbers on #163359 prep PR (instr −0.4% all-primary, cycles 0, RSS −0.6%, binary −0.9%; their claims); goals#396 Accepted; RFC #3943 still Open (RalfJung engaged on #163340). Transfer impact: NONE — §5 static-core strike stands, guarded-speculative arm remains the only viable transfer. Wave-6 trip conditions recorded: #163338 merge, #163338 try-perf run, or RFC #3943 acceptance.
- Still running: Workers C (SURV-075), D (SURV-077). Worker B done earlier.

## Wave-5 log 2026-10-04 ~11:45 CEST
- Worker C (SURV-075 protocol) COMPLETE → `track-surv075-wave5.md` (339 lines). Q1: full load-bearing protocol constructible BEFORE any graalgo prototype — 9+1 benchmark corpus (x/benchmarks Sweet harnesses + root macro-benches + BenchmarkGames as error-light contrast), 8-shape branch taxonomy (S1 error branches highest-risk), instruments honestly scoped (Go coverage counters ground truth; arm B = gc's likelyadjust.go heuristics, verified structural in local Go 1.27.1 source; arm C gc+PGO = end-to-end ceiling only), M1/M2/M3 metrics (M2 = consumer-impact simulators needing no compiler: layout fall-through fraction, inlining-budget misallocation, wrong-leg unswitch rate), kill triggers K1–K4 (K4 anti-rigging control: failure on BenchmarkGames set = methodology suspect, not a SURV-075 kill), promotion bar ≥1.5% M2 geomean win. Q2: XGBoost/GraalCC verdict = NOT extractable (zero trace in public oracle/graal repo; docs-only; usable flag in E5 builds ≠ extractable) + granularity kill vs SURV-006 anyway (per-method binary cold/hot too coarse for per-call-site inlining simulation). Caveats recorded: GraalNN numbers vendor claims; grep.app 429-limited the code search (tree-listing basis).
- Still running: Worker D (SURV-077). Workers A, B done.

## Wave-5 log 2026-10-04 ~11:50 CEST — SYNTHESIS
- All 4 workers complete. Synthesis landed in SURVEY.md:
  - **SURV-077 STRUCK (wave 5)** — vacuous analog proven two ways (Go 1.27.1 source + disassembly: no array covariance → declared element type always exact → no store check on the store path). 11 candidate Go checks vetted; all killed (guard-is-the-check, no declared-type property, or gc-covered). C4 (iface→iface assertion getitab path) spun off as §3 angle on SURV-070 (dominant-type guard, devirt lane, prototype-gated — NO new ID: hotness unverified, estimated-small win). §1 entry = struck stub → §5 (ID kept, never reused/renumbered).
  - **SURV-075 updated**: load-bearing Go-idiom generalization protocol designed (3 stages; 9+1 corpus; 8-shape taxonomy, S1 highest-risk; M1/M2/M3; kill triggers K1–K4 armed; promotion bar ≥1.5% M2 geomean). Stage-0 execution = program-owner decision. **GraalCC/XGBoost verdict: NOT extractable** (docs-only, zero public-source trace; granularity-killed vs SURV-006 regardless).
  - **JEP 544 Candidate→Targeted for JDK 28** (direct fetch; within last ~10 days) — §3 angle; wave-4's Proposed→Target trigger satisfied; wave-6 watch = ramp-down survival to 28 GA.
  - #157943: #163336 stays merged; #163338 OPEN (gated #163337); #163359 try-perf numbers (their claims) mildly strengthen "real but young"; transfer ruling unchanged; wave-6 trip conditions recorded.
  - **No new ranked IDs** (nothing bar-cleared); no prototype-gated promotions. **77 active ranked (SURV-001…078 minus struck 077): tiers 7/39/17/14.** Verified: 78 `#### SURV-` headers, no dupes, sequence continuous.
  - §3 +7 wave-5 angles; §5 extended (SURV-077 strike, T3/T2/T4 negatives, #157943 status, GraalCC); §6 wave-6 queue written.
- SPEC.md / PHASES.md / registry NOT touched. Next SURV- IDs: 079+. No successor spawned — supervisor cron drives waves.

## Wave 5 — COMPLETE (2026-10-04 ~11:50 CEST)
- Wave 5 fully synthesized: 77 active ranked (SURV-001…078 minus struck SURV-077), tiers 7/39/17/14; SURVEY.md §1–§7 updated; §6 carries the wave-6 queue; all four wave-5 track files durable in the survey dir.
- Open threads for wave 6: SURV-075 Stage-0 execution decision (program owner); SURV-070 C4 assertion-site hotness verification; #163338/RFC-#3943/JEP-544 watch triggers; T3/T4 standing watches.
- Registry `graalgo-opt-survey` stays `active` (survey runs indefinitely until owner STOP or Phase 5 begins); heartbeating continues.

## Wave 6 — in progress (2026-10-04 ~12:37 CEST)
Coordinator: wave-6 subagent (spawned by graalgo-opt-survey-heartbeat supervisor). Wave-5 state: 77 active ranked (SURV-001…078 minus struck 077, tiers 7/39/17/14). Next SURV- IDs: 079+.
Queue (SURVEY.md §6 "Wave-6 queue (prioritized)"): T3 standing triggers (method notes) · #157943 watch trip conditions · T2 re-sweep (Graal 25.x, JEP 544 ramp-down, Valhalla JEP 401) · T4 watches (#24314, #77893 CLs, label-probe notes) · SURV-075 Stage-0 owner decision · SURV-070 C4 hotness verification · re-rank pass.
- Registry `graalgo-opt-survey` stays `active`; coordinator heartbeats hourly until wave 6 marked complete. Supervisor cron drives waves (no successor spawn needed).

## Wave-6 log 2026-10-04 ~12:45 CEST
- Fan-out dispatched (3 workers, web-research lanes = light; no heavy pipelines this wave):
  - Worker A: T3 standing triggers (method notes) → `track-t3-wave6-triggers.md` (queue item 1)
  - Worker B: #157943 watch trip conditions (#163338 merge / try-perf on #163338 / RFC #3943) → `track-surv024-wave6-watch.md` (queue item 2)
  - Worker C: T2 re-sweep (Graal 25.x, JEP 544 ramp-down, Valhalla JEP 401) + T4 watches (#24314, #77893 CLs, label-probe notes) → `track-t2t4-wave6-resweep.md` (queue items 3, 4)
- Coordinator-side items (no workers, notes by coordinator):
  - Queue item 5 (SURV-075 Stage-0 decision): memory + survey-dir + goal-files check → NO owner decision recorded anywhere; stays pending. (Recorded in synthesis.)
  - Queue item 6 (SURV-070 C4 hotness): MEASURED locally (Go 1.27.1, EPYC 9D25) → `track-surv070-wave6-c4hotness.md`. Sites hot (printArg 33%, errors.as 27% in fmt/error shapes) but itab-probe residual sub-1% (getitab 0.85% cum); monomorphic site = 4.78 ns/op (site-cache hit path); guard would save at most a couple cycles on the dominant path. Verdict: NOT bar-clearing — no new SURV- ID; C4 stays a §3 angle with hotness now measured.
- Notes-only mandate: workers write per-track notes only; no SURVEY.md/CHECKPOINT.md edits by workers, no SURV- ID assignment. Synthesis at end: dedupe vs backlog + SURV-001…078, vet vs bar, rank from 079+, update SURVEY.md §1–§6, re-rank pass, wave-7 queue, mark complete.

## Wave-6 log 2026-10-04 ~12:52 CEST
- Worker A (T3 triggers) COMPLETE → `track-t3-wave6-triggers.md`. 0/6 standing triggers fired, all with exact-check records (2026-10-04 ~12:37–12:50 CEST): (a) Meta new JIT — not fired; (b) Pyston pyston_main — still Aug 12 2024, "(No longer maintained)"; (c) Swift move-only SIL opt pass — not fired; (d) SE-0474 — still Accepted, partial impl behind flag; (e) TruffleRuby Options.java — no new speculat/guard option; (f) GraalPy CHANGELOG — latest commit Aug 18 2026, §25.4 unchanged vs 178ea261 pin. Method refinement for wave 7: check BOTH file-history and raw-vs-pin for GraalPy.
- Still running: Workers B (#157943 watch), C (T2/T4 re-sweep).

## Wave-6 log 2026-10-04 ~12:55 CEST
- Worker B (#157943 watch) COMPLETE → `track-surv024-wave6-watch.md` (112 lines). 0/3 trip conditions fired: #163338 still OPEN (S-waiting-on-author, gated on #163337, last activity 2026-09-30 force-push); no try-perf on #163338 (timeline: one comment only, author's 2026-09-25 LLM disclosure); RFC #3943 still Open (newest review comment 2026-09-30; 2026-10-02 updated_at = review-churn; no FCP). Siblings unchanged (#163336 stays merged; #163337/35/39/40 open). SURV-024 transfer ruling stands (§5 static-core strike; guarded-speculative E3 arm only). Method note: GitHub API state/timeline endpoints beat page fetches for binary checks.
- Worker C (T2/T4 re-sweep) COMPLETE → `track-t2t4-wave6-resweep.md`. All negative with exact-check records: Graal 25.x changelog byte-identical (25.5 tops, 14,501 bytes); JEP 544 direct re-verification BLOCKED (HTTP 403, tool-level, not retried — recorded as a gap; last verified wave-5 = Targeted/Release 28; fresh secondaries show no Integrated movement); JEP 401 unchanged (Integrated/Release 28); OpenJ9 v0.62.0 housekeeping only; #24314 static; #77893's five CLs all still NEW (SURV-041's gap intact); label-probe watches (#81887, #81846/#81755, #81957, #81968) zero activity. New watch note: Azul Oct-1 Cloud Native Compiler announcement (2x–5x warm-up claim, unverified, no published technique — watch note only).

## Wave-6 log 2026-10-04 ~13:00 CEST — SYNTHESIS
- All 3 workers + 2 coordinator-side items complete. Synthesis landed in SURVEY.md:
  - **No new ranked IDs** (nothing bar-cleared); no prototype-gated promotions. **77 active ranked (SURV-001…078 minus struck 077): tiers 7/39/17/14.** Verified: 78 `#### SURV-` headers, no dupes, sequence continuous.
  - §3 +7 wave-6 angles: JEP 544 wave-6 re-verification gap (403-blocked, wave 7 re-verifies); Azul Cloud Native Compiler watch note; SURV-070 C4 hotness measured (sites hot, probe residual sub-1%, monomorphic site 4.78 ns/op — NOT bar-clearing, no new ID, stays a §3 angle); SURV-024 watch (0/3 trips, ruling unchanged, GitHub API method note); SURV-075 Stage-0 (no owner decision recorded); T3/T2/T4 wave-6 negatives.
  - §5 extended with wave-6 re-sweep negatives + C4 kill-as-candidate.
  - §6: wave-6 queue marked ALL COMPLETE; per-track wave-6 coverage; wave-6 re-rank pass (no movements); wave-7 queue written (JEP 544 direct re-verification FIRST, re-armed #157943 trips, GraalPy file-history+raw-vs-pin method, Azul watch, T3/T4 standing watches, SURV-075 decision re-check, re-rank).
- SPEC.md / PHASES.md / registry NOT touched. Next SURV- IDs: 079+. No successor spawned — supervisor cron drives waves.

## Wave 6 — COMPLETE (2026-10-04 ~13:00 CEST)
- Wave 6 fully synthesized: 77 active ranked (SURV-001…078 minus struck SURV-077), tiers 7/39/17/14; SURVEY.md §1–§7 updated; §6 carries the wave-7 queue; all wave-6 track files durable in the survey dir.
- Open threads for wave 7: JEP 544 direct re-verification (403-blocked in wave 6); re-armed #157943 trip conditions; SURV-075 Stage-0 execution decision (program owner); standing T3/T4 watches.
- Registry `graalgo-opt-survey` stays `active` (survey runs indefinitely until owner STOP or Phase 5 begins); heartbeating continues.
