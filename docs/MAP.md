# Where things are — start here

The first doc to open. Task → place, the data flow of the product path, what
is legacy, and what the names mean. The full annotated tree is
[REPO_STRUCTURE.md](REPO_STRUCTURE.md); the working contract is
[AGENTS.md](../AGENTS.md); the plan of record is
[RECOVERY.md](arch/RECOVERY.md); [SPEC.md](arch/SPEC.md) is authoritative.
The proposed post-M3 layout is [ADR-0017](dev/adr/ADR-0017-repo-layout.md).

**Two pipelines live in this tree.** The **new path** (`-toolexec`, GIR,
the `graalgo.go` node package) is the product being built. The **v1 path**
(JSON frontend, `synth_*.go`, `p3*` interop) still runs most legacy gates
and is **deleted at the M3 cutover** — never build on it.

## I want to…

| Task | Look here (new path) | Test it with |
|---|---|---|
| change language semantics | lowering: [internal/toolexec/emit/](../internal/toolexec/emit/); execution: [graalgo/go/nodes/](../internal/backend/truffle/src/main/java/graalgo/go/nodes/) built by [graalgo/go/build/NodeFactory.java](../internal/backend/truffle/src/main/java/graalgo/go/build/NodeFactory.java); semantics: [SPEC.md](arch/SPEC.md), [truffle-design.md](arch/truffle-design.md) §15 | `make test-m1a` (add a program to [test/m1a/programs/](../test/m1a/programs/)) |
| change the frontend (emitter) | [internal/toolexec/emit/](../internal/toolexec/emit/); its JVM-free oracle [girinterp/](../internal/toolexec/girinterp/) | `make test-toolexec`, then `make test-m1a` |
| change the GIR schema | [internal/toolexec/ir/schema.txt](../internal/toolexec/ir/schema.txt) → `make generate` regenerates the Go codec and the Java one in [internal/backend/gir/](../internal/backend/gir/) | `make test-toolexec test-gir` |
| change the node package / value model | [graalgo/go/](../internal/backend/truffle/src/main/java/graalgo/go/) (`loader`, `types`, `values`, `nodes`, `build`, `runtime`, `intrinsics`); package split in [truffle/README.md](../internal/backend/truffle/README.md) | `make build test-m1a test-m1w13 pe-check-m1a` |
| add an intrinsic (Go symbol implemented in Java) | declare it in [registry.txt](../internal/toolexec/intrinsics/registry.txt), implement in [graalgo/go/intrinsics/](../internal/backend/truffle/src/main/java/graalgo/go/intrinsics/), `make generate` | `make test-toolexec`, a program in [test/m1r/programs/](../test/m1r/programs/) |
| fix a std package's behavior | upstream Go is never modified: fix the backend (above), or the facade [stdlib/fork/runtime/](../stdlib/fork/runtime/), or as last resort a diff in [stdlib/patches/](../stdlib/patches/); see [stdlib/README.md](../stdlib/README.md) | `make test-m1r test-m1c` |
| add a syscall | [internal/toolexec/syscalls/table.txt](../internal/toolexec/syscalls/table.txt) (checked against std's call sites; a wrong entry is memory-unsafe, risk R15), executor [graalgo/syscall/](../internal/backend/truffle/src/main/java/graalgo/syscall/) | `cd internal/toolexec && go test ./syscalls`, `make test-m1c` |
| change the scheduler | [graalgo/sched/](../internal/backend/truffle/src/main/java/graalgo/sched/) (shared with v1) + the new path's adapter [graalgo/go/runtime/](../internal/backend/truffle/src/main/java/graalgo/go/runtime/) (`G`); [graalgo/safepoint/](../internal/backend/truffle/src/main/java/graalgo/safepoint/) | `make test-java-fast`, [tools/torture/](../tools/torture/), `make test-m1c` |
| change the launcher / tool protocol | [internal/toolexec/cmd/](../internal/toolexec/cmd/) (`graalgo-launcher`, `graalgo-tool`, `graalgo-stub`, each with a README), runner [graalgo-bundle-run](../internal/backend/graalgo-bundle-run) | `make test-toolexec` |
| add a differential program | new path: [test/m1a/programs/](../test/m1a/programs/) (no imports), [test/m1r/programs/](../test/m1r/programs/) (std), [test/m1c/programs/](../test/m1c/programs/) (sync/time/syscalls), [test/m1w13/programs/](../test/m1w13/programs/) (`unsafe`); a fuzz reproducer → [test/fuzz/repro/](../test/fuzz/repro/) | the dir's `run.sh` / make target |
| add a v1-era corpus program | [test/corpus/](../test/corpus/README.md) + [test/differential/](../test/differential/README.md) (gc snapshot in [test/snapshots/](../test/snapshots/)) | `make snapshot-check SNAPSHOT_PROGRAMS=…` |
| run the gates | [Makefile](../Makefile) header; `make check-repo` (docs-only, no JVM), `make check`, `make all` (per-push), `make pre-land` (before a lane reports done: `all` + external links + harness self-test + drift); [AGENTS.md](../AGENTS.md) §2 | — |
| benchmark | [test/bench/](../test/bench/README.md), [performance.md](arch/performance.md) | `make bench-suite`, `make bench-m1a` |
| fuzz | [test/fuzz/](../test/fuzz/README.md) (new path, nightly) | `make fuzz-newpath N= SEED=` |
| read/change requirements & SPEC | [SPEC.md](arch/SPEC.md); one YAML per requirement in [requirements/](../requirements/); CLI [tools/req/](../tools/req/README.md) | `cd tools/req && go run . lint` |
| find a decision | ADRs in [docs/dev/adr/](dev/adr/); the dated [decision log](arch/RECOVERY.md#decision-log); design decisions D1–D12 in [toolexec-design.md](arch/toolexec-design.md) §0 | — |
| find a known bug / divergence | [bugs.md](dev/bugs.md); exclusion registers in [test/differential/](../test/differential/README.md); Java quarantine [test/java/quarantine.txt](../test/java/quarantine.txt) | — |
| set up the toolchain | [ENV.md](dev/ENV.md) | — |

## Data flow of the new path

```
 user: graalgo build ./...            (alias go=graalgo)
   |
   v
 graalgo-launcher           internal/toolexec/cmd/graalgo-launcher  (+ launcher/, argv/, release/)
   | execs stock `go` with -toolexec graalgo-tool and the stdlib fork overlay
   v
 stock cmd/go  ── per package ──>  graalgo-tool compile     internal/toolexec/cmd/graalgo-tool
   |                                 1. stock gc compile (export data, build ID)
   |                                 2. go/types over the same files
   |                                 3. emit GIR              internal/toolexec/emit  -> ir (schema.txt)
   |                                 4. append GIR to archive internal/toolexec/archive
   |   stdlib source: upstream GOROOT + fork  stdlib/fork, stdlib/patches  via internal/toolexec/fork
   v
 graalgo-tool link          internal/toolexec/link (resolution vs intrinsics/registry.txt), bundle/
   | writes: graalgo-stub (internal/toolexec/cmd/graalgo-stub) + appended bundle = the executable
   v
 ./prog  -> stub execs  internal/backend/graalgo-bundle-run  -> graalgo.launcher.BundleMain
   v
 JVM (internal/backend/truffle):  GoLanguage (graalgo.language)
   -> GIR decode (internal/backend/gir sources) -> graalgo.go.loader -> graalgo.go.build.NodeFactory
   -> graalgo.go.{nodes,values,types,intrinsics,runtime}
   -> shared runtime: graalgo.sched, graalgo.runtime, graalgo.safepoint, graalgo.jfr, graalgo.syscall (Panama)
```

## Legacy (v1) — deleted at M3, do not build on it

Authoritative list: [toolexec-design.md §9.1](arch/toolexec-design.md#91-m3-delete-inventory-w11).

- [cmd/graalgo/](../cmd/graalgo/) (v1 driver; becomes the launcher at M3),
  [internal/driver/](../internal/driver/), [internal/vet/](../internal/vet/)
- [internal/frontend/](../internal/frontend/) whole: `gofrontend`, 140
  `synth_*.go`, generators, vendored [go/](../internal/frontend/go/),
  [p3frontend/](../internal/frontend/p3frontend/), [v2/](../internal/frontend/v2/)
- [internal/backend/interop/](../internal/backend/interop/README.md) whole
  (`graalgo.p3*`, `graalgo.iface`, `graalgo.rtstack`, `graalgo.rtmemstats`)
- in [internal/backend/truffle/](../internal/backend/truffle/README.md):
  `graalgo.frontend`, `graalgo.frontend.v2`, `graalgo.values`,
  `graalgo.sched.launcher`, `graalgo.launcher.Main`, the v1 halves of
  `graalgo.language` and `graalgo.runtime.Intrinsics`
- launchers [graalgo-run](../internal/backend/graalgo-run) and
  [graalgo-sync-run](../internal/backend/graalgo-sync-run);
  [tools/gate1/](../tools/gate1/)
- docs of the v1 format: [frontend-v1.md](arch/frontend-v1.md),
  [frontend-v2.md](arch/frontend-v2.md) (historical),
  [bridge-inventory.md](arch/bridge-inventory.md) (the frozen list)

Still **v1-driven** today but not in §9.1 (they must be re-pointed at M3,
see ADR-0017): `make snapshot-check` / [test/differential/](../test/differential/README.md),
[test/regression/](../test/regression/), [test/e2e/](../test/e2e/),
[test/jit/](../test/jit/), [test/corpus/lifecycle/](../test/corpus/lifecycle/),
`make bench` ([test/bench/run.sh](../test/bench/run.sh)),
[tools/fuzz/](../tools/fuzz/), [tools/fuzz-soak/](../tools/fuzz-soak/),
[tools/mutation/](../tools/mutation/), [tools/godebug-diff/](../tools/godebug-diff/),
[tools/run_stdlib_gate.sh](../tools/run_stdlib_gate.sh), `make fast-run`.

**The boundary is enforced.** [check_layering.py](../.github/scripts/check_layering.py)
(`make check-repo`) fails on any reference from `graalgo.go.*` (transitively)
into `graalgo.frontend`, `graalgo.values`, `graalgo.p3*`, `graalgo.iface`,
`graalgo.rtstack`, `graalgo.rtmemstats`, and on any `internal/toolexec`
import of `internal/{frontend,driver,vet}`; grandfathered edges are
shrink-only in [layering_allowlist.txt](../.github/scripts/layering_allowlist.txt).
[check_no_bridges.py](../.github/scripts/check_no_bridges.py) rejects any new
`synth_*.go` or interop stand-in.

## Names

| Name | Meaning | Defined in |
|---|---|---|
| M0–M5 | toolexec milestones; **M3** = cutover, v1 deleted | [toolexec-design.md](arch/toolexec-design.md) §8 |
| `test/m1a` | gate of M1a: the whole language, programs that import nothing | [test/m1a/](../test/m1a/README.md) |
| `test/m1r` | gate of M1r: the runtime floor (registry, pointers, descriptors, first syscalls) — upstream std source on the new path | [test/m1r/](../test/m1r/README.md) |
| `test/m1c` | gate of M1c: `sync` semaphores, `internal/bytealg`, `time`, syscall executor | [test/m1c/](../test/m1c/README.md) |
| `test/m1w13` | gate of work package W13, the pointer model (`unsafe`) | [test/m1w13/](../test/m1w13/README.md) |
| M1b, M1d | leaf std / `reflect`+`fmt` milestones (no test dir of their own yet) | toolexec-design §8 |
| W1–W13 | toolexec work packages (W11 = M3 deletion) | toolexec-design §9 |
| D1–D12, R1–R16, E1–E15, P1–P6 | toolexec-design decisions, risks, experiments, prototypes | toolexec-design §0, §10–§12 |
| T0.A–T4 | recovery tracks | [RECOVERY.md](arch/RECOVERY.md) "Tracks" |
| `WP-*` | recovery work packages (e.g. WP-NODETEST, WP-PERF) | RECOVERY.md "New work packages" |
| TR-0–TR-13, P-A–P-F | Truffle work items and measurements | [truffle-design.md](arch/truffle-design.md) §10.2, §13.2 |
| `GG-NNNN`, `GG-SCHED-NNNN`, … | requirements (one YAML each) | [requirements/](../requirements/), SPEC Appendix B |
| `BUG-NNNN` | graalgo bug ids cited by exclusions and quarantines | [bugs.md](dev/bugs.md) |
| `ADR-NNNN` | architecture decision records | [docs/dev/adr/](dev/adr/) |
| DV1–DV6 | launcher deviations with retirement triggers | [ADR-0010](dev/adr/ADR-0010-launcher-deviations.md) |
| `wt/<area>/<lane>` | lane branch, worktree `~/dev/graalgo-wt/<lane>` | [AGENTS.md](../AGENTS.md) §4 |
| `p3*`, `phase1/`, `wave1/`… | research-era names (Phase 3, corpus waves); legacy | [REPO_STRUCTURE.md](REPO_STRUCTURE.md) |
| `synth_*.go` | v1 hand-written stdlib stand-ins (frozen, deleted at M3) | [bridge-inventory.md](arch/bridge-inventory.md) |

## Docs: what is current

Current: [SPEC.md](arch/SPEC.md), [RECOVERY.md](arch/RECOVERY.md),
[toolexec-design.md](arch/toolexec-design.md),
[truffle-design.md](arch/truffle-design.md), [abi-spike.md](arch/abi-spike.md),
[limitations.md](arch/limitations.md), [performance.md](arch/performance.md),
[traceback-normalization.md](arch/traceback-normalization.md),
[bridge-inventory.md](arch/bridge-inventory.md), [dev/](dev/).
History (read for context only): [FEASIBILITY.md](arch/FEASIBILITY.md),
[PHASES.md](arch/PHASES.md), [phases/](arch/phases/), [reviews/](arch/reviews/),
[spikes/](arch/spikes/), [research/](arch/research/), [docs/spec/](spec/),
[frontend-v1.md](arch/frontend-v1.md), [frontend-v2.md](arch/frontend-v2.md),
[build-artifact-native-image.md](arch/build-artifact-native-image.md),
[non-scalar-specialization-audit.md](arch/non-scalar-specialization-audit.md),
[research/](../research/) (Phase-5 survey).
