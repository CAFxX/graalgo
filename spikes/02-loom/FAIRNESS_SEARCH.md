# FAIRNESS_SEARCH.md — exhaustive hunt for fair virtual-thread scheduling under carrier saturation

**Date:** 2026-10-03 · **Question:** is there ANY known way — shipped, in-flight, or credibly proposed — to get fair (starvation-free) scheduling of Java virtual threads under carrier saturation, or to customize/replace the virtual-thread scheduler?
**Context:** spike 02-loom KILLED the 1:1-goroutine-on-vthreads architecture (0/200,000 latency samples vs stock Go under 2-carrier saturation; 7 workaround families failed). Carlo's challenge: "there must be something." This document is the exhaustive answer.
**Method:** 3 parallel research tracks (OpenJDK official / community+ecosystems / GraalVM+EA) + independent JDK 25 source verification. Primary sources only for mechanism claims (JDK source, JBS, JEP text, mail archives, loom repo docs, vendor docs). 40 raw candidates → deduplicated below.

## Bottom line (read this first)

**Nothing shippable today solves it.** The "something" exists, but only in two forms, neither usable by graalgo on any downloadable JDK:
1. **Unshipped loom-repo prototypes** — a custom-scheduler hook (`-Djdk.virtualThreadScheduler.implClass`) and a forced-preemption prototype, both experimental, explicitly "may change or be removed at any time", no JEP, no timeline, not in JDK 21–28.
2. **One uninstallable community scheduler** — Netty's FIFO-everywhere carrier scheduler, which fixes the mechanism but installs only via the loom-repo hook (not honored by any released JDK).

Everything else found is a mitigation with a hard boundary, irrelevant, or refuted. **The kill verdict stands.** Three concrete re-tests are named in §6; the loom-repo hook landing in a mainline EA build is the one to watch.

---

## 1. Unified catalog

Verdicts judge the *mechanism* (would it fix the 200k-vs-0 torture if deployed); the "Shippable?" column judges whether graalgo could use it today on a stock or GraalVM JDK.

### A. Shipped in mainline JDKs — no fix exists

| # | Candidate | Mechanism verdict | Shippable? | Notes |
|---|---|---|---|---|
| A1 | `afterYield()` external-submit on empty local queue (JDK 25 `VirtualThread.java:577-585`) | MITIGATES | Yes (already in the spike's JDK) | Partial mitigation already present during the 0-sample runs — empirically insufficient. Helps only when the yielding carrier's local queue is empty. |
| A2 | `jdk.virtualThreadScheduler.parallelism/maxPoolSize/minRunnable` tuning | MITIGATES | Yes | Sizing only. Works while #spinners < #carriers; defeasible by any program that spawns more spinners; JVM-global. "Reduced saturation", not fairness. |
| A3 | `Thread.yield()` / `LockSupport.parkNanos` / `Thread.sleep(0)` / `onSpinWait` choice | REFUTED | n/a | No published measurement shows any of these producing fairness; the spike tested yield and timed-park (both 0 samples). None changes queue discipline. |
| A4 | JEP 444 "Scheduling virtual threads" | IRRELEVANT | n/a | Normative doc of the mechanism; explicitly scopes time sharing OUT ("The scheduler does not currently implement time sharing…"). Names the spike's exact scenario (few platform threads, 100% CPU) as out of scope. |
| A5 | JEP 491 (no pinning) | IRRELEVANT | n/a | Different starvation mechanism (pinned carriers); spike's pinning audit found 0 events. |
| A6 | JEPs 446/453/480 (scoped values / structured concurrency) | IRRELEVANT | n/a | Zero scheduler content (full-text searched). |
| A7 | JDK-8292969 (FJP under-utilization) | IRRELEVANT | n/a | Fixed in 19.0.2; opposite failure mode (idle workers, not saturated starvation). |
| A8 | `VirtualThreadSchedulerMXBean` / JFR events | IRRELEVANT | n/a | Observe-only. |
| A9 | Structured concurrency (`StructuredTaskScope`) | IRRELEVANT | n/a | No scheduling controls; subtasks inherit the default carrier scheduler. |

### B. In-flight — the "something" (loom repo only, verified 2026-10-03)

| # | Candidate | Mechanism verdict | Shippable? | Notes |
|---|---|---|---|---|
| B1 | **Loom custom-scheduler prototype** (`-Djdk.virtualThreadScheduler.implClass=<fqcn>`, `Thread.VirtualThreadScheduler` with `onStart`/`onContinue`) | **SOLVES** | **No** | Verified live in `openjdk/loom` `fibers` branch docs (read 2026-10-03): "The purpose of the experimental support is to allow exploration and get feedback to help inform this project on whether to expose anything. The experimental support may change or be removed at any time." A fair FIFO executor here removes the FJP path entirely — the torture's starvation mechanism disappears. Absent from all mainline JDKs (verified: zero `implClass` matches in JDK 25 sources and in `openjdk/jdk` master; no JEP in draft/submitted/in-flight). Ron Pressler: "we'd like to support pluggable custom schedulers" (roadmap, no commitment). Sources: [CustomSchedulers.md](https://github.com/openjdk/loom/blob/HEAD/loom-docs/CustomSchedulers.md), [loom-dev Feb 2026](https://mail.openjdk.org/pipermail/loom-dev/2026-February/008323.html) (Alan Bateman: "system-wide and cannot be changed mid-flight… It can be re-visited if efforts in this area go further than the prototype"). |
| B2 | **Loom forced-preemption ("Time Sharing") prototype** (thread-local handshakes) | **SOLVES** | **No** | Same doc, §"Time Sharing": "Early prototype support for *forced preemption*… exists in a branch in the loom repo. The prototypes in the `fibers` branch for custom schedulers do not support forced preemption at this time." True preemptive timeslicing = the actual fix (Go's async preemption equivalent). No JEP, no timeline, not integrated with anything shippable. |
| B3 | Per-thread scheduler API (`Thread.ofVirtual().scheduler(...)`) | REFUTED (as a path) | No | **Removed** from the fibers branch ("removed… to allow for more testing with prototype 1", per the same doc). The surviving direction is system-wide-only (B1). |

Naming caveat: researchers found "v1/v2" used inconsistently across loom-dev threads; the doc above is authoritative as of 2026-10-03: prototype 1 = `implClass` (alive), prototype 2 = per-thread API (removed).

### C. Community prepackaged workarounds

| # | Candidate | Mechanism verdict | Shippable? | Notes |
|---|---|---|---|---|
| C1 | **franz1981/Netty-VirtualThread-Scheduler** (FIFO-everywhere carrier, Apache-2.0) | **SOLVES** (mechanism) | **No** | Replaces the carrier pool with N platform threads draining MPSC queues FIFO on both ends; a yielding spinner resubmits to the tail while the carrier pops the head — the burial the spike measured cannot occur. Installs via `-Djdk.virtualThreadScheduler.implClass=…`, which exists **only on loom-branch builds** ("A Loom-enabled JDK (Java 27+, builds.shipilev.net or build from openjdk/loom)" per README). Depending on it = depending on the unshipped hook. No torture-scenario measurement published (design rationale, not benchmark). Strongest independent validation of re-test trigger (b): queue discipline is the whole game. Source: [GitHub](https://github.com/franz1981/Netty-VirtualThread-Scheduler). |
| C2 | vaadin-loom `--add-opens` reflection hack (`ThreadBuilders$VirtualThreadBuilder(Executor)`) | IRRELEVANT alone; **enabler** for the one undemonstrated JDK-change-free path | No (unshippable by program constraints) | Documented working on JDK 21/25 across Oracle/Corretto/Temurin, self-described "JVM-implementation-sensitive". Installs a *per-builder* scheduler — enough for graalgo (it creates all vthreads) — but depends on test-only internals ("invoked by tests") the Loom team can refactor at any release. The spike already ruled this class out for a shipping toolchain. Source: [vaadin-loom](https://github.com/mvysny/vaadin-loom/blob/HEAD/CLAUDE.md). |
| C3 | Carrier oversubscription (`parallelism` >> spinners) | MITIGATES | Yes | Same boundary as A2: idle carriers steal the buried continuation, but any P-spinner program defeats it. No community fairness measurement found. |
| C4 | Quasar/Pulsar (pre-Loom M:N fibers) | REFUTED | No | Dead (last release Nov 2018, JDK 11 max); never built a Loom bridge; its own docs admit "runaway fibers" with detection-only, no fairness discipline; requires bytecode instrumentation incompatible with Truffle-AST execution. Existence proof that pluggable M:N schedulers are *buildable* (mildly supports the re-scope's feasibility in principle) but provides nothing shippable. No prepackaged M:N-on-vthreads library exists anywhere in the search. |
| C5 | pderop/looma (actor engine, Sept 2026) | IRRELEVANT as fix; **corroborates** | n/a | "Virtual threads are not time-sliced, so [cooperative yield] is the only lever that exists against a busy actor." Independent, recent confirmation of the root cause. |

### D. Adjacent ecosystems / app servers — no fairness layer anywhere

| # | Candidate | Verdict | Notes |
|---|---|---|---|
| D1 | kotlinx.coroutines Loom support | IRRELEVANT | No built-in Loom dispatcher (as of 2026); the community pattern (`newVirtualThreadPerTaskExecutor().asCoroutineDispatcher()`) delegates wholesale to the default carrier scheduler — inherits the pathology. |
| D2 | Tomcat (vendor-confirmed) | IRRELEVANT as fix; **corroborates** | Mark Thomas (tomcat-user, Sept 2023): "all virtual threads share the common ForkJoin scheduler at the JVM level… there is currently no built-in way to assign different scheduler priorities or separate carrier pools per application." Remediation = isolation (avoidance). |
| D3 | Jetty 12 `VirtualThreadPool` (200-thread cap) | IRRELEVANT | Admission rate-limiter, not a scheduling discipline. |
| D4 | Helidon Níma/SE, Spring Boot 3.2+, Netty Loom experiments | IRRELEVANT | All run on the default carrier pool; guidance is uniformly "don't put CPU-bound work on vthreads" (avoidance). No starvation incident with a scheduling fix found in any vendor tracker. |
| D5 | Pinning incident reports (production writeups) | IRRELEVANT | Different mechanism (carrier pinning), fixed by JEP 491; spike's pinning audit was clean. |

### E. GraalVM — changes nothing

| # | Candidate | Verdict | Notes |
|---|---|---|---|
| E1 | GraalVM JDK (JIT) | REFUTED as fix | Ships the standard class library; vthread machinery untouched. No scheduler flags beyond stock properties. |
| E2 | GraalVM Native Image (SubstrateVM) | REFUTED as fix | Compiles `java.base` from source **including `VirtualThread.java`** — same FIFO FJP scheduler, same pathology (25.0.4 notes: "adopted virtual-thread updates from JDK-8369227/8354894/8366893", i.e. it tracks the JDK file; JDK-8292969's reporter confirmed Native Image showed identical FJP behavior to HotSpot). |
| E3 | Truffle polyglot vthread support | IRRELEVANT | Experimental; no scheduler content. Footnote for graalgo: 65535-threads-per-context cap exists (scale ceiling, separate issue). |
| E4 | Espresso (Java on Truffle) bound-thread vthreads | IRRELEVANT | Guest-Java implementation detail, not the host vthreads graalgo would use; forfeits vthread scalability anyway. |

### F. Release train (26/27/28 EA) — no scheduler work

JDK 26: only JDK-8369238 (class-init unpinning) + JDWP tweaks — IRRELEVANT. JDK-8369227 (lost-unpark regression, fixed 25.0.3/26.0.1/27) — IRRELEVANT (correctness, not fairness). JDK-8354894/JDK-8366893 ("Starvation.java" test timeouts) — test-only, IRRELEVANT. JDK 27/28 JEP lists (523, 533, 534, 537, 401, 535, 539–544): zero vthread-scheduler items — REFUTED as a source of relief.

**Raw counts:** 40 candidates across 3 tracks (+ source verification) → deduplicated above. Survivors: **2 SOLVES-mechanism, both unshippable** (B1, B2 loom prototypes; C1 uninstallable community scheduler) + **3 MITIGATES with hard boundaries** (A1, A2/C3) + 1 unshippable enabler (C2). **0 shippable fixes.**

---

## 2. JDK 25 source verification (independent, local src.zip)

1. **Default scheduler is FIFO, not LIFO — correction to REPORT.md §3.** `VirtualThread.createDefaultScheduler()` (`VirtualThread.java:1429-1454`): `boolean asyncMode = true; // FIFO`; worker queues inherit the FIFO bit (`ForkJoinWorkerThread.java:78`). Shared *submission* queues are cfg=0 (LIFO owner-poll), but carriers take from them via the scan path at the base (FIFO end). The report's "LIFO local queues" phrasing is contradicted by the source (independently confirmed by 2 of the 3 research tracks).
2. **Yield path is conditional** (`VirtualThread.afterYield()`, lines 577-585): `Thread.yield()` → if carrier's local queue empty → `externalSubmit` to a probe-chosen shared queue (stealable); else → `submitRunContinuation()` → `execute()` → `poolSubmit()`.
3. **`poolSubmit` has carrier affinity** (`ForkJoinPool.java:2614-2627`): it keys on `JLA.currentCarrierThread()`, so submissions made while executing on a carrier — `afterYield` resubmits *and unparks issued by mounted vthreads* — land in **the carrier's own work queue**. (The report did not identify this path.)
4. **No public scheduler API — confirmed.** `ThreadBuilders.VirtualThreadBuilder(Executor)` is package-private, `// invoked by tests` (`ThreadBuilders.java:211-219`).
5. **Properties are sizing-only — confirmed.** `jdk.virtualThreadScheduler.parallelism` (default: availableProcessors), `.maxPoolSize` (default: max(parallelism,256)), `.minRunnable` (default: max(parallelism/2,1)). No fairness/discipline knob.
6. **Worker scan** (`ForkJoinPool.runWorker`): xorshift-randomized start per round, full queue coverage, takes at the base (FIFO end); `topLevelExec` drains the worker's own queue via `nextLocalTask(fifo)` after each task.

## 3. Residual puzzle (honest)

Naive analysis of §2 (randomized full-coverage scans, FIFO takes, unpark→carrier-own-queue→`nextLocalTask` pickup) predicts the O1 latency pair *should* make progress — yet 10+ runs show 0 samples, and the mechanism-independent `DirectUb` probe (pure `LockSupport.park`/`unpark` from a platform thread, no channels, no monitors) also showed burial. **The precise queue-level mechanism of the deterministic starvation is therefore not established by static source analysis**; the report's LIFO-burial story is contradicted on the FIFO point. What IS established and unaffected: (a) the empirical failure is systematic across modes and runs; (b) no public API can alter any of this; (c) 7 workaround families failed empirically, including the mechanism-independent probe. **The kill verdict rests on (a)-(c), not on the mechanism story.** Suggested follow-up (does not re-run the prototype): a minimal 4-vthread no-channel reproduction with JFR `jdk.VirtualThreadStart` analysis to determine whether woken continuations are never scheduled vs scheduled-but-buried.

## 4. Prepackaged M:N verdict (Carlo's sharpening)

**No prepackaged M:N-task-scheduler-on-vthreads library exists.** The closest historical item (Quasar) is dead and never solved fairness. The re-scope option (interpreter-level M:N scheduler) is therefore **neither validated nor killed** by this search — neutral. (Quasar is a mild existence proof that pluggable M:N user schedulers are buildable.)

## 5. Bottom line

Carlo's "there must be something" is answered: **the something exists, but only as unshipped loom-repo prototypes (B1, B2) and one uninstallable community scheduler (C1).** Nothing on JBS, in any JEP (444/446/453/480/491 + index: no scheduler JEP in any state), on loom-dev/core-libs-dev (maintainer position: custom schedulers are roadmap, preemption deliberately unexposed), in the JDK 26/27/28 release trains, in GraalVM (JIT or Native Image), or in any adjacent ecosystem (Kotlin, Tomcat, Jetty, Helidon, Spring, Netty, Quasar) provides fair vthread scheduling under carrier saturation on a downloadable JDK — or contradicts the spike's root-cause claim. Two independent community sources (looma, Tomcat's Mark Thomas) corroborate it. **The KILL verdict stands.**

## 6. Concrete re-tests (the only runs that could overturn this)

- **R1 (watch item):** if `-Djdk.virtualThreadScheduler.implClass` (B1) or equivalent lands in a mainline EA build, re-run the exact spike torture (2 yield-spinners + latency pair, `parallelism=2`) with a fair FIFO executor as the custom scheduler. Solved iff ~200,000/200,000 samples at p50 <1µs. (A secondary sanity re-run of the stock torture on JDK 27 GA is expected to still show ~0 samples.)
- **R2:** if forced preemption ("Time Sharing", B2) is ever exposed, re-run the torture with never-yielding spinners — the true fix, Go's async-preemption equivalent.
- **R3 (JDK-change-free, technical only):** vaadin-loom's `--add-opens` hack (C2) to install a FIFO MPSC carrier scheduler (C1's core) per-builder, then the torture. Expect it to fix POLL mode — satisfying kill trigger (c) *technically* — while remaining unshippable (test-only internals, breaks the drop-in constraint). Present that distinction explicitly if run.

---
*Track partials: [partial-openjdk.md](search/partial-openjdk.md) · [partial-community.md](search/partial-community.md) · [partial-graalvm-ea.md](search/partial-graalvm-ea.md) · [partial-source.md](search/partial-source.md)*
