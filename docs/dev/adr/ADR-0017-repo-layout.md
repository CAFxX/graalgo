# ADR-0017 — Repository layout after the M3 cutover, and a staged migration

**Date:** 2026-11-20
**Status:** **PROPOSED** (lane `docs-map`, `wt/docs/map`); stage 1
accepted by the coordinator and done 2026-11-25 (m3-plan D0); the later
stages are still for the owner's decision.
**Scope:** every top-level and second-level directory;
[REPO_STRUCTURE.md](../../REPO_STRUCTURE.md), [MAP.md](../../MAP.md),
[toolexec-design.md](../../arch/toolexec-design.md) §9.1 (M3 delete
inventory, W11), [AGENTS.md](../../../AGENTS.md).

## Context

The owner: "it's always difficult to know where things are". Measured on
`6e309544` (tracked text, jars excluded: ~934k lines):

1. **Two pipelines interleaved.** The product path (`internal/toolexec` →
   GIR → `graalgo.go.*` → `graalgo-bundle-run`) and the v1 path
   (`internal/frontend`, `cmd/graalgo`, `internal/{driver,vet}`,
   `internal/backend/interop`, `graalgo.{frontend,values}`) share
   `internal/` and one Java module, [truffle/](../../../internal/backend/truffle/README.md),
   with no visual cue. The product's compiler sits under a directory named
   after a mechanism (`toolexec`), its node package under a module named after
   a framework (`truffle`), and the word "frontend" means the v1 path.
2. **The v1 path is ~490k lines** that go at M3: `internal/frontend`
   426k (vendored `go/` 99k, 140 `synth_*.go` 167k, `p3frontend` 16k, `v2`
   4k), `interop` 34k, v1 Java in truffle ~26k (`frontend` 21k, `values`
   3k, `sched.launcher` 1k), `internal/vet` 5k, `internal/driver`,
   `tools/gate1`, two launchers. About half the tree.
3. **Test directories are named after milestones** (`m1a`, `m1r`, `m1c`,
   `m1w13`), which say when a gate was written, not what it proves; the
   v1-era `test/corpus` holds the stderr normalizer every new-path gate
   uses (`lifecycle/normalize.py`).
4. **docs/arch mixes current design with history**: of 609 files, 9 are
   current; `phases/`, `reviews/` (254 files), `spikes/` (317), `research/`,
   FEASIBILITY, PHASES, frontend-v1/v2 are archive.
5. **§9.1 misses v1-driven pieces outside the deleted directories** (found
   by grepping for `graalgo-sync-run`, `graalgo-run`, `P3SyncMain`,
   `SchedMain`, `gofrontend` outside them): `make snapshot-check`'s
   [backends-p3.json](../../../test/differential/backends-p3.json) and the
   diffharness `srctest`/`typeparam` modes; [test/jit/](../../../test/jit/)
   (`test-jit` re-pointed to the new path, `pe-check` deleted for
   `pe-check-m1a`: m3-plan D3); `test/bench/run.sh` (deleted at D3, `make
   bench` = `bench-suite`);
   [test/lib/infra.sh](../../../test/lib/infra.sh); the
   `run_differential.sh` of the topic and wave corpora (deleted at D0/D3,
   replaced by `make stdlib-gate`);
   [test/e2e/](../../../test/e2e/README.md); `tools/fuzz` (deleted in stage 1),
   `tools/fuzz-soak/` (deleted at D3), [tools/mutation/](../../../tools/mutation/)
   and [tools/godebug-diff/](../../../tools/godebug-diff/) (re-pointed at D3),
   `tools/run_stdlib_gate.sh` (deleted at D3);
   `make fast-run` (deleted at D3); v1-frontend Java tests in shared packages
   (`GcDifferentialTest`, `ChannelDifferentialTest`, `TimerDifferentialTest`,
   `launcher.Differential`); `sched.nodes`/`sched.audit`/`launcher.ToolPaths`
   (reached only from `SchedMain`/`Main`). Tests that call the built `graalgo` binary
   ([test/regression/](../../../test/regression/)) switch by themselves when
   `cmd/graalgo` becomes the launcher.

## Decision (proposed)

**Target layout after M3** — one pipeline, named by function:

| Target (planned) | Contents | From |
|---|---|---|
| cmd/graalgo | the launcher | internal/toolexec/cmd/graalgo-launcher |
| cmd/graalgo-tool, cmd/graalgo-stub, cmd/graalgo-goroot | the wrapper, the executable stub, the fork applier | internal/toolexec/cmd/* |
| compiler/ (Go module; or the root module) | emit, ir, link, bundle, archive, argv, launcher, release, fork, intrinsics (registry), syscalls (table), girinterp (test-only) | internal/toolexec/* minus cmd |
| runtime/ (one javac module) | `graalgo.go.*` (language, nodes, values, types, loader, intrinsics), scheduler, safepoint, jfr, syscall executor, GIR codec, pinned jars, `bin/graalgo-bundle-run` | internal/backend/{truffle,gir}, graalgo-bundle-run |
| stdlib/ | fork/ and patches/ | unchanged |
| test/lang, test/std, test/sys, test/unsafe, test/descriptors | the per-push differential gates | test/m1a, m1r, m1c, m1w13, imgcheck |
| test/conformance | diffharness, gc snapshots, exclusion registers, snapshot corpus | test/differential, test/snapshots |
| test/lib | shared runner code incl. normalize.py | test/lib + test/corpus/lifecycle/normalize.py |
| test/{fuzz,bench,e2e,regression,java,pe} | as today (pe = test/jit) | same |
| tools/ | req, torture, mutation, stdlib-* tools re-pointed at the new path | tools/ minus gate1 |
| docs/{MAP.md,REPO_STRUCTURE.md,arch,dev} | current docs only | docs/ |
| docs/archive/ | phases, reviews, spikes, research, FEASIBILITY, PHASES, frontend-v1/v2, spec/, bridge-inventory (after M3), phase-5 survey | docs/arch/*, docs/spec, research/ |
| requirements/ | unchanged (`tools/req` walks up to it) | same |

**Old → new, every top-level and second-level directory** (stage numbers
refer to the migration table below; "=" means unchanged):

| Today | After | Stage |
|---|---|---|
| .github/scripts, .github/workflows | = (path strings updated) | 1, 2, 3 |
| cmd/graalgo (v1 driver) | cmd/graalgo = the launcher | 2 |
| docs/REPO_STRUCTURE.md, docs/MAP.md, docs/dev | = | — |
| docs/arch | current docs stay; history → docs/archive | 1 |
| docs/spec | docs/archive/spec | 1 |
| internal/toolexec | compiler/ + cmd/graalgo-{tool,stub,goroot} | 3a |
| internal/backend/truffle, internal/backend/gir, internal/backend/graalgo-bundle-run | runtime/ | 3b |
| internal/backend/fast-cycle | runtime/dev (its `run` re-pointed to bundles) | 2, 3b |
| internal/backend/interop, graalgo-run, graalgo-sync-run | deleted | 2 |
| internal/frontend, internal/driver, internal/vet | deleted | 2 |
| requirements | = | — |
| research/phase5-opt-survey | docs/archive/research | 4 |
| stdlib/fork, stdlib/patches | = | — |
| test/m1a, test/m1r, test/m1c, test/m1w13, test/imgcheck | test/lang, test/std, test/sys, test/unsafe, test/descriptors | 3c |
| test/differential, test/snapshots | test/conformance (harness, registers, snapshots) | 2 (re-point), 3c |
| test/corpus | = frozen corpus; its `run_differential.sh` scripts deleted, normalize.py → test/lib | 1, 2 |
| test/jit | test/pe (re-pointed to bundles at m3-plan D3) | 2, 3c |
| test/e2e | = (re-pointed to the launcher) | 2 |
| test/fuzz, test/bench, test/regression, test/java, test/lib | = (bench `run.sh` deleted at m3-plan D3) | 2 |
| test/spikes | docs/archive/spikes-abi once abi-spike residual risks close | 4 |
| tools/gate1, tools/fuzz | deleted (tools/fuzz superseded by test/fuzz) | 2 |
| tools/fuzz-soak, tools/mutation, tools/godebug-diff, tools/run_stdlib_gate.sh | m3-plan D3: tools/mutation, tools/godebug-diff re-pointed to the new path; tools/fuzz-soak, run_stdlib_gate.sh deleted | 2 |
| tools/stdlib-fork, stdlib-vendor, stdlib-linkdiff, stdlib-excl | kept if the upgrade runbook (WP-UPG1/2) uses them, else archived | 2 |
| tools/req, tools/torture | = | — |

Not proposed: renaming Java packages (`graalgo.go.*` → `graalgo.*`) — high
churn, low value; reconsider only after M3 if `graalgo.go` reads oddly.

## Migration — every stage is a rename/delete with no behavior change

| Stage | When | What | Breaks (must be fixed in the same commit) | Verify |
|---|---|---|---|---|
| 0 | **done now** (this lane) | [MAP.md](../../MAP.md), README with status per directory, status labels in REPO_STRUCTURE | nothing | `make check-repo` |
| 1 | **done** 2026-11-25 (m3-plan D0; accepted by the coordinator) | `git mv` the archive docs (`phases/`, `reviews/`, `spikes/`, `research/`, FEASIBILITY, PHASES, frontend-v1/v2, and `docs/spec`, now moved) into [docs/archive/](../../archive/); the lifecycle corpus's `normalize.py` → [test/lib/](../../../test/lib/normalize.py) (moved) | links in live docs (SPEC, toolexec-design, abi-spike, ADR-0003/0011/0013, freeze-stub); `ARCHIVED` in [check_doc_paths.py](../../../.github/scripts/check_doc_paths.py); `HISTORICAL` in [check_gg_ids.py](../../../.github/scripts/check_gg_ids.py); normalize.py's 6 callers (test/m1a,m1r,m1c,m1w13 `run.sh`, test/fuzz/setup.sh, girinterp `harness_test.go`) | `make check-repo`; `make test-m1a test-m1r`; `git diff -M --stat` = pure renames + path edits |
| 2 | **at M3 (W11)** | §9.1 deletions **plus** the item-5 gaps above (re-point or delete each); `cmd/graalgo` ← the launcher | Makefile targets `gen-synth`, `test-v2`, `test-p3frontend`, `gate1*`, `graalgo`, `test-ci`/`test-jit`, `bench`, `fast-run`, `snapshot-check` (backend config); workflows `fuzz-soak.yml`, `stdlib-gate.yml`, `mutation.yml`, `conformance.yml` (path filters); allowlists and budgets of §9.1 | `make all` green; the M3 gate of toolexec-design §8 |
| 3a | right after M3 | `internal/toolexec` → `compiler/` + `cmd/` | ~40 code files name `internal/toolexec` (Makefile, `test/lib/toolexec.sh`, every `test/*/run.sh`, `go generate` output paths, [check_layering.py](../../../.github/scripts/check_layering.py), `conformance.yml`), the module path `graalgo-toolexec` | `make all`; `git status` clean after it |
| 3b | after 3a | `internal/backend/{truffle,gir}` + `graalgo-bundle-run` → `runtime/` | ~22 code files (Makefile, the stub's runner lookup, `intrinsics/gen` and `syscalls/gen` output paths, `check_offheap`/`check_duplicate_fqn`/layering scripts, `large_files_allowlist.txt`, `ratchet_budget.json` keys, pinned-jar checksum gate) | `make all`; ratchet budgets unchanged after `--update` |
| 3c | after 3b | test renames (table above) | Makefile targets (`test-m1a` → `test-lang` …, old names kept one release as aliases), `M1A_PROGRAMS`-style variables, AGENTS.md §2, `known.txt`/exclusion paths, `.github/workflows/ci.yml` | same pass counts per gate before and after |
| 4 | later | `research/` → archive when Phase-5 work closes; drop `internal/` if empty | doc links | `make check-repo` |

Each stage is one lane, rebased on a master with no other lane mid-flight
in the touched paths; the first commit of a stage is the `git mv` only, the
second fixes the path strings, so review sees renames as renames.

## Interim mitigation (until stage 3)

- [MAP.md](../../MAP.md) is the entry point (linked from README, AGENTS.md,
  REPO_STRUCTURE.md): task → place, data flow, legacy list, name glossary.
- Every directory of the new path and every mixed one has a README whose
  first line states **new**, **shared**, **v1** or **mixed**; v1-era READMEs
  carry a status banner.
- The boundary stays mechanical: [check_layering.py](../../../.github/scripts/check_layering.py)
  and [check_no_bridges.py](../../../.github/scripts/check_no_bridges.py).
- Optional gate (proposed, not built): every directory listed in MAP.md has
  a README whose first heading carries a status word, with a planted-violation
  test.

## Consequences

- After stage 3 the tree reads as the pipeline: `cmd/` → `compiler/` →
  `runtime/`, with `stdlib/`, `test/`, `tools/`, `docs/`.
- Stages 1 and 3 cost one quiet window each; skipping them leaves MAP.md and
  the READMEs as the only guide, which drifts unless the optional gate lands.
- toolexec-design §9.1 should list the item-5 gaps whatever is decided here.
