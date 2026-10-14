# P2-M8 Code Review — reviewer1 (torture battery + sync primitives + drainer provenance)

**Scope:** `~/workspace/graalgo`, snapshot commit `74e6b2a`
("P2-M8: torture report + Go stress corpus + make torture/gmp-matrix targets").
Read-only review; no files modified.

**Files reviewed:**
1. `phases/phase2/src/test/java/graalgo/sched/torture/TortureBattery.java` (562 lines)
2. `phases/phase2/src/main/java/graalgo/runtime/sync/` — `GoMutex`, `GoRWMutex`, `GoWaitGroup`, `GoOnce`
3. `phases/phase2/src/test/java/graalgo/runtime/sync/SyncStressTest.java`
4. `phases/phase2/src/main/java/graalgo/sched/Drainer.java` (provenance counters)
5. `phases/phase2/src/main/java/graalgo/sched/SchedulerCore.java` (`wakeProvenance()`)
6. `phases/phase2/tools/torture/run_battery.sh`, `gmp_matrix.sh`
7. `phases/phase2/hidden_files/torture/REPORT.md`, `corpus/`, `logs/`
8. `phases/phase2/Makefile` (torture/gmp-matrix targets)
9. Supporting sources read for verification: `ChanOps.java`, `SelectOp.java`,
   `GoChan.java` (ctor/close), `ResumeToken.java`, `SchedMain.java` (`--gmp`),
   `ChannelTest.java`, `TimerDifferentialTest.java`, `SchedTestRunner.java`

**Ground truth for F1–F6 semantics:** `spikes/02b-userspace-scheduler/REPORT.md` (§2, §3.4–3.6).

**Note on working-tree drift (important):** during this review (2026-10-05
~20:10 UTC) `phases/phase2/hidden_files/torture/REPORT.md` acquired
*uncommitted* edits that change numbers vs the `74e6b2a` snapshot I was asked
to review (e.g. F4_q1ms 12.9µs → 48.9µs, "7x margin" → "2.9x margin", new
§3 gap disclosures). Findings below are against the snapshot; where the
working tree already addresses a finding I say so. The snapshot remains the
review baseline.

---

## Verdict: GO-WITH-FINDINGS

No P0 (correctness/soundness) issues found in the new code. The torture port
is faithful where it exists, the F6 hang diagnosis and fix are correct, the
four sync primitives are genuinely scheduler-aware with sound handoff logic,
and the drainer provenance counters are placed correctly and cover all WAKE
provenances. The P1 findings are: (a) F2 (O2–O5) was never ported to the
battery although the class javadoc lists it as a leg; (b) one remaining
number mismatch in REPORT.md §6 vs the archived F6 log (the working-tree
edit fixed the F4 numbers but not this one); (c) a misleading comment on
the F4 poll measurement. None invalidates the done-gate verdicts, but (a)
and (b) should be fixed before sign-off.

---

## P1 findings (should fix)

### P1-1. F2 (spike O2–O5) is documented as a battery leg but was never ported

`TortureBattery.java:23` javadoc lists "F2: O2–O5 correctness — channels,
select fairness, timers, scale", but `main()` (lines 550–556) dispatches
only F1/F3/F4/F5/F5b/F6, and `run_battery.sh` has no F2 leg. So the claim
"spike 2b F1–F6 on the production path" (battery script header, REPORT.md
§1 title) is overstated for F2.

What actually covers the O2–O5 semantics today:
- O2 (channels, no loss): covered at unit level — `ChannelTest`
  (unbuffered/buffered/close semantics) — plus F1/F6's unbuffered
  ping-pong under preemption. The spike's "20,000 msgs × 2 senders × 2
  channel-cap configs" volume leg has no direct equivalent.
- O3 (select fairness, 8 channels, max/min 1.044): `ChannelTest.testSelectFairness`
  covers 2-case fairness; the 8-channel ratio measurement was not ported.
- O4 (timers, 2,000 sleeps): `TimerDifferentialTest` / `TimerTest` cover
  timer semantics; no 2k-sleep volume leg.
- O5 (scale, spin-barrier throughput): **not covered anywhere** at torture
  level.

Fix options: (i) add a real F2 leg, or (ii) correct the javadoc to map F2 →
the unit tests explicitly and record the O5 gap as a re-test trigger in
REPORT.md §5 (the working-tree edit already started this honest-gap style
for the matrix in §3 — apply the same here). What is *not* acceptable is
leaving the javadoc claiming a leg that doesn't exist.

### P1-2. REPORT.md §6 F6 provenance numbers contradict §1 and the archived log

Snapshot REPORT.md §6 says the F6 run gave ``direct=12830 stashed=2
ioExit=0``, but §1's table and the archived
`hidden_files/torture/logs/F6_wakeup.log` both say
`direct=12829 stashed=3 ioExit=0`. Same total (12832), different split —
looks like two different runs were quoted, or a transcription error. The
M6 resolution ("the mechanism is hit in normal operation") holds either
way, but a done-gate evidence report must quote one run consistently.
(The concurrent working-tree edit fixed the F4 number mismatches but left
this one — still open.) Reconcile §6 to the archived log.

### P1-3. F4 poll-fast-path comment is false and the number is mislabeled

`TortureBattery.java:291`: `// poll() fast path on a goroutine (quantum
never expires here).` Two problems:
1. The quantum *does* expire: 10M polls at the measured ~237ns/poll ≈
   2.4s of looping ≫ 10ms quantum, so hundreds of quantum-expiry parks
   are included in the measurement.
2. 236.89ns/poll (from `F4_q1ms.log`) is not obviously a "fast path"
   number for what should be a ThreadLocal read + `nanoTime` compare —
   worth one sentence on where the time goes, or drop the "fast path"
   label.

Informational only (not gated), but the label invites misreading.

---

## P2 findings (nits / minor)

### P2-1. Dead provenance counters in Drainer

`Drainer.java:61-62` declares `wakeResumeQuantum` and `admitInit` but they
are never incremented anywhere and `wakeProvenance()` doesn't report them.
Remove them, or wire them up. (They are *not* needed for WAKE-provenance
completeness — see finding 5 below.)

### P2-2. F6 asserts the lost-wakeup invariant only indirectly

The spike's F6 asserted `parks == admissions` explicitly. The port relies
on: drainer hard assert (cap) + `done.await` timeout (hang ⇒ FAIL) +
`errors == 0`. A lost wakeup would surface as a hang, so this is *sound*,
but printing `sched` admissions vs a park count would restore direct parity
with the spike's invariant. (`Drainer.admissions()` exists but is
package-private and not surfaced via `SchedulerCore`.)

### P2-3. `unpark()` while holding the object's monitor (style)

`GoWaitGroup.add()` and the `GoOnce` winner path call `sched.unpark()`
while holding `synchronized(this)` / `synchronized(waitLock)`. Safe —
`unpark` is a non-blocking inbox offer and the drainer never acquires these
monitors, so no lock-ordering issue — but collecting handles and unparking
after the monitor (the pattern `GoMutex`/`GoRWMutex` already use) is
cleaner and future-proof.

### P2-4. Undocumented goroutine-context requirement

`GoOnce.doOnce`'s slow path calls `sched.currentGoroutine()`, which throws
`IllegalStateException` on a non-scheduler thread. The fast path
(`state == DONE`) is thread-safe, but the class javadoc doesn't state the
goroutine-context requirement. One-line doc fix. (Same latent requirement
exists in the other three primitives via `parkCurrent`; `GoMutex`'s
javadoc at least frames itself as a runtime primitive.)

### P2-5. F3's fairness bound vs the spike

The port gates `max/mean <= 2.0`; the spike observed 1.05 (archived
`F3_quiesce.log` here shows max/mean 1.00). The looser bound is disclosed
in the leg javadoc — acceptable — but if the bound is ever tightened toward
the observed value, note the spike's verdict-scope caveat (2-vCPU VM).

---

## Detailed analysis

### 1. Torture port fidelity (F1, F3, F4, F5, F5b, F6)

**F1 (O1 torture) — faithful.** Binary gate `samples > 0` matches the
spike's "pair gets samples" gate (spike: exit 0 / PARTIAL below the 200k
cap). All five spike legs are mirrored (`q1ms`, `q100us`, `cap2par8`,
`q10ms`, `gmp32par2` incl. the effective-cap clamp print). Spinner CV is
reported, not gated — same as the spike. Shutdown discipline is sound:
`stop[0]=true`, then `ping.close()`/`pong.close()` to unblock the pair —
`GoChan.send` on a closed channel panics ("send on closed channel", gc
parity), the `PanicException` is caught and suppressed post-stop, and the
stop-write is visible to the woken goroutines via the close→unpark→acked
volatile chain. `awaitQuiescence()` then drains.

**F3 (quiescence) — faithful, with one disclosed mechanism substitution.**
The port uses explicit `yieldCurrent()` per iteration instead of
quantum-expiry spinning, and says so in the comment ("Quantum-expiry
fairness is covered by F1's poll() spinners"). The starvation signal
(`admitted_before_stop == nSpin`, sampled before `stop=true`) and the
latch-based deterministic drain (120s timeout so genuine starvation still
fails) are exactly the spike §3.4 harness fix. Commit `7377122`'s F3 fix —
counting `admitted` on the *first yield during the run* instead of
post-loop `yields > 0` — is the correct port of the spike's
`firstAdmissions` instrumentation; the old form reintroduced the very
drain-window artifact §3.4 warned about. Max/mean ≤ 2.0 + `drainedOk` are
sane gates. One termination subtlety checked: a never-admitted spinner is
parked in `awaitAck` on the INIT token; setting `stop` doesn't unblock it,
but the drainer still admits it via the init lane, it observes `stop` and
counts down the latch — the test cannot hang on starvation, it fails on
the counter. Good.

**F4 (overhead) — faithful; the informational 1ms leg is justified.**
Per-hop `yieldCurrent()` cost vs 1% of the *configured* quantum is the
spike §1.3/§3.6 discipline. `run_battery.sh:run_info` records the 1ms
FAIL without failing the battery and prints "(informational FAIL
discounted: ...)". Justification holds: GG-SCHED-0001's falsifier is
defined at the 10ms production default (`DEFAULT_QUANTUM_NANOS`,
P2-D5), where the leg passes at 34.7µs < 100µs; the ~49µs hop is
structural drainer/FJP cost that cannot fit a 10µs budget at 1ms — the
spike's documented, still-fired trigger. The working-tree REPORT.md edit
now also discloses honestly that 48.9µs regressed vs the spike's 33.0µs
at 1ms (VM-noise scope). Accept.

**F5 (cap fidelity) — faithful and the observational check is sound.**
I verified the no-false-positive argument: between `current.incrementAndGet()`
and `current.decrementAndGet()` there is no park point, so every goroutine
in the window is in-flight (admitted, not parked); `inflight ≤ cap` is
hard-asserted by the drainer after every route+admit (`Drainer.checkInvariants`,
halting loudly on violation). Hence `violations > 0` is reachable only via
a genuine drainer bug. The spike's "mixed I/O+compute throughput" leg was
not ported, but it was a noisy non-gate ("VM noise dominates") — acceptable
omission.

**F5b (cap-exhaustion) — faithful.** Same shape as the spike: 2× indefinite
`ioEnter` + blocking-take + `ioExit`, one spinner (8s + 2s sustained window
— the "sustained progress, not a burst" comment is the right gate), one
500ms short I/O that must re-acquire through the wake lane. Threshold
`spinnerIters > 1M` is generous vs the spike's 511M but the gate is
liveness, not throughput. `shortIoDone == 1` + `shortOk` + `spinnerOk`
conjoin correctly. The `LinkedBlockingQueue.take()` on the vthread while
in `ioEnter` state is the intended blocking-NIO simulation (carrier
unmounts, slot released) per spike §3.5.

**F6 (lost-wakeup stress) — faithful.** 32 pairs × 200 ping-pongs with the
ponger selecting over `{ping, dch}`; `rt = t0+1` echo check (`pong.send(v+1)`)
is a genuine integrity check, not a tautology. 180s completion gate +
zero errors. The `wakeProvenance` print supports the M6 investigation.

### 2. The F6 select/recv hang fix (commit 7377122) — diagnosis correct, API correct

**Diagnosis correct.** `SelectOp.execute()` completes the chosen case
*atomically under the winning channel's monitor* and stashes the
received value/ok (`SelectOp.java` class javadoc: "A received value/ok is
stashed here and read via `ChanOps#selectRecvValue` /
`#selectRecvOk`"). The old code then called `ping.recv()`, which blocked
for the *next* value on the unbuffered channel — while the pinger was
parked in `pong.recv()` awaiting the reply. Mutual wait ⇒ hang (would
surface as the 180s `done.await` timeout). The fix's comment states this
exactly.

**`selectRecvValue()` is the right API.** It is the documented convention
in `ChanOps`'s class javadoc ("After `selectGo` returns the chosen index,
a chosen recv case's value/ok are read via `selectRecvValue` /
`selectRecvOk` (per-thread stash, set by the select)"). The stash is a
`ThreadLocal`, and each goroutine is one vthread for life, so the
select→read pairing on the same thread is race-free; F6 reads it
immediately in the same loop iteration with no intervening `selectGo`,
so staleness is impossible. The `selectRecvValue with no preceding
selectGo` guard fails loudly on misuse.

### 3. Sync primitives — truly scheduler-aware; handoffs correct; no lost wakeups

All four primitives block only via `SchedulerCore.parkCurrent` (vthread
parks on a drainer ticket; re-admitted through the WAKE lane) and wake via
`SchedulerCore.unpark`. No `synchronized`-wait, no raw `LockSupport.park`,
no latches on goroutine paths. Critical sections are short and never park
while held (P2-3 notes the two `unpark`-under-monitor style exceptions,
both safe). This satisfies P2-D4 / GG-SCHED-0004.

**GoMutex lock/unlock transfer — correct.** `unlock()` holds `locked=true`
across the handoff (`next = waiters.pollFirst()`; `locked=false` only when
nobody waits), so ownership transfers atomically with zero window for a
barging fast-path acquirer — the classic thundering-herd/double-owner bug
is absent. FIFO waiter order is preserved (a racing `lock()` sees
`locked==true` and enqueues behind). Unlock-of-unlocked panics
(`IllegalStateException`, gc message parity); cross-goroutine unlock
allowed (gc parity, documented). Lost-wakeup analysis: contender enqueues
under the monitor *before* `parkCurrent` arms the ticket, and the drainer
matches PARKED/UNPARK by handle id tolerating both orders (`unparkPending`
stash) with ack-before-unpark — no interleaving loses the wakeup.
Non-reentrancy deadlocks like gc — documented.

**GoRWMutex reader/writer handoff — correct.** Accounting invariants hold
on every path: `rLock` fast path requires `!writer && waitingWriters==0`
(writer preference, gc parity); parked readers are counted by the *waker*
(`readers += toWake.size()` in `unlock()` before unparking — matches the
"accounting already incremented" comment in `rLock`); `rUnlock` hands
exclusively to the longest-waiting writer when the last reader leaves
(`writer=true` set before unpark, so no barging); writer `unlock()` wakes
*all* readers or hands to the next writer. Recursive-RLock-with-waiting-
writer deadlocks like gc — documented, not special-cased (parity).
One asymmetry noted but benign: a fresh `lock()` can barge ahead of an
already-queued writer when `readers==0 && !writer` (gc doesn't guarantee
writer FIFO either).

**GoWaitGroup — correct.** `await()` fast path on zero counter (no park);
enqueue-then-park vs `add()`-drains-waiters are mutually exclusive under
the monitor, and the park/unpark ticket protocol covers the residual race
— no lost wakeup. Negative-counter panic present (simplified vs gc's
"Add concurrent with Wait" detection — documented as simplified).

**GoOnce — correct.** `IDLE→RUNNING→DONE` via CAS; the winner sets DONE
and drains waiters under `waitLock`, while slow-path contenders re-check
`DONE` under the same lock before enqueueing — no missed wakeup. Panic in
the action still transitions to DONE (gc parity: a panicking `Once` never
re-runs). 32-racer stress asserts exactly-once.

**Red-green credibility:** the class javadoc claims each test fails against
a broken primitive. The mutex/rwmutex/waitgroup tests assert exact counts
that a non-excluding / wakeup-losing variant would violate; the
`testMutexCapOne` (cap=1 progress) test is the meaningful scheduler-
awareness probe. I did not re-run the red half, but the assertions are
non-tautological by inspection.

### 4. Drainer provenance counters — right sites, full coverage, no perf concern

Increment sites (`Drainer.route()`, all under the drainer lock on the
drainer thread):
- `wakeUnparkDirect++` — UNPARK matched a parked generation → WAKE
  enqueued (line 174). ✓
- `wakeUnparkStashed++` — PARKED found a pre-arrived UNPARK in
  `unparkPending` → WAKE enqueued (line 164). ✓
- `wakeIoExit++` — IO_EXIT → WAKE enqueued (line 208). ✓

**Coverage is complete:** WAKE tokens enter `wakeFifo` at exactly these
three sites plus the `case WAKE -> wakeFifo.addLast(token)` inbox branch,
which has **no producers** (verified by grep: no `Kind.WAKE` offered
anywhere outside `Drainer`). The M6 watch-item question — "re-admission
with no matching UNPARK in flight" — is therefore fully instrumented, and
REPORT.md §6's root-cause explanation (the `unparkPending` fast path)
matches the code exactly.

**Counting at enqueue vs at admission:** counters increment in `route()`
when the WAKE token is enqueued, while `wakeProvenance()` documents
"admissions by cause". Equivalent in practice: lane tokens are only
removed by `pollLane()` inside `admit()`, so every enqueued WAKE is
eventually admitted (a parked goroutine cannot exit before admission, so
no WAKE can be orphaned by an EXIT racing it).

**Performance:** increments are `AtomicLong` on the single drainer thread
under its lock; a `LOCK XADD` (~20ns) per WAKE against microsecond-scale
unpark costs — negligible. (Plain `long`s would suffice given the single
writer, but `AtomicLong` is harmless.)

**One thing the counters don't do** (P2-1): `wakeResumeQuantum` and
`admitInit` are declared but never incremented — dead code, likely
leftovers from a planned RESUME/INIT accounting that `wakeProvenance()`
doesn't report either.

### 5. Scripts, Makefile, corpus, logs

- `run_battery.sh`: sound. `timeout 300` per leg (generous vs the longest
  leg), log archiving to `hidden_files/torture/logs/`, `RESULT=`-line
  summary, exit 2 on any leg FAIL; `run_info` discounts only the
  documented F4_q1ms leg while still printing the discount notice. `set -u`
  safe.
- `gmp_matrix.sh`: 4×2 matrix, `--gmp=N` vs `GOMAXPROCS=N`, byte-compare
  via `cmp -s`, both exits required 0. Corpus programs
  (`gmp_compute.go`, `gmp_mixed.go`) print a single deterministic checksum
  — byte-compare is valid. `gmp_mixed.go`'s own header honestly notes it is
  heterogeneous *compute*, not I/O+compute (the working-tree REPORT.md
  edit now discloses this gap too).
- `Makefile`: `torture` / `gmp-matrix` targets depend on `main`
  and `$(OUT_TEST)/.built` — correct build ordering.
- `SyncStressTest` is wired into `SchedTestRunner` (line 37) — the stress
  tests actually run under `make test`.
- Archived logs match the (working-tree-corrected) report numbers except
  P1-2 above: F1 36,757/52,672 samples, F3 2000/2000 admitted, F4_q10ms
  34.7µs, F5 0 violations, F5b 5.94M iters, F6 0 errors with
  `direct=12829 stashed=3 ioExit=0`.

### 6. What I did not find

No P0: no lost-wakeup path in the park/unpark protocol or the four
primitives (all orderings traced); no double-owner window in mutex
handoff; no in-flight accounting leak on any token path (EXIT/IO_ENTER
mismatch cases fail fast via `AssertionError`); F5's observational gate
is provably sound (see §1); the F6 fix cannot read a stale select value.
`wakeProvenance()` output format matches its documented format string.

---

## Recommended actions before sign-off

1. Fix P1-1: add the F2 leg or correct `TortureBattery`'s javadoc to map
   F2 → unit tests explicitly, recording the O5 gap as a re-test trigger.
2. Fix P1-2: reconcile REPORT.md §6's `direct=12830 stashed=2` with §1 and
   the archived F6 log (`direct=12829 stashed=3`).
3. Fix P1-3: correct or drop the "quantum never expires here" comment on
   the F4 poll measurement.
4. Consider P2-1–P2-4 (dead counters, `unpark`-under-monitor style,
   Once context doc, explicit parks==admissions print).
5. Note: uncommitted working-tree edits to REPORT.md (post-snapshot,
   ~20:10 UTC) already fixed the F4_q1ms number (12.9→48.9µs), the margin
   claims (7x→2.9x), and added honest §3 gap disclosures — verify these
   are committed intentionally and re-check §6, which they missed.
