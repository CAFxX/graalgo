# Community workarounds + adjacent ecosystems — catalog

**Question:** is there ANY known way — shipped, in-flight, or credibly proposed — to get
fair (starvation-free) scheduling of Java virtual threads under carrier saturation, on
current or upcoming JDKs?

**Kill-trigger relevance rule:** "reduced saturation" (fewer spinners, more carriers,
blocking instead of spinning) is NOT a fairness fix. Only a mechanism that keeps N
spinners from starving others on N carriers counts.

**Verdict taxonomy:** SOLVES (would fix the 200k-vs-0 torture; how, exactly) /
MITIGATES (narrower — boundary stated) / IRRELEVANT (doesn't touch the mechanism) /
REFUTED (claimed or conceivable, shown not to work or not to exist).

**Ground truth this catalog is checked against** (spike 02-loom, verified in JDK 25
sources + measured): Loom's carrier FJP has LIFO local queues and no woken-thread
priority; `Thread.yield()` resubmits the continuation to the carrier's own LIFO queue so
the carrier immediately re-pops the spinner; a woken vthread lands under the spinner's
ever-repushed continuation; with #spinners >= #carriers no carrier ever idles to steal.
Yield and timed-park both measured 0/200k samples. Platform-thread unblocker also 0.

---

## A. Prepackaged schedulers / fairness layers

### A1. franz1981/Netty-VirtualThread-Scheduler — custom FIFO-everywhere carrier scheduler

A locality-first virtual-thread scheduler by Francesco Nigro (Netty contributor, 85
stars, Apache-2.0, active 2025–2026). Replaces the carrier pool with N permanent
platform threads, each draining its own MPSC run queue **FIFO everywhere** (local drain
and steal both from the head — "single-ended, no deque, no LIFO option"). Rationale
stated in the README: "FJP with Loom (asyncMode): FIFO… Fairness: virtual threads are
I/O-bound continuations, not recursive tasks. FIFO prevents starvation of VTs waiting
to resume after I/O." Optionally replaces the *built-in* scheduler entirely
(`io.netty.loom.replaceBuiltinScheduler=true`), routing every vthread through it.

Why it matters for the torture: on a FIFO MPSC carrier, a yielding spinner's
continuation resubmits to the **tail** while the carrier pops from the **head** — the
woken latency-pair continuation, enqueued earlier, surfaces. The LIFO burial the spike
measured cannot happen. Mechanism analysis says this **would fix the 200k-vs-0 torture
for yielding spinners** (the spike's POLL production shape). Boundary: no cooperative
scheduler fixes *never-yielding* spinners — Go covers that case with true preemption,
which no vthread scheduler has.

Why graalgo can't use it: installation requires
`-Djdk.virtualThreadScheduler.implClass=io.netty.loom.scheduler.NettyScheduler`, which
exists **only on loom-branch builds** ("A Loom-enabled JDK (Java 27+,
builds.shipilev.net or build from openjdk/loom)" per the README prerequisites). It is
not honored by any released JDK (see A2). Depending on it = depending on exactly the
backdoor the spike ruled out for a shipping toolchain. No published measurement of the
torture scenario exists — the FIFO-fixes-starvation claim is the author's design
rationale, not a benchmark.

- Links: https://github.com/franz1981/Netty-VirtualThread-Scheduler (README: queue-order/FIFO table, "Replace built-in scheduler (experimental)")
- Verdict: **SOLVES** (mechanism — FIFO resubmission discipline fixes the LIFO burial
  for yielding spinners), **but uninstallable on shipping JDKs**. Strongest single
  validation of the spike's re-test trigger (b): queue discipline is the whole game.

### A2. openjdk/loom experimental "Custom Schedulers" + loom-dev "Custom Scheduler Prototype v2"

The loom repo documents the experimental hook: `-Djdk.virtualThreadScheduler.implClass=$EXECUTOR`
(a public class implementing `java.util.concurrent.Executor`, receiving
`Thread.VirtualThreadTask`s) and `Thread.Builder.OfVirtual.scheduler(Executor)`. Stated
purpose: "allow exploration and provide feedback to help inform the project on whether
to expose anything." As of Feb 2026 it is **"Custom Scheduler Prototype v2"** on
loom-dev (Alan Bateman): system-wide only, cannot be changed mid-flight, class must be
on the class/module path. It has not shipped in any released JDK (21/25/26/27): the
spike verified JDK 25 sources expose only `parallelism`/`maxPoolSize`, with no public
per-builder scheduler API; downstream project looma (Sept 2026) gates its Netty-scheduler
integration on "a JDK before 28" not existing yet and reports `LinkageError` on stock
JDK 25 without `--enable-preview`.

- Links: https://github.com/openjdk/loom/commit/99eef9e610df4fa30ce6a19e8aa6422a8b3c923c ;
  https://mail.openjdk.org/pipermail/loom-dev/2026-February/008323.html ;
  https://github.com/pderop/looma/blob/HEAD/docs/jdk.md
- Verdict: **REFUTED as a current workaround** (in-flight prototype, not a shipping API).
  This is re-test trigger (a), not a solution.

### A3. pderop/looma — actor engine on Loom, A/B tests JDK scheduler vs Netty carrier-affine scheduler

Recent (Sept 2026), serious project: one actor = one virtual thread for life, same
engine measured against the JDK builtin scheduler and the Netty custom scheduler via
ServiceLoader. Two findings that directly corroborate the spike: (1) "Cooperative
`Thread.yield()` every 64 drained envelopes keeps a CPU-bound mailbox from holding a
carrier indefinitely; **virtual threads are not time-sliced, so this is the only lever
that exists against a busy actor**." (2) Their JDK-vs-carrier A/B is described as
"**locality only**" — even the team running the Netty scheduler in production does not
claim it as a fairness fix.

- Link: https://github.com/pderop/looma/blob/HEAD/docs/jdk.md
- Verdict: **IRRELEVANT** as a fix (no fairness mechanism; cooperative yield = the
  spike's POLL, already measured at 0 samples). **Corroborates** the root-cause claim
  from an independent, recent source.

### A4. Quasar / Pulsar (Parallel Universe) — pre-Loom M:N fibers

The pre-Loom M:N fiber library (Ron Pressler, now Loom lead at Oracle). Did have a
pluggable scheduler seam: default `FiberForkJoinScheduler` (FJP work-stealing) and,
since 0.4.0, `FiberExecutorScheduler` ("run fibers on any thread, not just in a
ForkJoinPool"). But: last release 0.8.0 was **Nov 2018** (JDK 11 max) — the project is
dead, no Loom bridge was ever built. Its own docs admit the fairness problem without
solving it: a "runaway fiber" (CPU loop without blocking) "may severely impact system
performance (as most of the scheduler's threads might be tied up)" — detection and JMX
reporting only, no fairness discipline. Fibers additionally require bytecode
instrumentation (`-javaagent`), which is incompatible with graalgo's Truffle-AST
execution model. No reusable scheduler code for a shipping toolchain.

- Links: https://docs.paralleluniverse.co/quasar/ ("The Scheduler and Monitoring",
  "Runaway Fibers"); https://github.com/puniverse/quasar
- Verdict: **REFUTED** as a prepackaged option (dead, instrumentation-based, never
  solved fairness). Conceptually it validates that pluggable M:N schedulers are
  buildable — which supports the *re-scope* option's feasibility in principle — but it
  provides nothing shippable.

### A5. mvysny/vaadin-loom — the `--add-opens` reflection hack, documented in the wild

`LoomUtils.newVirtualBuilder(Executor)`: "reflection hack that returns a
`Thread.Builder.OfVirtual` whose scheduler is **our** executor. **This is the only way
to redirect continuations away from the default ForkJoinPool**." Requires
`--add-opens java.base/java.lang=ALL-UNNAMED`, reflecting into
`java.lang.ThreadBuilders$VirtualThreadBuilder` (the package-private test-only
constructor the spike found; references JDK-8308541) and reading the private
`VirtualThread.runContinuation`. The author is explicit about fragility: CI
matrix-tests JDK 21/25 × Oracle/Corretto/Temurin because "the reflection hack in
`LoomUtils` is JVM-implementation-sensitive." (Project is a frozen prototype; the
successor is `vaadin-blocking-dialogs`.)

Assessment for graalgo: this is the *enabler* half of the only JDK-change-free path
found (hack → install a fair FIFO scheduler like A1's core on a stock JDK; per-builder
is enough since graalgo creates all its vthreads, and child vthreads inherit the
creator's scheduler). Nobody has demonstrated that combination end-to-end, and the
hack depends on test-only internals the Loom team can change at any release — exactly
what was ruled out for shipping. As a standalone mechanism it changes no queue
discipline (vaadin-loom installs a UI-thread executor, not a fair scheduler).

- Link: https://github.com/mvysny/vaadin-loom/blob/HEAD/CLAUDE.md ("Required JVM args", "Architecture")
- Verdict: **IRRELEVANT** as a standalone fix; **the documented enabler** for the one
  undemonstrated JDK-change-free workaround — see the concrete re-test in the bottom
  line.

---

## B. Yield / park / sleep / onSpinWait primitive comparisons

Searched for measurements comparing `Thread.yield()`, `LockSupport.parkNanos(1)`,
`Thread.sleep(0)`, `Thread.onSpinWait()` on virtual threads under saturation.
**No published measurements found.** What exists is generic advice (e.g. "prefer
`Thread.sleep(1)` over `yield`" — for *platform* threads, circa 2012) and Loom
explainer content, none of it a fairness experiment. The spike itself tested the two
serious candidates: back-edge `Thread.yield()` (POLL/UNCOND) and timed-park
(PARK 1µs–1ms) — **both 0/200k samples**. Mechanism reasoning: none of these primitives
changes the carrier's resubmission queue discipline, which is where the burial happens;
`onSpinWait()` doesn't even yield the carrier (pure CPU hint).

- Verdict: **REFUTED** as fairness fixes. (No community measurement contradicts the
  spike's 0-sample results.)

## C. Oversubscription (`jdk.virtualThreadScheduler.parallelism` >> cores)

**No community demonstration found** that oversubscription restores fairness, or any
measurement of fairness-vs-parallelism at all. The property is discussed only as a
throughput/carrier-count knob (Heinz Kabutz's Loom talks document it alongside
`maxPoolSize`; the spike's O5 measured throughput plateauing under oversubscription —
no fairness claim). Mechanism analysis: with P carriers > S spinners, idle carriers can
steal the buried continuation, so oversubscription *reduces* the starvation window —
but it is defeasible by any program that spawns P spinners, wastes cores, and changes
nothing about the LIFO discipline. It is "reduced saturation" wearing a flag.

- Verdict: **MITIGATES** at best — boundary: works only while spinner count < carrier
  count; an adversarial workload trivially defeats it; never a fairness guarantee.

## D. Adjacent ecosystems

### D1. kotlinx.coroutines Loom support

There is **no built-in Loom dispatcher** in kotlinx.coroutines (as of 2026). The
documented community pattern is user-defined:
`Executors.newVirtualThreadPerTaskExecutor().asCoroutineDispatcher()` (e.g.
spring-projects/spring-framework#33788 discusses exactly this as the "LOOM dispatcher
if available and configured"). Coroutine dispatchers therefore inherit the default
carrier scheduler wholesale — same LIFO burial, no fairness layer. JetBrains has only
"explored" Loom-enabled dispatchers (blog speculation, nothing shipped).

- Links: https://github.com/spring-projects/spring-framework/issues/33788 ;
  https://dev.to/jofisaes/kotlin-coroutines-vs-java-virtual-threads-a-good-story-but-just-that-48bf
- Verdict: **IRRELEVANT** — delegates scheduling to Loom's default scheduler; inherits
  the pathology.

### D2. Tomcat — cross-application starvation, confirmed by the vendor, no fix

Tomcat committer Mark Thomas (tomcat-user, Sept 2023), quoted in a community writeup:
with virtual threads "**all virtual threads share the common ForkJoin scheduler** at
the JVM level… a flood of requests in one app can potentially starve threads for
another… **there is currently no built-in way to assign different scheduler priorities
or separate carrier pools** per application." Remediation discussed = isolate
applications (separate executors/JVMs) — i.e. avoidance.

- Links: https://github.com/kopn9k/virtual_threads ("Watch out for starvation across
  multiple web apps"); https://marc.info/?l=tomcat-user&m=169394364509789
- Verdict: **IRRELEVANT** as a fix (avoidance, not a mechanism). **Corroborates** the
  root cause from a vendor source.

### D3. Jetty 12 — `VirtualThreadPool` 200-thread default

Jetty 12's virtual-thread pool caps at 200 concurrent vthreads by default — an
*admission* rate-limiter ("to avoid overwhelming normal production systems"), not a
scheduling-fairness discipline. No starvation-under-saturation incident or fix found in
Jetty's tracker.

- Link: https://medium.com/@yvindkvien/if-you-read-the-docs-the-jetty-virtualthreadpool-has-a-default-limit-of-200-max-concurrent-threads-b940282d76a3
- Verdict: **IRRELEVANT** — admission control ≠ scheduling fairness.

### D4. Helidon Níma/SE, Spring Boot 3.2+, Netty — no fairness layer anywhere

Helidon, Spring Boot (`spring.threads.virtual.enabled=true`), and Netty's Loom
experiments all run request handling on virtual threads scheduled by the **default**
carrier pool. No framework documents a fairness discipline, priority scheme, or
saturation workaround beyond "don't put CPU-bound work on vthreads" (the standard
guidance, e.g. "Virtual threads optimize for blocking I/O. CPU-bound tasks gain
nothing"). No starvation-under-saturation incident with a scheduling fix found in any
vendor tracker.

- Verdict: **IRRELEVANT** across the board — the entire ecosystem delegates to the
  default scheduler.

### D5. Pinning incident reports (the other starvation)

Several production writeups (e.g. Sandeep94pr, Nov 2025) describe carrier *pinning*
(`synchronized`/native blocking pinning the carrier) stalling throughput. This is a
**different mechanism** from the kill trigger (LIFO burial under saturation, no
pinning involved) and is fixed by JEP 491 (JDK 24+); the spike's own pinning audit
found 0 `jdk.VirtualThreadPinned` events. Fixes discussed (offload blocking code to a
platform pool, replace `synchronized` with `ReentrantLock`) address pinning, not queue
discipline.

- Link: https://medium.com/@sandeep94pr/fixing-virtual-thread-pinning-in-java-lessons-from-real-world-production-issues-8d3227a26b02
- Verdict: **IRRELEVANT** to the kill trigger (different mechanism, already fixed).

## E. Structured concurrency / `StructuredTaskScope`

`StructuredTaskScope` (JEP 505, JDK 25) changes task *structure* (fork/join/shutdown
semantics), not *scheduling*: subtasks run on threads from the configured
`ThreadFactory`, and with a virtual-thread factory those are vthreads on the default
carrier scheduler. There are no priority, fairness, or scheduler-selection controls in
the API.

- Verdict: **IRRELEVANT** — as expected, it does not touch scheduling.

## F. Reflection / `--add-opens` hacks — shippability assessment

Documented in the wild only by vaadin-loom (A5): `--add-opens
java.base/java.lang=ALL-UNNAMED` + reflection into the package-private
`ThreadBuilders$VirtualThreadBuilder(Executor)` ("invoked by tests"). Demonstrated
working on JDK 21/25 across Oracle/Corretto/Temurin — but self-described as
"JVM-implementation-sensitive," and the constructor it depends on is explicitly
test-only scaffolding for the in-flight custom-scheduler prototype (A2), whose eventual
public shape is system-wide via property, not per-builder. A shipping toolchain that
tracks the latest JDK would be one Loom-team refactor away from a broken launcher, on
every JDK release, forever — the exact dependency the spike ruled out.

- Verdict: **REFUTED** for a shipping toolchain (works today, unshippable by the
  program's own constraints).

## G. Loom team official positions

### G1. Forced preemption exists internally, deliberately unexposed

loom-dev (via HN): "Loom's virtual threads can also be **forcibly preempted by the
scheduler at any safepoint to implement time sharing. Currently, this capability isn't
exposed** because we're yet to find a use-case for it (other than one special case…).
If you believe you have one, please send it to the loom-dev mailing list."

- Link: https://news.ycombinator.com/item?id=27885091 (loom-dev comment thread)
- Verdict: **IRRELEVANT** as a workaround — confirms the gap is intentional, not an
  oversight. (If Loom ever exposes it, that's a new re-test trigger; it would be the
  true fix, equivalent to Go's async preemption.)

### G2. "Custom Scheduler Prototype v2" (loom-dev, Feb 2026)

Covered in A2 — in-flight, system-wide-only, prototype. Not a workaround; re-test
trigger (a).

---

## Bottom line

1. **16 candidates cataloged; 2 survive triage** — A1 (Netty FIFO scheduler:
   SOLVES-the-mechanism, but uninstallable on shipping JDKs) and C (oversubscription:
   MITIGATES within a hard, defeasible boundary). **Nothing shippable today.**
2. **Nothing contradicts or qualifies the spike's root-cause claim** — every independent
   data point corroborates it, including looma (Sept 2026: "virtual threads are not
   time-sliced… the only lever that exists") and the Loom team itself (preemption
   deliberately unexposed; custom schedulers still prototype-only).
3. The closest hit **validates rather than threatens** the spike's analysis: the Netty
   scheduler's FIFO-everywhere discipline is exactly re-test trigger (b) ("JDK changes
   carrier queue discipline for YIELDED continuations") — queue discipline is the whole
   game, and the community's best scheduler agrees.
4. **Concrete re-test for trigger (c)** — the one JDK-change-free path nobody has
   demonstrated: vaadin-loom's `--add-opens` hack (documented working on JDK 21/25
   across three vendors) to install a FIFO MPSC carrier scheduler (A1's core, minus the
   implClass hook) per-builder, then run the spike's torture. Expect it to fix POLL
   mode; expect it to remain unshippable (test-only internals). If it fixes the torture,
   trigger (c) is satisfied *technically* while the shipping constraint still rules it
   out — that distinction should be presented to Carlo explicitly.
5. No prepackaged M:N-on-vthreads library exists that would validate-or-kill the
   re-scope option directly. Quasar is the historical existence proof that pluggable
   M:N user schedulers are buildable, but it is dead and never solved fairness —
   neutral for the re-scope decision, not evidence either way.
