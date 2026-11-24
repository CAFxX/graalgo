# ADR-0019 — Cancellation, blocking and exit as a Truffle citizen

**Date:** 2026-11-24
**Status:** accepted (lane tr-safepoint, WP-TR-AUDIT-3 of
[truffle-audit.md](../../arch/truffle-audit.md); findings F-10.1, F-10.3)
**Scope:** `graalgo.sched` (Carriers, SchedulerCore, Drainer),
`graalgo.syscall` (Executor, BlockingCall), `graalgo.language` (GoContext,
GoLanguage), `graalgo.runtime.sync.PinLock`,
`graalgo.launcher` (BundleMain, GoPolyglot). Playbook §10, §13.

## Context

The audit measured that no blocking wait of the runtime went through
`TruffleSafepoint` (`--cpusampler`: 0 samples, every sample missed), so
`Context.close(true)`, hard exit, samplers and the debugger could not reach a
thread that waited. The carrier-default lane fixed the eval thread's exit
wait; still open were idle and blocked carriers, parked goroutine threads
(thread mode), and a goroutine blocked in a system call, which made
`close(true)` wait forever: its carrier waited for the off-pool system-call
thread outside the safepoint protocol, and the native buffers lived in an
arena owned by the waiting thread, so the wait could not simply be abandoned.
`os.Exit` ended the eval with a normal `int` result: an embedder could not
tell an exit from a return, and `TruffleLanguage.exitContext` was not
implemented.

## Decision

1. **Every blocking wait of a polyglot thread is Truffle-blocked.** Idle
   carriers (`setBlockedThreadInterruptibleFunction` around the run-queue
   poll), pinned parks and thread-mode parks (`LockSupport.park` as an
   `Interruptible` that turns the interrupt into `InterruptedException`), the
   system-call wait, and contended pin-lock acquisition
   (`ReentrantLock::lockInterruptibly`). Parked goroutines in carrier mode are
   unmounted continuations, not threads: thread-local actions target threads,
   so they reach every carrier (idle, running, blocked) and the eval thread;
   a parked goroutine is reached when it next runs, or unwound by the
   scheduler teardown. A cancellation or hard exit that reaches a carrier
   outside a goroutine ends the carrier and stops the pool.
2. **Cancellable system calls.** A may-block call owns its native buffers:
   one `malloc` block freed by whichever side finishes last (the goroutine
   after copying results out, or the off-pool thread after the kernel
   returned when the goroutine left first). The goroutine's wait is
   Truffle-blocked and also ends when the scheduler aborts its calls (teardown,
   or another call abandoned during teardown). A transfer on a descriptor
   (`rd`/`wr` table entries) first `poll(2)`s the descriptor together with a
   per-scheduler abort `eventfd`; the abort makes it return `EINTR` without
   issuing the transfer, so the off-pool thread ends as well. Alternatives
   rejected: `pthread_kill`/signals (would need a native signal handler
   installed into the JVM, and `SA_RESTART` semantics are process-wide);
   making descriptors non-blocking (visible to other processes sharing the
   descriptor, e.g. stdin); timeouts with retry (latency and busy wakeups);
   freeing buffers on cancel (memory corruption while the kernel writes).
3. **Process exit is Truffle's hard exit.** `main` returning is a natural exit:
   the eval returns 0. Every other exit — `os.Exit(n)`, an uncaught panic or
   a fatal error (2) — tears the scheduler down (goroutines are already
   frozen at the exit point, so nothing prints after it) and calls
   `TruffleContext.closeExited(null, n)`: the eval throws a
   `PolyglotException` with `isExit()` and `getExitStatus() == n`;
   `exitContext` freezes the program for an exit started elsewhere and
   flushes the context's streams (gc has no exit hooks and runs no defers on
   exit). `graalgo-bundle-run` maps the exit status to the process status.
   The legacy v1 wire eval keeps its `int` result until M3.
4. **Teardown never trips the scheduler's invariants.** A goroutine ended by
   the context's cancellation or exit (anywhere, even parked with its slot
   released) stops the drainer before its EXIT token, the drainer does not
   halt the VM on an invariant violation during teardown, and a goroutine
   that ends releases the pin lock its thread still holds (a cancellation
   can unwind it from inside a pin section, where no deferred `procUnpin`
   runs).
5. **No extra Truffle poll in our slow path.** Truffle polls at `LoopNode`
   back-edges, root returns and in compiled loops; the probe's spinners
   (plain loops and a backward `goto` loop, interpreted and compiled) answer
   cancellation within the budget without one, and a `TruffleSafepoint.poll`
   added to `Safepoint`'s full check made no measurable difference (the
   mutation that removed it stayed green), so it was not kept: untested
   code is not added. Revisit if a loop shape without a Truffle poll appears
   (`TraceMissingSafepointPollInterval`, the nightly safepoint-stress mode
   of the audit's §7).
6. **Native access is declared.** A bundle eval fails unless the context
   allows native access (its system calls are Panama downcalls).

## Consequences

- `test/m1r/cancel.sh` (`make test-goroutine-modes`) checks, in both modes:
  close(true) within 1 s with goroutines blocked on channels, select, Mutex,
  WaitGroup, timers, sleep, read(2) on a pipe and on stdin, panicking and
  spinning (plain and `goto` loops, 10k parked in carrier mode); a
  thread-local action reaching every carrier / parked goroutine thread; a
  sampler recording samples; os.Exit and panic as hard exits; no thread left.
- Remaining limits: a may-block call that is not a descriptor transfer
  (`openat` of a FIFO, `fsync`) cannot be woken — its goroutine leaves at
  once but the off-pool thread stays until the kernel returns; a write larger
  than `PIPE_BUF` that blocks after `poll` reported room is likewise not
  interruptible. The environment is still read from `/proc/self/environ`,
  not `Env.getEnvironment()` (F-10.3 residue: the embedder's environment
  policy is bypassed). Process-global runtime state (the pin lock, the
  scheduler slot) remains until WP-TR-AUDIT-7.
- Triggers to revisit: Truffle releases with blocking-native-call support
  for downcalls; the per-context runtime state of WP-TR-AUDIT-7 (the abort
  `eventfd` and the pin lock become per context).
