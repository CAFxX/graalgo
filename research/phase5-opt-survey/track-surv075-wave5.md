# SURV-075 follow-up — load-bearing measurement protocol (wave 5)

**Worker:** wave-5 track worker. **Date:** 2026-10-04. **Mission:** wave-5 queue
item 4 — (Q1) design the measurement protocol for the SURV-075 load-bearing
question (does a Java-trained branch-prior model generalize to Go idioms?);
(Q2) verdict on the JDK 25 XGBoost call-count profiler as a SURV-006 input —
extractable for Go AOT or not. NO SURV-IDs (coordinator assigns). [SURVEY.md](SURVEY.md),
[CHECKPOINT.md](CHECKPOINT.md), SPEC.md, PHASES.md, and the requirements registry are NOT
edited here.

**Evidence labels.** All GraalNN/GraalCC performance numbers below are
third-party/vendor claims, labeled as such. gc internals below are verified
against the local Go 1.27.1 toolchain source (`~/tools/go`, 2026-10-04); the
oracle/graal absence claims are verified against a sparse tree-listing of the
public repo at HEAD (2026-10-04).

**SURV-075 recap (from [SURVEY.md](SURVEY.md) §1, Tier 2, WOULD-LIKE).** Adopt GraalNN-style
ML-inferred control-split branch probabilities as the day-0 static profile
source for E5 pure-AOT Go images. Verified precedent: GraalNN (Milikic,
Cugurovic, Jovanovic, Oracle Labs, CGO'25, DOI 10.1145/3696443.3708958) — a
GNN that learns structural CFG information, two-stage (context-insensitive at
parse, context-sensitive per inlining site during inlining), trained once and
shipped with the compiler; predicts branch probabilities only (call counts go
to a separate model). Shipped: GraalVM for JDK 25, GNN inference default at
`-O3` (`-H:+MLProfileInferenceUseGNNModel` opts in at `-O2`); refined
conservative-O2 / aggressive-O3 variants. Vendor claims (their numbers, never
ours): paper — 10.13% runtime speedup "on industry-standard benchmarks",
+2.5% over existing static profiling, +2.94% binary size; docs.oracle.com —
~6% on Renaissance/DaCapo/DaCapo-con-Scala vs default Oracle GraalVM config,
1–2% binary size increase; JDK 25 release notes — refined models "expected"
1–3% at O2/O3 plus >1% binary size reduction at O2. Paper benchmarks unnamed
in accessible sources (ACM DL 403). **Not available in GraalVM Community
Edition** — non-blocking only because graalgo pins Oracle GraalVM 25.0.4.1.1
LTS. Open risk (the load-bearing one): the model was trained on Java
workloads; Go idioms (`if err != nil` per call, defer/panic, select dispatch,
type switches, bounds checks) shift the branch-prior distribution — unverified.

---

## §1 — Q1: Measurement protocol

### 1.1 What is being decided

Whether SURV-075's premise — *a Java-trained ML branch-prior model is a
useful day-0 profile source for Go AOT* — survives contact with Go-idiom
code. The protocol is staged so that a verdict is reachable **before** any
graalgo prototype exists (Stage 0/1), with a confirmation stage once E5 exists
(Stage 2). The survey's rule holds throughout: paper numbers never promote;
only measured prototypes promote.

### 1.2 Go benchmark corpus (with Go-idiom justification)

Selection principles: (a) represent the Go-idiom branch-shape distribution,
not just "fast code"; (b) pinned versions, fixed workloads, benchstat
discipline; (c) include an error-light, loop-heavy **contrast set** so the
protocol can show transfer *working* where it should (a protocol that can only
fail is rigged); (d) prefer harnesses the Go team itself maintains.

| # | Benchmark | Pinned source | Why it is in (Go-idiom representativeness) |
|---|-----------|---------------|----------------------------------------------|
| 1 | **etcd** | `golang.org/x/benchmarks` Sweet harness (pins `etcd-io/etcd` v3.6.0-alpha.0) | Canonical Go-server idiom: every RPC/storage op is `if err != nil`; raft tickers + watch multiplexing are select-heavy; defer-heavy cleanup. Highest error-branch density in the corpus. |
| 2 | **cockroachdb** | Sweet harness (pins `cockroachdb/cockroach` v25.2.0) | SQL/KV: type-switch dispatch over datum/row types, interface-heavy, error branches on every KV op. Stresses S4 (type switches) + S1. |
| 3 | **gVisor** | Sweet harness (pins `google/gvisor` release-20240513.0) | Syscall emulation: bounds-check-heavy memory copies, error returns on every syscall path. Stresses S2 (bounds checks) + S1. |
| 4 | **esbuild** | Sweet harness (pins `evanw/esbuild` v0.23.1) | Parser: token-switch dispatch chains, slice bounds checks, error-*light* (author keeps hot paths monomorphic). Contrast: where do hand heuristics already win? |
| 5 | **tile38** | Sweet harness (pins `tidwall/tile38` v1.29.1) | Geo server: map-heavy (nil-map/comma-ok shapes), network select loops. Stresses S3 + S5. |
| 6 | **build** (root macro-bench) | `golang.org/x/benchmarks` root (`build.go`) | Compiles a fixed tree with `cmd/compile` — the compiler workload; deep call chains, error branches in tooling code. |
| 7 | **http** (root macro-bench) | `golang.org/x/benchmarks` root (`http.go`) | `net/http` hello-world — SURV-005's own named workload; middleware layering, error branches. |
| 8 | **json** (root macro-bench) | `golang.org/x/benchmarks` root (`json.go`) | `encoding/json`: type-assertion/comma-ok heavy, interface dispatch. Stresses S3/S4. |
| 9 | **BenchmarkGames Go** | shootout entries (binary-trees, fannkuch-redux, n-body, spectral-norm, mandelbrot, regex-redux) | Error-light loop kernels — the "Java-transfer-likely" control set. If the surrogate model fails *here too*, the failure is not Go-idiom-specific (different kill trigger, §1.8 K4). |
| 10 | *(stretch)* kube-apiserver-ish / grpc-go benches | unpinned — harness later | The literal "kube-ish" workload; grpc-go adds protobuf dispatch + error branches. Stretch because harnessing cost is high; 1–9 suffice for the verdict. |

Corpus hygiene (non-negotiable): pin exact commits; fixed workload inputs;
`benchstat` over ≥10 samples; idle machine; interleave arm runs; record
`go version`, CPU, GOMAXPROCS/GOGC. Follow the survey's measurement-honesty
convention: raw logs archived with checksums (cf. AGENTS.md lesson on
evidence provenance).

### 1.3 Branch-shape taxonomy (per-shape breakdown — where generalization breaks)

The protocol classifies every conditional branch in the corpus into shapes,
because the verdict must say *where* the Java-trained prior breaks, not just
*whether*. Recognition rules are stated against **gc SSA** (implementable
today via `GOSSAFUNC` dumps / `-gcflags=-d=ssa/...`), so Stage 0 needs no new
compiler:

- **S1 error branches** — `if err != nil` after a call. Recognition: compare
  against nil of error-typed value (needs IR type info — use `ir.Node` type
  check before SSA lowering, or pattern-match OEQ-with-nil where one successor
  leads to return/panic). Expectation: deeply skewed not-taken (>99%).
  Java has exceptions, not error returns — Java-trained models never saw this
  shape at this density. **Highest generalization risk.**
- **S2 bounds checks** — implicit `OpIsInBounds`/`OpIsSliceInBounds`. Trivially
  recognizable; usually not-taken; gc already eliminates many (BCE). Java has
  them too — expect transfer.
- **S3 nil-map / comma-ok** — map access with `ok`, type assertion with `ok`.
  Go-idiom-dense; Java analog (null checks) exists but the comma-ok *shape* is
  Go-specific.
- **S4 type switches / interface dispatch** — multi-way dispatch chains from
  `switch v := x.(type)` (lowered to itab comparisons). Java has
  instanceof-chains — expect partial transfer; Go's density is higher.
- **S5 select/channel dispatch** — `runtime.selectgo` call + case dispatch.
  Often uniform-ish or workload-skewed; Java has no analog (closest: poll loops).
- **S6 defer/recover + panic paths** — `BlockDefer`, explicit panics.
  Near-zero taken. gc already marks `BlockExit` "very unlikely"
  (`likelyadjust.go`). Expect transfer (both languages agree: panics are cold).
- **S7 loop back-edges** — strongly taken. Both languages agree; gc's weak
  loop heuristic ("prefer to stay in loop") already nails these.
- **S8 general data-dependent** — everything else.

### 1.4 Ground-truth instrument (constructible TODAY)

**Branch-probability ground truth:** build each corpus benchmark with Go
coverage in count mode (`go test -cover` / `-coverprofile` on the benchmark
binaries) and derive per-branch taken probabilities from block execution
counts. For `if`-shaped branches the then/else block counts map directly;
for multi-way (S4/S5) use per-case block counts as an approximation.
Limitations (stated, not hidden): statement-block granularity, not true edge
counters; coverage instrumentation perturbs timing but the *relative*
probabilities are what calibration needs; gc has no plugin API for a sharper
custom SSA instrument — this is the best available without forking gc.

**gc+PGO ceiling (arm C):** standard gc PGO flow — build instrumented,
train on the fixed workload, rebuild with `-pgo`. Verified from gc source:
PGO feeds exactly two consumers (inlining budgets, interface devirtualization)
— it does **not** produce per-branch probabilities, so arm C is an
*end-to-end wall-clock ceiling only*, not a calibration reference.

**No-profile arm (arm B) instrument:** gc's hand-written static heuristics,
`cmd/compile/internal/ssa/likelyadjust.go` (verified 2026-10-04): an
unlikeliness rank `blDEFAULT(0) < blCALL(1) < blRET(2) < blEXIT(3)` propagated
backward from exits/calls, plus a weak loop-stay heuristic ("prefer not to
exit loops"); the source itself notes *"Calls. TODO not all calls are equal,
names give useful clues"* — name-based heuristics are explicitly **not**
implemented. `b.Likely` (BranchLikely/BranchUnlikely/BranchUnknown) is
consumed by backend block layout (`block.go`) — i.e., gc's no-profile
baseline is *structural only*: loop-nest + call-presence + exit-avoidance. No
error-branch specialization, no probability values. For calibration
comparison, map the three outcomes to probabilities by **fitting** (not
hand-picking) on a training split — the fair version of "hand-written static
heuristics".

### 1.5 The three arms

- **Arm A — ML-inferred (Java-trained model applied to Go IR features).**
  Two realizations, in stage order:
  - **A1 — surrogate study (Stage 1, constructible today with open
    tooling).** Train a fresh model on *Java* benchmarks' CFGs + measured
    branch probabilities (Java training set: Renaissance/DaCapo/DaCapo-Scala —
    the suites the GraalNN docs name for their ~6% claim, likely overlapping
    the paper's set), then apply it to *Go* CFGs (gc SSA) and score against
    the §1.4 ground truth. This tests exactly the load-bearing question —
    "does a Java-trained CFG-structural prior generalize to Go?" — without
    needing Oracle's proprietary weights. Model class is the protocol
    implementer's choice (GNN per the paper, or gradient-boosted trees on CFG
    features as a cheaper faithful proxy — the *transfer* question is
    model-class-agnostic at the prior level; document the choice).
  - **A2 — direct (Stage 2, gated on Phase-1 frontend + E5 pipeline).** Run
    Go through Oracle GraalVM 25 `-O3` with
    `-H:+MLProfileInferenceUseGNNModel` via the Truffle pipeline and read out
    inferred profiles. This is the confirmation, not the gate — the protocol
    must not be circular (the measurement cannot require E5 to exist before
    SURV-075 is decided).
- **Arm B — no-profile:** gc as-is (likelyadjust heuristics), with the
  fitted 3-value probability mapping (§1.4). Zero implementation cost.
- **Arm C — ceiling:** gc+PGO on the same fixed workload. Per Graal's own
  rule (auto-disables ML inference when `--pgo` is present — a rule graalgo
  should adopt), real profiles dominate by design; arm C is the ceiling, never
  the bar.

Fairness rules: identical machine/pinning/workload across arms; arm B's
probability mapping fitted on a disjoint split from the evaluation corpus
(no peeking); report per-benchmark, never pooled-only.

### 1.6 Metrics

- **M1 — calibration per shape.** Brier score and expected calibration error
  of predicted P(taken) vs §1.4 ground truth, reported per shape S1–S8 and
  pooled. (Arm C excluded — no branch probs.)
- **M2 — consumer-impact simulators** (no compiler build needed; take
  predicted + true probabilities as inputs):
  - *SURV-005 layout:* run a Pettis-Hansen-style greedy block placer on
    predicted probs; score = fall-through fraction evaluated under **true**
    probs (fraction of dynamic branches whose hot successor is fall-through).
    Reports the i-cache/layout cost of mispredicted priors.
  - *SURV-006 inlining simulation:* score = inlining-budget misallocation =
    bytes of budget spent on call sites predicted hot but actually cold
    (<1% of caller execution) + hot sites missed. (SURV-006 needs
    per-call-site context-sensitive hotness — note the A1 surrogate must
    emit per-site predictions, mirroring GraalNN's stage-2 refinement, or
    the SURV-006 simulation is vacuous.)
  - *SURV-010/013 never-taken picking:* precision/recall of the
    "never-taken" classification at threshold ε=1% predicted P(taken);
    cost metric = wrong-leg unswitch rate (branch predicted never-taken that
    is actually taken >5%).
- **M3 — end-to-end (Stage 2 only).** Wall-clock geomean over the corpus +
  binary size, arms A2 vs B vs C. Never the Stage-0/1 verdict input.

### 1.7 Stages and the honesty ledger

| Stage | Needs | Delivers | Cannot deliver |
|-------|-------|----------|----------------|
| **Stage 0** (days, gc only) | Corpus (§1.2), shape classifier (§1.3), coverage ground truth, arm-B dump (§1.4) | Go-idiom prior distributions per shape; gc-heuristic per-shape accuracy (the "hand-written" baseline, fully measured) | Anything about ML transfer |
| **Stage 1** (weeks, open tooling) | A1 surrogate: Java CFG extraction (Soot/WALA/javac-based) + Java branch-prob measurement + model training; Go CFG extraction (gc SSA) | **The load-bearing verdict input:** per-shape generalization gap (M1), consumer-impact gap (M2) | Oracle's actual model behavior; end-to-end wall clock |
| **Stage 2** (E5 exists) | Phase-1 Go frontend + Oracle GraalVM 25 Truffle pipeline | A2 direct confirmation; M3 end-to-end; GraalCC on/off evaluation (§2) | — |

Explicitly out of reach today: running Oracle's GraalNN weights on Go
(proprietary, and needs the E5/Truffle pipeline); end-to-end wall-clock of
ML-inferred Go AOT; any GraalCC-on-Go measurement.

### 1.8 Kill triggers and promotion bar

Thresholds are protocol-design choices, anchored to the paper's vendor claim
of +2.5% over static profiling on Java: the Go bar is set *below* it to price
in cross-language transfer loss, and every trigger names the shape-level
evidence required (no pooled-only kills).

- **K1 — calibration kill.** If A1's Brier score is worse than fitted arm B
  on **≥2 of the 5 Go-idiom shapes** (S1, S3, S4, S5, S6), the Java-trained
  prior does not generalize → SURV-075 stays WOULD-LIKE. Promotion then
  requires either Go-retraining evidence or a shape-aware prior override
  (e.g. hard-code error-branch prior 99/1) proposed and measured as a *new*
  item — not a silent patch to SURV-075.
- **K2 — consumer-impact kill.** If inferred profiles regress the M2
  composite (layout fall-through + inlining misallocation + wrong-leg
  unswitch, geomean) by **≥1% vs no-profile**, or lose **>2% on the layout
  metric** on the error-heavy members (etcd/cockroachdb class), the "day-0
  default" claim is killed → SURV-075 drops to THINKING (opt-in only) unless
  retrained.
- **K3 — systematic-shape smoking gun.** If error-branch (S1) mean absolute
  error is **>5pp** while ground-truth mean is **>99% not-taken** (i.e. the
  model systematically under-predicts the skew), distribution shift is proven
  → scope change: SURV-075 becomes "train our own Go GNN" with a different
  cost model; the Java-trained adoption is dead.
- **K4 — control-set kill (anti-rigging).** If A1 fails calibration even on
  the BenchmarkGames contrast set (S7-dominated, where Java priors should
  transfer), the failure is not Go-idiom-specific — the surrogate methodology
  itself is suspect (bad Java training set, broken feature extraction);
  fix the methodology, do not kill SURV-075 on that evidence.
- **Promotion bar.** A1 beats fitted arm B by **≥1.5% geomean on the M2
  composite** AND no shape class is worse by **>1pp** calibration AND
  (Stage 2) no corpus member regresses **>2%** end-to-end. Explicitly
  **never required:** beating gc+PGO — real profiles dominate by design
  (adopt Graal's auto-disable rule: ML inference off when real profiles are
  present).

---

## §2 — Q2: the JDK 25 XGBoost call-count profiler as a SURV-006 input — verdict: NOT extractable

### 2.1 What is publicly known (all fetched/verified 2026-10-04)

1. **Official docs** (graalvm.org JDK 25 release notes; identical text on
   docs.oracle.com): *"Added an XGBoost-based static profiler for call count
   profile inference in Native Image. The model classifies methods as either
   cold or regular/hot, guiding optimizations and reducing binary size with
   minimal impact on runtime performance. To enable it, use the option:
   `-H:+MLCallCountProfileInference`."* Followed by: *"Not available in
   GraalVM Community Edition."*
2. **Internal name "GraalCC"** (Oracle engineer Alina Yurtseva's GraalVM-25
   notes): *"We added another ML model for image size reduction. GraalCC
   predicts method execution and can significantly reduce executable size.
   Use `-H:+MLCallCountProfileInference`."* A companion demo notes *"The
   `size` profiles use it."*
3. **No paper, no feature documentation.** Unlike GraalNN (CGO'25 paper with
   architecture, two-stage design, CFG-structural learning), GraalCC has no
   publication and no public description of its inputs, feature engineering,
   model format, or training data.
4. **Absent from the public oracle/graal repo** (verified by sparse
   tree-listing at HEAD, 2026-10-04): zero filename hits for
   `MLCallCountProfileInference`, `GraalCC`, or `XGBoost`; no ML option
   declarations in `SubstrateOptions.java` or the hosted PGO utilities
   (`PGOUtils.java`); the public `BuildOutput.md` documents only the
   "ML-inferred" *label* as a possible PGO source. Implementation, feature
   extractor, and weights are Oracle-proprietary.

### 2.2 Verdict: NOT extractable — with the use/extract distinction

- **Not extractable** as a standalone artifact for Go AOT: no source, no
  weights, no feature spec, no model format are public. There is nothing to
  lift — unlike GraalNN, where at least the paper specifies the design well
  enough to reimplement the *idea* (which is what SURV-075 proposes).
- **Weakly usable, not extractable:** because graalgo pins Oracle GraalVM
  25.0.4.1.1 LTS, the `-H:+MLCallCountProfileInference` flag *could* be
  toggled in E5 builds and evaluated on/off — but (a) the feature extractor's
  expected input distribution (Java-method IR) is undocumented and the
  Go-via-Truffle IR distribution shift is unmeasured (the same generalization
  question as SURV-075, with even less public evidence), and (b) it is the
  wrong tool for the stated job (next paragraph).
- **Granularity mismatch with SURV-006 regardless of provenance.** SURV-006
  (priority inlining + path duplication) needs *per-call-site,
  context-sensitive* hotness to simulate inlining benefit over the call
  graph — the call-count analog of GraalNN's stage-2 refinement. GraalCC
  emits a *per-method binary* cold-vs-regular/hot classification, designed
  for size ("guiding optimizations and reducing binary size"), not for
  performance simulation. Even a perfectly extractable GraalCC would be too
  coarse to drive SURV-006's benefit simulator.
- **Recommendation:** do not pursue GraalCC as a SURV-006 input. If E5 needs
  static call-graph hotness, the honest options are (a) train our own
  method-hotness model on Go (new item, prototype-gated), (b) derive
  call-site hotness from the same branch-profile source SURV-075 provides
  (note the gap: branch probs ≠ call counts — a method can be hot with cold
  branches), or (c) rely on E4 profiler data in hybrid mode. GraalCC itself
  is at most a **size-flag to evaluate on/off** once E5 builds exist — a
  build-flag experiment, not a profile source.

---

## §3 — Open questions (for the coordinator / future waves)

1. **GraalNN's training set** is unnamed in accessible sources (paper
   paywalled; ACM DL 403). The docs' ~6% claim names Renaissance, DaCapo,
   DaCapo-con-Scala — plausible overlap, but Stage-1's Java training set
   selection needs confirmation (ACM DL access or author contact).
2. **Trip-count prediction** by GraalNN is unverified either way — affects
   SURV-010's consumer value, not the branch-prior question; park until
   Stage 2.
3. **Context-sensitive stage on Go:** does inlining-phase refinement trigger
   meaningfully for Go's typically-monomorphic call sites, or does Go mostly
   get stage-1 (context-insensitive) value? Stage-2 question; affects how
   much of the paper's +2.5%-over-static claim could even apply.
4. **Coverage-counter granularity** for multi-way branches (S4 select,
   S5 type-switch): block counts approximate case probabilities; validate
   against a sampled ground truth on one benchmark before trusting S4/S5
   calibration numbers (Stage-0 methodology risk).
5. **Surrogate model class** (GNN vs boosted trees on CFG features): the
   transfer question is prior-level and model-class-agnostic, but document
   the choice — a too-weak surrogate could false-trigger K4 rather than K1.
6. **GraalCC feature set** is permanently opaque unless Oracle publishes;
   treat as a black-box build flag, never as a profile-source dependency.
7. **LLVM-proxy arm** (llgo → LLVM static branch heuristics as a second
   no-profile reference): optional. xgo-dev/llgo is active (LLVM 22, Go 1.27
   syntax) but its 1:1-OS-thread goroutines distort select-heavy benchmarks;
   recommend only if Stage 1 wants a second no-profile reference point.

---

*Track notes: [track-surv075-wave5.md](track-surv075-wave5.md). NO SURV-IDs assigned. [SURVEY.md](SURVEY.md),
[CHECKPOINT.md](CHECKPOINT.md), SPEC.md, PHASES.md, requirements registry untouched per
brief.*
