# Review: P2-M8 methodology (done-gate 1 evidence)

**Reviewer:** reviewer2 (methodology), independent
**Date:** 2026-10-05
**Scope (read-only):** `phases/phase2/hidden_files/torture/REPORT.md` @ commit
74e6b2a vs `phases/phase2/PLAN.md` §2(H)/§6(gate 1)/§7(kill)/§9(falsifiers),
`phases/phase2/CHECKPOINT.md` P2-M8 entries, `phases/phase2/tools/torture/`
(battery + matrix scripts), archived logs `hidden_files/torture/logs/`,
matrix outputs `hidden_files/torture/gmp-matrix/`, corpus
`hidden_files/torture/corpus/`, spike 2b REPORT §2/F4 (precedent).
**Method:** every number in REPORT.md re-checked against its archived log;
matrix cells re-compared; scripts read for what was actually executed.

## Verdict: GO-WITH-FINDINGS

No P0. Both kill criteria are genuinely NOT TRIGGERED on the evidence: the
torture battery passes (11/12 legs; the 12th is an expected informational per
the falsifier definitions), the 6 archived matrix cells are byte-identical vs
gc, and the cap is never violated. But REPORT.md contains numbers that do not
match its own archived logs (P1-1, P1-2), the matrix as archived is narrower
than the plan's matrix and the report over-claims it (P1-3), differential
evidence is missing for two of the three named suites (P1-4), and the
sync-scope deviation was decided by the worker, not the program owner, while
the report presents it as settled (P1-5). "Done-gate 1: MET" should be
recorded as conditional on owner acceptance of the scope deviation, and the
P1 number fixes must land before sign-off.

---

## P1 findings (should fix before sign-off)

### P1-1. F4_q1ms mean misreported: 12.9µs in the report vs 48.9µs measured
REPORT.md §1 ("12.9µs > 10µs budget") and §4 ("12.9µs | 10µs | INFO-FAIL")
do not match the archived evidence. `logs/F4_q1ms.log` and
`logs/battery_summary.log` both say `hop_ns mean=48862` (= 48.9µs). The
12.9µs figure appears nowhere in any log; its apparent source is the stale
comment in `tools/torture/run_battery.sh` ("the ~13us hop"), i.e. the report
copied a script-comment expectation instead of the measured number. The
verdict (INFO-FAIL) is unchanged — both numbers exceed the 10µs budget — but
the evidence document must report measured numbers (standing program rule:
no sign-off on stale numbers). Note also that production regressed vs the
spike precedent at 1ms (spike 2b: 33.0µs; production: 48.9µs); still
informational per the falsifier (see Q2), but the delta deserves one line,
not silence.

### P1-2. "7x margin" at 10ms is wrong; the report contradicts itself
REPORT.md §7 (kill-criterion 2 assessment) says "10ms overhead budget met
with 7x margin (F4)". Measured: mean 34,730ns vs 100µs budget = 2.9x margin.
REPORT.md §4 correctly says "3x margin". The "7x" (also in the
`run_battery.sh` comment) is unsupported by any archived run. The kill-2
conclusion (NOT TRIGGERED) still holds at 2.9x, but the assessment paragraph
cites a wrong number — fix to ~3x and fix the script comment that seeded it.

### P1-3. GOMAXPROCS matrix as archived is narrower than planned and over-claimed
The plan's matrix (CHECKPOINT.md P2-M8 item 3; PLAN.md §2 WS H) is
{1,2,4,N} × {compute, mixed I/O+compute, channel-heavy} = 12 cells.
Delivered and archived: 6 cells (compute/mixed × gmp 1/2/4), all
byte-identical vs gc (re-verified by re-running `cmp` on every pair).
Three gaps, all undisclosed or mislabeled in REPORT.md §3:
1. **No distinct gmp=N evidence.** `nproc` = 2 on this host, so
   `gmp_matrix.sh`'s loop `for gmp in 1 2 4 "$NPROC"` runs gmp=2 twice and
   the second run overwrites the first's files. The report's gmp=N column
   and the "8/8" claim in §7 are not backed by independent evidence; at
   most 6 distinct cells exist. (The script's own comment, "N = host CPUs;
   4 on a 2-CPU VM", is confused — N=2 here.)
2. **Channel-heavy is not a matrix.** F1 does contain genuine
   channel-heavy work (one latency pair ping-ponging on two *unbuffered*
   channels under preemption: 36k–52k samples/leg × 4 channel ops each —
   verified in `TortureBattery.legF1`), so the "channel-heavy" label is
   defensible, but it ran only at gmp=2 (plus a cap-clamp leg), not across
   {1,2,4,N}, and not differential vs gc. The report's parenthetical
   "(Channel-heavy cells are Java-level: TortureBattery F1, all PASS.)"
   discloses the Java-level part but not the missing GMP dimension.
3. **"mixed" ≠ I/O+compute.** The corpus file `gmp_mixed.go` is honest
   ("true I/O-slot behavior cannot be expressed in Go source yet… This
   program varies compute intensity"), i.e. it is heterogeneous *compute*.
   True mixed I/O+compute exists only as F5b at a single gmp (Java-level,
   cap-exhaustion leg), not in the matrix. The report's bare "mixed" row
   label invites comparison with the plan's "mixed I/O+compute".
Fix: qualify the §3 table (6 Go cells + what F1/F5b cover at Java level),
drop the gmp=N column or re-run on a host where N is distinct, and state
the actual achieved matrix explicitly.

### P1-4. Differential-vs-gc evidence missing for two of the three named suites
Gate 1's letter requires "`runtime`/`sync`/`context` stress suites pass
differentially vs gc". Delivered:
1. **runtime:** `gmp_compute`/`gmp_mixed` byte-identical vs gc (verified).
   But `rt_finalizer_storm.go` ("Completes cleanly") has *no archived run
   at all* — no script runs it, no log exists. The claim is unverified in
   the evidence package.
2. **sync:** Java-level `SyncStressTest` only (8 tests incl. exclusion,
   cap-1 progress, RWMutex, WaitGroup, Once — real tests, run via the
   reflective `SchedTestRunner`). No differential vs gc is possible with
   Go `import "sync"` blocked, and the report honestly does not claim one
   for sync — but then the gate's differential requirement for sync is
   unmet, and "Done-gate 1: MET" papers over it.
3. **context:** REPORT.md §2 claims "Differential vs gc: identical
   ('cancelled')" for `ctx_cancel.go`, but no differential run is archived
   anywhere — no script references the file, no log, no JSON. The claim
   has no evidence behind it.
Fix: archive actual runs (or drop/soften the claims to "ran cleanly under
graalgo, differential pending"). The gate-1 evidence list also names a
"differential corpus JSON with 0 unexplained divergences" — no JSON
exists; the matrix .out/.err pairs are the de facto evidence. Either
produce the JSON or remove it from the evidence list.

### P1-5. The [OPEN] sync-scope decision is presented as settled; it was never owner-decided
PLAN.md §3: "[OPEN] `sync` package scope for gate 1… needs program-owner
decision before P2-M8." CHECKPOINT.md shows the worker made the call
("Scope decisions (from [OPEN]s)": 4 Java primitives, Go bindings deferred
to WS G/Phase 3 because the Phase-1 overlayImporter is frozen). REPORT.md
§5 documents the deferral and a re-test trigger — good — but never states
that the [OPEN] remains undecided by the program owner or that the
implemented scope is the worker's *proposal*. A reader of the report cannot
tell a decided scope from a provisional one. Given gate 1 names sync
explicitly, the "Done-gate 1: MET" verdict is conditional on the owner
accepting this deviation. Fix: add one explicit paragraph — "[OPEN]
undecided by program owner; scope below is the worker's proposal; gate-1
verdict conditional on owner acceptance" — with the existing re-test
triggers. Same provisional-status note applies to `context` (pattern-level
only, needs interfaces).

---

## P2 findings (nits / should-address)

### P2-1. M6 watch-item §6 numbers are stale vs the final log (conclusion unaffected)
REPORT.md §6 says "F6 run: direct=12830 stashed=2 ioExit=0" and "2 stashed
wakes in F6". The archived `logs/F6_wakeup.log` (and battery_summary) say
`direct=12829 stashed=3 ioExit=0`. Stale from an earlier run. The
conclusion stands — the mechanism is hit in normal operation, and the
evidence is actually *stronger* than §6 claims (stashed=3 in F6, stashed=2
in F1_q1ms, stashed=15 in F1_q100us). Fix the numbers to the archived run.

### P2-2. "6.8µs in quiet run" (§4) has no archived evidence
No quiet-run log exists in `logs/`. Either archive the run or drop the
parenthetical; an evidence document should not cite unarchived runs.

### P2-3. F4 tail is unacknowledged
The archived F4 distributions are extremely skewed: at 10ms,
p50=5.5µs / mean=34.7µs / p99=394µs / max=25.6ms; at 1ms, p99=1.02ms /
max=24.5ms. The budget is defined on the *mean* (GG-SCHED-0001: "mean hop
overhead > 1% of quantum at 10 ms"), so the PASS verdict follows the
falsifier's letter — but the report quotes only the mean. The spike 2b
report was honest about VM-contention noise driving the tail (§3.6); this
report should carry the same one-line caveat plus the standing re-test
trigger (already in §5: re-measure on quiet server hardware).

### P2-4. F3 re-ran at 2k, not the spike's 10k
Spike 2b F3 was 10k spinners; the production re-run is 2k (`F3 2000 15`),
and the "full 10k extended" in the script comment was never executed or
archived. The report honestly reports 2k (admitted 2000/2000, max/mean
1.00 — verified). Consider running the 10k extended leg before sign-off;
at minimum the report should note the scale difference vs the spike.

### P2-5. `run_battery.sh` comments are the stale-number source
The "~13us hop" and "~7x margin" comments seeded P1-1/P1-2. Update them to
the measured production numbers so the next reader isn't misled.

---

## Answers to the review questions

**Q1. Does the evidence support done-gate 1?**
On the gate's binary FAIL definition (kill criterion 1: "any systematic
divergence the scheduler cannot close") — yes: 11/12 battery legs pass with
the 12th an expected informational, 6/6 archived matrix cells byte-identical
vs gc, cap never violated, 0 lost wakeups in F6. But the *letter* of the
gate ("`runtime`/`sync`/`context` stress suites pass differentially vs gc,
including a pinned GOMAXPROCS matrix") is only partly evidenced: the matrix
is 6 cells not 12 (P1-3), sync has no differential (P1-4.2, P1-5), and the
context/finalizer differential claims lack archived runs (P1-4.1, P1-4.3).
"MET" is defensible on the FAIL definition but should be recorded as
conditional pending the P1 fixes and owner acceptance of the scope
deviation.

**Q2. Is the F4_q1ms informational treatment honest?**
The *classification* is honest: GG-SCHED-0001's falsifier fires only "at
10 ms" (PLAN.md §9), and spike 2b P2-D5 explicitly decided the 1%-of-quantum
budget fires at 1ms as a measured, accepted structural cost — the
`run_info` discount in `run_battery.sh` is mechanical, not ad hoc. It does
not hide a budget problem under the program's own definitions, and §5
carries the quiet-hardware re-measure trigger. What is *not* honest is the
number: 12.9µs reported vs 48.9µs measured (P1-1), and the unremarked
regression vs the spike's 33.0µs at 1ms.

**Q3. Is the sync/context scope handling honest?**
Half. The *technical* handling is transparent: §5 documents what was
deferred (Go `sync` bindings blocked by the frozen frontend; `context`
needs interfaces) and gives re-test triggers for both, and the corpus files
carry honest scope comments. What is missing is the *governance* status:
the [OPEN] required a program-owner decision before P2-M8; the worker
decided instead, and the report presents the outcome as settled rather
than provisional (P1-5).

**Q4. Is the M6 watch-item closure sound?**
Yes — sound, not premature. The `unparkPending` fast-path mechanically
explains the observation (UNPARK processed before PARKED → stashed →
immediate re-admit on PARKED looks, to a JFR-timeline observer, like
re-admission with no UNPARK in flight). The new provenance counters show
stashed wakes occurring in normal operation (F6: 3; F1_q1ms: 2;
F1_q100us: 15), which is exactly the predicted signature. What would
falsify it: reproducing the JFR anomaly in a run with stashed=0 and no
direct wake in-window, or any errors>0 / lost wakeup in F6. Minor: §6's
numbers are stale vs the final log (P2-1); the conclusion does not depend
on them.

**Q5. Numbers vs logs.**
Match: F1_q1ms (36,757 / CV 0.0011), F1_q10ms (52,672 / 0.0175),
F1_gmp32par2 (41,397; cap clamp min(32,2)=2), F1_q100us, F1_cap2par8,
F3 (2000/2000 admitted, max/mean 1.00), F4_q10ms (34.7µs < 100µs),
F5/F5_cap1/cap2 (maxTrueParallel 1/2, 0 violations), F5b (5,941,526
spinner iters), F6 §1 row (direct=12829 stashed=3), matrix cell
comparisons (6/6 byte-identical, re-verified).
Do NOT match: F4_q1ms mean (12.9µs claimed, 48.9µs logged — P1-1); §7
"7x margin" (2.9x measured — P1-2); "8/8" matrix cells (6 archived —
P1-3); F6 §6 provenance (12830/2 claimed, 12829/3 logged — P2-1);
"6.8µs quiet run" (no archived run — P2-2).

---

## Recommended actions before sign-off
1. Fix REPORT.md numbers to the archived logs (P1-1, P1-2, P2-1) and the
   `run_battery.sh` comments (P2-5); add the F4-tail caveat (P2-3).
2. Qualify the §3 matrix table to the actually-achieved coverage; drop or
   independently evidence the gmp=N column (P1-3).
3. Archive real runs for `rt_finalizer_storm.go` and `ctx_cancel.go`
   (differential where claimed) or soften the claims; resolve the missing
   "differential corpus JSON" (P1-4).
4. Record the [OPEN] as owner-undecided with the implemented scope marked
   provisional, and record the gate-1 verdict as conditional on owner
   acceptance (P1-5).
5. Optionally run the 10k F3 extended leg the script comment promises
   (P2-4).
