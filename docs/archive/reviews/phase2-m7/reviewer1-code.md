# P2-M7 review — reviewer 1 (independent, read-only)

Snapshot: `9828c4a` (branch master, `~/workspace/graalgo`). Scope: the seven
P2-M7 commits `a1c2673..9828c4a` vs `e332dbf` — new
`graalgo.sched.audit.PinAudit`, `JfrEvents.ensureRegistered()` fix
(class literals → per-class `enabled()`), `SchedulerCore` constructor
hookup, `JfrRegistrationTest` + `PinAuditTest` (new), `JfrEventsTest`
timing-test skip, Makefile pin-surface grep gate + pin-audit smoke in
`make check` + `make pin-audit` target, `hidden_files/pinning/AUDIT.md`.

**Verdict: GO-WITH-FINDINGS** — 0 P0, 1 P1, 7 P2. The fix is correct and
empirically verified; the audit tool detects genuine pins (proven on a
real fixture and on this JDK); the fail-closed classification is right.
The single P1 is a fail-open in the `make check` smoke: a child timeout
or unreadable JFR yields a vacuous PASS, and the suite currently runs at
~4m16s against a 5-minute timeout — the margin is thin enough that this
is not theoretical. No finding touches scheduler safety: the
`<clinit>`-forcing calls run once on the startup thread and cannot change
scheduling behavior.

Method: read every new/changed file and the full AUDIT.md, traced all
`new SchedulerCore` sites (constructor never runs on a vthread),
enumerated every `extends Event` class in the repo (exactly 4, all
covered), spot-checked the §3 static claims (GoChan/SelectOp park
placement, blocking-site thread attribution, synchronized sites), tested
the grep gate against adversarial declarations, and ran four empirical
probes on the pinned GraalVM 25 (`/tmp/pinprobe`, `/tmp/redtest`,
throwaway — repo untouched).

---

## Answers to the 6 review questions

### 1. Is the JFR `<clinit>` fix CORRECT and COMPLETE? — Yes, with one P2 gap

Correct: each of the 4 event classes declares its own `public static
boolean enabled()` touching its subclass-static `PROTOTYPE`
(`GoroutineStartEvent.java:24,38` etc.). Invoking a static method
*declared by the class* triggers initialization (JLS 12.4.1) — the old
code's class literals only loaded. Probe (`/tmp/pinprobe/ClinitProbe`,
GraalVM 25): after load-only (`Class.forName(name, false, loader)` —
same semantics as a class literal) `FlightRecorder.getEventTypes()`
shows **0** graalgo types; after `JfrEvents.ensureRegistered()` it shows
**4**. PROBE_PASS. The `boolean[]` + `length != 4` guard makes a dropped
call a compile error. `SchedulerCore`'s constructor calls it before
`new Drainer(this)` / timer heap (`SchedulerCore.java:115`), and
`SchedMain` constructs the scheduler before the first spawn
(`SchedMain.java:137` → spawn at 143) — all on the platform main thread.
All `new SchedulerCore` sites (launcher + tests) run on platform
threads; none on a vthread (verified by grep).

Complete for today: exactly 4 `extends Event` classes exist in
phase1+phase2 `src/main`, all 4 listed. **Gap (P2, finding 2):** nothing
enumerates the package — a 5th event class added later without updating
`ensureRegistered()` silently reintroduces lazy-init pins on carriers.
The `length != 4` check guards only the array literal, not the
class-to-list correspondence.

### 2. Is PinAudit's pin DETECTION sound? — Yes, modulo one P1 fail-open

- **Positive control is genuine.** `pinproof.jfr` is a real FLR\0
  recording (magic verified). My probe parsed it with the repo's own
  `readPins`: exactly 1 event, thread `pin-jni-vt`, stack
  `VirtualThread.postPinnedEvent → … → doPark → parkWhileCritical` —
  a true park-while-in-JNI-critical pin. The tool sees pins when they
  exist (and the historical first full run found 13, then 0 after the
  fix — end-to-end evidence against systematic false negatives).
- **Event settings correct.** Inline `-XX:StartFlightRecording`
  settings (`jdk.VirtualThreadPinned#enabled=true`,
  `#stackTrace=true`) — the `.jfc` pitfall is documented and avoided.
  `System.exit()` in SchedMain still dumps the JVM-managed recording
  (proven by the 13-pin run).
- **JEP 491 premise verified empirically.** Synchronized+`parkNanos`
  on a vthread → 0 pin events on this GraalVM 25 (my `/tmp/redtest`
  probe) — the "only JNI-critical/park pins" collapse of the audit
  surface holds on the pinned toolchain.
- **P1 (finding 1): timeout/unreadable-JFR is fail-OPEN, not fail-closed.**
  `PinAudit.java:224` kills the child after 5 min → exit -1 → `skipped`;
  `readPins` throwing `IOException` → `skipped`. Either way the pins list
  stays empty, `unexpected == 0` (`PinAudit.java:183-184`), and the tool
  prints `AUDIT_RESULT=PASS` with exit 0. In `make check`'s smoke the
  `--tests` leg is a single child: if it times out, the gate passes
  *without the suite having run*. Measured: the smoke's child runs
  ~4m16s of the 5m budget on this VM — ~45s of margin; torture-test
  variance or a loaded machine flips it to vacuous-pass. (Corpus legs
  have the same shape per-program, but there a skip is at least
  per-program and reported; the single `--tests` leg is total bypass.)
- Timeouts are otherwise handled sanely (destroyForcibly, exit -1,
  skip entry). Missing JFR file → empty list (only reachable if the
  child died before JFR init, which also yields a non-zero exit →
  skipped). Non-`IOException` escapes from `RecordingFile` would
  propagate out of `main` → non-zero exit → fail-closed. Fine.

### 3. Is the CLASSIFICATION sound? — Yes; fail-closed is correct

Empty allowlist → `allowMatch` returns null → `expected=false` for
every pin → `unexpected > 0` → exit 1 (`PinAudit.java:183-184`). Any pin
is UNEXPECTED, including pins on non-goroutine vthreads — the safe
direction for a done-gate, and moot in practice: the only vthreads in
the system are `goroutine-<id>` carriers (`SchedulerCore.java:342`);
the drainer is a platform daemon thread (`Drainer.java:60`), which can
never emit the event. The comment at `readPins` ("anything else is
unexpected by default (fail closed)") matches the code. `make check`
propagates the exit code (`rc=$$?; …; exit $$rc`, `Makefile:133`).

### 4. Are the TESTS non-tautological? — Yes

- `PinAuditTest`: asserts exactly 1 pin from the real fixture, thread
  name, and stack frames (`doPark`, `parkWhileCritical`), plus
  allowlist-EXPECTED and allowlist-mismatch-UNEXPECTED. Cannot pass
  vacuously: missing/corrupt fixture → 0 pins → assertion failure;
  a broken event-name filter → 0 pins → failure. (Coupling note, not
  a finding: it pins the test to the JDK's stack shape —
  `parkWhileCritical` — so a future JDK rename turns it red; that's
  maintenance, not tautology.)
- `JfrRegistrationTest`: asserts all 4 named types registered after
  `ensureRegistered()`. Red-green is sound by construction + my probe:
  the old class-literal version leaves 0 types registered (probe:
  load-only → 0), so the assertion fails on old code, passes on new.
  Ordering dependency (must run before any scheduler activity) is
  satisfied — it is first in `SchedTestRunner.TESTS` — and documented
  in the class javadoc (P2, finding 5: the guarantee is by list-order
  convention only).
- `JfrEventsTest` timing skip: checks
  `FlightRecorder.getRecordings().isEmpty()` and skips only when a
  recording is active. In the smoke child the pin recording is active →
  skip; the preceding `assertTrue(!JfrEvents.anyEnabled())` still holds
  (graalgo events are `@Enabled(false)`, the pin recording doesn't
  enable them). The timing test still runs under plain `make test`.
  Documented, correct tradeoff.

### 5. Makefile wiring — works, with two nits

- `make check`'s pin smoke: builds first (`check: main
  $(OUT_TEST)/.built`), passes `-Dpinaudit.test.cp="$(TEST_CP)"`
  (required by `runChild`'s `__TESTS__` branch), captures the log,
  propagates the exit code. I ran the exact recipe: `AUDIT_RESULT=PASS
  unexpected=0`, `EXIT=0`, "Skipped: 0", in ~4m16s.
- `make pin-audit`: full corpus + tests + `--out` report; non-zero
  exit fails the make run (last recipe line). ✓
- **P2 (finding 3):** the pin-surface grep gate misses
  `public synchronized native void c();` — verified empirically
  against the exact pattern (`Makefile:120`). `synchronized` is not in
  the modifier alternation. A JNI method declared `synchronized`
  (legal Java) bypasses the "no native method declarations" gate.
- **P2 (finding 6):** `--out` report omits the verdict: the report
  string is written before the `AUDIT_RESULT=` println, so the
  archived `pinaudit-report.md` contains "Pin events total: 0" but no
  PASS/FAIL line.

### 6. Any other pinning surface missed? — No, with two doc corrections

- Park placement: `GoChan.send`/`recv` park after monitor exit
  (`GoChan.java:316,342`); `SelectOp`'s 3 park sites (245/265/273)
  are outside `synchronized (ch)`. ✓
- Blocking sites: `awaitQuiescence` callers are SchedMain's main
  thread + test threads ✓; `TimerHeap` wait is the timer thread ✓;
  `SchedTrace` sleep is a spawned platform thread ✓.
- **P2 (finding 4):** AUDIT.md §3 says "`GcInterop:247` sleep (called
  from `gc()`, platform)" — wrong: `GcCallNode.java:23` (phase1
  frontend) calls `GcInterop.gc()`, i.e. a Go `runtime.GC()` builtin
  executes the `Thread.sleep(50)` loop **on a goroutine carrier**.
  The conclusion (no pin) still holds — sleep parks without pinning
  on JDK 25 — but the "platform only" attribution is inaccurate.
- `synchronized` sites in phase1 runtime (`UnsafeHandles`,
  `Intrinsics.fastrand64`, probe classes): short, no park/sleep/
  wait/I-O inside — and post-JEP-491 they wouldn't pin anyway. ✓
- Grep gate covers `src/main` of both phases for `native`
  declarations and `java.lang.foreign` imports. The FFM-import check
  can't catch reflection-only FFM use, but that's not a realistic
  construction path; the `synchronized native` hole (finding 3) is
  the realistic one.

**Informational (P2, finding 7):** I could not reproduce a pin from
lazy `<clinit>` on a carrier in isolation — neither single-vthread
nor 16-way racing first-touch repros produced any pin event, while
the original run saw 13 pins across 186 fresh JVMs (~2% of
first-emissions). The pin stacks implicate `MetadataRepository.register`
(`synchronized`) during `<clinit>`, and `Continuation` still carries a
`PINNED_MONITOR` transient-failure mode, so a monitor/class-init-lock
race is plausible — but the precise park site is unidentified and the
report presents it as settled ("which parks while pinned"). This does
not weaken the fix: it removes *all* lazy `<clinit>` from carriers, so
whatever the racy site was, it can no longer trigger on a carrier
(probe-verified). It does mean the 0-pin re-audit is a single sample
of a racy phenomenon — the robustness argument rests on the mechanism,
not on the re-run count.

---

## Findings

### P1 (should fix)

**1. Pin-audit smoke can pass vacuously on child timeout / unreadable JFR (fail-open).**
`PinAudit.java:224` (`p.waitFor(5, TimeUnit.MINUTES)`) → kill → exit
-1 → `skipped`; `PinAudit.java:116-121,132-135` (`readPins`
`IOException`) → `skipped`. Both leave the pins list empty, so
`PinAudit.java:183-184` computes `unexpected == 0` → prints
`AUDIT_RESULT=PASS`, exit 0. In `make check` the `--tests` leg is one
child: a timeout means the gate goes green with the suite never
having run. Measured on this VM: the leg takes ~4m16s of the 5m
budget (~45s margin) — torture-test variance or a loaded machine can
flip it. Same shape per-program for the corpus, but there skips are
per-program; for `--tests` it is total gate bypass. This is the
"verdict parsers must require positive evidence" class: the smoke has
no positive evidence the suite executed. Fix options: (a) exit 1 when
`--tests` was requested but the test leg was skipped (non-zero exit
or unreadable JFR); (b) have the Makefile assert `Skipped …: 0` in
the smoke log; (c) raise the timeout well above the measured
~4m16s. (a) is the robust one — it also protects `make pin-audit`.

### P2 (should fix / consider)

**2. No guard against a 5th event class bypassing `ensureRegistered()`.**
`JfrEvents.java:84-99`: the `initialized.length != 4` check guards
only the array literal. A future `*Event extends Event` added to
`graalgo.jfr` without updating the list silently reintroduces
lazy-init pins on carriers (I verified exactly 4 exist today — the
gap is forward-looking). Recommend a test that reflectively lists all
`Event` subclasses in the package and asserts each is registered
after `ensureRegistered()`.

**3. Pin-surface grep gate misses `synchronized native`.**
`Makefile:120`: pattern
`^[[:space:]]*(public|private|protected|static|final|[[:space:]])*native[[:space:]]`
— empirically verified `public synchronized native void c();` is
MISSED (`synchronized`, `strictfp` absent from the alternation). A
`synchronized` JNI declaration bypasses the gate. Fix: extend the
alternation (or match `\bnative\b` on declaration lines).

**4. AUDIT.md §3 misattributes `GcInterop.gc()`'s thread.**
`AUDIT.md:49`: "`GcInterop:247` sleep (called from `gc()`,
platform)" — but `phases/phase1/src/main/java/graalgo/frontend/GcCallNode.java:23`
invokes `GcInterop.gc()` for Go `runtime.GC()` builtins, which execute
on the goroutine carrier. The no-pin conclusion still holds
(`Thread.sleep` parks unpinned on JDK 25), but the claim is wrong;
fix the attribution.

**5. `JfrRegistrationTest` non-vacuity rests on runner list order.**
`SchedTestRunner.java:21` places it first and the class javadoc
documents why — but it's convention, not enforcement. If the class
ever runs after scheduler activity in the same JVM, the assertion is
trivially true. Acceptable as-is; noted.

**6. Archived audit report omits the verdict.**
`PinAudit.java:171-176`: `--out` writes the report before the
`AUDIT_RESULT=` println (`PinAudit.java:180-181`), so
`hidden_files/pinning/pinaudit-report.md` records "Pin events total:
0" but no PASS/FAIL line. Append it to the report string.

**7. Mechanistic overclaim: the exact park site in lazy `<clinit>` is unidentified and racy.**
`JfrEvents.java:76` ("`FlightRecorder.register` (which parks while
pinned — P2-M7)") states as fact what my probes show is racy: two
synthetic repros (single-vthread and 16-way racing first-touch of
`<clinit>` on carriers, pin event armed) produced 0 pins, vs 13 pins
in the original 186-program run. The stacks implicate
`MetadataRepository.register` during `<clinit>`; the precise
park-while-pinned site is unknown (a `PINNED_MONITOR` transient
failure in `Continuation` is a plausible candidate). Soften the
javadoc/AUDIT.md wording to what is evidenced: the pin stacks show
`register` ← `<clinit>` on carriers, and forcing `<clinit>` on the
startup thread eliminates the class (13 → 0, mechanism
probe-verified).

**P2 nit (no finding number):** `PinAudit.java:97`
`Files.createTempDirectory("pinaudit")` is never deleted (only the
per-program `.jfr` files are) — litter in `out/test-tmp`.

---

## Empirical probes (all on `~/tools/graalvm-jdk-25`, repo untouched)

1. **`<clinit>` mechanism** (`/tmp/pinprobe/ClinitProbe.java`):
   load-only → 0 graalgo event types; `ensureRegistered()` → 4.
   PROBE_PASS — the fix does what the comment claims.
2. **Fixture genuineness** (`/tmp/pinprobe/FixtureProbe.java`):
   `pinproof.jfr` has FLR\0 magic; `readPins` → exactly 1 pin,
   thread `pin-jni-vt`, stack `postPinnedEvent → … → doPark →
   parkWhileCritical`. The positive control is real.
3. **JEP 491 premise** (`/tmp/redtest/SyncPin.java`):
   synchronized + `parkNanos` on a vthread → 0 pin events.
   The audit's collapsed surface holds on the pinned toolchain.
4. **`make check` smoke recipe**, run verbatim: `AUDIT_RESULT=PASS
   unexpected=0`, `EXIT=0`, "Skipped: 0", ~4m16s wall. (This runtime
   is the basis for finding 1's margin analysis.)

Reviewed commit: `9828c4a` (range `a1c2673..9828c4a` vs `e332dbf`).
