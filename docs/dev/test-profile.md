# Test-suite cost profile

What the per-push gates cost, what dominates each, and the levers. The
owner's rule: more tests are welcome, longer test runs are not; test
efficiency is nearly as important as effectiveness. Re-measure with
`make test-times` ([test/lib/gates.py](../../test/lib/gates.py)); the CPU
times are ratcheted against the baseline of the environment (local:
SLOWER warning above 1.2x; CI: failure above 2x + 30 s; see "Baselines per
environment" below).

## Baselines per environment

The ratchet compares like with like. A gate's CPU time depends on the host
(the 16-core dev host vs the 4-vCPU GitHub runner: snapshot-check 659 vs
~1370 s) and on whether the Go build and gc-oracle caches were warm
(generate: 1.7 s warm on the dev host, 38 s on a cold runner). c35a2888 went
red comparing a cold CI run with the warm dev-host numbers. So:

- one file per environment:
  [test/duration-baseline.json](../../test/duration-baseline.json) (dev
  host; `make test-times` default) and
  [test/duration-baseline-ci.json](../../test/duration-baseline-ci.json)
  (the runner; ci.yml passes `GATE_BASELINE=`);
- inside each, `cpu`/`wall` hold warm-cache numbers and
  `cpu_cold`/`wall_cold` cold-cache ones. `GATE_CACHE` (`--cache`) says
  which a run is: CI sets `cold` when its `actions/cache/restore` matched
  no key, locally it is `warm`. A warm run compares with `cpu`, falling
  back to `cpu_cold` (never lower, so the fallback is only lax); a cold run
  compares only with `cpu_cold`, a target without one is NEW (never
  failed). Both paths are planted in
  [test_gates.py](../../test/lib/test_gates.py).
- recording: `python3 test/lib/gates.py record RUN.json --baseline FILE`
  files the run under the section of its `cache`. Dev host: from
  `.tmp/gate-times.json` of a warm `make test-times`. CI: download the
  `gate-times` artifact of a green run (`gh run download <id> -n
  gate-times`) and record it into `duration-baseline-ci.json`. Re-record
  only for a reviewed, intended cost change, and say so in the commit.
  The baselines are forward-only: a target whose `cpu`/`cpu_cold` number
  is new or rose more than 25% (+1 s) since the git base needs a
  `reason` entry for that target added in the same change (`record
  --reason TEXT` writes it for exactly those targets);
  [check_ratchets.py](../../.github/scripts/check_ratchets.py) (`make
  check-repo`) fails otherwise.
  The CI file's cold sections hold, per gate, the higher of the first two
  green runs (09acff6b, push and dispatch; both missed the cache, so
  every gate of `pre-land`'s list and the setup targets has a cold
  number); the first green warm run should fill `cpu`/`wall`. When two
  runs of one kind are recorded together, take the max per gate so a
  normal run does not flap.

## How measured (2026-11-20 .. 24)

Shared 16-core host, load 12–40 from other lanes (wall times are noisy;
CPU = user+sys of the gate's process tree is the stable number). One gate
at a time, `nice -n 19`, `SNAPSHOT_JOBS=1`, JVMs `-Xmx2g`–`4g
-XX:ActiveProcessorCount=4`. "Before" is master a366807f (a pristine
export, 11-20), "after" this lane merged with master bc89d2e2 (11-24, load
~20 with another lane's `make -j2 pre-land` alongside; snapshot-check and
check from the 11-23 green run of the same tree); both run `make
pre-land`'s list (`all`, `check-links-external`, `test-diffharness`) from
warm Go build caches. The baseline file holds the "after" CPU times;
the rows of the gates master added on 11-24 (pe-check-m1a's pe_std.sh,
test-goroutine-modes) were measured on a quiet host (load ~1, right after a
reboot), so they read lower than the same gates under the usual 10–20 load.

| target | before wall s | CPU s | after wall s | CPU s | dominated by |
|---|---:|---:|---:|---:|---|
| test-diffharness | 814 | 1288 | 36 | 40 | gc builds (was: graalgo JVMs) |
| snapshot-check | 723 | 1133 | 433 | 659 | 2 JVMs/program (now ~1), 113 MB v1 JSON each |
| check | 219 | 349 | 177 | 278 | frontend go test, fresh exports (re-measured on master 95b5da40 + this lane, quiet host 11-24: 163 / 268) |
| test-m1a | 202 | 350 | 212 | 257 | one JVM start per program |
| test-regression | 122 | 166 | 126 | 169 | v1 frontend + JVM per package |
| test-toolexec | 72 | 132 | 40 | 128 | go vet + go test |
| test-p3frontend | 71 | 76 | 60 | 80 | go vet + go test |
| test-java-fast | 59 | 37 | 40 | 39 | corpus classes, sleeps |
| check-links-external | 57 | 1 | 8 | 1 | network |
| test-m1r | 28 | 55 | 24 | 40 | JVM per program |
| test-ci | 28 | 42 | 27 | 38 | smoke + JIT guard |
| test-m1c | 22 | 41 | 18 | 30 | JVM per program |
| test-m1d | 16 | 36 | 13 | 28 | JVM per program |
| test-m1w13 | 15 | 28 | 13 | 20 | JVM per program |
| test-imgcheck | 9 | 24 | 10 | 21 | 6 JVM runs |
| pe-check-m1a | 9 | 15 | 13 -> 28 | 17 -> 50 | JIT compiles; master added pe_std.sh (std paths of the realistic programs) |
| test-goroutine-modes | - | - | 25 | 42 | new on master (GG-0073): one JVM per scheduler suite/program; CDS + gc-oracle cache apply |
| build | 8 | 39 | 36 | 48 | javac |
| java-test-classes | 7 | 20 | 19 | 23 | javac |
| **total** (all targets) | **2487** | **3848** | **1313** | **1926** | 41.5 -> 21.9 min; master's 11-24 gates (pe_std.sh, goroutine modes) add ~1 min wall |

## Top costs before, and what dominated them

1. **test-diffharness** (814 s): the gc-vs-gc self-test also ran the whole
   corpus under graalgo (`validate` ran every configured backend, compared
   two) and repeated its green pass in a separate `diffharness run`.
2. **snapshot-check** (723 s): two JVMs a program (`--check`, then the
   run), each re-running gofrontend (1.7 s, 113 MB of v1 JSON for a hello
   world) and parsing it.
3. **check** (219 s): `internal/frontend/build.sh check` ran twice (truffle
   module's `check` and the root's): 2 x ~45 s of frontend `go test`.
4. **test-m1a** (202 s): ~183 programs, one runner JVM each (0.67 s wall,
   1.5 s CPU for a hello world, mostly class loading) + gc leg + builds.
5. **test-regression** (122 s): 7 v1 `graalgo test` packages.
6. **test-toolexec**, **test-p3frontend** (72, 71 s): go vet + go test.
7. **test-java-fast** (59 s wall, 37 s CPU): corpus classes and fixed
   `Thread.sleep`/stress windows (the wall–CPU gap).
8. **check-links-external** (57 s wall, 0.6 s CPU): sequential fetches,
   once per citation.
9. **CI only**: no Go cache ever restored (setup-go keys on a root
   `go.sum`), gates one after another on a 4-vCPU runner.
10. **m1r/m1c/m1d/m1w13/imgcheck** (9–28 s): m1a's pattern, smaller.

## What changed

- diffharness `validate` runs only its two backends (`cost_test.go`); the
  duplicate green pass is gone; the rerun control reuses the green run's
  reference outcome; `DH_JOBS` programs at a time.
- snapshot check: graalgo is `lazyBuild` — no `--check` JVM unless the run
  diverges; then build+run is the verdict (signatures unchanged), and a
  lazy-only divergence fails `-retry-budget` (`cost_test.go`).
- check: `build.sh check` runs once, on a scratch gofrontend copy.
- m1a/m1r/m1c/m1d/m1w13/imgcheck: the gc leg is replayed from
  [gcoracle.sh](../../test/lib/gcoracle.sh)'s content-addressed cache
  (key: toolchain, GO* env, module files, flags; never a timed-out run or
  an output naming the scratch dir); the runner JVMs map a class-data
  sharing archive of the truffle classes ([jvmcds.sh](../../test/lib/jvmcds.sh):
  0.67 -> 0.36 s wall, 1.47 -> 0.84 s CPU per hello-size program);
  snapshot-check and test-regression get the same for graalgo-sync-run
  (12.5 -> 9.6 s CPU per corpus program).
- check-links-external: each distinct URL fetched once, 8 at a time.
- `make pre-land`, `make test-times` and CI run the gates through
  gates.py: timed, ratcheted, `GATE_JOBS` at a time with buffered output
  (the truffle JVM-test gates sharing `out/test-tmp` never overlap). CI:
  `GATE_JOBS=2`, the Go build cache and the gc-oracle cache restored by
  `actions/cache/restore` (saved even by a red run, so caches do not stay
  cold while master is red), the runner's own baseline, and the docs tier
  for a Markdown-only change off master.
- `make quick`: the inner loop (repo gates, module suites, slices).

## Remaining top costs and next levers

1. **snapshot-check** (433 s at `SNAPSHOT_JOBS=1`; CI runs 4): the v1
   path — gofrontend writes 113 MB of JSON per program and the JVM parses
   it. Levers: cache the export per (source, gofrontend) hash; all of it
   retires with v1 at M3 (toolexec-design D11/D12).
2. **check** (177 s): the frontend `go test` (~45 s of sequential tests,
   each exporting std) and the validator over fresh exports. In CI the
   restored Go cache now serves unchanged `go test` packages.
3. **test-m1a** (170–212 s, 257 s CPU): ~0.85 s of JVM CPU a program after CDS. Lever: a
   batch runner (programs in fresh polyglot Contexts of one warm JVM, with
   process isolation kept for exit/deadlock/signal programs) — needs
   BundleMain support.
4. **test-regression** (126 s): 7 v1 `graalgo test` packages (retires at
   M3).
5. **test-toolexec / test-p3frontend** (40 / 60 s): Go-cached when
   unchanged (locally, and in CI through the restored cache).
6. **test-java-fast** (41 s): ControlFlow/Arithmetic corpus classes (21 s)
   and fixed `Thread.sleep`/stress windows in the scheduler tests; replace
   sleeps with condition waits where the test is about ordering.

CI (4 vCPU; not measurable from this lane): `all` + the harness self-test
now run two gates at a time with the Go cache restored; the step log
carries the per-gate table. Findings handed back: BUG-0012 (gofrontend
corrupts its JSON under host load; reproduced with gofrontend alone, it
reddened most pre-land runs of 11-23/24); m1d mutant
`poller-lost-read-wakeup` survives on master too; two stale m1c mutant
anchors (`compare-signed`, `index-misses-last-position`).
