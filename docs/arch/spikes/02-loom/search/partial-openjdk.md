# OpenJDK official sources: fair scheduling / custom scheduler for virtual threads

Track: JBS + JEPs + mailing lists (loom-dev, core-libs-dev) + JDK source (src.zip from GraalVM JDK 25 build, OpenJDK-derived; used as primary source for mechanism claims).
Date: 2026-10-03. Status: COMPLETE.

Question: is there ANY shipped / in-flight / credibly-proposed official way to get fair
(starvation-free) scheduling of Java virtual threads under carrier saturation, or to
customize/replace the virtual-thread scheduler?

## Candidate 1 — Loom repo "Custom Scheduler Prototype v2" (loom-dev, Feb 2026)

Description: experimental support in the openjdk/loom *project repo* (not mainline) for
plugging a custom virtual-thread scheduler two ways: (a) JVM-wide via
`-Djdk.virtualThreadScheduler.implClass=$EXECUTOR` (public class implementing
`java.util.concurrent.Executor`, public no-arg ctor, on classpath/module path), and
(b) per-thread via `Thread.Builder.OfVirtual.scheduler(Executor)`; tasks arrive as
`Thread.VirtualThreadTask`, which the scheduler must run on a platform thread. With a
fair FIFO executor behind it, the saturation-starvation mechanism disappears entirely
(the FJP carrier pool is replaced wholesale).

Primary sources:
- Docs page: https://github.com/openjdk/loom/commit/99eef9e610df4fa30ce6a19e8aa6422a8b3c923c
  ("The purpose of the experimental support is to allow exploration and provide feedback
  to help inform the project on whether to expose anything.")
- loom-dev thread: https://mail.openjdk.org/pipermail/loom-dev/2026-February/008323.html
  — Alan Bateman (2026-02-26): "In this prototype, the scheduler is configured
  system-wide and cannot be changed mid-flight. ... It can be re-visited if efforts in
  this area go further than the prototype. For now, the custom scheduler has to deployed
  on the class path or module path."
- Absence evidence: exact-string web search for `jdk.virtualThreadScheduler.implClass`
  returns ONLY the loom-dev thread; grep of JDK 25 `java.base/java/lang` sources finds no
  such property (only `parallelism`, `maxPoolSize`, `minRunnable`, `timerQueues`); the
  JEP index (https://openjdk.org/jeps/0, updated 2026-02-25) lists no draft/submitted/
  in-flight JEP for scheduler customization. => Not in any mainline JDK (21–25), not
  announced for 26/27.

Verdict: MITIGATES (as prototype). Mechanistically it would SOLVE the torture (fair
executor replaces the FJP), but: (a) loom-repo-only, no mainline release; (b) explicitly
"whether to expose anything" — zero commitment it will ever ship; (c) prototype API
surface is JDK-internal (`Thread.VirtualThreadTask`); (d) system-wide only, cannot be
changed mid-flight. Boundary: usable only on a loom-repo build.
Re-test trigger: if `implClass` (or equivalent) lands in a mainline EA build, re-run the
spike torture (2 yield-spinners + latency pair, `parallelism=2`) with a fair FIFO custom
scheduler; SOLVED iff 200,000/200,000 samples at p50 <1µs.

## Candidate 2 — JEP 444 "Scheduling virtual threads" section (delivered, JDK 21)

Description: the normative statement of the scheduler design.

Primary source: https://openjdk.org/jeps/444 (full text read). Verbatim:
- "The JDK's virtual thread scheduler is a work-stealing ForkJoinPool that operates in
  FIFO mode." (JDK 25 source confirms: `boolean asyncMode = true; // FIFO` in
  `VirtualThread.createDefaultScheduler()`.)
- "The scheduler does not currently implement time sharing for virtual threads. Time
  sharing is the forceful preemption of a thread that has consumed an allotted quantity
  of CPU time. While time sharing can be effective at reducing the latency of some tasks
  when there are a relatively small number of platform threads and CPU utilization is at
  100%, it is not clear that time sharing would be as effective with a million virtual
  threads."
- "Virtual threads have a fixed priority of Thread.NORM_PRIORITY. The
  Thread.setPriority(int) method has no effect on virtual threads. This limitation may be
  revisited in a future release."
- Only tuning knobs: `jdk.virtualThreadScheduler.parallelism`,
  `jdk.virtualThreadScheduler.maxPoolSize`. No hooks, no fairness configuration.

Verdict: IRRELEVANT as a solution (documents the mechanism, offers no knob). The
"time sharing" passage is the official statement that preemptive fairness is explicitly
out of scope — and it names exactly the spike's scenario ("small number of platform
threads and CPU utilization at 100%").

## Candidate 3 — `Thread.yield()` javadoc (JDK 25 source, java.base/java/lang/Thread.java)

"A hint to the scheduler that the current thread is willing to yield its current use of
a processor. The scheduler is free to ignore this hint."

Verdict: IRRELEVANT (documents the absence of a guarantee; no mechanism).

## Candidate 4 — JDK-8292969 "Bad Thread Utilization in ForkJoinPool"

Primary: https://bugs.openjdk.org/browse/JDK-8292969 — Status: Resolved/Fixed in 19.0.2
(backport JDK-8294762). 8000 yielding vthreads; some iterations used only 4 of 30+ FJP
workers despite queued work — a work-*distribution* bug, fixed. The spike's failure mode
is the opposite (workers busy, starvation under saturation) on JDK 25, six releases
after the fix.

Verdict: IRRELEVANT (fixed; different failure mode).

## Candidate 5 — JEP 491 "Synchronize Virtual Threads without Pinning" (delivered, JDK 24)

Primary: https://openjdk.org/jeps/491 (full text read). Addresses starvation/deadlock
from *pinned* carriers ("no virtual threads can run because all of the platform threads
available to the JDK's scheduler are either pinned by virtual threads or blocked in the
JVM"). The spike's torture has no pinning — yields are honored. No scheduler hooks,
no fairness knobs, no priority support in the JEP.

Verdict: IRRELEVANT (different starvation mechanism, already fixed in JDK 24).

## Candidate 6 — JEPs 446, 453, 480 (Scoped Values / Structured Concurrency previews)

Primary: https://openjdk.org/jeps/446, https://openjdk.org/jeps/453,
https://openjdk.org/jeps/480 (all read; full-text search for "scheduler" returns zero
hits in each). They fork work onto virtual threads but define no scheduling policy and
expose no scheduler API.

Verdict: IRRELEVANT (no scheduler content whatsoever).

## Candidate 7 — Scheduler tuning properties
(`jdk.virtualThreadScheduler.parallelism` / `maxPoolSize` / `minRunnable`)

Primary: JEP 444 + JDK 25 source (`VirtualThread.createDefaultScheduler()`). These set
carrier count and compensation ceilings; none changes queue discipline, adds preemption,
or prioritizes woken threads.

Verdict: IRRELEVANT to fairness (cannot fix saturation starvation at any setting).

## Candidate 8 — `ThreadBuilders.VirtualThreadBuilder(Executor)` via reflection

Description: JDK 25 source (`java.base/java/lang/ThreadBuilders.java`):
`class ThreadBuilders` is package-private; `VirtualThreadBuilder(Executor scheduler)` is
package-private with the comment `// invoked by tests`. Mechanistically, installing a
fair FIFO Executor here *would* fix the torture for vthreads created through that
builder. Access from user code requires `setAccessible` into `java.lang`, which strong
encapsulation blocks by default (`--add-opens java.base/java.lang=ALL-UNNAMED` needed),
is version-fragile JDK-internal API, and conflicts with graalgo's drop-in `alias
go=graalgo` requirement. The spike already classified it as "no public API"; nothing
found on this track changes that — the sanctioned exploration vehicle is Candidate 1.

Verdict: MITIGATES (mechanism works; unofficial, fragile, unsupported). Boundary:
reflection hack into java.lang, needs --add-opens, can break in any release. Not a
shipped/in-flight/credibly-proposed official path.

## Candidate 9 — `jdk.management.VirtualThreadSchedulerMXBean`

Primary: JDK 25 source (`jdk.management` module). Observability (pool size, queued
task count) only — no scheduling control.

Verdict: IRRELEVANT.

## JDK 25 yield path (verified in source, for the record)

`Thread.yield()` (Thread.java:458) → `VirtualThread.tryYield()` → `Continuation.yield`
→ unmount → `afterYield()`: on `YIELDING`, "external submit if there are no tasks in
the local task queue" (`pool.externalSubmit`, shared submission queue), else
`submitRunContinuation()` → `scheduler.execute(runContinuation)` from the carrier
thread → the carrier's *local* FJP queue. No priority, no fairness boost, no
time-slice: the yielded continuation rejoins the same pool as ordinary work. This
confirms the spike's root-cause claim at the source level (the "LIFO local queue"
phrasing is loose — the scheduler is asyncMode/FIFO per JEP 444 and the source — but
the load-bearing fact is verified: a yielding spinner's continuation is resubmitted
with no precedence over, and no isolation from, the spinner's own re-submissions, so
under saturation a woken vthread has no guaranteed path to a carrier).

## Gaps / not located

- The "Custom Scheduler Prototype v1" announcement thread on loom-dev was not located
  (only the v2 follow-up from Feb 2026); pipermail index pages returned 403 during this
  session, individual message pages worked.
- No JBS issue tracks the custom-scheduler prototype (loom-repo work; none found via
  search). If one exists, it would strengthen Candidate 1's status — worth one JBS
  lookup by the parent if desired.
- Lead (secondary, not evidence): Ron Pressler interview
  (https://www.infoworld.com/article/2334607/project-loom-understand-the-new-java-concurrency-model.html):
  "we'd like to support pluggable custom schedulers" — consistent with Candidate 1 being
  the active exploration, not with anything shipped.

## Bottom line

Nothing on the official track contradicts or qualifies the spike's root-cause claim.
Across JBS, all Loom JEPs (444/446/453/480/491), the JEP index (no scheduler JEP in
draft/submitted/in-flight), loom-dev, and the JDK 25 source: there is NO shipped or
committed mechanism for fair vthread scheduling under carrier saturation or for
replacing the scheduler on any mainline JDK. The single in-flight item is the
loom-repo-only Custom Scheduler Prototype v2 (Feb 2026), explicitly prototype-only
with no commitment to expose anything. Carlo's "there must be something" is answered:
the something exists, but only as a prototype in the loom repo — it is not in any JDK
you can download.
Re-test (concrete): watch for `jdk.virtualThreadScheduler.implClass` (or a JEP
integrating the prototype) in a mainline EA build; then re-run the exact spike torture
(2 Thread.yield spinners + 1 latency pair, `jdk.virtualThreadScheduler.parallelism=2`)
with a fair FIFO Executor as the custom scheduler. Expect 200,000/200,000 latency
samples at p50 <1µs iff the mechanism is solved. Until then, the KILL verdict stands.
