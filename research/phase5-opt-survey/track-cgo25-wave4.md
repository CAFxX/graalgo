# CGO'25 Paper Pair — Wave-4 Track Notes (queue item 2, T2/T5 lane)

**Worker:** wave-4 track worker. **Date:** 2026-10-04. **Mission:** mine two CGO'25
papers spotted by the T1 wave-3 forward-citation sweep for angles relevant to a
GraalVM/Truffle-based AOT+JIT optimizing compiler for Go. NO SURV- IDs (coordinator
assigns at synthesis from 075+); raw candidates marked raw/vetted; [SURVEY.md](SURVEY.md) and
[CHECKPOINT.md](CHECKPOINT.md) are NOT edited here.

**Spot (from [track-1-wave3-citations.md](track-1-wave3-citations.md) sweep stats):** "GraalNN: Context-Sensitive
Static Profiling with Graph Neural Networks" (Milikic et al., CGO'25) and "Improving
Native-Image Startup Performance" (Basso et al., CGO'25). Both spotted titles were
accurate — no "spot was wrong" negative to record.

**Candidate bar:** every claim traces to a primary source with title/venue/year/authors
or URL + fetch date (2026-10-04); recalled numbers are unreliable — verify; never
present computed/modeled numbers as measured; failed verifications go in as
first-class negatives with the reason.

---

## P1 — GraalNN: Context-Sensitive Static Profiling with GNNs (VERIFIED) [vetted]

**Citation.** Milikic, L., Cugurovic, M., Jovanovic, V. (all Oracle Labs),
"GraalNN: Context-Sensitive Static Profiling with Graph Neural Networks",
*Proceedings of the 23rd ACM/IEEE International Symposium on Code Generation and
Optimization (CGO 2025)*, main conference, pp. 123–136. DOI: 10.1145/3696443.3708958.
Venue/authors confirmed on the official CGO 2025 program page
(https://2025.cgo.org/details/cgo-2025-papers/45/GraalNN-Context-Sensitive-Static-Profiling-with-Graph-Neural-Networks
— fetched 2026-10-04; session "ML Tools & Optimization", authors listed as
Lazar Milikic, Milan Cugurovic, Vojin Jovanovic, Oracle Labs); DOI form and page
range independently appear in a third-party bibliography (Victor Selegean ML
research report, fetched 2026-10-04). ACM DL abstract page returned HTTP 403 —
abstract quoted verbatim from the CGO program page instead. Full paper text not
accessible (paywalled); everything below beyond the abstract comes from official
GraalVM documentation (fetched 2026-10-04).

**Profiler design (from the CGO abstract, quoted):** "a Graph Neural Network
(GNN)-based static profiling framework that directly learns structural information
from control-flow graphs (CFGs). This reduces the reliance on handcrafted features
and minimizes the effort required for feature engineering while improving the model's
ability to predict profiles based on captured control-flow dependencies. GraalNN
follows a two-stage approach: it predicts context-insensitive profiles during the
parsing phase and refines them with context-sensitive profiles during the inlining
phase."

**What the GNN predicts (verified from official GraalVM docs, not the paper):**
control-split branch probabilities — i.e., static branch-taken frequencies at
control-flow splits. GraalVM build output labels this profile source "ML-inferred":
"An ML model is used to infer profiles for control split branches statically"
(oracle/graal BuildOutput.md, fetched 2026-10-04). It does NOT predict call counts
(that is a separate XGBoost-based static profiler, `-H:+MLCallCountProfileInference`,
classifying methods as cold/regular/hot — GraalVM for JDK 25 release notes, fetched
2026-10-04). Whether it predicts loop trip counts is unverified either way.

**How the profiles feed optimization decisions:** branch probabilities are the
currency driving block layout, inlining decisions, unrolling, and PEA. The
inlining-phase refinement is the key architectural point: when Graal does late
inlining, the same method's branches get *per-call-site* (context-sensitive)
profiles, so the inliner and downstream opts see different probabilities for the
same code inlined into different callers. (Detailed consumer list is inferred from
how branch profiles work in Graal generally — the paper abstract does not
enumerate consumers; marked as such.)

**Training-run-dependent or statically generalizable?** Statically generalizable —
this is the load-bearing property. The model is trained once (by Oracle) and
*shipped with the compiler*; no per-program training run is needed. GraalVM for
JDK 24 docs: "`-O3` uses the Graal Neural Network (GraalNN) static profiler for
ML-powered profile inference" (docs.oracle.com, fetched 2026-10-04). GraalVM for
JDK 25 release notes: GNN-based control-split profile inference enabled by default
at `-O3` (`-H:+MLProfileInferenceUseGNNModel` opts in at `-O2`); two refined
variants (conservative-for-O2, aggressive-for-O3), "expected to deliver performance
improvements of 1–3% for both O2 and O3 optimization levels, as well as a binary
size reduction of over 1% in O2" (**vendor claims**, not measurements). If the user
supplies a real PGO profile (`--pgo`), ML inference is disabled automatically —
real profiles still dominate. The JDK 24 docs mark GraalNN/`-O3` as "Not available
in GraalVM Community Edition" — non-blocking for graalgo only because the program
pins Oracle GraalVM 25.0.4.1.1 LTS (per program decisions 2026-10-04).

**Evidence label (their measured numbers vs our needs).** Paper claims (their
measurements, not ours): 10.13% runtime speedup "on a wide range of industry-standard
benchmarks", "surpassing existing static profiling techniques by over 2.5%", binary
size increase limited to 2.94%, "outperforming all existing static profiling
solutions by at least 0.9%". Honest ambiguity: the abstract does not name the
benchmarks or state the baseline explicitly (presumably the prior static profiler
GraalSP / other static techniques — unverified). What we need that the paper does
not give: (a) benchmark names; (b) whether 10.13% holds on Go-shaped code — the
model was trained on Java workloads; Go idioms differ systematically (per-call
`if err != nil` error branches, defer/panic paths, channel select dispatch), so the
branch-prior distribution the GNN learned may shift; this is an unverified
generalization gap, not a verdict; (c) interaction with speculation — GraalNN feeds
*static* decisions; our speculative JIT candidates use *dynamic* profiles (E4), so
GraalNN's role for us is as the pure-AOT-mode profile source.

**Why it matters for graalgo.** This is the missing profile source for **E5
pure-AOT mode**: the always-on profiler (E4) only exists at runtime (JIT/hybrid);
a pure-AOT Go image has no dynamic profiles and cannot afford a training run per
deployment. GraalNN gives day-0 static branch profiles with no training run — and
generalizes to unseen programs by construction (it ships in the compiler). Direct
feeds: SURV-005 (PGO-driven code layout — ML-inferred profiles replace the missing
PGO in AOT), SURV-006 (inlining benefit simulation — context-sensitive per-site
profiles are exactly the input a call-graph simulator needs), SURV-010/SURV-013
(versioning/unswitching need branch probabilities to pick never-taken branches). It
is also the static-profile floor under SURV-014's cross-run speculation cache
(day-0 before the cache warms). Complementary to — not competing with — the Basso
startup paper: GraalNN is peak-throughput static profiling, Basso is
first-execution-order layout; both are AOT-build inputs. The crisp divider:
Basso is training-run-dependent (per-program instrumented run), GraalNN is
statically generalizable (trained once, ships with the compiler).

**Bucket.** Candidate: **WOULD-LIKE** — "adopt GraalNN's ML-inferred control-split
profiles as the pure-AOT-mode profile source for Go" (not built by us).
Precedent: **ABLE** — shipped in production GraalVM (O3 default in JDK 25; `-O3`
in JDK 24; refined O2/O3 variants + XGBoost call-count profiler in JDK 25 — all
per official Oracle docs fetched 2026-10-04). ABLE-on-ABLE adoption: we consume a
shipped feature through our existing substrate (Graal) rather than building a
profiler.
**Bar check.** (a) ✓ semantics-preserving (profiles only steer heuristics; wrong
predictions cost performance, never correctness — no guard needed, unlike the
speculative candidates). (b) plausible-vs-gc+PGO — *conditional*: the 10.13% claim
is vs static-profiling baselines on Java benchmarks; whether inferred profiles move
a Go-AOT image vs gc+PGO on a named Go benchmark is unverified (marked as the open
measurement). (c) ✓ E5 (pure-AOT mode is unusable without some profile source;
gc+PGO's advantage is partly that gc *gets* real profiles at build time — GraalNN
narrows that gap for free). (d) ✓ not in SURV-001…074/backlog (the survey assumed
the always-on profiler everywhere; the AOT profile *source* was implicit).
(e) gc has no static ML profiler ✓ (gc's static branch heuristics are hand rules;
gc's PGO needs a training run).
**Evidence honesty:** 10.13%/2.5%/2.94%/1–3% are all source claims; the paper's
benchmark set and baseline are unnamed in accessible sources; the Go-idiom
generalization gap is flagged, not hand-waved.

---

## P2 — "Improving Native-Image Startup Performance" (VERIFIED) [vetted]

**Citation.** Basso, M., Prokopec, A., Rosà, A., Binder, W., "Improving Native-Image
Startup Performance", *Proceedings of the 2025 IEEE/ACM International Symposium on
Code Generation and Optimization (CGO 2025)*. Venue confirmed by independent
third-party citation (Are-We-Fast-Yet README by Stefan Marr et al., fetched
2026-10-04) and the authors' title page. **DOI unknown** — the authors' PDF carries
a placeholder DOI (`10.1145/nnnnnnn.nnnnnnn`); the ACM DL listing was not located
(see Negatives). Authors' 13-pp PDF: https://www.dag.inf.usi.ch/wp-content/uploads/cgo25.pdf
(fetched 2026-10-04, full text read).

**Mechanism (from the paper).** Profile-guided binary reordering to cut page faults
at startup, in two parts:

1. **Code ordering (§4):** two strategies reordering compilation units (CUs) in the
   `.text` section by *first-execution order* from an instrumented profiling build:
   (a) *CU ordering* — trace CU entry events (root-method signature), dedupe
   preserving execution order, lay CUs out in that order; (b) *method ordering* —
   trace every method entry, dedupe preserving order. CU ordering outperformed
   method ordering (paper's measurements).
2. **Heap-snapshot ordering (§5):** three strategies reordering objects in
   `.svm_heap` (Native Image's pre-initialized heap snapshot — 40–60% of binary
   size per the paper) by *first-access order* from the profile trace. The hard
   problem: matching profile objects to build objects — object identities and heap
   contents are NOT persistent across Native Image builds (nondeterministic class
   initializers; PGO itself changes inlining/PEA). Three 64-bit ID schemes for
   matching "semantically same" objects: (a) *incremental ID* (encounter-order
   counter, per-type-segregated [type-id:32, seq:32]; fragile across builds);
   (b) *structural hash* (MurmurHash3 over recursively encoded type name + all
   fields/elements, MAX_DEPTH=2, primitives/strings hashed by value); (c) *heap
   path* (hash of the first heap-graph path root→object + heap-inclusion-reason
   string: static-field signature / constant-in-method signature / InternedString /
   DataSection / Resource). Heap-path was the most robust across compilations.
3. **Profiling methodology (§6):** a tracing profiler inside the Graal compiler at
   IR level — handcrafted IR nodes, thread-local buffers of executed path IDs
   (IR-level path profiling with path-cutting from their prior TOPLAS'23 work),
   event tracing not counting; records object IDs on field/array accesses; runs
   even during VM initialization (instrumentation calls do no heap access). A Java
   post-processing framework produces ordered CSVs consumed by the optimizing
   image build. Notably, they reject bytecode-level instrumentation because it
   "disrupt[s] the optimizations normally done by the compiler" and over-profiles
   (their ref [7]).

**Measured claims (the paper's; Are-We-Fast-Yet suite, 16-core Xeon Gold 6326,
caches dropped between runs, 10 images × 10 runs each, 95% CIs):**
- Code ordering: CU ordering reduces `.text` page faults 1.58× (SSD) / 1.66×
  (NFS); method ordering 1.52× / 1.63×. Max 1.66× on Mandelbrot/Towers (SSD),
  1.71× on Bounce/List (NFS).
- Heap ordering: structural hash / heap path ~1.40–1.41× `.svm_heap` page-fault
  reduction (SSD), 1.42× both (NFS); incremental ID 1.30× (both). Max 1.48×
  (Storage, SSD), 1.60× (List, NFS). Benchmarks touch on average only ~4% of
  objects in `.svm_heap` — the snapshot is mostly never touched at startup.
- Combined (cu + heap path): 1.65× total page-fault reduction (SSD), 1.68× (NFS);
  end-to-end execution-time speedup 1.59× (SSD), 1.58× (NFS). Code ordering alone
  1.26× both media; heap ordering alone 1.07–1.16×.
- **Negative within the paper:** heap ordering alone caused 0.97–0.98×
  *slowdowns* on the long-running Havlak benchmark — startup layout is
  anti-optimal for steady state.
- Related-work positioning worth noting: PH [43] and τ [42] function-reordering
  and SARSA [10] RL reordering target steady-state locality, not startup; HALO
  [48] is post-link heap-layout PGO; "to the best of our knowledge, no previous
  work attempts to reorder objects in binary files to reduce page faults" (§8.4).

**Why it matters for graalgo (Go-AOT startup).** The mechanism is language-
agnostic: reorder *binary sections by first-use order from a training run*. For a
Go AOT image, the `.svm_heap` analog is the read-only pre-initialized data segment
(package-level vars, interned strings, `go:linkname` tables, reflect/type metadata
— Go AOT binaries are likewise page-fault-dominated at cold start when served over
NFS/containers, the FaaS cold-start case). Execution-order code layout is the
startup-side dual of SURV-005's hot/cold splitting (steady-state side). The
heap-path ID trick (root-path + inclusion reason) is the transferable idea for any
build where the pre-init image contents aren't stable across builds — but Go AOT
builds are deterministic, so the simpler incremental mapping would suffice for us
(see Negatives). **Caveats:** needs a *representative training run* (standard PGO
staleness risk); pays only when cold pages dominate (dropped-caches SSD / NFS);
no evidence on Go or non-Java heaps. Per the survey's Phase-5 rule this is
*enabling infrastructure* (cold-start latency), not a peak-throughput candidate —
the paper itself shows the Havlak steady-state regress.

**Bucket.** Candidate: **WOULD-LIKE** — "profile-guided first-use-order binary
layout for Go-AOT images (code + pre-init data), training-run dependent"
(not built by us). Precedent: **THINKING** (published CGO'25 research; modified
Graal + Native Image prototype; not shipped — distinct from the shipped
GraalNN/GraalSP ML profilers, which are peak-throughput, not startup).
**Bar check.** (a) ✓ semantics-preserving (pure layout change). (b) plausible —
*conditional*: no named *Go* benchmark with numbers; the paper's 1.59× on AWFY is
the plausibility anchor only. (c) ✓ E5 (pure-AOT image builds; cross-run profile
cache sibling of SURV-014). (d) ✓ not in SURV-001…074 or backlog (SURV-005 is
steady-state code layout; this is first-execution-order layout + pre-init-data
ordering — different objective). (e) gc does nothing of the sort ✓.
**Evidence honesty:** all numbers are the paper's; DOI unverified; nothing here is
independently measured by us.

---

## Buckets (standing)

ABLE = shipped in production today. THINKING = roadmaps/RFCs/active proposals, not yet
shipped. ATTEMPTED = tried and abandoned/reverted, WITH the reason. WOULD-LIKE = wishlist.

## Negatives (queue item 2)

1. **Basso paper DOI unverifiable** — the authors' PDF carries placeholder DOI
   `10.1145/nnnnnnn.nnnnnnn`; ACM DL listing not located (ACM returned 403 on the
   GraalNN DOI too, so this is an access limitation, not evidence against
   publication). Venue (CGO 2025) confirmed via independent third-party citation
   (Are-We-Fast-Yet README). Record the DOI as unknown, do not invent it.
2. **Basso heap-ordering regressed the long-running Havlak benchmark (0.97–0.98×)** —
   startup-optimal layout is steady-state-pessimal. A startup-layout candidate must
   be gated to cold-start scenarios; never the default for long-running services.
3. **Basso's heap-path ID machinery is over-engineering for Go** — it exists because
   Native Image heap snapshots are nondeterministic across builds; Go AOT builds are
   deterministic, so simple encounter-order mapping suffices for the transfer.
4. **Basso needs a representative training run** — the mechanism is
   training-run-dependent by construction (instrumented build → trace → optimized
   build). For FaaS with stable inputs this is fine; for input-varying workloads the
   profile-staleness risk is unaddressed in the paper.
5. **GraalNN full text inaccessible** (ACM 403) — mechanism claims beyond the
   abstract + official docs are marked unverified, not asserted.
6. **GraalNN does not replace dynamic profiles** — it predicts control-split branch
   probabilities only; value ranges, trip counts, and call counts need other
   sources. It is not a substitute for the E4 always-on profiler in JIT/hybrid mode.
7. **GraalNN CE availability** — `-O3`/GraalNN marked "not available in GraalVM
   Community Edition" (JDK 24 docs); non-blocking only while graalgo pins Oracle
   GraalVM LTS.
8. **Startup candidates are enabling infrastructure, not Phase-5 peak-throughput
   candidates** — per the survey's Phase-5 rule, Basso-style startup work must beat
   gc+PGO on a *throughput* benchmark to promote; as written it is cold-start
   latency infrastructure (own it as such, cf. SURV-014's framing).

## Sweep stats (queue item 2)

- Searches run: 7 targeted searches; result entries triaged: ~40.
- Pages/PDFs fetched and read: 3 (CGO 2025 program page for GraalNN; Basso et al.
  full 13-pp PDF; Oracle GraalVM release-notes/docs pages for GraalNN shipping status).
- 1 fetch failed: ACM DL (HTTP 403) — abstract sourced from the official CGO
  program page instead.
- DOI for Basso et al. not locatable — recorded as unknown.
- Raw candidates: 2 (both vetted at the citation level; P1 mechanism detail
  limited by paywall).
- Spot verification: both spotted titles were accurate — no "spot was wrong"
  negative to record; the wave-3 spotter's titles match the actual papers.
