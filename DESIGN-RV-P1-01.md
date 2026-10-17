# RV-P1-01 — Design proposal: `testing/synctest` (`synctest.Run`/`inBubble` runtime panics)

- Lane: `fix-p1-synctest` (chat `graalgo-independent-codebase-review`)
- Worktree: `/home/hatch/workspace/worktrees/graalgo/fix-p1-synctest` (branch `wt/graalgo-independent-codebase-review/fix-p1-synctest`)
- Phase: **DESIGN ONLY** — no code written, no builds, no test runs.
- Finding: `internal/backend/interop/src/main/java/graalgo/p3link/Synctest.java:101,110`
  — any Go program using `testing/synctest` aborts at runtime with
  `Panic.throwPanic(GoString.fromUtf8("synctest.Run: not implemented by graalgo"))`.
- References: Go 1.27.1 sources at `~/tools/go/src` (`runtime/synctest.go`,
  `runtime/time.go`, `runtime/runtime2.go`, `runtime/proc.go`, `runtime/chan.go`,
  `internal/synctest/synctest.go`, `testing/synctest/synctest.go`).

---

## 1. What `testing/synctest` is

`testing/synctest` (Go 1.25+, graduated from `GOEXPERIMENT`) runs a test
function inside an isolated **bubble**: a set of goroutines with a **fake
clock** whose time advances only when every goroutine in the bubble is
**durably blocked**. Public API (`src/testing/synctest/synctest.go`):

- `Test(t, f)` — runs `f` in a new bubble via `internal/synctest.Run`;
  waits for all bubble goroutines to exit; **deadlock panics the test**.
  Must not be called from inside a bubble.
- `Wait()` — blocks until every *other* goroutine in the bubble is durably
  blocked; panics outside a bubble (this is already gc-faithful in
  `Synctest.waitForBubble`).
- `Sleep(d)` — `time.Sleep(d)` + `Wait()`.

Runtime mechanics in gc (`src/runtime/synctest.go`, 452 lines):

1. **Per-goroutine bubble pointer** (`g.bubble`), **inherited at spawn**
   (`proc.go:5401`: `newg.bubble = callergp.bubble`).
2. **`synctestBubble`**: `{mu, timers, id, now, root, waiter, main,
   waiting, done, total, running, active}`. `now` starts at
   `946684800000000000` ns = midnight UTC 2000-01-01.
3. **Quiescence accounting via the `changegstatus` hook** (called on *every*
   goroutine status transition): maintains `total` / `running`
   (non-durably-blocked) / `active` (miscellaneous activity pins).
   "Durably blocked" = `_Gwaiting` with a wait reason in the
   `isIdleInSynctest` set (`runtime2.go`): nil-chan send/recv, empty
   select, `Sleep`, `sync.Cond.Wait`, bubbled `WaitGroup.Wait`, bubbled
   channel ops, and the synctest-internal reasons. **Not** durable: mutex
   locks, blocking I/O, syscalls.
4. **`synctestRun(f)`**: installs the bubble on the caller (root), spawns
   `f` as `bubble.main`, then parks in a loop: when the bubble goes
   quiescent (`running==0 && active==0`, detected in `maybeWakeLocked`),
   either wakes the `Wait`er, or advances `now` to the next fake-timer
   deadline and fires timers (`bubble.timers.check`), or — no timers left —
   returns (if `total==1`) or **panics with deadlock**
   (`synctestDeadlockError`: "deadlock: all goroutines in bubble are
   blocked" / "...main bubble goroutine has exited but blocked goroutines
   remain").
5. **Fake time** (`runtime/time.go`): inside a bubble, `nanotime` returns
   `bubble.now` (and no monotonic reading); timers created in a bubble get
   `isFake=true` and are queued on the bubble's own timer heap; `Stop`/
   `Reset` of a fake timer from outside the bubble is fatal.
6. **Association** via heap specials (`specialBubble`): channels, timers,
   tickers created in a bubble are bound to it (`chan.go:117`);
   operating on a bubbled object from outside the bubble **panics**;
   package-level variables can never be associated (always unbubbled);
   `WaitGroup` associates on first `Add`/`Go` in a bubble; waking a
   bubble-blocked `Cond.Wait` from outside is fatal.
7. **Internal linkname surface** (11 stubs in `internal/synctest`):
   `Run`, `Wait`, `IsInBubble`, `associate`, `disassociate`,
   `isAssociated`, `acquire`, `release`, `inBubble` (+ the `Bubble` type
   used by `syscall/js`).

A correct implementation therefore requires from the graalgo scheduler:
a per-goroutine bubble pointer with spawn inheritance; quiescence
accounting driven by *every* park/wake/spawn/exit event, classified by
durability; a virtual clock consulted by all `time` wire nodes; a
fake-timer heap driven by the `Run` coordinator loop (not by wall
clock); per-object bubble association with cross-bubble panics; and
deadlock detection with gc's panic messages.

---

## 2. Option (a): implement the bubble runtime — design sketch

New `graalgo.sched.SynctestBubble` (or `graalgo.runtime`): id, fake `now`,
fake timer heap (priority queue — the wall-clock `TimerHeap` cannot be
reused; it is driven by a real timer thread), `total`/`running`/`active`,
`waiter`, `main`, `done`. New volatile `bubble` field on
`GoroutineHandle` (per-goroutine state, per the Truffle skill's lifecycle
guidance); `SchedulerCore.spawn` inherits the spawner's bubble
(gc `proc.go:5401` parity).

**Quiescence detection in the Drainer.** The drainer already sees every
scheduler event (`INIT`/`PARKED`/`UNPARK`/`EXIT`/`RESUME`/`IO_ENTER`/
`IO_EXIT`). It would maintain per-bubble counters, classifying each
`PARKED` by durability. `JfrEvents.notePark` already stashes
`handle.parkReason` unconditionally, so the drainer can read it off the
token's handle — but durability is not a `ParkReason` today:

| gc durable reason | graalgo site | work needed |
|---|---|---|
| bubbled chan send/recv | `GoChan` parks (`ParkReason.CHANNEL`, 2 sites) | durable only if the channel is associated with the *current* bubble → needs a durable bit alongside the reason, plus `GoChan.bubble` field set at `make(chan)` |
| empty select / bubbled select | `SelectOp` (`ParkReason.SELECT`) | same durable-bit treatment |
| Sleep | `Timers.sleep` (`ParkReason.TIMER`) | in-bubble sleep must register on the *fake* heap, not `sched.timerHeap()` |
| `sync.Cond.Wait` | cond wait path | audit required (which park reason it uses today) |
| bubbled `WaitGroup.Wait` | `GoSemaphore.acquireWaitGroup` (linkname `sync.runtime_SemacquireWaitGroup`, whose `synctestDurable` arg is currently **ignored**) | durable only if the WaitGroup is bubble-associated → association side-table keyed by identity/addr (real `sync` comes from disk; no fields can be added). Fidelity item: gc exempts package-level `WaitGroup` vars from association — the frontend would have to mark them |
| `SynctestRun`/`SynctestWait` | new `ParkReason.SYNCTEST` | root/waiter parks |
| mutex / I/O / netpoll / quantum | — | correctly non-durable already: `GoMutex` uses `synchronized` (never parks — naturally "still active", matching gc); `IO_ENTER`/`NETPOLL`/`RESUME` parks count as active |

When `running==0 && active==0`, the drainer unparks the bubble root (or
the `Wait`er) — the analog of gc's `changegstatus` → `maybeWakeLocked`.

**`Synctest.run` (replacing the panic):** fail if already in a bubble;
create the bubble; attach to the current handle; spawn `f` as main;
enter the coordinator loop (park self with `SYNCTEST` reason); on each
wake: waiter present → unpark waiter; fake timers pending → `now = next
deadline`, fire timers (unpark sleepers through the drainer);
`total==1` → detach, return; else panic with gc's deadlock messages.

**Fake clock:** the `timenow`/`timesleep`/`timenew`/`timeafter`/
`timetick`/`timeafterfunc` wire nodes (`synth_machinery.go`) check the
current handle's bubble: in-bubble → `timenow` returns `bubble.now`
with no monotonic component (audit graalgo's `Time` representation);
timer-creating nodes register on the bubble's fake heap and associate
(`GoTimer.bubble`); cross-bubble `Stop`/`Reset`/channel-op → gc-parity
panic/fatal.

**`Wait`/`inBubble`/`acquire`/`release`:** `Wait` registers as waiter and
parks (panics "wait already in progress" on concurrent `Wait`, per gc);
`inBubble` installs the acquired bubble on the current goroutine and runs
`f`; `acquire`/`release` do the `active` inc/dec (keeps `syscall/js`
parity).

**Scope verdict: multi-week project (roughly a P2-milestone-sized
effort), NOT a remediation-lane fix.** Cost drivers: (1) invasive,
perf-sensitive bookkeeping in the drainer's token loop (AGENTS.md §6);
(2) fake-clock plumbing through every `time` wire node plus a new
fake-timer heap and coordinator loop; (3) durability classification at
every park site; (4) association for channels/timers/tickers/WaitGroups
(side table + package-var fidelity item); (5) deadlock detection, which
shares machinery with the not-yet-designed RV-P1-19 lane (ordering
dependency — doing bubbles first duplicates that design); (6)
differential testing against gc's synctest corpus
(`testing/synctest` + `internal/synctest` tests, plus stdlib tests that
use synctest: `database/sql`, `encoding/json`, `net/http`, ...).
Bolting this on in a remediation lane risks landing a subtly unfaithful
bubble runtime — exactly the failure mode the review criticizes.

---

## 3. Option (b): compile-time diagnostic — exact design

**Hook (primary, production compile path):** `overlayImporter.Import`
in `internal/frontend/export.go` (~line 603) — the single choke point
through which every import in user code passes during type-checking.
The other `getDiskPackage` call sites (`export.go:4421`, `:11562`)
operate on packages already resolved through the importer, so they
cannot bypass it. Insert before the `getDiskPackage` fallback:

```go
// RV-P1-01: scope exclusion. testing/synctest's entire public API
// requires bubble semantics (fake-time scheduling with
// quiescence-gated clock advancement), which graalgo does not
// implement — any importing program would abort at runtime in
// synctest.Run. Reject at compile time with a diagnostic naming the
// missing feature instead of emitting a compilable program that
// panics. Tracked: <work-registry id> (re-test trigger: gc's
// synctest corpus green under graalgo).
if reason, ok := unsupportedImports[path]; ok {
    return nil, fmt.Errorf("gofrontend: import %q: not supported by graalgo: %s", path, reason)
}
```

with

```go
var unsupportedImports = map[string]string{
    "testing/synctest": "synctest bubbles (fake-time scheduling: the bubble clock advances " +
        "only when every goroutine in the bubble is durably blocked) are not implemented; " +
        "any synctest.Test would abort at runtime",
}
```

(A map, not a one-off `if`, so RV-P1-03's scope-exclusion tracking has
a single table to adopt.)

**Deliberately NOT rejected:** `internal/synctest`. Its 9 non-`Run`
intrinsics (`IsInBubble`→false, `associate`→`Unbubbled`,
`acquire`→nil, `Wait`→gc's unbubbled panic, …) are *exactly* gc's
behavior in a world where bubbles cannot exist — and the diagnostic
establishes exactly that world. `run`/`inBubble` keep their runtime
panics as **unreachable defense-in-depth** (reachable only via a direct
`internal/synctest` import, which user code cannot express through the
`go` command's internal-visibility rule).

**Mirror hook (secondary):** the same case in
`internal/frontend/p3frontend/importer.go`'s `Import` switch, for
consistency. p3frontend is not the production compile path (and its
suite is not in CI), but leaving the hole means a future p3frontend
user silently regresses to the runtime panic.

**`Synctest.java`:** behavior unchanged; javadoc updated to state the
compile-time guarantee — the "honest no-bubble answers" are now
*complete*, not aspirational.

**Tracking (per RV-P1-03's prescribed pattern):** new work-registry item
(e.g. `synctest-bubbles`) whose spec is §2 of this document; re-test
trigger: gc's `testing/synctest` + `internal/synctest` suites green
under graalgo; noted ordering dependency on RV-P1-19 (shared
drainer-based quiescence/deadlock machinery).

**Negative coverage** (RV-P1-03 pattern — each exclusion fires on
exactly its scope):
- `internal/frontend` test: exporting a file with
  `import "testing/synctest"` fails; the error names
  `testing/synctest` and the missing feature.
- Same test: a file importing only `sync` (which uses
  `internal/synctest` *linknames*, not imports) still compiles —
  the exclusion is exactly scoped and doesn't break `WaitGroup` users.
- Existing `sync_export_test.go` / linkname tests continue to pin the
  no-bubble intrinsic answers.

**Blast radius: zero.** Grep of the repo shows no test, corpus program,
or differential fixture imports `testing/synctest` (only vendored
`x/tools` sources mention it); `make snapshot-check` is unaffected.
The package is testing-only and experimental; no non-test stdlib
package imports it (`net/http/httptest` mentions it only in a doc
comment).

---

## 4. Recommendation: option (b)

1. **Honest scope.** (a) is 3–6 weeks of scheduler-integrated work with
   a differential-testing burden; (b) is a one-day, exactly-scoped
   change. Attempting (a) in a remediation lane is how you get a
   half-faithful bubble runtime.
2. **Makes the existing design coherent.** `Synctest.java`'s no-bubble
   answers are precisely correct iff bubbles can never exist; the
   diagnostic is what makes that true. Today the design is one import
   away from incoherence.
3. **Follows the review's own prescribed pattern.** RV-P1-03: declared
   scope limits are compile-time rejections tracked as work-registry
   items with re-test triggers and negative coverage — not runtime
   panics. RV-P1-17(a) likewise prefers failing at export.
4. **Strictly better UX for the only affected users** (test authors):
   a compile error naming the feature beats a runtime panic mid-suite.
5. **Keeps the door open without pretense:** §2 becomes the registry
   item's spec, so a future bubbles lane starts from a real design, not
   from "future work" in a javadoc.

---

## 5. No-bridge argument for (b)

The 2026-10-15 no-bridges order bans *workarounds that stand in for the
real implementation* — synthetic packages, import overlays, name-keyed
runtime dispatch, shims. The diagnostic is none of these:

- It **fakes no behavior and substitutes nothing**: it performs no
  dispatch on the package name at runtime, installs no alternative
  semantics, and changes no compilable program's meaning. It is a
  refusal, not a workaround.
- The single name-keyed element (the import-path check) **cannot
  silently alter semantics** — it is a pure rejection at the
  type-check boundary, in the same category as the existing WS G/H/D/F
  scope exclusions, not in the category of import overlays that
  substitute behavior.
- It is **reversible without residue**: when bubbles are implemented,
  the diagnostic and its tests are deleted and the registry item
  closed — no callers to migrate, no behavioral drift to unwind.
- The runtime panics it supersedes remain as unreachable backstops,
  documented as such — defense in depth, not a second implementation.

---

## 6. Implementation plan for (b) (on approval)

1. `internal/frontend/export.go`: add `unsupportedImports` table +
   check in `overlayImporter.Import` (before `getDiskPackage`).
2. `internal/frontend/p3frontend/importer.go`: mirror case.
3. `internal/backend/interop/.../p3link/Synctest.java`: javadoc update
   only (no behavior change).
4. `internal/frontend`: negative tests (rejects `testing/synctest`;
   still accepts `sync`-only programs; error text names the feature).
5. Work-registry item `synctest-bubbles` with §2 as spec, re-test
   trigger, RV-P1-19 ordering note.
6. Verify: affected `go test ./internal/frontend/` suites +
   `make check`; `make snapshot-check` expected unaffected (no corpus
   usage — verified by grep). Full `make all` before push per repo
   AGENTS.md.
7. Independent review (standing order), then `wt.py ready`.
   **This lane does NOT integrate.**
