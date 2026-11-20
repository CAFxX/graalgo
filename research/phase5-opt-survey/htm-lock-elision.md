# HTM lock elision for graalgo — research note and experimental design

**Status:** exploration, 2026-11-20 (lane docs-tm). Owner request: "explore whether,
on microarchitectures that efficiently support transactional memory, we can
automatically elide locks everywhere (or in most places) and replace them with
transactional memory execution." Backlog item: [PHASES.md](../../docs/arch/PHASES.md)
§7 ("Hardware transactional lock elision"); SPEC §2.6 `[PROPOSAL]` paragraph; work
package WP-TMELIDE in [RECOVERY.md](../../docs/arch/RECOVERY.md). Earlier survey
context: [SURVEY.md](SURVEY.md) ATTEMPTED item 2 (TSX, with the owner note of
2026-10-06 that TSX-enabled targets are fair game) and
[T6-optimization-inventory.md](../../docs/arch/research/T6-optimization-inventory.md) §4
(escape-analysis lock elision, a different technique: removing a lock on a
non-escaping object, expected ~0).

**Evidence labels.** `[VERIFIED]` = checked against the cited primary source (or
reproduced here) on 2026-11-20; `[HIST]` = well-established history from the cited
source, not re-checked line by line; `[UNVERIFIED]` = plausible, secondary or
from memory — must be checked before anything depends on it. Performance numbers
are the cited source's claims, never our measurements.

## 1. Verdict

Plausible only as a narrow, adaptive, opt-in optimization on one hardware family;
**not** plausible as "elide locks everywhere".

1. The only HTM that ships enabled on mainstream hardware today is Intel RTM on
   Xeon server parts that are not TAA-affected (Ice Lake-SP, Sapphire/Emerald/Granite
   Rapids); HLE is gone, AMD never shipped HTM, Arm withdrew TME without silicon,
   POWER10 dropped TM, IBM Z is sunsetting it from z17 (§2).
2. The JVM side has no supported way to emit `XBEGIN`/`XEND`: HotSpot's C2-only
   `UseRTMLocking` was removed in JDK 24 and Graal never implemented RTM; the
   remaining paths (a Panama critical downcall that leaves a transaction open across
   the return into compiled Java code, or a forked Graal with new LIR) are either
   unsupported-and-fragile or a compiler fork (§4).
3. The win, where it exists, is confined to *contended* locks guarding *non-conflicting*
   short critical sections (mutex-protected caches/maps, read-mostly `RWMutex`);
   uncontended locks get slower, because a transaction begin/commit costs more than
   the one CAS of Go's `sync.Mutex` fast path. The cheapest falsifier does not need the
   JVM at all: measure the ceiling with gc plus a hand-written TLE wrapper (Go's
   assembler accepts `XBEGIN`/`XEND`/`XTEST`, verified below) before spending anything
   on the graalgo side (§6).

**This host:** AMD Ryzen 7 9700X (Zen 5) under WSL2; `/proc/cpuinfo` has neither
`rtm` nor `hle` `[VERIFIED]` (AMD has no TSX). No measurement in this note was or
could be taken here; the first step of WP-TMELIDE is getting access to RTM hardware.

## 2. Hardware and OS status (2026-11)

| Platform | HTM | Status today |
|---|---|---|
| Intel client (Core) | TSX (HLE+RTM) | Disabled by June 2021 microcode on Skylake…Coffee/Whiskey Lake (CPUID bit `RTM_ALWAYS_ABORT`; Linux clears RTM/HLE when it is set) `[VERIFIED]` ([tip commit "x86/tsx: Clear CPUID bits when TSX always force aborts"](https://lkml.iu.edu/2106.1/09813.html), [Xen changelog](https://lists.xenproject.org/archives/html/xen-changelog/2021-06/msg00211.html)); 10th-gen Comet Lake / Ice Lake client and later client parts ship without TSX `[UNVERIFIED]` (secondary: [Wikipedia, TSX](https://en.wikipedia.org/wiki/Transactional_Synchronization_Extensions)). Not a target. |
| Intel HLE (`XACQUIRE`/`XRELEASE`) | — | Removed from Intel products released in 2019 and later per SDM vol. 1 §2.5 (May 2020) `[UNVERIFIED]` (quoted via Wikipedia; SDM not re-read). Irrelevant: RTM is the only interface. |
| Intel Xeon (P-core) | RTM | Present on parts not affected by TAA (CVE-2019-11135): the Linux commit that changes the default names "Cooper/Ice Lake, Sapphire/Emerald/Granite Rapids" `[VERIFIED]` ([tip f8c7600d, 2026-01-06](https://lkml.iu.edu/hypermail/linux/kernel/2601.0/05826.html)). Linux kept TSX **off by default** (`CONFIG_X86_INTEL_TSX_MODE_OFF`; `tsx=auto` disables it only where `X86_BUG_TAA` is present) `[VERIFIED]` ([kernel TAA doc](https://www.kernel.org/doc/html/latest/admin-guide/hw-vuln/tsx_async_abort.html)) until that commit switched the Kconfig default to AUTO, merged for Linux 7.0 `[VERIFIED]` for the diff, release tagging `[UNVERIFIED]` ([Phoronix](https://phoronix.com/news/intel-tsx-auto-linux-7.0)). SUSE kernels have shipped TSX enabled for ~6 years (same commit message) `[VERIFIED]`. Sapphire Rapids added `TSXLDTRK` (suspend load tracking) `[UNVERIFIED]`. Xeon 6 E-core parts (Sierra/Clearwater Forest): no TSX `[UNVERIFIED]`. |
| Cloud VMs on those Xeons | RTM if the BIOS, the host kernel and the hypervisor's CPUID all allow it | Per provider/instance type `[UNVERIFIED]` — must be probed (`grep -w rtm /proc/cpuinfo`, CPUID.7.0:EBX[11] and EDX[11] `RTM_ALWAYS_ABORT`). Bare-metal instances let us set `tsx=on` on our kernel but not the BIOS. |
| AMD | none | AMD's ASF proposal (2009) never shipped `[HIST]`; no Zen generation has HTM `[VERIFIED]` for this host's Zen 5. |
| Arm | TME (Armv9 optional) | Withdrawn: "FEAT_TME is withdrawn from all future versions of Arm Architecture Reference Manual for A-profile" (Arm known issue D24093), no silicon adopted it; LLVM removed TME support 2025-11-14 `[VERIFIED]` ([LLVM commit 8cc93c49](https://llvm.googlesource.com/llvm-project/+/8cc93c490db1e229bd811b805c21867adc533ea4)). |
| IBM POWER | `tbegin.` (POWER8/9) | ISA 3.1 removed TM; Linux removed TM from POWER10 features (2020); POWER10 has only a degenerate facility in POWER8/9 compatibility modes `[VERIFIED]` ([linuxppc-dev patch](https://lore.kernel.org/linuxppc-dev/20200827035529.900-1-jniethe5@gmail.com/t/), [QEMU patch](https://patchew.org/QEMU/20240312131419.2196845-1-npiggin@gmail.com/20240312131419.2196845-3-npiggin@gmail.com/)). |
| IBM Z | Transactional Execution (`TBEGIN`, zEC12+) | "support being sunset starting with z17" `[VERIFIED]` ([IBM Z Processor Optimization Primer v5](https://www.ibm.com/support/pages/system/files/inline-files/IBM%20Z%20Processor%20Optimization%20Primer%20v5.pdf)); not a graalgo target anyway. |
| RISC-V | none | No ratified HTM extension `[UNVERIFIED]`. |

Arm's own words in the LLVM commit — TME "has not been adopted by the ecosystem.
This mirrors what Arm has observed with similar extensions in other
architectures" — are the industry-wide summary `[VERIFIED]`.

## 3. Software precedent

- **HotSpot.** `UseRTMLocking` (C2-only, x86 and ppc, never default) came with
  JDK-8031320 (2014) `[HIST]` ([RFR](https://mail.openjdk.org/pipermail/hotspot-dev/2014-March/013045.html));
  deprecated in JDK 23 (JDK-8328986: complexity and maintenance burden; the new
  lightweight locking would not support it; it "only helped some cases of heavy lock
  contention"; Intel agreed with removal, JDK-8328991) and obsoleted with its code
  removed in JDK 24 (JDK-8329141) `[VERIFIED]` ([JDK-8328986 integration](https://mail.openjdk.org/pipermail/hotspot-runtime-dev/2024-March/069094.html),
  [JDK-8329141](https://bugs.openjdk.org/browse/JDK-8329141),
  [JDK-8328991](https://bugs.openjdk.org/browse/JDK-8328991)). The ppc
  implementation went earlier (JDK-8307907, 2023) `[UNVERIFIED]` release.
  Our pin (GraalVM JDK 25) has none of it.
- **Graal.** Never implemented RTM locking — the RTM jtreg tests are excluded when
  Graal is the JIT `[VERIFIED]` ([JDK-8209972](https://bugs.openjdk.org/browse/JDK-8209972)).
  No Truffle or JVMCI API exposes transactional instructions `[UNVERIFIED]` (no
  evidence of one found).
- **glibc.** Mutex elision is runtime opt-in through `glibc.elision.enable`
  (default off; the configure-time default-on switch was removed in 2017) and only
  on x86-64 with RTM, POWER and z `[VERIFIED]` ([manual](https://www.gnu.org/software/libc/manual/html_node/Elision-Tunables.html),
  [2.42 x86 source](https://sources.debian.org/src/glibc/2.42-1/sysdeps/unix/sysv/linux/x86/elision-conf.c)).
  glibc also blacklists TSX on CPU models with the broken microcode
  ([patch](https://patchwork.sourceware.org/comment/118206/)) `[VERIFIED]`.
- **Go.** No golang/go proposal or runtime work on HTM found `[UNVERIFIED]`
  (searched, nothing surfaced — absence is weak evidence). The relevant result is
  **GOCC** (Uber Research + UCSB, USENIX ATC '21): a source-to-source rewrite of
  `Lock`/`Unlock` pairs into calls to an RTM library with a hashed-perceptron
  per-site predictor that learns when to elide `[VERIFIED]`
  ([paper](https://www.usenix.org/system/files/atc21-zhang-zhizhou.pdf),
  [arXiv](https://arxiv.org/abs/2106.01710), [code](https://github.com/uber-research/GOCC)).
  Reported: up to 10× on lock-heavy library benchmarks; fastcache geomean +15.65%
  (max +35.6%); zap "mild", worst slowdown 7%; tally sensitive group −10% at 1 CPU,
  +145/283/267% at 2/4/8 CPUs; overall "rare and relatively small slowdowns". Of
  ~8,000 `Unlock()`s in 21 MLOC of Uber Go code, ~76% are `defer`red — so a
  critical section often extends to function exit, which lengthens transactions.
  GOCC deliberately emits *patches* developers review, citing developers' demand for
  visibility: "using HTM without developers' knowledge can prove unwelcome"
  `[VERIFIED]`. That is the most direct counter-argument to the "automatic
  everywhere" framing, from the people who built the closest prior art.
- **Academic.** Speculative lock elision (Rajwar & Goodman, MICRO 2001) `[HIST]`;
  Intel TSX on HPC workloads: 1.41× average over locks/atomics, 1.31× on a
  user-level TCP/IP stack (Yoo et al., SC13) `[VERIFIED]` for the abstract numbers
  ([PDF](https://pages.cs.wisc.edu/~markhill/restricted/sc13_haswell.pdf));
  the "lemming effect" — once one thread falls back to the lock, every concurrent
  transaction that subscribed to the lock word aborts and also falls back, so
  elision collapses to serial locking under bursts (Dice et al., 2008–2009) `[HIST]`;
  mitigations: wait for the lock to be free before retrying, bounded retries by
  abort cause, adaptive per-lock policies (Afek, Levy, Morrison, PODC 2014,
  "software-improved hardware lock elision"; Diegues & Romano, ICAC 2014,
  "self-tuning Intel TSX"; Calciu et al., TRANSACT 2014, improved single-global-lock
  fallback) `[HIST]`. Abort sources on RTM: data conflicts at cache-line granularity
  (false sharing counts), capacity (write set bounded by L1D, read set larger),
  interrupts and context switches, page faults, syscalls and many privileged or
  "unfriendly" instructions (`PAUSE`, `CPUID`, x87/some vector state changes) `[HIST]`
  ([Intel TSX anti-patterns](https://www.intel.com/content/www/us/en/developer/articles/technical/tsx-anti-patterns-in-lock-elision-code.html)).
  Begin+commit cost is tens of cycles on Haswell–Skylake, i.e. more than an
  uncontended `LOCK CMPXCHG` on an owned line `[UNVERIFIED]` — to be measured (§6).

## 4. Applicability to graalgo

### 4.1 Where locks are in a Go program on graalgo

- **`sync.Mutex`/`RWMutex`/`Once`/`WaitGroup`/`Cond`/`Pool`/`Map`** run upstream's
  Go code verbatim on the new path; only the semaphore slow path is ours
  ([SyncIntrinsics.java](../../internal/backend/truffle/src/main/java/graalgo/go/intrinsics/SyncIntrinsics.java):
  gc's `sema.go` algorithms, striped roots with JVM monitors guarding queue edits
  only, waiters parked through `SchedulerCore.parkCurrent`). The fast path is one CAS
  on `Mutex.state` through our atomics; `RWMutex.RLock` is an atomic add on
  `readerCount` — a shared, written cache line, which is exactly what elision removes.
- **Channels** ([GoChan.java](../../internal/backend/truffle/src/main/java/graalgo/runtime/GoChan.java),
  [SelectOp.java](../../internal/backend/truffle/src/main/java/graalgo/runtime/SelectOp.java))
  use JVM `synchronized` monitors. Eliding those would need the JVM's own monitor
  implementation (where RTM was removed). Blocking channel operations park/unpark
  virtual threads, which always aborts. Out of scope.
- **Scheduler internals** (Drainer, wake lane, timer heap): short, mostly
  single-writer, already tuned (WP-SCHED, ADR-0016); any park/unpark inside aborts.
  Out of scope.
- **Maps** are not locked in Go (concurrent writes are a fatal race); user code wraps
  them in a `Mutex` or `RWMutex` — the target workload class.
- **Atomics** are already lock-free; nothing to elide.

So "everywhere" in practice means: the user-visible `sync.Mutex`/`RWMutex` lock and
unlock, rewritten structurally (an intrinsic around upstream's code applied to every
mutex, never name-keyed). The fallback path *is* upstream's code, unchanged.

### 4.2 Semantic hazards (each needs a rule and a test)

1. **Unlock from another goroutine** is legal in Go. If goroutine A elides `Lock` and B
   calls `Unlock`, under gc B's `Unlock` releases a really-held lock; under naive
   elision B reads a free lock word and raises "fatal error: sync: unlock of unlocked
   mutex". Rule: inside an elided region, every runtime synchronization operation
   other than the matching `Unlock` on the same goroutine (channel op, park, wake,
   another mutex's slow path, `go` statement; plain atomics on other words are
   harmless) executes `XABORT` first, so the region falls back
   to the real lock before any other goroutine can learn of it; HTM's strong
   isolation means B cannot observe A's speculative writes without aborting A.
   GOCC's static Lock/Unlock pairing is the stricter alternative.
2. **Re-locking the same mutex** in one goroutine deadlocks under gc (and the
   deadlock detector reports it). Inside a transaction the lock word still reads
   free, so a naive elided re-lock would *not* deadlock — a differential divergence.
   Rule: keep a per-transaction set of elided mutexes; re-lock of a member aborts.
3. **Starvation mode / fairness.** Go's `Mutex` switches to FIFO handoff after a
   waiter waits 1 ms. Elided acquirers never queue; a fallback acquirer aborts every
   subscribed transaction (lemming). GG-FAIR/GG-SCHED observables must be shown
   unchanged; the policy must stop eliding a mutex while its starving bit is set.
4. **Parking, blocking I/O, syscalls, allocation slow paths** inside a critical
   section abort (virtual-thread unmount copies the stack; TLAB refill and GC
   barriers' slow paths call into the VM). Fine for correctness, fatal for the win on
   sites that do them — hence per-site adaptivity.
5. **Safepoints and GC.** Our cooperative preemption poll reads a shared epoch that a
   ticker bumps every ≤ 1 ms (WP-POLLCOST); every transaction that reads it aborts on
   the bump — at most one abort per ms per thread, acceptable. A JVM safepoint or
   handshake arms the poll and the transaction aborts before the thread stops, so
   the thread is never at a safepoint mid-transaction; whether *every* HotSpot/Graal
   invariant survives an abort that rewinds compiled Java frames is the open
   feasibility question (§4.3), not something to assume.
6. **Memory model.** A committed elided critical section is atomic and serialized
   with every other critical section on the same lock (they all read the lock word;
   the fallback writes it), so the Go memory model's "Unlock n happens before Lock
   n+1" edges are preserved for race-free programs; racy readers can observe fewer
   interleavings, never more (strong isolation) `[HIST]` for the TLE argument.
7. **False sharing and capacity**: conflicts are per cache line; our value model
   decides layout (descriptor-based structs), so the lock word and hot fields may
   share lines in ways gc's layout would not — measure, and composes with the
   struct field reorder item.
8. **Debuggability and profiling**: breakpoints, single-stepping, sampling
   interrupts (JFR, `perf`) abort transactions; JFR events need an abort-cause
   counter per site, and a kill switch (`GODEBUG`-style knob routed through the
   registry, default off until proven).
9. **Determinism**: elision makes lock acquisition order depend on abort timing;
   tests that assert scheduler observables need a deterministic mode (elision off).

### 4.3 How graalgo could emit XBEGIN/XEND at all

| Path | Feasibility (2026-11, GraalVM JDK 25 pin) |
|---|---|
| HotSpot `UseRTMLocking` | Gone (JDK 24), never covered Graal-compiled code, and only applied to Java monitors `[VERIFIED]`. |
| Truffle/JVMCI intrinsic | No API to inject arbitrary machine instructions from a Truffle language `[UNVERIFIED]`. Would need new Graal LIR ops (`XBEGIN` with an abort-target block, `XEND`, `XABORT`, `XTEST`) and a node that models "control may resume here with all memory effects of the region rolled back" — i.e. a fork of the Graal compiler, which the pinned Oracle GraalVM distribution does not allow and which conflicts with "build on Truffle/Graal machinery, don't reimplement it" (PHASES §7). Native Image would need the same in its compiler. |
| Panama critical downcall | `Linker.Option.critical` (JDK 22+) skips the thread-state transition `[HIST]`. Two tiny native stubs (`xbegin`, `xend`) would leave the transaction open across the return into compiled Java code; an abort rewinds registers and memory to the `xbegin` stub, which returns a status to the same Java call site. Architecturally coherent but **unsupported**: no JVM promises that compiled code, deoptimization, GC barriers or code-cache patching are abort-safe. Cost: two downcalls (several ns each `[UNVERIFIED]`) per critical section on top of begin/commit, so it cannot win uncontended. Spike-only, never production without an upstream JVM contract. |
| Native helper running the whole critical section | Impossible: the critical section is Go code compiled by Truffle. |

## 5. "Automatic everywhere" vs adaptive

Eliding every lock regresses the common case: most Go mutexes are uncontended (the
fast path is one CAS) or guard sections that allocate, log, `defer`, or block. The
defensible design is profile-driven: Truffle already profiles; a per-site (or per
mutex allocation site) state machine — *plain* by default; promote to *elide* only
after observed contention (slow-path entries / CAS failures) on short critical
sections; demote on abort ratio > 50% over a window or any capacity/explicit abort
streak; re-probe with exponential backoff (GOCC's perceptron and glibc's adaptive
counters are the precedents). Retry policy: no retry on capacity or explicit aborts;
≤ 3 retries on conflict aborts with the `RETRY` hint, each only after the lock word
reads free (anti-lemming). Promotion compiles the elided variant; demotion is a
deopt — the same guard/deopt discipline as every other Phase-5 item.

## 6. Experimental design (WP-TMELIDE)

**Step 0 — hardware.** This host cannot run RTM (§1). Acquire a TSX-enabled Xeon
(Sapphire/Emerald/Granite Rapids; bare metal preferred, else a cloud VM whose
`/proc/cpuinfo` shows `rtm` and CPUID shows no `RTM_ALWAYS_ABORT`); record CPU
model, microcode, kernel `tsx=` mode, SMT on/off. If none is reachable, WP-TMELIDE
stops here with that recorded.

**Step 1 — ceiling with gc, no JVM.** A TLE wrapper around `sync.Mutex`/`RWMutex`
in Go assembly under gc 1.27.1 (Go's assembler accepts `XBEGIN`/`XEND`/`XTEST`:
assembled and disassembled here on 2026-11-20 — `XBEGIN` = `c7 f8 rel32`, `XEND` =
`0f 01 d5`, `XTEST` = `0f 01 d6` `[VERIFIED]`), or GOCC itself. Benchmarks, each at
1, 2, 4, 8, 16, 32 threads and all cores (SMT on and off):

| Id | Workload | Expectation |
|---|---|---|
| B1 | uncontended `Lock; x++; Unlock` (cf. the suite's `mutex`) | elision loses (overhead) |
| B2 | contended single counter (true conflict) | no win; measures lemming/fallback cost |
| B3 | `map[int]V` behind one `Mutex`, 1 Ki–1 Mi keys, 90/10 and 99/1 get/put, uniform and Zipf | the main candidate |
| B4 | same map behind `RWMutex`, 99.9/0.1 | the other main candidate (`readerCount` line) |
| B5 | fastcache / go-cache / tally benchmarks (GOCC's set) | realistic; cross-check against GOCC's numbers |
| B6 | critical section that allocates / parks / does I/O | 100% abort; fallback overhead must be < 2% |

Metrics: throughput (ns/op, benchstat, ≥ 10 runs), p99 latency for B3–B5, abort
rate by cause (RTM status bits; `perf stat -e tx-start,tx-commit,tx-abort` and the
capacity/conflict events), fallback fraction, starvation-mode entries.

**Step 2 — JVM spike (only if step 1 passes).** The Panama critical-downcall path
of §4.3 on HotSpot 25 (G1 and ZGC) under GC, deopt and safepoint torture (the
existing torture legs), plus Native Image; outcome: works / crashes / aborts
always, with abort-cause histograms. In parallel, ask the GraalVM team whether an
RTM intrinsic is acceptable upstream — without that, step 3 cannot ship.

**Step 3 — graalgo prototype (only if step 2 passes).** Adaptive policy of §5 as an
intrinsic around upstream `sync` code; differential corpus and the scheduler
batteries unchanged with elision forced on; then B1–B6 vs gc and gc+PGO.

**Kill criteria** (any one ends the item, recorded as a negative result in
[SURVEY.md](SURVEY.md)'s ATTEMPTED list):

- no TSX-enabled hardware reachable for the program's deployment targets;
- step 1: gc+TLE beats gc plain by < 10% on B3–B5 at ≥ 8 threads (if the ceiling is
  that low, graalgo's extra downcall overhead makes it negative);
- step 2: no abort-safe way to emit the instructions on the pinned GraalVM;
- step 3: < 10% over gc+PGO plain-lock on B5 or a realistic server workload, or
  > 2% regression on B1 / the bench suite with the adaptive policy on, or any
  differential, deadlock-report or fairness divergence (GG-0007's bar applies:
  a claimed win must beat gc+PGO on its named benchmark or be struck).

**Estimate** `[EST]`: steps 0–1 3–6 agent-days once hardware exists; step 2 5–10;
step 3 15–30. Expected outcome on current evidence: a negative or
narrow-niche result.

## 7. Sources

Primary sources are linked inline above. Additionally: Intel TSX anti-patterns
([intel.com](https://www.intel.com/content/www/us/en/developer/articles/technical/tsx-anti-patterns-in-lock-elision-code.html));
GOCC ATC '21 slides
([usenix.org](https://www.usenix.org/system/files/atc21_slides_zhang-zhizhou.pdf));
an early Go RTM experiment noting the cost of non-inlinable assembly calls
([0xmjk/go-tsx-rtm](https://github.com/0xmjk/go-tsx-rtm)) `[UNVERIFIED]`.
