# Test-suite cost profile

What the per-push gates cost, what dominates each, and the levers. The
owner's rule: more tests are welcome, longer test runs are not; test
efficiency is nearly as important as effectiveness. Re-measure with
`make test-times` ([test/lib/gates.py](../../test/lib/gates.py)); the CPU
times are ratcheted against
[test/duration-baseline.json](../../test/duration-baseline.json) (local:
SLOWER warning above 1.2x; CI: failure above 2x + 30 s).

## How measured (2026-11-20)

Shared 16-core host, load 12–40 from other lanes (wall times are noisy;
CPU = user+sys of the gate's process tree is the stable number). One gate
at a time, `nice -n 19`, `SNAPSHOT_JOBS=1`, JVMs `-Xmx2g`–`4g
-XX:ActiveProcessorCount=4`. "Before" is master a366807f (a pristine
export), "after" this lane; both run `make pre-land`'s list (`all`,
`check-links-external`, `test-diffharness`) from warm Go build caches.

| target | before wall s | CPU s | after wall s | CPU s | dominated by |
|---|---:|---:|---:|---:|---|
| test-diffharness | 814 | 1288 | 39 | 41 | gc builds (was: graalgo JVMs) |
| snapshot-check | 723 | 1133 | 440 | 709 | 2 JVMs/program (now ~1), 113 MB v1 JSON each |
| check | 219 | 349 | 166 | 264 | frontend go test, fresh exports |
| test-m1a | 202 | 350 | 170 | 257 | one JVM start per program |
| test-regression | 122 | 166 | 124 | 177 | v1 frontend + JVM per package |
| test-toolexec | 72 | 132 | 35 | 111 | go vet + go test |
| test-p3frontend | 71 | 76 | 50 | 69 | go vet + go test |
| test-java-fast | 59 | 37 | 38 | 38 | corpus classes, sleeps |
| check-links-external | 57 | 1 | 8 | 0 | network |
| test-m1r | 28 | 55 | 22 | 42 | JVM per program |
| test-ci | 28 | 42 | 24 | 38 | smoke + JIT guard |
| test-m1c | 22 | 41 | 20 | 32 | JVM per program |
| test-m1d | 16 | 36 | 16 | 30 | JVM per program |
| test-m1w13 | 15 | 28 | 12 | 21 | JVM per program |
| test-imgcheck | 9 | 24 | 8 | 20 | 6 JVM runs |
| pe-check-m1a | 9 | 15 | 8 | 15 | JIT compiles |
| build | 8 | 39 | 9 | 42 | javac |
| java-test-classes | 7 | 20 | 6 | 21 | javac |
| **total** (all targets) | **2487** | **3848** | **1198** | **1937** | 41.5 -> 20.0 min |

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
9. **CI only**: no Go build cache ever restored (setup-go keys on a root
   `go.sum` this repo lacks): std compiled cold per configuration, gates
   one after another on a 4-vCPU runner.
10. **m1r/m1c/m1d/m1w13/imgcheck** (9–28 s): m1a's pattern, smaller.

## What changed

- diffharness `validate` runs only the reference and the alternate
  backend (`cost_test.go` plants a third that must not run); the separate
  green pass is gone (validate's step 1 is the green run); the rerun
  control reuses the green run's reference outcome; programs run
  `DH_JOBS` at a time (default `SNAPSHOT_JOBS`).
- snapshot check: the graalgo backend is `lazyBuild` — a program runs
  without the `--check` JVM first; only a diverging one is re-run with
  build+run, whose outcome is the verdict (signatures unchanged); diverging
  without the build but matching with it is a flake and fails
  `-retry-budget` (`cost_test.go`).
- check: `build.sh check` runs once (the root's); it validates a scratch
  copy of gofrontend instead of rewriting the binary other gates execute,
  and deletes its 113 MB exports.
- m1a/m1r/m1c/m1d/m1w13/imgcheck: the gc leg is replayed from
  [gcoracle.sh](../../test/lib/gcoracle.sh)'s content-addressed cache
  (key: toolchain, GO* env, module files, flags; never a timed-out run or
  an output naming the scratch dir); the runner JVMs map a class-data
  sharing archive of the truffle classes ([jvmcds.sh](../../test/lib/jvmcds.sh):
  0.67 -> 0.36 s wall, 1.47 -> 0.84 s CPU per hello-size program);
  snapshot-check and test-regression get the same for graalgo-sync-run
  (12.5 -> 9.6 s CPU per corpus program; not yet in the "after" column).
- check-links-external: each distinct URL fetched once, 8 at a time.
- `make pre-land`, `make test-times` and CI run the gates through
  gates.py: timed, ratcheted, `GATE_JOBS` at a time with buffered output
  (the truffle JVM-test gates sharing `out/test-tmp` never overlap). CI:
  `GATE_JOBS=2`, the Go build cache and the gc-oracle cache restored by
  `actions/cache`, and the docs tier for a Markdown-only change off master.
- `make quick`: the inner loop (static repo gates, module suites, the
  mutation slice of the snapshot corpus, 8 m1a + 2 m1r + 1 m1d programs).

## Remaining top costs and next levers

1. **snapshot-check** (440 s at `SNAPSHOT_JOBS=1`; CI runs 4): the v1
   path — gofrontend writes 113 MB of JSON per program and the JVM parses
   it. Levers: cache the export per (source, gofrontend) hash; all of it
   retires with v1 at M3 (toolexec-design D11/D12).
2. **check** (166 s): the frontend `go test` (~45 s of sequential tests,
   each exporting std) and the validator over fresh exports. In CI the
   restored Go cache now serves unchanged `go test` packages.
3. **test-m1a** (170 s): ~0.85 s of JVM CPU a program after CDS. Lever: a
   batch runner (programs in fresh polyglot Contexts of one warm JVM, with
   process isolation kept for exit/deadlock/signal programs) — needs
   BundleMain support.
4. **test-regression** (124 s): 7 v1 `graalgo test` packages (retires at
   M3).
5. **test-toolexec / test-p3frontend** (35 / 50 s): Go-cached when
   unchanged (locally, and in CI through the restored cache).
6. **test-java-fast** (38 s): ControlFlow/Arithmetic corpus classes (21 s)
   and fixed `Thread.sleep`/stress windows in the scheduler tests; replace
   sleeps with condition waits where the test is about ordering.

CI (4 vCPU, not measurable from this lane): the job ran `make all`, then
the 12-min harness self-test, one gate at a time and with no Go cache; it
now runs both through gates.py two at a time with the caches restored.
The per-gate table is in the step log.

Findings handed back (not this lane's to fix): `test/m1d/mutants.sh`'s
`poller-lost-read-wakeup` survives on master a366807f (with every cache
off too); `test/m1c/mutants.sh` has two stale anchors (`compare-signed`,
`index-misses-last-position`); `test/m1c/programs/time_timers.go` failed
once (stderr differs) at load 14 and passed on the rerun.
