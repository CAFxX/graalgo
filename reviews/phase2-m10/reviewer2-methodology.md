# Reviewer 2 (methodology + documentation) — P2-M10 closeout

**Reviewer:** Muse (independent methodology reviewer)
**Date:** 2026-10-06
**Snapshot:** commit `9f6d2c9` ("P2-M10: divergence triage (DIV-M3-1, GG-0048 list) + M8 conditional owner-acceptance recorded"), checked out in a scratch copy under `/tmp/graalgo-review`; the live tree was not modified. All test/mutation/gate experiments ran against sources compiled from the snapshot (phase-1 classes consumed read-only from the live tree's build outputs, which are at the same commit).
**Verdict: GO-WITH-FINDINGS** — no P0; one P1 that must be closed before final sign-off; six P2s.

## Method

1. Read `phases/phase2/PLAN.md`, `SPEC.md` (§3.5, §7.2), `phases/phase2/DIVERGENCES.md`, `hidden_files/torture/REPORT.md`, `hidden_files/pinning/AUDIT.md`, `hidden_files/jfr/EVENTS.md`, `hidden_files/modelcheck/REPORT.md`, `phases/phase2/CHECKPOINT.md` tail, plus the cited code, tests, corpus READMEs, and `GODEBUG.md`.
2. Compiled all 34 phase-2 `src/main` and 22 `src/test` sources from the snapshot with the pinned GraalVM 25.0.4.1.1 toolchain and ran the disputed tests individually through a reflection runner mirroring `SchedTestRunner`'s protocol.
3. Mutation-tested every red-green claim: disabled the model explorer's lost-wakeup detector; injected a 2µs busy-spin into `JfrEvents.parkResumed`; forced a PinAudit `--tests`-leg failure; ran `JfrRegistrationTest` against the pre-fix `ensureRegistered()`.
4. Constructed one violation per `make check` grep gate (TODO, `println`, `native` decl, FFM import, `Netpoll.` ref, nio import, off-heap pattern) and one signature drift for `FreezeGapTest`.
5. Recomputed every generated-artifact number from raw evidence: `battery_summary.log`, the 8 gmp-matrix cell pairs, `p2-m6-evidence.jfr` via `jfr summary`/`jfr print`, and two independent runs of the model explorer.

## Findings

### P1

**P1-1 — Done-gate evidence is not pinned in the fixed snapshot.** At `9f6d2c9` the versioned tree contains only 6 files under `hidden_files/torture/` (REPORT.md, 4 corpus files, `F3_10k.log`). Missing from version control: all 13 battery leg logs + `battery_summary.log`, the entire `gmp-matrix/` (32 files), `stress-differential/` (16 files), and every `reviews/phase2-m*/` directory (all milestones have ≥2 reviews on disk — verified — but all untracked). PLAN.md §6 names "archived logs" and "reviews/phase2-m*/" as gate evidence; a future `git checkout 9f6d2c9` loses them. I verified every number against the untracked artifacts and they all check out (see "Attacks that failed"), so this is preservation, not fabrication — but the closeout must commit the evidence set (or record content hashes in CHECKPOINT.md) before the final sign-off. Related: the 19:56 battery leg logs were already overwritten on disk by the 22:03–22:05 in-flight re-run; only `battery_summary.log` still preserves the REPORT's numbers.

### P2

**P2-1 — `torture/REPORT.md` is stale on the discharged M8 conditional.** §5 says the sync-scope owner decision "was never made" and the verdict is "MET (conditional — see §5 governance note)" (§5, §8 line 139); §8 line 159 then says "Done-gate 1: MET." unconditionally — internally inconsistent. CHECKPOINT.md (same commit, P2-M10 entry) records the supervisor's owner-acceptance and states "The conditional on done-gate 1 is discharged." The done-gate-1 evidence artifact must be updated to reflect the recorded decision instead of contradicting it.

**P2-2 — `pinaudit-report.md` lacks the promised `AUDIT_RESULT` verdict line.** Commit `ecaabe0` (reviewer1 P2-6) changed `PinAudit` to append `AUDIT_RESULT=...` to the archived report, but the archived report was generated at `9828c4a`, an ancestor of `ecaabe0`, and was never regenerated. `grep -c AUDIT_RESULT` on the archived file returns 0. Either re-run `make pin-audit` or document why the archived report predates the tool fix.

**P2-3 — `AUDIT.md` §6 honest-accounting arithmetic is off by one.** 185 corpus files − 39 skipped = 146 clean runs, not 147; "161 units with pin evidence" should be 160 (146 clean + 13 panic + 1 tests-leg). Verified by counting: 148 phase-1 + 37 phase-2 `.go` files at both the audit commit and HEAD; the report's skipped list is exactly 13 panic-runs + 26 build-failures. The section whose purpose is precise honesty should be precise.

**P2-4 — DIVERGENCES.md GG-0048 (v) cites the wrong test.** "Cooperative yield: `Scheduler.yield()` is the Gosched vehicle, exercised by `JfrParkCostTest`" — `JfrParkCostTest` never calls `Scheduler.yield()`; it measures the JFR hook cost directly. The yield path is exercised by `JfrEventsTest.testHotLoopParkFilter` (via `runYielders`, line 343). The substance (yield path covered) is true; the citation is wrong.

**P2-5 — DIVERGENCES.md GG-0048 (i) sample range is inaccurate.** "5 legs, 36k–52k samples each" — per `battery_summary.log`: 36,757 / 16,771 / 25,024 / 52,672 / 41,397. Two legs are below 36k. Correct range is 17k–52k, or list per-leg. (CV range 0.001–0.018 is accurate.)

**P2-6 — Debug-gate exclusions are whole-file.** Seven files are fully excluded from the `System.out/err.println` gate (`SchedulerCore`, `Drainer`, `TimerHeap`, `GcInterop`, `SchedTrace`, `GcTrace`, `PinAudit`). Each exclusion is justified in the Makefile comments (panic reporting is Go parity; trace lines are the GODEBUG feature), and I verified the gate still catches `println` in non-excluded files — but any debug `println` inside those seven files is invisible to the gate. Documented residual; consider narrowing to line-anchored exclusions.

**P2-7 (editorial) — `modelcheck/REPORT.md` sections run 7, 9, 8.** Numbering only.

## Attacks that failed (claims that survived adversarial verification)

**A1 — "The REPORT.md numbers don't reproduce."** Failed: every torture number matches `battery_summary.log` exactly (F1 36,757/CV 0.0011; F1_q10ms 52,672/CV 0.0175; F4_q10ms 34.7µs PASS; F4_q1ms 48.9µs INFO-FAIL; F5 0 violations; F5b 5.94M iters; F6 direct=12829/stashed=3). All 8 GOMAXPROCS cells byte-identical vs gc (diffed). `p2-m6-evidence.jfr` yields exactly the EVENTS.md counts (Start 4 / End 4 / Park 4 / ChannelBlock 3 / VirtualThreadStart 4) with the stated reasons and durations (3×CHANNEL ≈274/5/8ms, 1×TIMER ≈400ms). The model explorer reproduced all 12 scripts' state counts identically across two runs (threeway 12,365/8; init-under-wake-flood 2,896/3; the rest per REPORT §5), confirming the canonical-snapshot claim.

**A2 — "The red-green tests are tautological."** Failed on all four:
1. `testModelDetectsWithheldUnpark`: with the explorer's lost-wakeup detector disabled (mutation), the test FAILS with "explorer must flag a park whose unpark never arrives as LOST WAKEUP"; the other 16 tests still pass, showing the mutation is targeted.
2. `JfrParkCostTest`: with a 2µs busy-spin in `parkResumed` (replicating the documented method), the test FAILS — "blew the budget: 5527.6ns/park vs 980.5ns" (8× nanoCost). Clean run passes at 0.81× nanoCost.
3. PinAudit vacuous-PASS fix: forcing the `--tests` leg to exit non-zero yields `AUDIT_RESULT=FAIL` / `INFRA_FAIL: testsFailed=true` — no vacuous pass. `PinAuditTest`'s fixture assertions (exactly 1 pin, thread `pin-jni-vt`, `doPark`+`parkWhileCritical` frames, allowlist behavior) are specific and failable.
4. `JfrRegistrationTest`: FAILS against the pre-fix class-literal `ensureRegistered()` ("event type not registered"), PASSES on the fix — the P2-M7 pin root-cause fix is genuinely guarded.

**A3 — "The grep gates don't catch what they claim."** Failed: placeholder gate catches TODO; debug gate catches `println` in non-excluded files; pin-surface gate catches `public native`, `private static native`, and `java.lang.foreign` imports (an earlier apparent miss was my harness running from the wrong cwd — `../phase1/src/main/` must exist); netpoller gate catches `Netpoll.` references and `java.nio.*` imports in `src/main`; off-heap/barrier gate catches `allocateMemory` et al. `FreezeGapTest` catches signature drift (mutated `startTimer` void→long: "expected void got long"). The gates are sound for their stated purposes.

## Cross-doc consistency

1. DIV-M3-1 matches the code (`GoTimer` javadoc: 1-buffered `GoChan`, `len()` returns 1) and both provenance claims check out (timer corpus README carries the same divergence note and the Phase-3 trigger). Re-test trigger is blocking and actionable: fix before Phase 3 lowers `time`, re-run the differential timer corpus.
2. DIV-M4-1..9 spot-checked against `GcInterop` (nil finalizer arg documented; `IllegalArgumentException("runtime.SetFinalizer: nil object")` for nil); DIV-M4-5's "no Go call path" scope note is consistent with the marker dispatch described.
3. DIV-M5-5's five-knob table matches `GODEBUG.md`'s registry values and rationales exactly.
4. DIV-M6-1 matches `EVENTS.md`'s runtime/trace stance.
5. GG-0048 dispositions match cited evidence: (i) fairness — F1 legs + select differential 12/12 (P2-M2 sign-off, CHECKPOINT); (ii) starvation-freedom — F3/F6 numbers + 12/12 model scripts; (iii) latency — 34.7µs/100µs budget, 2.9x margin, F4_q1ms INFO-FAIL honestly labeled per spike precedent; (iv) GOMAXPROCS — 8/8 byte-identical (verified) with the channel-heavy and I/O+compute scope gaps recorded honestly in both REPORT §3 and DIVERGENCES; (v) Gosched — F1 quantum legs + yield coverage (citation wrong per P2-4, substance true).
6. Every re-test trigger names an actionable trigger: DIV-M3-1 (blocking, Phase-3), server-hardware re-measure (standing since spike 2b), Go-source channel-heavy matrix, frontend channel/timer pin-audit re-run (AUDIT.md §6), token-protocol change → re-run explorer (modelcheck §1), WS G overlay → bind sync primitives and re-run as Go programs (torture §5). The "no trigger needed" entries are all CLOSED with zero accepted divergences — correct.
7. No contradictions between PLAN.md, SPEC.md §3.5/§7.2, DIVERGENCES.md, and the evidence docs beyond P2-1 (conditional wording) and the P2-4/P2-5 nits. PLAN §3's spike-2b overhead numbers (33µs/29.9µs) vs REPORT's production numbers (34.7µs/48.9µs) are different measurement scopes (prototype vs production), not a contradiction — though a reader could confuse them.

## Done-gate evidence links (gates 1–5)

1. **Stress + torture:** REPORT.md exists with per-leg numbers + log-line citations; numbers verified against `battery_summary.log`; 8/8 matrix cells byte-identical; kill criteria assessed NOT TRIGGERED. Verdict support: MET — conditional discharged per recorded owner decision (REPORT.md text needs the P2-1 update).
2. **Pinning audit:** AUDIT.md exists with methodology, JEP-491 platform finding, event-boundary division of labor, and honest coverage accounting (modulo P2-3 arithmetic); `pinaudit-report.md` shows 186 units / 0 pins (modulo P2-2 missing verdict line); the 26 build-failures contributing no pin evidence is stated plainly with a re-run trigger. Verdict support: MET.
3. **JFR flowing:** EVENTS.md + `p2-m6-evidence.jfr` exist; counts, reasons, and durations verified against the file. Verdict support: MET.
4. **Reviews + race/model-checking:** model report exists; all 12 explorer scripts reproduced; the §9 race-detector-equivalence argument is explicit and scoped to this design (single-threaded drainer + atomic/volatile-only vthread side + monitor-held channels + empirical stress). ≥2 reviews per milestone P2-M1–M9 confirmed on disk (P1-1: untracked). Verdict support: MET modulo evidence pinning.
5. **Divergence list:** DIVERGENCES.md versioned; four entries CLOSED with zero accepted divergences, one ACCEPTED-WITH-TOLERANCE with documented band; DIV-M3-1 added with blocking Phase-3 trigger; GG-0048 list complete per SPEC §7.2 (i)–(v). Verdict support: MET.

## Recommendations

1. (P1-1) Before final sign-off: `git add` the battery logs + `battery_summary.log`, `gmp-matrix/`, `stress-differential/`, and `reviews/phase2-m*/` (or record SHA-256 hashes in CHECKPOINT.md). The "in-flight" re-run must not overwrite the evidence the REPORT cites — archive per-run with timestamps.
2. (P2-1) Update `torture/REPORT.md` §5/§8 to record the discharged conditional.
3. (P2-2) Regenerate or annotate `pinaudit-report.md` with the `AUDIT_RESULT` line.
4. (P2-3/4/5) Fix the three doc nits (146/160; `JfrEventsTest.testHotLoopParkFilter`; 17k–52k).
5. Consider line-anchored instead of whole-file debug-gate exclusions (P2-6).

## Bottom line

The methodology held up under adversarial re-execution: the numbers reproduce from raw logs, the red-green tests fail when their bugs are present (verified by four independent mutations), the grep gates catch constructed violations, and the honest-gap accounting (Java-level channel matrix, 26 vacuous pin-audit programs, untracked-evidence caveat in AUDIT.md itself) is genuinely honest. Nothing I found challenges a done-gate verdict's substance — the findings are documentation accuracy and evidence preservation. **GO-WITH-FINDINGS**; close P1-1 before the final sign-off.
