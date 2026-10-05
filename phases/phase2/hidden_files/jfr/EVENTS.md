# P2-M6 JFR evidence — done-gate 3

Goroutine lifecycle + channel events flowing, consumable by standard JFR
tooling. Workstream F (GG-0008 JFR slice → active).

## Event schema (`graalgo.jfr`, all `@Enabled(false)` — opt-in)

| Event | Fields | When emitted |
|---|---|---|
| `graalgo.GoroutineStart` | `goroutineId`, `threadName` (`goroutine-<id>`) | vthread passes first admission |
| `graalgo.GoroutineEnd` | `goroutineId`, `lifetimeNanos` | vthread finally block, before EXIT token |
| `graalgo.GoroutinePark` | `goroutineId`, `reason`, `durationNanos` | on resume (see rate control) |
| `graalgo.ChannelBlock` | `goroutineId`, `channelId`, `operation` (SEND/RECV/SELECT) | operation genuinely blocks, before parking |

`reason` ∈ CHANNEL, TIMER, SELECT, QUANTUM, IO, UNKNOWN. `channelId` is a
per-`GoChan` diagnostic id; `-1` = multi-channel select. Non-blocking
fast-path channel ops emit nothing by design (rate control).

The JDK's `jdk.VirtualThreadStart` fires for our vthreads for free and
correlates via `threadName`; `jdk.VirtualThreadPinned` is consumed by the
P2-M7 pinning audit.

## Enabling

```java
Recording rec = new Recording();
rec.enable("graalgo.GoroutineStart").with("stackTrace", "false");
rec.enable("graalgo.GoroutineEnd").with("stackTrace", "false");
rec.enable("graalgo.GoroutinePark").with("stackTrace", "false");
rec.enable("graalgo.ChannelBlock").with("stackTrace", "false");
```

or on a live JVM via `jcmd`:

```
jcmd <pid> JFR.start name=graalgo \
  '+graalgo.GoroutineStart#enabled=true' \
  '+graalgo.GoroutineEnd#enabled=true' \
  '+graalgo.GoroutinePark#enabled=true' \
  '+graalgo.ChannelBlock#enabled=true'
```

Disable stack traces: these are high-rate scheduler events; stacks
would dominate the recording.

## Rate control (measured, not assumed)

- **Park threshold**: non-quantum parks commit only if ≥ 100µs
  (`-Dgraalgo.jfr.parkThresholdNanos=`). Quantum parks are the scheduler
  hot loop — measured 26% exceed 100µs, so a threshold *cannot* filter
  them; they are **sampled 1-in-1024** instead (deterministic via parkSeq).
- **Default-off**: `@Enabled(false)` on all four events. JFR enables
  custom events by default for *any* active recording (caught by
  `testNoEventsWhenDisabled`, which was red before the fix) — without
  this, an unrelated recording would silently flip our events on.
- **Direct enablement checks**: `Event.isEnabled()` measures ~8ns on the
  pinned toolchain (GraalVM 25.0.4.1.1) — called directly on the park
  path, no caching layer. (An early flawed probe reported ~600ns and a
  caching design was built on it; review re-measured single-digit ns on
  two JVMs and the cache was removed.)

## Overhead (this VM: AMD EPYC 9D25 @ 1.5GHz; `System.nanoTime()` ~60ns here)

- Disabled guard (notePark + parkResumed, no recording): **~23ns/park**,
  0.38× one `System.nanoTime()` — i.e. well under a single clock read.
  A quantum-park round-trip is tens of microseconds; the guard is < 0.1%.
  (Earlier docs said 60ns/0.63× from a pre-review probe and ~10ns in a
  class comment; 23ns/0.38× is the re-measured value after the
  enablement-cache removal.)
- Yield hot loop, recording active with events enabled vs no recording:
  throughput within noise (smoke test; the precise claims are the guard
  cost above and the filter test below).

## Recorded evidence

`p2-m6-evidence.jfr` (this directory): channel+timer workload
(3 blocking recvs, delayed sender, 400ms sleep, 200 yields) under a
recording with all four graalgo events + `jdk.VirtualThreadStart`.

```
jfr print --events graalgo.GoroutineStart,graalgo.GoroutineEnd,\
  graalgo.ChannelBlock,graalgo.GoroutinePark p2-m6-evidence.jfr
```

Event counts (`jfr summary`):

| Event | Count |
|---|---|
| graalgo.GoroutineStart | 4 |
| graalgo.GoroutineEnd | 4 (all paired with a start) |
| graalgo.GoroutinePark | 4 (3× CHANNEL: 274ms, 5ms, 8ms; 1× TIMER: 400ms) |
| graalgo.ChannelBlock | 3 (all RECV; the sends completed via direct handoff and correctly emit nothing) |
| jdk.VirtualThreadStart | 4 (thread names `goroutine-<id>`, correlating) |

## gc boundary (DIV-M6-1)

`runtime/trace` is out of scope: it traces the gc runtime's internal
scheduler states, which have no meaningful mapping onto a JVM-hosted
user-space scheduler. JFR is the JVM-native observability surface
(SPEC §2.7/§3.10); the mapping is documented per event above, and
`go tool trace` compatibility is not claimed.

## Tests

`graalgo.jfr.JfrEventsTest` (6 tests): event flow with field validation,
start/end pairing, no-events-when-disabled (red-green for the
`@Enabled(false)` fix), hot-loop filter (deterministic 1-in-1024
sampling), disabled guard cost (< 2× nanoTime), yield throughput smoke,
IO interval spanning the syscall (red-green for the begin/commit fix).
