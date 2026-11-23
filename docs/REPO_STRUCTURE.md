# Repository structure

> **Looking for where to make a change? Start at [MAP.md](MAP.md)** (task →
> place, data flow, legacy list, names). This file is the full annotated tree.

graalgo is organized by **function**, not by research phase. New code never
gets a phase name; the remaining phase-named paths are legacy: the archived
[docs/arch/phases/](arch/phases/) records, the frozen
[test/corpus/phase1/](../test/corpus/phase1/) and
[test/corpus/phase2/](../test/corpus/phase2/) corpora, and the v1
`p3*` Java packages and [p3frontend/](../internal/frontend/p3frontend/)
module, all retired with the v1 pipeline at M3. What graalgo is and how to run it lives in
[README.md](../README.md); how agents work in this repo lives in
[AGENTS.md](../AGENTS.md). This document is the map of where things live and
why, plus the [old → new mapping](#old--new-mapping) for contributors from
the research era.

Related: [Makefile](../Makefile) (root build entry point),
[.github/workflows/](../.github/workflows/) (CI).

## Status at a glance

**new** = the `-toolexec` product path; **shared** = used by both paths,
stays after M3; **v1** = deleted at M3
([toolexec-design.md §9.1](arch/toolexec-design.md#91-m3-delete-inventory-w11));
**v1-driven** = not deleted, but runs programs through the v1 launchers
today and must be re-pointed at M3
([ADR-0017](dev/adr/ADR-0017-repo-layout.md), proposed). Each listed
directory's README repeats its status.

| Status | Directories |
|---|---|
| new | [internal/toolexec/](../internal/toolexec/README.md), [internal/backend/gir/](../internal/backend/gir/README.md), `graalgo.go.*` + `graalgo.syscall` in [internal/backend/truffle/](../internal/backend/truffle/README.md), [graalgo-bundle-run](../internal/backend/graalgo-bundle-run), [stdlib/](../stdlib/README.md), [test/m1a/](../test/m1a/README.md), [test/m1r/](../test/m1r/README.md), [test/m1c/](../test/m1c/README.md), [test/m1w13/](../test/m1w13/README.md), [test/imgcheck/](../test/imgcheck/), [test/fuzz/](../test/fuzz/README.md), [test/bench/suite/](../test/bench/suite/), [test/bench/realistic/](../test/bench/realistic/README.md) |
| shared | the runtime packages of [internal/backend/truffle/](../internal/backend/truffle/README.md) (`sched`, `runtime`, `safepoint`, `jfr`, `language`), [test/corpus/](../test/corpus/README.md), [test/lib/](../test/lib/), [test/java/](../test/java/), [test/snapshots/](../test/snapshots/README.md), [tools/req/](../tools/req/README.md), [tools/torture/](../tools/torture/), [requirements/](../requirements/), [.github/](../.github/) |
| v1 | [cmd/graalgo/](../cmd/graalgo/README.md) (becomes the launcher), [internal/frontend/](../internal/frontend/README.md), [internal/driver/](../internal/driver/), [internal/vet/](../internal/vet/README.md), [internal/backend/interop/](../internal/backend/interop/README.md), `graalgo.{frontend,values,sched.launcher}` + `launcher.Main` in truffle, [graalgo-run](../internal/backend/graalgo-run), [graalgo-sync-run](../internal/backend/graalgo-sync-run), [tools/gate1/](../tools/gate1/README.md) |
| v1-driven | [test/differential/](../test/differential/README.md) (`snapshot-check`), [test/regression/](../test/regression/), [test/e2e/](../test/e2e/README.md), [test/jit/](../test/jit/), `make bench`, [tools/](../tools/README.md) `fuzz`, `fuzz-soak`, `mutation`, `godebug-diff`, `run_stdlib_gate.sh`, `make fast-run` |
| history | [docs/arch/](arch/) `phases/`, `reviews/`, `spikes/`, `research/`, FEASIBILITY, PHASES, frontend-v1/v2; [docs/spec/](spec/); [research/](../research/); [test/spikes/](../test/spikes/) |

## Directory tree

- [README.md](../README.md) — what graalgo is, quickstart (drop-in `go` via
  `alias go=graalgo`).
- [AGENTS.md](../AGENTS.md) — operating manual for agents working in this
  repo.
- [Makefile](../Makefile) — root build entry point. `make all` regenerates
  all generated artifacts and runs the per-push suite; CI then runs
  `git diff --exit-code` — drift fails the build.
- [cmd/](../cmd/)
  - [graalgo/](../cmd/graalgo/) — the **v1** driver: a custom
    5-command reimplementation (`build`, `run`, `test`, `vet`, `fmt`) over
    the v1 JSON pipeline. Pending replacement at M3 by the `-toolexec`
    launcher ([internal/toolexec/cmd/graalgo-launcher/](../internal/toolexec/cmd/graalgo-launcher/),
    SPEC GG-0011).
- [internal/](../internal/) — private implementation. Nothing outside the
  module imports it (Go `internal` convention); everything user-facing flows
  through [cmd/graalgo/](../cmd/graalgo/).
  - [toolexec/](../internal/toolexec/) — separate module (own `go.mod`):
    the `-toolexec` wrapper of the recovery's per-package compile/link path
    ([toolexec-design.md](arch/toolexec-design.md)), currently a walking
    skeleton. [cmd/graalgo-tool/](../internal/toolexec/cmd/graalgo-tool/)
    is the wrapper `go build -toolexec` runs (shadow gc compile + IR archive
    member, link to stub + bundle, `-V=full` identity, response files,
    `-chain`); [cmd/graalgo-launcher/](../internal/toolexec/cmd/graalgo-launcher/)
    is the thin `graalgo` launcher that execs stock `go` with graalgo-tool
    injected (replaces [cmd/graalgo/](../cmd/graalgo/) at the M3 cutover);
    [cmd/graalgo-stub/](../internal/toolexec/cmd/graalgo-stub/) is the
    launcher stub prepended to linked programs;
    [argv/](../internal/toolexec/argv/) holds response files, cmd/go's
    quoted fields and the graalgo-tool command line;
    [launcher/](../internal/toolexec/launcher/) composes the launcher's
    environment and argv; [release/](../internal/toolexec/release/) names
    the tracked Go release;
    [ir/](../internal/toolexec/ir/) is the GIR container: the schema
    ([schema.txt](../internal/toolexec/ir/schema.txt)), its generator
    ([schema/](../internal/toolexec/ir/schema/), [gen/](../internal/toolexec/ir/gen/)),
    the generated Go codec, canonical type keys, golden and malformed
    vectors ([testdata/](../internal/toolexec/ir/testdata/));
    [emit/](../internal/toolexec/emit/) lowers go/types-checked packages to
    GIR; [girinterp/](../internal/toolexec/girinterp/) is a test-only
    reference interpreter of GIR with the differential harness against gc
    (its programs in [testdata/progs/](../internal/toolexec/girinterp/testdata/progs/))
    and the emitter mutation test;
    [cmd/girstat/](../internal/toolexec/cmd/girstat/) measures emitter
    coverage and GIR size/speed over std;
    [archive/](../internal/toolexec/archive/) reads/extends gc package
    archives; [bundle/](../internal/toolexec/bundle/) is the program-bundle
    format (with the link's resolution table);
    [fork/](../internal/toolexec/fork/) applies the standard-library fork
    of [stdlib/](../stdlib/) to an upstream GOROOT (validation, strict
    patch application, `-overlay` file, assembled GOROOT, fork-size audit)
    and [cmd/graalgo-goroot/](../internal/toolexec/cmd/graalgo-goroot/) is
    its command (`make goroot`);
    [intrinsics/](../internal/toolexec/intrinsics/) is the intrinsic
    registry — [registry.txt](../internal/toolexec/intrinsics/registry.txt)
    is the single source of truth, from which
    [gen/](../internal/toolexec/intrinsics/gen/) generates the backend's
    Java dispatch table; [syscalls/](../internal/toolexec/syscalls/) is the
    syscall role table of the executor —
    [table.txt](../internal/toolexec/syscalls/table.txt), the generated Java
    table, and the R15 gate that checks every entry against the roles
    derived from the standard library's own call sites (`Scan`, `Check`);
    [link/](../internal/toolexec/link/) is link-time
    symbol resolution and reachability (toolexec-design §5.3).
    Tested by `make test-toolexec`.
  - [frontend/](../internal/frontend/) — Go source → IR. Moved here from
    `phases/phase1/frontend/` (code lane 2, 2026-10-13). The main Go module
    (own `go.mod`, `package main`) is the `gofrontend` tool:
    [main.go](../internal/frontend/main.go),
    [export.go](../internal/frontend/export.go) (AST → wire emission),
    [diskimport.go](../internal/frontend/diskimport.go) and
    [mr_disk.go](../internal/frontend/mr_disk.go) (real-stdlib disk imports),
    [mrdesugar_m5.go](../internal/frontend/mrdesugar_m5.go) (multi-result
    lowering), the [`synth_*.go`](../internal/frontend/) synthetic stdlib
    implementations, the `gen_synth_*.py` generators (with
    [gen_synth_helper/](../internal/frontend/gen_synth_helper/), the
    declaration extractor they call), the Go test suite and its fixtures in
    [testdata/](../internal/frontend/testdata/).
    Built by [build.sh](../internal/frontend/build.sh); see
    [README.md](../internal/frontend/README.md) and
    [VENDORING.md](../internal/frontend/VENDORING.md). Sibling trees in the
    same directory:
    - [p3frontend/](../internal/frontend/p3frontend/) — separate module (own
      `go.mod`): the phase-3 importer/loader
      ([loader.go](../internal/frontend/p3frontend/loader.go),
      [importer.go](../internal/frontend/p3frontend/importer.go),
      [flatten.go](../internal/frontend/p3frontend/flatten.go),
      [export.go](../internal/frontend/p3frontend/export.go),
      [asm.go](../internal/frontend/p3frontend/asm.go)) with its own test
      suite.
    - [go/](../internal/frontend/go/) — vendored pristine go1.27.1
      `src/go/*`; load-bearing via the `build.sh` overlay, intentional per
      [VENDORING.md](../internal/frontend/VENDORING.md).
    - [javatest/](../internal/frontend/javatest/) — `NodeFactoryTest.java`
      plus `golden_*`/`bad*` fixtures (Java wire-format tests living under
      the Go module dir).
    - [tools/](../internal/frontend/tools/) — `probe/` and
      `validate_frontend_v1.py` (wire-format validation).
    - [v2/](../internal/frontend/v2/) — separate module (own `go.mod`): the
      frontend v2 prototype, superseded by the toolexec GIR path
      ([frontend-v2.md](arch/frontend-v2.md) is historical) (parser → checker → emitter stages plus the
      Go→Java binary interface in
      [encode/](../internal/frontend/v2/encode/), decoded by
      [BinaryDecoder.java](../internal/backend/truffle/src/main/java/graalgo/frontend/v2/BinaryDecoder.java)
      in the Java backend; wire contract pinned by
      `TestEncodeIntContract`/`TestEncodeGoldenDoc` on the Go side and
      `BinaryDecoderTest` on the Java side; `make test-v2` runs its suite).
  - The IR is the `frontend/v1` JSON wire format: specified in
    [docs/arch/frontend-v1.md](arch/frontend-v1.md), produced by
    [gofrontend](../internal/frontend/) (validated by
    [internal/frontend/tools/validate_frontend_v1.py](../internal/frontend/tools/validate_frontend_v1.py)),
    consumed by `graalgo.frontend.NodeFactory` in the backend. There is no
    separate IR package — the format is the contract.
  - [backend/](../internal/backend/) — IR → Truffle nodes. Two Java
    modules, each with its own `Makefile` (plain `javac`, `-Werror`); root
    `make build` builds truffle, then interop against it.
    - [truffle/](../internal/backend/truffle/) — the core backend module:
      [src/main/java/](../internal/backend/truffle/src/main/java/) holds
      the Truffle AST nodes and node factories (`graalgo.frontend`), the
      value model (`graalgo.values`), and the Go runtime on the JVM
      (`graalgo.runtime` intrinsics, channels, panics, maps, timers, GC
      interop; `graalgo.sched` scheduler and its launcher; `graalgo.jfr`;
      `graalgo.safepoint`; `graalgo.launcher` — graalgo-run's `Main`), and
      the Truffle language registration
      [graalgo.language](../internal/backend/truffle/src/main/java/graalgo/language/)
      (`GoLanguage`, id `go`; `GoContext`): the product launcher
      (P3SyncMain) runs every program as `Context.eval` of the
      frontend/v1 wire document (truffle-design TR-1).
      Nodes and runtime call each other, so they compile in one javac
      round. Two test runners: `graalgo.test.Runner` (nodes, values,
      corpus differentials) and `graalgo.sched.SchedTestRunner`
      (scheduler/runtime). Until RECOVERY T0.D (2026-11-18) this was two
      modules (`truffle/` + `runtime/`) plus 8 phase-1 stub classes whose
      same-FQN replacements won by classpath order; the stubs are deleted
      and [check_duplicate_fqn.py](../.github/scripts/check_duplicate_fqn.py)
      forbids any FQN in two modules. The Truffle jars live in
      [lib/](../internal/backend/truffle/lib/); the launchers
      [graalgo-run](../internal/backend/graalgo-run) and
      [graalgo-sync-run](../internal/backend/graalgo-sync-run) sit alongside.
      The **new node package** (toolexec W4, decision D12) lives in the same
      module under
      [graalgo/go/](../internal/backend/truffle/src/main/java/graalgo/go/)
      and shares only the runtime library with v1:
      [loader/](../internal/backend/truffle/src/main/java/graalgo/go/loader/)
      (bundle, package and lazily decoded function units, generic
      instantiation),
      [intrinsics/](../internal/backend/truffle/src/main/java/graalgo/go/intrinsics/)
      (the Java implementations of registered Go symbols and the generated
      dispatch table `IntrinsicTable.java`),
      [types/](../internal/backend/truffle/src/main/java/graalgo/go/types/)
      (type descriptors interned by canonical key, flat layouts, method
      sets, lazy itabs),
      [values/](../internal/backend/truffle/src/main/java/graalgo/go/values/)
      (the flat value model of SPEC §3.4: `Flat`, `Ptr`, `Slice`, `GoMap`,
      `Iface`, `Closure`),
      [nodes/](../internal/backend/truffle/src/main/java/graalgo/go/nodes/)
      (typed node families, memory locations, DSL arithmetic, calls with
      inline caches, open-coded defers),
      [build/](../internal/backend/truffle/src/main/java/graalgo/go/build/)
      (the node factory over decoded GIR) and
      [runtime/](../internal/backend/truffle/src/main/java/graalgo/go/runtime/)
      (panics, goroutine state, the adapter onto the scheduler);
      [graalgo/syscall/](../internal/backend/truffle/src/main/java/graalgo/syscall/)
      is the syscall executor (Panama downcalls to libc `syscall(2)` through
      the generated role table; the only package the off-heap gate allows to
      use `java.lang.foreign`). `GoLanguage`
      parses a linked bundle (`application/x-graalgo-bundle`) with it; the
      runner of linked executables is
      [graalgo-bundle-run](../internal/backend/graalgo-bundle-run)
      (`graalgo.launcher.BundleMain`). Design notes:
      [truffle-design.md §15](arch/truffle-design.md).
      [src/test/java/graalgo/conventions/](../internal/backend/truffle/src/test/java/graalgo/conventions/)
      is the class-file checker of the Truffle coding conventions
      (`NodeConventionsTest`, its planted-violation `fixtures/` and the
      shrink-only `allowlist.txt`; [truffle-design.md §16](arch/truffle-design.md)).
    - [gir/](../internal/backend/gir/) — the Java GIR decoder/encoder
      (JDK-only, own [Makefile](../internal/backend/gir/Makefile));
      `Gir.java`/`GirCodec.java` are generated from
      [schema.txt](../internal/toolexec/ir/schema.txt). Tested by
      `make test-gir`. Its sources are also compiled into the truffle
      module, whose bundle loader (`graalgo.go.loader`) consumes them.
    - [fast-cycle/](../internal/backend/fast-cycle/) — fast debug cycle:
      [FastCycleDaemon.java](../internal/backend/fast-cycle/FastCycleDaemon.java)
      plus the [`fast-cycle`](../internal/backend/fast-cycle/fast-cycle)
      wrapper script; wired into the root Makefile `fast-*` targets
      (`fast-build`, `fast-run`, `fast-test`, `fast-watch`). See
      [README.md](../internal/backend/fast-cycle/README.md).
    - [interop/](../internal/backend/interop/) — Go↔Java interop and
      isolates (also the natural home of Phase 5's privileged runtime
      surface):
      [src/main/java/](../internal/backend/interop/src/main/java/) holds
      phase-3 `graalgo.{iface,p3chan,p3crypto,p3link,p3math,p3net,p3os,
      p3reflect,p3sync,p3time,p3tls,p3unsafe,p3x509}` (stdlib bindings) plus
      [`graalgo.rtstack`](../internal/backend/interop/src/main/java/graalgo/rtstack/)
      (the synthetic `runtime` package's stack-inspection intrinsics:
      `Callers`/`Caller`/`CallersFrames`/`FuncForPC` and the
      `Frames`/`Func` methods) plus
      [`graalgo.rtmemstats`](../internal/backend/interop/src/main/java/graalgo/rtmemstats/)
      (the synthetic `runtime` package's MemStats-family intrinsics:
      `ReadMemStats`/`GOROOT`/`Version`/`Stack`, implemented against JVM
      telemetry) plus
      [`graalgo.p3log`](../internal/backend/interop/src/main/java/graalgo/p3log/)
      (shared diagnostic logging for launch-time warnings; backed by
      `java.util.logging` — raw `System.err` printing is rejected by the
      interop `check` gate).
  - [driver/](../internal/driver/) — artifact emission for `graalgo build`:
    takes the frontend-validated wire JSON and produces a real executable
    binary that embeds it
    ([artifact.go](../internal/driver/artifact.go),
    [artifact_test.go](../internal/driver/artifact_test.go)).
  - [vet/](../internal/vet/) — (v1 driver) static analyzers for `graalgo vet`:
    `go vet`-style checks (printf, unreachable, copylocks, etc.) implemented
    against the parsed AST and type information.
- [stdlib/](../stdlib/) — Go standard library support.
  - Synthetic package implementations live in
    [internal/frontend/](../internal/frontend/) (`synth_*.go`, `package main`
    — part of the `gofrontend` binary, not separable packages). Each one
    documents **why** it is synthetic and **what unblocks the real one**.
    The set is frozen (no new `synth_*.go` may be added) and is deleted
    wholesale at M3 with the v1 frontend (toolexec-design D11); see
    [bridge-inventory.md](arch/bridge-inventory.md).
    The 18 generator-produced `synth_*.go` are regenerated by
    [`make gen-synth`](../Makefile) via
    [`internal/frontend/run_gen_synth.py`](../internal/frontend/run_gen_synth.py)
    from the 10 [`gen_synth_*.py`](../internal/frontend/) generators
    (gc 1.27.1 sources, version-pinned); six files additionally apply a
    frontend-constraint adaptation patch from
    [`internal/frontend/gen_synth_adapt/`](../internal/frontend/gen_synth_adapt/).
    The full generator→output mapping lives in the `GENERATED_SYNTH` block in
    the [`Makefile`](../Makefile); CI fails on any drift.
  - [fork/](../stdlib/fork/) — the *replaced* packages of the `-toolexec`
    path (toolexec-design D8, §5.1): one complete directory per import
    path, taking the place of upstream's.
    [fork/runtime/](../stdlib/fork/runtime/) is the facade `runtime` (the
    identifiers std names outside the runtime family, declarations and
    documentation upstream's, functions without bodies resolved by the
    intrinsic registry); [fork/runtime/race/](../stdlib/fork/runtime/race/)
    is the empty package a `GRAALGO_RACE=ignore` build links;
    [fork/internal/runtime/maps/](../stdlib/fork/internal/runtime/maps/) is
    the facade of the Swiss-table package (reflect's map iterator over the
    backend's map object).
  - [patches/](../stdlib/patches/) — minimal diffs against upstream
    [go/](https://go.dev/dl/) sources, as
    `patches/<import path>/<file>.patch`:
    [patches/reflect/](../stdlib/patches/reflect/) (the call path and the
    type constructors, which gc's ABI frames, itabs and self-made
    descriptors cannot serve; toolexec-design §13) and
    [patches/internal/abi/](../stdlib/patches/internal/abi/) (the body of a
    generic compiler intrinsic). Which packages are replaced or patched is exactly
    what these two directories contain: no tool has a package list, and
    [internal/toolexec/fork/](../internal/toolexec/fork/) refuses a fork
    that does not apply exactly to the tracked release.
- [test/](../test/) — organized by what each suite proves, not by which phase
  wrote it.
  - [corpus/](../test/corpus/) — differential corpus programs (moved here
    from `phases/phase3/corpus/`, 2026-10-13).
    `phase1/`, `phase2/` (frozen v1-era corpora), `wave1/`–`wave3/`
    (stdlib wave corpora, see [run_stdlib_gate.sh](../tools/run_stdlib_gate.sh)),
    and topic corpora (`atomic/`, `iface/`, `map/`, `slice/`, `sync/`,
    `timectx/`, `traceback/`, `fastrand/`).
    - [lifecycle/](../test/corpus/lifecycle/) — program lifecycle and
      panic model vs gc (exit on main return, fatal goroutine panics,
      deadlock detection, panic chains, runtime error values, GOMAXPROCS,
      slot-releasing pipe reads); runner
      [run_differential.sh](../test/corpus/lifecycle/run_differential.sh).
  - [differential/](../test/differential/) — the differential harness: run
    each corpus program under graalgo and under `gc`, byte-compare outputs.
    Moved here from `tools/diffharness/` (2026-10-13). Reviewed,
    reason-coded expected divergences (SPEC §5.6):
    [corpus-exclusions.yaml](../test/differential/corpus-exclusions.yaml)
    (corpus) and
    [srctest-exclusions.yaml](../test/differential/srctest-exclusions.yaml)
    (GOROOT/test, layer 2); listed tests still run and fail when stale.
    Layout: [cmd/diffharness/](../test/differential/cmd/diffharness/) (the
    CLI: `snapshot`, `srctest`, `typeparam` modes),
    [internal/](../test/differential/internal/) (`harness`, `srctest`,
    `typeparam`, `mutate` packages),
    [fuzz/](../test/differential/fuzz/) (native `testing.F` differential
    fuzzing + replay), [testdata/](../test/differential/testdata/) (harness
    self-test fixtures), [corpus/](../test/differential/corpus/) (the
    snapshot corpus).
  - [lib/](../test/lib/) — [infra.sh](../test/lib/infra.sh), the shared
    exit-code contract of the shell differential runners (0 pass,
    1 divergence, 2 usage, 3 infrastructure error);
    [toolexec.sh](../test/lib/toolexec.sh) builds graalgo-tool, the stub
    and the standard-library fork overlay for runners of the `-toolexec`
    path; [gcoracle.sh](../test/lib/gcoracle.sh) replays their gc leg from
    a content-addressed cache; [jvmcds.sh](../test/lib/jvmcds.sh) gives
    their runner JVMs a class-data-sharing archive;
    [gates.py](../test/lib/gates.py) runs the gates timed (`make
    test-times`, `make pre-land`, CI) and ratchets their CPU time against
    [duration-baseline.json](../test/duration-baseline.json)
    ([test-profile.md](dev/test-profile.md)); `test_*.py` are their
    self-tests (`make check`).
  - [m1r/](../test/m1r/) — the differential gate of the standard library
    through the `-toolexec` path (`make test-m1r`, ~15 s, per-push in `make all`):
    [run.sh](../test/m1r/run.sh) builds each program of
    [programs/](../test/m1r/programs/) against graalgo's fork with
    `go build -toolexec graalgo-tool`, runs it on the new node package and
    compares stdout, normalized stderr and exit status with gc.
  - [m1c/](../test/m1c/) — the M1c gate (`make test-m1c`, ~15 s, per-push in `make all`):
    `sync`'s semaphores and notify lists, `internal/bytealg`, `time` and the
    syscall executor, with [run.sh](../test/m1c/run.sh) following the m1r
    conventions; [programs/](../test/m1c/programs/) marked
    `//m1c:requires <probe>` are PENDING until the program of that name in
    [probes/](../test/m1c/probes/) matches gc (another lane's piece landed);
    [mutants.sh](../test/m1c/mutants.sh) plants the bugs listed in
    [mutants.py](../test/m1c/mutants.py) and requires the gate to fail.
  - [m1d/](../test/m1d/) — the M1d gate (`make test-m1d`, ~15 s, per-push in `make all`):
    hello world and a verb matrix with the real `fmt`, `os` over
    `internal/poll` and the syscall role table, the reflect probe, and the
    packages on top (`encoding/json`, `regexp`, `text/template`, …);
    [run.sh](../test/m1d/run.sh) follows the m1r conventions and reads
    `// args:`, `// env:` and `// stdin:` directives of a program;
    [mutants.sh](../test/m1d/mutants.sh) plants the bugs listed in
    [mutants.py](../test/m1d/mutants.py) and requires the gate to fail.
  - [fuzz/](../test/fuzz/) — the differential fuzzer of the new path
    (`make fuzz-newpath N= SEED=`, nightly
    [fuzz-newpath.yml](../.github/workflows/fuzz-newpath.yml), not per-push):
    [run.py](../test/fuzz/run.py) builds each program with gc and through
    graalgo-tool ([setup.sh](../test/fuzz/setup.sh)), classifies it (match,
    diverge, gg-crash, gg-build, timeout, bad-program), dedups signatures and
    delta-debugs failures (`--minimize`); [gen.py](../test/fuzz/gen.py)
    generates seeded programs (println output, generics, interfaces,
    closures, runtime panics under recover); [corpus/](../test/fuzz/corpus/)
    holds hand-written adversarial programs by area (numeric, strslice,
    lang, control, stdlib); [repro/](../test/fuzz/repro/) the minimal
    reproducers of open bugs, listed with their pinned signatures in
    [known.txt](../test/fuzz/known.txt). A fixed bug's reproducer moves to
    the gate it belongs to (test/m1a, test/m1r, ...).
  - [java/](../test/java/) — [ClassListRunner.java](../test/java/ClassListRunner.java),
    runs named backend unit-test classes with per-test timing
    (`make test-java-fast` / `test-java-slow`);
    [check_lists.py](../test/java/check_lists.py) keeps the Makefile lists,
    the module runners and every `*Test.java` consistent;
    [quarantine.txt](../test/java/quarantine.txt) lists known-failing slow
    classes, judged by [quarantine_verdict.py](../test/java/quarantine_verdict.py).
  - [e2e/](../test/e2e/) — end-to-end: real published Go modules compiled
    as-published (e.g. `github.com/google/uuid`), output matching `go run`.
  - [snapshots/](../test/snapshots/) — committed behavior snapshots of
    **gc** (`go1.27.1.json`); `make snapshot-check` fails when graalgo
    diverges from gc outside the reviewed expected-divergence list. See
    [test/snapshots/README.md](../test/snapshots/README.md).
  - [regression/](../test/regression/) — regression tests for fixed
    package-level bugs, one Go test package per bug:
    [pkg031/](../test/regression/pkg031/) (strings.Repeat hang),
    [bufio-split/](../test/regression/bufio-split/),
    [funcname/](../test/regression/funcname/),
    [p0-clear-crash/](../test/regression/p0-clear-crash/),
    [tls-wrap-hang/](../test/regression/tls-wrap-hang/),
    [chan-semantics/](../test/regression/chan-semantics/) (named channel
    types, channel equality, send-to-interface boxing);
    run through the graalgo driver + backend via `make test-regression`
    (in `make all`, hence per-push CI).
  - [jit/](../test/jit/) — JIT regression guard:
    [run_pe_guard.sh](../test/jit/run_pe_guard.sh) runs
    [peguard/main.go](../test/jit/peguard/main.go) with traced, synchronous
    Truffle compilation and fails on any compilation failure (e.g. a partial
    evaluation bailout), on a hot function or OSR loop that never compiles,
    or on output that differs from gc's. `make test-jit` (part of
    `make test-ci`). [run_pe_check.sh](../test/jit/run_pe_check.sh)
    (`make pe-check`, TR-0) runs the whole benchmark set with
    `CompilationFailureAction=ExitVM` and counts bailouts (must be 0).
  - [imgcheck/](../test/imgcheck/) — the descriptor-image gate (`make
    test-imgcheck`, per-push; toolexec W9a): [main.go](../test/imgcheck/main.go)
    walks the type descriptors of the generated corpus
    ([gen.py](../test/imgcheck/gen.py) → [corpus.go](../test/imgcheck/corpus.go))
    as `internal/reflectlite` reads them and prints every fact;
    [run.sh](../test/imgcheck/run.sh) compares the run built by gc (where
    [oracle_gc.go](../test/imgcheck/oracle_gc.go) checks the walker against
    package `reflect`) with the run on the node package, then requires five
    seeded builder errors to be detected.
  - [m1w13/](../test/m1w13/) — the pointer-model gate (`make test-m1w13`,
    ~1.5 min, per-push in `make all`; toolexec W13, SPEC §3.4,
    GG-0072/GG-0062): [run.sh](../test/m1w13/run.sh) runs programs that use
    package `unsafe` through graalgo-tool on the node package against gc.
    [gen.py](../test/m1w13/gen.py) generates a corpus of struct types and
    from it the layout program whose gc output is
    [layout_corpus.txt](../test/m1w13/layout_corpus.txt) (the fixture of the
    backend's `LeafTableTest`) and the access program (loads and stores at
    every byte offset, per addressable kind);
    [programs/](../test/m1w13/programs/) holds unsafe idioms of real code,
    [checkptr/](../test/m1w13/checkptr/) programs gc's `-d=checkptr` stops
    (same fatal text), [restricted/](../test/m1w13/restricted/) programs gc
    runs and the flat value model stops
    ([limitations.md](arch/limitations.md)).
  - [m1a/](../test/m1a/) — the differential gate of toolexec milestone M1a
    (`make test-m1a`, ~2.5 min, per-push in `make all`): [run.sh](../test/m1a/run.sh)
    builds every corpus program that imports nothing with the stock
    `go build -toolexec graalgo-tool`, runs the linked bundle on the new
    node package and compares stdout, normalized stderr and exit status
    with gc. [transform.py](../test/m1a/transform.py) makes the phase-1
    programs print their `// observe:` variables (both legs; tested by
    [test_transform.py](../test/m1a/test_transform.py));
    [programs/](../test/m1a/programs/) adds programs for areas the corpus
    is thin on (open-coded and chained defers, Goexit, recover through
    wrappers, range-over-func, the flat value model, interfaces, generics,
    goroutines, bounds and conversion edge cases);
    [multipkg/](../test/m1a/multipkg/) holds multi-package modules.
    [defer_path.sh](../test/m1a/defer_path.sh) pins GG-0071's open-coding
    limit and runs the defer programs with every function forced onto the
    chain.
    [bench.sh](../test/m1a/bench.sh) runs the [bench/](../test/bench/)
    workloads on the new node package, rewritten by
    [bench.py](../test/m1a/bench.py) to need no standard library
    (`make bench-m1a`), and as the partial-evaluation gate of the new
    nodes (`make pe-check-m1a`, per-push: zero failed compilations).
  - [bench/](../test/bench/) — gc vs graalgo steady-state micro-benchmarks
    (fib, loops, slices, maps, structs, interface dispatch, string builder,
    channel ping-pong); [run.sh](../test/bench/run.sh) checks output
    equality with gc and prints best-round times (`make bench`; not a gate).
    [suite/](../test/bench/suite/) is the benchmark suite of
    [performance.md](arch/performance.md): 27 programs on a shared harness
    ([suite/h](../test/bench/suite/h/): `runtime.nanotime` by linkname,
    `println`), run by [suite.sh](../test/bench/suite.sh) /
    [suite.py](../test/bench/suite.py) under gc, gc+PGO and graalgo (new
    path) with results checked against gc, warmup curves, steady-state
    medians and JSON (`make bench-suite`; not a gate).
    [realistic/](../test/bench/realistic/README.md) holds whole-workload
    programs (stdlib pipelines, allocation tail latency, goroutine scaling)
    of [north-star.md](arch/north-star.md) on the same runner (`--dir`;
    peak RSS, CPU time, `STAT` lines): `make bench-realistic`, not a gate.
  - [spikes/abi/](../test/spikes/abi/) — prototypes behind
    [abi-spike.md](arch/abi-spike.md) (not part of any gate; each has a
    `run.sh` or is a `go run` tool):
    [unsafescan/](../test/spikes/abi/unsafescan/) inventories `unsafe` and
    descriptor-layout patterns in std from typed source;
    [reflectprobe/](../test/spikes/abi/reflectprobe/) measures how much of
    the reflect API agrees with gc under the current backend;
    [descview/](../test/spikes/abi/descview/) synthesizes gc-layout type
    descriptors in Java and has gc's own reflect judge them;
    [sysscan/](../test/spikes/abi/sysscan/) inventories the raw system
    calls std issues and types their arguments;
    [sysexec/](../test/spikes/abi/sysexec/) executes Go-shaped raw syscalls
    through a role table and one Panama downcall (compiled together with
    descview's Java sources).
- [docs/](../docs/)
  - [REPO_STRUCTURE.md](../docs/REPO_STRUCTURE.md) — this map.
  - [MAP.md](../docs/MAP.md) — the task-oriented entry point (task →
    place, data flow, legacy list, naming).
  - [arch/](../docs/arch/) — architecture documentation, consolidated from
    the research-era [SPEC.md](arch/SPEC.md), [PHASES.md](arch/PHASES.md), and
    [FEASIBILITY.md](arch/FEASIBILITY.md).
    [RECOVERY.md](arch/RECOVERY.md) is the 2026-11-17 recovery plan;
    [abi-spike.md](arch/abi-spike.md) reports the two de-risking prototypes
    of that design (verbatim reflect on descriptor views; the syscall table);
    [toolexec-design.md](arch/toolexec-design.md) is its track-T1 design for
    the per-package `-toolexec` compile/link path (tool protocol, IR,
    stdlib fork boundary, measured intrinsic surface, milestones).
    [traceback-normalization.md](arch/traceback-normalization.md) defines
    what "same caller frames and tracebacks as gc" compares (GG-0070).
  - [dev/](../docs/dev/) — environment ([ENV.md](dev/ENV.md)), the bug
    register cited by exclusions and quarantines ([bugs.md](dev/bugs.md)),
    and architecture decision records (ADRs; research-era ones moved here from
    [docs/dev/adr/](../docs/dev/adr/)).
  - [spec/](../docs/spec/) — language conformance notes (the research-era
    [spec/](spec/) content, curated).
- [tools/](../tools/) — developer tooling (not the product):
  [stdlib-fork/](../tools/stdlib-fork/),
  [stdlib-vendor/](../tools/stdlib-vendor/) (vendors a pinned GOROOT/src
  into `~/dev/graalgo-stdlib/<ver>/` and verifies it against
  [vendor-manifest.json](../tools/stdlib-fork/vendor-manifest.json)) and
  [stdlib-linkdiff/](../tools/stdlib-linkdiff/) (stdlib fork/linkname
  tooling), [run_stdlib_gate.sh](../tools/run_stdlib_gate.sh) and
  [stdlib-excl/](../tools/stdlib-excl/) (wave-corpus differential gates),
  [fuzz-soak/](../tools/fuzz-soak/) (long-running generator fuzzing:
  [gen/](../tools/fuzz-soak/gen/) program generator,
  [harness/](../tools/fuzz-soak/harness/) batch/diff/triage scripts,
  [gha/](../tools/fuzz-soak/gha/) GitHub Actions fan-out recipe,
  [findings/](../tools/fuzz-soak/findings/) recorded findings,
  [run_soak.sh](../tools/fuzz-soak/run_soak.sh) entry point used by
  [fuzz-soak.yml](../.github/workflows/fuzz-soak.yml)) and
  [fuzz/](../tools/fuzz/) (fuzzing),
  [torture/](../tools/torture/) (scheduler torture),
  [godebug-diff/](../tools/godebug-diff/),
  [mutation/](../tools/mutation/) (mutates graalgo on a scratch copy and
  requires the per-push differential gate to go red; `make mutation`,
  nightly [mutation.yml](../.github/workflows/mutation.yml)),
  [gate1/](../tools/gate1/) (`go test` trial harness; [shim/](../tools/gate1/shim/)
  is the minimal `testing.T` replacement its drivers import), and
  [req/](../tools/req/) (requirements CLI; `tools/req lint` runs in
  `make check`).
- [requirements/](../requirements/) — live requirements registry
  (`GG-*.yaml`); the [req](../tools/req/) CLI walks up to find it. Moves
  deferred (see the old → new mapping).
- [research/](../research/) — active research tracks:
  [phase5-opt-survey/](../research/phase5-opt-survey/) (Phase-5 work).
  Finished studies are archived under
  [docs/arch/research/](arch/research/).

> Migration complete (2026-10-14): the `phases/` directories have been
> removed from the tracked tree. All active code, docs, and tooling now
> live in the new layout. Historical phase documents are archived under
> [docs/arch/phases/](arch/phases/). Gitignored local build debris (e.g.
> stale `phases/*/out/` on a developer's disk) is not part of the repo and
> is safe to delete.

## Old → new mapping

For contributors from the research era. Old paths are historical references —
they are not links because the moved/deleted paths no longer exist at the
new locations.

| Today | Becomes |
|---|---|
| `phases/phase1/frontend/` | [internal/frontend/](../internal/frontend/) |
| `phases/phase1/src/main/java/{nodes,values,frontend}/` | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) |
| `phases/phase1/src/main/java/{runtime,launcher,safepoint}/` | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) (via the `runtime/` module, merged into truffle by T0.D) |
| the 8 same-FQN shadow classes (`graalgo/runtime/` × 7, `graalgo/safepoint/Safepoint`) | deleted by T0.D (2026-11-18); the phase-2 implementations are the only copies, in [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) |
| `phases/phase2/src/main/java/{sched,jfr,runtime,safepoint}/` | [internal/backend/truffle/src/main/java/](../internal/backend/truffle/src/main/java/) (scheduler keeps package `graalgo.sched`) |
| `phases/phase3/src/main/java/{iface,p3*}/` | [internal/backend/interop/src/main/java/](../internal/backend/interop/src/main/java/) |
| `phases/phase1/lib/` (Truffle jars) | [internal/backend/truffle/lib/](../internal/backend/truffle/lib/) |
| `phases/phase1/graalgo-run`, `phases/phase3/graalgo-sync-run` | [internal/backend/](../internal/backend/) |
| `phases/phase3/stdlib-fork/` + synthetics | [stdlib/](../stdlib/) |
| `phases/phase3/corpus/` | [test/corpus/](../test/corpus/) (landed 2026-10-13) |
| `tools/diffharness/` | [test/differential/](../test/differential/) (landed 2026-10-13) |
| `phases/phase3/src/p3frontend/` | [internal/frontend/p3frontend/](../internal/frontend/p3frontend/) (landed 2026-10-14) |
| `phases/phase3/stdlib-fork/` (tooling) | [tools/stdlib-fork/](../tools/stdlib-fork/) (landed 2026-10-14) |
| `phases/phase3/fuzzing/` | [tools/fuzz-soak/](../tools/fuzz-soak/) (landed 2026-10-14) |
| `phases/phase1/corpus/`, `phases/phase2/corpus/` | [test/corpus/phase1/](../test/corpus/phase1/), [test/corpus/phase2/](../test/corpus/phase2/) (landed 2026-10-14) |
| `phases/phase2/tools/torture/` | [tools/torture/](../tools/torture/) (landed 2026-10-14) |
| `modprobe/` | [test/e2e/](../test/e2e/) (landed 2026-10-14; harness, uuid cases, bug log) |
| `SPEC.md`, `PHASES.md`, `FEASIBILITY.md` | [docs/arch/](../docs/arch/) (one consolidated arch doc) |
| `adr/` | [docs/dev/adr/](../docs/dev/adr/) (landed 2026-10-14) |
| `spikes/` | [docs/arch/spikes/](../docs/arch/spikes/) (landed 2026-10-14; cited by the archived phase docs) |
| `reviews/` | [docs/arch/reviews/](../docs/arch/reviews/) (landed 2026-10-14; cited by the archived phase docs) |
| `research/` study tracks (`T1`–`T8`, OUTLINEs) | [docs/arch/research/](../docs/arch/research/) (landed 2026-10-14); `research/phase5-opt-survey/` stays — active Phase-5 work |
| `requirements/` | still at root — live registry, `tools/req/` walks up to find it; move deferred |
| `spec/` | removed 2026-10-14 — was outline drafts and rework scratch, not curated spec content (which lives in [docs/spec/](../docs/spec/)) |
| `hidden_files/` | removed — evidence lives in commit messages |
| `phases/*/out/`, `*.err`, compiled binaries | removed — build artifacts, covered by [.gitignore](../.gitignore) |
| `phases/` (all) | **removed 2026-10-14** — migration complete, zero tracked files remain |

## Principles

1. **Function, not phase.** Directory names describe what the code does, not
   which milestone produced it. Scaffolding is not architecture.
2. **One binary, one entry point.** Today [cmd/graalgo/](../cmd/graalgo/)
   (v1); from M3 the toolexec launcher. [internal/](../internal/) exists to
   serve it.
3. **No research debris ships.** Experiment logs, wave reports, scratch
   files → commit messages or deleted. If it does not serve the tool, it does
   not stay in the tree.
4. **Stdlib honesty.** Every synthetic package implementation (in
   [internal/frontend/](../internal/frontend/) as `synth_*.go`) names the root cause that keeps
   the real stdlib source from compiling, and what fixes it. A synthetic
   without that note is a placeholder and gets treated as one.
5. **Tests prove the tool.** [test/](../test/) is organized by what each
   suite proves (corpus programs, differential comparison, end-to-end
   modules), never by authorship.
6. **Every file/dir named in a doc is a link.** Relative paths, resolvable
   from the doc's own directory, mechanically checked by
   [.github/scripts/check_markdown_links.py](../.github/scripts/check_markdown_links.py).

## What's intentionally NOT in the repo

- **Build artifacts** — `out/` directories, `*.err` logs, compiled binaries,
  the gofrontend binary and `overlay.json`. Regenerated by `make all` from
  sources; covered by [.gitignore](../.gitignore).
- **Experiment logs (`hidden_files/`)** — per-run evidence belongs in commit
  messages (which are permanent and searchable), not in untracked scratch
  directories. Must never reappear (see [AGENTS.md](../AGENTS.md)).
- **Vendored copies of upstream Go** —
  [stdlib/patches/](../stdlib/patches/) holds *diffs* against upstream Go
  sources, never forks of the sources themselves. Upstream Go is never
  modified; fixes go into graalgo.
- **Research-era working notes** — wave run logs, per-commit scratch,
  speculative drafts. Consolidated into [docs/arch/](../docs/arch/) and
  [docs/dev/](../docs/dev/), or deleted.
- **Credentials and tokens** — CI uses connector-managed credentials; no
  secrets are ever committed (see [AGENTS.md](../AGENTS.md)).
- **Duplicated sources of truth** — validity rules, generated artifacts, and
  test baselines live in exactly one place each; docs link to the source,
  never copy it.
