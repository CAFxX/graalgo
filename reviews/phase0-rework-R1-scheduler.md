# Peer review R1 — graalgo Phase-0 rework, concurrency/scheduler lens

**Reviewer:** R1 (concurrency/scheduler specialist), adversarial
**Date:** 2026-10-03
**Scope reviewed:**
1. `SPEC.md` §3.5 (concurrency runtime), §7.2 (preemption-equivalence bar), §8.2 risk rows 2/10/11, §10.1 provenance ledger
2. `spikes/02b-userspace-scheduler/REPORT.md` + full prototype source (`src/usched/*.java`, `src/work/*.java`) + archived logs (`logs/`) + `run.sh`
3. Requirements: GG-SCHED-0001–0004, GG-WAKE-0001–0003, GG-FAIR-0001–0003, revised GG-0017, GG-0048; ADR-0001 (D5)
4. Greenfield protocol (`~/workspace/skills/greenfield/SKILL.md`) honesty bars

**Method:** I read every line of `UserScheduler.java`, `GoChannel.java`, `GoSelect.java`, `GoTimer.java`, `Goroutine.java`, `Safepoint.java`, `Go.java`, `MpscQueue.java`, all six falsifier harnesses, `run.sh`, and all archived logs; traced every park/unpark/wakeup path by hand; checked every `[VERIFIED]` number in §3.5 against the logs; checked file timestamps for evidence provenance.

**Headline:** the park/drainer *mechanism* genuinely closes the spike-2 kill mechanism, and the park/unpark pairing argument survived everything I threw at it. But there are **two P0s**: (1) the archived evidence does not support the post-fix PASS claims — the battery was never re-run after the fixes, so F2(O5)/F3(quiescence) PASS verdicts are unverified provenance; (2) §3.5's "scheduler-transparent NIO … no starvation risk" is false — cap-exhaustion by blocked I/O indefinitely postpones runnable goroutines, contradicting §7.2(ii) and GG-FAIR-0003, and it breaks the standard Go server pattern. §3.5 is not committable as written.

---

## P0 — blocks the PASS/commit

### P0-1. Archived evidence does not support the post-fix PASS claims (F2-O5, F3-quiescence); battery never re-run after fixes

**Location:** `spikes/02b-userspace-scheduler/logs/`, `REPORT.md` §§2–3, Appendix A; `SPEC.md` §10.1 provenance ledger ("Spike 2b (option C): PASS … `[VERIFIED]`").

**What's wrong.** File timestamps tell the story:
- Battery ran 10:52–10:56 (`logs/*.log`).
- `O5Scale.java` (spin-barrier fix for the §3.1 deadlock) modified **10:55:46** — after both `o5_scale_cap*.log` (10:54:25, 10:55:26).
- `UserScheduler.java` (initFifo fix for §3.2 INIT starvation) modified **10:58:45** — after *every* log.
- `logs/SOURCES.sha256` regenerated **10:58:58** (post-fix sources).

The battery was never re-run. Consequences, verified by reading the logs:
1. `logs/quiescence_10k.log` (the only quiescence log) shows the **pre-fix** run: `never_scheduled=2643 … FAIR=FAIL`, `RESULT=FAIL`. The REPORT's claimed post-fix result (`never_scheduled=0`, max/mean 1.01, §2/F3 and §3.2) appears **nowhere** — `grep -rn "never_scheduled=0"` hits only `REPORT.md`.
2. `logs/o5_scale_cap1.log` and `o5_scale_cap2.log` both read `o5 workers did not finish RESULT=FAIL` — the pre-fix deadlock. The REPORT's "O5 (scale): PASS after fixing test barrier" (§2/F2) has no archived log.
3. `logs/battery_full.log` ends `BATTERY_DONE failed=1`.
4. The checksummed sources therefore **postdate** all logs: the archived (sources, logs) pair is provably not from any single actual run. `sha256sum -c logs/SOURCES.sha256` passes against post-fix sources, which only sharpens the mismatch.

**Why it matters.** Greenfield protocol: "Archive (checksum) the exact harness, fork, and generator sources with every measurement log *at build time*" (measurement honesty). AGENTS.md's standing lesson: "A log whose binary's source cannot be identified is unverified provenance, not a result." Two of six falsifiers' PASS verdicts currently rest on unarchived terminal output. A peer review cannot audit what isn't recorded; the Phase-0→1 gate (ADR-0001 sign-off) would be committing on memory.

**Suggested fix.** Re-run `./build.sh && ./run.sh` from the current (post-fix) sources, confirm the full battery state (expected: everything PASS except the honest F4-mean FAIL at 1ms, which is the documented fired trigger), and archive the fresh logs + checksums. ~4 minutes of machine time. Until then, REPORT.md §§2–3 should mark the F2(O5)/F3 post-fix claims `[UNREPRODUCED-IN-ARCHIVE]` or equivalent — do not let the provenance ledger's `[VERIFIED]` stand on missing logs. (Note: `o1_*`, `o2/o3/o4`, `stress`, `gomaxprocs_*` logs *are* pre-fix too, but the initFifo change is behavior-preserving for those harnesses — all goroutines start upfront, INIT lane drains at startup — so a full re-run covers them uniformly anyway.)

### P0-2. "Scheduler-transparent NIO … no starvation risk" is false; cap-exhaustion by blocked I/O indefinitely postpones runnable goroutines

**Location:** `SPEC.md` §3.5 ("Loom-unmounted blocking I/O (NIO) is scheduler-transparent: it frees the carrier but still counts in-flight — a documented, bounded deviation (no starvation risk; throughput only) `[DECIDED]`"); GG-SCHED-0004 rationale (carves NIO out of the scheduler-aware-blocking ban); contradicts §7.2(ii) and GG-FAIR-0001–0003.

**What's wrong.** Trace the accounting: a goroutine blocked in `SocketChannel.read()` unmounts its vthread (carrier freed by Loom) but never parks on the user-space scheduler, so `inflight` is not decremented. The drainer's `admit()` only fires while `inflight < cap`. Therefore, with `cap = N`, if N goroutines are simultaneously blocked in NIO, **zero** runnable goroutines are admitted — for the entire duration of the longest I/O block. If any block is indefinite (idle keep-alive read with no deadline — routine), postponement is indefinite.

This is not a corner case; it is the standard Go network-server shape. Concrete instance: `GOMAXPROCS=8`, 1000 concurrent connections, one goroutine per connection blocked in `read()`. Eight blocked readers hold all 8 in-flight slots; the accept loop (itself blocked in NIO `accept()`, holding a slot) and every handler with ready work starve. Stock Go runs this fine — the netpoller parks all 1000 without consuming a P. Under §3.5 as specified, the server stalls. "Throughput only" is the wrong category: this is a **liveness** deviation.

It directly contradicts two load-bearing requirements:
- §7.2(ii) (GG-0048): "starvation-freedom — every runnable goroutine eventually runs."
- GG-FAIR-0003: "Every runnable goroutine SHALL be admitted within a bounded number of drainer admissions (no indefinite postponement)."

A bounded-wait claim cannot coexist with an unbounded-postponement mechanism. The `[DECIDED]` label is also the wrong class for what is presented as a factual safety assertion.

**Why it matters.** GG-0018 ("the netpoller is deleted") + this deviation is a committed architectural shape. If it ships as written, any Go program with more concurrent blocking I/O operations than GOMAXPROCS — i.e., most servers — can deadlock. The F5 "mixed I/O+compute" falsifier does not cover this: it measures *throughput degradation* (120.3M vs 152.0M iters/s, ~21% under-utilization with one sleeper at cap=2) using `Thread.sleep` as the stand-in, and never probes a runnable goroutine starved behind I/O-blocked cap exhaustion. So the false safety claim is both untested and wrong.

**Suggested fix** (pick one; (a) is recommended — it preserves "netpoller deleted"):
- (a) **Slot-releasing NIO wrapper.** Extend GG-SCHED-0004's ban to cover NIO: the adaptation layer wraps every blocking NIO call as `slotRelease()` → blocking call → `slotReacquire()` (re-park on the scheduler until `inflight < cap`). The carrier is still freed by the Loom unmount during the block (no netpoller needed — the vthread wakes itself from the kernel on I/O completion); only the *slot accounting* becomes scheduler-aware. Transient carrier oversubscription between I/O completion and re-admission is bounded and self-draining (every such vthread immediately re-parks). Delete the "scheduler-transparent" carve-out; restate the deviation honestly if any remains.
- (b) Reintroduce a readiness poller (park IDLE + `Selector` thread issuing WAKEUP) — i.e., scope GG-0018's "deleted" to "the event loop no longer replaces blocking I/O; a readiness mechanism remains for slot accounting."
- Either way: add a falsifier to §8.2 — *cap-exhaustion starvation probe*: at cap=N, N goroutines blocked in the NIO stand-in indefinitely + 1 runnable spinner; the spinner MUST make progress (this is the F5 scenario that would have caught P0-2). And scope §7.2(ii)/GG-FAIR-0003 explicitly if any residual deviation is kept.
- Related gap: §3.5 enumerates only socket NIO. Blocking **file** I/O on virtual threads *pins* the carrier (no unmount — JEP 491 fixed only `synchronized`), so it holds a carrier *and* a slot. The adaptation-layer audit (GG-SCHED-0004's verification plan) must enumerate every blocking form: socket NIO, file I/O, `Process.waitFor`, DNS, etc.

---

## P1 — must fix, doesn't block the verdict

### P1-1. "No FJP discipline assumed" holds only if effective in-flight ≤ carrier count; `cap > parallelism` is untested and unclamped

**Location:** §3.5 ("no scheduling discipline is assumed from it `[DECIDED]`"; "in-flight unparks stay ≈ carrier count"); `run.sh` (all runs use `par=2` except `o1_cap2_par8` with `par=8, gmp=2` — no run with `gmp > par`).

**What's wrong.** The sound core of option C is a *bounded-submissions* argument: `LockSupport.unpark(parkedVThread)` submits the continuation to the FJP, but at most `cap` continuations are ever outstanding, and with `cap ≤ FJP-parallelism` each finds a carrier — so no queueing discipline is exercised. That argument silently assumes `cap ≤ parallelism`. Nothing enforces it: `usched.gomaxprocs` (→ Go's `GOMAXPROCS`, which users legitimately set above NumCPU) is independent of `jdk.virtualThreadScheduler.parallelism`. With `gmp=1000, par=8`, 1000 continuations queue in the FJP and you are back to relying on the untrusted FJP queue discipline — the exact thing spike 2 killed (and the spike-2 starvation mechanism was never fully root-caused per the program's own notes, so "the FJP will probably drain it fairly" is not an argument you get to make).

**Why it matters.** A user-visible knob (`GOMAXPROCS=10000`) reintroduces dependence on the killed mechanism's untrusted component, with no falsifier covering it.

**Suggested fix.** Clamp the effective cap: `effectiveCap = min(GOMAXPROCS, vThreadSchedulerParallelism)` in the drainer, with a brief semantics note (observationally equivalent on the same hardware: Go on 8 cores also runs only ~8 truly concurrently; oversubscription becomes drainer-FIFO time-slicing, which is *more* Go-faithful than FJP queueing). Add a falsifier config `gmp=32, par=2` to the battery proving liveness under the clamp. Alternatively, run the no-clamp config as a falsifier — but given the unresolved spike-2 mechanism, the clamp is the cheap, sound choice.

### P1-2. Pinning breaks the bounded-submissions argument; GG-0057's +k carriers cover only cgo

**Location:** §3.5 ("A vthread in a native/FFM call pins its carrier (JEP 491 `[VERIFIED]`); with the FJP as a dumb pool this transiently reduces pool capacity rather than breaking scheduling fairness — the mechanism implication is `[DECIDED]`").

**What's wrong.** If a carrier is pinned, effective carriers drop below `cap` while in-flight stays at `cap`: outstanding FJP submissions can exceed available carriers, re-engaging FJP queue discipline (mildly). "Rather than breaking scheduling fairness" is asserted, not shown. Worse, the mitigation ("cgo runs off the carrier pool … budgeted per GG-0057") covers only cgo — not FFM calls from the stdlib adaptation layer, not blocking file I/O (which pins *without* unmounting). The pinning matrix (risk row 11) is correctly gated to Phase 2, but §3.5's prose already draws the comfort conclusion.

**Suggested fix.** State the assumption explicitly in §3.5: "the no-discipline argument requires effective carriers ≥ in-flight; every pinning source must either run off-pool (cgo, per GG-0057) or be counted in the +k budget." Extend GG-0057's scope (or add a GG-SCHED requirement) to cover FFM/file-I/O pinning, not just cgo. Keep risk row 11's kill trigger as the enforcement.

### P1-3. The 10ms default quantum was never tested, yet it is `[DECIDED]` and SHALL'd on the basis of F1/F4 runs at 1ms/100µs

**Location:** §3.5 ("Default quantum 10ms `[DECIDED]`: the spike-2b F4 scoped fix"); GG-SCHED-0001 (statement SHALLs 10ms; verification plan cites "Spike 2b F1/F4"); `run.sh` (no 10ms run exists).

**What's wrong.** F4's verdict was "MARGINAL PASS *with scoped fix*" — the mean fired the kill trigger at 1ms (2.7% > 1%), and 10ms compliance (0.27%) is pure arithmetic (`26.8µs/10ms`), correctly labeled `[ESTIMATE]` in §3.5 but then hardened into a `[DECIDED]` default and a SHALL whose verification plan points at experiments that ran at 1ms/100µs. Nothing in the battery exercises 10ms quanta — not the torture (F1), not the overhead projection, not the quiescence fairness shape at the new granularity.

**Why it matters.** The 10ms choice is load-bearing for both the overhead budget and preemption responsiveness. The arithmetic is sound, but a `[DECIDED]` default on untested config is exactly the class of claim the Greenfield honesty bar exists to catch — and the fix costs one battery re-run (cf. P0-1, which requires a re-run anyway).

**Suggested fix.** Add `run o1_q10ms 2 2 10000000 work.O1Torture 2 5` and an overhead run at 10ms quanta to `run.sh`; fold into the P0-1 re-run. If the numbers hold, the `[DECIDED]`/SHALL stands on measured evidence; if wakeup latency (wakeFifo path) or fairness shifts, re-scope then. Also record the explicit tradeoff the spec already gestures at: 10ms matches Go's sysmon `forcePreemptNS` granularity for compute-bound preemption, and wakeup latency is quantum-independent (wakeFifo priority lane) — state both, since the current text lets a reader infer 10ms quanta slow down channel/timer wakeups (they don't).

### P1-4. §3.5's "Loom never sees it as schedulable work" is mechanically inaccurate

**Location:** §3.5 ("it is held by the *scheduler's* queue, never by an FJP work queue — Loom never sees it as schedulable work, so the FJP cannot starve it `[DECIDED]`").

**What's wrong.** Every resume *does* go through the FJP: `LockSupport.unpark(parkedVirtualThread)` submits the continuation to Loom's scheduler. What the design actually guarantees is the bounded-submissions argument (P1-1): outstanding continuations ≤ cap ≤ carriers, so the FJP never has a queue whose *discipline* matters — only eventual execution of every submitted task, which even a degenerate pool provides. "Never sees it as schedulable work" misstates the mechanism and would mislead the Phase-2 implementer about where the safety comes from (it comes from the bound, which must then be *preserved* — cf. P1-1/P1-2).

**Suggested fix.** Reword to: "Parked vthreads are held by the scheduler's queue; each resume submits at most one continuation to the FJP, with at most `cap` outstanding — so the FJP is never asked to choose fairly among queued continuations, only to eventually run each submitted one."

### P1-5. Drainer liveness: an interrupt kills the entire scheduler; no requirement covers it

**Location:** `UserScheduler.drainLoop` (`catch (InterruptedException ie) { …; return; }`); GG-SCHED-0002.

**What's wrong.** If the drainer platform thread is ever interrupted, `qTake()` throws, the catch block re-interrupts and **returns** — the single drainer thread dies, and every parked goroutine hangs forever with no recovery path. Nothing in the design holds a reference that could interrupt it today (`drainerThread` is private), but "nobody does it" is not a mechanism, and the MPSC `qTake` path *explicitly* checks `Thread.interrupted()`. A scheduler whose total failure mode is one stray `interrupt()` call needs a stated robustness property.

**Suggested fix.** Extend GG-SCHED-0002: "the drainer SHALL be non-terminating — it SHALL NOT exit on `InterruptedException` (swallow-and-continue or supervisor-restart), and production SHALL NOT expose the drainer thread to interruptors." Add a fault-injection case to the Phase-2 suite (interrupt the drainer mid-torture; liveness must hold).

### P1-6. SCHED-0002 vs WAKE-0003 specify the same lane-priority discipline twice

**Location:** GG-SCHED-0002 ("admission order SHALL be FIFO runFifo, with the wakeFifo … and initFifo … priority lanes checked first") vs GG-WAKE-0003 ("Woken and channel-ready goroutines SHALL be admitted via the wakeFifo priority lane ahead of runFifo … newly created goroutines SHALL be admitted via the initFifo lane ahead of runFifo").

**What's wrong.** Two SHALLs, one discipline. Divergent evolution risk: a future edit reorders lanes in one and not the other.

**Suggested fix.** Merge: keep the lane *existence + priority order* in exactly one requirement (suggest GG-SCHED-0002, the drainer discipline), and reduce GG-WAKE-0003 to the wakeup-side contract (wakers enqueue WAKEUP tokens; the drainer honors lane priority per GG-SCHED-0002) — or vice versa. Do not leave both stating the order.

### P1-7. GG-FAIR-0003's "bounded number of drainer admissions" is not falsifiable — no bound is given

**Location:** GG-FAIR-0003 statement: "Every runnable goroutine SHALL be admitted within a bounded number of drainer admissions (no indefinite postponement)."

**What's wrong.** "Bounded" with no bound is not a SHALL — no experiment can fail it except literal infinite postponement, which is unobservable in finite time. (And as P0-2 shows, the current design *does* admit indefinite postponement via NIO cap-exhaustion, so the requirement as written is already false under §3.5.)

**Suggested fix.** Give the bound teeth, e.g.: "…within `K × (runnable goroutines)` drainer admissions with K=… (to be calibrated in Phase 2; the 10k-quiescence probe is the first calibration point)" — plus the second clause's concrete probe (`never_scheduled=0`) stays. Then verify against the P0-2 falsifier.

### P1-8. GG-SCHED-0004's statement bans what its rationale permits (NIO carve-out)

**Location:** GG-SCHED-0004 statement ("All blocking … SHALL go through scheduler-aware primitives … raw Java blocking that bypasses the scheduler … is banned") vs rationale ("scheduler-transparent Loom-unmounted NIO is the documented bounded deviation").

**What's wrong.** The statement says *all*; the rationale exempts NIO. A static-audit verification ("audit of adaptation layer for raw blocking calls") cannot pass against a self-contradictory requirement.

**Suggested fix.** Resolve in the same edit as P0-2: either remove the carve-out (recommended — slot-releasing wrapper makes NIO scheduler-aware) and let the statement stand, or write the carve-out into the statement with its exact scope and the P0-2 falsifier as its guard. Do not leave statement and rationale disagreeing.

### P1-9. GG-FAIR-0002 embeds a computed number in the SHALL and duplicates its verification plan in the statement

**Location:** GG-FAIR-0002: "Mean per park/resume hop overhead SHALL be at most 1% of the configured quantum (at the 10ms default: 0.27% computed from the measured 26.8us/hop); the budget SHALL be re-measured on server hardware before any production performance claim."

**What's wrong.** (a) The parenthetical presents the 0.27% *projection* inside the normative sentence — a reader (and a future auditor) will cite the SHALL as establishing 0.27%, which was never measured. The cited evidence (F4) actually *failed* the mean at 1ms. (b) "SHALL be re-measured" in the statement duplicates the verification plan's re-test trigger — process constraint, not requirement.

**Suggested fix.** Statement: "Mean per park/resume hop overhead SHALL be ≤1% of the configured quantum." Move the 0.27% projection to rationale labeled `[ESTIMATE]` (it already is, in prose — keep it out of the statement), and leave re-measurement solely in the verification plan.

### P1-10. All ten new requirements have empty `parent:` — traceability gap to GG-0017/GG-0048

**Location:** All of GG-SCHED-0001–0004, GG-WAKE-0001–0003, GG-FAIR-0001–0003 (`parent: ""`).

**What's wrong.** The requirements form a flat list with no links to the parent requirements they refine (GG-0017 owns the scheduler mechanism; GG-0048 owns the preemption-equivalence bar). The Greenfield registry's value is impact analysis ("retiring/superseding searches the whole codebase"); unparented SHALLs defeat it. GG-0017's own statement also substantially overlaps the new IDs (it restates the token model, Safepoint.poll, FJP-demotion, and the GOMAXPROCS→cap mapping) — acceptable *if* the children point at it.

**Suggested fix.** Set `parent: GG-0017` on the SCHED/WAKE requirements and `parent: GG-0048` on the FAIR requirements (FAIR-0001–0003 refine the equivalence bar's fairness/starvation-freedom observables). Consider trimming GG-0017's statement to the token-model + FJP-demotion core and letting the children own the mechanism detail.

---

## P2 — nits, polish, and honesty notes

### P2-1. `[ESTIMATE]` and `[UNVERIFIED]` are not in the Greenfield protocol's label registry
§3.5 uses both ("the projection is `[ESTIMATE]`", "measured pool-capacity behavior is `[UNVERIFIED]`"); §9's estimate table also uses `[ESTIMATE]`. The protocol (§2 of the skill) enumerates `[HIST]/[VERIFIED]/[PROPOSAL]/[DECIDED]/[OPEN]`. The extensions are *good* (more honest than stretching `[VERIFIED]`), but an unregistered label is a label nobody agreed on. **Fix:** add `[ESTIMATE]` (computed/projected, with stated basis) and `[UNVERIFIED]` (claimed, not yet measured) to the skill's label list, or map them to existing labels.

### P2-2. "Judged host-specific" asserts a cause for the 38µs cold-drainer wakeup without evidence
§3.5 residual (b) and REPORT §3.3: the honest core is "unexplained on the 2-vCPU VM" (thread priority, lock-free queue, spinning all tried, didn't help; minimal repro 4.3µs). "Judged host-specific" promotes a hypothesis to a finding. **Fix:** demote to hypothesis ("suspected host/VM thread-wakeup latency; alternative: systematic deep-sleep wakeup cost of the parked drainer"). Note the arithmetic is robust either way: even a systematic 38µs is 0.38% at 10ms — under budget; the risk is only if it scales *worse* on server hardware, which risk-row-10's kill trigger ("cold-drainer p99 dominating the mean beyond the scoped fix") already covers. Keep the trigger.

### P2-3. The 13× rate gap is not purely "prototype cost" — part of the per-quantum hop is structural
REPORT §2/F1 and §3.5 residual (c): "prototype per-hop cost (~40µs vs Go's ~1µs futex), not a design flaw." Partially true, partially not: a production implementation still pays, per quantum, vthread unmount/remount + cross-thread drainer handoff + FJP resubmission — Go pays an in-P context switch (~100ns-class). That structural delta never goes to zero; production shrinks it, it doesn't eliminate it. **Fix:** reword to "prototype per-hop cost plus a structural per-quantum cross-thread hop that production minimizes but cannot eliminate; the budget argument holds because quanta are long (10ms), not because the hop goes away." The existing "no production performance claim until re-measurement" disclaimer is correct — keep it prominent.

### P2-4. Garbled prose: "matching Go's default NumCPU with no work"
§3.5: "The default cap is available processors, matching Go's default NumCPU with no work `[VERIFIED]` on the default mapping." "with no work" is meaningless here. **Fix:** reword ("matching Go's default `GOMAXPROCS=NumCPU`").

### P2-5. Report's pre-fix quiescence number (2,768) doesn't match the archived log (2,643)
REPORT §3.2: "2,768 of 10,000 new goroutines never scheduled before the fix"; `logs/quiescence_10k.log`: `never_scheduled=2643`. Minor, but the reviewer's job includes this: quote the archived number. (Moots once P0-1's re-run lands.)

### P2-6. Unstated assumption: the default Loom FJP scheduler
The bounded-submissions argument assumes the default virtual-thread scheduler (`jdk.virtualThreadScheduler` unset). A custom scheduler (e.g., a single-threaded or unfair one) voids every FJP-related claim. **Fix:** one sentence in §3.5 — "graalgo requires the default Loom carrier scheduler; custom `jdk.virtualThreadScheduler` settings are unsupported (documented restriction)."

### P2-7. MPSC queue path is dead code in the battery
`MpscQueue` ("Not for production use") is only activated by `-Dusched.mpsc`, which `run.sh` never sets; the INIT cross-queue wakeup path (`taker` protocol) is therefore untested. That's fine for a diagnostic — but say so, and ensure no production design inherits the `taker`-nulling race without review.

### P2-8. `awaitAck` busy-spins if the vthread is interrupted
`while (g.acked.get() < t) LockSupport.park();` — a pending interrupt makes `park()` return immediately without clearing the flag, turning the wait into a spin until admission. Not a correctness bug (no wakeup lost), but a production robustness wart. **Fix:** clear-and-restore interrupt status around the park, or document "vthreads in scheduler parks are never interrupted" as an invariant.

### P2-9. Interleavings the F6 stress test does *not* cover (scope for the Phase-2 model-checking gate)
The pairing protocol itself is sound (see "what survived" below), but for the record, F6's randomized coverage does not deterministically exercise: (a) `close()` waking parked *senders* via the `aborted` path while receivers drain; (b) a stale-ticket `wakeup()` racing re-arm to a fresh ticket (safe by CAS + monotonic generations — analyzed, not stress-targeted); (c) select's in-flight stale `SelSignal` vs re-registration (safe by `claimed` CAS ordering — analyzed, not targeted); (d) INIT lane under WAKEUP flood (F3 covered RESUME flood only); (e) `cap=1` channel-wakeup liveness (O5's original deadlock config was *worked around* by changing the test, which is legitimate per GG-SCHED-0004's ban, but the ban's enforcement is a static audit that doesn't exist yet); (f) WAKEUP-before-IDLE with generation skew under drainer batching. **Fix:** the GG-WAKE-0002 "formal model-checking" gate is the right gate — but write its scope explicitly as this list, modeled at the *protocol* level (events + drainer state machine; small enough for TLA+/JPF), not just "re-run stress harder."

### P2-10. GG-0017/GG-0048 revision history is fine — keep the pattern
Both carry `created` + `revised` history entries, no ID reuse, still `draft`. Pre-acceptance revision of unlocked drafts is protocol-compliant. No action; noting as checked.

---

## What survived the attack (for the record)

An adversarial review should state what it failed to break:

1. **The park/drainer mechanism genuinely closes the spike-2 kill mechanism.** Spike 2 died on unbounded yield-resubmission into FJP queues under FJP scheduling discipline. Option C's indefinite park + explicit drainer admission removes the vthread from FJP contention entirely; outstanding FJP submissions are bounded by `cap` (modulo P1-1/P1-2). The O1 binary gate (15,603 vs 0 samples, log-verified) is the right falsifier and it passed.
2. **The park/unpark pairing argument is sound.** I traced stale tickets vs re-arm (monotonic `parkSeq` + `armed` CAS ⇒ stale wakeups can't match new generations), duplicate admission (one event → one `RunnableReq`; at-most-once `wakeup` CAS), WAKEUP/IDLE in both orders (`wakePending`/`idleSet` matching with stale-generation self-clean), select's multi-channel `claimed` CAS, and the acked-before-unpark ordering with `LockSupport` permit semantics (the "reversing loses wakeups" comment is exactly right). The `admissions == parks` invariant held at 50,558/50,558 in the archived log.
3. **The 10ms quantum choice is well-reasoned** (matches Go's sysmon preemption granularity; wakeup latency is quantum-independent via `wakeFifo`) — it just needs the P1-3 measurement.
4. **Honesty infrastructure is mostly working:** `[ESTIMATE]` on the 0.27% projection, the residual-risks list keeping both overhead numbers, the "tested interleavings only" caveat on wakeups, verdict scoping in REPORT Appendix B, and risk-row-10's kill trigger for the cold-drainer unknown are all the right instincts. The P0s are gaps in *applying* that infrastructure (unarchived re-run; an untested falsifier for the NIO deviation), not in its design.

---

## Overall judgment

1. **Does the spike-2b PASS verdict stand? — Conditionally.** The *mechanism* verdict (user-space park/drainer eliminates the yield-to-FJP starvation; cap assert holds; no wakeups lost under stress) survived adversarial review of the protocol and is supported by archived logs for F1, F4 (mean-fire is the honest trigger), F5, and F6. **But** the verdict as *evidenced* does not stand: P0-1 (no archived post-fix logs for F2-O5/F3-quiescence; checksummed sources postdate every log) means two falsifiers' PASS claims are currently unverified provenance. Re-running the battery (~4 min) and archiving is necessary and sufficient to close this.
2. **Does §3.5 describe a committable design? — Not as written.** P0-2 (scheduler-transparent NIO with "no starvation risk") is a load-bearing false claim that contradicts §7.2(ii) and GG-FAIR-0003 and breaks the standard Go server pattern; it must be reworked (recommended: slot-releasing NIO wrapper per GG-SCHED-0004's own rule, plus the cap-exhaustion falsifier) before the ADR-0001 D5 sign-off. P1-1 (clamp effective cap to carrier count) and P1-5 (drainer liveness) are also commit-blockers in the weaker sense — they must be in the requirements before Phase 2, though they don't overturn the mechanism verdict.
3. **The Phase-2 model-checking gate (GG-WAKE-0002) is adequate** for the residual pairing risk, provided its scope is written as the P2-9 interleaving list — the current "tested interleavings only, no formal verification" caveat is honest and should be preserved verbatim until the gate is discharged.

**Bottom line:** option C remains the right architecture and the spike validated the right thing, but the rework is not yet committable: fix the evidence archive (P0-1), fix the NIO slot-accounting hole (P0-2), then the P1s, then sign.
