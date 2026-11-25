# P2-M6 Review — Reviewer 2 (methodology / testing / documentation)

**Scope:** graalgo Phase-2 milestone P2-M6 — JFR goroutine/channel events
(new `graalgo.jfr` package: 4 events + `JfrEvents` facade; scheduler,
channel, timer hookups; `JfrEventsTest` 5 tests; evidence
`phases/phase2/hidden_files/jfr/EVENTS.md` + `p2-m6-evidence.jfr`;
`DIV-M6-1` in `phases/phase2/DIVERGENCES.md`). Read-only review; nothing
modified.

**Snapshot note:** the brief named commit `d61a340`, but master HEAD is
`4a9f393` ("P2-M6: remediate reviewer1 (P1 io double-count, emitQuietly
isolation, doc fixes)", 2026-10-05 16:47 UTC), a child of `d61a340`.
Reviewer 1 already did a pass and their fixes landed. I reviewed **HEAD
(`4a9f393`)** — the tree the sign-off would actually cover — and call out
where reviewer 1's remediation is verified vs. where it introduced new
issues. All empirical checks below were run against HEAD with the pinned
toolchain (GraalVM 25.0.4.1.1, `make test`, 2-core VM).

**Verdict: GO-WITH-FINDINGS.** The instrumentation is sound, the evidence
is real and verified, the red-green story checks out, and reviewer 1's
remediation is confirmed working. But there are two P1s that should be
fixed before sign-off (a doc falsehood about the evidence file, and a
timing-unsound test assertion that flakes), plus a P1 honesty fix to
DIV-M6-1 and a P1 unreproduced performance claim. Details below.

---

## Verified (things I checked independently, not taken on trust)

1. **Evidence-file counts match EVENTS.md.** `jfr summary` on
   `p2-m6-evidence.jfr`: `graalgo.GoroutineStart`=4,
   `graalgo.GoroutineEnd`=4, `graalgo.GoroutinePark`=4,
   `graalgo.ChannelBlock`=3, `jdk.VirtualThreadStart`=4. All as documented.
2. **Park durations match and make sense.** `jfr print`:
   3× CHANNEL at 274.0ms / 5.08ms / 8.38ms, 1× TIMER at 400.6ms —
   matching EVENTS.md's "274ms, 5ms, 8ms; 400ms". Plausible for the
   recorded workload (delayed sender, 400ms sleep).
3. **Start/end pairing holds.** Starts {1,2,3,4}, ends {4,2,1,3} — no
   orphans. Lifetimes sensible (35.9ms yielder … 401.5ms for the 400ms
   sleeper).
4. **Red-green for `testNoEventsWhenDisabled` is genuine.** I wrote a
   standalone probe: start a bare `Recording`, *then* lazily register a
   custom event class (mirroring how the graalgo hookups load event
   classes mid-workload — the test deliberately does *not* call
   `ensureRegistered()`). Pre-fix state (no `@Enabled`): the running
   recording captured all 3 emitted events → test would FAIL (red).
   Post-fix (`@Enabled(false)`): 0 captured → PASS (green). The
   checkpoint's "caught by testNoEventsWhenDisabled (red)" claim is TRUE,
   and the test is non-vacuous. Caveat: it depends on the JDK picking up
   lazily-registered default-enabled types into a running recording
   (verified on GraalVM 25; see finding P2-5).
5. **`testHotLoopParkFilter` determinism is real.** 5/5 replicated runs of
   the exact workload produced exactly 16 `QUANTUM` park events (seq
   1024/2048/3072/4096 × 4 goroutines — the mask logic is exactly as
   documented). The 12..20 band (reviewer 1 tightened it from 8..32) is
   honest headroom, not doing the work.
6. **The "measured 26%" is reproducible.** EVENTS.md's "measured, not
   assumed" claim: with 4 concurrent yielders on 1 carrier (the test's
   config) I measured 24.3% of quantum-park round-trips ≥100µs
   (p50=76µs — drainer contention dominates). Single-yielder gives 1.9%,
   so the number is workload-shaped, but the design motivation (a pure
   duration threshold *cannot* filter quantum parks under contention) is
   validated.
7. **Full suite green:** `make test` → 157/157 PASS, three consecutive
   runs at HEAD. No regressions from the hookups in other test classes.
8. **Reviewer 1's remediation verified:** IO double-count removed
   (`notePark` gone from `ioEnter`/`ioExit`; `ioBlocked` is now the
   single authoritative IO event with a clear comment in
   `SchedulerCore.java:176-186`); `emitQuietly` + `droppedEmits`
   counter added (`JfrEvents.java:48-72`); stale duplicated "Rate
   contract" javadoc paragraph removed; hot-loop band tightened.

---

## Findings

### P1-1. `testEventsFlow`'s `dur >= 200ms` assertion is timing-unsound and flakes

`JfrEventsTest.java:180,183` asserts every CHANNEL/TIMER park lasts
≥200ms, on the theory that g2's 250ms sleep is fully inside g1's park.
Across 9 fresh-JVM runs of the test I observed failures with CHANNEL
park durations of 195, 182, 178, 165, 157, 115, 64, 5.6, 3.8 and
0.25ms — i.e. ~40% flake rate ("CHANNEL park too short"). It passes
reliably inside `make test` only because 150+ prior tests warm the JVM.

Two verified mechanisms, both in the test workload (the JFR hook
measures the true notePark→parkResumed interval faithfully — the
instrumentation is not the bug):

1. **The waiter-visibility probe is pathological on virtual threads.**
   g2 spins on `while (recvWaitersCount()==0) Thread.yield();`. On a bare
   vthread `Thread.yield()` costs 9.4µs, but in this workload I measured
   34 iterations over 1242ms (~36ms/yield). Root cause: this VM has
   **2 cores**; `awaitAck` parks vthreads with `LockSupport.park()`,
   which pins carriers, so parked goroutines (g1, g3) hold the only
   carriers and g2's yielded continuations queue behind FJP compensation
   (tens of ms each). Observed probe delays: 0ms to 4.4s for the same
   workload. The 250ms sleep anchor is therefore unpredictable, and the
   50ms slack (200 vs 250ms) is nowhere near enough.
2. **The measured park can end long before `recv` returns.** In one run:
   g2 sent at +1659ms, g1's recv returned at +1694ms with the correct
   value, but the committed CHANNEL park was 5.6ms ending at +278ms —
   i.e. the drainer re-admitted g1 ~1.4s before the handoff, and g1 then
   stalled between `awaitAck` return and `recv` return (Java-monitor
   contention with g2's `synchronized` spin loop under carrier
   starvation fits the timeline). No UNPARK could have existed at +278ms
   (g2 sent at +1659ms), so this early re-admission also deserves a look
   by the scheduler owner — see open question below. Either way, "park
   duration ≥ 200ms" is not a stable property of this workload.

**Recommendation (milestone-scoped):** replace the yield-spin probe with
a sleep-paced poll (or a real handshake), and replace the absolute
200ms bound with a magnitude check that tolerates the observed jitter
(the `>= PARK_THRESHOLD_NANOS` check at line 175 already guards the
filter; a separate loose lower bound like 50ms still catches unit bugs
such as ms-vs-ns confusion without encoding a wall-clock prediction).
The TIMER assertion at line 183 has the same 50ms-slack fragility.

**Open question for the scheduler owner (not JFR's bug):** what
re-admitted a PARKED goroutine at +278ms with no matching UNPARK in
flight? The drainer's PARKED path (`Drainer.java:141-152`) only
re-admits on UNPARK, and the timer fires `unpark` on the correct handle
(`TimerHeap.java:412`). On this 2-core VM with carrier pinning, timing
is extremely distorted, so this may be a benign artifact — but
`GoChan.recv` has no re-park loop, so a genuine spurious wakeup would
return a null value. Worth 30 minutes with the drainer, not a JFR fix.

### P1-2. EVENTS.md misreports its own evidence file

`EVENTS.md:78`: "`graalgo.ChannelBlock` | 3 (all RECV; the sends
completed via direct handoff and correctly emit nothing)". The file
actually contains **2 RECV + 1 SEND** (goroutine 2, channel 1, at
16:36:11.652, followed by a 5.08ms CHANNEL park — a genuine 5ms block).
The hookup code (`GoChan.java:303-314`) emits SEND only on the true
blocking path after `trySend` fails, so the SEND is legitimate behavior
under a racy workload, not an instrumentation bug — but the doc
describes the intended workload instead of the actual recording, and
its "correctly emit nothing" narrative is false for this file. Fix the
doc to describe what's in the file (or regenerate the file).

### P1-3. DIV-M6-1 over-claims the no-mapping rationale

`DIVERGENCES.md:171-184` characterizes `runtime/trace` as exposing "the
gc runtime's internal scheduler states (P, M, G transitions, STW
phases)" which "have no meaningful mapping onto a JVM-hosted user-space
scheduler". That understates the package: `runtime/trace`'s headline
events are goroutine/channel/mutex-level (`GoStart/GoEnd/GoBlockSend/
GoBlockRecv/GoBlockSelect/GoUnblock`), which map *directly* — this
milestone IS that mapping under JFR-native names. What truly has no
counterpart is P/M/STW machine state plus the trace binary format and
the `go tool trace` UI. The current wording could mislead a reader into
thinking no mapping was possible. Also, "Go programs using
`runtime/trace` observe nothing (the package is not in the supported
surface)" is ambiguous: if the package can't be imported, programs fail
to build loudly — "observe nothing" reads like a silent no-op. Tighten
both sentences.

### P1-4. The "~600ns `Event.isEnabled()`" claim does not reproduce

The enablement cache (`JfrEvents.java:110-130`, `parkEnabledCache`,
per-handle first-park refresh, documented staleness) is motivated by
"`Event.isEnabled()` costs ~600ns on this JVM (it is not a field read)"
— now repeated in 5 files after reviewer 1's edit
(`JfrEvents.java:13,118`; `*Event.java:35-41`; `EVENTS.md:46`). Measured
on the pinned toolchain: **3.5ns** (no recording), **5.9ns** (active
recording); on stock OpenJDK 21: 8ns / 26ns; fully cold (no warmup):
24–70ns. Nowhere near 600ns in any configuration I could construct. The
caching is functionally sound (I see no correctness issue), but it is
non-trivial complexity (shared static volatile, cross-goroutine
staleness semantics) justified by a measurement that doesn't reproduce
— and the pre-reviewer-1 comment ("a single field read") was actually
closer to the truth. Either produce the 600ns measurement with its
methodology (JVM, flags, harness) or soften the claim; do not keep
asserting a number that two JVMs contradict. (The disabled-guard
*contract* — `< 2× nanoTime`, measured 36ns vs 53ns here — is fine and
unaffected.)

### P2-1. `jfr start --events graalgo.*` is not a real command

`EVENTS.md:33` and `JfrEvents.java:32`: the `jfr` tool has no `start`
subcommand, and `jcmd <pid> JFR.start` (verified via `jcmd help
JFR.start` on GraalVM 25) has no `--events` flag — event selection is
via `settings=<file>.jfc` or the `event-setting=value` syntax
(`jcmd <pid> JFR.start '+graalgo.GoroutineStart#enabled=true' …`).
Replace with a working recipe; the Java-API snippet above it is correct.

### P2-2. Inconsistent overhead numbers between doc and code

`EVENTS.md` says the disabled guard costs "60ns/park, 0.63× nanoTime";
`JfrEvents.java:10` says "~10ns"; I measured 36ns / 0.68× on this VM.
Same order of magnitude, but the class javadoc's "~10ns (a bit-test …
two volatile writes)" also undercounts the actual path (3 volatile
reads + ~4 volatile writes across `notePark`/`parkResumed`). Pick the
measured number, record the machine, drop the "~10ns".

### P2-3. SPEC §3.10 doesn't list `graalgo.GoroutinePark`

SPEC §3.10's custom-event list (`graalgo.GoroutineStart/End`,
`graalgo.ChannelBlock`, `jdk.GC*`, `graalgo.ModuleCrash`) omits the
milestone's fourth event. Either add it to §3.10 or record the addition
as a divergence. (GG-0008 itself needs no change — the slice is within
its statement.)

### P2-4. Rate-control gaps are unacknowledged

The "Rate control (measured, not assumed)" section covers parks and
channel fast-paths, but: (a) **`GoroutineStart/End` are unthrottled** —
a spawn storm emits 2 events per goroutine with no sampling or budget;
(b) **blocking channel paths have no sampling** — N goroutines doing
blocking ping-pong emit 2 events per iteration per goroutine with no
backpressure; (c) **1-in-1024 is not derived from a stated budget** —
at hot-loop rates (~130k parks/s/goroutine at 7.6µs) it yields
~130 events/s/goroutine, which is reasonable, but the doc should state
the target rate rather than presenting the constant as principled. One
paragraph on residual risk would close this honestly.

### P2-5. `testNoEventsWhenDisabled` leans on undocumented JDK behavior

The test is genuinely red-green (verified above), but it depends on a
running `Recording` picking up *lazily-registered* default-enabled
event types — behavior I verified on GraalVM 25 but which isn't part of
any contract the test cites. Calling `JfrEvents.ensureRegistered()`
before `rec.start()` would preserve the red-green property (bare
recording + missing `@Enabled(false)` still leaks) while removing the
JDK-behavior dependency. Strict improvement, no downside.

### P2-6. `testYieldThroughputSmoke` is a weak tripwire with flake risk

Documented as noisy, and it is: I measured ratio 1.20 (enabled
* faster than disabled). The 0.80 bound has maybe 1–2σ of margin
against the observed ±20% noise on this 2-core VM. Consider
median-of-3 or demoting to advisory (print, no assert) — as written it
can false-fail on a loaded machine.

### P2-7. `ioBlocked` timestamps the park at exit, not across the syscall

`JfrEvents.java:199-204` emits `GoroutinePark(reason=IO,
duration=fullSyscall)` at `ioExit` time, so in JMC the interval renders
at the exit instant rather than spanning the syscall. A `begin()` at
`ioEnter` / `commit()` at `ioExit` would be accurate (one allocation on
the non-hot IO path). Minor semantic wart; the duration value itself is
correct.

### P2-8. Provenance nits

- The "measured 26%" has no recorded artifact (script/note). I
  reproduced 24.3% with the test's exact contention config — worth one
  line stating the conditions, or the next reader can't tell what "26%"
  was measured under.
- `trace.WithRegion`-style span annotations: a gc user might expect them;
  presumably out of scope — one line in DIV-M6-1 would close it.
- Checkpoint: the P2-M6 START/progress entries are still uncommitted
  working notes and reference only 6f8ca2b/401132b/1c5a03d; no DONE
  entry yet (expected mid-review). Refs check out against real commits.

---

## Answers to the brief's key questions

1. **Non-tautological?** Yes, all five can fail — and `testEventsFlow`
   demonstrably does (though for timing reasons, see P1-1, not for JFR
   defects). `testNoEventsWhenDisabled`'s red-green story is verified
   genuine via an independent probe. `testHotLoopParkFilter`'s
   "deterministic 16" is real (5/5 exact), not band-assisted.
2. **EVENTS.md numbers vs the file:** counts, durations, and
   start/end pairing all verify — except the ChannelBlock characterization
   (P1-2: 2 RECV + 1 SEND, not "all RECV"). Park durations make sense
   for the workload.
3. **Overhead honesty:** the `< 2× nanoTime` bar is a reasonable,
   machine-independent regression tripwire and the test is honest about
   what it is. The throughput smoke test is pure noise, honestly
   labeled as such. The "~10ns" class-doc claim and the "~600ns"
   premise behind the caching are the dishonest numbers (P1-4, P2-2).
4. **Rate control:** principled where it counts (threshold at 10–20× the
   uncontended quantum round-trip; sampling because the threshold provably
   fails under contention — reproduced at 24.3%). 1-in-1024 itself is
   reasonable-but-arbitrary (no stated event-rate budget), and
   start/end + blocking paths are unthrottled (P2-4).
5. **DIV-M6-1:** boundary is in the right place (JFR is the JVM-native
   surface; `go tool trace` compat correctly disclaimed), but the
   rationale over-claims (P1-3).
6. **Doc consistency:** field/event/property names match the code;
   checkpoint refs check out; the two doc falsehoods are P1-2
   (`jfr start --events`) and P2-1, plus the SPEC §3.10 gap (P2-3).
7. **gc-user expectations:** `go tool trace` disclaimed ✓; pprof-block-profile
   translation is SPEC-planned future work (fine for M6); the
   `runtime/trace` *API* surface status wording needs tightening (P1-3);
   span annotations unacknowledged (P2-8).

## Suggested sign-off conditions

1. Fix P1-1 (deterministic probe and/or drop the absolute 200ms bound;
   scheduler owner to eyeball the +278ms early re-admission).
2. Fix P1-2 (EVENTS.md describes the actual file).
3. Fix P1-3 (honest DIV-M6-1 wording).
4. Resolve P1-4 (evidence the 600ns claim or soften it — five files now
   assert it).
5. P2s tracked or fixed at the owner's discretion; P2-4 (rate-control
   residual risk) deserves at least a doc paragraph.
