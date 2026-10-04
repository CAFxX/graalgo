# graalgo — Feasibility Study

**Date:** 2026-10-03 · **Status:** study, no implementation · **Tracks:** `research/T1`–`T8`
**Evidence labels:** `[HIST]` established history · `[VERIFIED]` primary-source confirmed ·
`[PROPOSAL]` recommended but undecided · `[DECIDED]` settled · `[OPEN]` must be closed

> **Erratum 2026-10-03:** spike 2 KILLED the 1:1-yield-to-FJP scheduler design (FJP starvation:
> 0 samples vs 200,000 on stock Go); spike 2b PASS decided **option C** — goroutines stay 1:1
> on virtual threads, but fairness is owned by a user-space quantum scheduler (indefinite
> `LockSupport.park()` on quantum expiry, FIFO+runnext token queues, platform-thread drainer,
> in-flight cap = GOMAXPROCS, Loom's FJP demoted to a dumb thread pool with the default
> scheduler). Concurrency-mechanism text in §1 (Verdict paragraph, hardest-problem #1) and
> §4(5) below is superseded; **`SPEC.md` §3.5 is normative**; decisions and residuals are in
> `adr/ADR-0001.md` (accepted 2026-10-03). Historical study record as of 2026-10-03
> pre-spike-2b; `SPEC.md` §1.1 is the normative record for everything the spikes decided.

---

## 1. Verdict

**CONDITIONALLY FEASIBLE** — as a multi-year R&D program (not as a quick project), with the
architecture in §4. Confidence is split: **MODERATE** that the technical design works end to end;
**LOW** (±50%) on the 20–40 person-year one-time cost and the ~4–5 year critical path. The dominant
risk is not any single mechanism — every mechanism has a shipping precedent — it is the *lifetime*
cost of requirement (2): tracking every Go release forever, at 2–4 PY/year, with no finish line.

**Recommended architecture (one paragraph):** Do not reimplement the `go` driver and do not fork
the compiler frontend. Ship `graalgo` as the *stock Go distribution* with the compile backend
swapped behind the existing `-toolexec` seam: `go/parser`+`go/types` (re-vendored per release)
feed a single Truffle AST that runs in three execution modes — (a) JIT-only on HotSpot for the dev
loop, (b) AOT-only Native Image for W^X/size-constrained targets, and (c) the default **AOT+JIT
hybrid**, where the application, the Truffle interpreter, and the Graal compiler itself are
AOT-compiled into one native binary and the embedded JIT re-optimizes hot guest code at runtime
(the shipping Truffle-on-SubstrateVM mechanism, not HotSpot-style tiering). Go values live as Java
objects (write barriers and exact roots come free from the JVM); goroutines map 1:1 to virtual
threads with a user-space quantum scheduler (option C: indefinite park-on-quantum-expiry;
back-edge *yields* superseded 2026-10-03 — see SPEC.md §3.5 / ADR-0001; Loom never preempts); the stdlib is reused
maximally — 58% of packages compile unchanged, the coupled 42% builds against a ~60–70-function
intrinsic surface plus a ~78-package adapted fork, with linkname compatibility by *symbol name*,
not gc ABI; cgo is replaced by Panama FFI; GraalVM isolates provide the momo project's
crash-containment domains. Version tracking is an **agent-driven, tooled workflow** bounded at
P95 ≤ 200 agent-hours per release — not a mechanical pipeline — gated by a differential-vs-gc
testing strategy that is built early and treated as a first-class deliverable.

**The 3 hardest problems:**

1. **Goroutine scheduler semantics on virtual threads.** Go has M:N work-stealing with async
   preemption; Loom gives 1:1 virtual threads on carriers with *no* preemption. Faithful
   `GOMAXPROCS`, fairness, and preemption-equivalent behavior are provided by a user-space
   quantum scheduler: quantum-expiry parks (`LockSupport.park()` at quantum-expiring
   `Safepoint.poll()`s), FIFO+runnext token queues, a platform-thread drainer, and an in-flight
   admission cap = min(GOMAXPROCS, carrier parallelism) — *superseded 2026-10-03 by spike 2b /
   ADR-0001*; the earlier back-edge-yield and custom-carrier-pool mechanism was killed with
   spike 2. The single most semantics-sensitive piece of the runtime, and the one most likely
   to produce Heisenbugs that differential testing must catch. `[DECIDED]`
2. **Perpetual version-tracking toil.** Go ships every ~182 days like clockwork `[VERIFIED]`;
   the per-release tax is bounded (P95 ≤ 200 agent-hours) but *forever*, at an estimated 2–4
   PY/year. This is the program's lifetime cost driver, and the only requirement with no
   completion date. The mitigation is architectural: choose seams that minimize per-release
   surface (stable `go/*` frontend, `-toolexec` wrapper instead of a driver fork) and build the
   API/ABI-diff + differential-test tooling early so each upgrade is scoped mechanically.
   `[VERIFIED]` on cadence; `[PROPOSAL]` on the bound.
3. **The semantic long tail: reflect/unsafe/linkname + cmd/go behavioral parity.** The first 80%
   (language core, most of the stdlib) is bounded engineering; the last 20% — 243 `unsafe` uses
   in `reflect`, 52 linknamed symbols with per-release churn, 929 txtar driver scripts encoding
   decades of behavioral edge cases — is what turned TruffleRuby into a decade-long program
   `[HIST]`. "Drop-in" is decided here, not in the happy path.

**Headline numbers:** 20–40 PY one-time (LOW confidence) + 2–4 PY/year steady-state forever;
critical path Phase 0→1→2→3→5 ≈ 4–5 calendar years at full staffing. Phase 0 is a kill-or-commit
gate: seven cheap spikes (five risk spikes + Sulong evaluation + testing-harness seed) retire the top risks or stop the program for <1 PY.

---

## 2. Requirements (as refined 2026-10-03)

1. **Drop-in:** `alias go=graalgo` indistinguishable from real `go` (CLI surface, flags, exit
   codes, module/build-cache/test/vet behavior); graalgo additionally exposes JVM-integration APIs.
2. **Version tracking:** always match the most recent Go release. Refinement `[DECIDED]`: a fully
   mechanical upgrade is not expected to be possible; the requirement is **bounded, tooled,
   agent-driven upgrades** — P95 ≤ 200 agent-hours and ≤ 10 wall-clock days per release, with
   breach triggering a toil-reduction sprint.
3. **Full Go↔Java interop**, both directions, shared data types.
4. **AOT and JIT with optimizations beyond gc**, in **three** execution modes `[DECIDED]`:
   (a) pure JIT (Truffle on HotSpot), (b) pure AOT (Native Image, no runtime compilation),
   (c) **AOT+JIT hybrid: the application code AND the JIT compiler itself are AOT-compiled into
   the binary; at runtime the embedded JIT re-optimizes hot code** (Truffle-on-SubstrateVM
   runtime compilation — verified shipping, not HotSpot-style tiering).
5. **Modern JVM integration:** virtual threads, async I/O (io_uring: no JDK JEP as of Oct 2026 —
   Loom roadmap only `[VERIFIED]`), isolates, JFR/VisualVM.
6. **Reuse the Go stdlib** wherever possible.
7. **Testing strategy as a first-class deliverable** `[DECIDED]`: differential testing vs gc as
   oracle, conformance corpora, per-release API/ABI diffing, behavior snapshots, generative
   differential fuzzing — **built early**, before the first upgrade it is meant to cheapen.

---

## 3. Prior art & effort calibration

1. **No Go-on-Truffle/GraalVM project has ever been published** `[VERIFIED]` (exhaustive search;
   nearest are a 23-commit 2020 toy and coursework). graalgo is genuine greenfield: no corpse to
   autopsy, no code to salvage. (`research/T1-prior-art.md`)
2. **gccgo** (in GCC, `-compiler gccgo` driver hook): the Go team never gated gc's evolution on it;
   it now lags the language badly. Lesson: currency needs a *mechanical, tooled* per-release
   process, not goodwill. `[HIST]`
3. **gollvm** (gofrontend + LLVM + libgo): README FAQ states explicitly "not intended as a
   replacement for gc"; stalled ~2020 on LLVM bit-rot. Lesson: an explicit non-replacement
   charter predicts exactly this outcome. `[HIST]`
4. **llgo** (2012–14): self-hosting, passed most of the stdlib suite — then vanished; its `gllgo`
   driver was the only drop-in-`go build` shim attempt and died on `go/build` hard-coding.
   Lesson: the driver seam is the hard part of drop-in, and it has *zero* successful prior art.
   `[HIST]`
5. **TinyGo** (alive): reuses `go/*` + `x/tools/go/ssa` for the whole frontend, reimplements the
   runtime, ports the stdlib package-by-package. Lesson: this is the proven shape — **reuse the
   frontend wholesale, reimplement only the runtime** — and survival came from a niche gc serves
   badly plus a sustained maintainer. `[HIST]`
6. **Effort calibration:** TruffleRuby took ~a decade of a dedicated multi-person Oracle Labs team
   to reach full production compatibility `[HIST]`. Price the runtime + stdlib port + driver, not
   the AST interpreter. gccgo ≈ 4–6 PY by one engineer and never reached parity `[HIST]`.
7. **Sulong alternative** (Go→LLVM IR→Truffle, no Go AST interpreter) `[OPEN]`: T1 flags it as the
   one architectural shortcut that must be evaluated before committing. Phase-0 spike verdict
   (see `PHASES.md` §2): it trades the AST-interpreter build cost for dependence on gollvm-class
   bit-rot risk and a second IR's version coupling. Default remains the direct Truffle AST
   (`[PROPOSAL]`, T5), but the spike must produce a scored comparison, not an assumption.

---

## 4. Recommended architecture (expansion of §1)

1. **Driver: wrap, don't reimplement, don't fork (yet).** Ship the stock Go distribution; swap
   only the compile backend behind the existing `-toolexec` seam (`src/cmd/go/internal/work/build.go:354`
   wraps every `compile`/`asm`/`cgo`/`link`/`cover`/`pack` invocation; `toolID()` queries
   `tool -V=full` *through* the wrapper) `[VERIFIED]` on go1.27.1. The driver is then identical by
   construction; per-release driver cost becomes a conformance gate (the 929 txtar scripts in
   `src/cmd/go/testdata/script/`), not a port. Forking `cmd/go` is the escape hatch if argv
   interception proves insufficient; a Java reimplementation is rejected (~74k LOC, lowest
   fidelity-per-effort). (`research/T3-toolchain-surface.md`)
2. **Frontend: `go/parser`+`go/types` as a vendored service**, re-vendored atomically per release
   (`[PROPOSAL]`, T2). Never fork compiler internals; never pin old language versions — support
   old code via `-lang` gates. `go.mod` `go 1.x` directives and GOTOOLCHAIN switching are inherited
   from the stock driver via the wrapper.
3. **One Truffle AST feeds all three modes** — modes differ only in packaging, never in a second
   backend (`[PROPOSAL]`, T5). DSL specialization budget is spent only at genuinely dynamic
   sites: interface dispatch (PIC on itab identity — the single most valuable specialization),
   type switches (decision trees + `ValueProfile`), maps (inline fast path, `@TruffleBoundary`
   slow path).
4. **Value model: Java objects throughout.** Descriptor-based structs (`long[]`+`Object[]`, since
   runtime class definition is unavailable in closed-world images), typed-array-backed slices,
   `(itab, data)` interfaces, `byte[]` strings. "Go pointer" = opaque Java reference: the GC
   moves objects freely, write barriers and exact roots come free from the JVM; pinning only at
   the cgo/FFM boundary. `unsafe.Pointer` v1 = handle-table emulation (`[PROPOSAL]`, T5).
5. **Goroutines = virtual threads 1:1** (JEP 444 `[VERIFIED]`); `synchronized` no longer pins on
   JDK 24+ (JEP 491 `[VERIFIED]` from the JEP) — only native/FFM calls pin, matching Go's
   existing cgo economics. **Loom never preempts**: `Safepoint.poll()` at back-edges (and
   function entries) triggers quantum-expiry parks (`LockSupport.park()`; the yield-based
   mechanism was superseded 2026-10-03 by spike 2b / ADR-0001); `GOMAXPROCS` maps to the
   scheduler's in-flight cap = min(GOMAXPROCS, carrier parallelism) on the default Loom
   carrier pool (no custom carrier pool). The netpoller is
   *deleted* in favor of blocking NIO on virtual threads (io_uring has no JDK JEP as of Oct 2026).
   `panic` = `AbstractTruffleException` carrying the value with lazy traces; `Goexit` =
   `ControlFlowException`; defer = explicit per-goroutine chain. (`research/T5`, `T7`)
6. **Stdlib: maximal reuse.** 229 of 398 package dirs (58%) pass a strict pure-Go check (no
   `unsafe`, no `runtime` import, no linkname, no `.s`) and compile unchanged given the frontend
   + intrinsic surface `[VERIFIED]` by source measurement. The load-bearing 42% builds against a
   **~60–70-function intrinsic surface** (memmove/memequal, makeslice/growslice, map/chan/select
   ops, interface/itab machinery, panic/defer, scheduler, netpoller contract, atomics, timers,
   fastrand, write barrier, traceback) plus exact `rtype`/itab layouts for `reflect`'s 243
   `unsafe` uses. Plan 9 asm is 485 files / ~127k lines: runtime-core asm evaporates (no Go stacks
   on Truffle), atomics/bytealg are intrinsified, crypto is emulate-then-intrinsify, `math` is
   covered by JVM IEEE754. **Linkname compatibility is by symbol name, not gc ABI** (52
   stdlib→runtime symbols + ~100 reverse edges): graalgo compiles both sides, so only the names
   must match — the tax is per-release churn of internal symbols, paid via a linkname-diff step
   and an adapted stdlib fork limited to the ~78 coupled packages. Linux-first is well-bounded
   (72 syscall files, epoll); darwin is a moderate deferral; windows is 2–3× cost (IOCP + 6.7k-line
   syscall layer) and deferred. (`research/T4-stdlib-reuse.md`)
7. **cgo → Panama FFI** (JEP 454, final in JDK 22 `[VERIFIED]`): keep the stock `cgo` tool and
   host gcc for the C side under all options — do not reimplement C compilation. The hard part
   is the link/ABI boundary and a JVM-aware `runtime/cgo` (Panama bridge for thread-state,
   callbacks, tracebacks): medium-hard, the program's riskiest integration item. Arena lifetimes
   beat cgo pointer rules; the new toolchain-author risk is concentrated in the C ABI
   struct-layout engine. (`research/T3`, `T7`)
8. **Isolates ↔ momo.** GraalVM isolates fit Carlo's momo project (per-module crash containment +
   transparent restart, value-only boundaries) *better than the runtime fork does*: isolate-per-
   generation gives containment by construction, crash-only restart = new isolate, `MaxHeapSize` +
   `isResourceExhausted` maps to memory quotas. Honest gaps: no CPU quota mechanism exists for
   isolates; `SpawnIsolate` maturity must be re-verified on the pinned GraalVM distribution; and
   isolates are native-image-only, so pure-JIT mode needs a weaker story. (`research/T7`)

---

## 5. Three-mode matrix

| Concern | (a) JIT-only (HotSpot) | (b) AOT-only (Native Image) | (c) AOT+JIT hybrid **[PROPOSAL: default]** |
|---|---|---|---|
| Startup | Slow (interpreter + warmup) | Fastest | Fast (AOT baseline; no interpreter warmup) |
| Peak performance | Highest ceiling (full speculation) | Lowest (no speculation/deopt) | Near-JIT (speculation+deopt on hot guest code) |
| Build cost | None (no image build) | Minutes-scale image build | Highest (image build + embedded compiler, tens-of-MB image growth `[ESTIMATE]`) |
| Debuggability | Best (full JVM tooling, JFR, VisualVM) | Weakest (no JMX/VisualVM — known gap) | Mixed (JFR for host; runtime-compiled guest as `jdk.Compilation` events `[UNVERIFIED]`) |
| Implementation effort | Baseline | Closed-world metadata pipeline | (b) + embedded-compiler config |
| Per-release toil | Frontend+runtime only | + reachability-metadata regen | ≈ (b): the embedded compiler is GraalVM-version-coupled, ~zero marginal Go-release cost `[PROPOSAL]` |
| Use | Dev inner loop | W^X / size-constrained targets | Default deployment |

What the hybrid's embedded JIT can do that pure AOT cannot: speculation + deopt on live guest
behavior, continuous re-profiling / adaptive interface dispatch, partial evaluation of the Go
program itself `[VERIFIED]` against GraalVM primary sources (closed-world analysis computes the
runtime-compilable method set; graph encodings live in the image heap; Protos validated guest
compilation in a native executable, Sept 2026). What it cannot do vs HotSpot: recompile frozen
host code, compile arbitrary new host bytecode (experimental only), HotSpot-grade code-cache
management `[UNVERIFIED]`; CE lacks Oracle-only runtime optimizations. What breaks:
closed-world correctness cliffs (a missing metadata entry = failure, not slowdown — worse for Go
`reflect`/`plugin`/cgo than for Java), build-time vs live profile staleness, deopt-metadata
bloat, W^X/platform vetoes, CE/Oracle split. **Meta-gate:** if the hybrid never beats *both*
single modes on any workload class, cut the mode. Six falsifying experiments (image-size delta,
warmup, safepoint overhead, pinning audit, deopt torture, stack-depth parity) are specified in
`research/T5` §8.3 and `research/T6` §9.5 — the recommendation must survive contact with
measurement.

---

## 6. Deep dives (condensed; full analysis in the cited notes)

**6.1 Frontend & version tracking** (`research/T2-version-tracking.md`). Go releases every ~182
days; measured per-release churn (1.26.7→1.27.1: `src/go` 2,893 / `cmd/go` 6,540 / `runtime`
15,338 lines changed). Toil audit per component with mechanical-vs-judgment split (e.g. new
`go/types` API surface: mechanical; changed runtime intrinsic semantics: manual review). The
agent-driven upgrade runbook: 9 phases from rc1 trigger → mechanical rebase → scoped diffs →
language/runtime/stdlib/driver/interop deltas → oracles → done-gates + upgrade report; light path
for patch releases. **"Bounded" is quantified:** P95 ≤ 200 agent-hours, ≤ 10 wall-clock days,
skip-list growth ≤ 15, rebase conflicts ≤ 5, zero unexplained divergences — breach triggers a
toil-reduction sprint. A weekly release-watch agent *prepares* upgrades (vendor + regen + diffs +
intake report) but never merges or declares done. Architecture guidance: prefer seams that
minimize per-release toil — stable `go/*` frontend over forked internals, `-toolexec` wrapper
over driver fork (fork ≈ 2× toil forever).

**6.2 Toolchain drop-in surface** (`research/T3-toolchain-surface.md`). Drop-in = 19 commands +
shared build-flag set + `go env` variables + GOPROXY/sumdb module behavior + two-level test-result
caching + vet-via-JSON-`vetConfig` + exit codes 0/1/2 + `PackagePublic` JSON stability — with the
929 txtar scripts as the de-facto conformance suite. What drop-in does **not** require:
byte-identical binaries, or byte-identical cross-backend build-cache sharing (action IDs hash
tool identity via `-V=full`, so differing backends auto-namespace in a shared `GOCACHE`;
behavioral cache compatibility is the bar, verified by test). cgo keeps stock C compilation; the
risk is the link/ABI boundary + JVM-aware `runtime/cgo`.

**6.3 Stdlib reuse** (`research/T4-stdlib-reuse.md`). See §4(6) for the numbers. Strategy per asm
category: intrinsify (atomics/bytealg), evaporate (runtime core), emulate-then-intrinsify
(crypto), JVM-covers (math). Build-tagged scope: Linux-first bounded; darwin moderate deferral;
windows 2–3× deferred; `runtime/cgo` + `plugin` stubbed as unsupported initially.

**6.4 Truffle implementation** (`research/T5-truffle-architecture.md`). See §4(3–5). Adversarial
PE verdict: Go's static typing vacates Truffle's type-discovery miracle — the real wins are
speculative interface devirtualization, Graal's escape analysis, and inlining budget. Expect a
gc-class baseline with headroom, not a miracle.

**6.5 Optimization inventory** (`research/T6-optimization-inventory.md`). Adversarial result: the
"beyond gc" pitch is real but narrow — (1) loop auto-vectorization (gc has none; GraalVM added
it), (2) always-on runtime profiling vs gc's opt-in PGO, (3) partial escape analysis past gc's
flow-insensitive EA. gc is a strong baseline (mid-stack inlining since 1.12, PGO inlining +
devirtualization, good EA, excellent BCE) — **every claimed win must beat gc *with* PGO**, and
every claim in the inventory names its falsifying benchmark. Lock elision is ~irrelevant for Go;
Valhalla is non-actionable (JEP 401 preview, still nullable, no flattened layouts — revisit
2028+). Workload map: plausibly beats gc on vectorizable loops / shifting interface dispatch /
partial-escape allocation shapes; matches on straight-line and PGO'd code; loses on startup,
warmup, memory footprint, and cgo/interop crossings. All quantitative claims `[SPECULATIVE]`
until the named benchmarks run.

**6.5a Primary optimization thesis: the devirtualization cascade** `[PROPOSAL]` (Carlo, 2026-10-03;
mechanism `[VERIFIED]` against Graal/Truffle semantics, magnitude `[SPECULATIVE]`). The single
largest structural gap in gc's optimizer is interface dispatch: gc cannot inline through an
interface call at all, so every hot `io.Reader.Read` / `http.Handler` / `error` path pays itab
indirection *plus* the loss of all downstream optimization. The Truffle JIT closes this with a
cascade: (i) polymorphic inline caches profile the itab at each call site; (ii) hot monomorphic/
bimorphic sites are speculatively devirtualized to direct calls with deopt guards; (iii) the
concrete method body inlines; (iv) with the concrete types now visible, Graal's escape analysis
scalar-replaces and stack-allocates values that previously "escaped" into the interface —
heap-to-stack promotion across abstraction boundaries. Go is unusually well positioned for this:
structural interfaces give a fixed, compile-time-known method set per (type, iface) pair (no
v-table hierarchy walk), and value semantics mean less aliasing for EA to chew through once
inlined. Honest bounds: megamorphic sites do not devirtualize (Phase-0 must measure real call-site
polymorphism distributions); speculation costs deopt guards, code bloat, and warmup — the win is
steady-state and must be measured against gc *with* PGO on interface-heavy workloads
(http handlers, encoding/json, io pipelines); pure-AOT mode gets only the PGO-baked subset of
this, so the cascade is also the technical argument for why the JIT and hybrid modes matter.
Falsifying benchmark: interface-heavy suite, gc+PGO vs graalgo-JIT steady state — if the cascade
does not move it, the thesis is wrong and the "beyond gc" pitch shrinks to vectorization.

**6.6 JVM ecosystem integration** (`research/T7-jvm-integration.md`). Polyglot API on
`org.graalvm.polyglot` with SCOPED host access; proposed `graalgo/java` package with
copy-semantics mappings (string/String, []byte/byte[], slices→List views, maps→Map,
error↔exception proxies, func↔lambdas, channels as opaque handles) — illustrative, `[PROPOSAL]`.
Delve is structurally impossible → Truffle Chrome Inspector (`--inspect`, CDP) replaces it;
`go tool pprof` compat via a `profile.proto` translation layer on a pprof endpoint `[PROPOSAL]`.
Custom JFR events for goroutine/channel/GC activity (incl. `graalgo.ModuleCrash` for momo).
Panama replaces cgo (§4(7)); momo's MM-0017 verifier ban must gate the new FFI path too.

---

## 7. Testing strategy (first-class deliverable)

Built early (harness seed in Phase 0 — it doubles as the development conformance net; see
`PHASES.md` §9), because it is the mechanism that makes "bounded per-release toil" credible:

1. **Differential testing vs gc as oracle.** The reference `go` toolchain is ground truth: compile
   and run the same programs under both, compare stdout/stderr, exit codes, panic
   messages/traces modulo addresses, and sandboxed file/network effects. Excluded: timings,
   addresses, raw goroutine scheduling. Nondeterminism tamed via `GOMAXPROCS=1` + multiset
   comparison + repeat runs; the harness itself is mutation-validated.
2. **Conformance corpora.** Stdlib's own `go test ./...` as the primary corpus, plus targeted
   corpora for historically churny areas (runtime, reflect, unsafe, sync, net/netpoller,
   os/signal, plugin/cgo). Backend-specific tests live in a **versioned exclusion list with
   reason codes, reviewed per release** — the list may only grow through review, never silently.
3. **Release-to-release API/ABI diffing.** Tooling diffs exported symbols, `go/types`-visible
   signatures, the runtime intrinsic set, and the cmd/go flag surface between releases,
   producing the scoped work list for the upgrade agent. This is the toil-reduction engine: the
   diff tells the agent where to look.
4. **Behavior snapshots.** Per-release recorded differential-corpus results under gc, so silent
   semantic drift in either toolchain is caught mechanically on upgrade.
5. **Generative differential fuzzing.** A program generator across the feature space (generics,
   interfaces, cgo-free), run under graalgo vs gc, with corpus minimization — the long-tail
   semantic-equivalence net. Reuse `go-fuzz` / `testing.F` infrastructure where possible; a
   gosmith-class Go generator is `[UNVERIFIED]` and may need building.
6. **Gate integration.** Fast differential smoke → conformance corpora → stdlib+script suites →
   fuzz soak; each layer gates the upgrade, red gates route to triage with the diff-scoped work
   list, and test results *are* the upgrade report. Every layer meets the falsifiability bar:
   each test demonstrably capable of failing (mutation/red-green evidence; no tautological tests).
7. **Cost honesty.** Full oracle compute ≈ 2–6h attended per upgrade (hardware-dependent,
   `[ESTIMATE]`); per-commit runs smoke + conformance only, the full four layers run per-release.

---

## 8. Risks (top 5 by probability × cost, with cheapest falsifying spike)

1. **Plan 9 asm + cgo in runtime/stdlib** — spike: intrinsify atomics/bytealg for amd64, emulate
   the rest, run the affected stdlib tests. Kill trigger: any load-bearing package whose asm has
   no intrinsifiable/emulatable formulation.
2. **Goroutine M:N semantics on Loom virtual threads** — spike: 1:1 mapping + back-edge yields +
   custom carrier pool; run the `runtime`/`sync`/`net` stress tests differentially vs gc. Kill
   trigger: systematic scheduling-observable divergence that yields cannot close.
   > **Historical note (2026-10-03):** this spike **FIRED its kill trigger** — preemption
   > torture 0 vs 200,000 latency samples; Loom's FJP has no woken-thread priority and no
   > public scheduler API (verified in JDK 25 sources). The 1:1-yield design is dead;
   > superseded by the user-space scheduler (spike 2b PASS). This study text is preserved
   > as the historical record; `SPEC.md` §3.5 is the normative architecture and its §10.2
   > ledger carries the supersession.
3. **"Always latest Go" lifetime cost** — the highest *lifetime* expected cost (2–4 PY/yr forever).
   Spike: perform a dry-run upgrade of the Phase-0 prototype across one real release boundary
   and measure toil against the ≤200 agent-hour bound. Kill trigger: bound breached with no
   credible toil-reduction path.
4. **reflect/unsafe/linkname long tail** — spike: implement the intrinsic surface's type-layout
   core + handle-table `unsafe`, run `reflect`/`unsafe` suites. Kill trigger: per-release
   linkname churn exceeds the diff-tooling's ability to scope it (skip-list growth > 15 twice).
5. **cmd/go behavioral paper cuts** — spike: `-toolexec` backend swap on a matrix of the txtar
   scripts. Kill trigger: argv-interception gaps that force the driver fork *and* the fork's
   merge burden breaks the toil bound.

Program kill criteria: any Phase-0 spike kill trigger fires without a scoped workaround; two
consecutive releases breach the toil bound; the hybrid's meta-benchmark fails (never beats both
single modes on any workload class → cut the mode, not the program).

---

## 9. Estimates

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

Confidence **LOW** (±50%), calibrated on TruffleRuby (~10 yrs, ~5–7 core, ~40–60 PY to production
viability), GraalJS, gccgo (4–6 PY, never reached parity), TinyGo (subset since 2018), and gc
itself (decades). No discount applied for agent assistance — the agent-leverage factor is a
separate assumption, `[DECIDED]` dropped per SPEC §7.14 (consistency audit 2026-10-03). Critical path 0→1→2→3→5 ≈ 4–5 calendar
years at full staffing; driver (4) and pipeline (6) parallelize after the Phase-1 interface
freeze. Full per-phase done-gates and kill criteria: `PHASES.md`.

---

## 10. Open questions (decision-relevant; must close in Phase 0 unless noted)

1. `[OPEN]` Sulong (Go→LLVM→Truffle) vs direct Truffle AST — Phase-0 scored spike verdict.
2. `[OPEN]` Exact goroutine preemption-equivalence bar: which scheduling observables must match gc
   (fairness? starvation freedom? latency distribution?) — the differential oracle needs the list.
3. `[OPEN]` `unsafe.Pointer` v1 semantics: handle-table emulation vs raw-address mode, and what
   `reflect`'s 243 unsafe uses actually require.
4. `[OPEN]` Netpoller deletion vs adaptation: is blocking-NIO-on-virtual-threads sufficient for
   the full `net`/`os/signal` contract, including edge-triggered subtleties?
5. `[OPEN]` cgo→Panama: who authors the C ABI struct-layout engine, and is its platform list
   acceptable for a drop-in claim?
6. `[OPEN]` AOT reachability metadata for the full stdlib: can the tracing agent derive it, or
   is hand-authored config a permanent tax?
7. `[OPEN]` `plugin.Open` semantics under closed-world AOT — supported, emulated, or excluded?
8. `[OPEN]` Hybrid default confirmation: the six falsifying experiments (§5) decide.
9. `[OPEN]` `SpawnIsolate` maturity on the pinned GraalVM distribution; isolate CPU quotas (no
   mechanism exists — accept, emulate, or drop the requirement?).
10. `[OPEN]` `go vet` under graalgo: reuse stock vet binary (wrapper-compatible?) or rehost
    analyses on `go/types` service?
11. `[PROPOSAL]` Pure-JIT mode needs a (weaker) momo-isolation story — isolates are
    native-image-only. Options: processes, or accept weaker containment in dev mode.
12. `[OPEN]` OSR in Native Image for the hybrid's long-loop story — verify, don't assume.
13. `[OPEN]` Windows support: accept the 2–3× cost later, or declare Linux(+darwin)-only?
14. `[DECIDED]` Agent-leverage discount on the 20–40 PY estimate — **dropped** (SPEC §7.14:
   no discount applied to program estimates; staffing is headcount, not agent-hours).
15. `[OPEN]` `GODEBUG` compatibility surface: which knobs are emulated, which are no-ops, who
    decides per release?

---

## 11. Claim provenance & verdict-scope ledger

| Claim | Source | Label |
|---|---|---|
| No Go-on-Truffle ever published | `research/T1-prior-art.md` (exhaustive web search) | `[VERIFIED]` |
| TruffleRuby decade calibration; gccgo/gollvm/llgo/TinyGo lessons | T1 (project histories, primary readmes) | `[HIST]` |
| Go ~182-day release cadence; per-release churn line counts | T2 (release notes; `~/tools/go` diffs) | `[VERIFIED]` |
| `-toolexec` seam; 19 commands; 929 txtar scripts; exit codes | T3 (`~/tools/go` go1.27.1 source paths) | `[VERIFIED]` |
| 58% pure-Go stdlib; ~60–70 intrinsics; 485 asm files / 127k lines; 52+~100 linkname edges | T4 (`rg`/`find`/`wc` over `~/tools/go/src`, methods documented) | `[VERIFIED]` |
| One AST → three modes; value model; ~~back-edge yields~~ (killed 2026-10-03; superseded by the user-space scheduler, SPEC §3.5); panic-as-exception | T5 (GraalVM docs/source; JEP 444/491) | `[PROPOSAL]`/`[VERIFIED]` on JEPs |
| JIT-in-image shipping (runtime-compilable set; graph encodings; Protos Sept 2026) | T5/T6 (oracle/graal docs, graalvm.org) | `[VERIFIED]` |
| Narrow optimization wins; gc+PGO bar; Valhalla non-actionable | T6 (GraalVM 25.3 notes; JEP 401 status) | `[PROPOSAL]`; numbers `[SPECULATIVE]` |
| JEP 491 pinning removal; JEP 454 Panama; no io_uring JEP; isolates↔momo | T7 (JEPs; GraalVM embedding docs) | `[VERIFIED]`; gaps `[UNVERIFIED]` |
| 20–40 PY + 2–4 PY/yr; phase ranges; kill criteria | T8 (calibrated, LOW confidence) | `[ESTIMATE]` |
| Bounded upgrade: P95 ≤200 agent-hours | T2 (calibrated on 1.26/1.27) | `[PROPOSAL]` |

Scope note: every negative verdict above is scoped to the tested/observed configuration named in
its track note (per the standing rule). `[UNVERIFIED]`/`[SPECULATIVE]` items name their
falsifying experiment in the track notes; none are presented as measured.
