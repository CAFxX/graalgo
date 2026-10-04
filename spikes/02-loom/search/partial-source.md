# Partial: JDK 25 source verification (local src.zip, ~/tools/graalvm-jdk-25/lib/src.zip)

All claims below verified against the extracted source, not inferred.

## 1. Default vthread scheduler construction — CONFIRMED, with a correction

`java.base/java/lang/VirtualThread.java:1429-1454` (`createDefaultScheduler()`):
- `boolean asyncMode = true; // FIFO` → `new ForkJoinPool(parallelism, factory, handler, asyncMode, 0, maxPoolSize, minRunnable, pool -> true, 30, SECONDS)`.
- Worker (carrier) `WorkQueue`s inherit the pool config including the FIFO bit (`ForkJoinWorkerThread.java:78`: `new ForkJoinPool.WorkQueue(this, 0, (int)pool.config, ...)`).
- **CORRECTION to REPORT.md §3:** the report says "Loom's carrier is a ForkJoinPool whose local queues are LIFO". The source shows `asyncMode=true` (FIFO) for the default vthread scheduler. Shared *submission* queues are created with cfg=0 (`ForkJoinPool.java:2582`: `new WorkQueue(null, id, 0, false)` → no FIFO bit → LIFO owner-poll), but carriers poll shared queues only via the scan path which takes from base (FIFO end). The "LIFO local queues" mechanism as written does not match the source.

## 2. Yield resubmission path — CONFIRMED, more conditional than reported

`VirtualThread.afterYield()` (`VirtualThread.java:547-585`), YIELDING branch (lines 577-585):
- If the carrier's local queue is empty → `externalSubmitRunContinuation(pool)` → `ForkJoinPool.externalSubmit` → probe-chosen **shared submission queue** (stealable).
- Else → `submitRunContinuation()` → `execute()` → `poolSubmit()`.
- The report's "resubmits the continuation to the carrier's own LIFO queue" is an oversimplification: the shared-queue path is taken whenever the carrier's local queue is empty (the common case in a spinner workload), and queue discipline is FIFO where it matters.

## 3. `poolSubmit` carrier affinity — VERIFIED (ForkJoinPool.java:2614-2627)

```java
if (((t = JLA.currentCarrierThread()) instanceof ForkJoinWorkerThread) &&
    (wt = (ForkJoinWorkerThread)t).pool == this) {
    internal = true;
    q = wt.workQueue;   // carrier's OWN queue
} else {
    q = submissionQueue(ThreadLocalRandom.getProbe(), true);  // shared queue
}
```
Submissions made while executing on a carrier — including `afterYield` resubmits and **unparks issued by mounted vthreads** — land in **the carrier's own work queue**, not a probe-chosen shared queue. (The report did not identify this path.)

## 4. Unpark path — VERIFIED (VirtualThread.java:848-884)

`unpark()` → `unpark(false)` → if PARKED→UNPARKED, `submitRunContinuation()` → `execute()` → `poolSubmit()` → carrier's own queue (by §3) when unparked by a mounted vthread; shared submission queue when unparked by a platform thread.

## 5. No public custom-scheduler API — CONFIRMED

`java.base/java/lang/ThreadBuilders.java:211-219`: `VirtualThreadBuilder(Executor scheduler)` is package-private with comment "invoked by tests". There is no public `Thread.ofVirtual().scheduler(...)` on JDK 25. The spike's claim holds exactly.

## 6. System properties — CONFIRMED, sizing only

From `createDefaultScheduler()`: `jdk.virtualThreadScheduler.parallelism` (default = availableProcessors), `jdk.virtualThreadScheduler.maxPoolSize` (default = max(parallelism, 256)), `jdk.virtualThreadScheduler.minRunnable` (default = max(parallelism/2, 1)). No property affects queue discipline, fairness, or priorities. (Also note: `ForkJoinPool.setParallelism` exists but the pool is not exposed; irrelevant.)

## 7. Worker scan — VERIFIED (ForkJoinPool.java:1975+, `runWorker`)

Per scan round: xorshift-randomized start, odd step covering all queues; takes from base (FIFO end) via `poll()`; after each task, `topLevelExec(t, fifo)` drains the worker's **own** queue via `nextLocalTask(fifo)` before the next scan round. `nextLocalTask`: FIFO-from-base when >1 task, top-pop when exactly 1.

## 8. Open puzzle (honest)

Naive queue analysis of the above (randomized full-coverage scans, FIFO takes, unpark→carrier-own-queue→`nextLocalTask` pickup) predicts the O1 latency pair SHOULD make progress — yet 10+ runs show 0 samples, and the mechanism-independent unpark-from-platform-thread probe also showed burial. The precise queue-level mechanism of the observed starvation is therefore NOT established by static source reading; the report's LIFO-burial story is contradicted on the FIFO point. What IS established: (a) the empirical failure is systematic and mechanism-independent of the yield path; (b) there is no public API to change any of this; (c) 7 workaround families failed empirically. The kill verdict rests on (a)-(c), not on the mechanism story. If the web search turns up the actual known mechanism (e.g., a JBS issue on vthread scheduling unfairness), it goes here as the corrected root cause.
