# BE-06 re-derive — verification evidence (2026-11-07 ~21:24–21:42 UTC)

Lane: BE-06-rederive. HEAD: `59f0a247` (B-02). Base verified: `git rev-parse HEAD`
in `~/workspace/graalgo` = `59f0a247088f0e0b6db7e0623076866727dd873c`.

## Quota (BLOCKED)

- Real 8 KB unique-content home write failed `Disk quota exceeded` at ~21:24 UTC
  (`head -c 8192 /dev/urandom > ~/workspace/.quota-probe-be06-rederive-*`, rc=1,
  probe file missing). No worktree claimed, no commit, no push, no repo-tree edits.
- `/tmp` flapped 40M–141M free during the run (other lanes staging); `/dev/shm` 72M free.
  All work in `/tmp/be06-rederive/` (1.3M total).

## Mechanism re-verified from disk truth (before fixing)

- `SchedMain.java` (full read): no `GoOs.init`, no `--` parsing, no
  `OsSurfaceInitializer`. Repo grep confirms: `GoOs.init` callers are only
  `P3SyncMain.java:515` (once per invocation, argv[0] absolutized) and
  `GoOsSurfaceInitializer.java:21` (via `Main.initializeOsSurface`); `SchedMain`
  has zero hits.
- `Main.java:249,375-396`: `initializeOsSurface` (ServiceLoader loop) +
  `buildGoArgv` (argv[0] = absolute normalized entry path) are package-private
  statics in `graalgo.launcher`; `SchedMain` lives in `graalgo.sched.launcher`
  and could not call them as-is. `GoOs.init` (`GoOs.java:275`) resets all os
  state for one run. S-22 note (REVIEW-22): share the helper, no third copy
  of the ServiceLoader loop.
- Prebuilt `out/classes` are STALE (runtime `.built` 2026-11-06 15:40, interop
  2026-10-30; both predate B-02): prebuilt `Main.class` has no
  `initializeOsSurface`/`buildGoArgv`, prebuilt interop has no
  `GoOsSurfaceInitializer.class` and its `META-INF/services/` lacks the
  `OsSurfaceInitializer` registration. Truffle classes are current (sources
  last changed 2026-11-06 10:31, `.built` 16:48). Hence the scratch rebuild
  below; `gofrontend` reused read-only from `/var/tmp/b02-verify/internal/
  frontend/gofrontend` (built 2026-11-07 12:28, after the last frontend change
  2026-11-06 17:02; smoke-tested on a trivial program, rc=0).

## Fix (5 files, +251/−4)

- `SchedMain.java`: `--` parsing (first token after `--` is the file, the rest
  are program args, flag-like args pass through) + `Main.initializeOsSurface(
  Main.buildGoArgv(file, programArgs))` after the `--check` early return,
  before `ClassPreloader.preload()` (same placement as `Main`); usage line
  updated; class javadoc notes the shared helper.
- `Main.java`: `buildGoArgv` and `initializeOsSurface` made `public static`
  (javadoc notes they are shared with `SchedMain` per BE-06/S-22). The
  ServiceLoader loop lives in exactly one place.
- New `SchedMainOsArgsTest.java` (5 tests, mirrors `LauncherOsArgsTest`):
  args after `--`, no program args, `--` before file, flag-like program arg,
  argv[0] absolute. Registered in `SchedTestRunner`.
- `graalgo.launcher/README.md`: SchedMain `--` contract documented.

## Verification (scratch, GraalVM 25.0.4.1.1 javac/java, `-Xlint:deprecation -Werror`)

- Compile: patched runtime files + full interop rebuild + tests all clean
  under the Makefile flags (`-proc:none` added; no annotation processing
  needed for these files). `javap` confirms `public static buildGoArgv` /
  `initializeOsSurface` on the patched `Main`.
- Classpath for runs (scratch first, then prebuilt): `classes/test :
  classes/runtime : classes/interop : runtime/out/classes :
  truffle/out/classes : runtime/out/test-classes : truffle/lib/*.jar`;
  `GRAALGO_GOFRONTEND=/var/tmp/b02-verify/internal/frontend/gofrontend`,
  `java.io.tmpdir=/tmp/be06-rederive/tmp`.
- RED (unpatched SchedMain from prebuilt classes + scratch interop provider):
  `SchedMainOsArgsTest` **5/5 FAIL** with the filed symptoms — `unknown flag:
  --` (exit 1), `panic: slice bounds out of range [1:0]` on empty `os.Args`
  (exit 2), `panic: index out of range [0] with length 0` on `os.Args[0]`
  (exit 2). ~178 s.
- GREEN (patched classes first): `SchedMainOsArgsTest` **5/5 PASS**.
- Regression on the same GREEN classpath: `LauncherOsArgsTest` (Main/B-02)
  **7/7 PASS**.
- Smoke: `GoDebugTest` **27/27 PASS** when run from the runtime module dir
  (the Makefile cwd; its `CORPUS` path is relative). An earlier 24/27 run
  from `/home/hatch` failed 3 subprocess tests with exit 1 for a harness
  cause (corpus path did not resolve from the wrong cwd — reproduced
  directly: the same program exits 2 under both RED and GREEN classpaths
  when invoked with an absolute path). Not a fix regression.
- Placeholder grep gate on the changed files: clean.

## Two process notes (do not repeat)

1. One GREEN attempt ran while `load-shed.sh backoff` had returned 1 (load
   14.25) because the script echoed the rc but did not abort — gate violation
   on my part. All later heavy runs were hard-gated (`backoff || exit 3`).
2. Two uncapped-heap test JVMs died with rc=137 (SIGKILL) mid-run during the
   extreme-load/memory-pressure window (load 14–27, shedder ledger: "memory
   pressure 960MB avail"). Capping the test JVM to `-Xmx768m` fixed it
   deterministically (GREEN 5/5, regression 7/7, smoke 27/27 all completed).
   The full-suite command should carry an explicit `-Xmx` cap while the box
   is this loaded. Root cause of the SIGKILL (OOM-killer vs another agent)
   could not be established from the sandbox (`dmesg` not permitted).

## Not verified here (owed after quota opens)

- Full runtime suite (`make test` in the runtime module / `SchedTestRunner`
  end-to-end) and `make all` gates in a real worktree.
- `make snapshot-check` differential (behavior change surface: none intended
  — previously-crashing/empty-argv programs now get correct argv; programs
  not touching `os.Args` are unaffected — but the gate must still run).
- Independent review + CI on the exact SHA (review is owed; no review
  coverage is claimed here).
