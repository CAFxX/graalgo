# Wave-6 T3 re-survey triggers (W6)

**Wave:** 6 · **Worker:** T3 triggers · **Fetch date:** 2026-10-04 (all checks ~12:37–12:50 CEST)
**Baseline:** wave-5 results as of 2026-10-04 ~11:37–11:55 CEST (see [track-t3-wave5-triggers.md](track-t3-wave5-triggers.md) §1). A trigger "fires" only if it changed since that baseline (~1h prior).
**Mission:** re-check the six standing T3 re-survey triggers from [SURVEY.md](SURVEY.md) §5 / wave-5 §1.
**Scope:** raw writeups only — no SURVEY.md/CHECKPOINT.md edits, no SURV- IDs assigned. All vendor/project performance numbers are their claims, never ours.

---

## 1. T3 re-survey trigger checks

All six triggers re-checked 2026-10-04 between 12:37 and 12:50 CEST, ~45–75
minutes after the wave-5 baseline (~11:37–11:55 CEST). **Zero of six fired.**

### 1.1 Trigger (a): Meta releases a new optimizing JIT — NOT FIRED

**Check performed:** `browser.search` "Meta new optimizing JIT 2026 runtime" (2026-10-04), scanning all 8 results.
**Findings:** no new Meta runtime optimizing-JIT announcement. Hits are unchanged in character from wave 5: Meta's 2024-vintage LLM Compiler (code-size research, not a runtime JIT), unrelated third-party JIT projects (Brood — an optimizing-tier design doc, Qore JIT -O3 defaulting, CratonVM JIT docs, marshmallow-jit), a .NET 11 JIT internals explainer (Microsoft), and a WebKit-on-RISC-V status report. No Cinder successor, no new Meta runtime JIT work announced.
**Trigger remains armed.**

### 1.2 Trigger (b): Pyston commits resume — NOT FIRED

**Check performed:** fetched `https://github.com/pyston/pyston/commits/pyston_main` (2026-10-04) — the actual default branch per the wave-5 method note (the repo has been "(No longer maintained)" since Aug 2024).
**Findings:** commits on `pyston_main`: latest group is still **Aug 12, 2024**; nothing since. Byte-consistent with the wave-5 check. No commit activity resumed.
**Trigger remains armed.**

### 1.3 Trigger (c): a move-only SIL pass appears in Swift — NOT FIRED

**Check performed:** `browser.search` "Swift move-only types SIL optimization pass new 2026" (2026-10-04), scanning all 6 results.
**Findings:** 2026 move-only activity is still *enforcement* and *bug-fixing*, not optimization exploitation: `-typecheck` false-pass probes (move-only enforcement runs in SIL after typechecking), community ownership/semantics references (anupamme/abi skill notes, ryo PL reference, llvm-mlir-book chapter, SIL docs mirrors restating the existing `copy_addr`/`explicit_copy_addr` checker semantics and guaranteed-checker copy reconstruction). No new optimization pass keyed on move-only types. Enforcement still lives in the `Mandatory/` checkers; the SURV-024-class transfer watch stays armed.
**Trigger remains armed.**

### 1.4 Trigger (d): Swift SE-0474 status changes — NOT FIRED

**Check performed:** fetched `https://github.com/swiftlang/swift-evolution/blob/main/proposals/0474-yielding-accessors.md` (2026-10-04), read the header fields (Status, Implementation).
**Findings:** status remains **Accepted**; implementation remains "Partially available on main behind the frontend flag `-enable-experimental-feature CoroutineAccessors`". No status change since wave 5 (map RMW fusion via borrowed pointers still experimental-gated).
**Trigger remains armed.**

### 1.5 Trigger (e): TruffleRuby lands new speculation options — NOT FIRED

**Check performed (2026-10-04):**
- Fetched the canonical generated `src/main/java/org/truffleruby/options/Options.java` from raw `master` (628 lines, read in full, both halves).
- Manual scan of the full declaration + `fromDescriptor` switch: **zero matches** for "speculat" or "guard" in option names.
**Findings:** option set unchanged from wave 5: the only JIT-adjacent knobs are `INLINE_DEFAULT`, `YIELD_ALWAYS_INLINE`, `METHODMISSING_ALWAYS_INLINE` + `METHODMISSING_ALWAYS_CLONE` (clone-vs-inline knob, not a speculation/guard knob), `CLONE_DEFAULT`, `CORE_ALWAYS_CLONE`, `ALWAYS_SPLIT_HONOR`/`NEVER_SPLIT_HONOR`, `ARRAY_SMALL`, `GLOBAL_VARIABLE_MAX_INVALIDATIONS`. No new optimizer-related option. (Note: this wave fetched current `master`, not the wave-3-pinned commit `c734f26543003fefd4519a29adbca62d0c711a2d` — the comparison against wave 5's recorded option list stands; the trigger condition "new optimizer-related option" is evaluated on the option set itself, which matches.)
**Trigger remains armed.**

### 1.6 Trigger (f): GraalPy's JIT changelog gains new entries — NOT FIRED

**Check performed (2026-10-04):**
- Fetched `https://github.com/oracle/graalpython/commits/master/CHANGELOG.md` (file history): most recent changelog commits are **Aug 18, 2026** — no commits after the wave-4/5 baseline period.
- Fetched raw `master` `CHANGELOG.md` (935 lines) and read all 25.x sections top-down: byte-consistent in coverage with the pinned `178ea261` fetch from wave 5 — latest section still **25.4** (BouncyCastle removal only). No new 2026 entry touches JIT internals (tiering thresholds, specialization policy, deopt machinery).
**Findings:** nothing new past the pinned commit's coverage. Graal 25.5's speculation items remain compiler-level, inherited, not GraalPy-level work.
**Trigger remains armed.**

### 1.7 Net result — triggers

**0 of 6 fired.** No raw candidate sketches (none were warranted). All six triggers remain armed for wave 7.

---

## 2. Method notes for wave 7

1. Pyston's real default branch is `pyston_main` — fetch `https://github.com/pyston/pyston/commits/pyston_main` directly (repo still "(No longer maintained)" since Aug 2024; `/commits/main` shows "No commits history").
2. TruffleRuby options live in the *generated* `src/main/java/org/truffleruby/options/Options.java` (header: "automatically generated from options.yml with 'jt build options'"); the canonical raw URL is `https://raw.githubusercontent.com/oracle/truffleruby/master/src/main/java/org/truffleruby/options/Options.java` (628 lines as of 2026-10-04).
3. GraalPy changelog: check both `https://github.com/oracle/graalpython/commits/master/CHANGELOG.md` (file history — catches commits landing after a pin) *and* the raw `master` file's 25.x sections against the pin `178ea261`; the two together disambiguate "file unchanged" from "pin stale". Latest changelog commit as of 2026-10-04: Aug 18, 2026.
