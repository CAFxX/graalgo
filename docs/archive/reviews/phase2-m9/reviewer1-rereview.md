# P2-M9 remediation re-review — reviewer1 (correctness)

**Verdict: GO-WITH-FINDINGS** — the P0-1 remediation is sound and complete;
all P1-1/P1-2 and P2 items verified fixed; full suite 192/0; cost test
10/10 stable in fresh JVMs plus in-suite. One new P2 (stale budget comment,
non-blocking). The previous NO-GO is lifted.

**Review basis:** commits `2573d38` ("P2-M9: remediate reviewer1
(P0-1/P1-1/P1-2/P2s)") and `4c20a31` ("P2-M9: REPORT budget wording") on top
of `6d3d9e3`/`acb7337`; working tree verified clean (`git status` shows no
modifications under `phases/phase2/src/`, no stash, no fault-injection
residue in `JfrEvents.java`).

**Method:** read every hunk of both remediation commits; re-derived the
budget arithmetic from the `JfrEvents.notePark`/`parkResumed` source
(counted clock reads and volatile ops by hand); ran `JfrParkCostTest` 10×
in fresh JVMs via a throwaway reflection driver; ran all 12 explorer
scripts 3× via a throwaway driver plus once in-suite; ran the full
`make test` (192/0); grepped for stale absolute-budget references and
recording leaks.

## Verification of the remediation claims

### 1. Machine-relative budgets — CONFIRMED, genuinely relative
`JfrParkCostTest.java`: `ENABLED_BUDGET_NANOCOSTS = 8.0`,
`DELTA_BUDGET_NANOCOSTS = 6.0`, asserted as
`enabledMed < 8 * nanoCost` and `(enabledMed - disabledMed) < 6 * nanoCost`
— exactly the remediation spec. No absolute nanosecond constant remains
in the gate.

**nanoTime measurement is sound:** 100k-iteration warmup, then 1M
iterations of `sink += System.nanoTime()` with the `if (sink == 0) throw`
anti-elimination guard — the identical pattern to the established
`JfrEventsTest.testDisabledGuardCostNegligible` (same warmup count, same
sink idiom). `nanoTime` is an opaque intrinsic whose results feed an
observed sink; the JIT cannot eliminate or CSE the calls.

**Multiples are principled.** Hand-counted on the measured path
(`parkSeq` pinned to 1, so every iteration takes the 1023/1024
early-return before `emitQuietly`):
- `notePark` (enabled): 1 volatile write (`parkReason`), 1 volatile read
  (`GoroutineParkEvent.enabled()`), **1 `nanoTime`**, 1 volatile write
  (`parkStartNanos`).
- `parkResumed` (enabled, sampled out): 1 volatile read + 1 volatile
  write (`parkReason`), 1 volatile read (`parkStartNanos`), 1 volatile
  read (`enabled()`), **1 `nanoTime`**, 1 volatile read
  (`parkSeq.get()`), mask check, return.

Total: **2 clock reads + ~8 volatile accesses** (both `parkReason` and
`parkStartNanos` are `volatile` in `GoroutineHandle`; the "~6" in the
brief is the right order of magnitude). 8x = 4x the clock-read count:
generous enough to absorb clock-speed/fixed-cost variation across
machines, tight enough that any microsecond-scale regression trips it by
an order of magnitude. The 6x delta covers the enabled-only delta (~2
clock reads + `parkSeq.get()`); it is belt-and-suspenders alongside the
enabled budget, not redundant — it isolates enabled-path-only growth.

**Verdict on the multiples: keep as-is; do NOT tighten.** Observed
`enabled/nanoCost`: 1.6–2.7x across my 10 runs, 2.9x in the worker's
data — 8x gives ≥2.8x headroom everywhere. On a fast-clock machine
(`nanoTime` ~20ns TSC) the hook's fixed costs (~20–50ns of volatile
ops/branches, which do not shrink with clock speed) push the ratio
toward 4–6x; tightening to 4–5x would reintroduce flakes on exactly the
class of machines this remediation was built for. The gate's purpose is
catching µs-scale regressions (tens of nanoCosts) — 8x/6x do that with
an order of magnitude to spare, as the fault injection proved.

### 2. Fault-injection evidence in CHECKPOINT — CONFIRMED, specific
`phases/phase2/CHECKPOINT.md` tail ("2026-10-05 — P2-M9 reviewer1
remediation") records: date, injection point (`JfrEvents.parkResumed`),
injection (2us busy-spin), failing test (`testJfrParkPathCost`), the
exact failure message with the measured value
("JFR enabled park-hook cost blew the budget: 6611.8776ns/park"), and
revert verification ("working tree verified identical to HEAD via
diff"). I independently verified the tree is clean — no spin/sleep
residue in `JfrEvents.java`, no uncommitted changes, no stash entries.

### 3. "Trickle test" wording — CONFIRMED fixed
`JfrParkCostTest.java:62` now reads: "the 1-in-1024 emit path is covered
separately by the trickle test and its JFR-machinery cost is out of scope
for this budget." This precisely resolves the original imprecision:
`testHotLoopParkFilter` covers filter *exactness* (the emit path is
exercised under a real recording), while emit-path *cost* is explicitly
scoped out of the hook budget. The scoping is correct — the emit path
fires 1-in-1024 parks, so even a 10µs emission amortizes to ~10ns/park.

### 4. P1-1 (REPORT overclaims) — CONFIRMED
- §2: "exactly one" → "at most one", with the Set-dedupe mechanism
  spelled out ("two unparks before one park collapse to a single stash
  entry — the second is absorbed, never duplicated, never owed").
- §4: the no-spurious-admission claim is qualified — explorer terminals
  catch it only via `unpark-consumed-on-care`, the structural argument
  (admit() only acks lane tokens; WAKE tokens originate only from
  UNPARK/IO_EXIT/stash-consumption, all under the single drainer lock)
  carries the rest. Honest and accurate.

### 5. P1-2 (canonical snapshots) — CONFIRMED, empirically
`Drainer.snapshotForTest` now sorts `parked`/`unparkPending`/`inIo` by
normalized name via `TreeMap`/`TreeSet` (plus the P2-4 bonus: uniform
`kind:id:gen` lane rendering). The `idNames` normalization remains a
per-`Exec` bijection (unchanged), so no key collisions are introduced.
I ran all 12 explorer scripts 3× in fresh JVMs plus once in-suite: **all
four runs produce byte-identical state counts, matching REPORT.md §5
exactly** (basic 22/2, cross 528/6, threeway 12365/8, quantum 7/1,
io 9/1, unpark-consumed-once 22/2, cap1-handoff 224/3,
exit-pending-unpark 17/1, unpark-during-quantum 17/1,
init-under-wake-flood 2896/3, mixed-io-park 339/4, unpark-io-park 36/2).
The worker's re-recorded table is faithful, not guessed. Side benefit:
canonical ordering *increased* merge hits (threeway 13200→12365,
init-under-wake-flood 3023→2896, cap1-handoff 227→224, cross 537→528),
so the model check is now strictly stronger as well as reproducible.

### 6. P2s — CONFIRMED
- P2-2: `WakeupRaceStressTest` burn comment now correctly attributes
  quantum expiry to the ~40µs channel round-trip latency, not the
  ~0.5µs burn.
- P2-3: `testModelInitUnderWakeFlood` javadoc documents the boundary
  (two unparks are pressure, not a flood; infinite-flood
  fairness/starvation and RESUME-vs-WAKE/INIT lane contention under cap
  are out of model-checker scope, covered by the P2-M8 torture battery).
- P2-7: REPORT §7 gains the `wakeAction` abstraction-gap entry (set
  before the PARKED offer, consumed after `awaitAck` — cannot affect
  pairing).

## Independent empirical results

**`JfrParkCostTest`, 10 fresh JVMs** (plus 1 in-suite run):

| run | disabled | enabled | nanoCost | enabled/nanoCost | delta/nanoCost | verdict |
|-----|----------|---------|----------|------------------|----------------|---------|
| 1 | 22.5ns | 131.3ns | 52.8ns | 2.49x | 2.06x | PASS |
| 2 | 22.3ns | 133.4ns | 50.0ns | 2.67x | 2.22x | PASS |
| 3 | 22.1ns | 129.1ns | 71.9ns | 1.79x | 1.49x | PASS |
| 4 | 21.9ns | 130.4ns | 70.6ns | 1.85x | 1.54x | PASS |
| 5 | 23.0ns | 130.0ns | 80.2ns | 1.62x | 1.33x | PASS |
| 6 | 24.7ns | 131.0ns | 50.7ns | 2.58x | 2.10x | PASS |
| 7 | 23.2ns | 131.9ns | 49.7ns | 2.65x | 2.19x | PASS |
| 8 | 22.6ns | 131.3ns | 49.3ns | 2.66x | 2.20x | PASS |
| 9 | 22.4ns | 129.2ns | 49.9ns | 2.59x | 2.14x | PASS |
| 10 | 22.4ns | 129.1ns | 55.9ns | 2.31x | 1.91x | PASS |
| in-suite | 51.2ns | 131.1ns | 54.4ns | 2.41x | 1.47x | PASS |

11/11 PASS. Margins: enabled budget 8x vs observed ≤2.7x (≥2.9x
headroom); delta budget 6x vs observed ≤2.2x (≥2.7x headroom).

**No vacuous pass:** the enabled leg measures ~6x the disabled leg
(130ns vs 22ns), consistent with the 2 clock reads being taken; the
`graalgo.GoroutinePark` event name matches the registered `@Name`; no
test leaves a JFR recording open in the suite JVM (PinAudit uses a
subprocess; `JfrRegistrationTest` creates none), so the disabled leg is
genuinely disabled.

**Full suite:** `make test` → **192 tests, 0 failures** (up from 190 —
the 2 new directed tests). In-suite explorer counts match REPORT §5.

## New findings

### P2-1. Stale "500ns budget" comment in `JfrEventsTest.java:447`
The NOTE explaining the smoke-test removal says the replacement "is
strictly more sensitive (500ns budget vs a 0.80 ratio gate…)" — but the
budget is no longer 500ns; it is machine-relative (8× nanoCost) since
this remediation. One-line doc fix (e.g. "machine-relative budget").
Non-blocking; the gate itself is correct. (The `1000ns/800ns` mention in
CHECKPOINT's reviewer2-remediation section is a dated historical log
entry and should stay as written.)

## Observations (not findings)

1. **The box's clock moved under us — and the remediation handled it.**
   Reviewer1 and the worker measured `nanoTime`≈160ns on this VM; my
   runs (minutes after the remediation commits) measure 50–80ns —
   likely a deployment migration. Absolute budgets would have needed
   recalibration; the machine-relative gate passes with unchanged
   margins. This is the remediation working as designed, in the wild.
2. `nanoCost` itself is somewhat noisy run-to-run (50→80ns) while the
   hook cost is rock-stable (~130ns) — the 8x/6x headroom absorbs this
   comfortably, and it is further reason not to tighten the multiples.
3. The CHECKPOINT fault-injection entry does not preserve the exact
   injection snippet (a 2us spin producing 6612ns suggests
   `parkNanos`-style overshoot rather than a calibrated busy-spin, but
   the mechanism is immaterial). The evidence serves its purpose —
   the test fails closed on a µs-scale regression — so this is a note,
   not a defect.

## Bottom line for the parent

The previous NO-GO is lifted. P0-1 is remediated exactly as specified:
budgets are genuinely machine-relative, the clock measurement follows
the established sound pattern, the multiples are principled and
empirically validated (11/11 stable passes, ≥2.7x headroom, fault
injection trips the gate by 5x). P1-1, P1-2, and all P2s are fixed and
verified — including independent re-runs proving the REPORT §5 table is
now reproducible. The single new P2 is a one-line stale comment.
Recommend sign-off once P2-1 is fixed (or accepted as a trivial
follow-up); no re-verification needed beyond it.
