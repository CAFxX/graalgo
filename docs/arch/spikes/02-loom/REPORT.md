# Spike 02-loom: goroutines on Loom — REPORT

**Verdict: FAIL** (kill trigger §8.2 fired — see §6)

Date: 2026-10-03. Prototype: [proto/](proto/) (Java, GraalVM JDK 25.0.4). Oracle:
stock Go 1.27.1. Machine: 2 vCPU / 7 GB. Sources checksummed in
[logs/SOURCES.sha256](logs/SOURCES.sha256); run manifest in [logs/RUN_MANIFEST.txt](logs/RUN_MANIFEST.txt).

---

## 1. What was built

A Java prototype of the T5 §3 design: one virtual thread per goroutine
(`loomsim/Go.java`, named `graalgo-goroutine-N`), a back-edge safepoint
mechanism (`loomsim/Safepoint.java` — the exact call the Truffle AST's
loop back-edge / function-entry nodes would emit), a per-goroutine preempt
flag set by a 10 ms watchdog thread (`loomsim/Watchdog.java`, modeling Go's
`forcePreemptNS` sysmon retake), plus model channel/select/timer
(`GoChannel`/`GoSelect`/`GoTimer`). Four safepoint modes: OFF (control),
UNCOND (yield every back-edge), POLL (yield only when own preempt flag set —
the production-shaped design), PARK (timed-park instead of yield — workaround
experiment).

Two design corrections vs the research docs, both verified against the JDK
25 primary source (`~/tools/graalvm-jdk-25/lib/src.zip`):
1. **T5 §3.6 is wrong:** there is NO public `Thread.ofVirtual().scheduler(Executor)`
   on JDK 25 (`ThreadBuilders.VirtualThreadBuilder(Executor)` is package-private,
   "invoked by tests"). Carrier-pool sizing is per-JVM via system properties
   `jdk.virtualThreadScheduler.parallelism` / `.maxPoolSize` only — GOMAXPROCS
   fidelity is a **launcher** concern (translate at boot). In-process
   `runtime.GOMAXPROCS(n)` cannot resize carriers — documented deviation.
   Details: [CARRIER_POOL.md](CARRIER_POOL.md).
2. **T7's `-Djdk.tracePinnedThreads` is gone** (absent from JDK 25 sources).
   The pinning audit path is the JFR event `jdk.VirtualThreadPinned`.

The scheduling-observable list (O1–O6) with per-observable measurement
methods is defined in [OBSERVABLES.md](OBSERVABLES.md) — this is the proposed close-out of
SPEC §10 Q2.

## 2. O1 — Preemption torture (THE falsifying experiment)

2 CPU-bound spinner goroutines + one latency pair ping-ponging on
unbuffered channels; GOMAXPROCS=2 / `parallelism=2`.

| impl | mode | samples/5s | RTT p50 | RTT p99 | spinner CV |
|---|---|---|---|---|---|
| Go 1.27.1 | — | 200,000 (cap) | 771 ns | 1,122 ns | 0.12 |
| proto | OFF | **0** | — (starved) | — | 0.09 |
| proto | POLL (10 ms) | **0** | — (starved) | — | 0.07 |
| proto | POLL (1 ms watchdog) | **0** (6 s) | — | — | 0.08 |
| proto | UNCOND | 0–2 | — | — | 0.17 |
| proto | PARK (1 µs–1 ms) | **0** | — | — | 0.05 |

Also: 15 s POLL run → 0 samples; parallelism=1 POLL → 0 samples
(925 yields honored); Go GOMAXPROCS=1 → 200,000 samples, p50 892 ns.

**The pair is strictly starved on the prototype under saturation; Go is
unaffected (p50 < 1 µs either way).** The yields ARE honored (752 yields in
6 s at 10 ms watchdog; 2,113 at 1 ms) — they just don't produce fairness.

## 3. Root cause (verified, not inferred)

Go's P runqueue is FIFO with a `runnext` slot: a preempted goroutine goes to
the back, a *woken* goroutine goes to the front. Loom's carrier is a
`ForkJoinPool` whose local queues are **LIFO** and which has no runnext
equivalent (verified in JDK 25 `VirtualThread.afterYield()` +
`ForkJoinPool` sources):
- `Thread.yield()` resubmits the continuation to the carrier's **own LIFO
  queue** (or a probe queue); the carrier immediately re-pops the spinner.
- A woken goroutine (channel notify → resubmit) lands **under** the
  spinner's ever-repushed continuation and never surfaces.
- With #spinners ≥ #carriers, no carrier ever goes idle, so no work-stealing
  rescues the buried continuation. Starvation is **unbounded**, not long-tailed.

Confirming evidence:
- O4 (timer latency): wakeups issued from a **platform** thread (external
  submit path) schedule fine — prototype p50 excess 480 µs vs Go 11.9 ms
  under the same load. The pathology is specific to vthread→vthread wakeups
  through the carrier-local LIFO queue.
- Direct probe: a freshly-unparked virtual thread never runs under
  saturation, even when unparked from a platform thread.
- Platform-thread "unblocker" workaround (route wakeups externally): **still
  0 samples** — the burial is in the FJP queue discipline itself, not the
  wakeup path.

## 4. Other observables — no divergence

- **O2 channel fairness:** 8×20k msgs, cap 0 and 16 — per-sender counts
  exactly equal on both (max/min = 1.0000). Match.
- **O3 select fairness:** 8 pre-filled channels, 20k selects — Java
  max|z|=1.09, Go max|z|=1.58 (both uniform noise). Match.
- **O4 timer latency:** above; prototype p50 *better* than Go under load
  (dedicated platform timer thread), p99 same order. No adverse divergence.
- **O5 GOMAXPROCS scaling:** monotonic to core count then plateau on both
  (Java 64.8→78.7→138.3; Go 251.5→460.0→723.1 Mops/s at 1/2/4; noisy VM).
  Shape matches; absolute values not comparable (different JITs).
- **O6 yield overhead** (ns/back-edge, interpreted-model upper bound):
  Go 0.7–0.9; OFF 3–9; POLL 8–18; UNCOND 23,000–57,000 (unmount/remount —
  confirms unconditional yield is not viable).

## 5. Pinning audit

JFR `jdk.VirtualThreadPinned` over channel ping-pong (2k), select block,
timer sleep, `synchronized`+`wait`: **0 events** — JEP 491 holds; the
runtime's blocking ops unmount cleanly. Controls attempted (pipe/socket/
FIFO native block, FFM `sleep(2)`) produced no events either — the event
fires on block-while-pinned, which this workload never does. No pinning
risk found in the runtime's own paths. (Note: JFR files capture env vars
including internal runtime metadata — counts were extracted
programmatically; the `.jfr` files are not for export.)

## 6. Kill assessment — trigger FIRED

Study §8.2 kill trigger: *"FAIL if systematic scheduling-observable
divergence vs gc that back-edge yields cannot close"* (systematic =
reproduces across runs and survives yield-density increases).

- **Systematic:** 10+ runs, 0 samples every time, at 5 s and 15 s, at
  parallelism 1 and 2. Not flaky.
- **Survives yield-density increases:** OFF → POLL@10 ms → POLL@1 ms (~7× pulse
  rate: 579→3,984 pulses/6 s) → UNCOND (every back-edge) → PARK (1 µs–1 ms).
  All 0 samples.
- **Mechanism is structural**, not a tuning gap: Loom's default FJP
  scheduler has LIFO local queues and no woken-thread priority; the public
  JDK 25 API offers no way to install a custom carrier scheduler or change
  queue discipline. Back-edge yields address "Loom never preempts" but not
  "Loom schedules unfairly" — and the latter is what Go's preemption
  fairness actually depends on (FIFO + runnext).
- **Impact:** breaks a fundamental Go liveness property. Idiomatic
  spin-wait (`for !done {}` with GOMAXPROCS spinners) hangs on the
  prototype while working on gc. This is not a paper cut; it is a semantic
  cliff that would break real programs silently (hangs, not errors).

**Scoped verdict:** back-edge safepoint yields in modes OFF/UNCOND/POLL/PARK
cannot achieve Go's preemption fairness on Loom's default ForkJoinPool
carrier scheduler (GraalVM JDK 25.0.4, 2-carrier saturation) because woken
continuations are buried under spinner resubmits in LIFO local queues.
**Re-test triggers:** (a) JDK exposes a public per-builder virtual-thread
scheduler API; (b) JDK changes carrier queue discipline for YIELDED
continuations (FIFO / runnext-equivalent); (c) a JDK-change-free workaround
is demonstrated (the platform-thread unblocker was tried and failed).

**Program decision (for Carlo):** the trigger fires without a scoped
workaround. Options: (1) re-scope — accept unbounded starvation under CPU
saturation as a documented semantic cliff (breaks real Go programs; hard to
defend for a "drop-in" toolchain); (2) re-scope — abandon 1:1 vthreads for a
custom M:N scheduler in the Truffle runtime (major redesign, loses most of
Loom's value); (3) park the design on the re-test triggers above; (4) kill
the program. My recommendation: do not proceed with 1:1 vthreads on the
default scheduler past Phase 0 without one of (2)/(3) decided.

## 7. Deliverables

- [REPORT.md](REPORT.md) (this file), [OBSERVABLES.md](OBSERVABLES.md) (SPEC §10 Q2 proposal),
  [CARRIER_POOL.md](CARRIER_POOL.md) (JDK 25 carrier-customization research note)
- Prototype: `[proto/src/loomsim/](proto/src/loomsim/)*.java` (Go, Goroutine, Safepoint,
  Watchdog, GoChannel, GoSelect, GoTimer), `[proto/src/work/](proto/src/work/)*.java`
  (PreemptTorture, ChanFairness, SelectFairness, TimerLatency,
  GomaxprocsScale, YieldOverhead, PinAudit, UnblockerProbe),
  [proto/build.sh](proto/build.sh)
- Go oracle programs: [go/o1_torture.go](go/o1_torture.go), `o2_chan.go`, `o3_select.go`,
  `o4_timer.go`, `o5_scale.go`, `overhead.go` (+ `go/bin/` binaries)
- Checksums: [logs/SOURCES.sha256](logs/SOURCES.sha256); manifest: [logs/RUN_MANIFEST.txt](logs/RUN_MANIFEST.txt);
  JFR: [logs/jfr/](logs/jfr/) (see §5 caveat); debug scratch: [scratch/](scratch/) (not part of
  the evidence chain)

## 8. Notes / loose ends

- The prototype channel/select are *models*, not ports; O2/O3 validate
  their fairness properties, not their performance (the select blocking
  path is slow — irrelevant to the verdict).
- Two measurement bugs were found and fixed during the spike (sample-count
  publication; a global-flag yield storm in the first POLL design) — both
  documented in the trajectory, all O1 numbers above are post-fix.
- [scratch/](scratch/) contains debugging probes (DumpTorture, SelMini, DirectUb…);
  kept for reproducibility of the investigation, not as deliverables.
