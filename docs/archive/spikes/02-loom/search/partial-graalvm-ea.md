# Partial results — Track: GraalVM / Truffle + in-flight JDK work (vthread scheduler fairness)

Researcher: subagent 876209a7 (parent: graalgo spike-02 coordinator)
Date: 2026-10-03
Question: is there ANY known way — shipped, in-flight, or credibly proposed — to get
fair (starvation-free) scheduling of Java virtual threads under carrier saturation,
or to customize/replace the virtual-thread scheduler?

Method: primary sources only for mechanism claims (openjdk/jdk + openjdk/jdk25u raw
source, openjdk/loom repo docs, graalvm.org + Oracle GraalVM docs, Oracle JDK release
notes, JBS). browser_search/browser_open only; no shell scraping.

## A. In-flight / proposed (not shipped in any JDK)

### A1. Loom-repo custom-scheduler prototype 1 (`-Djdk.virtualThreadScheduler.implClass`)
- Description: the openjdk/loom repo (`fibers` branch) carries an experimental
  prototype letting the *entire default* vthread scheduler be replaced via
  `-Djdk.virtualThreadScheduler.implClass=<fqcn>`, where the class implements
  `java.lang.Thread.VirtualThreadScheduler` (`onStart`/`onContinue`). The custom
  scheduler "may use its own pool of platform threads to execute the tasks, may
  assign virtual threads to be carried by specific platform threads, or may
  delegate to the built-in virtual thread scheduler." Doc explicitly warns:
  "The experimental support may change or be removed at any time." Prototype 2
  (per-thread `Thread.ofVirtual().scheduler(...)` API) "has been removed from
  the fibers branch." Ron Pressler (Loom lead, quoted in InfoWorld) puts this on
  the roadmap as medium-term: "we want to offer custom schedulers… we'd like to
  support pluggable custom schedulers." No JEP exists for it (verified by search).
- Primary sources:
  - https://github.com/openjdk/loom/blob/HEAD/loom-docs/CustomSchedulers.md
    (doc updated ~15 days before this research — prototype is alive)
  - https://www.infoworld.com/article/2334607/project-loom-understand-the-new-java-concurrency-model.html
    (Pressler roadmap quote)
- Verdict: **SOLVES (if shipped)**. Replacing the default scheduler with a fair
  executor (e.g. a plain FIFO `Executor` over N platform threads, or a
  round-robin carrier pool) removes the FJP local-queue re-pop path entirely, so
  a yielding spinner can no longer starve a woken latency vthread. How exactly:
  `onContinue` is invoked "when a virtual thread is scheduled to continue after
  being parked or blocked" — a fair `onContinue` hands continuations to carriers
  in FIFO order, giving the 200k-vs-0 torture a starvation-free schedule.
  Caveat: NOT in any shipped JDK — verified zero matches for `implClass` in
  `VirtualThread.java` on both openjdk/jdk master and openjdk/jdk25u (see B1).

### A2. Loom-repo "Time Sharing" forced-preemption prototype
- Description: `CustomSchedulers.md` ("Time Sharing" section): "Early prototype
  support for *forced preemption*, based on thread local handshakes, exists in a
  branch in the loom repo. The prototypes in the `fibers` branch for custom
  schedulers do not support forced preemption at this time." I.e. true
  preemptive timeslicing of CPU-bound vthreads is prototyped but not integrated
  with anything shippable.
- Primary source: https://github.com/openjdk/loom/blob/HEAD/loom-docs/CustomSchedulers.md
  (§"Time Sharing")
- Verdict: **SOLVES (if shipped)**. Forced preemption of spinners directly
  defeats the torture (carriers get timesliced regardless of yield behavior).
  Not in mainline; no JEP; no timeline.

## B. Shipped mainline behavior (openjdk/jdk master + jdk25u, read 2026-10-03)

### B1. Default scheduler is still a FIFO ForkJoinPool; NO custom-scheduler hook (master + 25u)
- Description: `VirtualThread.java` (both trees):
  - `private static final ForkJoinPool DEFAULT_SCHEDULER = createDefaultScheduler();`
  - `createDefaultScheduler()`: `boolean asyncMode = true; // FIFO`,
    `new ForkJoinPool(parallelism, factory, handler, asyncMode, 0, maxPoolSize,
    minRunnable, pool -> true, 30, SECONDS)` with `parallelism` from
    `jdk.virtualThreadScheduler.parallelism` (default = availableProcessors),
    `maxPoolSize` (default max(parallelism,256)), `minRunnable`.
  - `browser.find "implClass"` on the master file: **zero matches** — the loom
    prototype hook is absent from mainline.
  - Per-vthread `Executor scheduler` field exists and `submit()` handles
    non-FJP executors (incl. a `DelayedTaskSchedulers` helper "used when using a
    custom scheduler"), but no public API can install one: `Thread.ofVirtual()`
    exposes no scheduler setter, and `ThreadBuilders.VirtualThreadBuilder(Executor)`
    is package-private ("invoked by tests" per spike-02 findings). Scheduler
    inheritance only (child vthread inherits parent's).
- Primary sources:
  - https://raw.githubusercontent.com/openjdk/jdk/master/src/java.base/share/classes/java/lang/VirtualThread.java
    (L67, L1790–1842)
  - https://raw.githubusercontent.com/openjdk/jdk25u/master/src/java.base/share/classes/java/lang/VirtualThread.java
    (L67; identical)
  - https://openjdk.org/jeps/444 (§"Scheduling virtual threads": FJP in FIFO
    mode; `jdk.virtualThreadScheduler.parallelism`)
- Verdict: **REFUTED** (as a source of relief). Confirms the spike's root-cause
  claim rather than contradicting it. Qualification: the pool is configured
  `asyncMode=true` (FIFO), so the spike's "LIFO local queues" phrasing is loose —
  but the operative mechanism (yielded continuation re-queued where the same
  carrier immediately re-takes it) is confirmed by `submitRunContinuation`'s own
  comment: "For the default scheduler, and calling it on a worker thread, the
  task will be pushed to the local queue."

### B2. `afterYield()` Thread.yield path — identical in 25u and master
- Description: on `Thread.yield` (state YIELDING→YIELDED):
  ```java
  // external submit if there are no tasks in the local task queue
  if (currentThread() instanceof CarrierThread ct && ct.getQueuedTaskCount() == 0) {
      externalSubmitRunContinuation(ct.getPool());
  } else {
      submitRunContinuation();   // -> local queue when called on a worker
  }
  ```
  So mainline already contains a *partial* mitigation: a spinner yielding on an
  otherwise-idle carrier goes to the pool's external submission queue, not its
  own local queue. Crucially, this code is **identical in jdk25u** (L809–817) —
  the spike's 0/200k torture result was obtained *with* this mitigation in
  place, so it does not save the torture. When the carrier's local queue is
  non-empty (the contested case — e.g. the latency pair's continuation was just
  locally submitted there by an unpark), the yield still goes local and the
  carrier re-pops the spinner.
- Primary sources: master L767–821; jdk25u L809–817 (same URLs as B1).
- Verdict: **MITIGATES** (narrower). Boundary: helps only when the yielding
  carrier's local queue is empty; does not provide fairness under saturation.
  Empirically insufficient per spike-02 (0/200k on JDK 25 which has this code).

### B3. Carrier over-provisioning knobs (`parallelism` / `maxPoolSize` / `minRunnable`)
- Description: the three `jdk.virtualThreadScheduler.*` properties tune carrier
  count (verified in `createDefaultScheduler`, B1). Setting
  `parallelism` > #spinners leaves idle carriers that steal the latency pair's
  continuations. `maxPoolSize` caps pinning-compensation growth (default 256).
- Verdict: **MITIGATES**. Boundary: requires over-provisioning carriers beyond
  the spinner count; fails again as soon as #spinners >= #carriers; JVM-global
  (no per-workload policy); does not make scheduling fair.

### B4. JFR vthread events — observe-only, no new scheduler knobs
- Description: `jdk.VirtualThreadStart`/`End` (emitted in `run()`),
  `jdk.VirtualThreadSubmitFailed` (on `RejectedExecutionException`),
  `jdk.VirtualThreadPinned` (via `postPinnedEvent`). All observe-only. JDK 26
  consolidated release notes contain no new scheduler/fairness JFR knob; JDK 27
  JEP list and JDK 28 EA JEP list (401, 535, 539, 540, 541, 542, 543, 544) contain
  no vthread-scheduler item. `jdk.management.VirtualThreadSchedulerMXBean` is
  referenced by the loom prototype docs as an MBean a *custom* scheduler may
  implement — meaningless without A1.
- Primary sources: master VirtualThread.java imports + L609–617, L1173–1191;
  https://www.oracle.com/java/technologies/javase/26all-relnotes.html (searched
  "Virtual Thread": only JDK-8369238 + JDWP tweaks)
- Verdict: **REFUTED** (no new observability/control knob exists that would
  change scheduling).

## C. JDK 26 / 27 / 28 EA — release-train check

### C1. JDK 26: JDK-8369238 "Allow virtual thread preemption on some common class initialization paths"
- Description: a vthread waiting on another thread's class initializer now
  usually *unmounts* instead of staying pinned. Pinning-path fix only.
- Primary source: https://www.oracle.com/java/technologies/javase/26all-relnotes.html
  (only vthread behavior change in the 26 notes besides JDWP tweaks)
- Verdict: **IRRELEVANT**. The torture performs no class initialization;
  orthogonal to yield/queue fairness.

### C2. JDK 25.0.3 / 26.0.1 / 27: JDK-8369227 (lost-unpark regression)
- Description: "on JDK 25 an untimed park on a virtual thread can lose an unpark
  outright" — regression from JDK-8351927 (FJP delayed-task timers), fixed in
  25.0.3, 26.0.1, 27. Correctness bug, not fairness.
- Primary sources: JBS via secondary writeups; GraalVM 25.0.4 notes list it as
  adopted (see D2)
- Verdict: **IRRELEVANT**.

### C3. JDK-8354894 (`Starvation.java` test timeout) and JDK-8366893 (stress-test iteration cut)
- Description: both are *test-only* fixes (jtreg timeout/iteration tuning),
  backported in 25.0.3 PSU. Despite the suggestive name, JDK-8354894 changed no
  scheduling behavior.
- Primary sources: https://docs.bell-sw.com/liberica-jdk/latest/general/release-notes/
  (lists JDK-8354894 "java/lang/Thread/virtual/Starvation.java timeout on server
  with high CPUs"); https://github.com/battleblow/jdk/commit/9d3edc0af7be9fb16de3565642e487822d62e484
  (JDK-8366893 reduces stress-test iterations)
- Verdict: **IRRELEVANT** (both).

### C4. JDK-8292969 "Bad Thread Utilization in ForkJoinPool" (fixed in 19.0.2)
- Description: 8000 rapidly-yielding vthreads showed FJP worker under-utilization
  dips; fixed by JDK-8294762. Throughput/utilization bug, long fixed. Notable as
  evidence that GraalVM Native Image exhibited the *same* FJP behavior as
  HotSpot (reporter tested both) — i.e. SubstrateVM shares the JDK scheduler.
- Primary source: https://bugs.openjdk.org/browse/JDK-8292969 (Resolved/Fixed)
- Verdict: **IRRELEVANT** to fairness/starvation.

### C5. JDK 27 (GA Sep 2026) / JDK 28 (EA): no scheduler work in the release train
- Description: JDK 27 JEPs (523 G1-default, 533 structured concurrency 7th
  preview, 534 compact headers, 537 Vector API 12th incubator, …) and JDK 28 EA
  JEPs (401, 535, 539, 540, 541, 542, 543, 544) contain no virtual-thread
  scheduler/fairness item. No JEP proposes custom vthread schedulers (search
  "JEP custom scheduler virtual threads" returns only the loom repo docs).
- Primary sources: https://www.jvm-weekly.com/p/jdk-27-is-here-jvm-weekly-vol-192
  (secondary JEP list, caveat noted); JEP search negative result
- Verdict: **REFUTED** — nothing scheduler-related is even in the release train.

## D. GraalVM

### D1. GraalVM on HotSpot (JIT): no vthread scheduler changes
- Description: Oracle GraalVM ships the standard OpenJDK class library plus the
  Graal JIT; virtual-thread machinery (`java.lang.VirtualThread`,
  continuations, the FJP carrier pool) is untouched. GraalVM release notes
  mention vthreads only for bug-fix adoption (D2) and Truffle polyglot support
  (D3). No custom-scheduler support, no scheduler flags beyond the stock
  `jdk.virtualThreadScheduler.*` properties.
- Primary source: https://docs.oracle.com/en/graalvm/jdk/25/docs/release-notes/
  (25.0.4 notes: vthread mentions only under Native Image + Truffle sections)
- Verdict: **REFUTED** — GraalVM JIT changes nothing about the root cause.

### D2. GraalVM Native Image (SubstrateVM): same JDK scheduler, same root cause
- Description: SubstrateVM compiles `java.base` from source, including
  `VirtualThread.java`; Oracle GraalVM 25.0.4 notes: Native Image "Adopted
  virtual-thread updates from JDK-8369227, JDK-8354894, and JDK-8366893" —
  i.e. it tracks the JDK's `VirtualThread.java`, whose scheduler is the same
  FIFO FJP. The carrier pool and its `jdk.virtualThreadScheduler.*` tunables
  work in native images (e.g. community native-image builds pass
  `-Djdk.virtualThreadScheduler.parallelism=16` at build time). JDK-8292969's
  reporter confirmed Native Image showed the same FJP behavior as HotSpot.
- Primary sources:
  - https://docs.oracle.com/en/graalvm/jdk/25/docs/release-notes/
  - https://bugs.openjdk.org/browse/JDK-8292969 (Native Image parity quote)
  - https://github.com/christianhaeubl/game-of-life-csp (secondary: parallelism
    property used with native-image; caveat: community sample, not Oracle docs)
- Verdict: **REFUTED** as a fix — Native Image inherits the identical scheduler
  and the identical starvation pathology. (Absence claim: no GraalVM doc or
  substratevm source describes a distinct native-image vthread scheduler; the
  adopted-JDK-fix notes are positive evidence it tracks the JDK file.)

### D3. Truffle polyglot virtual-thread support (experimental)
- Description: GraalVM for JDK 23 release notes: "Added experimental support
  for virtual threads on HotSpot. Not all languages are currently supported…
  Truffle debugging, CPU time limits, and some memory limits are currently not
  supported on virtual threads. The number of threads is currently limited to
  65535 threads entered per context." Nothing about scheduler customization or
  fairness.
- Primary source: https://www.graalvm.org/release-notes/JDK_23/
- Verdict: **IRRELEVANT** to scheduler fairness. Footnote for graalgo: the
  65535-threads-per-context cap is a *separate* scale ceiling to be aware of if
  goroutines ever enter Truffle contexts as threads (they wouldn't as plain
  host vthreads, but the cap exists).

### D4. Espresso (Java on Truffle): vthreads as bound threads
- Description: Espresso CHANGELOG 23.1.0: "Note that virtual threads are
  implemented as bound thread, not through continuations." 1:1 bound platform
  threads have no carrier pool, hence no carrier-saturation pathology — but this
  is Espresso's guest-Java implementation, not the host-JDK vthreads graalgo
  would use, and it forfeits the memory scalability that motivates vthreads.
- Primary source: https://github.com/graalvm/graalvm-community-jdk25u/blob/HEAD/espresso/CHANGELOG.md
- Verdict: **IRRELEVANT** to graalgo's host-vthread path.

## Bottom line

Nothing on this track contradicts or qualifies the spike's root-cause claim —
mainline master and jdk25u both confirm: default scheduler is a FIFO-mode
ForkJoinPool with zero custom-scheduler hook (`implClass`: 0 matches), and the
`afterYield` yield path is byte-identical between 25u (the spike's JDK) and
master, so the 0/200k result already accounts for the in-tree partial
mitigation. GraalVM (JIT and Native Image) changes nothing; no JDK 26/27/28
release item touches scheduler fairness; no JEP proposes custom schedulers.

Survivors: 4 of 16 candidates — 2 SOLVES-if-shipped (loom custom-scheduler
prototype A1, loom forced-preemption prototype A2; both unshipped, experimental,
no JEP/timeline) and 2 MITIGATES (carrier over-provisioning B3, afterYield
external-submit B2). Carlo's "there must be something" is answered: there is —
but only as unshipped loom-repo prototypes, not as anything graalgo can use
today on a stock or GraalVM JDK.

Concrete re-test (only run that could overturn this): build openjdk/loom
`fibers` branch, install a fair scheduler via
`-Djdk.virtualThreadScheduler.implClass` (a plain FIFO `Executor` over N
platform threads implementing `onStart`/`onContinue`), re-run the spike-02
torture (2 spinners + latency pair, 2 carriers) expecting ~200,000/200,000
latency samples. A secondary sanity re-run of the stock torture on JDK 27 GA
is expected to still show ~0 samples, confirming no silent fix.
