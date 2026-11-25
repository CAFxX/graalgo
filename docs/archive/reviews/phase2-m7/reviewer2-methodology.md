# P2-M7 Review — Reviewer 2 (methodology)

**Scope:** graalgo Phase-2 milestone P2-M7 — pinning audit (WS G, GG-0057).
Commits `a1c2673`..`9828c4a` vs `e332dbf`; evidence
`phases/phase2/hidden_files/pinning/AUDIT.md` + `pinaudit-report.md`;
tool `graalgo.sched.audit.PinAudit` (methodology only — reviewer 1 covers
line-by-line code). Read-only review; nothing modified.

**Snapshot note:** I reviewed **HEAD
(`9828c4aeb6e5a2ed4afefa86001de903d6e1580e`)**, the tree the sign-off
would cover. All pinning-related files are committed and clean
(`git status` shows no modifications under `hidden_files/pinning/`,
`sched/audit/`, or `jfr/`). All empirical checks below were run against
HEAD with the pinned toolchain (GraalVM 25.0.4.1.1).

**Verdict: GO-WITH-FINDINGS.** The methodology is sound, the tool is
genuine, the positive control is real, the root-cause mechanism is
verified, the fix is correct and deterministic, and I independently
re-ran the audit's `--tests` leg at HEAD to 0 pins / PASS. But there are
two P1 doc-honesty findings: the audit does not acknowledge that the
found pin source was *intermittent* (so the re-audit "0" is weaker
evidence than the report implies), and the skip list conflates programs
that never ran with programs that ran and panicked (so "186 programs,
0 pins" overstates coverage). Neither blocks the gate — both are fixed
with AUDIT.md amendments, not new runs.

---

## Verified (independently checked, not taken on trust)

1. **Corpus counts check out.** 148 `*.go` under `../phase1/corpus`, 37
   under `corpus/`; +1 `SchedTestRunner` = 186. The report lists 39
   skipped entries (counted), not 40 — the brief's "40" is off by one;
   the report is authoritative.
2. **Positive control is a genuine recording.** `jfr summary` /
   `jfr print` on `src/test/resources/pinproof.jfr` (recorded 2026-10-05
   17:41 UTC): exactly 1 `jdk.VirtualThreadPinned`, thread `pin-jni-vt`,
   `pinnedReason="Native or VM frame on stack"`, stack
   `LockSupport.parkNanos → VirtualThread.parkNanos →
   parkOnCarrierThread`. Matches `PinAuditTest`'s assertions.
3. **Root-cause mechanism verified (JLS 12.4.1).** Scratch probe on
   GraalVM 25: referencing `Foo.class` does NOT run `<clinit>`; calling
   `Foo.enabled()` (subclass-declared static) DOES. The AUDIT's
   "class literals do not trigger `<clinit>`" claim is correct.
4. **Red-green mechanism verified both halves.** Scratch probe against
   built classes: after class-literal-only reference (pre-`0dd2e2f`
   behavior), `FlightRecorder` shows **0** graalgo event types; after
   `JfrEvents.ensureRegistered()` (fixed behavior), **4/4** types
   registered — on the calling thread, deterministically. The fix does
   what the report says it does.
5. **JEP 491 surface reduction verified.** Virtual thread parking 200ms
   inside `synchronized` on GraalVM 25: `-Djdk.tracePinnedThreads=full`
   silent AND 0 `jdk.VirtualThreadPinned` in a JFR recording with the
   event enabled. AUDIT §2's claim reproduces.
6. **The event is not throttled.** `jfr metadata` shows no `@Throttle`
   on `jdk.VirtualThreadPinned` — every park-while-pinned is recorded,
   so "0 pins" is not a sampling artifact.
7. **Skip categories verified by running them.** `ch01_unbuffered.go` →
   exit 2, build failure: `gofrontend: *ast.SendStmt is not supported in
   frontend/v1 scope (WS E/F)`. `pn04_panic_string.go` → exit 2,
   genuine Go panic (`panic: boom`). Two distinct skip classes, as
   suspected.
8. **The event's blind spot is real (verified).** A 2s blocking FFM
   `sleep(2)` downcall on a virtual thread emits **0**
   `jdk.VirtualThreadPinned` events — no park, no event. AUDIT §2
   reports this honestly; it means the dynamic leg cannot see
   pin-without-park, and the +0 budget for that class rests entirely on
   the static grep gate (see P2-1).
9. **`ioEnter`/`ioExit` have no production callers.** Only the
   definitions in `SchedulerCore`, comments in `Drainer`/`Netpoll`
   javadoc, and test callers (`IoSlotsTest`, `TimerTest`,
   `JfrEventsTest`). AUDIT §5's claim holds.
10. **Final state re-verified by me.** Ran `PinAudit --tests --gmp 2`
    at HEAD (the same leg `make check`'s smoke target runs): 0 pins,
    `AUDIT_RESULT=PASS`.
11. **Single park choke point.** All goroutine parking funnels through
    `SchedulerCore.awaitAck → LockSupport.park()` (one call site). The
    static audit's park-site enumeration is complete by construction —
    methodologically strong.
12. **The `--tests` leg genuinely exercises pin-relevant paths.**
    `ChannelTest`/`ChannelDifferentialTest` (blocking send/recv,
    including `testBlockedSendDoesNotPinCarrier`), select coverage,
    `TimerTest`/`TimerDifferentialTest`, GC tests
    (`GcInterop/Differential/Torture/VisibilityTest`). `SchedTestRunner`
    is not in the skip list → exit 0.

---

## Findings

### P1-1. The found pin source was intermittent; the report doesn't say so, and the raw stacks aren't preserved

Pre-fix, `ensureRegistered()` was **dead code** (verified: no callers at
`0dd2e2f^`). So in every one of the 186 child JVMs, the first
`GoroutineStartEvent.enabled()` call on a goroutine carrier ran
`<clinit>` → `FlightRecorder.register` — roughly 186 Start + 186 End +
Park/ChannelBlock registrations on carriers. Yet only **13 pins**
fired (~2–4% per registration). The pin-on-register is intermittent
(plausibly a JVM-startup race in JFR subsystem init: the park inside
`register` only happens if the recorder thread isn't up yet — which
would also mean these "pins" were benign startup noise, never carrier
contention; but without the stacks this is hypothesis, not fact).

Two consequences:

1. **The re-audit "0 pins" is weaker evidence than AUDIT §4/§6
   implies.** For an intermittent source, a single clean re-run could
   be luck. What actually proves the found source is gone is the
   *deterministic* fix (verified above: 4/4 types registered on the
   startup thread, every time) plus `JfrRegistrationTest` asserting the
   post-condition — not the re-audit count. The report should say this
   explicitly.
2. **The raw 13-pin stacks are not preserved anywhere.**
   `pinaudit-report.md` only records the final 0-pin run; the stacks
   quoted in AUDIT §4 exist only as prose. For a falsifier-gate
   ("unexpected pin → red"), the found-and-fixed evidence should be
   archived. If the stacks are unrecoverable, say so and lean on (1).

**Recommendation (doc-only, no new runs):** amend AUDIT §4 to (a)
acknowledge the intermittency and what it implies for re-audit
strength, (b) state that the determinism of the fix (probe +
`JfrRegistrationTest`) is the proof for this source, and (c) either
attach the raw pre-fix pin stacks or record that they were not
preserved.

### P1-2. The skip list conflates "never ran" with "ran and panicked"; "186 programs, 0 pins" overstates coverage

Of the 39 skipped: **26 are build failures** (all 12 `corpus/channels/`
+ all 14 `corpus/timers/` — frontend v1 doesn't support
channels/timers/select yet, verified in §V.7). For these,
`SchedMain` returns before `SchedulerCore` is even constructed: no
goroutines, no parks, no scheduler at all. Their "0 pins" is vacuous.
**13 are genuine panics** (12 `panic/` + `db05`), which do run and
contribute partial coverage. The report's single "Skipped (non-zero
exit, pin count still collected)" bucket plus the "186 programs, 0
pins" headline invites reading all 186 as coverage.

**Recommendation (doc-only):** split the skip list into
"did not run (build failure — no pin evidence)" vs "ran, exited
non-zero (partial pin evidence)". The honest headline is: 147 clean
runs + 13 panic-runs + 1 tests-leg (exit 0) with 0 pins; 26 programs
contribute no pin evidence. This also makes the real gap visible:
**zero Go-source coverage of channel/select/timer park paths** — all of
it comes from the Java-translated tests leg (see P2-3).

### P2-1. Residual: pin-without-park is invisible to the dynamic leg; say so in §1/§5, not just §2

Verified in §V.8: a blocking FFM downcall pins the carrier with no
park and no event. The methodology section (§1) describes the event as
the detector without stating its exact boundary (park-while-pinned
only). The +0 FFM/file-I/O budget is justified *today* (no `native`
methods, no `java.lang.foreign` in phase1+phase2 `src/main` — the
`make check` grep gate I confirmed is wired in — and no production
`ioEnter` callers), but the dynamic audit contributes nothing to that
claim. One paragraph in §1/§5 making the division of labor explicit
(dynamic leg ⇒ park-while-pinned; grep gate ⇒ no-park class) closes
this honestly. Note the grep gate covers only `src/main`, not
third-party jars — acceptable since any vthread pin *with* a park
would still trip the dynamic leg, but worth one line.

### P2-2. `9828c4a`'s corrupt-JFR→skipped change is mildly fail-open

A systemic recording failure (bad flags, disk-full temp dir, JVM bug)
now yields "0 pins + long skip list", exit 0. Zero occurrences in the
final run, and the skip list is in the report, so this is not a live
problem — but consider a skip-fraction guard (e.g. fail if >5% of
recordings are unreadable, or if the tests leg is unreadable).

### P2-3. Tie re-audit to frontend channel/timer/select support

When the frontend learns channels/timers/select, the 26 build-failure
programs will start executing genuinely new park paths (Go-source
driven `GoChan`/`SelectOp`, not just the Java-translated tests).
`make check`'s smoke leg (`--tests` only) will NOT catch a regression
there; only the full `make pin-audit` will. Record in AUDIT.md that
landing frontend channel support must be followed by a full
`make pin-audit` re-run before sign-off of whatever milestone lands it.

### P2-4. Nit: brief/report skip-count mismatch

The review brief says "40 SKIPPED"; the report lists 39. The report is
authoritative; no action beyond not propagating the "40".

---

## Answers to the brief's key questions

1. **Methodology sound?** Yes. `jdk.VirtualThreadPinned` semantics are
   exactly as the report states (park-while-pinned); it is unthrottled
   (§V.6); the JEP 491 surface reduction is empirically confirmed
   (§V.5); the positive control is a genuine single-event recording
   (§V.2). The known blind spot (pin-without-park, §V.8) is real but
   disclosed, and its control (grep gate + +0 budget) is coherent.
2. **Corpus coverage adequate?** For the current surface, yes — but
   only via the `--tests` leg for the pin-relevant paths. The 148
   single-goroutine phase-1 programs exercise spawn/start/end/drainer
   quiescence (and did catch the real bug). The 26 channel/timer Go
   programs contribute nothing (build failures); the 13 panic programs
   contribute partial coverage. See P1-2 for the honest accounting.
3. **Classification defensible?** Yes. Fix-not-allowlist was the right
   call: the pins were an artifact of the audit's own observability
   (JFR registration), the fix deterministically eliminates the source
   (§V.4) rather than spending budget, and detection is unsuppressed.
   The +0 FFM budget is justified: no native/FFM surface in
   `src/main`, no production `ioEnter` callers (§V.9), netpoller
   deleted (gate in `make check`).
4. **Skipped programs a problem?** The 26 build-failures are a coverage
   gap (not a methodology flaw — the frontend simply doesn't support
   those constructs yet); the 13 panics are fine (partial evidence,
   honestly collected). "Pin count collected" is true but §P1-2's
   relabeling is needed.
5. **Evidence complete for done-gate 2?** The gate's letter is met
   (0 unexpected pins on the corpus as run; I re-ran the tests leg to
   PASS at HEAD). For sign-off I want P1-1 and P1-2 addressed as
   AUDIT.md amendments — honesty fixes, not new runs. The one
   unrecoverable item is the raw 13-pin stacks; the regression test +
   deterministic fix cover the loss.
6. **AUDIT.md accuracy.** §1 matches the tool's actual behavior;
   §2's JEP 491 claims reproduce (§V.5) and the FFM control reproduces
   (§V.8); §3's park enumeration is complete by construction (single
   `awaitAck` choke point, §V.11); §5's claims hold (§V.9); §6's
   numbers match the report. Gaps are P1-1 (intermittency
   unacknowledged, stacks unpreserved) and P1-2 (skip accounting).

## Suggested sign-off conditions

1. Amend AUDIT.md per P1-1 (intermittency acknowledged; determinism of
   the fix stated as the proof; raw stacks attached or their loss
   recorded).
2. Amend `pinaudit-report.md` / AUDIT.md per P1-2 (split skip
   categories; honest coverage headline).
3. P2-1–P2-3 tracked at the owner's discretion (one paragraph + one
   re-audit trigger note); P2-4 is a nit.

**Commit reviewed:** `9828c4aeb6e5a2ed4afefa86001de903d6e1580e` (HEAD).
