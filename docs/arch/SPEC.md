# graalgo — Phase-0 Specification

**Date:** 2026-10-03 · **Status:** ACCEPTED 2026-10-03 (program-owner sign-off; Phase-1 entry gate passed) · **Requirements:** 70 total, all locked — `requirements/GG-0001`–`GG-0060`, `GG-SCHED-0001`–`0004`, `GG-WAKE-0001`–`0003`, `GG-FAIR-0001`–`0003`
**Study:** [FEASIBILITY.md](FEASIBILITY.md) · **Roadmap:** [PHASES.md](PHASES.md) · **Research:** `research/T1`–`T8`
**Evidence labels:** `[HIST]` established history · `[VERIFIED]` primary-source confirmed ·
`[PROPOSAL]` recommended but undecided · `[DECIDED]` settled · `[OPEN]` must be closed

## Table of contents

1. [Introduction](#1-introduction)
2. [Program requirements](#2-program-requirements)
3. [Architecture](#3-architecture)
4. [Interfaces & contracts](#4-interfaces--contracts)
5. [Testing strategy](#5-testing-strategy)
6. [Upgrade workflow](#6-upgrade-workflow)
7. [Phase-0 open-question closures](#7-phase-0-open-question-closures)
8. [Risks & falsification matrix](#8-risks--falsification-matrix)
9. [Estimates, staffing, critical path](#9-estimates-staffing-critical-path)
10. [Provenance & verdict scope](#10-provenance--verdict-scope)
- [Appendix A — Glossary](#appendix-a--glossary)
- [Appendix B — Requirement index](#appendix-b--requirement-index)
- [Appendix C — Pinned-dependency record](#appendix-c--pinned-dependency-record)

---

## 1. Introduction

### 1.1 What graalgo is

graalgo is a GraalVM/Truffle-based drop-in replacement for the whole Go toolchain:
`alias go=graalgo` is indistinguishable from real `go`, while additionally exposing
JVM-integration APIs and executing Go programs on the JVM in three modes — pure JIT,
pure AOT, and an AOT+JIT hybrid. The feasibility study ([FEASIBILITY.md](FEASIBILITY.md)) returned
**CONDITIONALLY FEASIBLE** as a multi-year R&D program; this spec is the normative
record of what will be built, replacing the study's recommendations with decisions.

### 1.2 Document status and companion deliverables

This is a Phase-0 spec: it specifies requirements and architecture. It contains no
implementation. The Phase-0 done-gates ([PHASES.md](PHASES.md) §2) are met by five deliverables
that live alongside this spec, not inside it:

1. this spec — all Phase-0 questions answered with evidence labels, `[OPEN]` = 0 on go/no-go items;
2. the 7 kill-or-commit spike verdicts, each written with a reproduction;
3. the differential-vs-gc smoke harness running in CI with mutation-validated oracles;
4. the signed architecture decision record (ADR);
5. two independent peer reviews of the spike verdicts, findings remediated.

This spec is the standard the spike verdicts are judged against: a spike that
contradicts a `[DECIDED]` item here either refines the spec (via a new requirement
ID — IDs are never edited in place) or stops the program per the kill criteria in §8.

### 1.3 Evidence labels and reading rules

Every claim in this spec carries one label. Architectural choices stated in §3–§4
are **[DECIDED]** by this spec; the companion ADR records their rationale and
signatures. `[PROPOSAL]` appears only where the spec names a *default pending a
spike verdict*, and is always paired with the `[DECIDED]` decision procedure that
will settle it. There are **zero `[OPEN]` items on go/no-go questions** — every
§10 question of the study closes in §7. Go/no-go items are: the program
requirements (§2), the §7 closures, and the §8 risks. Anything labeled
`[ESTIMATE]` is a quantified judgment, never a measurement; `[SPECULATIVE]` and
`[UNVERIFIED]` mark claims that must survive the falsifying experiments in §8
before they are promoted.

### 1.4 Terminology

Normative definitions live in Appendix A. The load-bearing terms: **backend** (the
Truffle-based compile+run pipeline replacing gc), **intrinsic surface** (the
~60–70-function runtime contract the adapted stdlib builds against, GG-0020),
**toolexec seam** (the stock driver's `-toolexec` interception point, GG-0011),
**AOT+JIT hybrid** (application + interpreter + the Graal JIT itself AOT-compiled
into one binary; the embedded JIT re-optimizes hot guest code at runtime —
Truffle-on-SubstrateVM runtime compilation, not HotSpot-style tiering),
**oracle** (the differential-vs-gc harness, GG-0030), **exclusion list** (versioned,
reason-coded backend-specific test exclusions, GG-0033), **parity stance** (a
once-decided position on a runtime behavior with no JVM analog, GG-0045),
**toil bound** (P95 ≤ 200 agent-hours and ≤ 10 wall-clock days per release, GG-0004).

---

## 2. Program requirements

The seven program requirements below are **[DECIDED]** (refined 2026-10-03; the
seventh — testing as a first-class deliverable — was added the same day). Each maps
to GG IDs whose registry text is authoritative; the prose here states intent and
bounds, never restates the SHALL.

### 2.1 Drop-in: `alias go=graalgo` (GG-0001, GG-0002)

Drop-in is decided at two boundaries. At the CLI boundary (GG-0001): all 19
commands registered in `src/cmd/go/main.go:52-70` `[VERIFIED]`, the shared
build-flag set, per-command flags (including `go test`'s two-phase flag parser),
`go env` variables, help output, and exit codes 0/1/2. At the behavior boundary
(GG-0002): module resolution (GOPROXY/sumdb), build-cache behavior, `go test`
JSON protocol with two-level result caching, and `go vet` invocation. The 929
txtar scripts in `src/cmd/go/testdata/script/` `[VERIFIED]` are the de-facto
conformance suite for both boundaries.

What drop-in does **not** require `[DECIDED]`: byte-identical binaries, or
byte-identical cross-backend build-cache sharing. Action IDs hash tool identity
via `tool -V=full` answered *through* the wrapper, so differing backends
auto-namespace in a shared `GOCACHE` `[VERIFIED]`; behavioral cache compatibility
(hit/miss patterns) is the bar (GG-0028).

### 2.2 Extra JVM APIs (GG-0003)

Alongside the indistinguishable core, graalgo exposes JVM-integration APIs —
the `graalgo/java` package and polyglot bindings — that have no stock equivalent
`[DECIDED]`. They are purely additive: no stock behavior changes when they are
present. The interop contract is specified in §4.3.

### 2.3 Version tracking: bounded, agent-driven, tooled (GG-0004)

"Always tracks the most recent Go release" is refined `[DECIDED]`: a fully
mechanical upgrade is not expected to be possible. The requirement is **bounded,
tooled, agent-driven upgrades** — P95 ≤ 200 agent-hours and ≤ 10 wall-clock days
per release, calibrated on the 1.26/1.27 deltas `[PROPOSAL]` promoted to
`[DECIDED]` as the bound, with breach triggering a toil-reduction sprint before
the next release. Go ships every ~182 days like clockwork `[VERIFIED]`; the
workflow starts at rc1, not release day. The workflow itself is §6; the bound is
enforced by measurement (GG-0042), not asserted.

### 2.4 Go↔Java interop, both directions (GG-0005)

Full interop with shared data types `[DECIDED]`, built on `org.graalvm.polyglot`
with `SCOPED` host access `[VERIFIED]` (the documented safe default for
untrusted guests). The sharpest edge — view vs copy at the boundary — is decided
per type in §4.3 with copy as the default. A Java exception crossing into Go is a
Go `error`, never a Go panic `[DECIDED]`.

### 2.5 Three execution modes (GG-0006, GG-0060)

Three modes, one Truffle AST, packaging-only differences `[DECIDED]`:

- **(a) pure JIT** — Truffle on HotSpot. The dev inner loop: fastest builds, best
  debugger, full JVM tooling. Slowest startup, full warmup tax.
- **(b) pure AOT** — Native Image, interpreter-only fallback runtime, no runtime
  compilation. Fastest startup, smallest binary, works on W^X platforms. Lowest
  peak performance.
- **(c) AOT+JIT hybrid** — the default (pending the §7.8 meta-gate): the
  application, the Truffle interpreter, and the Graal JIT itself are AOT-compiled
  into one native binary; at runtime the embedded JIT re-optimizes hot guest code
  with speculation and deoptimization. This is the verified-shipping
  Truffle-on-SubstrateVM mechanism `[VERIFIED]` (closed-world analysis computes
  the runtime-compilable method set; graph encodings live in the image heap),
  not HotSpot-style tiering.

Mode selection is via build flags/environment with documented defaults (GG-0060):
default = hybrid once the meta-gate passes, JIT-only before that. Mode selection
never changes program semantics — the modes are packaging. The implementation
differences (image builds, embedded compiler, reachability pipeline) are §3.9.

### 2.6 Beyond-gc optimizations (GG-0007)

The pitch is real but narrow `[PROPOSAL]` (adversarial result, T6): plausible wins
are (1) loop auto-vectorization (gc has none), (2) always-on runtime profiling vs
gc's opt-in PGO, (3) partial escape analysis past gc's flow-insensitive EA. gc is a
strong baseline (mid-stack inlining since 1.12, PGO inlining + devirtualization,
good EA, excellent BCE) `[HIST]`. **Every claimed win must beat gc *with* PGO on
its named falsifying benchmark, or the claim is struck from the docs** `[DECIDED]`.

#### 2.6.1 Primary optimization thesis: the devirtualization cascade

Carlo's thesis, adopted `[DECIDED]` as the program's primary optimization bet
(mechanism `[VERIFIED]` against Graal/Truffle semantics; magnitude
`[SPECULATIVE]`): gc cannot inline through an interface call at all, so every hot
`io.Reader.Read` / `http.Handler` / `error` path pays itab indirection *plus* the
loss of all downstream optimization — the single largest structural gap in gc's
optimizer. The Truffle JIT closes it as a cascade: (i) PICs profile the itab at
each call site; (ii) hot monomorphic/bimorphic sites are speculatively
devirtualized to direct calls with deopt guards; (iii) the concrete method body
inlines; (iv) with concrete types visible, Graal's escape analysis
scalar-replaces and stack-allocates values that previously "escaped" into the
interface — heap-to-stack promotion across abstraction boundaries. Go is unusually
well positioned: structural interfaces give a fixed, compile-time-known method set
per (type, iface) pair, and value semantics mean less aliasing for EA once
inlined. Honest bounds: megamorphic sites do not devirtualize (Phase 0 must
measure real call-site polymorphism distributions); the win is steady-state and
measured against gc *with* PGO on interface-heavy workloads; pure-AOT gets only
the PGO-baked subset — which is also the technical argument for why the JIT and
hybrid modes matter. **Falsifying benchmark:** interface-heavy suite, gc+PGO vs
graalgo-JIT steady state — if the cascade does not move it, the thesis is wrong
and the "beyond gc" pitch shrinks to vectorization.

### 2.7 Modern JVM integration (GG-0008)

Virtual threads (the goroutine mapping, §3.5), async I/O (blocking NIO on virtual
threads; io_uring has no JDK JEP as of Oct 2026 `[VERIFIED]` — Loom roadmap only),
GraalVM isolates (§3.11), and JVM-grade observability: custom JFR events
(goroutine/channel/GC, `graalgo.ModuleCrash` for momo), heap dumps, and VisualVM
in JIT mode `[DECIDED]`. Known gap, stated plainly: no JMX/VisualVM attach in
AOT-containing modes — there the pprof endpoint and JFR files are the primary
interfaces (GG-0024).

### 2.8 Stdlib reuse (GG-0009)

Maximal reuse `[DECIDED]`: 229 of 398 package dirs (58%) pass a strict pure-Go
check and compile unchanged given the frontend + intrinsic surface `[VERIFIED]`
by source measurement. The load-bearing remainder builds against the intrinsic
surface (GG-0020) plus an adapted stdlib fork for the coupled packages.
The coupled set was re-measured on go1.27.1 at P3-M6 (was "~78" from an
imprecise earlier check): **142 of 399 dirs (36%)**, versioned with per-dir
reasons in [coupled-packages-go1.27.1.json](tools/stdlib-fork/coupled-packages-go1.27.1.json)
`[VERIFIED]`; the P3-D1 re-test trigger fired and the re-examination
([ADR-0001-addendum-p3d1-reexamination.md](docs/dev/adr/ADR-0001-addendum-p3d1-reexamination.md)) concluded
P3-D1 STANDS — the real-adaptation subset (asm/cgo, ~44 packages) is smaller
than the original loose ~78, so the fork-adaptation budget is not exceeded. Plan 9
asm (485 files / ~127k lines `[VERIFIED]`) is handled per category: runtime-core
asm *evaporates* (no Go stacks on Truffle), atomics/bytealg are *intrinsified*,
crypto is *emulate-then-intrinsify*, `math` is *covered by JVM IEEE754*
`[DECIDED]`. Linkname compatibility is by symbol name, not gc ABI (GG-0021).

### 2.9 Testing as a first-class deliverable (GG-0010)

The four-layer testing strategy (§5) is built **early** — the Phase-0 harness seed
doubles as the development conformance net — because it is the mechanism that
makes "bounded per-release toil" credible `[DECIDED]`. Oracles are
mutation-validated from day one (GG-0034).

---

## 3. Architecture

Architectural choices in this section are **[DECIDED]** by this spec; the companion
ADR records rationale and signatures. Each subsection names its GG IDs and the
spike that falsifies it.

### 3.1 Driver: wrap, don't reimplement, don't fork (GG-0011)

Ship the **stock Go distribution**; swap only the compile backend behind the
existing `-toolexec` seam (`src/cmd/go/internal/work/build.go:354` wraps every
`compile`/`asm`/`cgo`/`link`/`cover`/`pack` invocation; `toolID()` queries
`tool -V=full` *through* the wrapper) `[VERIFIED]` on go1.27.1. The driver is then
identical by construction; per-release driver cost becomes a conformance gate (the
929 txtar scripts), not a port. A Java reimplementation is rejected (~74k LOC,
lowest fidelity-per-effort `[VERIFIED]` by line count). Forking `cmd/go` is the
**escape hatch**: permitted only if argv interception proves insufficient, and
priced honestly — the fork roughly doubles driver toil every release, forever
`[ESTIMATE]` (T2 §4.4: 20–40 vs 8–16 agent-hours). **Falsifier:** spike 5 (txtar
matrix through the wrapper); kill trigger = argv-interception gaps that force the
fork *and* the fork's merge burden breaks the toil bound.

### 3.2 Frontend service: go/parser + go/types, verbatim (GG-0012, GG-0013)

`go/parser` produces go/ast; `go/types` produces full type info; a node factory
walks the typed AST and builds Truffle nodes `[DECIDED]`. The `go/*` tree is
reused **verbatim** from the tracked release and re-vendored **atomically**
(frontend + stdlib as one unit) every release — pinning the frontend to N while
tracking stdlib N+1 does not work in general (go1.27's stdlib itself uses a 1.27
language feature, generic methods on `math/rand/v2.Rand` `[VERIFIED]`). Old code
is supported via per-file/per-module `-lang` gates exactly like the stock
toolchain, never by pinning (GG-0013). The Go 1 compatibility promise covers
`go/*` as additive-only API `[VERIFIED]`; per-release frontend deltas are new
exported API, new accepted syntax, and type-checker behavior behind `-lang`
gates. **Falsifier:** spike 3's dry-run upgrade measures frontend rebase toil
against the bound.

### 3.3 One Truffle AST (GG-0014)

Two node families per Truffle convention: expression nodes with typed execute
methods (`executeLong`/`executeInt`/`executeDouble`/`executeBoolean`/generic
`execute→Object`) keeping primitives unboxed, and void statement nodes; loops use
`@ExplodeLoop`-friendly structures or `LoopNode`/`RepeatingNode` for OSR
`[DECIDED]`. DSL specialization budget is spent **only at genuinely dynamic
sites**: interface dispatch (PIC on itab identity, limit ~3, megamorphic fallback
to hash lookup — the single most valuable specialization), type switches
(decision trees + `ValueProfile`), maps (inline fast path guarded on table
version, `@TruffleBoundary` slow path). Everything else uses typed execute
methods with no PICs — Go's static typing vacates Truffle's type-discovery
miracle, and the design must not pretend otherwise `[DECIDED]` (adversarial
result, T5 §5). Itabs are immutable, so speculation on them never invalidates —
no Assumption lifecycle management needed. **Falsifier, now closed:** spike 6's scored
comparison landed COMMIT-DIRECT (6 vs 19) 2026-10-03 — Sulong's IR version gap
(LLVM 22 emitted vs ≤12.0.1 readable) is dispositive; the direct-AST estimate
did not double, so the replan trigger never fired. The fork re-opens only on
spike 6's re-test triggers (current-LLVM Sulong support, a maintained
full-language Go→LLVM frontend, or precise-GC/threading/signal primitives in
Sulong). `[DECIDED]`.

### 3.4 Value model: Java objects throughout (GG-0015, GG-0016)

Go values live as Java objects `[DECIDED]`: descriptor-based structs (per-type
descriptor + flat `long[]` for scalars + `Object[]` for references — one-class-
per-struct is rejected because runtime class definition is unavailable in
closed-world images); slices as `{typedArray, offset, len, cap}` with typed Java
arrays; maps as a hand-written open-addressing table (Go specifies randomized
iteration order, so the runtime must own the iterator); strings as immutable
`GoString{byte[]}` (`java.lang.String` is UTF-16 and wrong for arbitrary Go
bytes); interfaces as `(dynamicType descriptor, data)` + itab; closures as
`{RootCallTarget, MaterializedFrame|null}`. "Go pointer" = opaque Java reference:
the GC moves objects freely; write barriers and exact roots come free from the
JVM; pinning happens only at the cgo/FFI boundary, scoped to the call
`[DECIDED]`. Off-heap as primary storage is rejected (scoped verdict, T5 §2.3):
for a managed Go implementation it reimplements the GC's job worse.

`unsafe.Pointer` v1 = handle-table emulation (object identity + field offset);
arithmetic escaping the object traps — a documented semantic restriction, never
silent (GG-0016) `[DECIDED]`. **Falsifier:** spike 4 (reflect/unsafe): if
`reflect`'s 243 unsafe uses require true raw-address arithmetic beyond what the
handle table + a raw-address FFM-arena mode can cover, the v1 semantics are
re-scoped.

### 3.5 Concurrency runtime (GG-0017, GG-0018, GG-0019; GG-SCHED-0001–0004, GG-WAKE-0001–0003, GG-FAIR-0001–0003)

**Goroutine = virtual thread + scheduler token** `[DECIDED]`. `go f(args)` ≡
`Thread.ofVirtual().start(...)` (JEP 444 `[VERIFIED]`); goroutine-local state
(defer chain, panic context) lives in virtual-thread-scoped holders. Loom never
preempts, and its `ForkJoinPool` is demoted to an **untrusted thread pool** — no
scheduling discipline is assumed from it. Spike 2 (1:1 vthreads *yielding* back
to the FJP) was **KILLED**: the preemption torture gave 0 latency samples vs
200,000 for stock Go — systematic starvation the yields could not close
`[VERIFIED]` (scoped: JDK 25.0.4, 2-vCPU VM, OFF / POLL-10ms / POLL-1ms-watchdog /
UNCOND / PARK 1µs–1ms configs; the kill indicts
the yield-to-FJP design, not 1:1 vthreads as the execution vehicle).
**Root causes (final, per [spikes/02-loom/FAIRNESS_SEARCH.md](spikes/02-loom/FAIRNESS_SEARCH.md)):** (1) Loom's
default carrier has no woken-thread priority / no runnext equivalent — woken
continuations are buried under spinner resubmits (the earlier "LIFO local
queues" mechanism story was corrected: the default scheduler is
FIFO/`asyncMode=true` with carrier affinity, verified in JDK 25 sources; the
starvation is empirically systematic across all modes and runs regardless);
(2) no public JDK API installs a custom carrier scheduler
(`Thread.ofVirtual().scheduler` is package-private; the
`-Djdk.virtualThreadScheduler.implClass` hook exists only in loom-branch
builds); (3) carrier sizing is JVM-global via system properties only —
`GOMAXPROCS` fidelity is a launcher concern; (4) seven workaround families
failed empirically. **Re-test triggers** (each "superseded by option C as a
program decision" — they apply to the killed design, not to option C):
(a) JDK exposes a public per-builder virtual-thread scheduler API;
(b) JDK changes carrier queue discipline for yielded continuations
(FIFO / runnext-equivalent); (c) a JDK-change-free workaround is
demonstrated (the platform-thread unblocker was tried and failed).

The mechanism difference that matters: scheduling discipline moved to user
space. On quantum expiry a vthread parks **indefinitely** via
`LockSupport.park()` and offers its scheduler token to the token queue. The
parked vthread unmounts and its carrier returns to the pool; it is held by the
*scheduler's* queue. Each resume submits at most one continuation to the FJP,
with at most `cap` outstanding — so the FJP is never asked to choose fairly
among queued continuations, only to eventually run each submitted one
`[DECIDED]`. The safety comes from the bound, which production must preserve:
effective cap = `min(GOMAXPROCS, carrier parallelism)` `[DECIDED]` (see the
pinning assumption below). graalgo requires the default Loom carrier
scheduler; custom `jdk.virtualThreadScheduler` settings are unsupported
(documented restriction) `[DECIDED]`.

**Quantum-based preemption.** `Safepoint.poll()` at loop back-edges and function
entries in the Truffle AST (mirroring gc's own preemption checks); on quantum
expiry the vthread parks and offers its token — back-edge *yields* are gone,
parking is the preemption primitive `[DECIDED]`. Default quantum **10ms**
`[DECIDED]`: the spike-2b F4 scoped fix, now measured. The rework battery measured
33.0µs mean / 10.0µs p50 per park/resume hop at 1ms quanta on the contended
2-vCPU VM (3.3% of the quantum, over the 1% budget)
`[VERIFIED]` (archived F4 log `overhead.log`: mean=33019ns p50=9985ns; the old
draft's 26.8µs is superseded — the report restates run-to-run as a
26.8→41→33–70µs wander, contention-driven); at 10ms the rework battery measured 29.9µs
mean = **0.30%** — comfortably under budget `[VERIFIED]` (archived F4 log
`overhead_q10ms.log`: mean=29884ns; prototype scope;
server-hardware re-measurement is a Phase-2 done-gate). Wakeup latency is
quantum-independent (wakeFifo priority lane), so 10ms quanta do not slow
channel/timer wakeups. Tradeoff stated explicitly: coarser compute-bound
preemption granularity at 10ms; 10ms matches Go's sysmon granularity `[HIST]`;
fine-grained handoff remains available via `runtime.Gosched()` and explicit
safepoint polls.

**Token queues and the drainer.** Three admission lanes: priority `initFifo`
(new goroutines), priority `wakeFifo` (woken/channel-ready — the runnext
analog), FIFO `runFifo` (quanta tokens) `[DECIDED]`. `initFifo` exists because
spike 2b §3.2 caught INIT starvation: unbounded FIFO growth let resume tokens
flood past INIT tokens (2,643 of 10,000 new goroutines never scheduled before
the fix, archived log; `never_scheduled=0`, per-spinner quanta fairness
max/mean 1.05 after) `[VERIFIED]` (prototype scope; the rework triaged an
800ms drain-window artifact in the original probe — the starvation signal is
now measured drainer-side via a latch, spike-2b rework §3.4) The drainer is a single platform thread:
blocking take → admit → `LockSupport.unpark(vthread)` `[DECIDED]`. It is
non-terminating: `InterruptedException` is swallowed and the loop continues
(prototype), and production SHALL NOT expose the drainer thread to interruptors
(GG-SCHED-0002) `[DECIDED]`. It is self-pacing — tokens arrive only as vthreads park, so in-flight unparks stay ≈
carrier count and the FJP never floods; observed drainer utilization 1.91% at
10,000 goroutines `[VERIFIED]` (archived `logs/quiescence_10k.log`: `drainerUtilPct=1.91`), so the drainer is not the
bottleneck.

**In-flight cap = GOMAXPROCS**, enforced as a hard assert in the drainer
`[DECIDED]`. Spike 2b's `maxTrueParallel` never exceeded the cap (caps 1 and 2,
including mixed I/O+compute) `[VERIFIED]` (prototype scope); production
preserves the assert — it is the mechanism, not a debug aid. The default cap is
available processors, matching Go's default `GOMAXPROCS=NumCPU` `[VERIFIED]`
on the default mapping; the effective cap is `min(GOMAXPROCS, carrier
parallelism)`, clamped in the constructor `[VERIFIED]` (prototype scope,
spike-2b rework §2/F5).

**Park/unpark safety.** Per-vthread `armed` ticket + generation counter;
`acked` set **before** unpark (reversing the order loses wakeups) `[DECIDED]`.
Production invariant: vthreads in scheduler parks are never interrupted (so
`awaitAck`'s `LockSupport.park()` loop cannot degrade into a spin); interrupts
are delivered at Go level, never via `Thread.interrupt()` on a
scheduler-parked vthread `[DECIDED]`. The lost-wakeup invariant — admissions
== parks at quiescence — held in stress:
48,140 parks / 48,140 admissions, 40,588 of 40,588 wakeups won, zero lost
`[VERIFIED]` (archived `logs/stress.log`: `parks=48140 admissions=48140 wakeupCalls=40588 wakeupWon=40588`). Residual, stated: tested interleavings only, no
formal verification — a model-checker re-test is a Phase-2 done-gate `[DECIDED]`.

**Scheduler-aware blocking (the F5 limitation).** Java blocking that bypasses
the scheduler (e.g. `CountDownLatch.await()`) still counts against the in-flight
cap → transient under-utilization; with cap=1 it deadlocked the O5 test barrier
in the prototype `[VERIFIED]`. **All blocking in the runtime/stdlib adaptation
layer SHALL go through scheduler-aware primitives** (channel park, timer park,
sleep, select park, scheduler-token sleep) `[DECIDED]`. Blocking I/O uses the
**slot-releasing NIO wrapper**: `ioEnter` releases the in-flight slot (the
drainer decrements `inflight` into a dedicated `ioBlocked` lane — no resume
token is enqueued), `ioExit` re-acquires via `wakeFifo` on completion
`[DECIDED]`; both WAKEUP/IOBLOCK orderings are tolerated via `wakePending`.
The pre-rework "scheduler-transparent NIO, no starvation risk" carve-out is
**deleted**: N NIO-blocked goroutines at cap=N indefinitely postponed runnable
goroutines (spike-2b rework §3.5), contradicting the carve-out. The
`CapExhaustion` falsifier guards the wrapper — red-green validated (raw
blocking fails, wrapper passes) `[VERIFIED]` (prototype scope). Unpaired
`ioEnter` trips a fail-fast drainer assert `[DECIDED]`. A Phase-2 measurement
trigger covers the wrapper's throughput cost `[DECIDED]`.

**cgo / pinning under option C.** A vthread in a native/FFM call pins its
carrier (JEP 491 `[VERIFIED]`); this transiently reduces pool capacity. The
no-discipline argument requires **effective carriers ≥ in-flight at all
times**: every pinning source must either run off-pool (cgo, per GG-0057) or
be counted in the +k carrier budget `[DECIDED]`. If a carrier is pinned while
in-flight is at cap, outstanding FJP submissions can exceed available carriers
and the FJP's queue discipline re-engages (mildly) — hypothesized safe;
unmeasured until the Phase-2 pinning matrix. Measured pool-capacity behavior is `[UNVERIFIED]` until the Phase-2 pinning
matrix. **cgo runs off the carrier pool** (separate platform-thread pool or
budgeted +k carriers), matching Go's existing cgo economics, budgeted per
GG-0057 `[DECIDED]`. FFM calls from the stdlib adaptation layer and blocking
file I/O (which pins *without* unmounting) are **not yet covered** by GG-0057's
cgo-only scope — GG-0057 is extended to them as of 2026-10-03 `[DECIDED]`.
`synchronized` no longer pins on JDK 24+ (JEP 491 `[VERIFIED]`) — channel/mutex
internals may use it. The `jdk.VirtualThreadPinned` audit stays a Phase-2
done-gate `[DECIDED]`.

**The netpoller is deleted** (GG-0018) `[DECIDED]`: blocking `java.nio` socket
operations park the virtual thread inside the slot-releasing NIO wrapper (slot
released while blocked, re-acquired via `wakeFifo` on readiness); the JDK's
internal poller handles readiness. This is the entire Loom value proposition
and the lowest-risk path.

**Channels** = Java objects (lock, circular buffer over a typed array, two
wait-queues of scheduler-parked virtual threads); **select** ports
`runtime/select.go` logic (lock channels in address order, poll, park one
Parker on all, spurious-wakeup loop, `fastrand` among ready cases). **Defer** =
explicit per-goroutine chain (open-coded defers are a later phase, not v1).
**Panic** = `AbstractTruffleException` carrying the value with lazy traces
(recovered panics stay cheap; the `ControlFlowException` alternative is rejected
because fatal panics need printed traces anyway — scoped verdict, T5 §4.4).
**Goexit** = `ControlFlowException`. **panic(nil)** → `*PanicNilError` (Go
≥1.21 semantics, replicated exactly).

**Residual risks, carried explicitly.** (a) *Overhead:* mean 33.0µs/hop
(rework battery @1ms quanta, contended 2-vCPU VM) over the 1% budget at 1ms quanta
(p50 10µs, at/under); scoped fix **measured**: 29.9µs mean = 0.30% at 10ms
quanta (spike-2b rework battery). The coarser-preemption tradeoff kept.
Re-test trigger: server hardware. (b) *Cold-drainer wakeup:* 38µs p50 when
the drainer was parked (vs 4.3µs minimal repro, 5µs hot p50) — unexplained on
the 2-vCPU VM; suspected host/VM thread-wakeup latency (hypothesis —
alternative: systematic deep-sleep wakeup cost of the parked drainer).
Correctness unaffected; even a systematic 38µs is 0.38% at 10ms (under
budget) — the risk is only if it scales *worse* on server hardware.
Re-test trigger: server hardware (risk-row-10 kill trigger kept). (c)
*Absolute rate:* 11,824 samples/5s vs stock Go's 200,000 (~17×) `[VERIFIED]`
(archived O1 leg `o1_q1ms.log`; legs range 7,014–13,919); RTT p50 ~55–64µs
vs <1µs `[VERIFIED]` (archived O1 legs; one noisier leg at 276µs, contention-driven)
— prototype per-hop cost plus a structural per-quantum cross-thread
hop (vthread unmount/remount + drainer handoff + FJP resubmission; Go pays an
in-P context switch) that production minimizes but cannot eliminate. The
budget argument holds because quanta are long (10ms), not because the hop goes
away; **no production performance claim** until the production-path
re-measurement trigger fires (Phase 2).

**Falsifier:** the Phase-2 preemption-torture suite + pinning matrix re-run
against this design; kill trigger = systematic divergence the user-space
scheduler cannot close, or a residual risk hardening into a mechanism blocker.
This supersedes spike 2's yield-based falsifier.

*Requirements track:* the scheduler SHALLs above are encoded as GG-SCHED-0001–0004,
GG-WAKE-0001–0003, GG-FAIR-0001–0003 (all locked 2026-10-03); GG-0017 and GG-0048 are revised to
remove FJP-scheduler assumptions (spike 2b §6.2). §3.5 states the decided architecture
those IDs encode.

### 3.6 Intrinsic surface & linkname contract (GG-0020, GG-0021)

The runtime exposes a **versioned intrinsic surface of ~60–70 functions**
`[VERIFIED]` by source measurement of what the coupled stdlib needs: memmove/
memequal, makeslice/growslice, map/chan/select ops, interface/itab machinery,
panic/defer, scheduler glue, netpoller API contract (satisfied by blocking NIO
per §3.5 — the contract `net` calls into, not the deleted event loop), atomics, timers, fastrand, write
barrier, traceback — plus **exact `rtype`/itab layouts** for `reflect`'s 243
`unsafe` uses `[DECIDED]`. Linkname compatibility between graalgo-compiled
runtime and stdlib is **by symbol name, not gc ABI** (52 stdlib→runtime symbols +
~100 reverse edges `[VERIFIED]`): graalgo compiles both sides, so only names must
match. The tax is per-release churn of internal symbols, paid via a **linkname-
diff step** in every upgrade's Phase 2 (GG-0021). **Falsifier:** spike 4; kill
trigger = per-release linkname churn exceeding the diff tooling's scoping
(skip-list growth > 15 twice).

### 3.7 cgo → Panama FFI (GG-0022, GG-0058)

`import "C"` maps to Panama FFI (JEP 454, final in JDK 22 `[VERIFIED]`)
`[DECIDED]`: the **stock `cgo` tool and host gcc are kept** for the C side under
all options — C compilation is not reimplemented. The build-time binding step
(jextract-style, from C headers) produces `SymbolLookup` + `Linker.downcallHandle`
handles; C structs become `MemoryLayout` descriptors; Go funcs passed to C become
`Linker.upcallStub` handles tied to an `Arena` (arena scoping replaces cgo's
pointer-passing rules and is arguably easier to get right). **graalgo authors the
C ABI struct-layout engine** (field offsets, alignment, `_Bool`, bitfields,
unions) — the highest-risk part of the mapping, with no JVM help, and the
program's riskiest integration item `[DECIDED]`. Platform list: linux/amd64
first, linux/arm64 second; darwin and windows deferred (GG-0023). A virtual
thread executing a native/FFM call pins its carrier (JEP 491) — expected,
matching Go's "cgo calls consume an M" economics, budgeted per GG-0057.
**Momo's MM-0017 verifier ban gates the FFI path**: `import "C"` (and now the
Panama mapping) is banned in module code by verifier rule (GG-0058) `[DECIDED]` —
C code defeats generation-relative addressing and generation tagging either way.
**Falsifier:** spike 1 (asm/cgo) + the Phase-4 cgo corpus; kill trigger =
systematic ABI divergence from the layout engine.

### 3.8 OS scope: Linux-first (GG-0023)

Linux-first is well-bounded (72 syscall files, epoll `[VERIFIED]`) `[DECIDED]`;
darwin is a moderate deferral; **Windows is out of v1 scope** — 2–3× cost (IOCP +
6.7k-line syscall layer `[VERIFIED]`) — with a **re-test trigger** recorded in the
verdict-scope ledger (§10.2): re-measure after Linux is green. `runtime/cgo` +
`plugin` are stubbed as unsupported initially (plugin's full scope is §7.7).

### 3.9 Three-mode implementation & packaging (GG-0050)

The FEASIBILITY §5 matrix, as architecture `[DECIDED]`: startup (b) fastest ≈ (c)
fast < (a); peak (a) ≈ (c) > (b); build cost (a) < (b) < (c); debuggability (a) >
(c) > (b); implementation effort (a) baseline, (c) = (a) + image-build
configuration, (b) = (a) minus the optimizing runtime (a build flag). One
frontend, one value model, one runtime library serve all three — **the modes
differ only in packaging**; this is stated twice because it is the load-bearing
constraint. The hybrid's embedded compiler is **Go-version-independent** (a
platform component moving on the GraalVM bump cadence, not the Go release
cadence) `[DECIDED]` — the dominant per-release cost is the closed-world
reachability-metadata pipeline, identical in pure AOT and hybrid (GG-0052, §7.6).
**The AOT baseline must be correct and performant without the runtime JIT**
(GG-0050) `[DECIDED]`: every isolate/generation starts cold and loses its
profiles on restart, so the JIT is a steady-state bonus, never a correctness or
tail-latency dependency. W^X platforms veto runtime compilation entirely — that
is what mode (b) is for. **Falsifiers:** the six experiments in §8 rows 8–13; the meta-gate
(GG-0049) decides whether (c) stays the default.

### 3.10 Observability & debugging (GG-0024, GG-0008)

**Delve is structurally impossible** with graalgo (scoped verdict, T7 §6.2): Delve
debugs Go binaries via DWARF and Go runtime internals; graalgo executes Truffle
ASTs, so there is no Go binary to attach to. The replacement is the **Truffle
Chrome Inspector** (`--inspect`, CDP: breakpoints/stepping/variable
inspection/eval, including stepping across language boundaries) `[DECIDED]` —
graalgo must emit accurate `SourceSection`s from Go source positions so the
inspector shows Go code. `go tool pprof` compatibility comes via a
**`profile.proto` translation layer** (CPU profiles from stack sampling or
translated JFR `jdk.ExecutionSample` streams; heap profiles from allocation
samples) served from a `net/http/pprof`-compatible endpoint `[DECIDED]` —
a translation layer, not a format invention. Custom JFR events:
`graalgo.GoroutineStart/End` (aliasing the JDK's virtual-thread events in JIT
mode), `graalgo.GoroutinePark` (reason + duration; rate-controlled — duration
threshold for blocking parks, 1-in-1024 sampling for quantum parks),
`graalgo.ChannelBlock`, GC activity mapped onto `jdk.GC*`, and
`graalgo.ModuleCrash` for momo attribution `[DECIDED]`. Pinning detection is
free via `jdk.VirtualThreadPinned` (GG-0057).

### 3.11 momo isolates (GG-0025, GG-0026, GG-0027)

GraalVM isolates fit momo's crash-containment model better than the runtime fork
does `[PROPOSAL]` promoted to `[DECIDED]` as the architecture: **isolate-per-
module-generation** gives containment by construction (independent heap and GC;
no isolate can access another's heap `[VERIFIED]`), crash-only restart = new
isolate + re-run constructor + atomic pointer swap, and value-only boundaries
become unnecessary *within* the mechanism. Resources: `MaxHeapSize` +
`PolyglotException.isResourceExhausted()` map onto momo's memory-quota→panic→
restart; **no CPU quota mechanism exists for isolates — honest gap** (GG-0025):
not supported in v1, documented limitation, supervisor-side accounting as the
fallback. Cross-isolate calls cross a native boundary (costlier; `HostAccess.
SCOPED` recommended to avoid host/guest cyclic GC references `[VERIFIED]`).
Isolates are **native-image-only**: in pure-JIT mode the fallback is **OS
processes per generation** (GG-0026) `[DECIDED]` — weaker containment,
documented. `SpawnIsolate` was EE-only/experimental in 22.0 `[VERIFIED]` — its
maturity **must be re-verified on the pinned GraalVM distribution** before the
program depends on it (GG-0027) `[DECIDED]`; isolate create/teardown latency and
cross-isolate call overhead are measured against the momo kill+join baseline
(the numbers are `[UNVERIFIED]` until the spike measures them).

---

## 4. Interfaces & contracts

Contracts are the seams the upgrade workflow and the phase handoffs depend on.
Each is versioned; each names its compatibility check.

### 4.1 AST↔runtime intrinsic contract (GG-0059)

The intrinsic signatures (GG-0020) are the contract between the Truffle AST
lowering (Phase 1) and the Java runtime library (Phase 2) `[DECIDED]`. At the
**Phase-1 interface freeze** the signatures are versioned and documented; Phase 4
(driver) starts in parallel only after this freeze. Compatibility is checked in
CI: the AST may only call frozen signatures. **Kill trigger** ([PHASES.md](PHASES.md) §3):
interface freeze impossible without breaking the toil bound → architecture
replan.

### 4.2 `-toolexec` argv contract (GG-0059)

The wrapper intercepts the tool invocations the driver routes through
`-toolexec` — `compile`, `asm`, `cgo`, `link`, `cover`, `pack` `[VERIFIED]` —
and answers `tool -V=full` with the graalgo backend identity so action IDs
auto-namespace `[DECIDED]`. The argv mapping per tool is documented and
versioned at the same Phase-1 freeze; `GOFLAGS=-toolexec=…` handling (user flags
composing with the wrapper) is part of the contract. Divergence from the
stock tool's argv expectations is a drop-in bug, caught by the txtar matrix.

### 4.3 Go↔Java interop contract (GG-0005)

Per-type mappings `[DECIDED]` (illustrative APIs in T7 §2.3 are not normative;
this table is):

| Go | Java | Semantics |
|---|---|---|
| `string` | `java.lang.String` | copy |
| `[]byte` | `byte[]` | bulk copy; no shared backing array |
| other slices | `java.util.List` via `ProxyArray` | view, copy on explicit request |
| maps | `java.util.Map` via `ProxyObject` | view, copy on explicit request |
| `error` | `RuntimeException` proxy (value + message + Go stack) | copy |
| Java `Throwable` | Go `error` (`Message()`, `JavaStack()`, `Unwrap()`) | copy |
| `func` | lambda / functional interface (`ProxyExecutable`) | adapter, generated |
| channel | opaque handle (`Send`/`Recv`/`Close`) | handle, never a `BlockingQueue` |

**View-vs-copy defaults to copy**; views are explicit and documented per call
`[DECIDED]` — view-vs-copy is the sharpest edge in the contract. Go-from-Java is
annotation/codegen-driven at build time (closed-world-friendly: no runtime
reflection scanning, which also keeps the native-image reachability story
tractable). Host access defaults to `SCOPED`-like scoping, never `ALL`
`[DECIDED]`. Language features cross the boundary only by explicit decision per
release (T2 §4.5).

### 4.4 Build-cache identity (GG-0028)

Behavioral compatibility, not byte-identity `[DECIDED]`: `go test` caching shows
the same hit/miss patterns; cache *entries* differ because tool identity differs.
The wrapper's `tool -V=full` answer namespaces graalgo artifacts away from gc
artifacts in a shared `GOCACHE` automatically `[VERIFIED]` on the hashing design.
`GOCACHEPROG` remains a ready-made injection point.

### 4.5 GODEBUG contract (GG-0055)

Every `GODEBUG` knob is classified per release — **emulated / no-op /
unsupported-with-justification** — in a versioned registry `[DECIDED]`. Unknown
knobs default to deny-with-error (never silent ignore). The registry is reviewed
every upgrade as part of Phase 2 (`godebug-diff` against `doc/godebug.md`
history). Parity-stance decisions for GC/scheduler knobs with no JVM analog are
recorded once in the architecture log (GG-0045) and then mechanical forever —
e.g. Green Tea GC → "the JVM's GC *is* our GC; `nogreenteagc` is
accepted-and-ignored" (stance pattern `[VERIFIED]` against 1.26/1.27 knobs).

---

## 5. Testing strategy

The strategy is a first-class deliverable (GG-0010), built early because it is
the development conformance net first and the upgrade-toil reducer second
`[DECIDED]`. Per-commit CI runs layers 1–2; per-release runs all four layers
(~2–6h attended oracle compute `[ESTIMATE]`).

### 5.1 Layer 1 — fast differential smoke (GG-0029, GG-0030, GG-0031)

A curated ~200-program corpus through the differential harness, minutes-scale
`[ESTIMATE]`. Red = stop, triage now. This layer is the Phase-0 harness seed and
the per-commit gate.

### 5.2 Layer 2 — language conformance (GG-0032)

`src/test/` language suite (356 files `[VERIFIED]`) — the spec-semantics anchor
independent of the stdlib — plus new-feature tests written **red-green first**
(they must fail before the lowering change). Runs per-commit on frontend
changes.

### 5.3 Layer 3 — stdlib + cmd/go script suites (GG-0029)

`go test ./...` (stdlib) as the primary corpus, the 929 txtar driver scripts,
and the interop conformance suite. Hours-scale `[ESTIMATE]`; per-release gate.
Red routes to the four-way triage: *graalgo bug* (fix) / *upstream behavior
change* (adapt + snapshot update) / *backend-specific* (exclusion list + reason
code) / *nondeterminism* (harness normalization fix) `[DECIDED]`.

### 5.4 Layer 4 — generative differential fuzzing (GG-0037)

Two layers `[DECIDED]`: (i) native `testing.F` differential fuzzing of stdlib
functions (encoding/json, compress/flate, net/url, go/parser round-trip) with
identical corpora under gc and graalgo; (ii) program generation across the
feature space (generics instantiation, interface dispatch, closure capture,
defer/panic/recover chains, select, map iteration, atomics) through the §5.5
harness. Reuse `go-fuzz`/`testing.F` where possible; a gosmith-class Go program
generator is `[UNVERIFIED]` — verify existence/license and gc-parity before
depending on it, else build a small template-based generator. Corpus
minimization on every new divergence; minimized cases become permanent
regressions. Soak: overnight per release (8–24h `[ESTIMATE]`, unattended).

### 5.5 The differential oracle (GG-0030, GG-0031)

The reference `go` toolchain is ground truth `[DECIDED]`. **Compared:** stdout
bytes, stderr bytes (after normalization), exit codes, panic messages and
goroutine traces **modulo addresses** (regex-scrub `0x…`, goroutine IDs,
timestamps, durations, sandbox-external paths), and sandboxed file/network
effects (FS writes redirected to a temp dir, tree diffed; loopback-only
networking with a recorded stub server). **Excluded:** wall-clock timings,
absolute addresses, GC statistics, raw goroutine scheduling. **Nondeterminism
controls** (GG-0031): normalization passes before comparison; `GOMAXPROCS=1` for
the scheduling-sensitive subset; order-unspecified outputs (map iteration)
compared as multisets — annotated per-test, never global; 3–5 repeat runs to
classify flaky vs deterministic divergence; fixed seeds for generator inputs;
`testing/synctest` preferred in new conformance tests.

### 5.6 Exclusion list (GG-0033)

Backend-specific exclusions are **expected and versioned** `[DECIDED]`: tests
asserting gc internals (GC stats, exact stack traces modulo inlining,
`//go:linkname` into gc-only symbols), debugger tests, cgo tests, tests depending
on gc scheduling quirks. The list lives in-repo (e.g.
`conformance/exclusions/go1.27.yaml`) with a **reason code per entry**
(backend-specific / unspecified-behavior / deferred-parity). It grows **only
through per-release review**; its release-to-release diff is reviewed every
upgrade; a shrinking list is the project's health metric.

### 5.7 Oracle validation (GG-0034)

The harness is validated by **mutation** `[DECIDED]`: deliberately break
graalgo's lowering (invert a comparison, drop a defer) and confirm the harness
goes red. A differential harness that has never been seen failing is not
evidence. Every test is demonstrably capable of failing (red-green/mutation
evidence; no tautological tests) — the world-class-implementation testing
doctrine applies to the harness itself.

### 5.8 API/ABI diffing — the toil-reduction engine (GG-0035)

Built once, run every upgrade Phase 2 `[DECIDED]`: `apidiff` (new
pkgs/funcs/methods/consts from `api/go1.N.txt`), `go/types`-visible signature
diff, `rtlink` (linkname-target list + `runtime`-internal import graph — the true
runtime-coupling watch-list, since exported `runtime` API was unchanged in both
1.26 and 1.27 `[VERIFIED]`), `flagdiff` (cmd/go `--help` snapshots), and
`godebug-diff`. Output: the categorized per-component work list that drives
upgrade Phases 3–7. The tooling carries its own red-green evidence: N-vs-N
expects empty, N-vs-(N+1) expects exactly the known `api/go1.N+1.txt` content.
**`respdiff`** (added 2026-10-03, spike 03 F8): diffs `objabi.expandArgs` /
`ParseArgs` / `encodeArg` between releases — the response-file wire format
changed in 1.27 with no `useResponseFile` change, silently corrupting
cross-version files through the wrapper. Any diff → re-sync the wrapper's
parser (verbatim port, unit-tested against a captured response file) and run
the §6.1 arg-shape battery before concluding.

### 5.9 Behavior snapshots (GG-0036)

Per release, record the differential-corpus results **under gc**: normalized
stdout/stderr hashes, exit codes, full normalized outputs, stored
content-addressed under `snapshots/go1.N/` `[DECIDED]`. On upgrade: re-run under
the *new* gc and under graalgo. Drift in gc's own snapshots = upstream semantic
change to triage (1.27's `compress/flate` output bytes and `net.UnixConn` EOF
change would both have been caught mechanically `[VERIFIED]` against the 1.27
notes); drift in graalgo's = regression.

### 5.10 Gate integration and triage (GG-0038)

Gating order is fast→slow, wired into upgrade Phase 8 `[DECIDED]`: smoke →
language conformance → stdlib corpus + scripts + interop → fuzz soak. Every red
gate produces a **triage record** (classification + disposition + issue link);
the upgrade report aggregates them. **No gate is waived without a recorded
reason** — waivers are how toil becomes unbounded.

---

## 6. Upgrade workflow

The agent-driven workflow that makes requirement (2) credible `[DECIDED]`.
Trigger: the release-watch agent fires at **rc1** (~2–4 weeks before .0), not at
release day.

### 6.1 The 9-phase runbook (GG-0039)

1. **Mechanical rebase.** Inputs: upstream source tarball + checksums. Fetch,
   checksum-verify against go.dev, lay down `third_party/go/src`; regenerate
   (stringer outputs, `go/types` Generate, unicode tables); rebase graalgo's
   patch stack (kept near zero per GG-0044) onto the new tree. Done-gate:
   vendored tree byte-identical to upstream except the recorded patch stack.
2. **Scoped diffs (the toil-reduction engine).** Run the §5.8 tooling; expand the
   release notes into a per-bullet checklist. Output: a **categorized work list
   per component** — this is what turns "upgrade Go" into bounded tickets.
3. **Language delta.** Implement the lowering for new semantics; add the `go1_N`
   gate; write conformance tests for the new feature *first* (red-green).
   Done-gate: new-feature tests green, `-lang` regression suite green.
4. **Runtime intrinsics.** Work the `rtlink` report top-down; record every
   parity-stance decision in the architecture log (one decision, permanent).
   Done-gate: every new runtime-surface item dispositioned (implemented /
   deferred-with-issue / wont-parity-with-reason).
5. **Stdlib.** Triage new suite failures with the four-way classification
   (§5.3); review the exclusion-list diff. Done-gate: no new failures vs the
   previous release's baseline.
6. **Driver.** Port the `flagdiff` + behavior deltas; run the 929 script tests
   via the adapter. Run the **arg-shape conformance battery** (added 2026-10-03,
   spike 03 F8 — single-script `cgo_long_cmd` is weak coverage): response files
   exercising quoting, embedded newlines, unicode, empty args, very long args,
   and arg-count boundaries, each run stock vs wrapped with byte-identical
   argv comparison. Done-gate: pass rate ≥ previous release; arg-shape battery
   green.
7. **Interop + AOT configs.** Regenerate configs with the tracing agent; review
   the config diff; run interop conformance. Done-gate: config diff reviewed, no
   unexplained additions.
8. **Oracles** (§5): smoke → conformance → stdlib+scripts → fuzz soak; triage
   loop per §5.10.
9. **Done-gates + upgrade report.** All gates green; file the upgrade report:
   effort log per component, deferred items with issues, toil metrics
   (agent-hours, wall-clock, skip-list delta, rebase conflicts). The report is
   what makes the *next* upgrade cheaper — and what proves "bounded".

### 6.2 Patch-release light path (GG-0040)

Patch releases (~monthly, security/bug-fix only `[VERIFIED]`) take the light
path — Phase 1 + Phase 8 only — unless the patch notes touch language/driver
behavior (read the notes; security patches occasionally do).

### 6.3 Weekly release-watch agent (GG-0041)

A scheduled agent watches golang-announce / `golang/go` releases for rc1 and .0
tags `[DECIDED]`. On detection it does **not** upgrade; it prepares: creates
`bump/go1.N`, runs Phases 1–2 (vendor + regen + all five diff tools), files an
**intake report** (categorized work list with per-component estimates), and
notifies. It **never merges, never declares done-gates, never edits the
exclusion list** — preparation is mechanical; judgment stays in the upgrade
workflow.

### 6.4 Toil accounting and bound enforcement (GG-0042)

Every upgrade report publishes per-component toil accounting; the P95 bounds
(≤ 200 agent-hours, ≤ 10 wall-clock days, skip-list net growth ≤ 15, rebase
conflicts ≤ 5 files, zero unexplained divergences) are checked mechanically, not
asserted `[DECIDED]`.

**P95-calibration trigger** (spike-3 honesty note, [spikes/03-versiontax/REPORT.md](spikes/03-versiontax/REPORT.md)
§6): the dry-run table compared extrapolated P50 against a P95 bound — a
category mismatch; heavy-class P95 is likely over the bound, and no breach was
observed, so the bound is promoted **without** bound-compliance evidence. The
first real upgrade SHALL measure the per-component toil distribution and
recalibrate; if measured P95 breaches ≤200 agent-hours, the toil-reduction
sprint (§6.5) fires before the next release `[DECIDED]`.

### 6.5 Breach handling (GG-0043)

Breaching any bound triggers a **toil-reduction sprint** before the next release
`[DECIDED]`. Two consecutive breaches force an explicit re-scope decision with
Carlo on requirement (2) — the only requirement with no completion date, and the
program's lifetime cost driver (2–4 PY/year forever `[ESTIMATE]`).

### 6.6 Patch-stack discipline (GG-0044)

Local patches against vendored Go sources are kept near zero `[DECIDED]`: every
local modification is a twice-yearly rebase conflict. Budget: ≤ 5 conflicted
files per upgrade; breach triggers a toil-reduction review (upstream the patch,
or move the customization behind a seam).

### 6.7 Parity stances (GG-0045)

For every runtime-internal behavior with no JVM analog (GC algorithm, scheduler
specifics, malloc size classes), the stance — *we parity X / we deliberately
diverge on Y because Z* — is decided once, in writing, in the architecture log
`[DECIDED]`. Phase 4 of the runbook then becomes mechanical: new items get
stances, old items are never re-debated.

### 6.8 GOTOOLCHAIN semantics (GG-0046)

When a module's `go`/`toolchain` line is newer than the bundled toolchain, stock
`go` *switches* toolchains (PATH lookup or module download + re-exec)
`[VERIFIED]`. graalgo's strategy is **track-latest + faithful selection**
`[DECIDED]`: implement the selection semantics faithfully, document
`GOTOOLCHAIN=local` as the pin, and keep lag near zero — every month of lag
pushes more real-world modules into switch/refuse paths. `go version` output,
`runtime.Version()`, the `toolchain` directive (incl. `toolchain default`), and
`GOTOOLCHAIN` env/`go env -w` handling are version-faithful.

---

## 7. Phase-0 open-question closures

One subsection per study §10 question. Each ends with its evidence label and the
observation that would falsify it. Zero `[OPEN]` on go/no-go items.

### 7.1 Sulong vs direct Truffle AST — decided

**Closure:** spike 6 landed **COMMIT-DIRECT** 2026-10-03 `[DECIDED]` — proceed
with the direct Truffle AST route. The scored comparison came out 6 vs 19
against Sulong; root cause is the IR version gap (Go→LLVM frontends emit LLVM
22, Sulong reads ≤12.0.1 — broken today) plus no maintained full-language
Go→LLVM frontend and Sulong's lack of precise-GC/threading/signal primitives
([spikes/06-sulong/REPORT.md](spikes/06-sulong/REPORT.md) §1–§3). The [PHASES.md](PHASES.md) REPLAN trigger (Sulong
wins *and* the direct-AST estimate doubles) is not met — no estimate-doubling
argument exists either. The default is the direct Truffle AST, full stop.
**Re-test triggers** (each re-opens the fork, per the verdict-scope rule):
(a) Sulong's bitcode support reaches current LLVM; (b) a maintained
full-language Go→LLVM frontend appears; (c) Sulong gains
precise-GC/threading/signal primitives. (GG-0047)

### 7.2 Preemption-equivalence bar — the observable list

**Closure:** the differential oracle must hold these scheduling observables
equivalent vs gc `[DECIDED]` (GG-0048): (i) fairness — no goroutine starvation
under contention within a bounded window; (ii) starvation-freedom — every
runnable goroutine eventually runs; (iii) latency distribution of wakeups
(channel ops, timers) within a documented tolerance band, not bit-identical
timings; (iv) `GOMAXPROCS=n` behavior incl. the pinned-carrier matrix; (v)
`runtime.Gosched` and preemption-point responsiveness. Exact preemption *timing*
is excluded (Go's memory model promises no preemption timing — the scheduler's
parks are semantically safe). **Falsifier:** the preemption-torture suite re-run
against the spike-2b design (Phase 2) showing systematic divergence the
user-space scheduler cannot close → kill trigger.

### 7.3 unsafe.Pointer v1 — handle-table emulation

**Closure:** §3.4 / GG-0016 `[DECIDED]`. **Falsifier:** spike 4 finds
`reflect`/`unsafe` requirements (the 243 unsafe uses, measured) that neither the
handle table nor an isolated FFM-arena raw-address mode can cover → re-scope
the v1 semantics.

### 7.4 Netpoller — deleted

**Closure:** §3.5 / GG-0018 `[DECIDED]` — blocking NIO on virtual threads
replaces the event loop. **Falsifier:** the Phase-2 differential run of the
`net`/`os/signal` suites against the §3.5 scheduler shows edge-triggered or
signal-delivery semantics that blocking NIO cannot express → adapt instead of
delete (scoped workaround, not a program kill).

### 7.5 cgo→Panama — authorship and platform list

**Closure:** graalgo authors the C ABI struct-layout engine in-house (Java);
platform list is linux/amd64 first, linux/arm64 second; darwin and windows
deferred with the §10.2 re-test trigger `[DECIDED]` (GG-0022). The stock `cgo`
tool and host gcc are kept — C compilation itself is never reimplemented.
**Falsifier:** the Phase-4 cgo corpus shows systematic ABI divergence from the
layout engine on a supported platform → kill trigger.

### 7.6 AOT reachability metadata — tracing agent, zero hand-authored target

**Closure:** the tracing agent derives reachability metadata for the full stdlib;
the target is **zero hand-authored entries** `[DECIDED]` (GG-0052). If the target
is missed, the hand-authored tax is measured and explicitly accepted — a
permanent tax is never silent. **Falsifier:** Phase-5 done-gate — agent-derived
configs with unexplained gaps, or a hand-authored tax nobody measured.

### 7.7 plugin.Open — unsupported in AOT modes, supported in JIT-only

**Closure** `[DECIDED]` (GG-0051): `plugin.Open` is a documented error in
AOT-only and hybrid modes (closed-world images cannot load new code) with an
exclusion-list entry; it is supported in JIT-only mode. **Falsifier:** a
workload class that requires plugins in deployment *and* cannot use JIT-only —
re-scopes the requirement, not the mechanism.

### 7.8 Hybrid default — the meta-gate decides

**Closure:** the hybrid becomes the default **iff** it beats both single modes
on ≥1 workload class across the six falsifying experiments (image-size delta,
warmup, safepoint overhead, pinning audit, deopt torture, stack-depth parity)
`[DECIDED]` (GG-0049). If the meta-benchmark fails, the mode is cut — the
program continues with (a)+(b). **Falsifier:** any single experiment in §8 rows 8–13
failing its bar for the hybrid's claimed advantage.

### 7.9 SpawnIsolate maturity + isolate CPU quotas

**Closure** `[DECIDED]` (GG-0027, GG-0025): `SpawnIsolate` maturity is
re-verified on the **pinned** GraalVM distribution in Phase 0 before the program
depends on it (it was EE-only/experimental in 22.0 `[VERIFIED]` — the verdict
"production-ready on CE" is not established). Isolate CPU quotas: **no mechanism
exists — accept the gap**; v1 documents it as a limitation with supervisor-side
accounting as the fallback. Re-verify 2028+. **Falsifier:** the pin's
`SpawnIsolate` proves unusable → the momo-isolation design re-scopes to
processes (GG-0026) in all modes.

### 7.10 go vet — reuse the stock binary

**Closure** `[DECIDED]` (GG-0053): reuse the stock `go vet` binary unchanged —
vet analyzes Go source via `go/*` packages, which the backend swap does not
touch, so the wrapper is compatible by construction. Verified by the txtar
matrix (spike 5 covers vet scripts). **Falsifier:** a vet script diverging
through the wrapper → the wrapper's `vet` interception is fixed, not vet
itself.

### 7.11 Pure-JIT momo isolation — OS processes

**Closure** `[DECIDED]` (GG-0026): isolates are native-image-only, so in
pure-JIT mode each momo generation runs in its own OS process. Weaker
containment than isolates (shared-nothing by OS, not by heap design), documented
as the dev-mode story. **Falsifier:** process-per-generation overhead breaks the
dev-loop budget → accept weaker in-process `Context` sandboxing with documented
fidelity loss.

### 7.12 OSR in Native Image — verify, don't assume

**Closure** `[DECIDED]` (GG-0054): OSR behavior in Native Image is verified
against the pinned GraalVM distribution in Phase 0 (Truffle's `RepeatingNode`
OSR recipe is documented; its exact behavior inside Native Image is
`[UNVERIFIED]` until measured). The hybrid's long-loop story documents the
*measured* mechanism. **Falsifier:** the pin lacks usable OSR → long loops in
the hybrid rely on deopt-from-AOT-baseline only, documented as such.

### 7.13 Windows — out of v1 scope

**Closure** `[DECIDED]` (GG-0023): Windows is out of v1 scope (2–3× cost: IOCP +
6.7k-line syscall layer `[VERIFIED]`); darwin is a moderate deferral. Re-test
trigger in §10.2: re-measure after Linux is green. **Falsifier of the
deferral:** none in Phase 0 — the trigger is the scheduled re-measurement, not a
spike.

### 7.14 Agent-leverage discount — dropped, measured instead

**Closure** `[DECIDED]` (GG-0056): the 20–40 PY estimate carries **no**
agent-leverage discount. Agent leverage is instead measured as a program metric
— actual agent-hours vs estimate per phase — and calibrated empirically. An
unquantified discount is not a plan. **Falsifier:** measured leverage
consistently ≥2× across phases → the estimate is re-baselined with evidence.

### 7.15 GODEBUG — versioned per-knob registry

**Closure** `[DECIDED]` (GG-0055): §4.5 — every knob classified per release
(emulated / no-op / unsupported + justification), unknown knobs deny-by-default,
registry reviewed per release via `godebug-diff`. **Falsifier:** a release whose
`doc/godebug.md` history adds a knob the registry cannot classify → the upgrade
cannot close its Phase-2 gate until it can.

---

## 8. Risks & falsification matrix

A phase that cannot fail is badly designed. Each row names the claim, the
cheapest falsifying experiment, and the kill trigger. Rows 1–7 are the Phase-0
spikes; rows 8–13 are the hybrid-mode falsifiers.

| # | Claim under test | Falsifying experiment | Kill trigger |
|---|---|---|---|
| 1 | Plan 9 asm + cgo in runtime/stdlib is intrinsifiable/emulatable (spike 1) — **completed 2026-10-03: PASS** (four-strategy classification exhaustive) | Intrinsify amd64 atomics/bytealg, emulate the rest; run affected stdlib tests under a stub backend | Any load-bearing package whose asm has no intrinsifiable/emulatable formulation |
| 2 | User-space scheduler over 1:1 vthreads delivers M:N scheduling observables (option C; spike 2 KILLED, spike 2b PASS — risk retired) | Spike-2b battery (F1–F6: preemption torture, channels/select/timers/scale, 10k-goroutine quiescence, overhead budget, GOMAXPROCS fidelity, lost-wakeup stress) + Phase-2 preemption-torture re-run at server scale | Systematic divergence the scheduler cannot close; residual risks hardening into mechanism blockers (overhead mean unfixable by the 10ms quantum, cold-drainer latency unexplained at server scale, slot-releasing NIO wrapper throughput cost unbounded) → re-scope |
| 3 | "Always latest Go" lifetime cost is bounded (spike 3) — **completed 2026-10-03: PASS** (dry run within P95 ≤200 agent-hours; P95-calibration trigger armed for the first real upgrade, §6.4) | Dry-run upgrade of the Phase-0 prototype across one real release boundary; measure toil vs ≤200 agent-hour bound | Bound breached with no credible toil-reduction path |
| 4 | reflect/unsafe/linkname long tail is toolable (spike 4) — **completed 2026-10-03: PASS** (descriptor type-layout core + handle-table unsafe covers `reflect`'s uses) | Type-layout core of the intrinsic surface + handle-table `unsafe`; run `reflect`/`unsafe` suites | Per-release linkname churn defeats diff scoping (skip-list growth > 15 twice) |
| 5 | cmd/go behavioral parity via the wrapper (spike 5) — **completed 2026-10-03: PASS** (1.27.1, linux/amd64, 929-txtar matrix) | `-toolexec` backend swap across a matrix of the 929 txtar scripts | argv-interception gaps force the driver fork *and* the fork's merge burden breaks the toil bound |
| 6 | Direct Truffle AST beats Sulong (spike 6) — **completed 2026-10-03: Sulong 6 vs direct 19; replan trigger not met** | Scored Go→LLVM→Truffle comparison: build cost, version coupling, semantic mismatch catalog | Re-test only on spike-6 triggers (current-LLVM Sulong, maintained Go→LLVM frontend, precise-GC/threading/signal primitives) |
| 7 | The differential harness can catch divergences (spike 7) — **completed 2026-10-03: PASS** (45/45 SAME, 9/9 mutation classes DETECTED) | Mutation: deliberately break the lowering (invert a comparison, drop a defer) and require the harness to go red | The harness stays green on a broken lowering → the oracle is not evidence; rebuild it |
| 8 | Hybrid image-size delta is distributable | Build a Go-subset Truffle interpreter as a native image with/without the optimizing runtime; measure against the ~40% GraalPy bound `[VERIFIED]` | Delta makes the binary undistributable → (c)-as-default dies |
| 9 | The embedded compiler needs no warmup | Interface-heavy + channel-heavy benchmark: time-to-peak for (a) vs (c) | (c) ≈ (a) on time-to-peak → the warmup advantage claim dies |
| 10 | Park/quantum overhead stays under the 1% budget at the 10ms default quantum | Park/resume-hop microbenchmark at 10ms quanta on server hardware (prototype: 33.0µs mean/hop at 1ms quanta on the contended 2-vCPU VM; 29.9µs at 10ms) | Mean over 1% with no scoped quantum fix, or cold-drainer p99 dominating the mean beyond the scoped fix → the §3.5 mitigation dies |
| 11 | cgo/pinning transiently consumes dumb-pool FJP capacity without breaking fairness (GG-0057) | cgo-heavy workload under (c) with JFR `jdk.VirtualThreadPinned` events + pool-capacity accounting | Pinning breaks fairness or transient capacity loss is unbounded → the dumb-pool model dies; the pinning audit is a Phase-2 done-gate |
| 12 | Deopt in-image is correct | Megamorphic interface-dispatch flip-flopping inside an image; results bit-identical to the interpreter | Any divergence → (c) correctness dies |
| 13 | Stack-depth parity is acceptable | Deep-recursion parity vs gc (expect earlier `StackOverflowError`; quantify) | The gap breaks real programs (e.g. deeply recursive stdlib paths) → silent-compat claims die |

**Program kill criteria** `[DECIDED]` (from [PHASES.md](PHASES.md) §11): any spike kill trigger
fires without a scoped workaround; the version-tax dry run breaches the toil
bound with no credible reduction path; two consecutive releases breach the toil
bound (→ toil-reduction sprint, then re-scope requirement (2) with Carlo if
unfixable); the hybrid's meta-benchmark fails → cut the mode, not the program.

## 9. Estimates, staffing, critical path

| Phase | Range (PY) |
|---|---|
| 0 — Spec + kill-or-commit spikes | 0.5–1 |
| 1 — Language core | 3–6 |
| 2 — Runtime (goroutines/channels/GC) | 4–8 |
| 3 — Stdlib bring-up | 6–12 |
| 4 — Driver drop-in | 3–6 |
| 5 — Optimizations + three modes | 3–6 |
| 6 — Upgrade pipeline completion | 1–2 |
| **Total one-time** | **20–40 PY** |
| Steady-state version tracking | 2–4 PY/year, forever |

All figures `[ESTIMATE]`, confidence **LOW** (±50%), calibrated on TruffleRuby
(~a decade, ~5–7 core engineers, ~40–60 PY to production viability), GraalJS,
gccgo (4–6 PY, never reached parity), TinyGo (subset since 2018), and gc itself
`[HIST]`. **No agent-leverage discount is applied** (GG-0056, §7.14) — leverage
is measured per phase and calibrated empirically. Critical path 0→1→2→3→5 ≈
**4–5 calendar years at full staffing** `[ESTIMATE]`; driver (4) and pipeline (6)
parallelize after the Phase-1 interface freeze (GG-0059). Staffing notes: Phase 0
needs 2–3 senior (compiler + runtime + JVM); Phases 2–3 are the staffing peak
(runtime and stdlib-port grind parallelize by package group); Phase 4 needs
toolchain-internals depth (cmd/go, cgo, Panama).

## 10. Provenance & verdict scope

### 10.1 Claim provenance ledger

Extends [FEASIBILITY.md](FEASIBILITY.md) §11 with the decisions this spec makes:

| Claim | Source | Label |
|---|---|---|
| Seven program requirements | Carlo, refined 2026-10-03 | `[DECIDED]` |
| Wrap-via-`-toolexec`; fork only as escape hatch | this spec §3.1 (T3 analysis) | `[DECIDED]` |
| Verbatim `go/*` frontend; atomic re-vendor; `-lang` gates | this spec §3.2 (T2 analysis) | `[DECIDED]` |
| One Truffle AST; specialization budget only at dynamic sites | this spec §3.3 (T5 adversarial) | `[DECIDED]` |
| Java-object value model; unsafe v1 = handle table | this spec §3.4 (T5) | `[DECIDED]` |
| Goroutine = vthread + scheduler token; user-space quantum scheduler (option C); netpoller deleted; panic/defer model | this spec §3.5 (spike 2b PASS) | `[DECIDED]` |
| Spike 2 (1:1 on FJP, back-edge yields): KILLED — 0 latency samples, systematic starvation | [spikes/02-loom/](spikes/02-loom/) | `[VERIFIED]` |
| Spike 2b (option C): PASS — 11,824 vs 0 samples, in-flight cap assert never tripped, 48,140/48,140 parks/admissions, 40,588/40,588 wakeups won, scoped fixes (initFifo, 10ms quantum) | [spikes/02b-userspace-scheduler/](spikes/02b-userspace-scheduler/) | `[VERIFIED]` |
| Spike 1 (asm/cgo surface taxonomy): PASS — four-strategy classification exhaustive over the cgo-relevant asm surface (scope §7; spike 4 drives `reflect.Value.Call` through it) | [spikes/01-asmcgo/](spikes/01-asmcgo/) | `[VERIFIED]` |
| Spike 3 (version-tax dry run): PASS — all five diff/scope tools run in <15s total across a real minor Go boundary; P95 ≤200 agent-hours / ≤10 days bound promoted to `[DECIDED]` with the P95-calibration trigger (§6.4) | [spikes/03-versiontax/](spikes/03-versiontax/) | `[VERIFIED]` |
| Spike 4 (reflect/unsafe): PASS — descriptor-based type-layout core + handle-table `unsafe.Pointer` emulation covers everything `reflect`'s unsafe uses | [spikes/04-reflect/](spikes/04-reflect/) | `[VERIFIED]` |
| Spike 5 (cmd/go parity via `-toolexec`): PASS — stock Go 1.27.1, linux/amd64, logging+delegating wrapper across the 929-txtar matrix (scope note in REPORT §7; argv-interception kill trigger carried in §8 row 5) | [spikes/05-cmdgo/](spikes/05-cmdgo/) | `[VERIFIED]` |
| Spike 6 (Sulong vs direct AST): **COMMIT-DIRECT** — 6 vs 19; IR version gap dispositive (LLVM 22 emitted vs ≤12.0.1 readable); re-test triggers (a)/(b)/(c) in §7.1 | [spikes/06-sulong/](spikes/06-sulong/) | `[DECIDED]` |
| Spike 7 (differential harness): PASS — 45/45 SAME (gc-a vs gc-b), 45/45 rerun SAME, 9/9 mutation classes DETECTED, unit tests green; operational and CI-green | [spikes/07-harness/](spikes/07-harness/) | `[VERIFIED]` |
| Intrinsic surface; linkname by symbol name | this spec §3.6 (T4 measurement) | `[DECIDED]` |
| cgo→Panama; graalgo authors the layout engine; MM-0017 gates FFI | this spec §3.7 (T7) | `[DECIDED]` |
| Linux-first; Windows out of v1 | this spec §3.8 (T4) | `[DECIDED]` |
| Hybrid default (pending meta-gate); AOT baseline without JIT | this spec §3.9/§7.8 (T5) | `[DECIDED]` procedure |
| Delve impossible → Chrome Inspector; pprof via profile.proto | this spec §3.10 (T7) | `[DECIDED]` |
| Isolates for momo; no CPU quota in v1; JIT-mode = processes | this spec §3.11 (T7) | `[DECIDED]` |
| Four-layer testing strategy, built early; mutation-validated oracles | this spec §5 (T2) | `[DECIDED]` |
| 9-phase upgrade runbook; release-watch agent; toil bounds | this spec §6 (T2) | `[DECIDED]` |
| All 15 §10 closures | this spec §7 | `[DECIDED]` |
| Devirtualization cascade (mechanism / magnitude) | Carlo 2026-10-03; T6 | `[VERIFIED]` / `[SPECULATIVE]` |
| 20–40 PY + 2–4 PY/yr; no agent-leverage discount | T8; this spec §7.14 | `[ESTIMATE]` |
| This change: §3.5/§7.2/§7.4/§8 row 2/§10 rewritten around option C | SPEC rewrite 2026-10-03 (spike 2b §6.1) | `[DECIDED]` |

### 10.2 Verdict-scope ledger

Per the standing rule: every negative verdict is scoped to the tested
configuration, with a re-test trigger. Phase-0 entries:

| Verdict | Tested configuration | Re-test trigger |
|---|---|---|
| Driver reimplementation rejected | ~74k LOC Java-driver estimate, T3 | A stock-driver seam disappears upstream |
| Off-heap primary storage rejected | Managed-Go value model, T5 §2.3 | A design that needs C-layout identity at rest |
| `ControlFlowException` for panic rejected | Need for printed traces on fatal panics, T5 §4.4 | Measured evidence that recovered-panic throughput is a bottleneck |
| `cmd/compile` internals fork rejected | Same porting surface as `go/*` with worse stability, T2 §3 | `go/*` compatibility promise withdrawn |
| Windows out of v1 | IOCP + 6.7k-line syscall layer cost, T4 | Linux green → re-measure (§7.13) |
| Isolates have no CPU quota | GraalVM isolate API survey Oct 2026, T7 §5.2 | 2028+ re-verification |
| Spike-2 kill (yield-to-FJP design) | JDK 25.0.4, 2-vCPU VM, OFF / POLL-10ms / POLL-1ms-watchdog / UNCOND / PARK 1µs–1ms configs | The kill indicts the yield-to-FJP design, not 1:1 vthreads as the execution vehicle; re-test triggers (a)/(b)/(c) in §3.5 — all "superseded by option C as a program decision" |
| Spike-2b fairness (11,824 samples/5s at 1ms; 13,337 at 10ms; CV 0.0016–0.0348 across legs) | 2-vCPU VM, drainer thread, 100µs/1ms/10ms quanta | Different CPU count or JDK → re-test |
| Spike-2b overhead (33.0µs mean/hop at 1ms; 29.9µs at 10ms = 0.30%) | 2-vCPU VM with high thread-wakeup latency | Server hardware → re-test |
| Spike-2b lost-wakeup freedom (48,140/48,140 parks/admissions; 40,588/40,588 wakeups won) | Tested interleavings in the stress battery | Formal verification / model checker before production |
| GOMAXPROCS in-flight cap | Prototype hard assert | Production must preserve the assert |

---

## Appendix A — Glossary

- **backend:** the Truffle-based compile+run pipeline replacing gc (frontend +
  AST lowering + Java runtime + mode packaging).
- **intrinsic surface:** the ~60–70-function versioned runtime contract the
  adapted stdlib builds against (GG-0020).
- **toolexec seam:** the stock driver's `-toolexec` interception point where the
  backend is swapped in (GG-0011).
- **AOT+JIT hybrid:** application + interpreter + the Graal JIT itself
  AOT-compiled into one binary; the embedded JIT re-optimizes hot guest code at
  runtime (Truffle-on-SubstrateVM, not HotSpot tiering).
- **oracle:** the differential-vs-gc test harness (GG-0030).
- **exclusion list:** versioned, reason-coded backend-specific test exclusions
  (GG-0033).
- **parity stance:** a once-decided position on a runtime behavior with no JVM
  analog (GG-0045).
- **toil bound:** P95 ≤ 200 agent-hours and ≤ 10 wall-clock days per release
  upgrade (GG-0004).
- **go/no-go item:** a program requirement (§2), a §7 closure, or a §8 risk —
  must end `[DECIDED]`/`[VERIFIED]`/`[HIST]`, never `[OPEN]`. (§8 risk rows
  carry their resolution in the §10.1 provenance ledger rather than a
  terminal label.)

## Appendix B — Requirement index

| ID | Title | Primary spec home |
|---|---|---|
| GG-0001 | Drop-in CLI surface | §2.1 |
| GG-0002 | Drop-in build/test/module behavior | §2.1 |
| GG-0003 | Extra JVM APIs | §2.2 |
| GG-0004 | Bounded version tracking | §2.3 |
| GG-0005 | Bidirectional Go-Java interop | §2.4 (§4.3 contract) |
| GG-0006 | Three execution modes | §2.5 |
| GG-0007 | Beyond-gc optimization bar | §2.6 |
| GG-0008 | Modern JVM integration | §2.7 (§3.10 mechanisms) |
| GG-0009 | Stdlib reuse | §2.8 |
| GG-0010 | Testing strategy built early | §2.9 |
| GG-0011 | Driver wrapper via -toolexec | §3.1 |
| GG-0012 | Verbatim frontend vendoring | §3.2 |
| GG-0013 | No language pinning | §3.2 |
| GG-0014 | One Truffle AST | §3.3 |
| GG-0015 | Java-object value model | §3.4 |
| GG-0016 | unsafe.Pointer v1 semantics | §3.4 (§7.3 closure) |
| GG-0017 | Goroutines on virtual threads with user-space scheduler | §3.5 |
| GG-0018 | Netpoller deletion | §3.5 (§7.4 closure) |
| GG-0019 | Panic/defer/Goexit model | §3.5 |
| GG-0020 | Intrinsic surface | §3.6 |
| GG-0021 | Linkname by symbol name | §3.6 |
| GG-0022 | cgo via Panama FFI | §3.7 (§7.5 closure) |
| GG-0023 | Linux-first OS scope | §3.8 (§7.13 closure) |
| GG-0024 | Debugging and profiling story | §3.10 |
| GG-0025 | Isolates for momo | §3.11 (§7.9 closure) |
| GG-0026 | JIT-mode isolation fallback | §3.11 (§7.11 closure) |
| GG-0027 | Pinned GraalVM distribution | §3.11 (§7.9 closure) |
| GG-0028 | Build-cache behavioral compatibility | §4.4 |
| GG-0029 | Four test layers | §5.1–§5.4 |
| GG-0030 | Differential oracle definition | §5.5 |
| GG-0031 | Nondeterminism controls | §5.5 |
| GG-0032 | Conformance corpora | §5.2 |
| GG-0033 | Versioned exclusion list | §5.6 |
| GG-0034 | Mutation-validated oracles | §5.7 |
| GG-0035 | API/ABI diff tooling | §5.8 |
| GG-0036 | Behavior snapshots | §5.9 |
| GG-0037 | Generative differential fuzzing | §5.4 |
| GG-0038 | Gate integration and triage | §5.10 |
| GG-0039 | Nine-phase upgrade runbook | §6.1 |
| GG-0040 | Patch-release light path | §6.2 |
| GG-0041 | Release-watch agent | §6.3 |
| GG-0042 | Toil accounting | §6.4 |
| GG-0043 | Breach handling | §6.5 |
| GG-0044 | Patch-stack discipline | §6.6 |
| GG-0045 | Parity stances | §6.7 |
| GG-0046 | GOTOOLCHAIN fidelity | §6.8 |
| GG-0047 | Sulong decision rule | §7.1 |
| GG-0048 | Preemption-equivalence bar | §7.2 |
| GG-0049 | Hybrid meta-gate | §7.8 |
| GG-0050 | AOT baseline quality | §3.9 |
| GG-0051 | plugin.Open scope | §7.7 |
| GG-0052 | Reachability metadata pipeline | §7.6 |
| GG-0053 | go vet reuse | §7.10 |
| GG-0054 | OSR verification | §7.12 |
| GG-0055 | GODEBUG registry | §4.5 (§7.15 closure) |
| GG-0056 | No agent-leverage discount | §7.14 (§9) |
| GG-0057 | Pinning audit | §3.5, §8 |
| GG-0058 | MM-0017 gate on FFI | §3.7 |
| GG-0059 | Interface freeze | §4.1, §4.2 |
| GG-0060 | Mode selection | §2.5 |
| GG-SCHED-0001 | Quantum park on Safepoint.poll expiry | §3.5 |
| GG-SCHED-0002 | Drainer admission discipline | §3.5 |
| GG-SCHED-0003 | In-flight cap equals GOMAXPROCS | §3.5 |
| GG-SCHED-0004 | Scheduler-aware blocking only | §3.5 |
| GG-WAKE-0001 | Park/unpark pairing safety | §3.5 |
| GG-WAKE-0002 | Lost-wakeup invariant | §3.5 |
| GG-WAKE-0003 | Wake and init priority lanes | §3.5 |
| GG-FAIR-0001 | Quanta distribution bounds | §3.5 |
| GG-FAIR-0002 | Per-hop overhead budget | §3.5 |
| GG-FAIR-0003 | Starvation freedom at scale | §3.5 |

## Appendix C — Pinned-dependency record

| Dependency | Pin | Status |
|---|---|---|
| Go release tracked | go1.27.1 (re-vendored atomically per release, GG-0012) | `[VERIFIED]` current as of 2026-10-03 |
| GraalVM/JDK distribution | Oracle GraalVM **25.0.4.1.1** (LTS line; jars re-pinned in [phases/phase1/lib/LIB.md](../../internal/backend/truffle/lib/LIB.md)) (GG-0027) | `[DECIDED]` 2026-10-04 — program owner: LTS line over the Innovation line (25.4.x); compiler/JIT deltas re-evaluated at Phase 5 |
| `go/*` frontend API | additive-only per the Go 1 compatibility promise | `[VERIFIED]` |

> Note on the former `[OPEN]` in this appendix: it was explicitly **not** a
> go/no-go item. The program decision — *pin the distribution and verify
> SpawnIsolate/OSR against the pin before depending on them* — is `[DECIDED]`
> (GG-0027, §7.9, §7.12). The concrete version string was recorded 2026-10-04:
> Oracle GraalVM 25.0.4.1.1 (LTS). **Standing directive (program owner,
> 2026-10-04):** avoid relying on any deprecated API wherever possible —
> enforced mechanically by `-Xlint:deprecation -Werror` in the Phase-1 build
> (a deprecated-API use is a build break, not a warning).
