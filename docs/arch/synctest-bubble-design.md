# synctest bubbles — design for a real implementation

Status: design only. Bubbles are NOT implemented (see
[limitations.md](limitations.md), "synctest bubbles"). This document
exists so the implementing lane does not have to re-derive the gc
contract, and so a shortcut implementation can be recognised as one:
any implementation that runs the bubble function without the
semantics below is a fake bubble and must not land. The current
fail-loud panics in
`../internal/backend/interop/src/main/java/graalgo/p3link/Synctest.java`
are strictly better than such a shortcut.

Reference implementation: gc 1.27.1, `src/runtime/synctest.go`,
`src/internal/synctest/synctest.go`, `src/testing/synctest/synctest.go`.
All gc behaviour quoted below was read from that tree, not from
memory of an older Go release.

## 1. The gc contract, in one page

A bubble is a set of goroutines plus its own fake clock.

- Creation: `internal/synctest.Run(f)` panics if the caller is
  already in a bubble ("synctest.Run called from within a synctest
  bubble"). Otherwise it creates a bubble whose clock starts at
  midnight UTC 2000-01-01 (946684800000000000 ns), starts `f` as
  the bubble's main goroutine, and parks the caller (the root) as
  the bubble driver until `f` returns or the bubble deadlocks.
- Membership: every goroutine started from inside the bubble joins
  it. `internal/synctest.inBubble(bubble, f)` installs an acquired
  bubble on the current goroutine, runs `f`, then uninstalls it; it
  panics if the goroutine is already bubbled ("goroutine is already
  bubbled"). `Bubble.Run` with a nil bubble just runs `f`
  unbubbled.
- Fake time: inside a bubble, `time` uses the bubble clock. Time
  advances only when every goroutine in the bubble is durably
  blocked (below). When that happens, either a pending
  `synctest.Wait()` returns, or the clock jumps to the next fake
  timer's fire time, whichever applies. Time stops advancing once
  the bubble's main goroutine has exited.
- Durably blocked (gc's `isIdleInSynctest` set, `src/runtime/runtime2.go`):
  blocking send/receive on a channel created in the bubble, a
  `select` whose every case is a bubble channel, `sync.Cond.Wait`,
  `sync.WaitGroup.Wait` when `Add` was called in the bubble,
  `time.Sleep`, the nil-channel and empty-select parks, and the
  synctest waits themselves. Everything else that can block is
  NOT durably blocking — notably mutex/RWMutex locks, network I/O
  and syscalls. A goroutine blocked on any of those keeps the
  bubble non-idle, so fake time does not advance for it. This is
  the property a naive implementation gets wrong, and the killer
  test for it is in §5.
- `synctest.Wait()`: panics outside a bubble ("goroutine is not in
  a bubble"); panics on a second concurrent `Wait` in the same
  bubble ("wait already in progress"); otherwise blocks until all
  other goroutines in the bubble are durably blocked, then returns.
- Deadlock: when all bubble goroutines are durably blocked, no
  `Wait` is pending, and no fake timer can advance time, `Run`
  panics. The two gc messages are "deadlock: all goroutines in
  bubble are blocked" (main still alive) and "deadlock: main bubble
  goroutine has exited but blocked goroutines remain" (main exited
  with stragglers). These are panics with a `synctestDeadlockError`
  value, not plain strings — the implementing lane must check what
  a guest `recover()` observes under gc before choosing the
  carrier.
- Association: a channel, timer, or ticker created in a bubble is
  associated with it; operating on it from outside the bubble
  panics. A `WaitGroup` becomes associated on its first `Add` or
  `Go` in a bubble; `Add`/`Go` from outside that bubble afterwards
  is a fatal error in gc (the implementing lane must verify the
  exact failure mode in `src/sync/waitgroup.go` — fatal vs panic —
  before coding it; do not guess). A `WaitGroup` held directly in
  a package variable cannot be associated in gc and its waits are
  not durably blocking; a `*WaitGroup` stored in a package
  variable can. `sync.Cond.Wait` is durably blocking, and waking
  a bubbled cond waiter from outside the bubble is a fatal error
  in gc. `acquire`/`release` maintain an active-count that keeps
  the bubble non-idle while held (used to propagate bubble
  membership through syscalls); `release` on nil type-asserts in
  gc and panics, which is why graalgo's current nil-tolerant
  no-op is documented as a deliberate divergence that only holds
  while `acquire` can never return a real bubble.
- Cleanup/finalizers (`runtime.AddCleanup`, `runtime.SetFinalizer`)
  run outside any bubble.

`testing/synctest.Test(t, f)` is a thin wrapper: it calls
`internal/synctest.Run` and fails the test outside the bubble if
the inner function reports failure. Implementing `internal/synctest.Run`
therefore unlocks `testing/synctest.Test` as far as the bubble
mechanics go; whether `testing` itself compiles and runs under
graalgo is a separate question for that lane to verify, not an
assumption of this design.

## 2. Why this is not a small fix

A bubble touches every place graalgo already has a separate,
real-time-based implementation, and the pieces only compose if
they share one notion of "idle":

| Piece | graalgo today (verified) | Bubble change required |
|---|---|---|
| Goroutine identity / spawn | `SchedulerCore.spawn` / `spawnGoFunc` start one virtual thread per goroutine; `GoroutineHandle` is the identity | Bubble membership must be inherited at spawn: a goroutine started from a bubbled goroutine joins the caller's bubble, and the bubble's goroutine count must be maintained on spawn and exit. Both spawn paths (scheduler and any Truffle `go` path) must inherit identically, or membership silently splits. |
| Fake clock | `time.Now` reads wall time (`TimeNowNode`, `GoTimerT` use `System.currentTimeMillis`); `GoTimer` deadlines are `System.nanoTime() + ns` in a per-scheduler `TimerHeap` | A per-bubble clock, starting at 946684800000000000 ns, must replace the wall-clock source for every time read and timer deadline created by a bubbled goroutine — `Now`, monotonic readings, `Sleep`, timers, tickers, and context deadlines whose timers are created in the bubble. Real-time code outside the bubble must be untouched. |
| Timer firing | `TimerHeap` fires on real time | Bubble timers need a fake-time heap (or fake-time entries in the existing heap keyed by bubble) whose minimum fire time is the bubble's `wakeTime`. Firing must be driven by the bubble driver (§3), never by a real-time thread, or fake time races wall time. |
| Park / block classification | `SchedulerCore.parkCurrent` parks generically; no per-park durable/non-durable classification exists | Every park site must report a durable or non-durable reason in gc's `isIdleInSynctest` sense: channel send/recv/select, cond wait, bubbled WaitGroup wait, sleep = durable; mutex, I/O, syscall, netpoller waits = non-durable. The bubble's running-count is derived from this classification, not from "parked". |
| Idle / quiescence | `SchedulerCore.awaitQuiescence` waits for live count zero; there is no deadlock detector (review finding R1-22) | The bubble driver needs its own idle predicate (all bubble goroutines durably blocked or exited, active-count zero), distinct from program quiescence. The general deadlock detector (R1-22) must additionally learn to exclude bubbled goroutines parked in fake time, or it will false-positive on every bubbled program. Sequence the two lanes together. |
| Association | `Synctest.associate`/`isAssociated`/`disassociate` take a raw address and always answer "unbubbled"; `GoSemaphore` ignores the synctest-durability flag | An identity-keyed association registry (bubble id per object) for channels, timers, tickers, WaitGroups, and conds, with cross-bubble operation panics/fatals per gc. Keying must be by object identity, not by the raw address alone: the implementing lane must validate the address-to-object mapping against the `UnsafeTable`/D5 address model before choosing the key, because an address is not a stable identity for every value kind. |
| `WaitGroup` durability | `sync.WaitGroup`'s bubble branches are dead code under the current no-bubble answers | Wire the durability flag through: `Add`/`Go` associate, `Wait` on a bubbled group is durably blocking, unassociated-group waits are not. |

No single one of these is the implementation. A lane that ships
only the fake clock, or only `Run` running `f` on a virtual
thread, has built the fake bubble this design forbids.

## 3. The bubble driver (port of gc's `synctestRun` loop)

State per bubble: unique id, fake `now`, root goroutine, main
goroutine, waiter, `done` (main exited), `total` (live bubble
goroutines), `running` (non-durably-blocked count), `active`
(acquire/release count). All mutations under one bubble lock;
the lock ordering against the scheduler, channel, timer-heap, and
WaitGroup locks must be fixed before coding — gc holds the bubble
lock while deciding idleness and never while running timers, and
graalgo must adopt an equivalent rule rather than discovering a
lock-order cycle in production.

Driver loop (root goroutine, after starting main):

1. Fire every fake timer whose deadline is at or before `now`.
2. Park the root until the bubble becomes idle (a bubble state
   transition wakes it).
3. If the next fake timer's fire time exists and main has not
   exited, set `now` to it and go to step 1. If main has exited,
   stop: time does not advance past main's exit.
4. If no fake timer can advance time, the bubble is deadlocked:
   panic with the gc message for the main-exited vs main-alive
   case (§1), after the bubble's goroutine set is final.
5. If a `Wait` caller is pending when the bubble becomes idle,
   wake it instead of advancing time; `Wait` returns once all
   other goroutines are durably blocked, without itself advancing
   the clock.

`Wait` itself: register as waiter (reject a second concurrent
`Wait`), park as a synctest wait (durable, so it does not itself
keep the bubble non-idle), and on wake clear the waiter and
return. The bubble's `running`/`active` accounting must follow
gc's `changegstatus`/`maybeWakeLocked` logic: a goroutine counts
as running while it is running, runnable, or non-durably blocked;
a transition to a durable park decrements it; `acquire`
increments `active`, `release` decrements it; the bubble is idle
exactly when `running == 0 && active == 0` (with the root's own
driver park handled as gc handles it, via the equivalent of
`synctestidle_c`, not by counting the root as a bubble worker).

## 4. Suggested landing sequence

Each phase must be independently correct and tested; no phase
runs bubble functions without bubble semantics.

1. Identity + association + failure modes. Bubble objects,
   spawn inheritance, `Run`-in-bubble / `inBubble`-already-bubbled /
   concurrent-`Wait` panics, association registry with
   cross-bubble panics. `Run` still panics (no driver yet) — the
   phase changes no observable behaviour except the new panics,
   which are gc-faithful.
2. Fake clock + fake timers. Per-bubble `now`, fake `time.Now`,
   fake `Sleep`/timer/ticker deadlines, `wakeTime`. No driver
   yet, so still no `Run` execution.
3. Durable classification + driver. Park-reason classification
   (§2), idle predicate, driver loop (§3), deadlock panics,
   `Wait` semantics. This is the phase where `Run` first executes
   `f` — and only because the driver is complete.
4. WaitGroup / Cond / channel association enforcement end to
   end, including the package-variable WaitGroup limitation and
   the fatal-vs-panic verification for cross-bubble WaitGroup use.
5. Differential corpus (§5) green against gc 1.27.1, then the
   panic-contract test in `SynctestContractTest` is replaced by
   bubble semantics tests and the limitations entry is removed
   in the same change.

## 5. Acceptance tests (differential vs gc 1.27.1)

Minimum corpus; every program runs under both gc and graalgo
with identical observable output (modulo only genuinely
nondeterministic values, which must be normalised by the harness,
never by masking a divergence):

1. The `testing/synctest` doc examples verbatim: fake-time
   sleep ordering (`TestTime`), `Wait` visibility (`TestWait`),
   `context.AfterFunc`, `context.WithTimeout` — including the
   initial clock value (midnight UTC 2000-01-01).
2. Nested bubbles: `Run` inside a bubble panics; `inBubble` on
   an already-bubbled goroutine panics; `inBubble` with an
   acquired bubble runs `f` inside that bubble (fake clock
   shared with the acquiring bubble).
3. Durable vs non-durable (the killer test): a bubbled goroutine
   blocked on a mutex held by an unbubbled goroutine must NOT let
   fake time advance and must NOT let `Wait` return; the same
   program with the goroutine blocked on a bubble channel or a
   `Sleep` must advance. A fake bubble fails the mutex case.
4. Deadlock: all-bubbled-goroutines-blocked with no timer panics
   with gc's message; main-exited-with-stragglers panics with the
   other message; a `Wait` pending at idle returns instead of
   deadlocking.
5. Association: channel/timer/ticker created in a bubble, used
   from outside, panics; WaitGroup associated in one bubble,
   used from another, fails exactly as gc fails it (per §1
   verification); a WaitGroup in a package variable behaves per
   gc's limitation.
6. Real-time isolation: wall-clock sleeps and timers outside the
   bubble are unaffected by fake-time advances inside it, and two
   concurrent bubbles have independent clocks.
7. Existing contract tests are replaced, not extended alongside:
   `SynctestContractTest.testRunPanics` and
   `testInBubblePanics` must fail on a real implementation by
   construction (see that class's tripwire comment). If they
   still pass after "bubbles land", bubbles did not land.

## 6. Open questions for the implementing lane (not guesses)

- Address-to-identity mapping for association (§2): validate
  against the D5 address model before choosing the registry key.
- Exact failure mode (fatal vs panic, and its carrier) for
  cross-bubble WaitGroup use and for waking a bubbled cond waiter
  from outside: read `src/sync/waitgroup.go` and the cond path in
  gc 1.27.1, then match it; the testing/synctest doc's "fatal
  error" wording is not precise enough to code from.
- What a guest `recover()` observes for the deadlock panic:
  `synctestDeadlockError` value vs plain string, and how it
  renders uncaught.
- Lock ordering between the bubble lock and the scheduler,
  channel, timer-heap, and WaitGroup locks, and how the driver
  park interacts with `SchedulerCore`'s drainer without
  double-counting the root as a live goroutine.
- Whether the Truffle `go`-statement path and the scheduler
  spawn path share one inheritance hook, or need two (with a
  test proving both inherit).
