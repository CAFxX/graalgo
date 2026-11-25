# T8 — Phased roadmap, binary done-gates, and honest estimates (graalgo)

Track: T8 (plan + estimate). Status: feasibility-study input, not a commitment.
All numbers below are **estimates** (planning ranges), never measured values.
Calibration sources are named in §6; confidence is stated there, not implied.

## 0. Assumptions and reading guide

Most-probable architecture assumed (per task brief; Phase 0 must confirm or replace it):

1. **Truffle AST interpreter + Graal JIT** for Go code, hosted on the JVM.
2. **JVM-hosted `go` driver** that wraps/replaces gc backends (compile/link steps),
   targeting `alias go=graalgo` CLI indistinguishability.
3. **Stdlib maximally reused**: gc's pure-Go stdlib sources compiled by graalgo;
   assembly/cgo/syscall surfaces handled by explicit strategy (the #1 risk).

Conventions used in this document:

4. **[PENDING T<n>]** marks a dependency on another track's findings. Assumed track
   map (coordinator to correct): T1 architecture/feasibility, T2 runtime &
   concurrency, T3 stdlib strategy, T4 cmd/go & CLI compatibility, T5 AOT/perf &
   Native Image, T6 version-tracking pipeline, T7 Java interop & extra APIs.
5. **Kill criteria** are evidence conditions that stop a phase or the program.
   A phase with no kill criterion is a phase that cannot fail, i.e. badly designed.
6. **Done-gates** follow the world-class-implementation contract
   (`~/workspace/skills/world-class-implementation/SKILL.md`): binary, with named
   evidence. "Done" is audited, not trusted.
7. Verdicts are scoped to the tested construction (standing rule): a spike that
   passes on Loom virtual threads says nothing about a custom M:N scheduler.

## 1. Evidence-label convention (momo-style, adopted for graalgo)

Every falsifiable claim in SPEC.md and in phase gate evidence carries one label:

1. **[HIST]** — established historical fact, sourced (e.g. release dates, prior-art timelines).
2. **[VERIFIED]** — confirmed by primary source or by an experiment run inside this
   program, with the evidence artifact named.
3. **[PROPOSAL]** — proposed by a track, not yet decided; carries an owner and a
   decision deadline.
4. **[DECIDED]** — decided by Carlo; changes require a new explicit decision, not drift.
5. **[OPEN]** — must be closed before the phase that depends on it can pass its gates;
   carries an owner and a deadline. An [OPEN] item past deadline blocks the gate.

Rule: no [PROPOSAL] may be cited as justification for staffing or schedule; only
[DECIDED] and [VERIFIED] items may.

## 2. Top 5 program risks (ranked by probability x cost), with cheapest falsifying spikes

All five spikes belong in Phase 0. Each spike has a binary verdict and a kill trigger.
Order reflects expected cost = P(failure) x cost-if-true, as judged here (judgment,
not measurement).

### Risk 1 — Plan 9 assembly + cgo inside runtime/stdlib make "drop-in" unreachable
Go's runtime, `math`, `crypto/*`, `sync/atomic` ship hand-written Plan 9 assembly;
real-world packages use cgo. Truffle has no machine-code backend for Go asm.
Candidate mitigations (intrinsics per intrinsic, pure-Go fallbacks, an asm->Truffle
translator, Sulong-style LLVM) are each unproven at this scale. P~0.7 that the
residual gap permanently excludes meaningful programs (anything importing
`crypto/tls`, `net/http` at speed, or cgo). Cost if true: the drop-in claim dies.
**Spike S1 (cheapest falsifier):** inventory every `.s` file in the target Go
tree's runtime+stdlib; classify by (a) hot path vs cold, (b) pure-Go fallback
exists under build tags or not. Then implement exactly three intrinsics in the
Phase-0 prototype (one runtime, one math, one crypto) and measure perf vs gc.
Kill trigger: if >30% of Tier-0 stdlib packages (see §4, Phase 3) need asm with no
fallback and no intrinsic path demonstrated, the drop-in scope is infeasible as
specified -> stop or rescope to "pure-Go subset". [PENDING T3]

### Risk 2 — Goroutine M:N semantics do not survive mapping onto JVM threads
Go programs assume ~KB stacks grown on demand, millions of goroutines, async
preemption of tight loops, and exact channel/`select` fairness. Loom virtual
threads differ in stack management, pinning behavior (synchronized/JNI), and
scheduler fairness. P~0.5 of semantic mismatch; cost if true is a custom M:N
scheduler on the JVM, i.e. reimplementing the Go runtime's hardest part.
**Spike S2:** minimal Truffle prototype with goroutines on Loom virtual threads.
Run: (a) spawn 1M goroutines, measure bytes/goroutine vs gc; (b) channel
ping-pong throughput vs gc; (c) `select` fairness histogram vs gc over 10M
selections; (d) preemption latency of a compute-only loop (does the JVM preempt
a non-blocking Go loop at all?). Kill trigger: >10x memory/goroutine vs gc, or
measurably unfair select, or un-preemptible loops with no mitigation path ->
scheduler architecture [DECIDED] against Loom-only; re-estimate Phase 2 at the
top of its range or stop. [PENDING T2]

### Risk 3 — "Always tracks the most recent Go release" has unbounded lifetime cost
Go ships every ~6 months [HIST] with language, runtime, and stdlib changes; the
program requirement is to always track latest. Each release taxes the frontend
reuse layer, runtime deltas, stdlib deltas, and the differential corpus. P~0.6
that the steady-state tax exceeds a small team's capacity, violating requirement
(2) permanently. This is the highest *lifetime* expected cost even though it is
not the highest single-event risk.
**Spike S3:** take the two most recent Go releases; classify every changelog
entry by subsystem touched under the assumed architecture; time-box the rebase
work per class using the Phase-0 prototype team's own velocity; extrapolate to
annual FTE. Kill trigger: steady-state estimate > staffing budget Carlo approves
-> requirement (2) is renegotiated (e.g. N-1 lag, LTS-style) before Phase 1.
[PENDING T6]

### Risk 4 — `reflect` / `unsafe` / `linkname` observable-behavior long tail
`reflect` underpins `encoding/json`, `fmt`, and much of the ecosystem; its
observable behavior (method sets, alignment, `Set` rules, itab identity) is
defined by gc's runtime type descriptors, not by the spec. `unsafe.Pointer`
arithmetic is observable. P~0.5 of an unbounded long tail of subtle
incompatibilities; cost: high, spread across Phases 2-3.
**Spike S4:** implement a minimal `rtype` model in the prototype; run the
`reflect` + `encoding/json` test subsets differentially against gc; count
mismatch *classes* (not instances). Kill trigger: each fixed class reveals new
ones with no convergence over a 4-week time-box -> the compat bar (§4, Q12) must
be lowered explicitly or the program stops. [PENDING T2, T3]

### Risk 5 — `cmd/go` behavioral drop-in dies by a thousand paper cuts
Build-cache correctness, module graph pruning, `go test` caching, flag parsing,
exit codes: an enormous, subtle surface. Reusing stock `cmd/go` sources
mitigates it, but only if the driver's coupling to gc backend assumptions is
shallow. P~0.45 that residual coupling makes `alias go=graalgo` a lie in
practice; cost medium-high.
**Spike S5:** compile stock `cmd/go` with gc; stub the compile/link invocations
with recorders; run `go build`/`go test`/`go vet` over a 20-module corpus; diff
command sequences, environment, and cache keys against stock gc. Kill trigger:
driver logic inseparable from gc backend assumptions in more than a handful of
places -> Phase 4 re-estimated at top of range, or driver reimplementation
scoped out. [PENDING T4]

Honorable mentions (tracked, not top-5): JVM warmup destroying `go build` UX
(Phase 5 must answer); double rebase tax (Go releases x GraalVM releases);
licensing of the combined work (GraalVM CE is GPLv2+CE; Go is BSD-style)
[PENDING T1]; generics instantiation blowup under partial evaluation.

## 3. Phased roadmap

### Phase 0 — Spec + falsifying spikes
Goal: retire Risks 1-5 or kill/rescope the program for <1 PY.
Scope: SPEC.md v0.1 (evidence-labeled, requirements registry with immutable
GRA-#### IDs), architecture decision record, spikes S1-S5, differential-test
harness skeleton.

Done-gates (all binary):
1. SPEC.md v0.1 merged; every falsifiable claim labeled per §1; zero [OPEN]
   items without owner+deadline. Evidence: the spec file + label audit log.
2. Requirements registry created (immutable IDs, never edited in place;
   supersede/retire only via new IDs). Evidence: registry file + lint clean.
3. All five spikes executed; each has a written verdict (pass/fail) with the raw
   data archived next to it. Evidence: `research/spikes/S<n>-verdict.md` + data.
4. Architecture [DECIDED]: interpreter shape (AST vs SSA), frontend reuse
   mechanism, goroutine mapping, GC strategy, asm strategy, driver strategy.
   Evidence: ADR documents signed off.
5. Kill/rescope decision recorded explicitly (go / rescope / stop) with reasons.
   Evidence: decision log entry. A missing entry fails the gate.
6. Peer review of the spec by >=2 independent reviewers; findings remediated.
   Evidence: review reports + remediation log.
7. Docs current: README, glossary, track map. Evidence: docs diff vs Phase-0 start.

Kill criteria: any spike verdict "infeasible within budget" with no scoped
alternative -> program stops here. Cost of being wrong here is the whole program.

### Phase 1 — Minimal viable Go subset (language core, no net/os stdlib)
Goal: compile and correctly execute the Go language core on Truffle; stdlib
limited to what language tests need (no `os`, no `net`, no `syscall`).
Scope: frontend reuse pipeline ([DECIDED] in Phase 0; leading [PROPOSAL]: run
gc's `go/parser`+`go/types` under stock gc as a typed-AST/IR service feeding the
Truffle backend), Truffle nodes for the full language incl. generics, builtins,
defer/panic/recover, minimal runtime stubs.

Done-gates:
1. Spec delta for Phase-1 scope merged (subset definition is exact, not "core-ish").
2. Zero placeholders: grep-clean for TODO/stub/`panic("not implemented")` in the
   Phase-1 tree. Evidence: grep log.
3. Differential test matrix vs gc: the Go repo's `test/` directory
   (fixedbugs, syntax, types suites) run under both toolchains; mismatches
   triaged to zero unexplained. Evidence: differential report + logs.
4. Non-tautological tests: every new test has red-green evidence (fails against
   a seeded bug or a stubbed backend). Evidence: mutation/red-green log.
5. Full suite green on every commit (CI). Evidence: CI log.
6. Independent peer review (>=2 reviewers, one with compiler background);
   all findings remediated and re-tested. Evidence: reports + remediation log.
7. Self-attack pass: fuzz the frontend pipeline with malformed Go (crash/hang
   oracle), hostile build inputs. Evidence: attack log with what was tried.
8. Docs current: language-subset doc, architecture doc, deviation log
   (everywhere graalgo intentionally differs from gc, with rationale).

Kill criteria: differential mismatches against gc show no convergence after the
time-box (same test as S4, extended); or the frontend-reuse mechanism proves
unversionable (breaks on a synthetic next-release parser change) -> back to
Phase 0 for a new architecture decision.

### Phase 2 — Runtime (goroutines, channels, GC interop, memory model)
Goal: the Go runtime model on the JVM: goroutines, channels, `select`, `sync`
primitives, GC interop, panic/recover across goroutines, finalizers, the
`runtime` package surface stdlib needs.
Scope: scheduler ([DECIDED] in Phase 0: Loom vs custom M:N), channel/select
implementation, memory-model conformance, race-detector story ([OPEN] -> must
close here: reimplement, degrade, or defer), `reflect`/`unsafe` core.

Done-gates:
1. Spec: observable-behavior contracts for scheduling, channels, memory model
   (what must match gc exactly vs what may differ, e.g. goroutine IDs).
2. Go `runtime/` + `sync/` test suites pass at >= the bar set in Phase 0
   ([PROPOSAL]: >=95% of gc-passing tests; bar itself is [DECIDED] in Phase 0).
3. Memory-model litmus suite: targeted tests for happens-before, channel sync,
   `sync/atomic`, `Once`, `WaitGroup`; race detector clean under `-race`
   equivalent if [DECIDED], else documented degradation.
4. Stress: channel-heavy and goroutine-churn workloads run 1h without
   deadlock/leak; leak oracle (goroutine count returns to baseline).
5. Combination tests: runtime features x language features (panic across
   goroutine boundaries, defer in selects, generics + channels).
6. Peer review (>=2, one concurrency specialist); self-attack (adversarial
   scheduling interleavings, resource exhaustion); docs current.

Kill criteria: S2's fail conditions reproduce at scale (memory/goroutine,
fairness, preemption) with no mitigation inside the perf budget -> program stops
or restarts Phase 2 under a new scheduler architecture with fresh estimates.
[PENDING T2]

### Phase 3 — Stdlib bring-up
Goal: compile gc's stdlib sources with graalgo; Tier-0 packages fully working.
Scope: tiered bring-up. [PROPOSAL] Tier 0: fmt, strings, strconv, sort, math,
encoding/*, bytes, errors, sync, context, reflect, unicode, path, io, slices,
maps, cmp. Tier 1: os (via JVM/FFM), syscall (per-OS surface), net (via Java
NIO), crypto (pure-Go or intrinsics per S1), time. Tier 2: everything else.
Assembly strategy per package per S1 verdict. Explicitly out unless [DECIDED]:
cgo (degraded or out — Phase 0 decides).

Done-gates:
1. `go test std` pass-rate >= bar [DECIDED] in Phase 0 ([PROPOSAL]: >=95% of
   tests that pass under gc, per tier; Tier 0 at 100% of gc-passing).
2. Differential fuzzing on encoding/serialization packages (JSON, gob) vs gc.
3. Every assembly file either replaced (intrinsic), deleted (pure-Go fallback),
   or explicitly listed in the unsupported-surface doc with rationale. No silent
   fallbacks: a missing intrinsic fails loudly at build time.
4. Package-level combination tests (e.g. net/http over crypto/tls over net).
5. Peer review per tier; self-attack (malformed inputs to parsers, resource
   limits); docs current (tier matrix, unsupported surface).

Kill criteria: a Tier-0/1 package proves unimplementable without asm/cgo and no
intrinsic path exists -> that package is cut by explicit [DECIDED] scope change;
if cuts hit Tier 0, the drop-in claim is rescoped (program continues only under
the rescoped claim). [PENDING T3]

### Phase 4 — `cmd/go` drop-in driver
Goal: `alias go=graalgo` indistinguishable on the CLI surface: flags, exit
codes, output formats, build cache, modules, `go test`/`go vet` integration.
Scope: driver reuse ([PROPOSAL]: stock `cmd/go` sources compiled by gc, backend
invocations swapped per S5); build-cache semantics reproduced exactly; module
graph pruning, vendoring, workspaces; cross-compilation matrix per [DECIDED]
scope (Q14).

Done-gates:
1. Flag-parity matrix: every `go` subcommand/flag exercised; diffs vs gc
   triaged to zero unexplained. Evidence: matrix + logs.
2. Corpus test: >=50 real-world modules (mixed: stdlib-only, net/http services,
   CLI tools) build AND `go test` with identical exit codes and
   byte-identical `go test` output modulo documented nondeterminism.
3. Build-cache correctness: cache-hit/miss decisions match gc on a recorded
   workload; a cache-poisoning adversarial test (mutate source under the cache,
   verify rebuild). Evidence: cache differential log.
4. Self-hosting milestone: graalgo compiles the driver's Go sources
   (dogfooding; allowed to be slower than gc).
5. Peer review, self-attack (hostile go.mod files, pathological module graphs,
   disk-full cache), docs current (user-facing behavioral deltas doc).

Kill criteria: cache-correctness bugs that cannot be fixed without reimplementing
gc's cache internals from scratch at >Phase-4 budget -> driver strategy returns
to Phase 0. [PENDING T4]

### Phase 5 — Optimizations + AOT / Native Image
Goal: performance that does not embarrass the drop-in claim, and startup fast
enough for `go build` UX.
Scope: Graal JIT tuning on the interpreter (specialization discipline, PE
friendliness), benchmark suite vs gc with published deltas (no silent
regressions), Native Image AOT for the driver and optionally for compiled Go
programs (closed-world implications for reflect documented), startup-time budget
([PROPOSAL]: `graalgo version` < 300ms cold; `go build` overhead vs gc
documented per corpus program).

Done-gates:
1. Benchmark suite (compute, JSON, HTTP, channel-heavy) vs gc; every benchmark
   has a published delta and a budget; regressions vs the Phase-3 baseline fail
   the gate. Evidence: benchmark logs + delta table.
2. Startup budget met on the driver; warmup behavior documented honestly
   (JIT) or eliminated (AOT).
3. AOT correctness: the Phase-4 corpus passes under AOT-compiled driver;
   reflect-heavy programs either work or are listed with the exact limitation.
4. Peer review (perf specialist), self-attack (pathological programs targeting
   deopt storms, megamorphic call sites), docs current (performance guide with
   honest deltas, not marketing).

Kill criteria: warmup/startup costs make interactive `go` UX unacceptable even
with AOT and no path exists within budget -> ship JIT-only with an explicit,
[DECIDED] UX degradation, or stop. This gate is allowed to be the one that
converts "drop-in" to "drop-in except startup". [PENDING T5]

### Phase 6 — Version-tracking pipeline + release automation
Goal: "always latest Go" as a operated pipeline, not a hope.
Scope: per-release bump runbook (frontend rebase, runtime/stdlib deltas,
differential corpus, spec delta review), weekly tip-tracking CI, release
automation (build, sign, publish), double-rebase handling (GraalVM releases).

Done-gates:
1. Pipeline exists and has executed a full dry-run bump against a synthetic
   next-release delta (or the real tip-to-tip window) inside the time budget
   set by S3. Evidence: run log + timing.
2. Runbook merged; on-call/owner assigned ([DECIDED] who staffs steady-state).
3. Release automation tested end-to-end on a fake release (artifacts published
   to a staging channel, verified installable).
4: Docs current: release process, support matrix (Go version x GraalVM version
   x OS/arch), EOL policy.

Kill criteria: dry-run bump exceeds the S3-derived budget by >50% -> the
"always latest" requirement is renegotiated (lag window or reduced matrix)
before the program is declared done. Steady-state cost is reported honestly in
§6 regardless. [PENDING T6]

## 4. Phase-0 spec skeleton: questions to close (20)

1. Interpreter shape: Truffle AST over Go source, over typed IR, or over SSA? [OPEN]
2. Frontend reuse mechanism: gc `go/parser`+`go/types` as a service vs ported
   frontend vs gofrontend? [OPEN]
3. Compat target: which Go release is the Phase-1 target, and what is the
   minimum supported version thereafter? [OPEN]
4. Goroutine mapping: Loom virtual threads, custom M:N, or hybrid? [OPEN]
5. GC strategy: all Go values as JVM heap objects vs hybrid (value types,
   escape analysis)? [OPEN]
6. Assembly strategy per package class: intrinsics / pure-Go fallback /
   translator / unsupported? [OPEN]
7. cgo: supported, degraded, or out of scope? [OPEN]
8. `unsafe`/`reflect` observable-behavior contract: exact list of what must be
   bit-identical vs may differ. [OPEN]
9. Driver strategy: stock `cmd/go` sources with swapped backends vs
   reimplementation? [PROPOSAL]
10. Self-hosting: must graalgo compile its own driver (and eventually itself)? [OPEN]
11. Performance targets: quantitative vs-gc bars per phase (throughput, memory,
    build time)? [OPEN]
12. Compatibility bar: exact `go test std` pass-rate and real-world corpus
    defining "drop-in"? [OPEN]
13. Build cache: reuse gc's cache design or reimplement semantics? [OPEN]
14. Cross-compilation matrix: which GOOS/GOARCH pairs are in scope per phase? [OPEN]
15. AOT requirement: is Native Image output required for `go build` artifacts,
    or is JIT-only acceptable? [OPEN]
16. Licensing: GraalVM CE vs Oracle GraalVM; combined-work license and
    distribution terms? [OPEN]
17. `plugin` buildmode / `c-shared` / `c-archive`: in scope or out? [OPEN]
18. Race detector: reimplement, approximate on JVM, or document as unsupported? [OPEN]
19. Generics: instantiation strategy under partial evaluation (monomorphize vs
    dictionary-passing)? [OPEN]
20. Distribution & UX: JVM-bundled launcher vs Native Image driver binary for
    `alias go=graalgo`? [OPEN]

## 5. Staffing and sequencing

Strictly sequential (critical path): Phase 0 -> 1 -> 2 -> 3 -> 5. The runtime
(Phase 2) needs the language core; stdlib (Phase 3) needs the runtime; perf work
(Phase 5) needs real programs.

Parallelizable:
1. Phase 4 (driver) parallelizes with Phases 2-3 once the compile-unit
   interface freezes at end of Phase 1 (driver work is separable).
2. Phase 6 pipeline *design* parallelizes with Phases 4-5; *execution* after.
3. Within Phase 3, stdlib packages are embarrassingly parallel once the runtime
   is stable (package-per-stream staffing).
4. Differential-test infrastructure and the real-world corpus are their own
   continuous track from Phase 1 onward, never gated on feature work.
5. Frontend-reuse work (Phase 1) parallelizes with runtime *design* spikes.

Team shape (senior compiler/runtime engineers): 2 in Phase 0; 4-6 in Phases 1-2;
6-10 in Phase 3 (package streams); 4-6 in Phases 4-5; 2 + pipeline owner in
Phase 6. At full staffing the critical path is ~4-5 calendar years to a
drop-in beta; the long pole is Phases 2-3 (novel runtime work + stdlib volume).

## 6. Honest effort estimates (person-years; 1 PY = 1 senior-engineer year)

| Phase | Range (PY) | Reasoning (1-2 lines) |
|---|---|---|
| 0 spec + spikes | 0.5-1 | 2 people x 3-6 months; spikes are deliberately cheap falsifiers. |
| 1 language core | 3-6 | Low end if frontend reuse ([PROPOSAL] Q2) holds; high end if the type system must be reimplemented. |
| 2 runtime | 4-8 | The novel hard work (scheduler, channels, memory model, reflect/unsafe); no prior art to copy. |
| 3 stdlib | 6-12 | Volume-dominated (~1M+ lines of Go); reuse helps, asm/cgo/syscall per S1 decides the end. |
| 4 driver | 3-6 | Low end if S5 shows shallow coupling and `cmd/go` sources are reused; high end otherwise. |
| 5 perf + AOT | 3-6 | Open-ended by nature; capped by budget, not by a natural finish line. |
| 6 pipeline build | 1-2 | One-time automation + runbook. |
| **Total one-time** | **20-40** | Sum of ranges, rounded. |
| Steady-state | 2-4 PY/yr | Version tracking (S3) + GraalVM rebase + corpus maintenance, forever. |

Calibration sources ([HIST] unless noted):
1. TruffleRuby: started 2013 as an intern project (JRuby+Truffle), ~5-7 core
   contributors; production-viable for Shopify-scale CI only ~2023, i.e. ~10
   years and on the order of 40-60 PY for a hard dynamic language + stdlib.
2. GraalJS: core Graal/Truffle team effort from ~2014; full ECMAScript
   compliance plus Node support took ~5+ years with a larger team (~40-60 PY).
3. gccgo: one engineer (Ian Taylor) from May 2008 with a draft spec; a credible
   alternative Go compiler after ~3-4 years (~4-6 PY) — reusing the GCC backend
   and never reaching gc parity.
4. TinyGo: started 2018, small team, still a language subset after ~7 years —
   subset compilers are cheap; full parity is the expensive part.
5. The gc toolchain itself: decades of work by dozens of contributors; `cmd/go`
   alone is six figures of lines.

What pushes an estimate to the top of its range: S1 failing (asm everywhere),
S2 failing (custom scheduler), S5 failing (driver reimplementation), any
[DECIDED] scope expansion (cgo, plugin buildmode, full cross matrix).

Confidence: **LOW (±50%)**. These are planning ranges calibrated on the five
data points above, not quotes. Base rate to keep in mind: every prior full Go
toolchain reimplementation (gccgo, gollvm, TinyGo) consumed 5-30 PY and none
reached drop-in parity; this program proposes that *plus* JVM hosting *plus*
perpetual latest-release tracking.

Explicit assumption on leverage: Carlo's operation is agent-heavy. Agents
compress implementation *volume* (Phases 1/3/4) but not the irreducible parts:
semantic decisions, novel runtime debugging, adversarial review. I have NOT
discounted the ranges for agent assistance; treat any discount as a separate,
explicit [DECIDED] planning assumption, not something baked in silently.

## T8 key takeaways
1. Phase 0 is a kill-or-commit gate: five cheap spikes (asm/cgo, goroutines-on-Loom, version-tax, reflect/unsafe, cmd/go coupling) retire the top risks or stop the program for <1 PY.
2. Total one-time cost is estimated at 20-40 PY (confidence LOW, ±50%), plus 2-4 PY/year forever for "always latest Go" — the lifetime cost driver, not a footnote.
3. The critical path is 0->1->2->3->5 (~4-5 calendar years at full staffing); the driver (Phase 4) and pipeline design (Phase 6) parallelize once interfaces freeze.
4. Every phase has binary done-gates per the world-class-implementation contract and explicit kill criteria; a phase that cannot fail is badly designed.
5. The two cheapest program-level de-riskings are reusing gc's frontend (`go/parser`+`go/types` as a service) and reusing stock `cmd/go` sources — Phase 0 must decide both, because they move Phase 1 and Phase 4 estimates by 2x.
