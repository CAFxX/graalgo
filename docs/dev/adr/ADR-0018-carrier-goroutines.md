# ADR-0018 — Goroutines as continuations on polyglot carrier threads (carrier mode)

**Date:** 2026-11-20
**Status:** **accepted** 2026-11-23 as the goroutine model, by the
coordinator under design authority delegated by the owner:
`GG-0073` supersedes `GG-0017` ("Each
goroutine SHALL map 1:1 to a Java virtual thread") and makes carrier mode the default, one virtual
thread per goroutine the fallback where `Continuation` is unavailable.
Reissued 2026-11-25 as GG-0074 (*Reissue* below): thread mode is an
explicit opt-in only, carrier mode without `Continuation` fails loudly, a
pinned park occupies its carrier while a spare compensates, and goroutines
in native images are unsupported (planned). The
default flipped 2026-11-24 (lane carrier-default; item 7). (History: accepted as an
**opt-in mode** 2026-11-20, lane sched-spawn, which stopped at the
requirement conflict; see *Requirement* below.)
**Scope:** [SPEC.md](../../arch/SPEC.md) §3.5;
[truffle-design.md](../../arch/truffle-design.md) §2.5–§2.6 (T3);
[north-star.md](../../arch/north-star.md) §4.3 and §5.3;
requirements `GG-0073` (superseding `GG-0017`), GG-SCHED-0001–0003, GG-SCHED-0006, GG-WAKE-0001–0003
(re-verified in carrier mode, unchanged).

## Context

The north-star lane measured goroutine creation growing with the number of
goroutines alive: 10,000 spawns took 2.3 s (gc 10 ms), 100,000 took 162 s
(gc 0.16 s), 200,000 did not finish in 25 minutes. A server with 100,000
connections cannot start.

**Root cause** (Truffle 25.0.4 sources): every goroutine is a polyglot
thread (`Env.newTruffleThreadBuilder(body).virtual(true).build()`), and
`TruffleThreadBuilder.build()` calls
`PolyglotContextImpl.checkMultiThreadedAccess(newThread)`, which runs
`checkAllThreadAccesses`: for every initialized language, a loop over every
`PolyglotThreadInfo` of the context's `threads` map calling
`isThreadAccessAllowed`. The map holds every live goroutine thread, so each
spawn is O(live goroutines) and a program's spawns are O(N²) — the JFR
profile's 60 % in `checkAllThreadAccesses`. No option skips the loop (it is
unconditional; `isThreadAccessAllowed` returning `true` does not shorten it).

Options weighed:

- **(a) Plain virtual threads that enter the context lazily**
  (`TruffleContext.enter`). The first enter of a thread that is not a
  polyglot thread runs `PolyglotContextImpl.collectDeadThreads()` — another
  loop over every registered thread — and an entered thread stays in the
  `threads` map until it dies: still O(live) per goroutine. Guest code on a
  thread that never entered is not an option: `ContextReference` and
  `ContextThreadLocal` read the thread's polyglot thread-local array.
- **(c) Batching registration, or thread recycling** (truffle-design P-A):
  the polyglot API has no batch; recycling bounds registrations by the peak
  live count, so a spawn is still O(peak).
- **(d) Engine or context options**: none disables the access check.
- **(b) Goroutines that are not threads.** `jdk.internal.vm.Continuation` —
  the mechanism under every virtual thread — run on a small pool of polyglot
  threads entered once each. Truffle sees a handful of threads; a spawn
  allocates a handle and a continuation and nothing walks a registry.

## Decision

1. **Carrier mode.** A goroutine is a `Continuation` (scope
   `graalgo-goroutine`) plus its `GoroutineHandle`; the carriers
   (`graalgo.sched.Carriers`) are platform polyglot threads
   (`newTruffleThreadBuilder(loop).virtual(false)`; plain platform threads
   without a context), started on demand up to the scheduler's carrier
   parallelism. The drainer's admission protocol is unchanged: it still
   decides who runs and holds in-flight to the cap (GG-SCHED-0002/0003);
   carriers only execute admitted goroutines from one FIFO run queue.
2. **Park/unpark is `LockSupport`'s permit protocol per goroutine**: a park
   yields the continuation unless a permit is pending; an unpark grants the
   permit and queues the goroutine if it has yielded (states RUNNING →
   PARKING → PARKED → QUEUED, one CAS decides who queues it). Every park site
   already loops on its condition, so a spurious return is harmless and no
   wake is lost (GG-WAKE-0001; checked by the wake-race suites in carrier
   mode).
3. **Goroutine-local state moves with the goroutine.** The carrier swaps,
   on every mount and unmount, the values that were per thread: the
   language's `ContextThreadLocal` slot (`GoThreadSlot.state`, `.g`: the
   carrier is the polyglot thread, the slot is the `M` of truffle-design
   §2.5), the host `GoroutineState` holder, the scheduler's current-handle
   and own-scheduler thread-locals. The select result (`ChanOps`) moves into
   `GoroutineState`. Rule for runtime code: nothing per goroutine in a host
   `ThreadLocal`, and nothing thread-confined held across a park.
4. **Pinned parks and blocking calls keep the pool alive.** A park that
   cannot yield (a monitor or native frame pins the continuation) blocks its
   carrier until unparked; a goroutine in a blocking system call
   (`ioEnter`…`ioExit`, GG-SCHED-0006) blocks its carrier too, pinned on
   purpose (the syscall executor holds a confined `Arena` across the call).
   Both count as blocked, and the pool starts spares so that its target of
   unblocked carriers stays; idle spares retire after 10 s. This is gc's
   handoff of a P from an M blocked in a syscall.
5. **Direct handoff on the same carrier.** A goroutine handing its slot to
   the goroutine it woke (ADR-0016) puts it in its carrier's next slot; the
   carrier runs it as soon as the waker yields — gc's runnext on one P. The
   handoff spin of ADR-0016 is skipped (it would only delay that).
6. **Teardown** unparks every live goroutine (an intrusive list under the
   quiescence lock), which unwinds at its park as before, then stops and
   joins the carriers.
7. **Default (since 2026-11-24; opt-in until then).** Carrier mode is the
   default of schedulers and contexts; `-Dgraalgo.sched.goroutines=thread`
   (schedulers and the context default) or the context option
   `go.GoroutineThreads=thread` selects one virtual thread per goroutine, an
   explicit opt-in. Every launcher passes `--add-exports
   java.base/jdk.internal.vm=ALL-UNNAMED`; a JVM without it fails at
   scheduler creation with a clear error naming both remedies, rather than
   falling back to the quadratic mode unnoticed.

## Measurements

`spawn` microbenchmark (N goroutines spawned, each parks on a channel; idle
RSS after a GC; scratch program, the same shape as the realistic
`goroutines` benchmark), host shared and loaded (load 15–17 on 16 cores, the
JVM at `nice 19`, `-XX:ActiveProcessorCount=4`; gc `GOMAXPROCS=4`):

| N | gc spawn | thread mode spawn | carrier mode spawn | gc idle B/goroutine | carrier idle B/goroutine |
|---:|---:|---:|---:|---:|---:|
| 10,000 | 15 ms | 2.8–3.3 s | 0.21 s | 2,771 | 13,000–16,000 (heap-growth noise) |
| 100,000 | 0.18 s | 162 s (north-star) | 1.0–1.5 s | 2,750 | 3,800–4,700 |
| 1,000,000 | 1.9 s | infeasible (O(N²)) | 11.5 s (`-Xmx10g`, peak RSS 4.0 GB) | 2,757 | 2,289 |

Per-goroutine creation is flat (1,000 vs 19,000 alive: the `spawn_flat`
check) at ~10 µs on that host, 6–8× gc's 1.8 µs (the target was 5×). The
heap per parked goroutine at 100,000 is ~2.3 KB (class histogram: the
frozen stack chunk 1.26 KB, the handle with its atomics 0.27 KB, drainer
bookkeeping 0.15 KB, Go state 0.4 KB); RSS per goroutine above that is G1
heap growth. Per goroutine, the whole spawn path (handle, continuation,
INIT token, admission, first run to the park) is now CPU work the host
contention inflates; nothing in it depends on N.

**Finding — mass deoptimization on wake.** Waking 100,000 parked goroutines
costs ~40 µs each: one `transfer_to_interpreter` deoptimization per
goroutine (JFR: 99,288 for 100,000) at the return from the receive's
boundary call, in code compiled while every goroutine was parked inside it
(the code after the park had never run, so it was compiled as an uncommon
trap; each frozen activation of the invalidated code traps on its own). It
is independent of the thread model (virtual threads have the same frames)
and is a warm-up artifact of the first lifecycle; a steady-state server is
not exposed to it. Follow-up for the PE lane.

## Requirement

`GG-0017` (locked 2026-10-03) says each goroutine maps 1:1 to a Java virtual
thread. On Truffle 25 that mapping makes creation O(live goroutines) (the
two loops above), so the north-star goal (servers with huge goroutine
counts) and `GG-0017`'s text conflict. Carrier mode keeps everything else
`GG-0017` asks — `Safepoint.poll()` preemption with an indefinite park and a
resume token, the Loom pool treated as untrusted (carriers are ours now),
GOMAXPROCS as the in-flight cap — and replaces "a Java virtual thread" with
"a continuation scheduled on carrier threads". Flipping the default needs a
superseding requirement, which only the owner can lock. Proposed text:
"Each goroutine SHALL be a JDK continuation — a virtual thread, or a
`jdk.internal.vm.Continuation` mounted on a scheduler carrier thread — plus
a scheduler token; …" (the rest of `GG-0017` unchanged).

**Resolution (2026-11-23).** The owner delegated design authority to the
coordinator, who accepted this ADR and superseded `GG-0017` with
`GG-0073` (locked the same day). Its
text generalizes the proposal: a goroutine is a lightweight execution
context unmounted when parked — by default a `Continuation` run by
scheduler carriers, with spawn cost independent of the live count and
per-parked-goroutine memory ≤ ~2× gc's — and one virtual thread per
goroutine is only the documented fallback where `Continuation` is
unavailable (native images until P-F, truffle-design §7.4). Every other
`GG-0017` obligation carries over. Decision 7's "default stays one virtual
thread" holds only until the default-flip lane lands.

**Reissue (2026-11-25).** Global review round 4 found `GG-0073`'s text
ahead of, or behind, what was built and intended: a parked goroutine was
"unmounted from every OS thread", but a pinned park (a monitor or native
frame, or the `ioEnter`/`ioExit` bracket around thread-confined native
memory) blocks its carrier while a spare compensates; thread mode was "the
fallback where `Continuation` is unavailable", but carrier mode without
`Continuation` fails at scheduler creation (item 7) and thread mode is an
explicit opt-in; native images were said to use that fallback, but nothing
selects a mode for an image. The coordinator reissued it as
[GG-0074](../../../requirements/GG-0074.yaml) with those three statements
corrected (native-image goroutines: unsupported, planned under P-F) and
the spawn-scaling check moved to the benchmark tier (`make
bench-spawn-flat`, weekly); SPEC §3.5 no longer binds carrier mode to
Loom's default carrier scheduler.

## Consequences

- Spawning is independent of the live count and 10⁶ idle goroutines fit
  in ~4 GB RSS; the opt-in thread mode is unchanged.
- `jdk.internal.vm.Continuation` is JDK-internal: every launcher and test
  JVM gains the `--add-exports` flag, and a JDK upgrade must re-run the
  carrier gates (`CarriersTest`, `make bench-spawn-flat`, and every
  other gate, which run carrier mode). Native images select no goroutine
  mode: unsupported until P-F verifies `Continuation` there
  (truffle-design §7.4, amendment A1).
- `Thread.currentThread()` in runtime code is a carrier, shared by many
  goroutines: per-goroutine state goes through the swapped holders (item 3);
  a host monitor held across a park pins the goroutine (it then blocks a
  carrier, compensated, not deadlocked). JFR events keep the goroutine-N
  naming.
- Gates: `graalgo.sched.CarriersTest` (fast Java list: the structural
  no-thread-per-goroutine check, pinned park and syscall compensation,
  teardown) and the m1r program `spawn_parked.go` vs gc (`make test-m1r`:
  20,000 goroutines parked at once, functional output only); the
  spawn-scaling ratio is a wall-clock check, so it moved out of the
  per-push gate (2026-11-25) to the benchmark tier:
  `test/bench/scaling/spawn_flat.go` vs gc (`make bench-spawn-flat`,
  weekly in ci.yml's full-suite job); since the flip every gate runs carrier mode, and `make
  test-goroutine-modes` (~30 s, per push) reruns the scheduler suites and six
  goroutine programs in the fallback; it also runs
  `test/m1r/programs/carrier_migration.go` (parks inside reflect calls,
  timer callbacks, sort and `sync.Once`, panics crossing them) in carrier
  mode under `-ea`, so Truffle's per-thread assertions see goroutines
  migrate, and `test/m1r/cancel.sh` (`Context.close(true)` of a running
  program in both modes: bounded, cancelled, no thread left).
  (Until the flip, `CarriersTest` reran `LifecycleTest`/`WakeupRaceStressTest`
  in carrier mode and the removed `test/m1r/carrier_mode.sh` ran
  `spawn_flat.go`.)
  Benchmark: `make bench-goroutines` (not a gate).
