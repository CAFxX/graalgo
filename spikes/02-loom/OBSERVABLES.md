# Scheduling observables for goroutine⇄virtual-thread conformance
## (spike 02-loom; proposed close-out of SPEC §10 Q2)

For each observable: what must match gc, and how it is measured. All
differential measurements are gc (oracle) vs the graalgo concurrency runtime.

### O1 — Preemption fairness under CPU saturation (LOAD-BEARING)
**Must match:** N CPU-bound goroutines must not starve other goroutines.
gc bounds the unfairness window via async preemption (~10ms, forcePreemptNS).
**How measured:** preemption-torture: N spinners (tight loop, back-edge
safepoint) + one latency pair ping-ponging on unbuffered channels.
Report: per-spinner iteration CV, ping-pong RTT p50/p99/max, sample count.
**Verdict rule:** systematic RTT collapse or sample starvation that survives
yield-density increases = FAIL (kill trigger §8.2).

### O2 — Channel send/recv fairness
**Must match:** waiters are paired FIFO; with K senders × M messages each on
one channel, per-sender receive counts must be equal within noise.
**How measured:** 8 senders × 20k msgs, unbuffered and buffered(16); report
per-sender counts and max/min ratio. (Implementation note: Go's unbuffered
handoff is FIFO by sudog queue order; the model uses FIFO ArrayDeques.)

### O3 — Select fairness
**Must match:** `select` over multiple ready cases chooses pseudo-randomly
(Go: fastrand poll order); distribution over K selects must be ~uniform.
**How measured:** 8 pre-filled buffered channels, 20k selects; report
per-case hits and max |z|-score vs uniform (binomial). |z| > 5 systematic
across runs = divergence.

### O4 — Timer latency distribution
**Must match:** `time.Sleep(d)` / `time.After(d)` wake latency distribution
under load must be within the same order as gc (p50/p99, not exact equality —
timers are inherently scheduler-dependent).
**How measured:** 10ms sleep loop × 1000 samples with N spinners running;
report excess-over-requested p50/p99/max. Systematic order-of-magnitude
regression vs gc = divergence.

### O5 — GOMAXPROCS throughput scaling shape
**Must match:** aggregate CPU throughput vs GOMAXPROCS must be monotonic
increasing to #cores, then plateau; oversubscription must not collapse.
**How measured:** 16 workers × fixed digest, 4s each at parallelism 1,2,4
(launcher translates GOMAXPROCS=n to
`-Djdk.virtualThreadScheduler.parallelism=n`); report Mops/s and
scaling ratios. Shape divergence (non-monotonic, collapse) = divergence.
Absolute numbers are NOT compared (different JITs, different per-op cost).

### O6 — Yield overhead (not a conformance observable; a cost bound)
**Must hold:** per-back-edge safepoint cost must be small enough that
CPU-bound Go code doesn't regress pathologically vs gc.
**How measured:** tight loop ns/iter with safepoint OFF/POLL/UNCOND vs gc's
loop. Note: the prototype measures the interpreted-model cost (explicit poll
call); PE-compiled code folds it to a branch — these numbers are an upper
bound for compiled mode, stated, not a prediction.

## Out of scope for this spike (noted, not measured)
- `runtime.LockOSThread` (no vthread equivalent; bound platform threads are a
  separate allocation — T5 §3.2).
- Netpoller integration (deleted in favor of blocking NIO; owned by runtime track).
- Deep-recursion stack limits (T5 §8.3.6, separate experiment).
- GOMAXPROCS *changes at runtime* (no public API to resize carriers; see
  CARRIER_POOL.md — documented deviation).
