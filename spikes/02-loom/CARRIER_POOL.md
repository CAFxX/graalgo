# Carrier-pool customization on JDK 25: how GOMAXPROCS maps
## (spike 02-loom research note; corrects T5 §3.6)

## Verified against primary source
JDK 25 sources, `~/tools/graalvm-jdk-25/lib/src.zip`,
`java.base/java/lang/Thread.java` (class javadoc, system-property table) and
`java.base/java/lang/ThreadBuilders.java`
(`VirtualThreadBuilder(Executor)` constructor).

## Findings

1. **The public `Thread.Builder.OfVirtual` API has NO `scheduler(Executor)`
   method on JDK 25.** The `VirtualThreadBuilder(Executor scheduler)`
   constructor exists but is package-private, marked `// invoked by tests`.
   A virtual thread's scheduler is chosen at construction: explicit (tests
   only) or inherited from the creating virtual thread, else the JVM-wide
   default scheduler.

2. **The default carrier pool is per-JVM and configured ONLY via two system
   properties** (documented in `Thread`'s class javadoc):
   - `jdk.virtualThreadScheduler.parallelism` (default: available processors)
   - `jdk.virtualThreadScheduler.maxPoolSize` (default: 256)
   (T7 §4.1 mentioned a third, `.minRunnable`; it does not exist in the JDK
   25 source — only the two above.)

3. **T5 §3.6's claim** — "`runtime.GOMAXPROCS(n)` → `Thread.ofVirtual()`
   `.scheduler(new ForkJoinPool(n))` `[SECONDARY-ONLY]`" — **is incorrect
   for JDK 25's public API.** There is no such builder method. The correct
   mechanism is launcher-level: the graalgo launcher translates
   `GOMAXPROCS=n` (env or flag) into
   `-Djdk.virtualThreadScheduler.parallelism=n` (and optionally
   `.maxPoolSize`) **before JVM boot**.

4. **Consequence: in-process `runtime.GOMAXPROCS(n)` changes cannot resize
   the carrier pool via any public API.** `ForkJoinPool` has no public
   `setParallelism`. This is a **documented semantic deviation**: Go applies
   GOMAXPROCS process-wide at call time (existing goroutines included);
   graalgo can only fix it at process start. Workloads that change
   GOMAXPROCS at runtime (rare; mostly benchmarks) will observe the
   launch-time value. Re-test trigger: JDK exposes a public per-builder
   scheduler API or a resizable carrier pool.

5. **Default already matches:** unset `GOMAXPROCS` → Go uses NumCPU;
   unset property → carriers = available processors. Same default, no work.

## What the spike validated (O5)
Throughput scaling shape with the property set per-launch (1, 2, 4) is
monotonic to core count and plateaus under oversubscription, matching the
Go GOMAXPROCS shape qualitatively (see REPORT.md). The mechanism is
adequate for launch-time GOMAXPROCS; it is not a runtime control.
