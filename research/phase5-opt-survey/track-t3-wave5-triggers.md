# Wave-5 T3 re-survey triggers + Rust #157943 watch (W5)

**Wave:** 5 · **Worker:** T3 triggers + Rust watch · **Fetch date:** 2026-10-04 (all checks between 11:37 and ~12:00 CEST)
**Baseline:** wave-4 results as of 2026-10-04 ~11:20 CEST (see `track-resweep-wave4.md` §B and `track-rustgoals-wave4.md`). A trigger "fires" only if it changed since that baseline.
**Mission:** (1) re-check the six named T3 re-survey triggers from SURVEY.md §5; (2) Rust #157943 watch — landing status of the 7 split PRs, new try-perf numbers vs the wave-4 priors (instr −0.3..−0.7%, cycles −2.2..−2.5%, bootstrap −3.2%), goals#396 and RFC #3943 status.
**Scope:** raw writeups only — no SURVEY.md/CHECKPOINT.md edits, no SURV- IDs assigned (079+ reserved for wave synthesis), no SPEC.md/PHASES.md/registry changes. All vendor/project performance numbers are their claims, never ours. This file is the wave-5 record; partial notes are left in place if interrupted.

---

## 1. T3 re-survey trigger checks

All six triggers re-checked 2026-10-04 between 11:37 and 11:55 CEST, ~20–40
minutes after the wave-4 baseline (~11:20 CEST). **Zero of six fired.**

### 1.1 Trigger (a): Meta releases a new optimizing JIT — NOT FIRED

**Check performed:** `browser.search` "Meta new optimizing JIT 2026"
(2026-10-04), scanning all 8 results.
**Findings:** no new Meta runtime optimizing-JIT announcement exists. The only
Meta compiler-adjacent items are the known ones: the 2024-vintage *LLM
Compiler* (LLM trained on LLVM IR/assembly for code-size research — not a
runtime JIT), Zoomer (AI-infra profiling platform, Nov 2025), and Muse
terminal coding agents. Other hits are unrelated third-party JIT projects
(Brood, WeavePy, Vortex, Colibri). No Cinder successor, no new Meta runtime
JIT work announced. (Corroboration: a news-vertical search "Meta announces new
JIT compiler Python" on 2026-10-04 returned only CPython 3.15's experimental
JIT upgrade stories — LLVM 21 register allocation, ~7–8% geomean on
pyperformance — no Meta announcement.)
**Trigger remains armed.**

### 1.2 Trigger (b): Pyston commits resume — NOT FIRED

**Check performed:** fetched `https://github.com/pyston/pyston` repo page,
then the commits page for the actual default branch. Note: `/commits/main`
returned "No commits history" and `/commits/master` shows 2021 CPython-era
commits — the real default branch is **`pyston_main`**.
**Findings:** commits on `pyston_main`: latest group is **Aug 12, 2024**
(same as wave 4: "Fix file naming"); nothing since. Repo header still reads
**"(No longer maintained)"** — *A faster and highly-compatible implementation
of the Python programming language.* Stars 2491, not archived, but dead.
Consistent with the wave-4 check (ecosyste.ms: 0 commits in the past year).
**Trigger remains armed.**

### 1.3 Trigger (c): a move-only SIL pass appears in Swift — NOT FIRED

**Check performed:** `browser.search` "Swift move-only types SIL optimization
pass new 2026" (2026-10-04), scanning all 6 results (community knowledge
bases, SIL docs mirrors, ownership probes).
**Findings:** 2026 move-only activity is still *enforcement* and *bug-fixing*,
not optimization exploitation: `-typecheck` false-pass probes (move-only
enforcement runs in SIL after typechecking), `MoveOnlyChecker` crash reports,
and docs restating the existing checker semantics (Raw SIL `copy_addr` ban on
move-only, guaranteed-checker copy reconstruction). No new optimization pass
keyed on move-only types. Enforcement still lives in the `Mandatory/`
checkers; the SURV-024-class transfer watch stays armed.
**Trigger remains armed.**

### 1.4 Trigger (d): Swift SE-0474 status changes — NOT FIRED

**Check performed:** fetched
`https://github.com/swiftlang/swift-evolution/blob/main/proposals/0474-yielding-accessors.md`
(2026-10-04), read the header fields (Status, Implementation).
**Findings:** status remains **Accepted**; implementation remains
"Partially available on main behind the frontend flag
`-enable-experimental-feature CoroutineAccessors`". No status change since
wave 4 (map RMW fusion via borrowed pointers still experimental-gated).
**Trigger remains armed.**

### 1.5 Trigger (e): TruffleRuby lands new speculation options — NOT FIRED

**Check performed (2026-10-04):**
- Located the options source: `options.yml` no longer exists as a standalone
  file in the repo tree; the checked-in **generated**
  `src/main/java/org/truffleruby/options/Options.java`
  (header: "automatically generated from options.yml with 'jt build
  options'") is the canonical list. Fetched and read it in full.
- `browser.find` for "speculat" and "guard" on the fetched file: **zero
  matches**.
- Fetched `CHANGELOG.md`; latest sections 40.1.0 (empty, unreleased) and
  40.0.0 — Performance entries are FFI/FFM native calls, G1 on macOS,
  `MutableTruffleString` bytesplice, `DynamicObject` nodes; no speculation
  options.
- Repo HEAD commit `c734f26543003fefd4519a29adbca62d0c711a2d` is unchanged
  from the wave-3 pin — no commits since the wave-3 source pass.

**Findings:** option set unchanged from wave 3: the only JIT-adjacent knobs
are `INLINE_DEFAULT`, `YIELD_ALWAYS_INLINE`,
`METHODMISSING_ALWAYS_INLINE` (note: `METHODMISSING_ALWAYS_CLONE` exists in
the current file alongside `METHODMISSING_ALWAYS_INLINE` — a clone-vs-inline
knob, not a speculation/guard knob; consistent with the wave-3 summary that
no guard/deopt policy options exist).
**Trigger remains armed.**

### 1.6 Trigger (f): GraalPy's JIT changelog gains new entries — NOT FIRED

**Check performed:** fetched
`https://github.com/oracle/graalpython/blob/HEAD/CHANGELOG.md` (2026-10-04),
read all 25.x sections top-down.
**Findings:** changelog is byte-identical in coverage to wave 4 — pinned at
the **same commit `178ea261`** as the wave-4 fetch; latest section is still
**25.4** (BouncyCastle removal only). No new 2026 entry touches JIT internals
(tiering thresholds, specialization policy, deopt machinery). Graal 25.5's
speculation items remain compiler-level, inherited, not GraalPy-level work.
**Trigger remains armed.**

### 1.7 Net result — triggers

**0 of 6 fired.** No raw candidate sketches (none were warranted). All six
triggers remain armed for wave 6. The bounded re-check discipline from wave 4
(≤30 min per trigger, changelog/options-file granularity) held.

---

## 2. Rust #157943 watch (SURV-024 priors)

All status claims fetched 2026-10-04 ~11:55–12:00 CEST.

### 2.1 Split-PR landing status

Wave-4 baseline: only #163336 (TailCopyToMove) merged (2026-09-30). Re-checked
all seven split PRs:

| PR | Title | Status (2026-10-04) | Notes |
|---|---|---|---|
| #163335 | [1/6] MIR preparation | **OPEN, under review** | tmiasko review ongoing; new `StorageAlloc` statement; op-sem concern raised by RalfJung (2026-09-28). Review order swapped by author 2026-09-30: prep now lands *after* the passes. |
| #163359 | [1.5/6] Ensure ZSTs always initialized | **OPEN** | split out of #163335; **new try-perf run** since wave 4 (see §2.2) |
| #163336 | [2/6] TailCopyToMove | **MERGED — stayed merged** | rollup merge 2026-09-30, r=tmiasko. No revert observed. |
| #163337 | [3/6] PreciseLiveness | **OPEN, under active review** | latest activity 2026-10-03: Amanieu building on tmiasko's dataflow pretty-printing (#163721); discussion of independent testability of the analysis |
| #163338 | [4/6] MoveElimination | **OPEN, not merged** | still awaits tmiasko review; depends on #163336 (merged) + #163337 (open); no try-perf runs observed |
| #163339 | [5/6] Storage reconstruction | **OPEN, not merged** | depends on #163338 |
| #163340 | [6/6] Miri support | **OPEN, not merged** | RalfJung reviewing (2026-09-30 comments on tail-call `move` semantics vs RFC) |

**Answer to the two named questions:**
- #163336 TailCopyToMove has **stayed merged** (landed 2026-09-30; no revert).
- #163338 MoveElimination has **not merged** — still open, gated on
  #163337's review.

### 2.2 New try-perf numbers since wave 4 (refresh of the SURV-024 priors)

Wave-4 priors (all from the 2026-08-29 run on the monolithic #157943):
instr −0.3..−0.7% · cycles −2.2..−2.5% · bootstrap −3.2% (their numbers).
**No newer try-perf run on #157943 itself** was posted (latest activity on
that PR remains the 2026-09-25 split announcement).

**New data point:** rust-timer posted a try-perf run on **#163359 (ZST
initialization, commit `ddd9a7e`)** — this PR existed but had no perf data in
wave 4. Rust's numbers (their claims, not ours):

- **Instruction count** (their "most reliable metric"): all-primary **−0.4%**
  mean — 31 improvements (mean −0.6%, range [−1.7%, −0.2%]) vs 5 regressions
  (mean +1.0%, range [+0.4%, +2.4%]); secondary: −0.4% overall
  (4 improved, 9 regressed)
- **Cycles**: primary — no benches with changes; secondary +0.4% (1 bench)
- **Max RSS**: primary **−0.6%** (3 benches)
- **Binary size**: primary **−0.9%** (14 improved −2.5% mean vs 7 regressed
  +2.4% mean, range up to +12.6% on one)
- **Bootstrap**: +0.10% (noise — compiler-build wall time, and this PR is
  prep work only)
- **Artifact size**: +0.07% (noise)

*(Exact run timestamp not rendered in the page fetch — observed on the PR
page 2026-10-04; commit ddd9a7e is recent. Treat the numbers as their claims
from a run posted in the last ~10 days.)*

**What this changes for the SURV-024 transfer ruling:** a *preparation-only*
PR (ZST initialization hygiene, no optimization logic) already shows a
−0.4% instruction-count improvement with 31/36 primary benches improving —
consistent with the wave-4 characterization "real but young optimization
effort, net-improving aggregates with per-benchmark regression risk". It
does **not** move the ruling: the static-soundness-core §5 strike stands
(RFC #3943 still unaccepted; the new semantics are not yet sound upstream —
Amanieu 2026-09-19: "Miri support work uncovered several issues with the
semantics"), and the guarded-speculative variant remains the only viable
transfer arm. The new numbers mildly strengthen the Stage-0 priors the wave-4
spike design records: MIR-level copy hygiene shows measurable instruction
deltas even before the pass lands — the headroom question for Go's
`copy()`/`append` sites is the spike's to measure, not to assume from Rust's.

### 2.3 goals#396 and RFC #3943 status

- **rust-lang/goals#396** (2026 "Continue MIR Move Elimination in 2026"):
  goal document still shows **Status: Accepted** — unchanged since wave 4.
  Champions unchanged (@Amanieu, @RalfJung, @dianqk).
- **rust-lang/rfcs#3943**: **still Open, under active review** — unchanged
  since wave 4. Recent activity through 2026-09-30 (TailCopyToMove merge
  cross-reference, thread resolutions; RalfJung co-author engagement continues
  on #163340). **Not accepted.** The "technically unsound at the moment"
  caveat (Amanieu 2026-06-22) is still live.

### 2.4 Net result — Rust watch

- Landed: 1 of 7 (#163336, stable).
- In review: 6 of 7; the critical-path pair (#163337 → #163338) is active
  but unmerged; #163339/#163340 cannot land before #163338; #163335 awaits
  t-opsem eyes.
- New benchmark data: #163359 try-perf (above) — the only new measured
  numbers since wave 4.
- Transfer ruling: **no change** (static core still struck; speculative arm
  still the viable one). The wave-4 prototype-gated spike design for
  SURV-024 stands as written; if #163338 lands in a later wave, its MIR
  test corpus becomes a reference input for the spike worker (the wave-4 note
  already flagged this).

---

## 3. Disposition for the wave-5 coordinator

1. **No SURV- IDs requested** — zero triggers fired, no raw candidates. The
   #163359 try-perf numbers are a watch update (SURV-024 priors refresh),
   not a candidate: they do not clear the candidate bar (Rust-side
   measurement, no Go analog, no benchmark named from SURV-024's sketch).
2. All six T3 triggers **remain armed** for wave 6. Check-method notes for
   the next worker: Pyston's default branch is `pyston_main` (not
   `main`/`master`); TruffleRuby's options live in the *generated*
   `src/main/java/org/truffleruby/options/Options.java` (options.yml itself
   is no longer a standalone tree file); GraalPy's changelog is at the
   pinned commit `178ea261`.
3. The #157943 watch should continue in wave 6 with these specific trip
   conditions: (a) #163338 merges → re-read its MIR test corpus for the
   SURV-024 spike; (b) a try-perf run appears on #163338 (MoveElimination) —
   that would be the first measured number on the actual optimization pass,
   superseding the 08-29 monolithic-PR run; (c) RFC #3943 accepted → the
   static-soundness caveat lifts (does not lift the §5 strike for Go — the
   strike is about Go's lack of a borrow checker, not about the RFC's
   status — but it changes the Rust precedent's maturity).
