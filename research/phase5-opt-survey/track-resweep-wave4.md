# Track 2/3/4 — Wave-4 re-sweep & trigger checks (W4)

**Wave:** 4 · **Worker:** T2/T3/T4 re-sweep · **Fetch date:** 2026-10-04
**Mission:** (A) T2 continuous re-sweep: Graal changelog since wave 3 (~2026-10-04
07:42 CEST); Leyden JEP 544 / Valhalla JEP 401 status (watch Proposed→Target);
TruffleRuby speculation options; GraalPy JIT-internal changelog. (B) T3 re-survey
trigger check: Meta new optimizing JIT; Pyston commits; Swift move-only SIL pass;
SE-0474; Sorbet compiler activity. (C) T4 watches: golang/go #24314, #77893 CL
status, #16798, #21536 (issue pages fetched, not recalled).
**Scope:** raw writeups only — no SURV.md/CHECKPOINT.md edits, no registry
heartbeats. New findings clearing the candidate bar enter as raw candidates
(coordinator assigns SURV- IDs from 075+); status-only changes are dated watch
notes. All vendor/project numbers are source claims, never our measurements.

---

## A. T2 continuous re-sweep

### A.1 Graal compiler changelog (fetched 2026-10-04)

**New since wave 3: GraalVM 25.5 (Internal 25.5.5) section in
`oracle/graal` `compiler/CHANGELOG.md`** (upstream file verified via
`browser.open`, commit 384c27b — upstream of the 25.4 ceiling wave 3
covered). Eleven entries, all default-on:

1. **GR-79881, GR-79883** — aggressive partial loop unrolling + simulation-based
   loop peeling in the community compiler (default on;
   `-Djdk.graal.AggressivePartialUnroll=false`).
2. **GR-79595** — `RangeCheckEliminationPhase`: rewrites eligible 64-bit range
   checks to equivalent 32-bit forms after counted strip mining.
3. **GR-79593** — `BreakChainedPhisPhase`: splits chained loop phi values in the
   low tier to improve register allocation.
4. **GR-79589** — partial redundancy elimination *scheduling* added to the
   community compiler.
5. **GR-79594** — write sinking: moves eligible field and constant-offset array
   writes out of loops (default on; `-Djdk.graal.OptWriteSinking=false`).
6. **GR-79588** — deduplication: shares equivalent statements/expressions from
   converging control-flow branches (code-size reduction).
7. **GR-79526** — extended lock elimination: coarsen locks across simple control
   flow + eliminate nested locking of the same object. *(Precursor GR-79526 was
   already "lock coarsening" in earlier waves; this extends it.)*
8. **GR-79587** — `GuardRangeGroupingPhase`: removes redundant integer range
   guards, retaining tightest lower/upper bounds for same-anchor guards.
9. **GR-79585** — replaces eligible loop-carried memory reads with value phis,
   letting invariant reads move out of loops (`-Djdk.graal.OptimizeLoopAccesses=false`).
10. **GR-79597** — early checkcast expansion: expose null and type checks as
    control flow for subsequent optimizations.
11. **GR-79590** — **speculative store checks**: removes array store checks by
    *speculating* that an array's declared type is exact
    (`-Djdk.graal.SpeculativeStoreCheck=false`). **This is a genuinely new
    Graal speculation capability since wave 3** — speculation over the
    declared-type-exact property.
12. **GR-79031** — strip mining for counted and non-counted loops, loop
    inversion, loop rotation, and **exact-arithmetic optimization**: replaces
    exact (checked) additions with normal additions, preserving overflow
    behavior via *loop-limit deoptimization* — a deopt-backed arithmetic
    speculation.

**Survey disposition:**
- **§3 complementary angle — SURV-023 (lock coarsening across simple control
  flow):** GR-79526 as-shipped now covers exactly SURV-023's stated scope
  ("coarsen locks across simple control flow") plus nested-lock elimination —
  Graal has converged on the design; SURV-023's remaining edge is the
  goroutine-mutex/gc-specific context (lock fast path, contention profiles),
  not the CFG shape. Record as convergence confirmation.
- **§3 complementary angle — SURV-008 (optimistic aliasing speculation):**
  GR-79594 (write sinking) and GR-79585 (loop-carried reads → value phis) are
  adjacent: Graal keeps investing in the load/store-elimination frontier;
  nothing to transfer beyond what SURV-008 covers.
- **GR-79590 — speculative store checks:** closest analog in the backlog is
  SURV-074 (speculative lock fast path, GraalPy precedent) as the *pattern*
  (guard a static declaration, specialize, deopt on violation). **Raw candidate
  RC-W4-1** — speculative declared-type-exact array-store check removal for Go
  slices (bar: (a) sound — deopt on exact-type violation; (b) benchmark:
  `[]T` write-heavy loops where the static element type is `any`-declared but
  monomorphic — Go's `any` slices are the exact analog of Java's
  `Object[]`-declared-exact arrays; (c) E3+E4; (d) not in backlog —
  SURV-019/SURV-028 touch interface representations but nothing speculates on
  slice declared-type exactness; (e) gc can't guard). Bucket: WOULD-LIKE
  (precedent ABLE — Graal 25.5 GR-79590, shipped).
- **GR-79031 exact-arithmetic deopt:** adjacent to SURV-009
  (value-profile-driven arithmetic specialization) — Graal's version uses
  *static* exactness (checked-add overflow provability) + loop-limit deopt;
  our edge remains the *profile-driven* variant. File as §3 angle on
  SURV-009, not standalone.
- **Negative:** still no new escape-analysis capability and no new inlining
  policy in Graal since wave 3; the speculation-policy story is now the two
  items above (GR-79590, GR-79031).

### A.2 Leyden JEP 544 / Valhalla JEP 401 status (fetched 2026-10-04)

- **JEP 544 ("Ahead-of-Time Code Compilation", JDK-8335368, owner John Rose):**
  **still Candidate for JDK 28 — no Proposed→Target transition.** Evidence:
  JVM Weekly vol. 192 (crawled 10h ago): "two new candidates from September,
  543 (Structured Concurrency, final) and 544 (Ahead-of-Time Code Compilation,
  from Leyden, owned by John Rose)". InfoWorld (10 days ago, "Java 28 starts
  to take shape"): "A sixth feature, ahead-of-time (AOT) code compilation,
  has been proposed for JDK 28" — *proposed*, not targeted. Watch item
  remains open; JEP number unchanged (544), JDK 28 due March 2027.
- **JEP 401 ("Value Objects (Preview)"):** integrated in mainline, targets
  JDK 28 preview, unchanged since wave 3 (28-ea+17 snapshot tested Sept 2026
  per ankurm.com; title remains "Value Objects (Preview)" — note: title
  changed from "Value Classes" framing in community coverage). No new
  movement on primitive classes / null-restricted value types. Remains
  THINKING.
- Both bucket statuses unchanged. Next re-sweep: watch for JEP 544
  Proposed→Target (JDK 28 ramp-down ~Dec 2026–Jan 2027).

### A.3 TruffleRuby — speculation options (trigger check, fetched 2026-10-04)

**Trigger NOT fired.** The T3 named trigger was "new options in `options.yml`
mentioning speculation/guards". No project-level announcement or 2026
release note (GraalVM 25.x notes, checked via Oracle release notes in wave 3)
mentions new TruffleRuby speculation/guard options; the general Ruby-JIT
narrative in 2026 is about MRI's ZJIT/YJIT, not TruffleRuby. Consistent with
the wave-3 T2 negative ("TruffleRuby/GraalPy: no project-level JIT/speculation
features in 2026") and the T3 wave-3 source pass (`options.yml` has no
guard/deopt knobs; only `INLINE_DEFAULT`, `YIELD_ALWAYS_INLINE`,
`METHODMISSING_ALWAYS_INLINE`).

### A.4 GraalPy — JIT-internal changelog (trigger check, fetched 2026-10-04)

**Trigger NOT fired.** Direct fetch of `oracle/graalpython` `CHANGELOG.md`
(current HEAD `178ea261`):
- **25.4:** remove BouncyCastle entirely (embedding dependency cleanup) — no
  JIT content.
- **25.3:** Python stdlib 3.13.14 + new 3.13 features — no JIT content.
- **25.2:** Windows standalone native OS backend — platform work, no JIT content.
- **25.1:** `-X jit=0|1|2` presets + launcher default `jit=1` (already recorded
  wave 3); string-literal interning; allocation reporting removed; intrinsified
  `_ctypes` removed — packaging/tooling/perf-methodology, not JIT internals.
- **25.0.0:** native C-API execution by default, host toolchain for extensions —
  interop work, no JIT internals.

No 2026 GraalPy changelog entry touches JIT internals (tiering thresholds,
specialization policy, deopt machinery). Graal 25.5's new speculation items
(A.1) are *compiler* changes GraalPy will inherit, not GraalPy-level work.

---

## B. T3 re-survey trigger check (fetched 2026-10-04)

Only re-survey on named triggers; this section records whether each trigger
fired, not new source passes.

1. **Meta announces a new optimizing JIT — NOT fired.** The only 2026 Meta
   compiler-adjacent stories are the *LLM Compiler* (an LLM trained on LLVM
   IR/assembly for code-size optimization research — 2024 vintage, not a
   runtime JIT) and Muse Code / Muse Spark 1.2/1.3 (terminal coding agents, not
   JITs). No new Meta runtime optimizing JIT announcement exists.
2. **Pyston commits resume — NOT fired.** Last commit remains 2024-08-12
   ("Fix file naming"); ecosyste.ms (synced 5 days ago): 0 commits in the past
   year, 0 committers; the repo now reads "(No longer maintained)". Confirms the
   §2 ATTEMPTED writeup; nothing to re-survey.
3. **Move-only SIL pass appears (Swift) — NOT fired.** 2026 Swift compiler
   activity around move-only is bug-fixing (MoveOnlyChecker crash reports,
   e.g. swiftlang/swift#89250) and ownership docs — no new
   optimization-exploitation pass keyed on move-only types found. Enforcement
   still lives in `Mandatory/` checkers only. The watch for SURV-024-class
   transfer material stays armed.
4. **SE-0474 ships — NOT fired.** Proposal page (updated 4 days ago): status
   remains **Accepted**, implementation "partially available on main behind
   the frontend flag `-enable-experimental-feature CoroutineAccessors`".
   Corroborating: a Swift forums rename pitch (191 days ago) notes "these
   features have not yet shipped in a stable Swift release". `_read`/`_modify`
   coroutine accessors remain experimental-gated; the trigger (proposal status
   change) has not occurred.
5. **TruffleRuby speculation options land — NOT fired** (covered under A.3).
6. **GraalPy JIT-internal changelog — NOT fired** (covered under A.4).

---

## C. T4 watches (golang/go — issue pages fetched 2026-10-04)

### C.1 #24314 — write-barrier omission on self-subslice

**Status: OPEN, unchanged.** API: state=open, comments=8, updated_at
2022-07-13, labels Performance/compiler/runtime. Issue page fetched in full —
thread is the 2018 discussion (Kirill / aclements race-safety objection /
josharian) with no substantive new comments since wave 1. **Watch note
(2026-10-04):** still open, no Go-team movement; the #15397-adjacent
barrier-elision negative in §5 stands.

### C.2 #77893 — CL status (SURV-041's gap)

**Status: none of the five CLs landed — SURV-041's gap does NOT narrow.**
Gerrit (`go-review.googlesource.com`, checked 2026-10-04): all five CLs are
status **NEW** (unmerged):

| CL | Subject | Owner (from thread) | Status |
|---|---|---|---|
| [758140](https://go.dev/cl/758140) | runtime: add starting stack size benchmark and add GODEBUG=gcstacksizetrace | thepudds | NEW (updated 2026-03-23) |
| [758141](https://go.dev/cl/758141) | runtime: exclude inactive goroutines from starting stack size heuristic | thepudds | NEW (updated 2026-03-30) |
| [764220](https://go.dev/cl/764220) | runtime: estimate starting stack sizes by calculating percentiles of scanned sizes | thepudds | NEW (updated 2026-04-08) |
| [793780](https://go.dev/cl/793780) | runtime: cache grown goroutine stacks to reduce stack-growth cost | geekswaroop | NEW (updated 2026-07-16) |
| [793920](https://go.dev/cl/793920) | runtime: add stack size watermark for initial stack size heuristic | thepudds | NEW (updated 2026-06-24) |

(Note: [SURVEY.md](SURVEY.md) §5/T4 wave-3 wrote "five 2026 CLs on #77893 are unmerged as
of 2026-10-04" — verified, still true.)

**New fleet data (watch note, 2026-09-27 — strengthens the precedent, does not
change the gap):** geekswaroop ran the *watermarking* CL (793920) in
production on 3 more services; reported copystack cost → ~0
(svc1: 4.12%→0%, selected start 64KB; svc2: 5%→0.4%, start 32KB) with
right-sized selection (P50/P75/P90/P95 table: e.g. svc3 mean 10.5KB,
max 23.5KB) and noted most traced goroutines die within one GC generation —
the `gdestroy` instrumentation captures peak watermarks for short-lived
goroutines, so data is not lost. Open question he flags: too-high starting
sizes may need later shrinkstack CPU. **Implication for SURV-041:** the
global-heuristic line is converging on *watermark*-based right-sizing, but it
remains **global** (not per-spawn-site) — none of the five CLs proposes
per-spawn-site histograms, so SURV-041's distinct edge (per-site sizing via
E4 profiler, no GC-team CL overlapping it) is intact. Bucket remains
THINKING. Next trigger: any of the five CLs → MERGED/ABANDONED.

### C.3 #16798 and #21536 — status re-check

- **#16798** (proposal: cmd/compile: tail call optimization for self-recursion
  only): **CLOSED** (state=closed, labels LanguageChange/v2/Proposal/
  FrozenDueToAge, updated 2019-01-16). Confirms the §5 negative recorded in T4
  wave 2 ("TCO for self-recursion — DECLINED"). No new data; watch retired.
- **#21536** (proposal: `//go:inline` directive): **CLOSED** (state=closed,
  labels Proposal/FrozenDueToAge, updated 2020-04-10). No movement; the
  wave-1 TBD is resolved as closed/frozen. Watch retired.

---

## Raw candidates (bar-checked, wave 4)

| ID (raw) | Idea | Bar check | Bucket | Disposition |
|---|---|---|---|---|
| RC-W4-1 | Speculative declared-type-exact array-store check removal for Go slices, after Graal GR-79590 | (a) sound — deopt on exact-type violation; (b) benchmark: `[]T` write-heavy loops with `any`-declared-but-monomorphic slices; (c) E3+E4; (d) not in SURV-001…074; (e) gc can't guard | WOULD-LIKE (precedent ABLE — Graal 25.5, shipped) | **Raw candidate** — coordinator to vet vs SURV-074 pattern overlap (same "guard a declaration" pattern, different check) |
| GR-79031 exact-arithmetic deopt | — | (d) fails — §3 angle on SURV-009 only (Graal's is static-exactness, ours is profile-driven) | — | Filed as §3 angle in A.1, not a candidate |
| GR-79526 lock-elimination extension | — | (d) fails — convergence confirmation on SURV-023's existing scope | — | §3 note in A.1 |

**Net new raw candidates for synthesis: RC-W4-1 only.**

---

## First-class negatives / watch notes (wave 4)

1. **Graal 25.5 re-sweep:** eleven new compiler entries; new speculation
   capability = GR-79590 (speculative store checks) + GR-79031 (exact
   arithmetic via loop-limit deopt). Still no new escape-analysis capability,
   no inlining-policy change.
2. **JEP 544:** still Candidate for JDK 28 — no Proposed→Target transition
   (JVM Weekly vol. 192, InfoWorld, both fresh). Next watch: JDK 28
   ramp-down Dec 2026–Jan 2027.
3. **JEP 401:** integrated mainline / JDK 28 preview, unchanged; no movement
   on primitive classes or null-restricted value types.
4. **TruffleRuby:** no speculation options landed (trigger not fired).
5. **GraalPy:** 25.4 changelog = BouncyCastle removal only; no JIT-internal
   entries in 25.0–25.4 (trigger not fired).
6. **T3 triggers:** none of the six fired (Meta JIT, Pyston, move-only SIL
   pass, SE-0474, TruffleRuby, GraalPy).
7. **#24314:** open, no movement. **#16798/#21536:** both closed/frozen —
   watches retired. **#77893:** all five CLs still unmerged (NEW); new
   watermark CL fleet data (copystack → ~0) strengthens SURV-041's precedent
   without narrowing its gap.

---

## Sources (all accessed/fetched 2026-10-04)

- `https://github.com/oracle/graal/blob/HEAD/compiler/CHANGELOG.md`
  (commit 384c27b — GraalVM 25.5 section, direct fetch).
- `https://github.com/oracle/graalpython/blob/HEAD/CHANGELOG.md`
  (commit 178ea261 — 25.4 section, direct fetch).
- `https://www.jvm-weekly.com/p/jdk-27-is-here-jvm-weekly-vol-192`
  (crawled 10h ago) — JEP 544/401 status; JDK-28 candidates.
- `https://www.infoworld.com/article/4205791/java-28-starts-to-take-shape.html`
  (10 days ago) — JEP 544 "proposed", not targeted.
- `https://www.ankurm.com/whats-coming-in-jdk-28-value-classes-preview-and-strict-field-initialization/`
  (6 days ago) — 28-ea+17 snapshot, JEP 401 preview.
- `https://github.com/swiftlang/swift-evolution/blob/HEAD/proposals/0474-yielding-accessors.md`
  (updated 4 days ago) — SE-0474 still Accepted, experimental-gated.
- `https://forums.swift.org/t/rename-borrow-mutate-accessors-and-borrow-inout-types-to-borrowed-mutable/85287`
  — confirms yielding accessors not yet in a stable release.
- `https://commits.ecosyste.ms/hosts/GitHub/repositories/pyston/pyston`
  (synced 5 days ago) — 0 commits in past year.
- `https://github.com/golang/go/issues/24314` (page fetched; API state open).
- `https://api.github.com/repos/golang/go/issues/{24314,77893,16798,21536}`
  — states, comment counts, update timestamps.
- `https://go-review.googlesource.com/changes/` (REST API) — CLs
  758140/758141/764220/793780/793920 all status NEW.
- Go Review CL links via `https://go.dev/cl/<n>`.
- Web searches: Meta optimizing JIT 2026, Pyston 2026, Swift move-only SIL,
  Sorbet compiler 2026, GraalPy JIT changelog, TruffleRuby options.


