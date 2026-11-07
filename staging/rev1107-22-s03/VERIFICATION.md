# S-03 — mechanism re-verification (disk truth, HEAD 59f0a247)

Checked 2026-11-07 ~22:13 CET against the main repo at
`~/workspace/graalgo`, read-only. HEAD verified:
`59f0a247088f0e0b6db7e0623076866727dd873c` ("B-02: plain launcher
initializes the Go os surface").

## The leak, from the code

`internal/backend/runtime/src/main/java/graalgo/runtime/sync/GoPool.java`
(at HEAD):

- `:101` — `allLocals` is `Collections.newSetFromMap(new IdentityHashMap<>())`,
  a strong set, pool-lock-guarded.
- `:120-128` — `local()`:
  ```java
  Local l = locals.get();
  if (!l.registered) {
      synchronized (lock) {
          allLocals.add(l);
          l.registered = true;
      }
  }
  ```
  Every calling thread registers once; no removal exists anywhere in
  the file (grep for `remove`/`clear` on `allLocals`: zero hits).
- `:180-197` — `get()` slow path iterates `allLocals` twice under
  `lock` (shared queues, then private slots).
- Class doc `:33-37` (at HEAD): "Locals of dead threads stay
  registered — their items remain stealable. The registry is bounded
  in practice by the scheduler's carrier pool plus the threads that
  touch the pool." The second sentence is wrong.

`internal/backend/runtime/src/main/java/graalgo/sched/SchedulerCore.java:367`:

```java
Thread vthread = Thread.ofVirtual().name("goroutine-" + h.id).unstarted(() -> {
```

One virtual thread per goroutine. `ThreadLocal<Local>` is therefore
per-goroutine, not per-carrier. A program spawning 1M goroutines
that touch a shared `sync.Pool` retains 1M `Local`s (each an
`AtomicReference` + `ConcurrentLinkedDeque` + `ThreadLocal` entry),
and every slow-path `get` scans them twice.

## Why the obvious weak-ref fix alone is wrong

Holding `allLocals` as `WeakReference<Local>` would let dead
threads' Locals (and their items) be GC'd unobserved. That breaks
the existing, pinned behavior `GoPoolTest.testCrossThreadSteal`
asserts: items `Put` by an already-dead thread must still be
`Get`-able. The fix therefore keeps strong refs but reaps dead
owners' Locals under the pool lock, draining their items to the
global queue first. Keying by carrier thread instead of vthread was
also rejected: vthreads migrate across carriers, so a carrier-keyed
`ThreadLocal` would change a goroutine's Local mid-flight.

## Original finding (REVIEW-2026-11-07-07, §S-03)

Confirmed verbatim present at HEAD `59f0a247`: same files/lines,
same mechanism. The older registry entry `graalgo-fix-sync-pool.json`
(DONE) refers to a different Pool issue, not S-03.

## Review-22's lane-state claim

REVIEW-2026-11-07-22 says S-03 had no lane; verified by this run's
brief — this is the first lane for S-03.
