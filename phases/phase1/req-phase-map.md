# graalgo requirements → phase map (Phase 1 focus)

Source: all 70 requirement files in `~/workspace/graalgo/requirements/` (GG-0001–GG-0060, GG-SCHED-0001–0004, GG-WAKE-0001–0003, GG-FAIR-0001–0003), all `locked`. Read-only pass — no IDs or statuses changed.

Phase definitions used (from task brief):
- **Phase 1** — complete Go language (through generics) on a Truffle AST: `go/parser`+`go/types` frontend service, Truffle nodes for all statements/expressions, descriptor-based value model, defer/panic/Goexit, intrinsic surface stubs for language tests, differential harness on language suites.
- **Phase 2** — goroutines/channels/GC interop (scheduler, user-space quantum preemption, wakeup protocol).
- **Phase 3** — stdlib adaptation layer incl. net/os; cgo/Panama FFI (linux/amd64 first).
- **Phase 4** — cmd/go integration (driver wrapper via `-toolexec`, CLI surface, build/test/module behavior).
- **Phase 5** — execution modes (pure JIT, AOT Native Image, AOT+JIT hybrid), isolates, reachability metadata, plugin/FFM details.
- **cross-cutting** — applies across phases; a "Phase-1 slice" is named where one exists.

## 1. Requirement → phase table

### 1a. Core Phase 1 (owned by Phase 1)

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0009 | 1 (slice) | Reuse Go stdlib maximally: verbatim vendor + adapted fork limited to runtime-coupled packages; Plan 9 asm per-category handling. | Phase-1 slice: vendoring + intrinsic stubs for language tests. Full adapted fork is Phase 3. |
| GG-0012 | 1 | Use `go/parser`+`go/types` vendored verbatim as frontend, re-vendored atomically per release. | Direct Phase-1 scope: the frontend service. |
| GG-0013 | 1 | Support older language versions via per-file/per-module `-lang` gates; no pinning below vendored stdlib version. | Language-version policy for the Phase-1 frontend. |
| GG-0014 | 1 | One Truffle AST from go/ast+go/types feeds all three execution modes; no second backend. | Direct Phase-1 scope: build the AST once, correctly. |
| GG-0015 | 1 | Go values as Java objects: descriptor-based structs, typed-array slices, (itab,data) interfaces, byte[] strings; opaque pointers. | Direct Phase-1 scope: the descriptor-based value model. |
| GG-0016 | 1 | `unsafe.Pointer` emulated via handle table; escaping arithmetic traps; restriction documented, never silent. | Language-level unsafe semantics for Phase-1 language tests. |
| GG-0019 | 1 | panic as AbstractTruffleException, Goexit as ControlFlowException, explicit per-goroutine defer chain, panic(nil)→*PanicNilError. | Direct Phase-1 scope: defer/panic/Goexit model. |
| GG-0020 | 1 (slice) | Versioned intrinsic surface of ~60–70 functions (memmove/memequal, makeslice/growslice, map/chan/select ops, itab, panic/defer, scheduler, atomics, timers, fastrand, traceback) + exact rtype/itab layouts. | Phase-1 slice: stubs for language tests. Full chan/select/scheduler/timer impl is Phase 2. |
| GG-0021 | 1 (slice) | Linkname compatibility runtime↔stdlib by symbol name, not gc ABI; linkname-diff step scopes per-release churn. | Phase-1 slice: symbol-name linkage for the stubbed runtime. Per-release churn scoping is cross-cutting. |
| GG-0059 | 1 | At Phase-1 interface freeze, AST↔runtime intrinsic signatures and `-toolexec` argv contract versioned and documented. | Explicitly a Phase-1 end-of-phase deliverable; Phase 4 start depends on it. |

### 1b. Cross-cutting requirements with a Phase-1 slice

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0007 | cross-cutting | Every claimed optimization win must beat gc+PGO on its falsifying benchmark or be struck. | Applies from Phase 1 onward; Phase-1 slice: no premature optimization claims on the AST interpreter. |
| GG-0010 | cross-cutting | Four-layer testing strategy built early, starting Phase 0 (harness seed), before the work it gates. | Phase-1 slice: differential harness on language suites is the Phase-1 gate. |
| GG-0023 | cross-cutting | v1 targets Linux (amd64 first, arm64 second); darwin deferred; Windows out of v1. | Constraints every phase; Phase-1 slice: dev + CI on linux/amd64. |
| GG-0024 | cross-cutting | Delve unsupported; Truffle Chrome Inspector is the debugger; `go tool pprof` via profile.proto translation. | Phase-1 slice: `--inspect` must work against the Phase-1 AST; pprof translation can come later. |
| GG-0027 | cross-cutting | Pin GraalVM/JDK distribution; SpawnIsolate/OSR verified against pin in Phase 0; Go bumps and GraalVM bumps ride separate trains. | Phase-1 slice: pin recorded and stable during Phase-1 JIT-mode work. |
| GG-0029 | cross-cutting | Upgrades/releases gated by four layers: differential smoke → language conformance → stdlib+cmd/go suites → generative fuzz soak. | Phase-1 slice: layers 1–2 on the language suites. |
| GG-0030 | cross-cutting | Differential-vs-gc oracle: stdout/stderr normalized, exit codes, panic traces modulo addresses, sandboxed effects; excludes timings/addresses/GC stats/raw scheduling. | Phase-1 slice: oracle definition applies to language-suite runs (excluding scheduling, which Phase 1 does not have). |
| GG-0031 | cross-cutting | Nondeterminism controls: GOMAXPROCS=1 scheduling subset, multiset comparison, 3–5 repeat runs, fixed seeds, synctest preference. | Phase-1 slice: multiset/repeat/fixed-seed controls; GOMAXPROCS subset only gains meaning in Phase 2. |
| GG-0032 | cross-cutting | Corpora: stdlib `go test ./...`, `src/test/` language suite, targeted corpora for churny areas. | Phase-1 slice: `src/test/` language suite is the spec-semantics anchor. |
| GG-0033 | cross-cutting | Backend-specific test exclusions in a versioned, reason-coded list; grows only through per-release review; shrinking is the health metric. | Applies from Phase 1: language-suite exclusions begin here. |
| GG-0034 | cross-cutting | Every oracle layer mutation-validated (break the lowering, harness goes red); red-green evidence required. | Phase-1 slice: red-green evidence for the language-suite oracle. |
| GG-0036 | cross-cutting | Per release, differential-corpus results under gc recorded content-addressed; gc drift = upstream, graalgo drift = regression. | Applies from the first Phase-1 differential runs. |
| GG-0037 | cross-cutting | Two fuzz layers: testing.F differential fuzzing + generated programs across the feature space through the differential harness. | Phase-1 slice: program generation across language features through the differential harness. |
| GG-0038 | cross-cutting | Per-commit CI runs layers 1–2; per-release all four; every red gate produces a triage record; no waiver without recorded reason. | Phase-1 slice: per-commit layers 1–2 on language suites. |
| GG-0042 | cross-cutting | Every upgrade report publishes per-component toil accounting; P95 bounds checked mechanically. | Phase-1 slice: measure toil on the language-test porting + vendor rebase work. |
| GG-0044 | cross-cutting | Local patches against vendored Go sources kept near zero; rebase conflicts ≤5 files per upgrade. | Applies from Phase 1 vendoring onward. |
| GG-0045 | cross-cutting | For every runtime-internal behavior with no JVM analog, the parity stance is decided once and recorded; never re-debated. | Phase-1 slice: record stances for language/runtime-internal behaviors as they arise. |
| GG-0047 | cross-cutting | Sulong-vs-direct-AST fork decided by Phase-0 scored spike; default = direct Truffle AST [PROPOSAL]. | Decision consumed in Phase 0; Phase-1 slice: direct AST is the settled architecture. |
| GG-0055 | cross-cutting | Every GODEBUG knob classified per release (emulated / no-op / unsupported); unknown knobs deny-with-error; reviewed per release. | Phase-1 slice: seed the registry for runtime-coupled knobs reachable from language tests. |
| GG-0056 | cross-cutting | No agent-leverage discount in the 20–40 PY estimate; agent leverage measured and calibrated empirically. | Applies from Phase 1: measure actual vs estimated agent-hours. |

### 1c. Cross-cutting, no Phase-1 slice (future-phase concerns)

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0004 | cross-cutting | Track most recent Go release via agent-driven upgrade workflow; P95 ≤200 agent-hours, ≤10 wall-clock days per release. | Upgrade-workflow concern, first exercised after Phase 1 ships the language core. |
| GG-0035 | cross-cutting | Upgrade diff tooling: apidiff, go/types signature diff, rtlink, flagdiff, godebug-diff, respdiff; tooling carries red-green evidence. | First used at the first upgrade, post-Phase 1. |
| GG-0039 | cross-cutting | Every major upgrade executes the 9-phase runbook starting at rc1. | Upgrade-time only. |
| GG-0040 | cross-cutting | Patch releases take the light path unless patch notes touch language/driver behavior. | Upgrade-time only. |
| GG-0041 | cross-cutting | Weekly release-watch agent detects rc1/.0, prepares `bump/go1.N`, notifies; never merges or declares done-gates. | Upgrade-time automation. |
| GG-0043 | cross-cutting | Any toil-bound breach triggers a toil-reduction sprint; two consecutive breaches force re-scope with Carlo. | Escalation policy over the upgrade cadence. |

### 1d. Phase 2 — goroutines/channels/GC interop

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0017 | 2 | Goroutines 1:1 on virtual threads + scheduler token; Safepoint.poll at loop back-edges/function entries; FJP untrusted; GOMAXPROCS = in-flight cap. | Explicitly the Phase-2 scheduler work (task brief excludes goroutines/channels/GC interop from Phase 1). Note: the Phase-1 AST must expose the Safepoint instrumentation seam (see §3). |
| GG-0025 | 2 | momo module generations map to GraalVM isolates; memory quotas via MaxHeapSize; no CPU quotas in v1. | Isolation/interop concern; needs the value model + Phase-2 scheduling. |
| GG-0048 | 2 (ambiguous) | Spec defines the exact scheduling observables the differential oracle must hold equivalent; unclosable divergence = kill trigger. | Scheduler observables; but "the spec SHALL define" may demand the definition before Phase 2 begins (see §3). |
| GG-0057 | 2 | `jdk.VirtualThreadPinned` JFR shows zero unexpected pins on conformance corpus; cgo/FFM and blocking file I/O off the carrier pool. | Scheduler/carrier discipline; cgo/FFM half reaches into Phase 3+. |
| GG-SCHED-0001 | 2 | On quantum expiry at Safepoint.poll, vthread parks via LockSupport.park + enqueues resume token; default quantum 10ms. | Child of GG-0017. |
| GG-SCHED-0002 | 2 | Single platform-thread drainer admits vthreads via LockSupport.unpark in lane-priority order; non-terminating. | Child of GG-0017. |
| GG-SCHED-0003 | 2 | Admitted-but-not-parked vthreads never exceed min(GOMAXPROCS, carrier parallelism); drainer hard-asserts. | Child of GG-0017. |
| GG-SCHED-0004 | 2 | All blocking via scheduler-aware primitives; blocking I/O via slot-releasing NIO wrapper (ioEnter/ioExit, unpaired ioEnter = fail-fast assert). | Child of GG-0017; the NIO wrapper slice reaches Phase 3 (net). |
| GG-WAKE-0001 | 2 | Per-vthread armed ticket + generation counter; acked flag set before unpark (order reversal loses wakeups). | Child of GG-0017. |
| GG-WAKE-0002 | 2 | At quiescence admissions == parks; zero lost wakeups under randomized park/unpark + channel-wakeup stress. | Child of GG-0017. |
| GG-WAKE-0003 | 2 | wakeFifo lane for woken/channel-ready goroutines ahead of runFifo; initFifo for new goroutines ahead of runFifo. | Child of GG-0017. |
| GG-FAIR-0001 | 2 | At 10,000-goroutine scale, per-goroutine admitted quanta max/mean within 2x. | Scheduler fairness metric. |
| GG-FAIR-0002 | 2 | Mean per park/resume hop overhead ≤1% of configured quantum. | Scheduler overhead metric. |
| GG-FAIR-0003 | 2 | Every runnable goroutine admitted within 4×N drainer admissions; 10,000-spinner quiescence probe never_scheduled=0. | Scheduler starvation metric. |

### 1e. Phase 3 — stdlib adaptation (net/os), cgo/FFI

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0003 | 3+ | Expose extra JVM-integration APIs (`graalgo/java` package, polyglot bindings) without altering stock behavior. | Polyglot surface; post-Phase-2, rides on the interop contract. |
| GG-0005 | 3+ | Bidirectional Go↔Java calls with shared types: copy-vs-view semantics, error↔exception mapping, func↔lambda adapters. | Interop contract; needs value model + Phase-2 scheduling landed. |
| GG-0009 | 3 | (adapted-fork slice of GG-0009) adapted fork limited to runtime-coupled packages. | Runtime-coupled package adaptation is the Phase-3 core. |
| GG-0018 | 3 | Go's netpoller event loop deleted; `net` implemented on virtual-thread-friendly blocking NIO. | Explicitly net; "no net/os (Phase 3+)". |
| GG-0022 | 3+ | cgo maps to Panama FFI; stock cgo tool + host gcc kept for C side; linux/amd64 first, arm64 second; darwin/windows deferred. | FFI boundary; linux/amd64 first places it Phase 3 at earliest. |
| GG-0058 | 3+ | momo's MM-0017 verifier ban on C interop gates graalgo's Panama FFI path: module code must not reach the FFI boundary. | Gate on the Phase-3 FFI path. |

### 1f. Phase 4 — cmd/go integration

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0001 | 4 | Present the stock `go` CLI: all 19 commands, flags, help, exit codes 0/1/2; `alias go=graalgo` indistinguishable at CLI. | Driver/CLI surface; explicitly "no cmd/go integration (Phase 4)". |
| GG-0002 | 4 | Reproduce stock `go` module resolution, build caching behavior, `go test` JSON protocol + two-level caching, `go vet` invocation. | Driver/build behavior; explicitly Phase 4. |
| GG-0011 | 4 | Ship the stock Go distribution; swap only the compile backend behind `-toolexec`; never reimplement the driver. | The `-toolexec` seam is the Phase-4 integration mechanism (GG-0059 freezes its argv contract at end of Phase 1). |
| GG-0028 | 4 | Behavioral build-cache compatibility (hit/miss patterns, not byte-identical); backends auto-namespace via toolID. | Build-cache behavior under the Phase-4 wrapper. |
| GG-0046 | 4 | Faithful GOTOOLCHAIN selection semantics on track-latest; `go version`, `runtime.Version()`, `toolchain` directive match. | Toolchain selection is driver territory. |
| GG-0053 | 4 | Reuse stock `go vet` binary unchanged; txtar matrix verifies vet behavior. | Driver-level reuse, verified through the wrapper. |

### 1g. Phase 5 — execution modes

| ID | Phase | Statement (one line) | Why this phase |
|---|---|---|---|
| GG-0006 | 5 | Three execution modes — pure JIT on HotSpot, pure AOT Native Image, AOT+JIT hybrid — fed by one Truffle AST. | Explicitly "no AOT/JIT modes (Phase 5)". |
| GG-0014 | 5 (feeds) | (single-AST slice of GG-0014) the same AST must feed all three modes with no second backend. | Phase-1 builds the AST; Phase 5 proves the one-AST claim. |
| GG-0026 | 5 | In pure-JIT mode, momo generations isolated via OS processes per generation, weaker containment documented. | Pure-JIT mode isolation story. |
| GG-0049 | 5 | Hybrid becomes default only if it beats both single modes on ≥1 workload class across six falsifying experiments; else cut. | The AOT+JIT hybrid meta-gate. |
| GG-0050 | 5 | AOT baseline correct and performant without the runtime JIT; JIT is a steady-state bonus, never a correctness dependency. | AOT baseline quality bar. |
| GG-0051 | 5 | plugin.Open unsupported in AOT modes (documented error + exclusion entry); supported in JIT-only. | AOT-mode scope limitation. |
| GG-0052 | 5 | AOT reachability metadata for full stdlib derived by tracing agent with zero hand-authored entries as target. | AOT reachability pipeline. |
| GG-0054 | 5 (ambiguous) | OSR in Native Image verified against pinned GraalVM in Phase 0; hybrid's long-loop story documents the measured mechanism. | Verification phase ambiguous: statement names Phase 0 for the OSR check, Phase 5 for the hybrid story (see §3). |
| GG-0060 | 5 | Users select execution mode via build flags/environment with documented defaults; mode selection never changes semantics. | Mode-selection UX; modes themselves are Phase 5. |

## 2. Definitive Phase-1 requirement list (sorted)

Requirements Phase 1 owns or exercises during Phase 1:

1. GG-0007
2. GG-0009 (vendoring + intrinsic stubs slice)
3. GG-0010 (differential harness on language suites)
4. GG-0012
5. GG-0013
6. GG-0014 (AST construction slice)
7. GG-0015
8. GG-0016
9. GG-0019
10. GG-0020 (stubs-for-language-tests slice)
11. GG-0021 (symbol-name linkage slice)
12. GG-0023 (linux/amd64 dev+CI slice)
13. GG-0024 (`--inspect` on the Phase-1 AST slice)
14. GG-0027 (pin recorded and stable slice)
15. GG-0029 (layers 1–2 on language suites slice)
16. GG-0030 (oracle on language suites slice)
17. GG-0031 (determinism controls slice)
18. GG-0032 (`src/test/` language suite slice)
19. GG-0033
20. GG-0034 (language-suite oracle red-green slice)
21. GG-0036
22. GG-0037 (language-feature program generation slice)
23. GG-0038 (per-commit layers 1–2 slice)
24. GG-0042 (toil measurement slice)
25. GG-0044
26. GG-0045 (parity stances recorded as they arise slice)
27. GG-0047 (direct-AST architecture confirmed)
28. GG-0055 (registry seed slice)
29. GG-0056 (agent-hour calibration slice)
30. GG-0059
31. GG-0017 (Safepoint instrumentation-seam slice only — full scheduling is Phase 2; see §3 flag 3)

Phase counts: Phase 1 owns 10 + exercises a slice of 21 = 31; Phase 2 = 14; Phase 3/3+ = 6; Phase 4 = 6; Phase 5 = 9; pure cross-cutting (no Phase-1 slice) = 6. Ambiguous phase tag on 4 items (GG-0017, GG-0048, GG-0054, GG-0057), resolved with the phase-1 slice explicitly above. Total: 70/70 covered (GG-0009, GG-0014, GG-0020, GG-0021 counted under Phase 1 as slices, GG-0009/GG-0014/GG-0054 also listed under their later phases).

## 3. Flags: contradictions and ambiguous phase tags

1. **GG-0020 names Phase-2/Phase-3 machinery in a Phase-1 intrinsic surface.** Exact text: "The runtime SHALL expose a versioned intrinsic surface of ~60-70 functions (memmove/memequal, makeslice/growslice, map/chan/select ops, itab machinery, panic/defer, scheduler, atomics, timers, fastrand, traceback) plus exact rtype/itab layouts for reflect's unsafe uses." "map/chan/select ops", "scheduler", and "timers" are Phase-2 (goroutines/channels/scheduling) features, yet the task brief scopes Phase 1 to "intrinsic surface stubs for language tests". Resolved: Phase 1 builds stubs only; full chan/select/scheduler/timer implementation is Phase 2. If the statement is read literally (all ~60–70 functions in Phase 1), it contradicts Phase-1 scope.

2. **GG-0017 requires the Phase-1 AST to carry a Phase-2 instrumentation seam.** Exact text: "the Truffle AST SHALL insert Safepoint.poll() at loop back-edges and function entries for quantum-based preemption (indefinite park + resume token on expiry)". The scheduler is Phase 2, but the AST (a Phase-1 deliverable) must insert — or at least reserve — the poll points. Resolved: Phase 1 owns the instrumentation seam (hook points in the AST); Phase 2 owns the poll semantics. Ambiguous until the interface freeze (GG-0059) fixes which side owns what.

3. **GG-0048's "spec SHALL define" timing is ambiguous.** Exact text: "The spec SHALL define the exact scheduling observables the differential oracle must hold equivalent (fairness, starvation-freedom, latency distribution, GOMAXPROCS behavior); systematic divergence the user-space scheduler cannot close is a kill trigger." The observables are Phase-2 scheduling work, but the definition must exist before Phase 2's oracle runs — i.e. it may be a Phase-1-exit/Phase-2-entry deliverable, not mid-Phase-2 work. Recommend assigning the *definition* to the Phase-1→2 boundary.

4. **GG-0054 splits across phases in one sentence.** Exact text: "OSR behavior in Native Image SHALL be verified against the pinned GraalVM distribution in Phase 0; the hybrid's long-loop story SHALL document the measured mechanism, not assume OSR." The Phase-0 verification clause should already be consumed (Phase 0 is closed); the hybrid story is Phase 5. If the Phase-0 verification is not in fact archived, this is a scope gap, not a Phase-5 item. Recommend confirming the Phase-0 verification evidence exists.

5. **GG-0047 is a consumed decision rule, not phase work.** Exact text: "The Sulong-vs-direct-AST fork SHALL be decided by the Phase-0 scored spike verdict; the default is the direct Truffle AST [PROPOSAL]; if Sulong wins and the direct-AST estimate doubles, the program replans (not necessarily stops)." It is cross-cutting-but-consumed: it authorized the Phase-1 architecture rather than describing work in any phase. No contradiction; listed so it is not mistaken for open Phase-1 scope.

6. **GG-0008 is multi-phase by construction.** Exact text: "graalgo SHALL integrate virtual threads (goroutines), async I/O, GraalVM isolates, and JVM-grade observability (JFR events, heap dumps, VisualVM in JIT mode)." This single requirement spans Phase 2 (virtual-thread goroutines), Phase 3 (async I/O), Phase 5 (isolates), and cross-cutting (observability). It has no Phase-1 slice at all. Recommend keeping it tagged cross-phase (2/3/5) rather than forcing a single phase.

7. **GG-0057's cgo/FFM half is Phase-3+ inside a Phase-2 requirement.** Exact text: "`jdk.VirtualThreadPinned` JFR events SHALL show zero unexpected pins on the conformance corpus; cgo/FFM calls and blocking file I/O (which pins without unmounting) SHALL run off the carrier pool (separate platform-thread pool) or be counted in the budgeted +k carriers." cgo/FFI is Phase 3+ (GG-0022) and blocking file I/O is Phase 3; the pinning audit cannot fully close until those land. Phase-2 can close only the corpus slice.

8. **GG-0011's `-toolexec` contract vs GG-0059's freeze — sequencing only, no contradiction.** Exact text (GG-0011): "graalgo SHALL ship the stock Go distribution and swap only the compile backend behind the existing `-toolexec` seam". The contract is frozen at the Phase-1 freeze (GG-0059) but implemented in Phase 4; Phase-1 tests must run without the wrapper (direct AST runner), which is consistent but worth stating explicitly.

## 4. Notes

1. Phase 1 has no goroutines, channels, net, os, AOT/JIT modes, or cmd/go integration — any requirement touching those is Phase 2+ by the brief's exclusion list, regardless of its title.
2. "Intrinsic surface stubs" (GG-0020) is the one requirement most at risk of scope creep into Phase 2; the ~60–70 function count includes scheduler/channel/timer entries that Phase 1 cannot meaningfully implement.
3. GG-0059 (interface freeze) is the Phase-1 exit gate: AST↔runtime intrinsic signatures and the `-toolexec` argv contract must be versioned and documented before Phase 4 can start in parallel.
4. Cross-cutting items with no Phase-1 slice (GG-0004, GG-0035, GG-0039, GG-0040, GG-0041, GG-0043) become live at the first upgrade cycle; they constrain Phase-1 design only via GG-0044 (patch-stack discipline) and GG-0042 (toil measurement).
5. This map is read-only classification only; it changes no requirement text, ID, or status.
