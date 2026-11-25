# P2-M5 (GODEBUG surface triage) — Reviewer 2 (methodology, testing, documentation)

**Reviewer:** independent methodology reviewer (Reviewer 2)
**Scope:** fixed snapshot `c97b9330aed02c45b56d5d758fc96de3cc87bf6e`, read-only.
`GODEBUG.md`, `DIVERGENCES.md` (DIV-M5-1..4), `registry.json`, `GoDebugTest.java`
(22 tests), `PanicOverlayDriftTest.java`, `corpus/godebug/`, `tools/godebug-diff/`,
`CHECKPOINT.md` (P2-M5 entries). Supporting code read as evidence:
`GoDebug.java`, `SchedMain.java`, `SchedTrace.java`, `GcTrace.java`,
`SchedulerCore.java` (recordPanic), phase-2 `Panic.java` overlay.

**Cross-checked against primary source:** Go 1.27.1 sources at
`~/tools/go/src` (`runtime/runtime1.go` `parseRuntimeDebugVars`/`parsegodebug`/`dbgvars`,
`runtime/panic.go`, `internal/godebugs/table.go`) **and** empirically against the
`go1.27.1` binary (`GODEBUG=... go version` / `go run`).

## Verdict: NO-GO

One P0 blocks sign-off: the review snapshot does not compile its own test tree,
so the "146/146 green" evidence cited in CHECKPOINT.md cannot have been produced
from the snapshot as committed. The fix already exists uncommitted in the worktree
(see P0-1); remediation is small, but sign-off requires the fix committed and the
suite re-run green on the final tree. Beyond the P0, there are 4 P1 methodology
findings (machine-check blind spot, triage-framework gap, weak comparators,
untested uncaught-panicnil path) and 10 P2s (three undocumented gc divergences
found by differential source/behavior comparison, doc inaccuracies, coverage gaps).

Methodology note on how this review was done: every claim about gc behavior was
verified against the go1.27.1 source *and* the binary, not from memory. That
discipline paid off — two of my initial hypotheses were wrong
(`schedtrace=+100` is accepted by gc's `strconv.ParseInt`, matching graalgo;
`multipartfiles` is behaviorally equivalent despite the provenance wart), and
three real divergences I did *not* expect were found (`cgocheck=2` fatals in gc,
`memprofilerate` is 64-bit in gc, `panicnil` requires exactly `== 1` in gc).

---

## P0

### P0-1: The snapshot under review does not compile its own test sources

**Files:** `phases/phase2/src/test/java/graalgo/sched/launcher/GoDebugTest.java:184`,
`phases/phase2/src/main/java/graalgo/sched/launcher/GcTrace.java` (snapshot version)

`GoDebugTest.testApplySettingsInstalls` (line 184) calls `GcTrace.resetForTests()`,
but the snapshot's `GcTrace.java` defines no such method (only `start()` and
`isStarted()`). I extracted the pristine snapshot to `/tmp` via `git archive`
and ran the Makefile's exact test-compile step:

```
src/test/java/graalgo/sched/launcher/GoDebugTest.java:184: error: cannot find symbol
            GcTrace.resetForTests();
                   ^
  symbol:   method resetForTests()
  location: class GcTrace
1 error
```

(Main sources compile clean; this is the single error.) The working tree contains
an *uncommitted* fix adding `resetForTests()` + the `listener` field to
`GcTrace.java` (`git diff c97b933 -- .../GcTrace.java`). Consequence:
CHECKPOINT.md's "phase-2 suite 146/146 green" (line ~804) cannot describe the
snapshot — it describes the worktree. This is the same failure class as the M8
false-suite-claim incident: suite-green is a deliverable, and here the evidence
is unattributable to the reviewed commit.

**Fix:** commit the worktree `GcTrace.java` change (it is correct and minimal),
then re-run on the final tree: full phase-2 suite, phase-1 suite (208), `make check`.
Re-verify before sign-off. **Do not sign off c97b933.**

---

## P1

### P1-1: The version/hash machine checks do not cover retriage — a disposition flip is undetectable

**Files:** `phases/phase2/src/test/java/graalgo/sched/launcher/GoDebugTest.java:30-45`
(`testRegistryKnobHash`), `:48-51` (`testRegistryVersion`)

I reimplemented the hash independently (sorted knob names, `","`-joined, SHA-256):
it matches `EXPECTED_HASH` exactly, so the check *is* capable of failing — but
only on knob add/remove/rename. The hash input is names alone
(`String.join(",", GoDebug.knobNames())`). A **retriage** — e.g. flipping
`scavtrace` from `unsupported` to `noop`, changing a default, or editing a
justification — changes neither the name set nor the version constant, so both
machine checks pass silently. `GODEBUG.md`'s "Changing the registry" procedure
("Edit registry.json (add/remove/**retriage** a knob). Bump version.") relies on
an honor-system bump for exactly the change class the machines cannot see. The
test comment ("any knob add/remove/rename breaks the hash") is accurate but
undersells the gap: the most safety-critical change (weakening a denial) is the
one that slips through.

**Fix:** hash `name:disposition:default` tuples (and removed `name:oldValues`),
or hash the canonical JSON bytes. Update the test comment to state precisely what
is covered. This is cheap and closes the hole completely.

### P1-2: Triage-framework gap — no disposition for "effective behavior equals a non-default value"; `tracebacklabels` is triaged inconsistently

**Files:** `phases/phase2/src/main/resources/graalgo/sched/godebug/registry.json`
(`tracebacklabels`, `asyncpreemptoff`, `dontfreezetheworld`, `containermaxprocs`,
`updatemaxprocs` entries); `phases/phase2/DIVERGENCES.md` (DIV-M5-1..4)

Five NO-OP knobs carry justifications that admit graalgo's behavior equals a
**non-default** value:

| knob | gc default | graalgo ≡ | user-visible? |
|---|---|---|---|
| `tracebacklabels` | 1 | 0 | yes — goroutine trace headers |
| `asyncpreemptoff` | 0 | 1 | timing-only |
| `dontfreezetheworld` | 0 | 1 | crash-interleaving only |
| `containermaxprocs` | 1 | 0 | yes — GOMAXPROCS under cgroup quota |
| `updatemaxprocs` | 1 | 0 | yes — GOMAXPROCS under cgroup quota |

The triage's own principle — applied to `scavtrace`, `gcpacertrace`,
`gcstoptheworld`, `inittrace` — is that knobs whose purpose is to produce
diagnostic output must be UNSUPPORTED, because NO-OP would silently not deliver.
`tracebacklabels` is exactly that class (it controls goroutine-header output),
yet it is NO-OP because the framework's deny rule ("deny on non-default") cannot
deny a *default*. The silent difference is buried in a justification column;
DIVERGENCES.md has no entry for any of the five. `containermaxprocs`/`updatemaxprocs`
are the sharpest: in a container with CPU quota < host CPUs, gc derives
GOMAXPROCS from the quota while graalgo uses `--gmp`/host carriers — a real,
silent behavioral difference at default settings.

**Fix:** (1) add DIV-M5-5 (or per-knob entries) documenting the five
"effective ≡ non-default" knobs as known divergences; (2) consider a fourth
disposition (e.g. `DIVERGENT-DEFAULT`: accepted, documented, listed in
DIVERGENCES.md) so the framework can express what today is inexpressible.
At minimum, `tracebacklabels` must not sit as a plain NO-OP.

### P1-3: `godebug-diff` comparators are existence-only — they can pass while the emulation is broken

**File:** `tools/godebug-diff/godebug-diff.sh`

- **schedtrace** (lines ~57-72): requires ≥1 `^SCHED` line from gc and ≥1
  `^SCHED <n>ms: goroutines=` line from graalgo, exits 0, stdout identical.
  Passes if the daemon prints **one** line at startup and dies (the emulated
  observable is *periodic* output — never verified); passes if the interval
  ignores N (e.g. always 1s); passes if `goroutines=` is a constant 0
  (content never sanity-checked). The committed REPORT.md shows 131 graalgo
  lines, but the comparator only demands 1.
- **scheddetail** (lines ~74-84): runs graalgo only — no gc counterpart at all —
  and asserts ≥1 `^SCHED-DETAIL` line. This is a self-referential smoke check:
  it passes if the line prints when `scheddetail=0` (no negative control is
  run), passes with static garbage content, passes with zero `SCHED` lines.
  It is the closest thing to a tautological test in the milestone.
- **gctrace** (lines ~86-100): requires ≥1 `^gc <n> @` line per side. Passes if
  graalgo prints on a **timer** rather than per GC cycle (the event-driven
  property is never verified); the `heap <b>-><a>` numbers are never
  sanity-checked (inverted or constant values pass). (Fail-safe direction is
  right: zero lines → FAIL, not PASS.)
- **panicnil** (lines ~102-114): the one genuinely discriminating comparator —
  I verified the logic: graalgo's default wraps nil in `*PanicNilError`
  (`Panic.java:80-89`), so a stuck-off flag would print
  `recovered: *PanicNilError` ≠ gc's `recovered: <nil> (<nil>)` → FAIL. Good.
  But see P1-4 for what's missing.

**Fix:** for the two trace knobs, require ≥2 lines with strictly increasing
timestamps, assert `goroutines≥1` while the probe runs, and assert the
`schedtrace=N` interval is honored within tolerance (compare line count against
wall time). For `scheddetail`, add the negative control (`scheddetail=0` →
zero `SCHED-DETAIL` lines) and ideally diff gc's detail output. For `gctrace`,
require ≥2 lines with increasing `@` timestamps and sane heap numbers
(`before ≥ 0`, values plausible). Name the comparators' strength honestly in
the script header ("existence + stdout-preservation smoke checks" vs what they
don't verify).

### P1-4: `panicnil` uncaught path is claimed but entirely untested; no negative control

**Files:** `phases/phase2/GODEBUG.md` ("an uncaught `panic(nil)` prints `panic: nil`"),
`phases/phase2/src/main/java/graalgo/sched/SchedulerCore.java:361-368`
(`recordPanic`), `phases/phase2/corpus/godebug/db02_panicnil.go`,
`tools/godebug-diff/godebug-diff.sh:102-114`

The emulation has two paths: `recover()` (covered by `db02_panicnil.go` + the
diff, byte-identical) and **uncaught** `panic(nil)` → `recordPanic` prints
`panic: nil` when the flag is on. The second path has zero coverage: no corpus
probe (db02 always recovers), no diff entry, no unit test. A regression that
deleted the `isPanicNilOldBehavior()` branch in `recordPanic` would print
`panic: null` and nothing in the milestone would notice. Related: there is no
negative control proving the *knob* is what changes behavior in the stuck-on
direction — nothing runs graalgo with `panicnil=0`/unset against gc's default
to show `*PanicNilError` rendering (the diff only exercises `panicnil=1`).

**Fix:** add a `db05_panicnil_uncaught.go` probe (no recover; expect `panic: nil`,
exit 2) and extend the diff's panicnil section with the uncaught comparison vs
gc, plus a negative control (`panicnil=0` on graalgo vs default gc →
`*PanicNilError`-shaped output, stdout differing *as expected*).

---

## P2

### P2-1: `cgocheck=2` — gc fatals, graalgo silently accepts (undocumented divergence)

**Files:** `phases/phase2/src/main/resources/graalgo/sched/godebug/registry.json`
(`cgocheck` noop entry), `phases/phase2/src/test/java/graalgo/sched/launcher/GoDebugTest.java:114`
(`testNoopAccepted` asserts `cgocheck=2` parses)

Verified empirically against go1.27.1: `GODEBUG=cgocheck=2 go version` →
`fatal error: cgocheck > 1 mode is no longer supported at runtime` (exit 2;
`runtime1.go:567` `throw`). graalgo accepts any value as NO-OP ("No cgo in
graalgo") and the test *pins* acceptance of `2`. So a GODEBUG string that
crashes under gc is silently accepted under graalgo. The justification addresses
only the cgo-absence, missing that gc treats `>1` as a startup fatal. This runs
counter to the milestone's fail-loud philosophy.

**Fix:** deny `cgocheck > 1` like gc (parse-time: `if key.equals("cgocheck") && n > 1`
→ deny), accept 0/1 as noop; or record a DIV entry and change the test. Denying
is strictly more gc-faithful.

### P2-2: `memprofilerate` parsed as int32; gc accepts full 64-bit (undocumented divergence)

**Files:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java:272`
(int32 range check), `phases/phase2/src/main/resources/graalgo/sched/godebug/registry.json`
(`memprofilerate` noop entry)

gc parses `memprofilerate` with `strconv.Atoi` (`runtime1.go:548`) — full
`int` (64-bit) range — while every other knob uses `ParseInt(_, 10, 32)`.
graalgo applies a uniform int32 check: `GODEBUG=memprofilerate=5000000000`
is valid under gc and a hard error under graalgo. DIV-M5-2's criterion
("not a base-10 int32") is therefore inaccurate for this one knob.

**Fix:** parse `memprofilerate` with 64-bit range (or document the exception in
DIV-M5-2 / the knob justification). Add a test pinning the boundary.

### P2-3: `panicnil=2` — gc requires exactly `== 1`; graalgo enables on any nonzero

**Files:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java:318`
(`s.get("panicnil") != 0`), `~/tools/go/src/runtime/panic.go:811`
(`if debug.panicnil.Load() != 1 { e = new(PanicNilError) }`)

Under gc, `GODEBUG=panicnil=2` keeps the *new* (wrapping) behavior; under
graalgo it enables old behavior. Narrow edge (nobody sets `panicnil=2`
deliberately), but it is a semantic divergence in an EMULATED knob.

**Fix:** `if (s.get("panicnil") == 1)` in `applySettings`; add a unit test
(`panicnil=2` → flag off).

### P2-4: Negative `schedtrace` denied by graalgo, silently treated as off by gc (undocumented)

**Files:** `phases/phase2/src/main/java/graalgo/sched/launcher/GoDebug.java:308-311`,
`phases/phase2/src/test/java/graalgo/sched/launcher/GoDebugTest.java`
(`testNegativeSchedtraceDenied`)

gc's `ParseInt("-1", 10, 32)` succeeds and the scheduler loop gates on
`schedtrace > 0`, so negative values are silently off. graalgo exits 2.
Denying is defensible under the fail-loud philosophy, but it is a divergence
from gc that belongs in DIVERGENCES.md (it is not "malformed" per DIV-M5-2).

**Fix:** add a DIV entry (or match gc: treat negative as off — but do not do
this silently; either is fine if documented).

### P2-5: CHECKPOINT.md says "10 removed"; there are 9 (registry and go1.27.1 agree)

**Files:** `phases/phase2/CHECKPOINT.md:761` ("+ 10 removed"), `:793`-area triage plan

`internal/godebugs/table.go` `Removed` has exactly 9 entries
(x509sha1, tlskyber, gotypesalias, tlsunsafeekm, tlsrsakex, tls3des,
tls10server, x509keypairleaf, asynctimerchan); `registry.json` has 9.
Related: `GoDebugTest.java:61` comments "33 dbgvars + memprofilerate +
45 non-runtime godebugs = 79" — the true decomposition is 33 + 1 + **44**
(`dataindependenttiming` is in both `dbgvars` and the table) + `multipartfiles`,
which go1.27.1 has **commented out** of its table.

**Fix:** correct to 9; fix the decomposition comment.

### P2-6: `multipartfiles` is in the registry but not recognized by go1.27.1

**File:** `phases/phase2/src/main/resources/graalgo/sched/godebug/registry.json:294`

`registry.json` claims `"generated_from": "go1.27.1: .../table.go"`, but
go1.27.1's table has `//{Name: "multipartfiles", ...}` commented out, and
empirically `GODEBUG=multipartfiles=1 go version` is silently ignored as
unknown. Keeping it as NO-OP is behaviorally equivalent to gc (both ignore
it) and arguably good for compat, but the provenance claim is inaccurate and
the justification ("Subsystem not present…") doesn't say what's special.

**Fix:** either drop it (exact gc known-set match) or keep it with a
justification that says why (e.g. "not recognized by go1.27.1 — commented out
upstream; accepted for forward/backward compat, behaviorally identical to gc's
silent ignore").

### P2-7: Phase-1 launcher never applies GODEBUG — silent ignore outside the milestone's binary

**Files:** `phases/phase2/src/main/java/graalgo/sched/launcher/SchedMain.java:44-52`
(only `GoDebug.apply()` call site in non-test code)

`GoDebug.apply()` is called only by `SchedMain` (`graalgo-run`). The Phase-1
launcher (`graalgo.launcher.Main`) never touches `GoDebug`: under it, *all*
GODEBUG — including typos — is silently ignored. If the Phase-1 launcher
remains user-facing, the milestone's "never silently ignore" holds only for
`graalgo-run`.

**Fix:** scope note in `GODEBUG.md` (or wire the same startup hook into the
Phase-1 launcher if it is still shipped).

### P2-8: Test coverage gaps (edge cases)

All in `GoDebugTest.java` / subprocess tests; none threaten the core claim, but
the milestone's falsifiable core deserves per-deny-class end-to-end coverage:

1. Removed-knob old value denied **at startup via subprocess** — only unit-tested
   (`testRemovedOldValueDenied`); the subprocess suite covers unknown-knob and
   unsupported-knob only.
2. `GODEBUG` with spaces: `"schedtrace=100 "` (trailing → malformed-deny per
   DIV-M5-2) and `" schedtrace=100"` (leading → unknown-deny per DIV-M5-1) —
   both untested; the leading-space case is the most likely real-world typo.
3. `schedtrace=0` explicitly → daemon never starts, zero SCHED lines —
   untested (only via the override test, which asserts the value, not the
   absence of output).
4. `scheddetail=1` **without** `schedtrace` → no output (gc parity) — untested.
5. `schedtrace=-1` at startup via subprocess — unit-tested only.
6. Atomicity: `bogus=1,schedtrace=100` → denial leaves nothing installed
   (holds by construction — `applySettings` runs after full parse — but unpinned
   by any test).
7. `memprofilerate` 64-bit boundary (see P2-2); `panicnil=2` (see P2-3).

### P2-9: `inittrace` justification is imprecise

**File:** `phases/phase2/src/main/resources/graalgo/sched/godebug/registry.json`
(`inittrace` entry): "Package initialization lives in the Phase-1 frontend
(frozen); init timing cannot be traced from Phase 2."

Init functions *execute* in Phase 2 (on the scheduler); what is frozen is the
frontend that *builds* them, which cannot be changed to add timing
instrumentation. The current wording suggests init lives entirely in Phase 1,
which is wrong and will confuse the next reader.

**Fix:** "Init functions execute on the Phase-2 scheduler but are built by the
frozen Phase-1 frontend, which cannot be instrumented for timing; …"

### P2-10: No re-triage trigger if `pprof` ever lands (`memprofilerate`, `profstackdepth`)

**Files:** `registry.json` (`memprofilerate`, `profstackdepth` noop entries)

Both justifications rest on "no profiler is produced". If a future milestone
adds CPU/heap profiling, both knobs silently become load-bearing while staying
NO-OP. The registry has no re-test-trigger field.

**Fix:** add a `revisit_if` / trigger note to these entries (e.g.
"re-triage if runtime/pprof or heap profiling lands"), or track it in PLAN.md.

---

## P3 (minor)

- **P3-1:** Registry resource missing → `IllegalStateException` (uncaught) →
  exit 1 + stack trace, not the documented exit-2 contract
  (`GoDebug.java:150-158`). Wrap as `GoDebugException` or document.
- **P3-2:** CHECKPOINT.md's "`asynctimerchan=1` denied (our timers are async)"
  is a confused rationale — the denial is gc parity (old-value fatal), not
  timer-related.
- **P3-3:** `godebug-diff.sh` `check()` keeps the *last* failure's detail
  string when several checks fail (each `|| { …; detail=…; }` overwrites).
  Cosmetic.
- **P3-4:** A non-`GoDebugException` from `applySettings` (e.g. an unexpected
  runtime throw from `SchedTrace.start`) propagates uncaught → exit 1 +
  stack trace, off the exit-2 contract. Defensive catch-and-wrap would close it.

---

## What was verified and holds

For the record — the core of the milestone is sound, which is why the verdict
is NO-GO rather than something worse:

1. **Falsifiable claim "unknown knobs deny-with-error, never silently ignore"
   holds on every path checked.** `SchedMain.run` calls `GoDebug.apply()` before
   anything else; every deny class (unknown, malformed, bare field, unsupported
   non-default, removed old value) throws `GoDebugException` → stderr +
   exit 2. Parsing is atomic (left-to-right, later-overrides-earlier, no partial
   application — `applySettings` runs only after the full string parses).
   The tool surfaces denials as FAIL, never silent. Empty fields skipped like
   gc; `+100` accepted like gc's `strconv.ParseInt` (verified, not assumed).
2. **Triage of the 9 UNSUPPORTED is defensible in every case** (checked each
   against what the knob would need): output/diagnostic knobs that cannot be
   honored must deny rather than silently not deliver. The NO-OP justifications
   for `invalidptr`, `madvdontneed`, `cgocheck` (modulo P2-1), `dataindependenttiming`,
   and the subsystem-absent family are sound.
3. **`testRegistryKnobHash` is genuinely capable of failing** — independently
   recomputed SHA-256 over the sorted name set matches `EXPECTED_HASH` exactly
   (modulo P1-1's coverage blind spot).
4. **Docs consistency:** `GODEBUG.md` matches `registry.json` on all 10
   spot-checked knob entries (disposition, default, justification) and all 9
   removed entries; DIV-M5-1..4 match the code (messages, exit codes, formats);
   the 22-test count (21 + 1 drift) and 4/9/66 disposition split check out.
5. **Knob universe is complete against go1.27.1** (modulo P2-6): 33 dbgvars +
   memprofilerate + 44 non-runtime + multipartfiles = 79; removed list matches
   upstream exactly (9/9, names and old values).

## Suggested remediation order

1. Commit the worktree `GcTrace.java` fix (P0-1) → re-run phase-2 suite,
   phase-1 suite, `make check` on the final tree → re-verify.
2. Strengthen the hash to cover disposition/default/removed (P1-1) — 15 minutes,
   permanent.
3. Document the five effective≡non-default knobs as divergences; decide the
   `tracebacklabels` disposition (P1-2).
4. Strengthen the four comparators + add uncaught-panicnil probe and negative
   controls (P1-3, P1-4); re-run `godebug-diff`.
5. Remediate P2-1..P2-4 (gc-parity divergences: deny `cgocheck>1`, 64-bit
   `memprofilerate`, `panicnil==1`, DIV entry for negative `schedtrace`).
6. Doc fixes: "10 removed" → 9 (P2-5), `multipartfiles` provenance (P2-6),
   `inittrace` wording (P2-9), coverage gaps (P2-8), P3s as time permits.

*Reviewed read-only; no repo files were modified. Snapshot drift note: HEAD moved
from `c97b933` to `c636e25` (CHECKPOINT-only change: "reviews dispatched") during
the review; all findings above are against `c97b933` and unaffected by that delta.
Pristine-snapshot compile check was performed on a `git archive` extraction to
`/tmp` (repo untouched).*
