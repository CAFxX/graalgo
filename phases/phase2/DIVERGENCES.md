# Phase-2 divergences from gc

Accepted behavioral divergences between the graalgo Phase-2 runtime and the
stock gc toolchain. Each entry records the divergence, why it is accepted,
and the observable consequence. New divergences are added by the workstream
that introduces them, in the same commit as the implementation.

## P2-M4 (GC interop, ADR-0002)

### DIV-M4-1: finalizer runs with a nil argument (no resurrection)

`runtime.SetFinalizer(x, f)`: gc invokes `f(x)` with the original object,
which the finalizer may resurrect by storing it. graalgo invokes `f(nil)`.
A `PhantomReference` action is denied the referent by construction, and the
original Go object cannot be recovered on a pure JVM heap.

Consequence: finalizers that dereference their argument diverge loudly
(nil-pointer panic inside the finalizer, reported on stderr by the finalizer
thread) rather than silently. Finalizers that only perform cleanup keyed by
captured state (close a handle, decrement a counter, signal via a global)
behave identically.

### DIV-M4-2: finalizer runs on a JVM thread, not a goroutine

gc runs finalizers on a dedicated finalizer goroutine with a full Go stack:
channel ops, `go` statements, and blocking syscalls work. graalgo runs them
on the single JVM finalizer thread (`graalgo-finalizer`). Touching
scheduler/channel/timer state from a finalizer body fail-fasts; use a global
flag + poll loop instead (see `corpus/gc/gc01_finalizer.go`).

Serialization is preserved: at most one finalizer runs at a time, in
enqueue order, like gc.

### DIV-M4-3: `runtime.GC()` is a bounded approximation

gc's `runtime.GC()` blocks until a GC cycle completes. graalgo calls
`System.gc()` then polls GC MXBean collection counts until they advance,
with a 30 s bound. If explicit GC is disabled (`-XX:+DisableExplicitGC`)
the counts never advance and the call returns after the timeout anyway.

Consequence: callers must not depend on reclamation having completed when
`runtime.GC()` returns. The `NumForcedGC` memstat counts invocations, not
completed cycles.

### DIV-M4-4: unmappable `runtime.MemStats` fields are absent

`GcInterop.memStats()` returns only fields with a JVM MXBean analog:
`HeapAlloc` (heap used), `HeapSys` (heap committed), `Sys`
(heap+non-heap committed), `NumGC`, `PauseTotalNs`, `NumForcedGC`,
`PauseNsLast` (most recent pause, ns). The allocator-internals fields
(`BySize`, `HeapObjects`, `TotalAlloc`, `Mallocs`, `Frees`, `HeapIdle`,
`HeapInuse`, `HeapReleased`, `StackInuse`, `StackSys`, `MSpanInuse`,
`MSpanSys`, `MCacheInuse`, `MCacheSys`, `BuckHashSys`, `GCSys`, `OtherSys`,
`NextGC`, `LastGC`, `GCCPUFraction`) have no MXBean analog and are absent
from the map (not zero — absent, so consumers can distinguish).

Rationale: a JVM heap has no spans/mcaches; fabricating the numbers would
be worse than omitting them.

### DIV-M4-5: `weak.Pointer` has no runtime finalizer interaction

Go's `weak.Pointer` integrates with the runtime's finalizer ordering.
graalgo's vehicle is a plain `java.lang.ref.WeakReference`; finalizer
ordering relative to weak clearing follows JVM semantics (phantoms enqueue
after weak clearing), which matches gc's observable order in practice but
is not contractual.

**Scope note (P2-M4):** `weak.Pointer` and `runtime.ReadMemStats` have no Go
call path in P2-M4 — the `gcinterop:` marker dispatch handles only
`SetFinalizer`/`KeepAlive`/`GC`, and the synthetic `runtime` package
declares only those three (ADR-0002: "frontend scope permitting" for
`weak`). `newWeakRef`/`weakRefValue`/`memStats()` are implemented and
unit-tested as vehicles for future wiring, not live surface.

### DIV-M4-6: `SetFinalizer` on an `unsafe.Pointer`-held object silently never runs

`unsafe.Pointer` values lower to `UnsafeHandles` — a `HashMap<Long,Object>`
of strong JVM references (SPEC §3.4). An object reachable *only* through
`UnsafeHandles` is a permanent strong root: it is never collected, so a
`SetFinalizer` registered on it never fires, silently. (ADR-0002 `*unsafe*`
interaction bullet; recorded here per its requirement.)

This is inherent to the handle design, not a bug: the handles exist
precisely to keep the referent alive across the "pointer as integer"
boundary. Programs that convert pointers to integers and back must not
expect finalization while a handle is live.

### DIV-M4-7: `runtime.SetFinalizer` on nil is `IllegalArgumentException`, not fatal

gc raises a **fatal** `runtime.SetFinalizer: pointer not in allocated block`
for a nil pointer argument. graalgo throws `IllegalArgumentException`
("nil object"), which escapes the interpreter as a Java exception rather
than a Go fatal. Loud, but a different mechanism and message.

### DIV-M4-8: `pprof` heap profiles out of scope for P2-M4

`net/http/pprof` heap profiles (`/debug/pprof/heap`) have no P2-M4
vehicle. A faithful heap profile would require walking the JVM heap with
Go-type attribution; the JVMTI/`jmap` surface exposes JVM types, not Go
types. Stance: out of scope; revisit when the object model carries enough
type metadata for attribution (Phase 4+).

### DIV-M4-9: `debug.FreeOSMemory` unsupported (no JVM analog)

`debug.FreeOSMemory()` forces a GC *and* returns memory to the OS. There is
no JVM analog: `System.gc()` suggests collection but does not compel the OS
to take pages back (heap shrinking is the collector's decision). Per ADR-0002
D4 ("no JVM analog (`System.gc()` is not it)"), the stance is
unsupported-with-justification. No Go call path is wired in P2-M4.

## Template for new entries

### DIV-<M>-<N>: short title

What diverges, why it is accepted, and the observable consequence.

## P2-M5 (GODEBUG surface triage, WS E)

### DIV-M5-1: unknown GODEBUG knob is a hard error

gc silently ignores GODEBUG settings it does not recognize. graalgo
exits 2 with `graalgo-run: fatal error: unknown GODEBUG setting "..."`.
Rationale: a typo (`GODEBUG=schedtace=100`) silently doing nothing is a
worse failure mode than failing loud at startup; the full supported
registry is published in `phases/phase2/GODEBUG.md`.

### DIV-M5-2: malformed GODEBUG value is a hard error

gc keeps the old value when a GODEBUG value does not parse as an int32.
graalgo exits 2. Same rationale as DIV-M5-1.

### DIV-M5-3: bare GODEBUG field without '=' is a hard error

gc skips fields without `=`. graalgo exits 2 suggesting `field=1`.
Same rationale as DIV-M5-1.

### DIV-M5-4: emulated trace formats are graalgo-shaped

`GODEBUG=schedtrace` prints `SCHED <ms>ms: goroutines=N cap=C
carriers=K` (gc prints P/M fields that have no counterpart in the
park/queue/drainer design); `GODEBUG=gctrace` prints per-cycle lines
with JVM-heap numbers (gc prints allocator-level detail). The emulated
observable is "a periodic/per-cycle scheduler/GC line on stderr",
verified differential by `tools/godebug-diff` (4/4 green). Byte-identity
is not attempted.

### DIV-M5-5: effective behavior permanently equals a non-default value

For five NO-OP knobs the runtime cannot exhibit gc's default behavior,
so the effective behavior always equals the noted non-default value
regardless of what is set (even the default):

| knob | gc default | graalgo effective | why |
|---|---|---|---|
| `asyncpreemptoff` | 0 | 1 | no signal-based async preemption exists |
| `dontfreezetheworld` | 0 | 1 | no stop-the-world phase exists to freeze |
| `containermaxprocs` | 1 | 0 | no container CPU-quota detection; never applied |
| `updatemaxprocs` | 1 | 0 | same |
| `tracebacklabels` | 1 | 0 | goroutine labels cannot exist (no pprof label API) |

These stay NO-OP (accepted, ignored) rather than UNSUPPORTED because
denying gc's *default* value would be hostile for zero benefit: the
program cannot get the default behavior either way, and every setting
is observably identical inside this runtime. The triage principle is:
deny when the user would observe missing output/behavior they asked
for (UNSUPPORTED); accept when the knob cannot have observable effect
here (NO-OP) — and document the effective value here.
