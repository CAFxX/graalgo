# Test-suite cost profile

What the per-push gates cost, what dominates each, and the levers. The
owner's rule: more tests are welcome, longer test runs are not; test
efficiency is nearly as important as effectiveness. Re-measure with
`make test-times` ([test/lib/gates.py](../../test/lib/gates.py)); the CPU
times are ratcheted against the baseline of the environment (local:
SLOWER warning above 1.2x; CI: failure above 2x + 30 s; see "Baselines per
environment" below).

## Round 3 (2026-11-25): the m1* gates' graalgo legs in one JVM

The m1* gates started one JVM per program (~0.4 s wall, ~0.9 s CPU of JVM
start, class loading and warm-up before a line of Go ran): ~250 programs
per push. Now each gate builds every program, then runs the queued
graalgo legs in ONE JVM ([batchrun.sh](../../test/lib/batchrun.sh),
`graalgo-bundle-run --batch`, `graalgo.launcher.BundleBatchMain`), each in
a fresh polyglot Context built exactly as `BundleMain` builds it (what
production does per program run), with its stdin/stdout/stderr dup2'd
onto descriptors 0-2, its working directory and `os.Args`
(`graalgo.syscall.ProcessStdio`: a program's system calls reach the real
descriptors), and its timeout as timeout(1) gives it (status 124, the
context cancelled). A JVM that dies under a program (or a cancel that does
not finish: the batch halts) leaves that program and every later one to
the gate's per-process leg, so a crash is never silent and never decides
a verdict. `GRAALGO_BATCH=off` is the old path; `BATCH_SAVE=DIR` keeps
every leg's output for a both-ways diff.

What stays per process: a program with its own environment (the JVM has
one environ: m1c `//m1c:env`, m1d `// env:`), a program naming a
process-global mutator that outlives its context or ends the process
(`batch_isolated`: rlimits, umask, credentials, session, signals, raw
exit), m1c's probes (they decide what runs), and the probes that need a
JVM of their own (cancel.sh, tools.sh, leak.sh, pe-check-m1a's JIT
checks, test-imgcheck's 6 runs).

Equivalence: m1a (188), m1r (26), m1c (16), m1d (6) and m1w13 (15) run
both ways with `BATCH_SAVE`: identical verdicts, and the 246 batched legs'
stdout, stderr and status byte-identical except one printed heap address
(random per run). The first diff found a real per-context leak: goroutine
ids came from a JVM-wide counter (the second program's main printed
`goroutine 103`); they are per scheduler now. Planted
([test/m1r/batch.sh](../../test/m1r/batch.sh), in test-goroutine-modes):
stdio/args/cwd apart, a goroutine leaked past main silent in the next
program, a hang -> 124 and the batch goes on, `os.Exit` from a goroutine,
main is goroutine 1 after 7 runs (fails with the old counter), a SIGKILLed
batch JVM -> per-process reruns of it and the rest, a divergent program
still fails m1r/run.sh; [test_batchrun.py](../../test/lib/test_batchrun.py)
(`make check`) covers the driver with a fake runner.

Also: graalgo-tool's end-to-end tests (each with its own GOCACHE, module
and binaries) run `t.Parallel`, `go test -parallel 4`. Not taken:
slicing snapshot-check across CI shards (m3-plan D1 moves it to the new
path, 433 -> 57 s).

Dev host (`nice -n 19`, warm gc-oracle cache, load 6–12; wall / CPU s):

| gate | before | after |
|---|---:|---:|
| test-m1a | 108.9 / 189 | 55 / 61 |
| test-m1r | 21.1 / 47 | 12.5 / 26 |
| test-m1c | 13.0 / 25 | 9.3 / 15 |
| test-m1d | 7.8 / 17 | 7.2 / 17 |
| test-m1w13 | 10.6 / 18 | 7.0 / 10 |
| test-toolexec | 30 (module go test) / 120 | 16 / 129 |
| test-goroutine-modes | 35 / 89 | 42 / 102 (+ batch.sh ~7 / 10; its 13 program legs in 4 JVMs; load ~12) |

Round-4 levers: leak.sh (12 s / 27 CPU s) and cancel.sh (7 / 27) in
test-goroutine-modes; test-pin-smoke's slow classes; the per-program build
loop that now dominates m1a (go build through graalgo-tool, transform.py,
2 normalize.py runs per program: one python process for the whole
compare phase); the CI baseline's walls for the m1* gates (re-record from
the first green run so `gates.py --shard` balances on the new numbers).

## Round 2 (2026-11-25): where the minutes went, and what changed

Profiled on master a542c799 (one `pre-land` through gates.py with a timing
SHELL wrapper over every recipe line, `nice -n 19`, `-j 1`, load 7–13),
the CI gate-times artifacts of a542c799 (warm) and 09acff6b (cold), and the
weekly full-suite's CLASS lines. Ranked by dev-host wall time:

| # | where | wall s | CPU s | what |
|--:|---|---:|---:|---|
| 1 | snapshot-check | 298 | 503 | 62 v1 JVMs: gofrontend 1.5 s + 113 MB JSON parse each (v1, retires at M3) |
| 2 | test-m1a | 153 | 233 | ~190 programs, serial: 0.36 s JVM each (CDS) + leg overhead |
| 3 | `check`'s pin-audit smoke | 108 | 192 | the whole SchedTestRunner suite (fast + slow classes) in thread mode under JFR |
| 4 | test-regression | ~110 | ~170 | 7 v1 `graalgo test` packages (tls-wrap-hang needs `-Xmx4g`) |
| 5 | `check`'s frontend `go test` | 47 | 66 | never cached: TMPDIR inside the module made every t.TempDir path a test input |
| 6 | test-p3frontend | 37 | 57 | go vet + go test (execs the v1 backend: not safely cacheable) |
| 7 | test-java-fast | 35 | 35 | corpus classes, fixed sleeps |
| 8 | test-goroutine-modes | 31 | 63 | thread-mode programs; 4 Java classes the pin smoke also runs (removed) |
| 9 | test-diffharness | 30 | 36 | gc builds of the corpus |
| 10 | test-toolexec | 29 | 118 | go test; graalgo-tool's e2e package is the critical path (30 s) |
| – | check-links-external | 21 | 0.5 | network: waited for a job slot |

CI (one 4-vCPU runner, 12.3 min job): CPU-bound (2300 CPU s in 700 s of
gates); `check` alone 361 s wall was the longest gate.

Changes (each with a planted violation; see the commits):

- **CI shards** (`gates.py --shard I/N`, ci.yml matrix of 4): disjoint
  shards covering the list, longest-processing-time-first over the
  runner baseline's wall times; `gates.py shards 4` prints them. Each job
  runs setup + its shard + the drift check. Expected job wall: ~6–7 min
  (the snapshot-check shard is the floor) instead of 12.3.
- **test-pin-smoke**: the pin-audit smoke left `check` for its own gate
  (same command), so a shard can run it beside the static gates; and
  test-goroutine-modes dropped its Java leg (4 classes in thread mode),
  a strict subset of what the pin smoke runs in the same mode.
- **frontend go test cacheable**: `make check` points TMPDIR at
  `<repo>/.tmp/frontend-check`; the one input outside the module (a
  corpus program) is hashed into `GRAALGO_FE_EXT_INPUTS`, which the test
  that reads it looks up; build.sh check fails a test naming a `../`
  path without that lookup. 47 -> 1.4 s on an unchanged tree.
- **LIGHT gates**: check-links-external runs beside the others without a
  job slot (-21 s at `-j 1`).
- **mutation.yml**: the gate runs 4 programs at a time (`MUTATION_JOBS`,
  default 1 locally). **conformance.yml**: graalgo-sync-run maps the CDS
  archive (round 1 measured -23% CPU per program).
- **Cache integrity** (BUG-0012): gcoracle entries and CDS archives carry
  a sha256 MANIFEST written last and verified on load (a mismatch deletes
  the entry: a miss); signal-killed or crashed gc runs are never stored;
  the oracle key hashes gcoracle.sh and the leg's shell functions.

Measured `make pre-land` (dev host, `-j 1`, `nice -n 19`, `SNAPSHOT_JOBS=1`,
`GRAALGO_JAVA_OPTS=-Xmx4g`, load 10–14; wall is noisy, CPU is the number):

| run | tree | wall s | CPU s (user+sys) | notes |
|---|---|---:|---:|---|
| master | a09719ea | 1045 | 1807 | gc-oracle cold |
| this lane | 8026dfc6 (= master f7da9e04 + lane) | 950 | 1699 | includes master's new leak.sh/tools.sh (~+20 CPU s); frontend suite missed (the merge changed it) |
| `make check`, unchanged tree | 8026dfc6 | 22 | 19 | was 162 / 280 with the pin smoke and the uncached frontend suite |

Per tier: CI per-push build-and-check 12.3 min on one runner -> four
shards of ~360 s of baseline work each (snapshot-check alone is 339 s):
~6–7 min expected (to be confirmed by the first sharded run, whose four
gate-times artifacts re-record the CI baseline); conformance and the
nightly mutation run lose ~20% and up to ~3x of their gate time (CDS; 4
programs at a time); the weekly full-suite is unchanged (12 min, its
two v1 classes are 437 of its 608 s).

Evaluated and not taken: `-XX:TieredStopAtLevel=1` (bundle JVM: -31% CPU,
same wall; sync JVM: -17% CPU, +11% wall; it changes the host JIT the
runtime's concurrency is tested under); `-XX:+UseSerialGC` (no gain); the
JDK 25 AOT cache (`-XX:AOTCacheOutput` produced no usable cache with the
JVMCI-enabled launch; dynamic CDS stays); a cache of gofrontend's v1
export (113 MB per program, ~7 GB for the corpus; v1 retires at M3);
go test caching for test-toolexec/test-p3frontend (their tests exec
`go build`/the backend, whose inputs go test cannot hash).

Next levers (round 3): a batch BundleMain (programs in fresh Contexts of
one JVM, process isolation kept for exit/deadlock/signal programs) for the
m1* gates (~0.3 s x ~300 programs); slicing snapshot-check across two CI
shards (needs per-slice exclusion accounting); `t.Parallel` in
graalgo-tool's e2e tests; the slow runtime classes the pin smoke runs per
push (GcDifferentialTest ~41 s, LauncherOsArgsTest ~20 s on CI);
test-goroutine-modes (35 s / 91 CPU s at f7da9e04, growing with each
probe: m1a/m1r/m1c thread-mode programs one JVM each, cancel.sh, tools.sh
and leak.sh's 12 contexts x 2 modes): one JVM per mode running every
probe and program in fresh Contexts of a shared engine, the same
assertions (toolexec_setup itself is only ~0.9 s per script).

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
| test-goroutine-modes | - | - | 25 | 42 | new on master (GG-0074): one JVM per scheduler suite/program; CDS + gc-oracle cache apply |
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
