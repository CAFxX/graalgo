# Spike 06 — Sulong evaluation: verdict and scored comparison

**Verdict: COMMIT-DIRECT** — proceed with the direct Truffle AST route. Scoped: this verdict holds
for the configuration tested 2026-10-03 (Sulong as documented/shipped by GraalVM ≤25, Go→LLVM
frontends available today, no JVM on the test box). Sulong does not win the scored comparison
(6 vs 19), so the PHASES.md REPLAN trigger (Sulong wins AND direct-AST estimate doubles) is not met.

## 1. Scored comparison (1–5, higher = better for graalgo's goals)

| # | Criterion | Direct Truffle AST | Sulong route (Go→LLVM→Sulong) | Notes |
|---|-----------|-------------------|-------------------------------|-------|
| (a) | Build cost per Go release | **4** | **1** | Direct: frontend = go/parser+go/types (stable, same-repo APIs); per-release work is new-syntax nodes (mechanical) + Java runtime-library deltas. Sulong: no maintained full-language Go→LLVM frontend exists — per-release cost means tracking Go in a dead/dormant frontend AND fighting LLVM↔Sulong IR skew. |
| (b) | Version coupling (what breaks at N→N+1) | **4** | **1** | Direct: one external dependency (the Go distribution, tracked anyway); go/ast+go/types are backward-compatible. Sulong: three moving parts — Go language→LLVM frontend (unmaintained), LLVM IR version (frontend emits 22, Sulong reads ≤12.0.1 — broken TODAY), Sulong itself (Oracle-maintained; `llvm-community` Maven artifact deprecated → `llvm-native`, direction unclear). |
| (c) | Semantic mismatch burden | **4** | **1** | Direct: mismatches are known, bounded, and mitigated in T5 (safepoint yields, handle-table unsafe, documented deviations); JVM donates precise GC, threads, monitors, safepoints. Sulong: goroutines, precise GC, signals, stack growth, and panic unwinding each require reimplementation against a substrate that lacks the primitive (catalog §3). |
| (d) | Debuggability / observability | **4** | **2** | Direct: Truffle nodes carry go/ast positions → source-level debugging is natural; JFR/heapdump/VisualVM work in-image (T5 §7.5). Sulong: two lowering steps (Go→LLVM IR→Truffle); Go source positions must survive a third-party frontend's DWARF; debugging is LLVM-IR-level and C-oriented. |
| (e) | Implementation effort (less = higher) | **3** | **1** | Direct is still a huge project (scored honestly at 3, not 5 — see §4). Sulong is strictly larger: it contains the direct route's frontend work (a Go frontend is still written — it just emits LLVM IR) PLUS a full Go runtime port to Sulong's C-ABI world PLUS repairing the IR version gap. The "shortcut" framing is inverted. |
| | **Total** | **19** | **6** | |

## 2. What was actually run (commands, versions, results)

Environment: Linux x86_64, no JVM (`command -v java` → not found; `~/tools/graalvm-jdk-25` absent —
recorded as a **setup gap** per the task; no JDK was installed). Go 1.27.1 (`~/tools/go/bin/go`).
Sulong could therefore not be executed; the evaluation is empirical at the IR level + analytical
against primary sources. The verdict is overdetermined (see §5), so the setup gap does not weaken it:
even with a JVM, the emitted IR cannot be parsed by Sulong.

**Go→LLVM ground truth, verified 2026-10-03:**

| Tool | Status (verified) | Evidence |
|------|-------------------|----------|
| gollvm (go.googlesource.com/gollvm) | Dormant ~18 months. HEAD `605d1b6` committed 2025-04-07 ("update to LLVM mainline", opaque pointers); own README still says "early stage of development". No release, no maintenance. | Gerrit `+log/HEAD?format=JSON` |
| llgo, old (go-llvm/axw) | Dead; dropped from the LLVM monorepo (stuck at Go 1.5 era). | Phoronix report on removal |
| xgo-dev/llgo ("LLGo") | Active (release v1.0.6, 2026); Go 1.27, LLVM 22, **conservative bdwgc GC**; aimed at C/Python/JS interop, not gc-compatibility. Not installed (310 MB + native deps; one working frontend sufficed for the spike's question). | GitHub API + README |
| TinyGo 0.42 (Sept 2026) | Active; Go 1.27 support, LLVM 22. Language **subset** with its own runtime and divergent semantics (cooperative scheduler, limited reflect). | Release notes, CHANGELOG.md |

**Empirical run — Go program to LLVM IR with a currently-working tool: SUCCESS.**

- Downloaded `tinygo0.42.0.linux-amd64.tar.gz` (sha256 `b87688fa2e19cee7…`, 184,946,240 bytes) from
  `https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo0.42.0.linux-amd64.tar.gz`;
  `tinygo version` → `0.42.0 linux/amd64 (using go version <unknown> and LLVM version 22.1.4)`.
- `tinygo build -o hello.ll hello.go` (hello-world + `fmt.Println`) → **4,102,055 bytes** of LLVM IR.
  (`-o *.ll` emission confirmed in primary source: `builder/build.go:672` in v0.42.0.)
- `tinygo build -o goroutine.ll goroutine.go` (`go worker(ch)` + channel) → **4,186,088 bytes**.
- IR facts (all from the emitted files):
  - **9,020 uses of opaque `ptr`, 0 typed pointers** (`i8*` etc.): the IR is 100% opaque-pointer
    style, unparseable by any LLVM ≤14 toolchain. Sulong's documented ceiling is bitcode **4.0–12.0.1**
    (graalvm.org JDK 24 reference; corroborated by JDK 21.3/22.2 docs). **Hard version blocker,
    verified at the artifact level.**
  - `go worker(ch)` lowers to `call i32 @tinygo_task_start(i64 ptrtoint (ptr @"main.worker$gowrapper" …))` —
    an **external C runtime function**: goroutines are whatever the frontend's runtime implements in C.
  - **0 `landingpad`/`personality`** in either file: Go panic/unwind is not mapped to LLVM EH in the one
    working frontend; a custom unwinder travels with the runtime.
  - `#dbg_value` debug records (LLVM 19+-style), `uwtable(sync)`, `captures(...)` attributes — modern
    LLVM surface throughout.

## 3. Semantic mismatch catalog

### 3.1 Sulong route (Go → LLVM IR → Sulong)

1. **Goroutines — no M:N substrate.** LLVM IR defines no thread-spawn primitive
   ([PRIMARY] LLVM LangRef: "does not define any way to start parallel threads of execution or to
   register signal handlers"); Sulong's pthread support was a 2019 "partial support" research project
   (JKU thesis) — the current completeness is unverified and the trajectory is not obviously toward
   full pthreads. Go needs millions of cheap goroutines with work-stealing, stack growth, and
   preemption. Empirical §2 shows the frontend must therefore ship an entire scheduler as C/LLVM
   runtime code — the single hardest component of any Go implementation, rebuilt from zero on a
   partially-threaded substrate.
2. **Precise GC — absent.** Sulong's memory model substitutes malloc-family calls with managed Java
   allocations but provides **no collector** for the LLVM heap ([PRIMARY] ECOOP'16 Sulong paper,
   §3.1). Go requires precise GC with stack maps; Sulong does not support LLVM statepoints/stackmaps,
   so exact root enumeration of compiled frames is unimplementable without modifying Sulong itself.
   Fallbacks: conservative GC (changes Go semantics — false retention, finalizer timing) or a
   from-scratch precise GC wired through an uncooperative parser. Multi-year either way.
3. **Signals — absent.** The Go runtime needs signal handling (async preemption, SIGPROF, nil-deref
   recovery). LLVM IR has no signal-registration primitive (LangRef, same citation as 1); Sulong
   exposes none. Go's preemption and stack-guard machinery cannot be expressed.
4. **Stack growth — absent.** Go's growable stacks need guard-page/trap or stack-copy machinery;
   Sulong's `alloca` is one fixed Java object per frame.
5. **Panic/unwind — unmapped.** Go panics do not lower to LLVM `landingpad`/`personality` (0 occurrences
   in the emitted IR, §2). A custom unwinder must be built, and it must interoperate with Sulong's own
   exception model wherever Go calls into native code and back.
6. **Maps / interfaces / channels / select / defer** — all reimplementable in IR/C, but that is the
   *same* runtime-porting work as the direct route's Java runtime, minus every JVM service (GC,
   threads, monitors, safepoints) the direct route gets for free.
7. **cgo — the one genuine Sulong win.** Native interop is Sulong's home turf (Truffle NFI). Scored
   honestly: it is a boundary feature, not the core, and does not justify the architecture.

### 3.2 Direct Truffle AST route (from T5, rescored here)

Mismatches are known and bounded: Loom never preempts (mitigated by compiler-inserted back-edge
safepoint yields, T5 §3.3); stack depth bounded by `-Xss` vs gc's 1 GB (documented deviation);
`unsafe.Pointer` via handle table (documented semantic restriction); `plugin.Open` needs redesign;
`runtime.GOMAXPROCS` thread migration is an open doc-vs-migrate decision; cgo pins carriers (JFR-
auditable). The JVM donates: precise GC with zero handwritten barriers, virtual threads with
heap-growable stacks (JEP 444), unpinning monitors (JEP 491), and safepoint infrastructure.

## 4. Adversarial: direct-AST weaknesses, scored honestly

1. **A full Go frontend on Truffle is greenfield.** No Go-on-Truffle (or Go-on-Sulong — searched,
   none found) has ever been published. Calibrated yardstick (T1): TruffleRuby took roughly a decade
   of a dedicated multi-person team to reach full production compatibility. Go's static typing removes
   Truffle's hardest problem (type discovery) but its runtime (goroutines, precise GC, value
   semantics) is heavier than Ruby's. The (e)=3 score reflects this: the direct route is a large,
   real project, not a small one.
2. **Performance must reach gc-class on optimism, not on proof.** T5 §5's adversarial ledger stands:
   the honest PE margin is speculative interface devirtualization + escape analysis + inlining
   budget — real but narrower than dynamic-language headlines. "Beyond gc" is a Phase-0-spike-gated
   claim, not a premise.
3. **Per-release work is real, not zero.** New syntax → new node types (mechanical but nonzero);
   runtime/stdlib behavioral deltas must be ported each release (spike 3, version-tax, owns the
   measurement). The (a)=4/(b)=4 scores (not 5) reflect this residual tax.

None of these flip the verdict: they are bounded engineering problems on a substrate that provides
the hard primitives (GC, threads, safepoints) for free, versus unbounded reimplementation on a
substrate that lacks them.

## 5. The Sulong steelman (strongest case, then why it fails)

- *"Reuse an existing Go→LLVM frontend; skip writing a Go frontend."* Fails: no maintained
  full-language frontend exists. TinyGo is a subset (fails the drop-in requirement); xgo-dev/llgo is
  new, LLVM-22, and conservative-GC (semantic divergence); gollvm is dormant and was never complete.
- *"Target a stable IR; let Graal do the optimizing."* Fails twice: Sulong's accepted IR is frozen at
  ≤12.0.1 while every maintained frontend emits 22 (verified §2 — the route is broken today, not at
  some future N+1); and Graal optimizing C-shaped IR cannot recover the Go-level facts (interface
  method sets, escape info) that go/types hands the direct route for free.
- *"Pin the frontend to emit LLVM ≤12 IR."* Concedes the version race permanently: a frontend frozen
  on a 2021-era LLVM while the ecosystem moves to 22+ bit-rots against the "always tracks latest Go"
  requirement from the other direction — and the semantic blockers (§3.1) remain untouched.
- *"Sulong is maintained by Oracle; less code for us to own."* Weakened 2026-10: the LLVM runtime was
  unbundled from the GraalVM distribution into a standalone, and the `llvm-community` Maven artifact
  is **deprecated** ("Please use the 'llvm-native' Maven artifact instead", mvnrepository, v25.0.1).
  Building graalgo's decade-long future on a component Oracle is de-emphasizing is a strategic risk
  the direct route (which depends only on Truffle/Graal — Oracle's core) does not carry.

## 6. Verdict-rule check

PHASES.md: REPLAN iff (Sulong wins the scored comparison) AND (direct-AST estimate doubles).
Sulong scores 6 vs direct's 19 — **Sulong does not win**; the second condition is moot. The Sulong
route's effort is in any case strictly larger (it contains the direct route's frontend work, retargeted,
plus a full runtime port plus version-gap repair), so no estimate-doubling argument exists either.

**Binary verdict: COMMIT-DIRECT.** Proceed with the direct Truffle AST route (go/ast+go/types
frontend, Java value model, goroutine→virtual-thread runtime). The Sulong alternative is closed
unless (a) Sulong's bitcode support reaches current LLVM, (b) a maintained full-language Go→LLVM
frontend appears, and (c) Sulong gains precise-GC/threading/signal primitives — any one of which is
a re-test trigger, per the verdict-scope rule.

## 7. Open questions / follow-ups

1. xgo-dev/llgo was assessed from docs only (not installed — 310 MB + LLVM 22/bdwgc/libffi native
   deps; one working frontend sufficed). If a future round wants a second IR data point, `llgo` is
   the candidate; expect the same opaque-pointer verdict (LLVM 22).
2. Sulong's *current* pthread completeness was not re-verified beyond the 2019 thesis and release-note
   mentions; it does not affect the verdict (the IR version gap alone is dispositive) but would matter
   if trigger (a) above ever fires.
3. The `llvm-native` Maven artifact rename deserves one search in a later track to confirm whether
   Oracle is replacing Sulong's managed execution with a native LLVM path — relevant only as an
   early-warning input, not to this verdict.
