# Track 2 — Wave-3 continuous re-sweep (W3)

**Wave:** 3 · **Worker:** W3 (T2) · **Date:** 2026-10-04
**Mission:** re-sweep from SURVEY.md §6 wave-3 queue item 4 —
(1) Graal changelog since ~wave 1; (2) Leyden JEP 544 status;
(3) Valhalla JEP 401 status; (4) TruffleRuby/GraalPy (project/changelog level,
W4 does source passes); (5) Crema/Terminus re-check.
**Depth:** project-level / changelog-level only; no source passes (W4's lane).
All vendor/project numbers below are source claims, never our measurements.

---

## 1. Graal changelog re-sweep (2026-10-04)

Source: `oracle/graal` `compiler/CHANGELOG.md` (verified via 3 forks, latest
crawl 3 days ago) + Oracle GraalVM 25.0.3/25.0.4 release notes. Wave-1 wave-1
already covered GR-77137 (priority inlining), GR-28213 (loop vectorization),
GR-79526 (lock coarsening). New since:

### 1a. GraalVM 25.4 (internal 25.4.4.1.1) — compiler/CHANGELOG.md
- **GR-79029:** `PullThroughPhiPhase` + `DuplicationPhase` added to the
  *community* compiler configuration, enabled by default
  (`-Djdk.graal.OptPullThroughPhi=false`, `-Djdk.graal.OptDuplication=false`).
- **GR-78871:** extended **read elimination** — now handles indexed array
  accesses with non-constant indices, reads from newly allocated arrays
  containing default values, and array clone operations; can also run *after*
  reads are fixed in the low tier.
- **GR-78795:** extended **`OptimizeDivPhase`** with magic-number optimizations
  for **unsigned** integer division and remainder by *constant* values.

### 1b. GraalVM 25.0.3 (2026-04-21, CPU) — Oracle release notes
- Compiler: *"Improved inlining by notifying the inliner when a call target is
  specialized."* (specialized Truffle/JIT call-target → inliner re-evaluates;
  a feedback edge from specialization back into the inlining decision).
- Native Image: *"Optimized the allocation slow path"*; *"Added options to
  disable aggressive PGO optimizations for cold code."*

### 1c. GraalVM 25.0.4 — Oracle release notes
- Compiler: minor fixes (`x < x + a` simplification, integer compare/shift/
  method-handle/indexed-store edge cases).
- **GC ergonomics (allocation fast paths):** *"Improved `CompactingOldGen`
  and the `Adaptive2` policy. These remain disabled by default in this
  release."* Disabled-by-default — nothing to port, but it confirms the
  industry is still iterating on GC policies around the allocation path.
- No new speculation, inlining, or EA policy items this round.

### 1d. Older-line EA note (Oct 2025 CPUs, worth one line)
- *"Improved partial escape analysis to handle excessive fixpoint iterations
  more efficiently"* (21.0.9 / 17.0.17). Compile-*time* scalability fix, not a
  capability change — filed as negative (nothing to transfer).

### Findings → survey disposition
- **§3 complementary angle — SURV-009 (value-profile-driven arithmetic
  specialization):** Graal just shipped the *static* version of SURV-009's
  core transform for the unsigned domain (GR-78795, magic-number
  div/mod-by-constant in `OptimizeDivPhase`). This (a) validates the
  transform's real-world value — Graal's maintainers keep investing in it,
  (b) fixes the *unsigned* sub-case, which SURV-009's writeup should
  explicitly include, and (c) sharpens our edge: the frontier is no longer
  "magic-number division" per se but the **profile-constant** (not
  compile-time-constant) divisor behind a guard+deopt — territory Graal
  cannot reach in a static AOT/JIT-without-deopt setting.
- **§3 complementary angle — SURV-006 (priority inlining + path duplication):**
  two adjacent signals. (1) Graal 25.0.3 now feeds call-target specialization
  *back into* the inliner — a feedback edge our adaptive-inlining policy
  design should copy deliberately (speculation decision → re-run inlining
  budget assessment, not just guard-arm). (2) `DuplicationPhase` (GR-79029)
  graduating to community-default status reinforces SURV-006's path-duplication
  leg; duplication to expose optimization opportunities is becoming
  table-stakes in Graal, so our differentiation must sit in *what the
  duplicates enable* (guarded specialization), not in duplication itself.
- **§3 complementary angle — read elimination (SURV-relevant, no ID):**
  GR-78871 extends load elimination to non-constant-index array reads and
  default-valued new arrays. gc does GVN/FwdStore fine, but our
  always-on profiler (E4) gives *value-range* facts — the transferable
  lesson is that the industry keeps widening the elimination frontier;
  nothing new proposed here, recorded so the next sweep doesn't re-flag it.
- **Negative (1d):** PEA fixpoint-efficiency change is compile-time
  scalability, not a new capability — no candidate.
- **Negative:** no new escape-analysis capability, no new speculation policy,
  no inlining-policy change beyond the feedback edge, in Graal since wave 1.

---

## 2. Leyden JEP 544 status watch (2026-10-04)

### What moved
- **JEP number confirmed, status pinned:** per JVM Weekly vol. 192 (Sept 2026,
  17 days ago) and InfoWorld (10 days ago), **JEP 544 "Ahead-of-Time Code
  Compilation"** (from Project Leyden, owner John Rose) is now a **JDK 28
  candidate** (JDK 28 due March 2027) — i.e. "Candidate", not yet
  Proposed-to-Target. The Leyden project page (openjdk.org/projects/leyden)
  now lists it under "In progress" as "TBD: Ahead-of-Time Code Compilation".
  The draft number behind it is **JDK-8335368**.
- **Prototypes ship in Leyden premain EA builds today:** Shipilev's Leyden
  premain build README documents the experimental flags — AOT code caching is
  **enabled by default** when creating an AOT cache, disabled via
  `-XX:-AOTCodeCaching` (alongside `-XX:-ArchiveDynamicProxies`,
  `-XX:-ArchiveReflectionData`, `-XX:-ArchiveLoaderLookupCache`).
- **Claimed performance (project claims, not our measurements):** LavX News
  (Sept 2026, reporting the proposal) — AOT code cuts startup 65–80% in
  microservice-like environments vs 50–70% from the AOT cache without code;
  `javac` initial-iteration time −75%. AOT code coexists with JIT and is
  replaced via deopt/reopt on workload drift.
- **Three open exploration pieces** (per Inside Java newscast, Jan 2026 —
  the project's own framing of what comes next):
  1. **Portability vs micro-arch specialization** — AOT code is bound to the
     training micro-architecture (and to GC write barriers); aggressive
     optimization vs portable-but-weaker code is an explicit developer choice.
  2. **Iterative training** — an AOT cache can act as *input to a second
     training run* (framework trains a base cache, app extends it).
  3. **Inspectability of training data** — the training-run → assembly
     information flow is opaque; they are retiring the ASCII dump that
     drives the assembly phase and want better tooling.

### Findings → survey disposition
- **§3 complementary angle — SURV-014 (cross-run speculation-profile cache):**
  Leyden's *iterative training* is the exact precedent for a **layered**
  cross-run cache (framework base cache + app delta cache, version-keyed per
  layer). Recommend SURV-014's writeup gain a "layered training" leg:
  base-layer cache from CI/nightly training runs + app-layer deltas from
  production runs, composable rather than monolithic. Leyden's portability
  tradeoff (item 1) is also directly the tradeoff SURV-014 §"Open risks"
  already flags — the project's choice to make it *developer-selectable*
  suggests we should too (a conservative/per-portable vs aggressive mode
  for the cache consumer), rather than fixing one policy.
- **§4 infra angle — "training-data inspectability":** Leyden's item 3 maps
  to a design requirement for SURV-014: the cross-run cache must ship with
  a dump/inspect tool (what was cached, from which run, which decisions
  were invalidated and why). Without it, a misbehaving speculation cache
  is undebuggable. This is enabling infrastructure, not a ranked candidate.
- **Bucket status:** JEP 544 remains **THINKING** (Candidate for JDK 28, not
  shipped; prototypes in Leyden premain EA). Number did not change (still 544).
- **Negative:** no successor number appeared; no condensers API shipped that
  would change the cross-run-profile story beyond what SURV-014 already
  covers. The AOT *cache format* still stores classes/profiles/objects (JEPs
  483/514/515/516, ABLE), not code.

---

## 3. Valhalla JEP 401 status watch (2026-10-04)

### What moved
- **Integrated into OpenJDK mainline, targeting JDK 28 (March 2027), preview,
  disabled by default.** Dan Smith (valhalla-dev, Aug 4 2026): the `lworld`
  branch is closed; integration "perturbs a very large amount of code"
  (197,000 lines across 1,816 files — DEV.to/JavaCodeGeeks). JEP 401 is now
  titled **"Value Objects (Preview)"**; JDK 28 EA builds at jdk.java.net/28
  include JEP 401 + **JEP 539 Strict Field Initialization in the JVM**.
- **Model clarification (relevant to SURV-022):** value classes give up
  *identity*, not nullability — `value class Point { int x, y; }` can still
  be null; non-nullable value types are a separate future JEP. Synchronizing
  on a value object throws `IdentityException`; `==` becomes substitutability.
- **Early performance report (community measurement, not Oracle's):**
  Ivan Ponomarev measured `System.arraycopy` **8–20× slower than a plain
  loop on null-restricted flat value arrays** (via jvm-weekly vol. 192).
  The flattening machinery is not yet performance-clean — intrinsics
  haven't caught up with the new layouts.

### Findings → survey disposition
- **§3 complementary angle — SURV-022 (identity-freedom analysis → header-free
  flattened values):** two transfers. (1) The arraycopy regression is a
  *failure lesson for flattening*: flattening a layout without updating every
  bulk operation that touches it produces cliffs. Our identity-freedom
  analysis must enumerate all bulk primitives (memmove/memclr, GC copy
  paths, runtime write barriers) as part of the flattening transform, or
  prototype-gate SURV-022 behind a "no-bulk-path regression" bar. Recommend
  this as an explicit open risk on SURV-022. (2) JEP 539 (strict field
  initialization) is a language-level mechanism for "fields protected from
  reads before initialization" — our analog (E1 descriptor-driven) gets
  zero-init elision *for free* when identity-freedom + definite-assignment
  are proven; worth one line in SURV-022's transfer note.
- **Bucket status:** JEP 401 remains **THINKING** (preview, JDK 28, disabled
  by default). Null-restricted/flat *storage* is still a later JEP —
  identity-freedom alone (what 401 delivers) is exactly the property
  SURV-022's analysis proves; representation freedom is where we lead.
- **Negative:** no primitive-classes / unified-primitives movement in 2026;
  nothing new on the identity-freedom proof machinery itself.

---

## 4. TruffleRuby / GraalPy (project-level, 2026-10-04)

Scope note: W4 covers source passes; this is changelog/release-notes level.

### TruffleRuby
- Oracle GraalVM 25.0.3 / 25.0.4 release notes (19 days ago): **no
  TruffleRuby-specific optimization entries** — only shared-framework fixes
  (Truffle: JNI local-ref leak in polyglot-isolate interop/OSR/guest-object
  migration; `HotSpotSpeculationLog` consistency fix; TRegex resource limits).
  The 17.x/21.x maintenance lines are bugfix-only for languages (faulty
  assertion in Truffle call-target splitting; SafepointNode-as-guard).
- No 2026 announcement of new JIT/speculation features in TruffleRuby at
  project level; its story remains "inherits Graal core compiler advances"
  (which are the §1 items above).
- Context (not a finding): CRuby 4.0's ZJIT (method-based, SSA IR, goal
  production-ready and faster than YJIT in 4.1) is the active Ruby JIT story
  in 2026 — but ZJIT belongs to track 3's wave-2 queue, not T2; not duplicated
  here.

### GraalPy
- Oracle release notes 25.x: **compatibility/platform only** — Python 3.12.8
  stdlib, fully-native C-API execution by default (`python.NativeModules`,
  host toolchain), Windows build support, Arrow zero-copy sharing. The one
  performance-methodology note: reported speedups now use the community
  `pyperformance` suite (~4× geomean over CPython, vendor claim).
- GraalPy CHANGELOG.md crawls are stale (still showing 22.3.0 in mirrors);
  no new speculation/JIT features found at project level.

### Disposition
- **Negative (first-class):** at changelog/release-notes level, neither
  TruffleRuby nor GraalPy added optimization-relevant JIT/speculation
  features in recent weeks — 2026 language work is compatibility, native
  interop, and platform support. Any new speculation ideas from these
  projects live below changelog granularity → W4's source passes.
- **One §4-adjacent observation:** GraalPy's switch to the community
  `pyperformance` suite as its reporting basis is a methodology lesson for
  our own benchmark discipline (community suites, reproducible, geomean) —
  relevant to the "beats stock gc **with PGO** on a named benchmark" rule;
  not a candidate.

---

## 5. Crema / Terminus re-check (2026-10-04)

**Wave-2 verdict is overturned: both now have verifiable public records.**

### Crema — VERIFIED public record
- Public tracking issue: **oracle/graal#11327 "Project Crema: Open World
  for Native Image"** (open). TL;DR: lift Native Image's closed-world
  assumption by adding a Java interpreter to the application layer; load,
  link, and execute dynamically-loaded classes at run time; build on Native
  Image Layers (#7626) and the JDWP debugger (#5648).
- Explicitly stated future direction (issue text): *"In future releases
  Crema can also potentially provide a JIT compiler for the dynamically
  loaded bytecodes."*
- Onboarding doc in-tree: `substratevm/docs/crema-onboarding.md` (crawled 3
  days ago). **Current boundaries** (documented, verbatim): no parallel class
  loading, no JNI support for runtime-loaded classes, no `condy`, limited
  reflection for runtime-loaded classes; several metadata paths still under
  construction (`ensureLinked`, enclosing-method support, raw parameter
  annotations).
- Active bug traffic proves it's live, not a stub: #13783 (interpret
  non-AOT methods in partial types), #13913, #14075 (Crema verifier NPE on
  class/interface merge), #14169 (segfault on mixed-width primitive statics,
  60 days ago), plus a working external user (borkdude/cream — Clojure on
  Crema).
- **Bucket correction:** Crema moves from "no verifiable public record" to
  **THINKING** (active development, not shipped; runtime class loading in
  Native Image is the delivered-so-far scope, JIT-for-dynamic-bytecode is
  the stated future).

### Terminus — VERIFIED public record
- Public docs in-tree: `substratevm/docs/project-terminus.md`
  ("make Native Image self-hosted — build a native executable of itself, no
  host JVM"), `terminus-terminology.md`, `terminus-migration.md` (crawled 3
  days ago). Public tracking issue **oracle/graal#12236** ("Project Terminus:
  Self-hosting Native Image"), reference epic **GR-66203**.
- Terminus migration is actively consuming the codebase: #13166
  "[GR-73223] Migrate JDKInitializationFeature to Terminus"; the migration
  guide defines Terminus-safe replacements
  (`RuntimeReflection`→`JVMCIRuntimeReflection`, `RuntimeJNIAccess`→
  `JVMCIJNIAccess`, etc.). A JVMCI-maintainer comment (#12230) states the
  end-state: *"Once Terminus is done, there will most likely only be an SVM
  and Espresso implementation of JVMCI and this JVMCI will live entirely
  in the Graal repo."*
- **Bucket correction:** Terminus moves from "no verifiable public record"
  to **THINKING** (active, in-tree docs + migration in progress; goal is
  builder self-hosting, an architecture/infra effort rather than a runtime
  optimization).

### Disposition
- Neither Crema nor Terminus yields a new ranked candidate. Crema's open-world
  AOT+interpreter(+future JIT) is a **§3 complementary angle on SURV-001**
  (closed-world whole-program AOT): it defines the open-world fallback
  ladder (AOT → interpreter → future JIT) that our hybrid mode (E5) should
  mirror for dynamic-loading edge cases. Terminus is architecture hygiene
  with no optimization transfer — recorded so future waves don't re-flag it.
- Standing note for the queue: the "Crema/Terminus re-check" item can be
  retired from the wave-3 queue — both verified 2026-10-04.

---

## Raw candidates (bar-checked)

| ID (raw) | Idea | Bar check | Bucket | Disposition |
|---|---|---|---|---|
| RC-W3-1 | Inliner feedback edge: re-run inlining budget when a call target specializes (after Graal 25.0.3) | (a) semantics-preserving; (b) plausible on polymorphic-call-heavy benches (gc+PGO static budgets can't react); (c) E3+E4; (d) not in backlog, gc doesn't do it | WOULD-LIKE (precedent ABLE — Graal 25.0.3 release notes) | **Raw candidate** — narrow but cheap; coordinator to vet vs SURV-006 overlap |
| RC-W3-2 | Layered cross-run training (framework base cache + app delta cache, à la Leyden iterative training) | This is a *leg* of SURV-014, not standalone — (d) fails as independent candidate | — | Fold into SURV-014 §3 angle, not a candidate |
| RC-W3-3 | Crema-style open-world fallback ladder for SURV-001 (AOT → interpreter → JIT on dynamic load) | (b) benchmark unclear — fallback path, not a beat-gc story; pure-compat work | — | Near-miss: file as §3 angle on SURV-001 (done above), not a candidate |
| RC-W3-4 | Deopt-safe virtual-thread/goroutine frame-thaw protocol (from JDK-8377715: thawing a vthread frame could undo a deopt, fixed JDK 27 April 2026) | (b) fails — correctness edge case, no beat-gc benchmark; (a) relevant to E2/E3 interplay | — | **Negative**: correctness note for §4 deopt discipline, not a candidate |

**Net new raw candidates for synthesis: RC-W3-1 only** (narrow; flagged with
its SURV-006 overlap question). Everything else from this sweep is
complementary angles, bucket corrections, or negatives.

---

## First-class negatives (this sweep)

1. Graal 25.0.3–25.0.4: no new escape-analysis capability (PEA change was
   fixpoint-efficiency only), no new speculation policy.
2. Leyden: no successor JEP number for 544; no condenser/AOT-cache API
   shipped beyond SURV-014's existing coverage.
3. Valhalla: no 2026 movement on primitive classes / unified primitives /
   parametric JVM beyond JEP 401+539 integration.
4. TruffleRuby/GraalPy: no project-level JIT/speculation features in 2026 —
   language work is compat + native interop + platform.
5. Crema/Terminus "unverifiable" verdict: **retired** — both verified with
   public records dated 2026-10-04 (see §5).

---

## Sources (all accessed 2026-10-04)

- `oracle/graal` `compiler/CHANGELOG.md` (mirrors: middas-0706/graal,
  connectionmaster/graal; crawled ≤3 days ago) — GraalVM 25.4 section.
- Oracle GraalVM release notes: 25.0.3 (2026-04-21), 25.0.4, 17.0.19, 21.0.11
  (docs.oracle.com, crawled ≤3 days ago).
- openjdk.org/projects/leyden (project page: JEP 544 "In progress / TBD").
- jvm-weekly.com vol. 192 (Sept 2026) — JEP 544 JDK-28 candidate; JEP 401
  mainline; Ponomarev arraycopy report; JDK-8377715 fix.
- infoworld.com "Java 28 starts to take shape" (10 days ago) — JEP 544
  proposed for JDK 28.
- news.lavx.hu "JEP 544 Brings Ahead-of-Time Code Compilation to Java
  HotSpot" (24 days ago) — proposal claims (65–80% startup).
- builds.shipilev.net Leyden premain README — experimental flags
  (`-XX:-AOTCodeCaching` etc.).
- nipafx.dev Inside Java newscast 104 notes (Jan 2026) — Leyden exploration
  pieces (portability, iterative training, inspectability).
- openjdk/valhalla-docs `site/_index.md` (Aug 2026) — JEP 401/539 integrated,
  JDK 28 target.
- github.com/oracle/graal/issues/11327 (Crema tracking),
  substratevm/docs/crema-onboarding.md (boundaries),
  issues #13783/#13913/#14075/#14169 (active Crema bugs),
  substratevm/docs/project-terminus.md, terminus-migration.md,
  issues #12236 (Terminus tracking), #12230 (JVMCI end-state comment).
- Note: direct fetches of github.com/oracle/graal/issues/11327 and
  openjdk.org/jeps/544 failed (upstream timeout / 403); all claims above are
  from search-indexed content and mirrored changelogs, cited as such.
