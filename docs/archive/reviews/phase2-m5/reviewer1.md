# P2-M5 (GODEBUG surface triage) — Reviewer 1 (correctness)

**Snapshot:** `c97b9330aed02c45b56d5d758fc96de3cc87bf6e` (read-only review; repo not modified)
**Reviewer:** 1 (correctness) · **Date:** 2026-10-05

## Verdict: NO-GO

The snapshot's own test tree does not compile (P0-1): `GoDebugTest.java:184`
calls `GcTrace.resetForTests()`, which does not exist in the committed
`GcTrace.java`. `make test` fails at `javac` with "cannot find symbol", so the
milestone's verification cannot run at this commit. (The working tree already
contains the missing method, uncommitted — committing it resolves the P0.)

Beyond that, the main code is in good shape: the parser matches gc semantics
plus the four documented divergences (each verified real against
`~/tools/go/src/runtime/runtime1.go`), the registry is complete and accurate
(79 knobs + 9 removed; all 34 runtime-knob defaults verified 34/34 against
gc; removed table names + old-values exact), and every deny path produces a
clear stderr diagnostic with exit 2. Four P1s and five P2s below, two of them
confirmed empirically against the real `gofrontend` binary.

## Method

- Diffed `Panic.java` overlay vs Phase-1 original: only the panicnil change +
  comments; flag-off paths are logically identical.
- Re-read gc's `parsegodebug` (`runtime1.go:492`), `dbgvars` table, and
  `internal/godebugs/table.go` (active + `Removed`) from `~/tools/go`
  (go1.27.1); verified each DIV-M5-1..4 claim is real and the code matches.
- Compiled the real `GoDebug.java` + `SchedTrace.java` against stubs in
  `/tmp` and ran an adversarial harness: **99/100 pass** (the one "fail" is a
  genuine parser discrepancy, P2-1). Cases covered: empty/null input,
  leading/trailing/doubled commas, duplicate keys (later wins), int32
  boundaries (±1), 1M-digit values, 200k keys, `+`/`-` signs, hex/octal/
  underscores/whitespace/scientific/NUL/superscript/Arabic-Indic digits,
  `=1` empty key, case-sensitivity, unicode keys, all 9 removed knobs with
  old and non-old values, unsupported non-default/default/negative values,
  daemon start/stop/idempotency and SCHED/SCHED-DETAIL line shapes, and 24
  malformed-JSON inputs against the hand-rolled parser.
- Verified the registry SHA-256 machine check (`testRegistryKnobHash`)
  reproduces `944ee8d6…b62e30` from the committed JSON.
- Ran the real gc-built `phases/phase1/frontend/gofrontend` under
  `GODEBUG=cgocheck=2` and `GODEBUG=schedtrace=50` to confirm env-inheritance
  effects (P1-3, P2-4).
- Confirmed the `String.format` locale effect with `Locale.ITALY` (P1-2).

---

## P0 (must fix)

### P0-1. Committed test tree does not compile — `GcTrace.resetForTests()` missing
- **Files:** `phases/phase2/src/test/java/graalgo/sched/launcher/GoDebugTest.java:184`
  vs `phases/phase2/src/main/java/graalgo/sched/launcher/GcTrace.java` (committed)
- `testApplySettingsInstalls` (line 184) calls `GcTrace.resetForTests()` in its
  `finally` block. The committed `GcTrace` has no such method (grep count 0;
  it exists only as an uncommitted working-tree modification). `$(OUT_TEST)/.built`
  therefore fails at `javac` with `cannot find symbol`, and `make test` cannot
  run at all at this commit. This is the only missing symbol (checked
  `SchedTrace.start/stop/isRunning`, `GcTrace.start/isStarted`,
  `Panic.setPanicNilOldBehavior` — all present).
- **Fix:** commit the existing working-tree addition of `resetForTests()` (or
  equivalent). Suggested guard for the future: the milestone's done-gate should
  include "test tree compiles at the tagged commit" — a green `make test` run
  on a dirty tree does not prove the commit builds.

---

## P1 (should fix)

### P1-1. Registry lazy-init publication race: `registryRemoved` can be observed null
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java:208-209`
- `loadRegistry()` publishes in order `registryVersion` (185) →
  `registryKnobs` (208) → `registryRemoved` (209), all volatile. A thread on
  the `ensureRegistry()` fast path (`registryKnobs != null`, no lock) can
  observe `registryKnobs != null` while the writer is preempted *between* the
  two stores; the subsequent `removedKnobs()` then returns null and
  `parse()`'s `for (RemovedKnob r : registryRemoved)` throws NPE — an
  uncaught crash (exit 1 + stack trace) instead of the clean exit-2 deny path.
  This is a real race, not just a JMM subtlety: the writer holds no lock
  visible to the fast-path reader during those two stores. (Single-threaded
  `SchedMain` startup makes production exposure unlikely; concurrent test use
  of `GoDebug.parse` is the realistic trigger.)
- **Fix:** publish `registryRemoved` *before* `registryKnobs` and document
  `registryKnobs` as the single publication guard:
  ```java
  registryRemoved = Collections.unmodifiableList(removed);
  registryKnobs = Collections.unmodifiableMap(knobs); // last: publication
  ```
  The volatile write→read happens-before chain then guarantees visibility
  (JLS 17.4.5 transitivity). Alternatively publish via a single immutable
  holder object.

### P1-2. `GcTrace` timestamp is locale-dependent
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GcTrace.java:64`
- `String.format("%.3f", secs)` uses the default locale. Verified: with
  `Locale.ITALY` it prints `gc 3 @1,234s ...` instead of the documented
  `gc 3 @1.234s ...`, breaking the GODEBUG.md line shape and any downstream
  log parsing. (Relevant: the user is in Italy.)
- **Fix:** `String.format(Locale.ROOT, "%.3f", secs)` (add
  `import java.util.Locale;`).

### P1-3. `GODEBUG=cgocheck=2` silently accepted — then kills the `gofrontend` child
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java`
  (`parse`, cgocheck is registry `noop`, default 1)
- gc fatals at startup when `cgocheck > 1` (`runtime1.go` `parsegodebug` tail:
  `throw("cgocheck > 1 mode is no longer supported at runtime...")`).
  graalgo accepts `cgocheck=2` as a silent noop — the one value in the whole
  triage that gc treats as fatal and graalgo swallows, contradicting the
  milestone's fail-loud philosophy (DIV-M5-1..3). Worse, it has a live
  consequence: `SchedMain.runGofrontend` inherits the environment, and the
  gc-built `gofrontend` child **fatals on startup** (verified empirically:
  `fatal error: cgocheck > 1 mode is no longer supported at runtime`,
  child exit 2), which `SchedMain` then reports as a *build failure* with a
  confusing message instead of running the program.
- **Fix:** mirror gc's post-parse check on the final value (so
  `cgocheck=2,cgocheck=1` stays legal, exactly like gc):
  ```java
  // gc fatals at startup if cgocheck > 1 (runtime1.go parsegodebug tail).
  Integer cgocheck = values.get("cgocheck");
  if (cgocheck != null && cgocheck > 1) {
      throw new GoDebugException("unsupported GODEBUG setting \"cgocheck="
          + cgocheck + "\": cgocheck > 1 mode is no longer supported at "
          + "runtime (matches gc's startup fatal)");
  }
  ```

### P1-4. `gofrontend` child inherits `GODEBUG` — trace lines pollute its stderr
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/SchedMain.java:225`
  (`runGofrontend`; `new ProcessBuilder(...)` inherits the full environment)
- Verified empirically: with `GODEBUG=schedtrace=50`, the gc-built frontend
  prints its own `SCHED 0ms: gomaxprocs=...` lines to stderr. On frontend
  success these are discarded, but on frontend *failure* they are captured as
  the "deterministic build log" the harness records — injecting
  timing-nondeterministic lines into recorded failure logs. Same for
  `gctrace=1` (per-cycle lines from the frontend's GC).
- **Fix (defense in depth, alongside P1-3):** run the frontend hermetic —
  `pb.environment().remove("GODEBUG")` in `runGofrontend`. The frontend is an
  internal service; no knob needs to reach it.

---

## P2 (nits / latent)

### P2-1. Parser accepts non-ASCII Unicode decimal digits; gc rejects them
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java`
  (`parse`, `Long.parseLong(value, 10)`)
- `Long.parseLong` accepts any Unicode decimal digit (`Character.digit`):
  `GODEBUG=schedtrace=١٢` (Arabic-Indic) parses as **12** in graalgo.
  Go's `strconv.ParseInt(s, 10, 32)` accepts ASCII `0-9` only — verified:
  `ParseInt("١٢")` → `invalid syntax`. DIV-M5-2 claims "not a base-10 int32"
  is a hard error; per gc's definition this input is not a valid base-10
  int32. (Found by the adversarial harness: 99/100.)
- **Fix:** pre-validate ASCII before parsing:
  ```java
  if (!value.matches("[+-]?[0-9]+")) throw new GoDebugException("malformed ...");
  ```

### P2-2. Hand-rolled JSON parser lacks `\b` and `\f` escapes
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java:454`
  (escape `switch` in `Json.string()`)
- Handles `" \\ / n t r u` but not `b` (backspace) and `f` (form feed), which
  are standard JSON escapes. The javadoc claims "strings with standard
  escapes". No current registry input triggers it (verified the real
  `registry.json` parses), but a future registry edit using `\b`/`\f` would
  fail with `bad escape` at startup.
- **Fix:** add `case 'b': b.append('\b'); break;` and
  `case 'f': b.append('\f'); break;`.

### P2-3. panicnil uncaught rendering: nil *interface value* prints `GoInterface{nil}`
- **Files:**
  `phases/phase2/src/main/java/graalgo/sched/SchedulerCore.java:361-369`
  (`recordPanic`);
  `phases/phase2/src/main/java/graalgo/runtime/Panic.java:74-83`
  (`throwPanic`)
- With `panicnil=1`, the `recordPanic` special case (`v == null` → print
  `panic: nil`) only fires for Java `null`. `var x any = nil; panic(x)` passes
  a nil `GoInterface`, which `throwPanic` deliberately leaves unwrapped when
  the flag is on — so the uncaught rendering is `panic: GoInterface{nil}`
  (`GoInterface.toString()`), not gc's pre-1.21 `panic: nil`. This contradicts
  GODEBUG.md's claim that unrecovered rendering has "(No divergence.)" — the
  claim holds for the `panic(nil)` literal path but not the nil-interface path.
- **Fix:** widen the special case in `recordPanic`:
  ```java
  Object v = pe.goValue();
  boolean isNil = v == null
      || (v instanceof graalgo.values.GoInterface gi && gi.isNil());
  if (isNil && graalgo.runtime.Panic.isPanicNilOldBehavior()) {
      System.err.println("panic: nil");
  } else {
      System.err.println("panic: " + v);
  }
  ```

### P2-4. `memprofilerate` rejects valid 64-bit values as "malformed"
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java`
  (`parse`, int32 range check)
- gc handles `memprofilerate` via `strconv.Atoi` (full `int`, 64-bit), so
  `GODEBUG=memprofilerate=1099511627776` is legal in gc but graalgo hard-errors
  with `malformed GODEBUG value ... is not a base-10 int32` — a misleading
  message for a well-formed value. Behaviorally moot (disposition `noop`),
  but the error mislabels the input.
- **Fix:** either widen the range check for `memprofilerate` to int64, or note
  the int32-only restriction in DIVERGENCES.md.

### P2-5. Missing-registry failure bypasses the clean deny path
- **File:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java`
  (`loadRegistry` → `IllegalStateException`); `SchedMain.java:48-53`
- If `registry.json` is absent from the classpath, `GoDebug.apply()` throws
  `IllegalStateException`, which `SchedMain.run` does not catch — uncaught
  stack trace, JVM exit 1, instead of the milestone's `graalgo-run: fatal
  error: ...` + exit 2 contract. Loud, but inconsistent.
- **Fix:** catch `RuntimeException` from `GoDebug.apply()` in `SchedMain.run`
  alongside `GoDebugException` (or wrap registry-load failures in
  `GoDebugException`).

---

## What checked out clean (no findings)

1. **DIV-M5-1..4 are real and the code matches the docs.** gc silently ignores
   unknown knobs, keeps the old value on unparsable values, and skips
   `=`-less fields (`runtime1.go:492-570`); graalgo hard-errors on all three
   exactly as documented; emulated trace formats are graalgo-shaped as
   documented.
2. **Parser edge semantics match gc** (adversarial harness, real code):
   empty segments skipped; left-to-right with later-wins duplicates;
   case-sensitive keys; `+`/`-` signs, leading zeros accepted; hex/underscores/
   whitespace/scientific rejected; int32 bounds exact; removed-knob old-value
   fatal only at startup with `Old` predicates matching gc's (`asynctimerchan`
   `1`/`2`, the six `1`s, the two `0`s); non-old removed values ignored.
3. **Registry completeness/accuracy:** 79 knobs = all 78 active gc knobs
   (`dbgvars` 33 + `internal/godebugs` active 59 − 6 overlap + `memprofilerate`)
   plus `multipartfiles` (correct: gc's live key is `multipartfiles`, the `#`
   only marks it undocumented). All 34 runtime-knob defaults verified against
   gc sources, 0 mismatches. Removed table: 9/9 names and old-values exact.
   Dispositions defensible (spot-checked 14 entries); the 4 EMULATED knobs are
   all actually implemented.
4. **Panic overlay fidelity:** with the flag off, `throwPanic` is logically
   identical to Phase-1 (only `!panicNilOldBehavior` guards added); `recover()`
   untouched. Flag is `volatile`, set once at startup before any goroutine
   spawns; `GoDebugTest` saves/restores it.
5. **Thread-safety (elsewhere):** `SchedTrace.start/stop` synchronized +
   CAS idempotent; daemon is a daemon thread, interrupts cleanly;
   `GcTrace.start` CAS-guarded (single listener set); `SchedulerCore.current()`
   throws `IllegalStateException` when uninstalled, which the daemon handles
   by skipping ticks — starting the daemon before scheduler install is safe.
6. **Error paths:** all six deny paths (unknown, no-`=`, malformed value,
   unsupported non-default, removed old value, negative schedtrace) throw
   `GoDebugException` → `SchedMain` prints `graalgo-run: fatal error: <msg>`
   and returns 2 (subprocess test asserts this). No silent-ignore path exists
   outside the documented NOOP/removed-non-old acceptances.
7. **Hook placement:** `GoDebug.apply()` is the first statement of
   `SchedMain.run`, before arg parsing, `gofrontend` launch, and scheduler
   install — no affected behavior can run first. Denials therefore also fire
   before the `gofrontend` child could observe a bad `GODEBUG`.
8. **Makefile:** `cp -r src/main/resources/. $(OUT_MAIN)/` places
   `registry.json` at the exact `ClassLoader.getResourceAsStream` path;
   debug-leftover gate exclusions for `SchedTrace`/`GcTrace` are justified in
   the comment (their stderr lines *are* the feature).
