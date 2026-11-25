# M3 readiness and deletion plan (W11)

**Date:** 2026-11-25. **Status:** plan of record for M3 (toolexec-design
§8, W11). Lane `m3-readiness` (`wt/m3/readiness`, base master `04e68ec5`).
Planning and measurement only: no code was deleted in this lane.

M3 is the cutover: the v1 pipeline (~497k tracked lines: `internal/frontend`
426k, interop 34k, v1 Java in the truffle module 22k, `internal/vet` 5k,
`cmd/graalgo` 4k, `internal/driver`, `tools/gate1`, two launchers, their
tests) is deleted. Everything the v1 path still covers must either be
covered by the new path (`-toolexec` → GIR → `graalgo.go`) or be dropped on
purpose. This document has the evidence (§1–§2), the gaps (§3), the ordered
steps (§4), and the decisions that belong to the owner (§5). What M3 deletes
and what stays: [toolexec-design.md §9.1](toolexec-design.md#91-m3-delete-inventory-w11),
updated by this lane.

## 0. Verdict

**The new path already beats v1 on every corpus that has a v1 baseline.**
M3's gate, "the corpus passes through the new path at ≥ v1's pass rate"
(toolexec-design §8), is met for the snapshot corpus and for GOROOT/test.
It is **not** met per program: 35 test/corpus programs, 32 GOROOT/test
files, 1 typeparam file and 1 snapshot program that v1 passes fail on the
new path (§2). Most of them have one
root cause each, and the causes are few. The deletion can start now (steps
D0–D3 of §4 do not depend on any gap). The big delete (D6) waits for gaps
G1–G4 to close or to be accepted as dropped coverage (§3, §5).

## 1. Method (reproducible)

Every v1-only corpus ran through the **new** path against gc, with the
existing harnesses and only the backend changed:

- **diffharness** (snapshot and `srctest`/`typeparam` modes): a backend
  config identical to [backends-p3.json](../../test/differential/backends-p3.json)
  except the `graalgo` backend, whose `build` is `go build -overlay <fork>
  -toolexec graalgo-tool -o {BIN} {SRC}` (the `toolexec_build` of
  [test/lib/toolexec.sh](../../test/lib/toolexec.sh), as a 4-line wrapper)
  and whose `run` is `{BIN}`. Same gc snapshot, same comparator, same
  `GOMAXPROCS=1`. Exclusions: none for the snapshot corpus (raw numbers);
  the existing v1 registers for `srctest`/`typeparam` (so "stale" = v1
  fails, new path matches).
- **test/corpus programs that import something** (486; the 140 import-free
  ones already run in `make test-m1a`): [test/m1r/run.sh](../../test/m1r/run.sh)
  with the programs as arguments (it takes any single-file `package main`):
  gc vs the new path, stdout bytes + normalized stderr + exit status, gc leg
  from the gc-oracle cache, `GODEBUG=cpu.all=off` on both legs.
- **v1 baseline for the new-path failures**: the same comparison with
  `graalgo-sync-run` (v1's product launcher) as the graalgo leg.

Host: dev host, `nice -n 19`, one job, `-Xmx2g -XX:ActiveProcessorCount=4`.

## 2. Coverage audit

### 2.1 Measured: v1-only corpora through the new path

| Corpus (v1 gate) | Programs | v1 vs gc | **new path vs gc** | per-program regressions vs v1 | new-path cost (wall / CPU) | v1 cost |
|---|--:|---|---|--:|---|---|
| snapshot corpus `test/differential/corpus` (`snapshot-check`, per push) | 61 | 54 match (7 exclusions) | **59 match**, 2 diverge | 1 (`47_reflect_funcof_structof`) | 57 s / 112 s | 433 s / 659 s (baseline) |
| GOROOT/test `run` files (`conformance.yml`, srctest) | 201 compared (356 scanned: 165 run, 32 errorcheck, 4 compile, 155 not programs) | 155 match (46 listed in `srctest-exclusions.yaml` diverge) | **162 match**, 39 diverge | 32 | 234 s / 330 s | – (CI only) |
| GOROOT/test/typeparam (`TypeparamDifferentialTest`, weekly) | 196 compared | 109 match (87 diverge, listed in `typeparam-exclusions.yaml`) | **186 match**, 10 diverge | 1 | 162 s / 178 s | part of 437 s weekly |
| test/corpus with imports (stdlib-gate waves, topic dirs, phase2; manual or no runner) | 486 | not run as a gate (BUG-0033: untriaged) | **379 pass**, 90 diverge, 17 corpus bugs | 35 of the 90 pass on v1 | 552 s / 895 s | – |
| test/corpus import-free (already `test-m1a`) | 140 | – | all pass (per-push gate) | 0 | in `test-m1a` | – |

Corpus bugs (17, counted out above): 11 programs gc itself rejects — they
call APIs of the v1 synthetic packages (`csv.NewReaderString`,
`csv.NewBufferWriter`, …) or import `internal/*`; 6 print nothing (the
fail-fast probes whose only output is an exit status). They test v1, not
Go: they are deleted with v1 (§4 D6), not ported.

Snapshot corpus: of v1's 7 exclusions, 5 match gc on the new path
(`10_closures`, `19_sort`, `40_method_values`, `42_json`: BUG-0001;
`58_float_minmax_signed_zero`: BUG-0009; `59_recover_named_results`:
BUG-0010); `27_panic_trace` (BUG-0011) still diverges, now only on the
frame-line form `main.main(...)` vs gc's `main.main()`. New divergence:
`47_reflect_funcof_structof` (`reflect: method code pointers are not
available`, the W9b reflect call path).

GOROOT/test: 39 of v1's 46 listed divergences match gc on the new path.
GOROOT/test/typeparam: 78 of v1's 87 listed divergences match gc; the 10
left are 4 harness classifications (gc rejects `issue47896.go` and
`issue50177.go` too; `pragma.go`, `issue54497.go` are errorcheck files),
`chans.go` (timeout), and 5 node-package bugs (G4): a `NullPointerException`
on a nil array length (`boundmethod.go`, and `interfacearg.go`, which v1
passes), the `RawIface` cast (`issue48318.go`), and a missing definition of a
local generic type (`main.T?1`: `issue54456.go`, `nested.go`).

### 2.2 Gate-by-gate: what each v1 gate covers and what covers it on the new path

| v1 gate / corpus | Cadence | Behavior covered | New-path coverage today | Action at M3 |
|---|---|---|---|---|
| `snapshot-check` (62 programs, `graalgo-sync-run`) | per push | language + small std on gc's snapshot | the same 61 programs: 59/61 (§2.1); `test-m1a` (~190 programs), `test-m1r`/`m1c`/`m1d` | **re-pointed** (D1, done); 2 entries in the register |
| `conformance.yml` srctest (GOROOT/test, `backends-p3.json`) | path-filtered push | the Go language suite | 162/201 (§2.1) | **re-point** (D2); register re-triaged from §2.1 |
| `TypeparamDifferentialTest` (`backends.json` → `graalgo-run`) | weekly | generics suite | 186/196 (§2.1) | replace the Java class by `diffharness typeparam run` on the new backend (D2) |
| `test-regression` (7 `graalgo test` packages via `cmd/graalgo`) | per push | `go test` on user packages (M2) + 7 fixed v1 bugs | not covered: `go test` through the launcher is M2 | **gap G5**: run them through the launcher when M2's `go test` lands; until then the 7 bugs are v1-only (`narrow-array-fmt` BUG-0003 and `tls-wrap-hang` BUG-0053 are v1 defects) |
| `test-p3frontend`, `test-v2`, `check`'s gofrontend `build.sh check`, `check_mr_table_subset.py` | per push | v1 frontends, synth tables, wire format | nothing to cover: the code under test dies | delete (D6) |
| `test-jit` (`test/jit/peguard`, `graalgo-sync-run`), `pe-check` | per push / manual | hot code compiles, no PE bailout | `pe-check-m1a` + [pe_std.sh](../../test/m1a/pe_std.sh) (per push), `make pe-report` (nightly) | **re-pointed** (D3, done): `peguard` built through graalgo-tool and run on the bundle runner (hot + OSR, CompileImmediately breadth without the `init·vars` initializers, a planted graph-size violation); v1's `pe-check` deleted (`pe-check-m1a` is the benchmark-set gate) |
| v1 Java classes, truffle (21) and interop (26 = all of `IfaceTestRunner`) | per push / weekly | v1 node classes, values, p3 stand-ins | the new node package's tests + the differential gates | delete (D6); quarantine's 3 lines go with them |
| shared-runtime Java tests that use v1 as driver or values (12: `FreezeSignatureTest`, `StructSizeNestedTest`, `ZeroSizeSliceTest`, `GcInteropTest`, `GcVisibilityTest`, `LifecycleTest`, `NodeConventionsTest`'s predicate, `GcDifferentialTest`, `GoDebugTest`, `LauncherOsArgsTest`, interop-hosted `SyncMapTest`/`AtomicContentionTest`) | per push / weekly | runtime, GC, scheduler, GODEBUG, `os.Args` | partly (`test-m1c`, `test-goroutine-modes`, `ContextLeakProbe`) | **retarget** each to `graalgo.go` values or bundles (D5); `SyncMapTest`/`AtomicContentionTest` move to the truffle module |
| `test-pin-smoke` (all `SchedTestRunner` classes in thread mode) | per push | pinning under JFR | itself, minus the 3 v1 classes it runs | shrinks with D5 |
| `mutation.yml` (32 mutants: 27 in `graalgo/frontend`, 4 in `graalgo/values`, 1 runtime; gate = v1 snapshot-check) | nightly | the oracle can fail | nothing: no new-path mutant set | **G6 closed** (D3): the new-path mutant set (see G6 below) |
| `fuzz-soak.yml` 4a (`tools/fuzz-soak`, `graalgo-sync-run`) | nightly | template-fuzzer differential | `fuzz-newpath.yml` (nightly) and fuzz-soak 4b, both new path | **re-pointed** (D3, done): 4a runs the new path's generator (`test/fuzz/gen.py`, derived from v1's) in batches of 200 via `make fuzz-newpath FUZZ_CORPUS=0` until the deadline, on a seed range disjoint from `fuzz-newpath.yml`'s; v1's `tools/fuzz-soak` (4b never used it) was deleted |
| `stdlib-gate.yml` (334 wave programs via 76 `run_differential.sh`) | manual (BUG-0033) | std package behavior | §2.1 row 4: 379/486 on the new path | **re-pointed** (D3, done): `make stdlib-gate` = `test/m1r/run.sh` over the 486 programs with imports, judged by [std_gate.py](../../test/corpus/std_gate.py) against [std-register.tsv](../../test/corpus/std-register.tsv) (112 entries: 95 deferred-parity citing BUG-0033 with the gap named, 17 corpus-bug; 374/486 match gc, 415 s wall); the 76 wave runners, the 3 `run_wave*.sh` and `tools/run_stdlib_gate.sh` deleted; still manual |
| test/corpus topic `run_differential.sh` (9 scripts), `tools/fuzz` | no caller | – | – | **deleted** (D0, done) |
| `test/e2e` (module mode, `graalgo-sync-run`) | manual | module-mode build/run | `test-toolexec` multi-package and launcher tests | re-point to the launcher (D4) |
| `make bench`, `fast-run`, truffle `run`/`run-sched`/`smoke`/`pin-audit` | manual | v1 benchmarks, warm-JVM loop | `make bench-suite`, `bench-realistic`, `bench-m1a` | **done** (D3): `bench` = `bench-suite`; `test/bench/run.sh`, `fast-run`/`fast-watch`, truffle `run`/`run-sched`/`smoke`/`pin-audit` deleted (`fast-build`/`fast-test` stay: Java only) |
| `tools/godebug-diff` (`SchedMain`) | manual | GODEBUG parity | WP-GODEBUG (new path, W5) | **re-pointed** (D3, done): probes built through graalgo-tool, run on the bundle runner; `panicnil` matches, `schedtrace`/`scheddetail`/`gctrace` are not emulated on the new path yet (WP-GODEBUG; [REPORT.md](../../tools/godebug-diff/REPORT.md)) |
| `gate1`, `gate1-pilot` | manual | v1 trial `go test` of 5 std packages | M2's std-test runner | delete (D6) |
| `test-diffharness` | per push | the comparator (gc vs gc) | itself | precondition on `graalgo-sync-run`/interop classes dropped (D1, done) |

## 3. Gaps that must close (or be accepted as dropped) before D6

Sizes are focused agent-days `[EST]`. "Programs" are the measured failures
of §2.1 the gap explains; ★ = v1 passes some of them (a per-program
regression).

| # | Gap | Programs | Proposed work | Size | Prio |
|---|---|---|---|---|---|
| G1 ★ | **link floor of `crypto/*`, `hash/crc32`, `math/big`, `hash/maphash`, `iter`**: `crypto/fips140.isBypassed` (23), `crypto/internal/fips140.fatal` (9), `fips140/subtle.xorBytes` (1), `hash/crc32.castagnoliSSE42` (17), `math/big.addMulVVWW` (2), `runtime.memhash` (3), `runtime.coroswitch` (2, `iter.Pull`) | 57 corpus (21 ★) | intrinsics or facade bodies for the named symbols (already "Up" in RECOVERY's north-star ranking) | 4–7 | P0 |
| G2 ★ | **runtime API surface of the facade**: `runtime.ReadMemStats` (3), `MemProfile` (2), `runtime/debug.setGCPercent`, `setPanicOnFault`, `runtime.Breakpoint` (3, not declared), `internal/cpu.cpuid` (12: `testing` imports), `runtime.fastrand`/`fastrandn` linknames (4), `syscall.runtimeClearenv` (1), `runtime.GOROOT()` (`winbatch.go`) | 10 srctest + 18 corpus (5 ★) | facade `runtime` + intrinsics (BUG-0017's list on the new path) | 3–5 | P0 |
| G3 ★ | **finalizers**: `SetFinalizer` never runs on the new path (`deferfin`, `mallocfin`, `tinyfin`, `lc32_finalizer_wakes_main`) or a finalizer panic is not fatal (`lc33`) | 3 srctest + 2 corpus (2 ★) | finalizer queue on the new value model (BUG-0016's fix done for `graalgo.go`) | 2–4 | P1 |
| G4 ★ | **new-path node bugs found by the run**: `ClassCastException` `Iface`↔`RawIface` (`switch.go`, `testing/quick`), `Complex` (`shift3.go`); `len` of a constant-length array pointer skips the call (`range.go`, `const4.go`: side effect elided); nil dereference at a large offset not trapped (`nilptr.go`) and nil method value calls (`ddd.go`, `method.go`); `new(x)` (`newexpr.go`, BUG-0005 still open on the new path); `slicecap.go`; zero-size allocation identity (`zerosize.go`); false `checkptr` traps (`inline_literal.go`, `unsafebuiltins.go`, `03c_uintptr2ptr_trap.go`: gc traps only under `-d=checkptr`); `runtime.Caller` skip through inlined frames (`inline_caller.go`); traceback `main.main(...)` vs `main.main()` (`27_panic_trace`, `maymorestack.go`); generics: nil array length NPE (`boundmethod.go`, `interfacearg.go`), local generic type definitions (`issue54456.go`, `nested.go`); timer channel `len`/`cap` must be 0 (go ≥ 1.23: `tm15`, `tc07`); `reflect.FuncOf`/`StructOf` method code pointers (`47_reflect_funcof_structof`, W9b) | 17 srctest + 5 typeparam + 5 corpus + 2 snapshot (v1 passes most) | one fix + regression program each, in `test/m1a/programs` or `test/m1r/programs` | 6–10 | P0 |
| G5 | **`go test` through the launcher (M2)**: `test-regression`'s 7 packages | 7 packages | M2; then `test-regression` runs on the launcher unchanged (`cmd/graalgo` = launcher) | in M2 | P1 (blocks D4) |
| G6 ✅ | **mutation oracle on the new path** (**closed** 2026-11-25, lane `m3-d3`) | – | [mutants.json](../../tools/mutation/mutants.json): 52 mutants over the `graalgo.go` nodes/values/runtime/build, the scheduler, `graalgo.syscall`, the `-toolexec` emitter and the `stdlib/` facade (slice bounds, int32 wrap, select bias, lost wakeups, map iteration, defer order, recover eligibility, string/byte aliasing, UTF-8, interface equality, checkptr, timer order, exit status, loop variables, evaluation order, constant folding, facade error text; plus the 23 of test/m1c and test/m1d's unrun `mutants.py`, 6 of which had rotted), each with kill programs of the per-push gates (snapshot-check, test-m1a/m1r/m1c/m1d/m1w13; a slice survivor re-runs them in full). Local run (dev host, 1 job, load 6–11): **50 killed, 2 justified survivors** (the two poller mutants: no per-push program reaches the poller, BUG-0080), 1056 s wall / 2860 s CPU. Tests added for 3 survivors: `test/m1r/programs/nondeterminism.go` (select and map-range randomness), `test/m1a/programs/loopvar_range.go`, `test/m1c/programs/goexit_timer.go`; found BUG-0080 (fd deadlines expire at once) | 2–3 | P1 |
| G7 | **syscall role table** (M4 scope): `getgroups` 115, `open` 2, `mmap` 9 (W8b), `kill` 62 (`sigchld.go`), `socket` 41 (`net`: 3 programs); `os/exec` spawn (`syscall.runtime_AfterFork`: 6 srctest + 1 corpus, GG-0064); `os` pipe hang (`08_pipe`) | 7 srctest + 9 corpus (3 ★) | W8b (M4) | in M4 | P2: accept as M4-deferred coverage (§5 decision 1) |
| G8 | **GC-observable behavior** (`stackobj`, `stackobj3`, `maplinear` timing): diverge on v1 too | 3 srctest | stay reason-coded (`backend-specific`, `timing: true`) | 0 | – |
| G9 | `GOMAXPROCS` default follows `-XX:ActiveProcessorCount` (`lc20_gomaxprocs`), unnormalized `log` timestamps (`testing/iotest`) | 2 corpus | harness artifacts of this measurement: the gate runs without the CPU cap / with a normalizer | 0.5 | P2 |

**Total before D6: G1–G4 + G6 = 17–29 agent-days**, parallel in 3 lanes
(link floor + runtime surface; node bugs; finalizers + mutants). G5 is M2's
own work; G7 is M4's.

## 4. Deletion plan: ordered, independently landable steps

Each step lands green on `make pre-land` and leaves the tree consistent
(docs in the same commit, AGENTS.md §8). Savings are the measured or
baselined gate costs of [test/duration-baseline.json](../../test/duration-baseline.json)
(dev host) and [test/duration-baseline-ci.json](../../test/duration-baseline-ci.json)
(CI, warm); "est." marks an estimate.

| Step | What | Depends on | Per-push saving (dev wall / CPU; CI CPU) | Weekly / nightly saving |
|---|---|---|---|---|
| **D0** ✅ | **ADR-0017 stage 1** (accepted; **done** 2026-11-25, lane `m3-d0d1`): the archive docs `git mv`'d to [docs/archive/](../archive/) (601 renames; links and the archive skips of `check_doc_paths`/`check_gg_ids`/`check_ratchets`/`check_makefile` re-pointed), the lifecycle corpus's `normalize.py` → [test/lib/](../../test/lib/normalize.py) (6 callers); the proven-dead files of §6 deleted (`tools/fuzz`, the 9 topic `run_differential.sh`, the frontend probe) | quiet window | 0 | 0 |
| **D1** ✅ | **snapshot-check on the new path** (**done** 2026-11-25, lane `m3-d0d1`): [backends-newpath.json](../../test/differential/backends-newpath.json) — the backend of §1, each program built as gc's leg builds it ([newpath_build.sh](../../test/differential/newpath_build.sh), set up once by [newpath.sh](../../test/differential/newpath.sh)) — is the `snapshot-check` (and `make snapshot`) config, and the built programs run **batched** in one JVM, a fresh context each (the diffharness speaks batchrun.sh's protocol: `batchRun`, `test/differential/internal/harness/batch.go`; `GRAALGO_BATCH=off` = one JVM each, same 61 verdicts); `corpus-exclusions.yaml` re-triaged to the 2 measured divergences (`27_panic_trace` BUG-0011, `47_reflect_funcof_structof` BUG-0059, both G4; 6 v1 entries pruned as STALE), v1's register kept as `corpus-exclusions-v1.yaml` for the v1 mutation oracle only (G6/D3 retire it); `test-diffharness` dropped its `graalgo-sync-run` precondition; the diffharness default `-config` is `backends-newpath.json` (`srctest`/`typeparam` keep theirs until D2); `jvmcds.sh sync-env` left the recipe; tracebacks through std scrub the toolchain's GOROOT. Planted DRIFT (changed output, unlisted divergence), STALE and UNPINNED fail the gate | — | **−410 s / −631 s** (433 → 23 wall, 659 → 28 CPU, measured; one JVM per program would be 52 / 104); CI −795 s CPU est. (875 → ~80 baseline estimate) | mutation.yml's per-mutant gate gets ~6× cheaper once G6 lands |
| **D2** | **conformance and typeparam on the new path**: `run_conformance.sh` uses the new backend, `srctest-exclusions.yaml` re-triaged from §2.1 (39 stale entries deleted, 32 new reason-coded entries citing G1–G4/G7 ids); `TypeparamDifferentialTest` replaced by a `make test-typeparam` (planned) over `diffharness typeparam run` in the weekly job; `typeparam-exclusions.yaml` re-triaged | D1 | 0 (path-filtered job: conformance 234 s wall at 1 job on the new path) | weekly: TypeparamDifferentialTest's share of the 437 s v1 classes |
| **D3** ✅ | **re-point or delete the v1-driven tools** (**done** 2026-11-25, lane `m3-d3`): `test-jit`'s `peguard` on the new path (bundle runner, planted graph-size violation; `pe-check` and `run_pe_check.sh` deleted); `make bench` = `bench-suite` (`test/bench/run.sh` deleted); `fast-run`/`fast-watch` and truffle `run`/`run-sched`/`smoke`/`pin-audit` deleted; fuzz-soak 4a on the new path's generator, v1's fuzz-soak tool deleted; the 76 wave runners, the `run_wave*.sh` and `run_stdlib_gate.sh` deleted, `stdlib-gate.yml` → `make stdlib-gate` with [std-register.tsv](../../test/corpus/std-register.tsv) (BUG-0033); `tools/godebug-diff` on bundles; G6's mutant set in `tools/mutation`, `corpus-exclusions-v1.yaml` deleted | D1 | test-jit ~30 → ~8 s wall (measured 7 s; v1's frontend JSON build gone); `check-repo` +~1 s (mutant-set validation, `test_std_gate.py`) | fuzz-soak 4a's runner time unchanged (deadline-bound), now on the new path; mutation.yml 1056 s wall locally at 1 job (CI: 4 jobs) |
| **D4** | **`cmd/graalgo` = the launcher** (ADR-0017 stage 2 head): `internal/toolexec/cmd/graalgo-launcher` built as `graalgo`; `test-regression` then runs through it unchanged; `test/e2e` re-pointed; delete `internal/driver`, `internal/vet` | **G5 (M2 `go test`)** | test-regression 126 s / 169 s → ~60 s CPU est. (no gofrontend, bundle JVM) | – |
| **D5** | **cut the shared code loose from v1** (RECOVERY "Layering debt" 1–3): v1 halves of `GoLanguage`/`GoContext`/`GoProgramBuilder`/`GoProgramRootNode`, `graalgo.runtime.Intrinsics`, the runtime helpers' v1 adapters, `SchedulerCore`'s `values` types; retarget the 12 shared-runtime tests of §2.2; `JT_RUNTIME_CP` and `java-test-classes` without interop; `layering_allowlist.txt` empty | D1 | pin smoke ~−20 s CPU est. (3 v1 classes) | `GcDifferentialTest`, `GoDebugTest` retargeted |
| **D6** | **the delete** (toolexec-design §9.1 in full): `internal/frontend`, `internal/backend/interop`, `graalgo.frontend`/`frontend.v2`/`values`/`sched.launcher`/`launcher.Main`, both v1 launchers, `tools/gate1`, the v1 Makefile targets and `generate` steps, v1 Java tests and quarantine lines, `check_mr_table_subset.py`, the v1 halves of `check` (gofrontend `build.sh check`, interop `check` — **moving** its stdlib-excl validate gate to the root `check` first), `check_no_bridges.py`'s grandfather list and the bridge inventory entries, `check_offheap.py`'s `GRANDFATHERED`, `check_duplicate_fqn.py`'s `interop` module, the `internal/frontend/go/` skips in `check_doc_paths.py`/`check_makefile.py`/`check_ratchets.py`, `ratchet_budget.json` regenerated, `large_files_allowlist.txt` entries, the duration-baseline rows | **G1–G4** (or §5 decision 2), D1–D5 | test-p3frontend −60 s / −80 s, test-v2 −1 s, `generate` −(gofrontend + 18 synth; ~25 s CPU cold on CI), `build` −interop javac (~15 s CPU est.), `check` −(gofrontend suite 1.4 s cached / 47 s uncached, mr-table, interop check), test-java-fast −~15 s CPU est. (21 v1 classes) | weekly: the remaining v1 slow classes (`MainTest`, `PanicCorpusTest`, `GenericsCorpusTest`, `VarsCorpusTest`, `BuiltinsCorpusTest`, `LinknameEndToEndTest`, `RangeConcurrentRegressionTest` with its 8 GB heap): together with D2 the 437 s |
| **D7** | docs: MAP (no legacy section), REPO_STRUCTURE, AGENTS.md §2/§6, test-profile.md, toolexec-design §9.1 marked done, bugs.md (BUG-0001/0002/0003/0012/0014/0015/0032/0036/0051/0053 closed with the code they describe, the rest re-stated on the new path); then ADR-0017 stages 3a–3c as their own lanes | D6 | 0 | 0 |

**Total per-push saving, D1–D6:** about −550 s wall and −900 s CPU on
the dev host out of `make pre-land`'s ~950 s / ~1700 s (est. ~50 % of the
CPU), and the snapshot-check shard stops being CI's floor (CI −725 s CPU
from D1 alone). Weekly: −437 s of the 608 s full suite, minus what the
replacement typeparam run costs (162 s wall at 1 job, measured).

## 5. Decisions for the owner

1. **Accept G7 (syscalls `open`/`mmap`/`getgroups`/`kill`/`socket`, `os/exec`
   spawn) as M4-deferred coverage at M3.** v1 passes 3 of those corpus
   programs; the new path's role table and spawn helper are M4 work
   (W8b, GG-0064). Proposed: yes, with reason-coded register entries citing
   the W8b/GG-0064 bug ids.
2. **Gate D6 on G1–G4 closed, or on "≥ v1's pass rate" only.** The pass
   rate is already met (§0); the per-program criterion of toolexec-design
   §8 ("zero regressions vs the v1 baseline per program") is not (35 + 32 + 1 +
   1 programs). Proposed: close G1, G2 and G4 (they are P0 for the north
   star anyway), and accept G3 if finalizers slip, with entries.
3. **test-regression before M2.** Either D4 waits for M2's `go test`, or
   the 7 packages are rewritten as `package main` programs for `test-m1r`
   now and `test-regression` is deleted at D6. Proposed: wait (G5 is M2's
   own gate).

## 6. Trivially dead files (proven unused; deleted in D0)

| Path | Evidence |
|---|---|
| `tools/fuzz` (deleted) | no Makefile target, workflow or script calls it; the only reference outside it is a `.gitignore` line (removed with it) |
| the 9 topic `run_differential.sh` of [test/corpus/](../../test/corpus/) (`atomic`, `fastrand`, `iface`, `lifecycle`, `map`, `slice`, `sync`, `timectx`, `traceback`) | `git grep run_differential.sh` outside test/corpus matches only docs; the `.go` programs stay (`test-m1a` reads them) |
| `internal/frontend/tools/probe/main.go` (deleted) | zero references outside docs |

`tools/stdlib-linkdiff` and `tools/stdlib-vendor` have no caller either, but
WP-UPG1 may reuse them (ADR-0017): kept until that lane decides.

## Decisions (coordinator, 2026-11-25)

1. **G7 (syscalls, `net`, `os/exec` spawn) is deferred to M4** and accepted
   as coverage dropped at M3: the affected programs get reason-coded
   `deferred-parity` entries citing their BUG ids, never silent removal.
2. **D6 (the §9.1 delete) is gated on G1–G4 closed**, not on the pass
   rate alone: zero per-program regressions vs v1 except the ones G5 (M2)
   and G7 (M4) own, each listed with its id. D0–D5 may land as soon as
   they are green, in order.
3. **test-regression's 7 packages become `package main` programs now**
   where a package is a self-contained repro (cheap, keeps the coverage on
   the new path before D4); the rest wait for M2's `go test` launcher.
