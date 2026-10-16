# P3-M9 wave-3 differential corpus (tier-1b: container/*, hash/*)

Wave-3 corpus for the P3-M9 synthetic `container/heap`, `container/list`,
`container/ring`, `hash/adler32`, `hash/crc32`, `hash/crc64`, `hash/fnv`,
`hash/maphash` packages. Conventions are the wave-1 ones
(`../wave1/README.md`: `NN_name.go` naming, deterministic-output rules,
`.timing` / `.scopelimited` / `.panic` / `.synthetic` marker semantics,
the tmp discipline, the shared-runner model) plus the wave-3 delta
(`hash/maphash` pins structural properties only — see below).

## Layout

- One subdir per package, named by full import path:
  `container/heap/`, `container/list/`, `container/ring/`,
  `hash/adler32/`, `hash/crc32/`, `hash/crc64/`, `hash/fnv/`,
  `hash/maphash/`.
- `run_wave3.sh` — the shared runner (a `../wave1/run_wave.sh`
  derivative with wave-3 tmp paths: `TMPDIR=$HOME/workspace/.tmp-graalgo-wave3`,
  `GOCACHE=$HOME/workspace/.gocache-m9-wave3`). Usage:
  `GR=~/workspace/graalgo ./run_wave3.sh [pkg|all]`.
- Per-package `run_differential.sh`: thin wrapper
  `exec "$(dirname "$0")/../run_wave3.sh" <pkg>`.
- Per-package `README.md`: scope boundary + retest trigger.

## Package notes

- `hash/adler32`, `hash/crc32`, `hash/crc64`, `hash/fnv`: pure-Go ports,
  value-identical to gc. Corpus pins published test vectors, streaming/
  chunking invariance, marshal round-trips, and exact error strings.
- `hash/maphash`: the stdlib funnels through the runtime memhash
  intrinsic, which no pure-Go synthetic can reproduce bit-exactly. The
  synthetic implements the full API over a P3-specific seeded hash:
  deterministic per fixed seed, NOT gc-value-identical. The corpus pins
  structural properties only (chunking invariance, Write/WriteByte/
  WriteString equivalence, seed sensitivity, Reset/Clone semantics,
  Bytes/String/Hash consistency, zero-Seed panics) — never exact values.
  A future memhash intrinsic would make exact values testable
  (retest trigger: `memhash intrinsic lands`).
- `container/*`: pure-Go ports of the stdlib; corpus pins ordering and
  structural invariants against sorted-reference checks.

## Tier-1d packages (this worker: embed, structs, unique, weak, internal/*)

- `unique`: synthetic adapter (`internal/frontend/synth_unique.go`,
  real-Go canonicalization map, exact gc 1.27.1 API). Corpus is fully
  differential: canonicalization/equality/`Value` across int, string,
  struct, array, bool types; concurrent `Make` from 8 goroutines
  (deterministic: fixed count + all-ok bool); adversarial (pointer
  identity vs pointee equality, `any`-typed values, 5000 distinct values).
- `structs`: synthetic marker type (`synth_structs.go`: `HostLayout`).
  Corpus is differential: embedding the marker, field access, zero value.
- `embed`, `weak`: scope-limited (no adapter — the feature is a
  compiler directive / runtime semantics, not a synthesizable package).
  `embed`: `reason: needs //go:embed directive materialization in
  exporter/P3SyncMain`, `retest: go:embed directive support lands`. `weak`:
  `reason: needs weak-pointer runtime semantics (Pointer.Value observes
  reclamation) in the GMP backend`, `retest: weak reference support lands
  in backend`. The gc leg still builds+runs them (validity + determinism).
- `internal/*`: external programs cannot import `internal/*` at all (go
  tool restriction), so these are `.scopelimited` + `nogc`. `internal/bytealg`
  has a synthetic adapter (`synth_bytealg.go`, portable real-Go impls,
  synthetic-only like `internal/reflectlite`); its correctness evidence is
  the tier1d harness (`phases/phase3/hidden_files/m9-tmp/tier1d-harness/`),
  which runs the corpus program (self-checking against `bytes`/`strings`)
  under gc against the extracted synthetic source. `internal/cpu`,
  `internal/godebug`, `internal/bisect` have no adapter: cpu needs cpuid
  intrinsics, godebug needs the godebugs table + runtime linknames, bisect's
  core needs `runtime.Callers`; none has an in-tree consumer.

## Frontend selection (tier-1d note)

`run_wave3.sh` shells out to `P3SyncMain`, which locates the `gofrontend`
binary via `GRAALGO_GOFRONTEND` (override), else `<phase1>/frontend/gofrontend`.
When the shared m9 tree's frontend is mid-flight (other workers' uncommitted
edits), point `GRAALGO_GOFRONTEND` at a known-good build:

```bash
GRAALGO_GOFRONTEND=~/workspace/tmp/tier1d-fe/gofrontend \
  ./run_wave3.sh unique
```
